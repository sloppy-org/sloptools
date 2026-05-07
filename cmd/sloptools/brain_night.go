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
	"github.com/sloppy-org/sloptools/internal/brain/ledger"
	"github.com/sloppy-org/sloptools/internal/brain/routing"
	"github.com/sloppy-org/sloptools/internal/brain/scout"
)

func encodeIndentJSON(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}

// cmdBrainNight dispatches `sloptools brain night`, the unified
// sweep → scout → judge orchestrator. Each stage is independently
// invokable via --only-stage. Routing is even-split round-robin across
// OpenAI ↔ Anthropic for medium / hard tiers; bulk is opencode/qwen.
// brain.toml at ~/.config/sloptools/brain.toml overrides the defaults.
func cmdBrainNight(args []string) int {
	fs := flag.NewFlagSet("brain night", flag.ContinueOnError)
	configPath := fs.String("config", "", "vault config path")
	sphere := fs.String("sphere", "", "vault sphere: work or private")
	onlyStage := fs.String("only-stage", "", "sweep | scout | judge (default: all)")
	claudeTier := fs.String("claude-tier", "", "force Anthropic at tier: haiku | sonnet | opus")
	openaiTier := fs.String("openai-tier", "", "force OpenAI at tier: mini | full")
	forceLocal := fs.Bool("force-local", false, "pin every stage to opencode/qwen")
	autonomy := fs.String("autonomy", brain.SleepDefaultAutonomy, "full or plan-only")
	budget := fs.Int("budget", brain.SleepDefaultBudget, "REM notes to dream over (judge stage)")
	nremBudget := fs.Int("nrem-budget", brain.SleepDefaultNREMBudget, "NREM consolidation rows to replay")
	coverageBudget := fs.Int("coverage-budget", brain.SleepDefaultCoverageBudget, "folder coverage changes before NREM")
	dryRun := fs.Bool("dry-run", false, "skip LLM, do not apply prune-links, do not write report file")
	brainTOMLPath := fs.String("brain-toml", "", "override brain.toml path (default ~/.config/sloptools/brain.toml)")
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
	router := routing.New(ldg, routing.Overrides{
		ClaudeTier: strings.TrimSpace(strings.ToLower(*claudeTier)),
		OpenAITier: strings.TrimSpace(strings.ToLower(*openaiTier)),
		ForceLocal: *forceLocal,
	})
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
	}

	if stage == "" || stage == "scout" {
		if err := runScoutStage(vault, ldg, router, runID, *dryRun, report); err != nil {
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
	Scout       *scoutSummary      `json:"scout,omitempty"`
	Judge       *brain.SleepResult `json:"judge,omitempty"`
	JudgeReport string             `json:"judge_report_path,omitempty"`
}

type scoutSummary struct {
	Status   string   `json:"status"`
	Picked   int      `json:"picked"`
	Reports  []string `json:"reports,omitempty"`
	Notes    string   `json:"notes,omitempty"`
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

// runScoutStage picks the top stale-or-uncertain canonical entities
// and runs the scout evidence pass over each. Reports land under
// <brain>/reports/scout/<run-id>/<slug>.md. The scout never edits
// canonical Markdown; suggestions are surfaced in the report payload
// and persisted in the per-pick evidence files for the judge stage to
// pick up.
func runScoutStage(vault brain.Vault, ldg *ledger.Ledger, router *routing.Router, runID string, dryRun bool, report *nightReport) error {
	picks, err := scout.PickEntities(scout.PickerOpts{
		BrainRoot: vault.BrainRoot(),
		Now:       time.Now().UTC(),
		TopN:      10,
	})
	if err != nil {
		return fmt.Errorf("scout pick: %w", err)
	}
	report.Scout = &scoutSummary{
		Status: "ok",
		Picked: len(picks),
	}
	if len(picks) == 0 {
		report.Scout.Notes = "no stale entities scored"
		return nil
	}
	res, err := scout.Run(context.Background(), scout.RunOpts{
		BrainRoot: vault.BrainRoot(),
		Sphere:    string(vault.Sphere),
		Picks:     picks,
		Router:    router,
		Ledger:    ldg,
		RunID:     runID,
		DryRun:    dryRun,
	})
	if err != nil {
		report.Scout.Status = "error"
		report.Scout.Notes = err.Error()
		return nil
	}
	report.Scout.Picked = res.Picked
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

// runJudgeStage runs the editorial pass via the Backend interface.
func runJudgeStage(cfg *brain.Config, sphere brain.Sphere, opts brain.SleepOpts, report *nightReport) error {
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
