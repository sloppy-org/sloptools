package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/brain"
	"github.com/sloppy-org/sloptools/internal/brain/backend"
	"github.com/sloppy-org/sloptools/internal/brain/ledger"
	"github.com/sloppy-org/sloptools/internal/brain/routing"
	"github.com/sloppy-org/sloptools/internal/brain/scout"
	"github.com/sloppy-org/sloptools/internal/brain/textbook"
)

func encodeIndentJSON(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}

// cmdBrainNight dispatches `sloptools brain night`, the unified
// sweep → scout → judge orchestrator. Each stage is independently
// invokable via --only-stage. Routing is even-split round-robin across
// OpenAI ↔ Anthropic for medium / hard tiers; bulk is local OpenCode Qwen.
// brain.toml at ~/.config/sloptools/brain.toml overrides the defaults.
func cmdBrainNight(args []string) int {
	fs := flag.NewFlagSet("brain night", flag.ContinueOnError)
	configPath := fs.String("config", "", "vault config path")
	sphere := fs.String("sphere", "", "vault sphere: work or private")
	onlyStage := fs.String("only-stage", "", "sweep | scout | judge (default: all)")
	claudeTier := fs.String("claude-tier", "", "force Anthropic at tier: haiku | sonnet | opus")
	openaiTier := fs.String("openai-tier", "", "force OpenAI at tier: mini | full")
	forceLocal := fs.Bool("force-local", false, "pin every stage to the configured local OpenCode Qwen model")
	autonomy := fs.String("autonomy", brain.SleepDefaultAutonomy, "full or plan-only")
	budget := fs.Int("budget", brain.SleepDefaultBudget, "REM notes to dream over (judge stage)")
	nremBudget := fs.Int("nrem-budget", brain.SleepDefaultNREMBudget, "NREM consolidation rows to replay")
	coverageBudget := fs.Int("coverage-budget", brain.SleepDefaultCoverageBudget, "folder coverage changes before NREM")
	dryRun := fs.Bool("dry-run", false, "skip LLM, do not apply prune-links, do not write report file")
	brainTOMLPath := fs.String("brain-toml", "", "override brain.toml path (default ~/.config/sloptools/brain.toml)")
	escalateOnConflict := fs.Bool("escalate-on-conflict", true, "after each bulk-tier scout report, run free opencode self-resolve passes (--self-resolve-passes) and only then escalate to a paid medium-tier reviewer if the classifier still flags the report (default true; pass --escalate-on-conflict=false to skip the self-resolve and paid-escalation path)")
	selfResolvePasses := fs.Int("self-resolve-passes", 1, "number of free opencode self-resolve passes between the bulk pass and a paid escalation, 0-3 (default 1, only applies when --escalate-on-conflict is true)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*sphere) == "" {
		fmt.Fprintln(os.Stderr, "--sphere is required")
		return 2
	}
	stage := strings.TrimSpace(strings.ToLower(*onlyStage))
	if stage != "" && stage != "sweep" && stage != "scout" && stage != "judge" {
		fmt.Fprintf(os.Stderr, "--only-stage must be one of: sweep, scout, judge (got %q)\n", *onlyStage)
		return 2
	}

	cfg, err := brain.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	vault, ok := cfg.Vault(brain.Sphere(*sphere))
	if !ok {
		fmt.Fprintf(os.Stderr, "brain night: unknown vault %q\n", *sphere)
		return 1
	}

	fileCfg, err := routing.LoadFile(*brainTOMLPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	caps := fileCfg.PlanCaps()
	ldg, err := ledger.New(vault.BrainRoot(), caps)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	sessionStart := time.Now().UTC()
	router := routing.New(ldg, routing.Overrides{
		ClaudeTier: strings.TrimSpace(strings.ToLower(*claudeTier)),
		OpenAITier: strings.TrimSpace(strings.ToLower(*openaiTier)),
		ForceLocal: *forceLocal,
	})
	router.SetSessionStart(sessionStart)
	if cfgStages, err := fileCfg.ApplyStages(routing.DefaultStageConfigs()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	} else {
		for s, c := range cfgStages {
			router.SetStageConfig(s, c)
		}
	}

	runID := time.Now().UTC().Format("20060102-150405")
	report := &nightReport{
		Sphere:    string(*sphere),
		StartedAt: time.Now().UTC(),
		RunID:     runID,
		OnlyStage: stage,
		DryRun:    *dryRun,
	}

	if stage == "" || stage == "sweep" {
		if err := runSweepStage(cfg, brain.Sphere(*sphere), *coverageBudget, *dryRun, report); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if err := runTextbookScan(vault, report); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}

	if stage == "" || stage == "scout" {
		if err := runScoutStage(vault, ldg, router, runID, *dryRun, *escalateOnConflict, *selfResolvePasses, report); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}

	if stage == "" || stage == "judge" {
		if err := runJudgeStage(cfg, brain.Sphere(*sphere), brain.SleepOpts{
			Sphere:         brain.Sphere(*sphere),
			Budget:         *budget,
			NREMBudget:     *nremBudget,
			CoverageBudget: *coverageBudget,
			Backend:        brain.SleepBackendCodex,
			Autonomy:       *autonomy,
			DryRun:         *dryRun,
			Router:         router,
			Ledger:         ldg,
			RunID:          runID,
		}, report); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}

	report.EndedAt = time.Now().UTC()
	report.Spend = computeSpend(ldg, sessionStart, report.EndedAt)
	if err := writeNightReport(vault, runID, report); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return printBrainJSON(report)
}

type nightReport struct {
	Sphere      string             `json:"sphere"`
	StartedAt   time.Time          `json:"started_at"`
	EndedAt     time.Time          `json:"ended_at,omitempty"`
	RunID       string             `json:"run_id"`
	OnlyStage   string             `json:"only_stage,omitempty"`
	DryRun      bool               `json:"dry_run"`
	Sweep       *brain.SleepResult `json:"sweep,omitempty"`
	Textbook    *textbook.Summary  `json:"textbook,omitempty"`
	Scout       *scoutSummary      `json:"scout,omitempty"`
	Judge       *brain.SleepResult `json:"judge,omitempty"`
	JudgeReport string             `json:"judge_report_path,omitempty"`
	Spend       *spendSummary      `json:"spend,omitempty"`
}

// spendSummary records the plan-share spent during this nightly run, in
// units of weekly-cap fraction. Per-night cap is 0.05 (5%) per provider
// by default; numbers above that mean the gate failed somewhere.
type spendSummary struct {
	AnthropicSessionShare float64 `json:"anthropic_session_share"`
	OpenAISessionShare    float64 `json:"openai_session_share"`
	AnthropicWeeklyShare  float64 `json:"anthropic_weekly_share"`
	OpenAIWeeklyShare     float64 `json:"openai_weekly_share"`
	SessionStart          string  `json:"session_start"`
}

type scoutSummary struct {
	Status       string   `json:"status"`
	Candidates   int      `json:"candidates"`
	Written      int      `json:"written"`
	SelfResolved int      `json:"self_resolved"`
	Escalated    int      `json:"escalated"`
	Reports      []string `json:"reports,omitempty"`
	Notes        string   `json:"notes,omitempty"`
}

// runSweepStage runs the deterministic, zero-LLM portion of sleep:
// folder-coverage, prune-links scan + apply, dream picker, NREM
// consolidation. The judge step is skipped (Backend=none, DryRun
// follows the caller). The result captures everything the sweep
// produced; the judge stage runs separately.
func runSweepStage(cfg *brain.Config, sphere brain.Sphere, coverageBudget int, dryRun bool, report *nightReport) error {
	res, err := brain.RunSleep(cfg, brain.SleepOpts{
		Sphere:         sphere,
		Budget:         brain.SleepDefaultBudget,
		NREMBudget:     brain.SleepDefaultNREMBudget,
		CoverageBudget: coverageBudget,
		Backend:        brain.SleepBackendNone, // sweep is deterministic
		Autonomy:       brain.SleepAutonomyPlanOnly,
		DryRun:         dryRun,
	})
	if err != nil {
		return fmt.Errorf("sweep: %w", err)
	}
	report.Sweep = res
	return nil
}

// runTextbookScan runs the deny-list classifier over the vault and
// records the per-verdict counts plus reject and compress lists in the
// night report. Zero LLM. Surfaces candidates for the judge stage to
// pick up; never archives or compresses on its own.
func runTextbookScan(vault brain.Vault, report *nightReport) error {
	c := textbook.New()
	s, err := c.Scan(vault.BrainRoot())
	if err != nil {
		return fmt.Errorf("textbook scan: %w", err)
	}
	report.Textbook = &s
	return nil
}

// runScoutStage picks the top stale-or-uncertain canonical entities
// and runs the scout evidence pass over each. Reports land under
// <brain>/reports/scout/<run-id>/<slug>.md. The scout never edits
// canonical Markdown; suggestions are surfaced in the report payload
// and persisted in the per-pick evidence files for the judge stage to
// pick up.
func runScoutStage(vault brain.Vault, ldg *ledger.Ledger, router *routing.Router, runID string, dryRun, escalateOnConflict bool, selfResolvePasses int, report *nightReport) error {
	picks, err := scout.PickEntities(scout.PickerOpts{
		BrainRoot: vault.BrainRoot(),
		Now:       time.Now().UTC(),
		TopN:      10,
	})
	if err != nil {
		return fmt.Errorf("scout pick: %w", err)
	}
	report.Scout = &scoutSummary{
		Status:     "ok",
		Candidates: len(picks),
	}
	if len(picks) == 0 {
		report.Scout.Notes = "no stale entities scored"
		return nil
	}
	res, err := scout.Run(context.Background(), scout.RunOpts{
		BrainRoot:          vault.BrainRoot(),
		Sphere:             string(vault.Sphere),
		Picks:              picks,
		Router:             router,
		Ledger:             ldg,
		RunID:              runID,
		DryRun:             dryRun,
		EscalateOnConflict: escalateOnConflict,
		SelfResolvePasses:  selfResolvePasses,
	})
	if err != nil {
		report.Scout.Status = "error"
		report.Scout.Notes = err.Error()
		return nil
	}
	report.Scout.Candidates = res.Candidates
	report.Scout.Written = res.Written
	report.Scout.SelfResolved = res.SelfResolved
	report.Scout.Escalated = res.Escalated
	for _, e := range res.Reports {
		if e.ReportPath != "" {
			report.Scout.Reports = append(report.Scout.Reports, e.ReportPath)
		}
	}
	if dryRun {
		report.Scout.Status = "dry-run"
	}
	return nil
}

// runJudgeStage runs the editorial pass via the Backend interface and
// wraps it in the integrity gate so the judge's canonical-Markdown
// edits are committed and pushed to the brain repo on success. Without
// this wrapping the judge writes files but leaves the working tree
// dirty, breaking the "git history is the activity log" rule from
// CLAUDE.md.
func runJudgeStage(cfg *brain.Config, sphere brain.Sphere, opts brain.SleepOpts, report *nightReport) error {
	if opts.DryRun {
		res, err := brain.RunSleep(cfg, opts)
		if err != nil {
			return fmt.Errorf("judge: %w", err)
		}
		report.Judge = res
		if res != nil {
			report.JudgeReport = res.ReportPath
		}
		return nil
	}
	var res *brain.SleepResult
	// Subject must match the `^brain sleep:` regex in
	// internal/brain/sleep_git.go::latestSleepCommit so subsequent runs
	// use this commit as the previous-sleep anchor for git scope and
	// for the conversation-log time window. Earlier "brain night: …
	// judge" wording was missed by the regex, causing every nightly to
	// blow its scope back to the last manual `sloptools brain sleep`
	// commit. The (night) qualifier lives in the body, not the subject.
	commitMsg := fmt.Sprintf("brain sleep: %s %s\n\nNight run; judge stage of `sloptools brain night --sphere %s`.\n",
		sphere, time.Now().Format("2006-01-02"), sphere)
	const skipIntegrityGate = true
	err := applyIntegrityGate(cfg, sphere, skipIntegrityGate, commitMsg, func() error {
		var runErr error
		res, runErr = brain.RunSleep(cfg, opts)
		return runErr
	})
	if err != nil {
		return fmt.Errorf("judge: %w", err)
	}
	report.Judge = res
	if res != nil {
		report.JudgeReport = res.ReportPath
	}
	return nil
}

// computeSpend reads the ledger and snapshots the session and weekly
// share for both paid providers. Errors are swallowed: if the ledger
// can't be read the spend simply doesn't appear in the report.
func computeSpend(ldg *ledger.Ledger, sessionStart, now time.Time) *spendSummary {
	out := &spendSummary{SessionStart: sessionStart.Format(time.RFC3339)}
	if v, err := ldg.RollingShare(backend.ProviderAnthropic, sessionStart, now); err == nil {
		out.AnthropicSessionShare = v
	}
	if v, err := ldg.RollingShare(backend.ProviderOpenAI, sessionStart, now); err == nil {
		out.OpenAISessionShare = v
	}
	if v, err := ldg.WeeklyShare(backend.ProviderAnthropic, now); err == nil {
		out.AnthropicWeeklyShare = v
	}
	if v, err := ldg.WeeklyShare(backend.ProviderOpenAI, now); err == nil {
		out.OpenAIWeeklyShare = v
	}
	return out
}

func writeNightReport(vault brain.Vault, runID string, r *nightReport) error {
	if r.DryRun {
		return nil
	}
	dir := filepath.Join(vault.BrainRoot(), "reports", "night")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("night: mkdir report dir: %w", err)
	}
	path := filepath.Join(dir, runID+".json")
	body, err := encodeIndentJSON(r)
	if err != nil {
		return err
	}
	return os.WriteFile(path, body, 0o644)
}
