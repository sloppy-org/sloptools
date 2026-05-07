package brain

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/brain/backend"
	"github.com/sloppy-org/sloptools/internal/brain/ledger"
	"github.com/sloppy-org/sloptools/internal/brain/prompts"
	"github.com/sloppy-org/sloptools/internal/brain/routing"
)

// SleepBackendCodex routes the dream review through Codex CLI.
const SleepBackendCodex = "codex"

// SleepBackendNone skips the LLM step entirely; the rendered Markdown
// sleep packet is written verbatim.
const SleepBackendNone = "none"

// SleepAutonomyFull lets Codex edit the brain directly. Git history is the
// rollback layer.
const SleepAutonomyFull = "full"

// SleepAutonomyPlanOnly keeps Codex in read-only mode and writes its report.
const SleepAutonomyPlanOnly = "plan-only"

// SleepDefaultModel is the Codex model used when --model is not given.
// The sleep judge edits a small rendered Markdown packet; this is an
// editorial pass, not canonical Markdown authorship, so the cheaper
// gpt-5.4-mini is sufficient. Canonical entity-note writes (graph apply
// in #122) keep gpt-5.5 separately. Claude is never a default; the
// opt-in escalation flag lives in #123.
const SleepDefaultModel = "gpt-5.4-mini"

// SleepDefaultBudget is the picker budget used when budget <= 0.
const SleepDefaultBudget = 20

// SleepDefaultNREMBudget is the maximum consolidation rows included in the
// NREM replay packet when the caller does not specify a budget.
const SleepDefaultNREMBudget = 60

// SleepDefaultCoverageBudget bounds the folder coverage prepass in each
// sleep cycle.
const SleepDefaultCoverageBudget = 40

// SleepDefaultAutonomy is intentionally autonomous. The brain repo's git
// history is the review and rollback layer.
const SleepDefaultAutonomy = SleepAutonomyFull

// SleepReportSubdir is the directory below brain root where the daily
// sleep report is persisted.
const SleepReportSubdir = "reports/sleep"

// SleepCodexRequest describes the Codex execution we want. Tests inject a fake.
type SleepCodexRequest struct {
	Model     string
	VaultRoot string // brain git root used as Codex working directory
	Packet    string
	Autonomy  string
}

// CodexExecFn shells out to the Codex CLI. Tests inject a fake.
type CodexExecFn func(ctx context.Context, req SleepCodexRequest) ([]byte, error)

// SleepOpts is the input to RunSleep.
//
// Router and Ledger are optional. When Router is non-nil, the sleep
// judge call is routed through the Backend interface (claude/codex/
// opencode CLIs in scratch sandboxes) instead of shelling out to codex
// directly. Backend, Model, and Autonomy still apply, but Backend acts
// as a hint for the legacy code path; the Router decides the concrete
// model + reasoning. Ledger is appended to per call when set.
type SleepOpts struct {
	Sphere         Sphere
	Budget         int
	NREMBudget     int
	CoverageBudget int
	Backend        string
	Model          string
	Autonomy       string
	DryRun         bool
	Now            time.Time
	CodexExec      CodexExecFn
	Router         *routing.Router
	Ledger         *ledger.Ledger
	RunID          string
}

// SleepResult is the orchestrator's structured outcome.
type SleepResult struct {
	Sphere           Sphere                `json:"sphere"`
	Date             string                `json:"date"`
	Backend          string                `json:"backend"`
	Model            string                `json:"model,omitempty"`
	Autonomy         string                `json:"autonomy"`
	DryRun           bool                  `json:"dry_run"`
	PruneDigest      string                `json:"prune_digest"`
	PruneCount       int                   `json:"prune_count"`
	PruneApplied     bool                  `json:"prune_applied"`
	PruneEditedPaths []string              `json:"prune_edited_paths,omitempty"`
	NREMCount        int                   `json:"nrem_count"`
	RecentCount      int                   `json:"recent_count"`
	Coverage         FolderCoverageSummary `json:"coverage"`
	Report           *DreamReport          `json:"report"`
	ReportPath       string                `json:"report_path,omitempty"`
	CodexUsed        bool                  `json:"codex_used"`
	GitContextUsed   bool                  `json:"git_context_used"`
	GitContextScope  string                `json:"git_context_scope,omitempty"`
}

type preparedSleepCycle struct {
	vault    Vault
	cold     []ColdLink
	plan     *MovePlan
	report   *DreamReport
	nrem     []ConsolidateRow
	recent   []string
	coverage FolderCoverageSummary
	packet   string
}

type sleepPruneOutcome struct {
	applied     bool
	editedPaths []string
}

// RunSleep orchestrates the brain sleep cycle:
//  1. gather recent git memory,
//  2. build NREM consolidation candidates,
//  3. build REM dream candidates,
//  4. apply mechanical cold-link pruning,
//  5. let Codex autonomously rewire the vault unless dry-run/backend=none,
//  6. write a sleep report under <brain>/reports/sleep/<date>.md.
func RunSleep(cfg *Config, opts SleepOpts) (*SleepResult, error) {
	backend, model, autonomy, err := normalizeSleepOpts(&opts)
	if err != nil {
		return nil, err
	}
	prep, err := prepareSleepCycle(cfg, opts, autonomy)
	if err != nil {
		return nil, err
	}
	prune, err := applySleepPrune(cfg, opts, prep)
	if err != nil {
		return nil, err
	}
	finalMarkdown, codexUsed, err := runSleepCodex(opts, backend, model, autonomy, prep)
	if err != nil {
		return nil, err
	}
	reportPath, err := writeSleepReport(prep.vault, opts.Now, opts.DryRun, finalMarkdown)
	if err != nil {
		return nil, err
	}
	return newSleepResult(opts, backend, model, autonomy, prep, prune, reportPath, codexUsed), nil
}

func normalizeSleepOpts(opts *SleepOpts) (string, string, string, error) {
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	if opts.Budget <= 0 {
		opts.Budget = SleepDefaultBudget
	}
	if opts.NREMBudget <= 0 {
		opts.NREMBudget = SleepDefaultNREMBudget
	}
	if opts.CoverageBudget <= 0 {
		opts.CoverageBudget = SleepDefaultCoverageBudget
	}
	backend := strings.TrimSpace(strings.ToLower(opts.Backend))
	if backend == "" {
		backend = SleepBackendCodex
	}
	if backend != SleepBackendCodex && backend != SleepBackendNone {
		return "", "", "", fmt.Errorf("unknown sleep backend: %s", opts.Backend)
	}
	autonomy := strings.TrimSpace(strings.ToLower(opts.Autonomy))
	if autonomy == "" {
		autonomy = SleepDefaultAutonomy
	}
	if autonomy != SleepAutonomyFull && autonomy != SleepAutonomyPlanOnly {
		return "", "", "", fmt.Errorf("unknown sleep autonomy: %s", opts.Autonomy)
	}
	model := strings.TrimSpace(opts.Model)
	if backend == SleepBackendCodex && !opts.DryRun {
		if model == "" {
			model = SleepDefaultModel
		}
	} else if backend == SleepBackendCodex && model == "" {
		model = SleepDefaultModel
	}
	return backend, model, autonomy, nil
}

func prepareSleepCycle(cfg *Config, opts SleepOpts, autonomy string) (*preparedSleepCycle, error) {
	vault, err := cfgVault(cfg, opts.Sphere)
	if err != nil {
		return nil, err
	}
	coverage, err := SyncFolderCoverage(cfg, opts.Sphere, FolderCoverageOpts{
		Limit: opts.CoverageBudget, DryRun: opts.DryRun, Now: opts.Now,
	})
	if err != nil {
		return nil, fmt.Errorf("folder coverage: %w", err)
	}
	cold, err := DreamPruneLinksScan(cfg, opts.Sphere)
	if err != nil {
		return nil, fmt.Errorf("prune scan: %w", err)
	}
	plan, err := BuildDreamPrunePlan(cfg, opts.Sphere, cold)
	if err != nil {
		return nil, fmt.Errorf("prune plan: %w", err)
	}
	report, err := DreamReportRun(cfg, opts.Sphere, opts.Budget)
	if err != nil {
		return nil, fmt.Errorf("dream report: %w", err)
	}
	attachDreamGitContext(report, vault, opts.Now)
	nrem, err := ConsolidatePlan(cfg, opts.Sphere)
	if err != nil {
		return nil, fmt.Errorf("nrem consolidate plan: %w", err)
	}
	recent := recentBrainMemory(vault, opts.Now)
	recent = mergeRecentPaths(recent, coverageNotePaths(coverage))
	nrem = prioritizeSleepNREM(nrem, recent, opts.NREMBudget)
	packet := renderSleepPacket(SleepPacket{
		Report:      report,
		PrunePlan:   plan,
		Cold:        cold,
		NREM:        nrem,
		RecentPaths: recent,
		Coverage:    coverage,
		Sphere:      vault.Sphere,
		Autonomy:    autonomy,
		Now:         opts.Now,
		GitPacket:   report.GitContext,
	})
	return &preparedSleepCycle{vault, cold, plan, report, nrem, recent, coverage, packet}, nil
}

func applySleepPrune(cfg *Config, opts SleepOpts, prep *preparedSleepCycle) (sleepPruneOutcome, error) {
	if opts.DryRun || len(prep.cold) == 0 {
		return sleepPruneOutcome{}, nil
	}
	summary, err := DreamPruneLinksApply(cfg, opts.Sphere, prep.plan.Digest)
	if err != nil {
		return sleepPruneOutcome{}, fmt.Errorf("prune apply: %w", err)
	}
	return sleepPruneOutcome{applied: true, editedPaths: summary.EditedPaths}, nil
}

func runSleepCodex(opts SleepOpts, backendName, model, autonomy string, prep *preparedSleepCycle) (string, bool, error) {
	if backendName != SleepBackendCodex || opts.DryRun {
		return prep.packet, false, nil
	}
	if opts.Router != nil {
		return runSleepWithRouter(opts, autonomy, prep)
	}
	execFn := opts.CodexExec
	if execFn == nil {
		execFn = defaultCodexExec
	}
	out, err := execFn(context.Background(), SleepCodexRequest{
		Model:     model,
		VaultRoot: prep.vault.BrainRoot(),
		Packet:    prep.packet,
		Autonomy:  autonomy,
	})
	if err != nil {
		return "", false, fmt.Errorf("codex exec: %w", err)
	}
	trimmed := strings.TrimRight(string(out), "\n")
	if trimmed == "" {
		return "", false, fmt.Errorf("codex returned empty output")
	}
	return trimmed + "\n", true, nil
}

// runSleepWithRouter routes the sleep-judge editorial pass through the
// Backend interface. The router picks one of {claude, codex, opencode}
// per the configured tier; the resulting CLI runs under a per-call
// scratch sandbox with HOME / CODEX_HOME / XDG_CONFIG_HOME isolated and
// MCP servers (sloppy + helpy, never slopshell) regenerated locally.
//
// The vault root is passed as WorkDir so the model can edit canonical
// Markdown directly under workspace-write sandbox mode (autonomy=full)
// or read-only (autonomy=plan-only).
func runSleepWithRouter(opts SleepOpts, autonomy string, prep *preparedSleepCycle) (string, bool, error) {
	pick, err := opts.Router.Pick(routing.StageSleepJudge)
	if err != nil {
		return "", false, fmt.Errorf("sleep route: %w", err)
	}
	be, err := backendForPick(pick.BackendID)
	if err != nil {
		return "", false, err
	}
	runID := opts.RunID
	if runID == "" {
		runID = opts.Now.UTC().Format("20060102-150405")
	}
	stage := "sleep-judge-" + opts.Now.UTC().Format("150405")
	promptDir, err := os.MkdirTemp("", "sloptools-sleep-prompts-")
	if err != nil {
		return "", false, fmt.Errorf("sleep route: prompt dir: %w", err)
	}
	defer os.RemoveAll(promptDir)
	if _, err := prompts.Extract(promptDir); err != nil {
		return "", false, fmt.Errorf("sleep route: extract prompts: %w", err)
	}
	stagePrompt := filepath.Join(promptDir, "sleep-judge.md")
	if _, err := os.Stat(stagePrompt); err != nil {
		stagePrompt = filepath.Join(promptDir, "folder-note.md")
	}
	sb, err := backend.NewSandbox(runID, stage, stagePrompt, backend.DefaultMCPConfig())
	if err != nil {
		return "", false, fmt.Errorf("sleep route: sandbox: %w", err)
	}
	defer sb.Cleanup()
	req := backend.Request{
		Stage:            stage,
		Packet:           prep.packet,
		SystemPromptPath: sb.SystemPromptIn,
		Model:            pick.Model,
		Reasoning:        pick.Reasoning,
		AllowEdits:       autonomy == SleepAutonomyFull,
		Sandbox:          sb,
		WorkDir:          prep.vault.BrainRoot(),
	}
	resp, err := be.Run(context.Background(), req)
	if err != nil {
		return "", false, fmt.Errorf("sleep route: backend run: %w", err)
	}
	if opts.Ledger != nil {
		_ = opts.Ledger.Append(ledger.Entry{
			Sphere:    string(opts.Sphere),
			Stage:     stage,
			Provider:  pick.Provider,
			Backend:   pick.BackendID,
			Model:     pick.Model,
			TokensIn:  resp.TokensIn,
			TokensOut: resp.TokensOut,
			WallMS:    resp.WallMS,
			CostHint:  resp.CostHint,
			Extras:    map[string]string{"tier": string(pick.Tier)},
		})
	}
	body := strings.TrimRight(resp.Output, "\n")
	if body == "" {
		return "", false, fmt.Errorf("sleep route: empty output")
	}
	return body + "\n", true, nil
}

func backendForPick(id string) (backend.Backend, error) {
	switch id {
	case "claude":
		return backend.ClaudeBackend{}, nil
	case "codex":
		return backend.CodexBackend{}, nil
	case "opencode":
		return backend.OpencodeBackend{}, nil
	}
	return nil, fmt.Errorf("sleep route: unknown backend id %q", id)
}

func writeSleepReport(vault Vault, now time.Time, dryRun bool, markdown string) (string, error) {
	if dryRun {
		return "", nil
	}
	reportPath := sleepReportPath(vault, now)
	if err := os.MkdirAll(filepath.Dir(reportPath), 0o755); err != nil {
		return "", fmt.Errorf("mkdir report dir: %w", err)
	}
	if err := os.WriteFile(reportPath, []byte(markdown), 0o644); err != nil {
		return "", fmt.Errorf("write report: %w", err)
	}
	return reportPath, nil
}

func newSleepResult(opts SleepOpts, backend, model, autonomy string, prep *preparedSleepCycle, prune sleepPruneOutcome, reportPath string, codexUsed bool) *SleepResult {
	res := &SleepResult{
		Sphere:           opts.Sphere,
		Date:             opts.Now.Format("2006-01-02"),
		Backend:          backend,
		Autonomy:         autonomy,
		DryRun:           opts.DryRun,
		PruneDigest:      prep.plan.Digest,
		PruneCount:       len(prep.cold),
		PruneApplied:     prune.applied,
		PruneEditedPaths: prune.editedPaths,
		NREMCount:        len(prep.nrem),
		RecentCount:      len(prep.recent),
		Coverage:         prep.coverage,
		Report:           prep.report,
		ReportPath:       reportPath,
		CodexUsed:        codexUsed,
		GitContextUsed:   prep.report.GitContextUsed,
		GitContextScope:  prep.report.GitContextScope,
	}
	if backend == SleepBackendCodex {
		res.Model = model
	}
	return res
}

func sleepReportPath(vault Vault, now time.Time) string {
	dir := filepath.Join(vault.BrainRoot(), SleepReportSubdir)
	datePath := filepath.Join(dir, now.Format("2006-01-02")+".md")
	if _, err := os.Stat(datePath); os.IsNotExist(err) {
		return datePath
	}
	stamp := now.Format("2006-01-02-150405")
	path := filepath.Join(dir, stamp+".md")
	for i := 2; ; i++ {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return path
		}
		path = filepath.Join(dir, fmt.Sprintf("%s-%02d.md", stamp, i))
	}
}

// defaultCodexExec runs `codex exec --model <model> -C <vault-root>` with
// the packet on stdin and reads only the final assistant message back via
// `--output-last-message <tempfile>` (the default codex stdout mixes the
// session metadata, replayed user/assistant turns, and a token-count
// footer, none of which we want in the persisted sleep report).
//
// `--skip-git-repo-check` lets the call succeed even when the working
// directory is not on codex's trusted-dir list (Nextcloud-synced vaults
// usually are not).
//
// `--ask-for-approval never` keeps the sleep run non-interactive. Git history
// is the rollback layer.
func defaultCodexExec(ctx context.Context, req SleepCodexRequest) ([]byte, error) {
	tmp, err := os.CreateTemp("", "sloptools-sleep-codex-*.md")
	if err != nil {
		return nil, fmt.Errorf("temp file: %w", err)
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	defer os.Remove(tmpPath)

	cmd := exec.CommandContext(ctx, "codex", codexExecArgs(req, tmpPath)...)
	cmd.Stdin = strings.NewReader(req.Packet)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("codex exec run: %w", err)
	}
	body, err := os.ReadFile(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("codex output: %w", err)
	}
	return body, nil
}

func codexExecArgs(req SleepCodexRequest, outputPath string) []string {
	return []string{
		"--ask-for-approval", "never",
		"exec",
		"--skip-git-repo-check",
		"--sandbox", codexSleepSandbox(req.Autonomy),
		"--model", req.Model,
		"-C", req.VaultRoot,
		"--output-last-message", outputPath,
		"-",
	}
}

func codexSleepSandbox(autonomy string) string {
	if autonomy == SleepAutonomyPlanOnly {
		return "read-only"
	}
	return "workspace-write"
}
