// Package bench runs the brain-night model matrix benchmark. It picks
// model + task pairs, executes each call via its CLI backend under a
// scratch sandbox, scores the output, appends to the ledger, and emits
// a Markdown report.
package bench

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/brain/backend"
	"github.com/sloppy-org/sloptools/internal/brain/ledger"
)

// ModelSpec names a single bench cell's model. Provider determines which
// CLI backend handles the call.
type ModelSpec struct {
	Provider  backend.Provider
	BackendID string // "claude", "codex", "opencode"
	Model     string // CLI model identifier
	Reasoning backend.Reasoning
	Label     string // short label used in the report
}

// DefaultModelMatrix is the v1 cell list. Every cell carries an
// explicit Reasoning value: medium for the medium tier (cheap, fast,
// the GPT-5.5 documented "balanced starting point"), high for the
// hard-tier paid models, high for opencode (its --variant maps to
// provider-specific reasoning depth and qwen3.6-35B-A3B benefits from
// it for structured output tasks). Never xhigh by default — that is
// for the hardest asynchronous agentic evals, not the brain night.
//
// Opencode uses the llamacpp/qwen identifier; the user's opencode
// config maps that to qwen3.6-35B-A3B.
func DefaultModelMatrix() []ModelSpec {
	return []ModelSpec{
		{ProviderLocal(), "opencode", "llamacpp/qwen", backend.ReasoningHigh, "opencode/qwen3.6-35B-A3B"},
		{ProviderOpenAI(), "codex", "gpt-5.4-mini", backend.ReasoningMedium, "codex/gpt-5.4-mini@medium"},
		{ProviderOpenAI(), "codex", "gpt-5.5", backend.ReasoningHigh, "codex/gpt-5.5@high"},
		{ProviderAnthropic(), "claude", "claude-haiku-4-5", backend.ReasoningMedium, "claude-haiku-4-5@medium"},
		{ProviderAnthropic(), "claude", "claude-sonnet-4-6", backend.ReasoningMedium, "claude-sonnet-4-6@medium"},
		{ProviderAnthropic(), "claude", "claude-opus-4-7", backend.ReasoningHigh, "claude-opus-4-7@high"},
	}
}

// ProviderLocal/OpenAI/Anthropic are alias accessors so tests that
// build their own matrices do not have to import backend.
func ProviderLocal() backend.Provider     { return backend.ProviderLocal }
func ProviderOpenAI() backend.Provider    { return backend.ProviderOpenAI }
func ProviderAnthropic() backend.Provider { return backend.ProviderAnthropic }

// Cell is one (task, fixture, model) tuple; one CLI invocation. With
// Options.Draws > 1, the same tuple can produce multiple cells with
// Draw = 1..N for stochastic stability assessment.
type Cell struct {
	TaskID    string
	FixtureID string
	Model     ModelSpec
	Draw      int
	Output    string
	Score     float64
	Passes    bool
	Rationale string
	WallMS    int64
	TokensIn  int64
	TokensOut int64
	Skipped   bool
	SkipKind  string
	Err       string

	// LLM judge fields (populated when Options.Judge is set).
	JudgeUsed   bool
	JudgePasses bool
	JudgeScore  float64
	JudgeFacts  []string
	JudgeNote   string
	JudgeErr    string
}

// Result is the full bench output.
type Result struct {
	Started time.Time
	Ended   time.Time
	Cells   []Cell
	OutDir  string
}

// Options is the input to Run.
type Options struct {
	Tasks     []Task
	Models    []ModelSpec
	OutDir    string // <brain-root>/data/brain/bench/<date>/
	PromptDir string // packaged prompt directory (internal/brain/prompts)
	RunID     string // shared sandbox prefix
	Ledger    *ledger.Ledger
	Sphere    string
	// Judge, when set, runs a second-pass LLM judge over each cell's
	// output to catch quality issues the deterministic rubric misses
	// (invented facts, degenerate H1, empty sections that should not be
	// empty). v1 default uses the bench's own routing tier; production
	// recommendation from the v1 manual re-grade is claude-sonnet-4-6
	// @ medium.
	Judge *Judge
	// Draws is the number of stochastic replicas per (task, fixture,
	// model) cell. 0 or 1 yields a single draw (the current default).
	// Larger values fan out and report mean + stdev per cell. Each draw
	// is a fresh CLI invocation; ledger guard runs per-draw so a partway
	// saturation skips the remaining draws cleanly.
	Draws int
	// SessionStart pins the start of this bench run for the per-night
	// 5%-of-plan-share gate; zero means weekly-only gating.
	SessionStart time.Time
}

// Run executes every (task, fixture, model) cell.
func Run(ctx context.Context, opts Options) (*Result, error) {
	if len(opts.Tasks) == 0 {
		return nil, errors.New("bench: no tasks")
	}
	if len(opts.Models) == 0 {
		return nil, errors.New("bench: no models")
	}
	if opts.OutDir == "" {
		return nil, errors.New("bench: OutDir required")
	}
	if opts.PromptDir == "" {
		return nil, errors.New("bench: PromptDir required")
	}
	if opts.RunID == "" {
		opts.RunID = time.Now().UTC().Format("20060102-150405")
	}
	if opts.SessionStart.IsZero() {
		opts.SessionStart = time.Now().UTC()
	}
	if opts.Draws < 1 {
		opts.Draws = 1
	}
	if err := os.MkdirAll(filepath.Join(opts.OutDir, "raw"), 0o755); err != nil {
		return nil, fmt.Errorf("bench: mkdir: %w", err)
	}
	res := &Result{
		Started: time.Now().UTC(),
		OutDir:  opts.OutDir,
	}
	for _, task := range opts.Tasks {
		fixtures, err := task.Fixtures()
		if err != nil {
			return nil, fmt.Errorf("bench: %s fixtures: %w", task.ID(), err)
		}
		stagePrompt := filepath.Join(opts.PromptDir, task.PromptFile())
		for _, f := range fixtures {
			for _, m := range opts.Models {
				for d := 1; d <= opts.Draws; d++ {
					cell := runCell(ctx, opts, task, f, stagePrompt, m)
					cell.Draw = d
					res.Cells = append(res.Cells, cell)
					_ = saveRaw(opts.OutDir, cell)
					if cell.Skipped && cell.SkipKind == "weekly_cap_exceeded" {
						break
					}
				}
			}
		}
	}
	res.Ended = time.Now().UTC()
	return res, nil
}

func runCell(ctx context.Context, opts Options, task Task, f Fixture, stagePrompt string, m ModelSpec) Cell {
	cell := Cell{TaskID: task.ID(), FixtureID: f.ID, Model: m}
	if opts.Ledger != nil {
		if err := opts.Ledger.Guard(m.Provider, opts.SessionStart, time.Now()); err != nil {
			cell.Skipped = true
			cell.SkipKind = "weekly_cap_exceeded"
			cell.Err = err.Error()
			return cell
		}
	}
	stageID := fmt.Sprintf("%s-%s-%s", task.ID(), f.ID, sanitize(m.Label))
	sb, err := backend.NewSandbox(opts.RunID, stageID, stagePrompt, backend.DefaultMCPConfig())
	if err != nil {
		cell.Err = err.Error()
		return cell
	}
	defer sb.Cleanup()

	be, err := newBackendByID(m.BackendID)
	if err != nil {
		cell.Err = err.Error()
		return cell
	}
	req := backend.Request{
		Stage:            stageID,
		Packet:           f.Packet,
		SystemPromptPath: sb.SystemPromptIn,
		Model:            m.Model,
		Reasoning:        m.Reasoning,
		AllowEdits:       false,
		Sandbox:          sb,
	}
	resp, err := be.Run(ctx, req)
	if err != nil {
		cell.Err = err.Error()
		return cell
	}
	cell.Output = resp.Output
	cell.WallMS = resp.WallMS
	cell.TokensIn = resp.TokensIn
	cell.TokensOut = resp.TokensOut

	score, pass, rationale := task.Score(f, resp.Output)
	cell.Score = score
	cell.Passes = pass
	cell.Rationale = rationale

	if opts.Judge != nil {
		v, jerr := runJudge(ctx, *opts.Judge, opts.RunID, task.ID(), f, resp.Output)
		cell.JudgeUsed = true
		if jerr != nil {
			cell.JudgeErr = jerr.Error()
		} else {
			cell.JudgePasses = v.Passes
			cell.JudgeScore = v.Score
			cell.JudgeFacts = v.InventedFacts
			cell.JudgeNote = v.Rationale
		}
	}

	if opts.Ledger != nil {
		_ = opts.Ledger.Append(ledger.Entry{
			Sphere:    opts.Sphere,
			Stage:     stageID,
			Provider:  m.Provider,
			Backend:   m.BackendID,
			Model:     m.Model,
			TokensIn:  resp.TokensIn,
			TokensOut: resp.TokensOut,
			WallMS:    resp.WallMS,
			CostHint:  resp.CostHint,
			Extras:    map[string]string{"task": task.ID(), "fixture": f.ID},
		})
	}
	return cell
}

func newBackendByID(id string) (backend.Backend, error) {
	switch id {
	case "claude":
		return backend.ClaudeBackend{}, nil
	case "codex":
		return backend.CodexBackend{}, nil
	case "opencode":
		return backend.OpencodeBackend{}, nil
	}
	return nil, fmt.Errorf("bench: unknown backend id: %s", id)
}

func sanitize(label string) string {
	out := make([]rune, 0, len(label))
	for _, r := range label {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			out = append(out, r)
		case r == '-' || r == '_':
			out = append(out, r)
		default:
			out = append(out, '-')
		}
	}
	return strings.Trim(string(out), "-")
}
