package brain

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// SleepBackendCodex routes the dream review through Codex CLI.
const SleepBackendCodex = "codex"

// SleepBackendNone skips the LLM step entirely; the rendered Markdown
// from the dream report is written verbatim.
const SleepBackendNone = "none"

// SleepDefaultModel is the Codex model used when --model is not given.
// Matches the brain-ingest phase 4 convention; see
// `~/Nextcloud/tools/brain-ingest/data/phase4/job_logs/phase4-rel-0004.log`.
const SleepDefaultModel = "gpt-5.5"

// SleepDefaultBudget is the picker budget used when budget <= 0.
const SleepDefaultBudget = 20

// SleepReportSubdir is the directory below brain root where the daily
// sleep report is persisted.
const SleepReportSubdir = "reports/sleep"

// CodexExecFn shells out to the Codex CLI. Tests inject a fake.
type CodexExecFn func(ctx context.Context, model, vaultRoot, packet string) ([]byte, error)

// SleepOpts is the input to RunSleep.
type SleepOpts struct {
	Sphere    Sphere
	Budget    int
	Backend   string
	Model     string
	DryRun    bool
	Now       time.Time
	CodexExec CodexExecFn
}

// SleepResult is the orchestrator's structured outcome.
type SleepResult struct {
	Sphere       Sphere       `json:"sphere"`
	Date         string       `json:"date"`
	Backend      string       `json:"backend"`
	Model        string       `json:"model,omitempty"`
	DryRun       bool         `json:"dry_run"`
	PruneDigest  string       `json:"prune_digest"`
	PruneCount   int          `json:"prune_count"`
	PruneApplied bool         `json:"prune_applied"`
	Report       *DreamReport `json:"report"`
	ReportPath   string       `json:"report_path,omitempty"`
	CodexUsed    bool         `json:"codex_used"`
}

// RunSleep orchestrates the brain sleep cycle:
//  1. dream prune-links scan + plan,
//  2. dream report,
//  3. render the Markdown packet,
//  4. optional Codex pass over the packet,
//  5. write the daily report under <brain>/reports/sleep/<date>.md,
//  6. apply prune-link rewrites unless dry-run.
func RunSleep(cfg *Config, opts SleepOpts) (*SleepResult, error) {
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	if opts.Budget <= 0 {
		opts.Budget = SleepDefaultBudget
	}
	backend := strings.TrimSpace(strings.ToLower(opts.Backend))
	if backend == "" {
		backend = SleepBackendCodex
	}
	if backend != SleepBackendCodex && backend != SleepBackendNone {
		return nil, fmt.Errorf("unknown sleep backend: %s", opts.Backend)
	}
	model := strings.TrimSpace(opts.Model)
	if backend == SleepBackendCodex && model == "" {
		model = SleepDefaultModel
	}

	vault, err := cfgVault(cfg, opts.Sphere)
	if err != nil {
		return nil, err
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

	packet := renderSleepPacket(report, plan, cold, vault.Sphere, opts.Now)
	finalMarkdown := packet
	codexUsed := false
	if backend == SleepBackendCodex && !opts.DryRun {
		execFn := opts.CodexExec
		if execFn == nil {
			execFn = defaultCodexExec
		}
		ctx := context.Background()
		out, err := execFn(ctx, model, vault.Root, packet)
		if err != nil {
			return nil, fmt.Errorf("codex exec: %w", err)
		}
		trimmed := strings.TrimRight(string(out), "\n")
		if trimmed == "" {
			return nil, fmt.Errorf("codex returned empty output")
		}
		finalMarkdown = trimmed + "\n"
		codexUsed = true
	}

	reportPath := filepath.Join(vault.BrainRoot(), SleepReportSubdir, opts.Now.Format("2006-01-02")+".md")
	if !opts.DryRun {
		if err := os.MkdirAll(filepath.Dir(reportPath), 0o755); err != nil {
			return nil, fmt.Errorf("mkdir report dir: %w", err)
		}
		if err := os.WriteFile(reportPath, []byte(finalMarkdown), 0o644); err != nil {
			return nil, fmt.Errorf("write report: %w", err)
		}
	} else {
		reportPath = ""
	}

	pruneApplied := false
	if !opts.DryRun && len(cold) > 0 {
		if _, err := DreamPruneLinksApply(cfg, opts.Sphere, plan.Digest); err != nil {
			return nil, fmt.Errorf("prune apply: %w", err)
		}
		pruneApplied = true
	}

	res := &SleepResult{
		Sphere:       opts.Sphere,
		Date:         opts.Now.Format("2006-01-02"),
		Backend:      backend,
		DryRun:       opts.DryRun,
		PruneDigest:  plan.Digest,
		PruneCount:   len(cold),
		PruneApplied: pruneApplied,
		Report:       report,
		ReportPath:   reportPath,
		CodexUsed:    codexUsed,
	}
	if backend == SleepBackendCodex {
		res.Model = model
	}
	return res, nil
}

// renderSleepPacket builds the deterministic Markdown packet that we hand
// to Codex (or write verbatim when backend=none).
func renderSleepPacket(report *DreamReport, plan *MovePlan, cold []ColdLink, sphere Sphere, now time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Brain sleep report — %s — %s\n\n", sphere, now.Format("2006-01-02"))
	fmt.Fprintf(&b, "Generated: %s\n\n", now.UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "## Picked topics (%d)\n\n", len(report.Topics))
	if len(report.Topics) == 0 {
		fmt.Fprintln(&b, "_(none)_")
		fmt.Fprintln(&b)
	} else {
		for _, t := range report.Topics {
			fmt.Fprintf(&b, "- %s\n", t)
		}
		fmt.Fprintln(&b)
	}
	fmt.Fprintln(&b, "## Cold-link prune scan")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "- count: %d\n", len(cold))
	fmt.Fprintf(&b, "- digest: %s\n\n", plan.Digest)
	if len(cold) > 0 {
		fmt.Fprintln(&b, "| Source | Target | Last touch (days) |")
		fmt.Fprintln(&b, "|--------|--------|-------------------|")
		for _, c := range cold {
			fmt.Fprintf(&b, "| %s | %s | %d |\n", c.Source, c.Target, c.LastTouchDays)
		}
		fmt.Fprintln(&b)
	}
	fmt.Fprintf(&b, "## Cross-link suggestions (%d)\n\n", len(report.CrossLinks))
	for _, s := range report.CrossLinks {
		fmt.Fprintf(&b, "- %s -> %s — %s\n", s.From, s.To, s.Reason)
	}
	if len(report.CrossLinks) == 0 {
		fmt.Fprintln(&b, "_(none)_")
	}
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "## Cold targets reached from picked notes (%d)\n\n", len(report.Cold))
	for _, c := range report.Cold {
		fmt.Fprintf(&b, "- %s -> %s (%d days)\n", c.Source, c.Target, c.LastTouchDays)
	}
	if len(report.Cold) == 0 {
		fmt.Fprintln(&b, "_(none)_")
	}
	return b.String()
}

// defaultCodexExec runs `codex exec --model <model> -C <vault-root>` with
// the packet on stdin and the LLM-rewritten Markdown on stdout.
func defaultCodexExec(ctx context.Context, model, vaultRoot, packet string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "codex", "exec", "--model", model, "-C", vaultRoot, "-")
	cmd.Stdin = strings.NewReader(packet)
	cmd.Stderr = os.Stderr
	return cmd.Output()
}
