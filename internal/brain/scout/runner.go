package scout

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/brain/backend"
	"github.com/sloppy-org/sloptools/internal/brain/evidence"
	"github.com/sloppy-org/sloptools/internal/brain/ledger"
	"github.com/sloppy-org/sloptools/internal/brain/prompts"
	"github.com/sloppy-org/sloptools/internal/brain/routing"
)

// RunOpts is the input to Run.
type RunOpts struct {
	BrainRoot string
	Sphere    string
	Picks     []Pick
	Router    *routing.Router
	Ledger    *ledger.Ledger
	RunID     string
	Now       time.Time
	DryRun    bool
	// EscalateOnConflict, when true, feeds bulk-tier scout reports
	// through a deterministic classifier. Flagged reports get a free
	// llamacpp self-resolve pass (up to SelfResolvePasses times), and
	// only when the self-resolve cannot clear the flags do they reach
	// the paid medium tier. Pattern: bulk → self-resolve (free) → paid
	// (only if still flagged).
	EscalateOnConflict bool
	// SelfResolvePasses is the number of llamacpp self-resolve passes
	// to attempt between the bulk pass and the paid escalation. Default
	// when EscalateOnConflict is true and this is zero is 1 — one
	// targeted close-the-gaps pass before paid. Capped at 3.
	SelfResolvePasses int
}

// RunReport summarises the scout pass. Candidates is the number of
// picks fed in (deterministic picker output); Written is the number of
// evidence reports actually produced (zero in dry-run, can be smaller
// than Candidates when the ledger guard or backend skips a pick).
// SelfResolved counts picks where a free llamacpp self-resolve pass
// cleared the classifier without needing paid escalation. Escalated
// counts picks that reached the paid medium tier after the
// self-resolve passes did not clear them.
type RunReport struct {
	RunID        string        `json:"run_id"`
	StartedAt    time.Time     `json:"started_at"`
	EndedAt      time.Time     `json:"ended_at"`
	Candidates   int           `json:"candidates"`
	Written      int           `json:"written"`
	SelfResolved int           `json:"self_resolved"`
	Escalated    int           `json:"escalated"`
	ReportsDir   string        `json:"reports_dir"`
	Reports      []ReportEntry `json:"reports"`
	Skipped      int           `json:"skipped"`
	Errors       []string      `json:"errors,omitempty"`
}

// ReportEntry records one entity report's outcome.
type ReportEntry struct {
	Path       string  `json:"path"`        // canonical-entity vault-relative path
	ReportPath string  `json:"report_path"` // <brain>/reports/scout/<date>/<slug>.md
	Score      float64 `json:"score"`
	Backend    string  `json:"backend,omitempty"`
	Provider   string  `json:"provider,omitempty"`
	Skipped    bool    `json:"skipped,omitempty"`
	Reason     string  `json:"reason,omitempty"`
	WallMS     int64   `json:"wall_ms,omitempty"`
	// SelfResolveCount is the number of free llamacpp self-resolve
	// passes that ran on this pick after the bulk pass triggered the
	// classifier. Zero means the bulk pass was clean (or the pick
	// short-circuited).
	SelfResolveCount int `json:"self_resolve_count,omitempty"`
	// SelfResolved is true when one of those self-resolve passes
	// cleared the classifier so paid escalation was not needed.
	SelfResolved bool `json:"self_resolved,omitempty"`
	// Escalated is true when the bulk + self-resolve passes still left
	// classifier-flagged content and a paid medium-tier reviewer
	// replaced the report content.
	Escalated         bool   `json:"escalated,omitempty"`
	EscalationReason  string `json:"escalation_reason,omitempty"`
	EscalationBackend string `json:"escalation_backend,omitempty"`
	EscalationModel   string `json:"escalation_model,omitempty"`
}

// Run executes the scout pass over the picks. Per pick:
//  1. ledger guard skips a pick whose tier is saturated;
//  2. the bulk tier (local llamacpp qwen) builds an evidence report from a
//     packet that names the entity + its locally-known anchors and
//     prompts the agent to verify against helpy MCP web/Zotero/TUGonline;
//  3. the report lands at <brain>/reports/scout/<run-id>/<slug>.md.
//
// The scout NEVER edits canonical Markdown. Suggestions go to the
// returned RunReport; the judge stage applies them later.
func Run(ctx context.Context, opts RunOpts) (*RunReport, error) {
	if opts.BrainRoot == "" {
		return nil, fmt.Errorf("scout: BrainRoot required")
	}
	if opts.Router == nil {
		return nil, fmt.Errorf("scout: Router required")
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now().UTC()
	}
	if opts.RunID == "" {
		opts.RunID = opts.Now.Format("20060102-150405")
	}
	dir := filepath.Join(opts.BrainRoot, "reports", "scout", opts.RunID)
	if !opts.DryRun {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("scout: mkdir %s: %w", dir, err)
		}
	}
	promptDir, err := os.MkdirTemp("", "sloptools-scout-prompts-")
	if err != nil {
		return nil, fmt.Errorf("scout: prompt dir: %w", err)
	}
	defer os.RemoveAll(promptDir)
	if _, err := prompts.Extract(promptDir); err != nil {
		return nil, fmt.Errorf("scout: extract prompts: %w", err)
	}
	stagePrompt := filepath.Join(promptDir, "scout.md")
	if _, err := os.Stat(stagePrompt); err != nil {
		stagePrompt = filepath.Join(promptDir, "folder-note.md")
	}

	report := &RunReport{
		RunID:      opts.RunID,
		StartedAt:  opts.Now,
		ReportsDir: dir,
		Candidates: len(opts.Picks),
	}
	// One BrokenTools instance for the whole scout run. The first scout
	// loop that hits the per-loop circuit on a tool reports it here; every
	// subsequent loop in this run starts with that tool stripped from its
	// allowlist, so we never pay the 3-retry tax on the same broken tool
	// 350 times.
	broken := backend.NewBrokenTools(1)
	for i, p := range opts.Picks {
		fmt.Fprintf(os.Stderr, "brain night: scout %d/%d start path=%s score=%.1f\n", i+1, len(opts.Picks), p.Path, p.Score)
		entry := runOnePick(ctx, opts, dir, stagePrompt, p, broken)
		if entry.Skipped {
			fmt.Fprintf(os.Stderr, "brain night: scout %d/%d skip path=%s reason=%s\n", i+1, len(opts.Picks), p.Path, entry.Reason)
		} else {
			fmt.Fprintf(os.Stderr, "brain night: scout %d/%d done path=%s backend=%s report=%s wall_ms=%d\n", i+1, len(opts.Picks), p.Path, entry.Backend, entry.ReportPath, entry.WallMS)
		}
		if entry.Skipped {
			report.Skipped++
		}
		if entry.ReportPath != "" {
			report.Written++
		}
		if entry.SelfResolved {
			report.SelfResolved++
		}
		if entry.Escalated {
			report.Escalated++
		}
		report.Reports = append(report.Reports, entry)
	}
	report.EndedAt = time.Now().UTC()
	return report, nil
}

// sparseBodyThreshold is the minimum note body length (after stripping
// frontmatter) required to spawn a scout agent. Notes below this threshold
// have no web-searchable anchors and consistently time out without output.
const sparseBodyThreshold = 200

func runOnePick(ctx context.Context, opts RunOpts, reportsDir, stagePrompt string, p Pick, broken *backend.BrokenTools) ReportEntry {
	startedAt := time.Now().UTC()
	entry := ReportEntry{Path: p.Path, Score: p.Score}

	// Pre-filter: skip notes whose body is too sparse to scout usefully.
	if !opts.DryRun && opts.BrainRoot != "" {
		body, _ := os.ReadFile(filepath.Join(opts.BrainRoot, p.Path))
		stripped := stripFrontmatter(string(body))
		if len(strings.TrimSpace(stripped)) < sparseBodyThreshold {
			entry.Skipped = true
			entry.Reason = "note too sparse for scout"
			_ = evidence.Append(opts.BrainRoot, []evidence.Entry{{
				TS:      startedAt,
				RunID:   opts.RunID,
				Entity:  p.Path,
				Claim:   "note body < 200 chars after stripping frontmatter",
				Verdict: evidence.VerdictSkipped,
			}})
			return entry
		}
	}

	pick, err := opts.Router.Pick(routing.StageScout)
	if err != nil {
		entry.Skipped = true
		entry.Reason = err.Error()
		return entry
	}
	entry.Backend = pick.BackendID
	entry.Provider = string(pick.Provider)
	if opts.DryRun {
		entry.Skipped = true
		entry.Reason = "dry-run"
		return entry
	}
	be := backendForPick(pick)
	stage := "scout-" + sanitize(p.Path)
	sb, err := backend.NewSandbox(opts.RunID, stage, stagePrompt, backend.DefaultMCPConfig())
	if err != nil {
		entry.Skipped = true
		entry.Reason = err.Error()
		return entry
	}
	defer sb.Cleanup()
	packet := buildScoutPacket(opts.BrainRoot, p)
	req := backend.Request{
		Stage:            stage,
		Packet:           packet,
		SystemPromptPath: sb.SystemPromptIn,
		Model:            pick.Model,
		Reasoning:        pick.Reasoning,
		AllowEdits:       false,
		MCPAllowList:     pick.MCPTools,
		MCPToolQuotas:    pick.MCPQuotas,
		MCPBrokenTools:   broken,
		Affinity:         backend.AffinityForPick(opts.RunID, p.Path, "scout"),
		Sandbox:          sb,
	}
	bulkStartedAt := time.Now().UTC()
	resp, err := be.Run(ctx, req)
	if err != nil {
		entry.Skipped = true
		entry.Reason = err.Error()
		return entry
	}
	entry.WallMS = resp.WallMS
	body := cleanReport(resp.Output)
	if body == "" {
		entry.Skipped = true
		entry.Reason = "empty backend output"
		return entry
	}
	rpath := filepath.Join(reportsDir, sanitize(p.Path)+".md")
	if err := os.WriteFile(rpath, []byte(body+"\n"), 0o644); err != nil {
		entry.Skipped = true
		entry.Reason = "write report: " + err.Error()
		return entry
	}
	entry.ReportPath = rpath
	bulkRawPath, bulkCleanedPath, _ := writeStageArtifact(rpath, "bulk", resp.Output, body)
	bulkRec := stageRecord{
		Stage:        stage,
		Backend:      pick.BackendID,
		Provider:     string(pick.Provider),
		Model:        pick.Model,
		Tier:         string(pick.Tier),
		StartedAt:    bulkStartedAt,
		WallMS:       resp.WallMS,
		TokensIn:     resp.TokensIn,
		TokensOut:    resp.TokensOut,
		CostHint:     resp.CostHint,
		RawPath:      bulkRawPath,
		CleanedPath:  bulkCleanedPath,
		RawBytes:     len(resp.Output),
		CleanedBytes: len(body),
	}
	stages := []stageRecord{bulkRec}
	if opts.Ledger != nil {
		_ = opts.Ledger.Append(ledger.Entry{
			Sphere:    opts.Sphere,
			Stage:     stage,
			Provider:  pick.Provider,
			Backend:   pick.BackendID,
			Model:     pick.Model,
			TokensIn:  resp.TokensIn,
			TokensOut: resp.TokensOut,
			WallMS:    resp.WallMS,
			CostHint:  resp.CostHint,
			Extras:    map[string]string{"path": p.Path, "tier": string(pick.Tier)},
		})
	}
	finalStage := stage
	if opts.EscalateOnConflict {
		d := classifyForEscalation(body)
		stages[len(stages)-1].ReasonAfter = d.Reason
		if d.Escalate {
			passes := opts.SelfResolvePasses
			if passes <= 0 {
				passes = 1
			}
			if passes > 3 {
				passes = 3
			}
			resolvePick, resolvePickErr := opts.Router.PickValueLocal(routing.StageScout)
			for i := 0; i < passes && d.Escalate; i++ {
				if resolvePickErr != nil {
					break
				}
				resolveTrigger := d.Reason
				newBody, rec, err := selfResolveOne(ctx, opts, p, packet, body, d.Reason, rpath, i+1, resolvePick)
				if err != nil {
					// Self-resolve failed: leave bulk body in place and fall
					// through to paid escalation below.
					break
				}
				body = newBody
				entry.SelfResolveCount++
				d = classifyForEscalation(body)
				rec.TriggerReason = resolveTrigger
				rec.ReasonAfter = d.Reason
				stages = append(stages, rec)
				finalStage = rec.Stage
			}
			if !d.Escalate {
				entry.SelfResolved = true
			} else {
				escalateTrigger := d.Reason
				rec, err := escalateOne(ctx, opts, &entry, p, packet, body, d.Reason, rpath)
				if err != nil {
					entry.EscalationReason = "attempted: " + d.Reason + "; failed: " + err.Error()
				} else {
					rec.TriggerReason = escalateTrigger
					stages = append(stages, rec)
					finalStage = rec.Stage
				}
			}
		}
	}
	// Write evidence log entries from the final (possibly escalated) report.
	if entries := evidence.ParseBullets(opts.RunID, p.Path, body, startedAt); len(entries) > 0 {
		_ = evidence.Append(opts.BrainRoot, entries)
	}

	_ = writeAuditFile(rpath, auditFile{
		Path:         p.Path,
		Title:        p.Title,
		ReportPath:   rpath,
		RunID:        opts.RunID,
		Sphere:       opts.Sphere,
		StartedAt:    startedAt,
		EndedAt:      time.Now().UTC(),
		FinalStage:   finalStage,
		SelfResolved: entry.SelfResolved,
		Escalated:    entry.Escalated,
		Stages:       stages,
	})
	return entry
}

// stripFrontmatter removes the YAML frontmatter block (--- ... ---) from a
// Markdown note body. Used by the sparse-pick pre-filter.
func stripFrontmatter(body string) string {
	body = strings.TrimSpace(body)
	if !strings.HasPrefix(body, "---") {
		return body
	}
	rest := body[3:]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return body
	}
	return strings.TrimSpace(rest[idx+4:])
}

// selfResolveOne, buildScoutPacket, sanitize, and backendForID live in
// resolve.go and packet.go to keep this file under the per-file line
// budget.
