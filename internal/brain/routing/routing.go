// Package routing picks a Backend + Model + Reasoning per stage of the
// brain night. It enforces three rules:
//
//   - paid pools: medium and hard tiers use configured paid providers;
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
	BackendID string // "codex" | "llamacpp"
	Provider  backend.Provider
	Model     string
	Reasoning backend.Reasoning
	Label     string
	MCPTools  []string       // curated allowlist for LlamacppBackend; nil = all tools
	MCPQuotas map[string]int // per-tool call cap; nil = unbounded
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
	Stage      Stage
	Tier       Tier
	Bulk       Choice         // local primary
	ValueLocal Choice         // resolve-pass local model; zero means reuse Bulk
	Medium     []Choice       // paid medium pool
	Hard       []Choice       // paid hard pool
	Fallback   Choice         // when all paid providers are saturated
	MCPTools   []string       // default MCP tool allowlist; overridable by brain.toml
	MCPQuotas  map[string]int // per-tool call cap inside the agent loop; absent = unbounded
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
	OpenAITier string // "mini"  | "full"  | "pro"   — force OpenAI    + tier
	ForceLocal bool   // pin every stage to LlamacppMoEModel
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
		return choiceToPick(s, cfg.Tier, cfg.Bulk, cfg.MCPTools, cfg.MCPQuotas), nil
	}
	if c, ok := r.applyOpenAIOverride(cfg); ok {
		return choiceToPick(s, cfg.Tier, c, cfg.MCPTools, cfg.MCPQuotas), nil
	}
	pool := r.poolForTier(cfg)
	if len(pool) == 0 {
		return choiceToPick(s, cfg.Tier, cfg.Bulk, cfg.MCPTools, cfg.MCPQuotas), nil
	}
	return r.roundRobin(s, cfg, pool), nil
}

// PickValueLocal returns the resolve-tier pick for a stage. When
// StageConfig.ValueLocal is zero, falls back to Bulk.
func (r *Router) PickValueLocal(s Stage) (Pick, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cfg, ok := r.cfg[s]
	if !ok {
		return Pick{}, fmt.Errorf("routing: unknown stage %s", s)
	}
	c := cfg.ValueLocal
	if c.BackendID == "" {
		c = cfg.Bulk
	}
	return choiceToPick(s, cfg.Tier, c, cfg.MCPTools, cfg.MCPQuotas), nil
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
		return choiceToPick(s, cfg.Tier, c, cfg.MCPTools, cfg.MCPQuotas)
	}
	return choiceToPick(s, cfg.Tier, cfg.Fallback, cfg.MCPTools, cfg.MCPQuotas)
}

func choiceToPick(s Stage, t Tier, c Choice, tools []string, quotas map[string]int) Pick {
	return Pick{
		Stage:     s,
		Tier:      t,
		BackendID: c.BackendID,
		Provider:  c.Provider,
		Model:     c.Model,
		Reasoning: c.Reasoning,
		Label:     c.Label,
		MCPTools:  tools,
		MCPQuotas: quotas,
	}
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
//     external evidence needed; deterministic from inputs. LlamacppMoE.
//   - scout (bulk): live web + Zotero + TUGonline + vault verification.
//     The 2026-05-07 first nightly proved llamacpp/qwen3-MoE with helpy +
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
//     Hard tier uses gpt-5.5. STAYS hard until a structural-integrity gate
//     exists for canonical Markdown writes; that gate is its own project.
//
// The --escalate-on-conflict flag (scout today) is the proven pattern
// for "bulk first, paid only on classifier doubt". The shared
// internal/brain/cleanup package and the audit-sidecar layer in
// internal/brain/scout/artifacts.go are the foundation for extending
// this to sleep-judge, compress, and entity-write once each stage has
// a deterministic classifier.
// scoutDefaultMCPTools is the curated tool allowlist for the scout stage.
// Tool names must match what sloppy and helpy expose (flat names, no prefix).
var scoutDefaultMCPTools = []string{
	"sloppy_brain",
	"web_search", "web_fetch", "pdf_read",
	"helpy_zotero", "helpy_tugonline", "helpy_tu4u", "helpy_ics",
}

// scoutDefaultMCPQuotas caps how many times each tool may be called inside
// one scout agent loop. The cumulative cost of unbounded calls is what
// produced the 2026-05-11 night of 2651 web_search hits and rate-limit
// hell — a 30-min wall budget alone is not enough when each loop can fire
// dozens of probes per minute. Tight defaults; relax in brain.toml only
// when a stage demonstrates it needs more.
var scoutDefaultMCPQuotas = map[string]int{
	"web_search":      5,
	"web_fetch":       8,
	"pdf_read":        6,
	"helpy_zotero":    4,
	"helpy_tugonline": 3,
	"helpy_tu4u":      3,
	"helpy_ics":       3,
}

// folderNoteDefaultMCPTools is the curated tool allowlist for folder-note.
var folderNoteDefaultMCPTools = []string{
	"sloppy_brain",
	"web_fetch", "pdf_read", "image_read", "helpy_office",
}

var folderNoteDefaultMCPQuotas = map[string]int{
	"web_fetch":    4,
	"pdf_read":     6,
	"image_read":   8,
	"helpy_office": 4,
}

// sleepJudgeFullAutonomyTools is the curated allowlist for sleep-judge when
// AllowEdits=true. Includes sloppy_brain (action=note_write for vault edits)
// and helpy tools for external fact confirmation.
var sleepJudgeFullAutonomyTools = []string{
	"sloppy_brain",
	"web_search", "web_fetch", "pdf_read",
}

var sleepJudgeDefaultMCPQuotas = map[string]int{
	"web_search": 4,
	"web_fetch":  6,
	"pdf_read":   4,
}

func DefaultStageConfigs() map[Stage]StageConfig {
	return map[Stage]StageConfig{
		StageFolderNote:  bulkStage(StageFolderNote, folderNoteDefaultMCPTools, folderNoteDefaultMCPQuotas),
		StageScout:       bulkStage(StageScout, scoutDefaultMCPTools, scoutDefaultMCPQuotas),
		StageTriage:      mediumStage(StageTriage, nil, nil),
		StageSleepJudge:  mediumStage(StageSleepJudge, sleepJudgeFullAutonomyTools, sleepJudgeDefaultMCPQuotas),
		StageCompress:    hardStage(StageCompress, nil, nil),
		StageEntityWrite: hardStage(StageEntityWrite, nil, nil),
	}
}

func bulkStage(s Stage, tools []string, quotas map[string]int) StageConfig {
	bulk := LlamacppMoEBulk()
	return StageConfig{
		Stage:      s,
		Tier:       TierBulk,
		Bulk:       bulk,
		ValueLocal: LlamacppMoEBulk(),
		Fallback:   bulk,
		MCPTools:   tools,
		MCPQuotas:  quotas,
	}
}

func mediumStage(s Stage, tools []string, quotas map[string]int) StageConfig {
	bulk := LlamacppMoEBulk()
	return StageConfig{
		Stage:     s,
		Tier:      TierMedium,
		Bulk:      bulk,
		Medium:    []Choice{CodexMiniMedium()},
		Fallback:  bulk,
		MCPTools:  tools,
		MCPQuotas: quotas,
	}
}

func hardStage(s Stage, tools []string, quotas map[string]int) StageConfig {
	bulk := LlamacppMoEBulk()
	return StageConfig{
		Stage:     s,
		Tier:      TierHard,
		Bulk:      bulk,
		Hard:      []Choice{CodexFullHigh()},
		Fallback:  bulk,
		MCPTools:  tools,
		MCPQuotas: quotas,
	}
}

// MCPToolsFor returns the configured MCP tool allowlist for a stage,
// or nil when the stage has none. Used by callsites that build their
// own Choice instead of going through Pick (sleep-judge runBulk).
func (r *Router) MCPToolsFor(s Stage) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cfg[s].MCPTools
}

// Choice constructors. Reasoning is mandatory; never xhigh by default.

const (
	// LlamacppMoEModel routes to slopgate's qwen3-MoE alias (fast, ~0.3s/token).
	// Used for bulk passes and as the saturation fallback for medium / hard stages.
	LlamacppMoEModel     = "llamacpp-moe/qwen"
	LlamacppMoELabel     = "llamacpp/qwen3-MoE"
	LlamacppMoEBackendID = "llamacpp"
)

// LlamacppMoEBulk returns a Choice for the fast MoE bulk pass via direct
// HTTP (no subprocess overhead).
func LlamacppMoEBulk() Choice {
	return Choice{
		BackendID: LlamacppMoEBackendID,
		Provider:  backend.ProviderLocal,
		Model:     LlamacppMoEModel,
		Reasoning: backend.ReasoningHigh,
		Label:     LlamacppMoELabel,
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
