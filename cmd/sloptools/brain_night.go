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
	"github.com/sloppy-org/sloptools/internal/brain/activity"
	brainEdit "github.com/sloppy-org/sloptools/internal/brain/edit"
	"github.com/sloppy-org/sloptools/internal/brain/ledger"
	"github.com/sloppy-org/sloptools/internal/brain/routing"
	"github.com/sloppy-org/sloptools/internal/brain/scout"
	"github.com/sloppy-org/sloptools/internal/brain/textbook"
	"github.com/sloppy-org/sloptools/internal/brain/triage"
)

// scoutCursor tracks which picks have been processed recently so large
// vaults cycle through all entities across multiple nights rather than
// always re-processing the highest-scored ones.
type scoutCursor struct {
	Processed []scoutCursorEntry `json:"processed"`
}

type scoutCursorEntry struct {
	Path        string    `json:"path"`
	ProcessedAt time.Time `json:"processed_at"`
}

func scoutCursorPath(sphere string) string {
	xdgConfig := os.Getenv("XDG_CONFIG_HOME")
	if xdgConfig == "" {
		home, _ := os.UserHomeDir()
		xdgConfig = filepath.Join(home, ".config")
	}
	return filepath.Join(xdgConfig, "sloptools", "scout-cursor-"+sphere+".json")
}

func loadScoutCursor(path string) scoutCursor {
	body, err := os.ReadFile(path)
	if err != nil {
		return scoutCursor{}
	}
	var c scoutCursor
	if err := json.Unmarshal(body, &c); err != nil {
		return scoutCursor{}
	}
	// Drop entries older than 7 days.
	cutoff := time.Now().UTC().Add(-7 * 24 * time.Hour)
	fresh := c.Processed[:0]
	for _, e := range c.Processed {
		if e.ProcessedAt.After(cutoff) {
			fresh = append(fresh, e)
		}
	}
	c.Processed = fresh
	return c
}

func saveScoutCursor(path string, c scoutCursor) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	body, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, body, 0o600)
}

func encodeIndentJSON(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}

// cmdBrainNight dispatches `sloptools brain night`, the unified
// sweep → scout → judge orchestrator. Each stage is independently
// invokable via --only-stage. Routing uses local qwen for bulk, local
// qwen122b for review/edit, gpt-5.4-mini for native-web scout fallback,
// and gpt-5.5 for hard escalation.
// brain.toml at ~/.config/sloptools/brain.toml overrides the defaults.
func cmdBrainNight(args []string) int {
	fs := flag.NewFlagSet("brain night", flag.ContinueOnError)
	configPath := fs.String("config", "", "vault config path")
	sphere := fs.String("sphere", "", "vault sphere: work or private")
	onlyStage := fs.String("only-stage", "", "sweep | scout | judge (default: all)")
	openaiTier := fs.String("openai-tier", "", "force OpenAI at tier: mini-native-web | full")
	forceLocal := fs.Bool("force-local", false, "pin every stage to the configured local llamacpp model")
	autonomy := fs.String("autonomy", brain.SleepDefaultAutonomy, "full or plan-only")
	budget := fs.Int("budget", brain.SleepDefaultBudget, "REM notes to dream over (judge stage)")
	nremBudget := fs.Int("nrem-budget", brain.SleepDefaultNREMBudget, "NREM consolidation rows to replay")
	coverageBudget := fs.Int("coverage-budget", brain.SleepDefaultCoverageBudget, "folder coverage changes before NREM")
	dryRun := fs.Bool("dry-run", false, "skip LLM, do not apply prune-links, do not write report file")
	brainTOMLPath := fs.String("brain-toml", "", "override brain.toml path (default ~/.config/sloptools/brain.toml)")
	escalateOnConflict := fs.Bool("escalate-on-conflict", true, "after each bulk-tier scout report, run free llamacpp self-resolve passes (--self-resolve-passes) and only then escalate to a paid medium-tier reviewer if the classifier still flags the report (default true; pass --escalate-on-conflict=false to skip the self-resolve and paid-escalation path)")
	selfResolvePasses := fs.Int("self-resolve-passes", 1, "number of free llamacpp self-resolve passes between the bulk pass and a paid escalation, 0-3 (default 1, only applies when --escalate-on-conflict is true)")
	maxScoutItems := fs.Int("max-scout-items", 20, "maximum scout picks to process per run (0 = unlimited); unprocessed picks roll over to the next run")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*sphere) == "" {
		fmt.Fprintln(os.Stderr, "--sphere is required")
		return 2
	}
	stage := strings.TrimSpace(strings.ToLower(*onlyStage))
	validStages := map[string]bool{"sync": true, "sweep": true, "scout": true, "triage": true, "edit": true, "propose": true, "feedback": true, "judge": true}
	if stage != "" && !validStages[stage] {
		fmt.Fprintf(os.Stderr, "--only-stage must be one of: sync, sweep, scout, triage, edit, propose, feedback, judge (got %q)\n", *onlyStage)
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
	nightLog("start sphere=%s run=%s stage=%s dry_run=%t", *sphere, runID, stageLabel(stage), *dryRun)

	if stage == "" || stage == "sync" || stage == "sweep" {
		nightLog("sync: collecting calendar, mail, and brain git activity")
		runSyncStage(vault, brain.Sphere(*sphere), report.StartedAt, runID, report)
		nightLog("sync: done digest_bytes=%d", len(report.ActivityDigest))
	}

	if stage == "" || stage == "sweep" {
		nightLog("sweep: deterministic folder coverage, link pruning, NREM replay")
		if err := runSweepStage(cfg, brain.Sphere(*sphere), *coverageBudget, *dryRun, report); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		nightLog("sweep: done")
		nightLog("textbook: scanning vault prose")
		if err := runTextbookScan(vault, report); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if report.Textbook != nil {
			nightLog("textbook: done files=%d reject=%d compress=%d", report.Textbook.Total, report.Textbook.Reject, report.Textbook.Compress)
		}
	}

	if stage == "" || stage == "scout" {
		nightLog("scout: picking stale or uncertain canonical entities")
		if err := runScoutStage(vault, ldg, router, runID, *dryRun, *escalateOnConflict, *selfResolvePasses, *maxScoutItems, string(*sphere), report); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if report.Scout != nil {
			nightLog("scout: done status=%s candidates=%d written=%d skipped=%d self_resolved=%d escalated=%d", report.Scout.Status, report.Scout.Candidates, report.Scout.Written, report.Scout.Skipped, report.Scout.SelfResolved, report.Scout.Escalated)
		}
	}

	if stage == "" || stage == "triage" || stage == "edit" || stage == "propose" || stage == "feedback" {
		nightLog("triage/edit: ranking evidence and applying focused entity edits")
		if err := runTriageEditStages(context.Background(), vault, ldg, router, runID, string(*sphere), *dryRun, report); err != nil {
			fmt.Fprintln(os.Stderr, "triage/edit:", err)
			// Non-fatal: judge can still run.
		}
		if report.Edit != nil {
			nightLog("triage/edit: done triage=%d edited=%d skipped=%d escalated=%d", len(report.Triage), report.Edit.Edited, report.Edit.Skipped, report.Edit.Escalated)
		} else {
			nightLog("triage/edit: done triage=%d", len(report.Triage))
		}
	}

	if stage == "" || stage == "judge" {
		nightLog("judge: editorial pass over sweep, scout, triage, activity digest")
		// Load the activity sync state so the judge's git window matches
		// the same [since, until) window as the activity digest.
		actState := activity.LoadState(string(*sphere))
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
			GitSince:       actState.LastSyncUntil, // same lower bound as activity digest
			GitUntil:       report.StartedAt,       // fixed at run start, no drift
		}, report); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		nightLog("judge: done report=%s", report.JudgeReport)
	}

	report.EndedAt = time.Now().UTC()
	report.Spend = computeSpend(ldg, sessionStart, report.EndedAt)
	if err := writeNightReport(vault, runID, report); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	nightLog("done sphere=%s run=%s elapsed=%s", *sphere, runID, report.EndedAt.Sub(report.StartedAt).Round(time.Second))
	return printBrainJSON(report)
}

type nightReport struct {
	Sphere         string             `json:"sphere"`
	StartedAt      time.Time          `json:"started_at"`
	EndedAt        time.Time          `json:"ended_at,omitempty"`
	RunID          string             `json:"run_id"`
	OnlyStage      string             `json:"only_stage,omitempty"`
	DryRun         bool               `json:"dry_run"`
	ActivityDigest string             `json:"activity_digest,omitempty"`
	Sweep          *brain.SleepResult `json:"sweep,omitempty"`
	Textbook       *textbook.Summary  `json:"textbook,omitempty"`
	Scout          *scoutSummary      `json:"scout,omitempty"`
	Triage         []triage.Item      `json:"triage,omitempty"`
	Edit           *brainEdit.Report  `json:"edit,omitempty"`
	Judge          *brain.SleepResult `json:"judge,omitempty"`
	JudgeReport    string             `json:"judge_report_path,omitempty"`
	Spend          *spendSummary      `json:"spend,omitempty"`
}

// spendSummary records the plan-share spent during this nightly run, in
// units of weekly-cap fraction. Per-night cap is 0.05 (5%) per provider
// by default; numbers above that mean the gate failed somewhere.
type spendSummary struct {
	OpenAISessionShare float64 `json:"openai_session_share"`
	OpenAIWeeklyShare  float64 `json:"openai_weekly_share"`
	SessionStart       string  `json:"session_start"`
}

type scoutSummary struct {
	Status       string   `json:"status"`
	Candidates   int      `json:"candidates"`
	Written      int      `json:"written"`
	Skipped      int      `json:"skipped,omitempty"`
	SelfResolved int      `json:"self_resolved"`
	Escalated    int      `json:"escalated"`
	Reports      []string `json:"reports,omitempty"`
	Errors       []string `json:"errors,omitempty"`
	Notes        string   `json:"notes,omitempty"`
}

// runSyncStage builds the activity digest from groupware (calendar + mail)
// and brain git history for the window [last_sync_until, runStart).
// The window bounds are fixed at runStart so activity during the run is
// not counted twice. State is persisted after a successful digest so the
// next run's window starts from runStart. Errors are non-fatal.
func runSyncStage(vault brain.Vault, sphere brain.Sphere, runStart time.Time, runID string, report *nightReport) {
	state := activity.LoadState(string(sphere))
	since := state.LastSyncUntil
	until := runStart // fixed: never drifts during the run

	digest, err := activity.BuildWithGit(string(sphere), vault.BrainRoot(), since, until)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sync: activity digest: %v\n", err)
		return
	}
	report.ActivityDigest = digest.Format()

	// Persist the window so the next run starts exactly from runStart.
	_ = activity.SaveState(string(sphere), activity.State{
		LastSyncUntil: until,
		LastRunID:     runID,
	})
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

// runScoutStage picks the top stale-or-uncertain canonical entities and
// runs the scout evidence pass over each. Reports land under
// <brain>/reports/scout/<run-id>/<slug>.md. The scout never edits
// canonical Markdown; suggestions are surfaced in the report payload and
// persisted in the per-pick evidence files for the judge stage to pick up.
//
// maxItems caps how many picks are processed this run (0 = unlimited).
// A cursor file under ~/.config/sloptools/ tracks recently-processed paths
// so large vaults cycle through all entities across nightly runs instead of
// always re-processing the top-scored subset.
func runScoutStage(vault brain.Vault, ldg *ledger.Ledger, router *routing.Router, runID string, dryRun, escalateOnConflict bool, selfResolvePasses, maxItems int, sphere string, report *nightReport) error {
	allPicks, err := scout.PickEntities(scout.PickerOpts{
		BrainRoot: vault.BrainRoot(),
		Now:       time.Now().UTC(),
		TopN:      200, // large pool; cursor + maxItems cap below
	})
	if err != nil {
		return fmt.Errorf("scout pick: %w", err)
	}

	cursorPath := scoutCursorPath(sphere)
	cursor := loadScoutCursor(cursorPath)
	recentlyProcessed := make(map[string]bool, len(cursor.Processed))
	for _, e := range cursor.Processed {
		recentlyProcessed[e.Path] = true
	}

	// Filter out recently-processed paths and apply the per-run cap.
	picks := make([]scout.Pick, 0, len(allPicks))
	for _, p := range allPicks {
		if recentlyProcessed[p.Path] {
			continue
		}
		picks = append(picks, p)
	}
	if maxItems > 0 && len(picks) > maxItems {
		picks = picks[:maxItems]
	}

	report.Scout = &scoutSummary{
		Status:     "ok",
		Candidates: len(picks),
	}
	if len(picks) == 0 {
		report.Scout.Notes = "no stale entities scored (all recently processed or none available)"
		return nil
	}
	res, err := scout.Run(context.Background(), scout.RunOpts{
		BrainRoot:          vault.BrainRoot(),
		Sphere:             sphere,
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
	report.Scout.Skipped = res.Skipped
	report.Scout.SelfResolved = res.SelfResolved
	report.Scout.Escalated = res.Escalated
	for _, e := range res.Reports {
		if e.ReportPath != "" {
			report.Scout.Reports = append(report.Scout.Reports, e.ReportPath)
		}
		if e.Skipped && e.Reason != "" {
			report.Scout.Errors = append(report.Scout.Errors, e.Path+": "+e.Reason)
		}
	}
	if dryRun {
		report.Scout.Status = "dry-run"
	}

	// Persist cursor for picks that were actually processed (not skipped).
	if !dryRun {
		now := time.Now().UTC()
		for _, e := range res.Reports {
			if !e.Skipped && e.ReportPath != "" {
				cursor.Processed = append(cursor.Processed, scoutCursorEntry{
					Path:        e.Path,
					ProcessedAt: now,
				})
			}
		}
		if saveErr := saveScoutCursor(cursorPath, cursor); saveErr != nil {
			fmt.Fprintf(os.Stderr, "scout cursor save: %v\n", saveErr)
		}
	}

	return nil
}

// runJudgeStage runs the editorial pass via the Backend interface and
// wraps it in the integrity gate so the judge's canonical-Markdown
// edits are committed and pushed to the brain repo on success. Without
// this wrapping the judge writes files but leaves the working tree
// dirty, breaking the "git history is the activity log" rule from
// vault instructions.
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
// computeSpend and writeNightReport live in brain_night_report.go.
