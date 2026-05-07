// Package ledger maintains a per-provider, rolling-7-day record of every
// brain-night model call. The ledger gates new calls so the user's paid
// plans stay under the configured weekly share. Plan ceilings live in
// ~/.config/sloptools/brain.toml; absent that, conservative defaults
// apply.
//
// The ledger lives at <brain-root>/data/llm-ledger.jsonl. One JSON line
// per call. Append-only by convention; readers can prune older lines as
// a maintenance task.
package ledger

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sloppy-org/sloptools/internal/brain/backend"
)

// Entry is one ledger line.
type Entry struct {
	TS           time.Time         `json:"ts"`
	Sphere       string            `json:"sphere"`
	Stage        string            `json:"stage"`
	Provider     backend.Provider  `json:"provider"`
	Backend      string            `json:"backend"`
	Model        string            `json:"model"`
	TokensIn     int64             `json:"tokens_in"`
	TokensOut    int64             `json:"tokens_out"`
	WallMS       int64             `json:"wall_ms"`
	CostHint     float64           `json:"cost_hint,omitempty"`
	PlanShareEst float64           `json:"plan_share_pct"`
	Extras       map[string]string `json:"extras,omitempty"`
}

// PlanCaps describes the per-provider ceilings used to gate paid CLI
// calls. The ledger enforces both gates (whichever fires first):
//
//   - share-of-weekly-tokens — relative to a placeholder weekly token
//     budget (Anthropic and OpenAI publish no real token quotas for
//     consumer Pro Max plans, so the budget number is a guess; see
//     brain.toml comments).
//   - count-of-paid-calls per night — deterministic, observable, does
//     not depend on the placeholder budget. This is the primary gate
//     in practice.
type PlanCaps struct {
	AnthropicWeeklyShareMax float64 // fraction of weekly cap allowed per night, e.g. 0.05
	OpenAIWeeklyShareMax    float64
	// Approximate tokens-per-week budgets used to translate observed
	// counts into a percentage. These are coarse; the user pays by plan
	// share, not by token, so the share estimate is informational.
	AnthropicTokensPerWeek int64
	OpenAITokensPerWeek    int64
	// Per-night paid-call counts. Zero means unlimited (gate disabled);
	// positive value means refuse the (N+1)th call to that provider in
	// this nightly session. Deterministic; survives over- or under-count
	// in the token scraper.
	AnthropicMaxCallsPerNight int
	OpenAIMaxCallsPerNight    int
}

// DefaultPlanCaps returns sensible defaults. The 30-call per-night
// ceiling is conservative for the documented brain night shape (≤30
// scout picks + ≤1 judge call ≈ 30 paid calls per provider when every
// pick escalates evenly). Adjust in brain.toml for tighter or looser
// budgets.
func DefaultPlanCaps() PlanCaps {
	return PlanCaps{
		AnthropicWeeklyShareMax:   0.05,
		OpenAIWeeklyShareMax:      0.05,
		AnthropicTokensPerWeek:    20_000_000, // coarse placeholder
		OpenAITokensPerWeek:       20_000_000,
		AnthropicMaxCallsPerNight: 30,
		OpenAIMaxCallsPerNight:    30,
	}
}

// Ledger appends and reads ledger entries.
type Ledger struct {
	path string
	caps PlanCaps
	mu   sync.Mutex
}

// New opens (creates) a ledger at <root>/data/llm-ledger.jsonl.
func New(root string, caps PlanCaps) (*Ledger, error) {
	if root == "" {
		return nil, fmt.Errorf("ledger: root required")
	}
	dir := filepath.Join(root, "data")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("ledger: mkdir: %w", err)
	}
	return &Ledger{
		path: filepath.Join(dir, "llm-ledger.jsonl"),
		caps: caps,
	}, nil
}

// Append writes one entry. PlanShareEst is computed if not preset.
func (l *Ledger) Append(e Entry) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if e.TS.IsZero() {
		e.TS = time.Now().UTC()
	}
	if e.PlanShareEst == 0 {
		e.PlanShareEst = l.shareForCall(e.Provider, e.TokensIn+e.TokensOut)
	}
	line, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("ledger: marshal: %w", err)
	}
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("ledger: open: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("ledger: write: %w", err)
	}
	return nil
}

// RollingShare returns the share-of-weekly-cap consumed by provider p
// in the window [since, now]. Result is in [0, ∞) where >= 1.0 means
// the weekly cap is exhausted by that window alone.
func (l *Ledger) RollingShare(p backend.Provider, since, now time.Time) (float64, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	totalTokens := int64(0)
	if err := l.scan(func(e Entry) {
		if e.Provider != p || e.TS.Before(since) || e.TS.After(now) {
			return
		}
		totalTokens += e.TokensIn + e.TokensOut
	}); err != nil {
		return 0, err
	}
	cap := l.providerCap(p)
	if cap <= 0 {
		return 0, nil
	}
	return float64(totalTokens) / float64(cap), nil
}

// WeeklyShare returns the rolling-7-day share-of-cap fraction.
func (l *Ledger) WeeklyShare(p backend.Provider, now time.Time) (float64, error) {
	return l.RollingShare(p, now.Add(-7*24*time.Hour), now)
}

// Guard refuses a new call to provider p when ANY of:
//
//   - per-night call count for this session already at the configured
//     MaxCallsPerNight (deterministic, primary gate);
//   - per-night token share crossed providerShareMax (5% of placeholder
//     weekly token budget — informational, depends on the placeholder);
//   - rolling 7-day token share crossed providerShareMax * 7 (35%).
//
// A zero sessionStart disables both per-night gates (used by tests that
// only care about the weekly ceiling).
func (l *Ledger) Guard(p backend.Provider, sessionStart, now time.Time) error {
	max := l.providerShareMax(p)
	if !sessionStart.IsZero() {
		callCap := l.providerCallCap(p)
		if callCap > 0 {
			calls, err := l.CountCalls(p, sessionStart, now)
			if err != nil {
				return err
			}
			if calls >= callCap {
				return fmt.Errorf("%w: provider=%s session_calls=%d cap=%d", ErrCapExceeded, p, calls, callCap)
			}
		}
		night, err := l.RollingShare(p, sessionStart, now)
		if err != nil {
			return err
		}
		if night >= max {
			return fmt.Errorf("%w: provider=%s nightly_share=%.4f cap=%.4f", ErrCapExceeded, p, night, max)
		}
	}
	week, err := l.WeeklyShare(p, now)
	if err != nil {
		return err
	}
	if week >= max*7 {
		return fmt.Errorf("%w: provider=%s weekly_share=%.4f cap=%.4f", ErrCapExceeded, p, week, max*7)
	}
	return nil
}

// CountCalls returns the number of ledger entries for provider p in the
// window [since, now].
func (l *Ledger) CountCalls(p backend.Provider, since, now time.Time) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	count := 0
	if err := l.scan(func(e Entry) {
		if e.Provider != p || e.TS.Before(since) || e.TS.After(now) {
			return
		}
		count++
	}); err != nil {
		return 0, err
	}
	return count, nil
}

// ErrCapExceeded indicates a provider's rolling weekly use crossed the
// configured cap.
var ErrCapExceeded = errors.New("ledger: weekly cap exceeded")

func (l *Ledger) scan(visit func(Entry)) error {
	f, err := os.Open(l.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("ledger: open: %w", err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for scanner.Scan() {
		var e Entry
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			continue
		}
		visit(e)
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("ledger: scan: %w", err)
	}
	return nil
}

func (l *Ledger) providerCallCap(p backend.Provider) int {
	switch p {
	case backend.ProviderAnthropic:
		return l.caps.AnthropicMaxCallsPerNight
	case backend.ProviderOpenAI:
		return l.caps.OpenAIMaxCallsPerNight
	}
	return 0
}

func (l *Ledger) providerCap(p backend.Provider) int64 {
	switch p {
	case backend.ProviderAnthropic:
		return l.caps.AnthropicTokensPerWeek
	case backend.ProviderOpenAI:
		return l.caps.OpenAITokensPerWeek
	}
	return 0
}

func (l *Ledger) providerShareMax(p backend.Provider) float64 {
	switch p {
	case backend.ProviderAnthropic:
		return l.caps.AnthropicWeeklyShareMax
	case backend.ProviderOpenAI:
		return l.caps.OpenAIWeeklyShareMax
	}
	return 1.0
}

func (l *Ledger) shareForCall(p backend.Provider, tokens int64) float64 {
	cap := l.providerCap(p)
	if cap <= 0 {
		return 0
	}
	return float64(tokens) / float64(cap)
}
