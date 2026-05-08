// Package routing picks a Backend + Model + Reasoning per stage of the
// brain night. It enforces three rules:
//
//   - even split: medium and hard tiers alternate OpenAI ↔ Anthropic per
//     call so neither plan dominates and neither plan goes unused;
//   - ledger gate: a saturated provider is skipped (and the stage falls
//     back to the unsaturated peer; if both are saturated, the stage
//     downgrades to opencode/local);
//   - explicit reasoning: every Pick returns a Reasoning value; the
//     caller never has to default to xhigh.
//
// Stage definitions live in code (the source-of-truth defaults) and may
// be overridden by ~/.config/sloptools/brain.toml.
package routing

import (
	"fmt"
	"sync"
	"time"

	"github.com/sloppy-org/sloptools/internal/brain/backend"
	"github.com/sloppy-org/sloptools/internal/brain/ledger"
)

// Stage names the brain-night step a Pick is for. Each stage carries a
// tier (Bulk / Medium / Hard) that determines which providers are
// eligible.
type Stage string

const (
	// StageFolderNote authors strict folder notes from a packet. Bulk.
	StageFolderNote Stage = "folder-note"
	// StageTriage promotes/maybes/rejects candidate entities. Medium.
	StageTriage Stage = "triage"
	// StageSleepJudge runs the editorial pass over a sleep packet. Medium.
	StageSleepJudge Stage = "sleep-judge"
	// StageScout runs bounded helpy MCP web/Zotero/TUGonline lookup. Bulk.
	StageScout Stage = "scout"
	// StageCompress shrinks textbook prose while keeping local anchors. Medium.
	StageCompress Stage = "compress"
	// StageEntityWrite authors canonical entity Markdown. Hard.
	StageEntityWrite Stage = "entity-write"
)

// Tier classifies a stage by quality requirement.
type Tier string

const (
	TierBulk   Tier = "bulk"
	TierMedium Tier = "medium"
	TierHard   Tier = "hard"
)

// Pick is the routing decision for one call.
type Pick struct {
	Stage     Stage
	Tier      Tier
	BackendID string // "claude" | "codex" | "opencode"
	Provider  backend.Provider
	Model     string
	Reasoning backend.Reasoning
	Label     string // "claude-sonnet-4-6@medium" etc.
}

// Choice is one eligible model for a tier slot.
type Choice struct {
	BackendID string
	Provider  backend.Provider
	Model     string
	Reasoning backend.Reasoning
	Label     string
}

// StageConfig binds a stage to its tier and the eligible model choices.
type StageConfig struct {
	Stage    Stage
	Tier     Tier
	Bulk     Choice   // local primary (always opencode/qwen)
	Medium   []Choice // even-split round-robin pool (one OpenAI + one Anthropic)
	Hard     []Choice // even-split round-robin pool (one OpenAI + one Anthropic)
	Fallback Choice   // when all paid providers are saturated
}

// Router holds the loaded stage configs, the ledger, and the per-stage
// round-robin counters that drive the even split.
type Router struct {
	cfg          map[Stage]StageConfig
	ledger       *ledger.Ledger
	overrides    Overrides
	mu           sync.Mutex
	rr           map[Stage]int
	now          func() time.Time
	sessionStart time.Time
}

// Overrides are session-level routing overrides set by CLI flags.
type Overrides struct {
	ClaudeTier string // "haiku" | "sonnet" | "opus" — force Anthropic + tier
	OpenAITier string // "mini"  | "full"  | "pro"   — force OpenAI    + tier
	ForceLocal bool   // pin every stage to opencode/qwen
}

// New builds a Router with the default stage configs.
func New(l *ledger.Ledger, ov Overrides) *Router {
	return &Router{
		cfg:          DefaultStageConfigs(),
		ledger:       l,
		overrides:    ov,
		rr:           make(map[Stage]int),
		now:          time.Now,
		sessionStart: time.Now(),
	}
}

// SetSessionStart pins the start of this nightly run so the per-night
// 5% cap can be enforced. Called once by the brain night entrypoint.
func (r *Router) SetSessionStart(t time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessionStart = t
}

// SetStageConfig replaces one stage's config (used by brain.toml loader).
func (r *Router) SetStageConfig(s Stage, c StageConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cfg == nil {
		r.cfg = make(map[Stage]StageConfig)
	}
	r.cfg[s] = c
}

// Pick returns the routing decision for a stage. It applies the override
// flags first, then the round-robin selector with ledger guard. When all
// paid providers are saturated, returns the bulk/local fallback.
func (r *Router) Pick(s Stage) (Pick, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cfg, ok := r.cfg[s]
	if !ok {
		return Pick{}, fmt.Errorf("routing: unknown stage %s", s)
	}
	if r.overrides.ForceLocal {
		return choiceToPick(s, cfg.Tier, cfg.Bulk), nil
	}
	if c, ok := r.applyClaudeOverride(cfg); ok {
		return choiceToPick(s, cfg.Tier, c), nil
	}
	if c, ok := r.applyOpenAIOverride(cfg); ok {
		return choiceToPick(s, cfg.Tier, c), nil
	}
	pool := r.poolForTier(cfg)
	if len(pool) == 0 {
		return choiceToPick(s, cfg.Tier, cfg.Bulk), nil
	}
	return r.roundRobin(s, cfg, pool), nil
}

func (r *Router) poolForTier(cfg StageConfig) []Choice {
	switch cfg.Tier {
	case TierHard:
		return cfg.Hard
	case TierMedium:
		return cfg.Medium
	}
	return nil
}

// roundRobin advances the per-stage counter, picks the next-eligible
// peer, and returns the Pick. If all peers in the pool are saturated,
// returns the bulk fallback.
func (r *Router) roundRobin(s Stage, cfg StageConfig, pool []Choice) Pick {
	now := r.now()
	start := r.rr[s] % len(pool)
	for i := 0; i < len(pool); i++ {
		idx := (start + i) % len(pool)
		c := pool[idx]
		if r.ledger != nil {
			if err := r.ledger.Guard(c.Provider, r.sessionStart, now); err != nil {
				continue
			}
		}
		r.rr[s] = (idx + 1) % len(pool)
		return choiceToPick(s, cfg.Tier, c)
	}
	return choiceToPick(s, cfg.Tier, cfg.Fallback)
}

func choiceToPick(s Stage, t Tier, c Choice) Pick {
	return Pick{
		Stage:     s,
		Tier:      t,
		BackendID: c.BackendID,
		Provider:  c.Provider,
		Model:     c.Model,
		Reasoning: c.Reasoning,
		Label:     c.Label,
	}
}

func (r *Router) applyClaudeOverride(cfg StageConfig) (Choice, bool) {
	switch r.overrides.ClaudeTier {
	case "haiku":
		return ClaudeHaikuMedium(), true
	case "sonnet":
		return ClaudeSonnetMedium(), true
	case "opus":
		return ClaudeOpusHigh(), true
	}
	return Choice{}, false
}

func (r *Router) applyOpenAIOverride(cfg StageConfig) (Choice, bool) {
	switch r.overrides.OpenAITier {
	case "mini":
		return CodexMiniMedium(), true
	case "full":
		return CodexFullHigh(), true
	}
	return Choice{}, false
}

// DefaultStageConfigs returns the stage→tier map. Evidence and rationale:
//
//   - folder-note (bulk): text-only synthesis from attached files; no
//     external evidence needed; deterministic from inputs. Opencode/qwen.
//   - scout (bulk): live web + Zotero + TUGonline + vault verification.
//     The 2026-05-07 first nightly proved opencode/qwen with helpy +
//     sloppy MCP produces research-grade evidence reports. Bulk only,
//     with `--escalate-on-conflict` (default true) routing flagged
//     reports through a free self-resolve pass and then a paid medium
//     pass when the deterministic classifier still flags the body.
//   - triage (medium): the routing name "triage" here is the escalation
//     POOL used by scout when the classifier flags content. It is NOT
//     a separate triage worker stage — there is no production call site
//     that picks `StageTriage` outside escalation. Tier stays medium;
//     the pool is codex/gpt-5.4-mini@medium only. Anthropic backends
//     were removed 2026-05-08 because the `claude` CLI subprocess
//     pattern (consumer-OAuth tokens, sibling-process refresh races
//     that log the user out of their interactive session) is not the
//     right shape for unattended batch escalations. Re-add only with
//     ANTHROPIC_API_KEY-based auth, never via the Pro/Max OAuth flow.
//     Renaming this stage to `StageEscalate` is a follow-up cleanup.
//   - sleep-judge (medium): editorial pass over a rendered sleep packet.
//     The packet bakes in citations; the model needs taste, not
//     retrieval. STAYS medium today; the bulk-first / paid-on-doubt
//     migration here needs a deterministic classifier of integrity-
//     gate output (expected sections retained, no forbidden sections
//     reintroduced, no code-fenced wrapper). Tracked as follow-up;
//     the shared cleanup package (internal/brain/cleanup) is in place
//     so the migration is a small refactor away.
//   - compress (hard): shrink textbook prose while preserving local
//     anchors. STAYS hard until the v1 fx-neort re-grade observation
//     ("medium tier on technical packets at risk of fabricating jargon")
//     is overturned by live evidence. No production call site today
//     either; the bench task exercises it but no nightly stage routes
//     through it yet.
//   - entity-write (hard): writes canonical entity notes. Highest stakes.
//     Hard tier (gpt-5.5 ↔ claude-sonnet) round-robin. STAYS hard until
//     a structural-integrity gate exists for canonical Markdown writes;
//     that gate is its own project.
//
// The --escalate-on-conflict flag (scout today) is the proven pattern
// for "bulk first, paid only on classifier doubt". The shared
// internal/brain/cleanup package and the audit-sidecar layer in
// internal/brain/scout/artifacts.go are the foundation for extending
// this to sleep-judge, compress, and entity-write once each stage has
// a deterministic classifier.
func DefaultStageConfigs() map[Stage]StageConfig {
	return map[Stage]StageConfig{
		StageFolderNote:  bulkStage(StageFolderNote),
		StageScout:       bulkStage(StageScout),
		StageTriage:      mediumStage(StageTriage),
		StageSleepJudge:  mediumStage(StageSleepJudge),
		StageCompress:    hardStage(StageCompress),
		StageEntityWrite: hardStage(StageEntityWrite),
	}
}

func bulkStage(s Stage) StageConfig {
	bulk := OpencodeQwenHigh()
	return StageConfig{
		Stage:    s,
		Tier:     TierBulk,
		Bulk:     bulk,
		Fallback: bulk,
	}
}

func mediumStage(s Stage) StageConfig {
	bulk := OpencodeQwenHigh()
	return StageConfig{
		Stage: s,
		Tier:  TierMedium,
		Bulk:  bulk,
		Medium: []Choice{
			CodexMiniMedium(),
		},
		Fallback: bulk,
	}
}

func hardStage(s Stage) StageConfig {
	bulk := OpencodeQwenHigh()
	return StageConfig{
		Stage: s,
		Tier:  TierHard,
		Bulk:  bulk,
		Hard: []Choice{
			CodexFullHigh(),
		},
		Fallback: bulk,
	}
}

// Choice constructors. Reasoning is mandatory; never xhigh by default.

func OpencodeQwenHigh() Choice {
	return Choice{
		BackendID: "opencode",
		Provider:  backend.ProviderLocal,
		Model:     "llamacpp/qwen",
		Reasoning: backend.ReasoningHigh,
		Label:     "opencode/qwen3.6-35B-A3B",
	}
}

func CodexMiniMedium() Choice {
	return Choice{
		BackendID: "codex",
		Provider:  backend.ProviderOpenAI,
		Model:     "gpt-5.4-mini",
		Reasoning: backend.ReasoningMedium,
		Label:     "codex/gpt-5.4-mini@medium",
	}
}

func CodexFullHigh() Choice {
	return Choice{
		BackendID: "codex",
		Provider:  backend.ProviderOpenAI,
		Model:     "gpt-5.5",
		Reasoning: backend.ReasoningHigh,
		Label:     "codex/gpt-5.5@high",
	}
}

func ClaudeHaikuMedium() Choice {
	return Choice{
		BackendID: "claude",
		Provider:  backend.ProviderAnthropic,
		Model:     "claude-haiku-4-5",
		Reasoning: backend.ReasoningMedium,
		Label:     "claude-haiku-4-5@medium",
	}
}

func ClaudeSonnetMedium() Choice {
	return Choice{
		BackendID: "claude",
		Provider:  backend.ProviderAnthropic,
		Model:     "claude-sonnet-4-6",
		Reasoning: backend.ReasoningMedium,
		Label:     "claude-sonnet-4-6@medium",
	}
}

func ClaudeOpusHigh() Choice {
	return Choice{
		BackendID: "claude",
		Provider:  backend.ProviderAnthropic,
		Model:     "claude-opus-4-7",
		Reasoning: backend.ReasoningHigh,
		Label:     "claude-opus-4-7@high",
	}
}
