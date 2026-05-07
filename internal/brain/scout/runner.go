package scout

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/brain/backend"
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
	// opencode self-resolve pass (up to SelfResolvePasses times), and
	// only when the self-resolve cannot clear the flags do they reach
	// the paid medium tier. Pattern: bulk → self-resolve (free) → paid
	// (only if still flagged).
	EscalateOnConflict bool
	// SelfResolvePasses is the number of opencode self-resolve passes
	// to attempt between the bulk pass and the paid escalation. Default
	// when EscalateOnConflict is true and this is zero is 1 — one
	// targeted close-the-gaps pass before paid. Capped at 3.
	SelfResolvePasses int
}

// RunReport summarises the scout pass. Candidates is the number of
// picks fed in (deterministic picker output); Written is the number of
// evidence reports actually produced (zero in dry-run, can be smaller
// than Candidates when the ledger guard or backend skips a pick).
// SelfResolved counts picks where a free opencode self-resolve pass
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
	// SelfResolveCount is the number of free opencode self-resolve
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
//  2. the bulk tier (opencode/qwen) builds an evidence report from a
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
	for _, p := range opts.Picks {
		entry := runOnePick(ctx, opts, dir, stagePrompt, p)
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

func runOnePick(ctx context.Context, opts RunOpts, reportsDir, stagePrompt string, p Pick) ReportEntry {
	entry := ReportEntry{Path: p.Path, Score: p.Score}
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
	be, err := backendForID(pick.BackendID)
	if err != nil {
		entry.Skipped = true
		entry.Reason = err.Error()
		return entry
	}
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
		Sandbox:          sb,
	}
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
	if opts.EscalateOnConflict {
		d := classifyForEscalation(body)
		if d.Escalate {
			passes := opts.SelfResolvePasses
			if passes <= 0 {
				passes = 1
			}
			if passes > 3 {
				passes = 3
			}
			for i := 0; i < passes && d.Escalate; i++ {
				newBody, err := selfResolveOne(ctx, opts, p, packet, body, d.Reason, rpath)
				if err != nil {
					// Self-resolve failed: leave bulk body in place and fall
					// through to paid escalation below.
					break
				}
				body = newBody
				entry.SelfResolveCount++
				d = classifyForEscalation(body)
			}
			if !d.Escalate {
				entry.SelfResolved = true
			} else {
				if err := escalateOne(ctx, opts, &entry, p, packet, body, d.Reason, rpath); err != nil {
					entry.EscalationReason = "attempted: " + d.Reason + "; failed: " + err.Error()
				}
			}
		}
	}
	return entry
}

// selfResolveOne runs a free opencode self-resolve pass over a bulk-
// tier report that the classifier flagged. The agent reads its own
// prior draft plus the original packet and produces a refined report
// that either resolves the flagged items with citations or marks them
// `- needs paid review:` for the next pass. The new body overwrites
// the report file; ledger gets a second entry tagged with the
// self-resolve stage. Returns the new body so the caller can re-
// classify it. Non-fatal errors are returned so the caller can decide
// whether to fall through to paid escalation.
func selfResolveOne(ctx context.Context, opts RunOpts, p Pick, originalPacket, bulkReport, reason, reportPath string) (string, error) {
	pick, err := opts.Router.Pick(routing.StageScout)
	if err != nil {
		return "", fmt.Errorf("router pick scout: %w", err)
	}
	be, err := backendForID(pick.BackendID)
	if err != nil {
		return "", fmt.Errorf("backendForID: %w", err)
	}
	stagePrompt, err := writeSelfResolvePrompt()
	if err != nil {
		return "", fmt.Errorf("write self-resolve prompt: %w", err)
	}
	defer os.Remove(stagePrompt)
	stage := "scout-resolve-" + sanitize(p.Path)
	sb, err := backend.NewSandbox(opts.RunID, stage, stagePrompt, backend.DefaultMCPConfig())
	if err != nil {
		return "", fmt.Errorf("sandbox: %w", err)
	}
	defer sb.Cleanup()
	packet := buildSelfResolvePacket(p, originalPacket, bulkReport, reason)
	resp, err := be.Run(ctx, backend.Request{
		Stage:            stage,
		Packet:           packet,
		SystemPromptPath: sb.SystemPromptIn,
		Model:            pick.Model,
		Reasoning:        pick.Reasoning,
		AllowEdits:       false,
		Sandbox:          sb,
	})
	if err != nil {
		return "", fmt.Errorf("backend run: %w", err)
	}
	body := cleanReport(resp.Output)
	if body == "" {
		return "", fmt.Errorf("empty self-resolve output")
	}
	if err := os.WriteFile(reportPath, []byte(body+"\n"), 0o644); err != nil {
		return "", fmt.Errorf("write self-resolved report: %w", err)
	}
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
			Extras:    map[string]string{"path": p.Path, "tier": string(pick.Tier), "self_resolve": "true"},
		})
	}
	return body, nil
}

// writeSelfResolvePrompt drops the close-the-gaps prompt to disk for
// the self-resolve call. The prompt is in internal/brain/prompts/
// scout-resolve.md and is extracted into a temp file so the sandbox
// can copy it (the sandbox needs a real file path).
func writeSelfResolvePrompt() (string, error) {
	dir, err := os.MkdirTemp("", "sloptools-scout-resolve-prompt-")
	if err != nil {
		return "", err
	}
	if _, err := prompts.Extract(dir); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "scout-resolve.md")
	if _, err := os.Stat(path); err != nil {
		// Older prompt sets may not have the file; fall back to scout.md
		// so the call still runs (with the broader exploratory prompt).
		path = filepath.Join(dir, "scout.md")
	}
	return path, nil
}

// buildSelfResolvePacket bundles the original packet, the bulk report,
// and the conflict reason for the same opencode model on its second
// pass.
func buildSelfResolvePacket(p Pick, originalPacket, bulkReport, reason string) string {
	var b strings.Builder
	b.WriteString("# Scout self-resolve packet\n\n")
	fmt.Fprintf(&b, "Path: `%s`\n", p.Path)
	fmt.Fprintf(&b, "Title: %s\n", p.Title)
	fmt.Fprintf(&b, "Classifier flagged the prior draft because: %s\n\n", reason)
	b.WriteString("## Original entity packet\n\n")
	b.WriteString(originalPacket)
	b.WriteString("\n\n## Your prior draft scout report\n\n")
	b.WriteString(bulkReport)
	b.WriteString("\n\n## Your task\n\n")
	b.WriteString("Resolve each flagged item with a targeted MCP query, or mark genuinely-unresolvable items with `- needs paid review:` so the next pass can route them. Rewrite the entire scout report in the same section structure.\n")
	return b.String()
}

// escalateOne runs a paid medium-tier second pass over a bulk-tier
// report that the deterministic classifier flagged. The medium-tier
// output overwrites the report file; ledger gets a second entry tagged
// with the escalation stage. ReportEntry.Escalated is set on success.
//
// Triage stage is the routing target because it shares the medium-tier
// pool (codex-mini ↔ claude-haiku round-robin) and its prompt is the
// closest match for "resolve this evidence packet".
func backendForID(id string) (backend.Backend, error) {
	switch id {
	case "claude":
		return backend.ClaudeBackend{}, nil
	case "codex":
		return backend.CodexBackend{}, nil
	case "opencode":
		return backend.OpencodeBackend{}, nil
	}
	return nil, fmt.Errorf("scout: unknown backend id %q", id)
}

// buildScoutPacket renders the packet sent to the scout agent. It names
// the entity, its current frontmatter, recent vault context, and tells
// the agent which evidence sources are allowed.
func buildScoutPacket(brainRoot string, p Pick) string {
	abs := filepath.Join(brainRoot, p.Path)
	body, _ := os.ReadFile(abs)
	var b strings.Builder
	b.WriteString("# Scout verification packet\n\n")
	fmt.Fprintf(&b, "Entity path: `%s`\n", p.Path)
	fmt.Fprintf(&b, "Title: %s\n", p.Title)
	if p.Cadence != "" {
		fmt.Fprintf(&b, "Cadence: %s\n", p.Cadence)
	}
	if !p.LastSeen.IsZero() {
		fmt.Fprintf(&b, "Last seen: %s\n", p.LastSeen.Format("2006-01-02"))
	}
	fmt.Fprintf(&b, "Score: %.2f (%s)\n\n", p.Score, p.Reason)
	b.WriteString("## Current note body\n\n")
	if len(body) > 0 {
		b.WriteString("```markdown\n")
		b.Write(body)
		if !strings.HasSuffix(string(body), "\n") {
			b.WriteString("\n")
		}
		b.WriteString("```\n\n")
	} else {
		b.WriteString("(note body not readable)\n\n")
	}
	if len(p.UncertaintyMarkers) > 0 {
		b.WriteString("## Specific claims to verify\n\n")
		b.WriteString("The picker flagged these claims in the note body. Verify each one specifically; do not confine the report to the entity's high-level identity.\n\n")
		for _, m := range p.UncertaintyMarkers {
			fmt.Fprintf(&b, "- %s\n", m)
		}
		b.WriteString("\n")
	}
	b.WriteString("## Your task\n\n")
	b.WriteString(strings.Join([]string{
		"Verify this entity against external evidence.",
		"Use helpy `web_search`, `web_fetch`, `zotero_packets`, `tugonline_*` for external lookups.",
		"Use sloppy `brain_search`, `brain_backlinks`, `contact_search`, `calendar_events` for vault and groupware cross-checks.",
		"Never edit canonical Markdown. Write only an evidence report.",
		"Never invent facts; if a claim has no source, say so explicitly.",
		"Never register slopshell as an MCP server.",
		"If a `## Specific claims to verify` block is present in this packet, address each listed claim before producing the high-level Verified / Conflicting / Suggestions sections.",
		"",
		"Output format (Markdown):",
		"",
		"# Scout report — <entity title>",
		"",
		"## Verified",
		"- <bullet> (source: …)",
		"",
		"## Conflicting / outdated",
		"- <bullet> (current: …; observed: …; source: …)",
		"",
		"## Suggestions",
		"- <bullet> (path:line or section)",
		"",
		"## Open questions",
		"- <bullet>",
	}, "\n"))
	b.WriteString("\n")
	return b.String()
}

func sanitize(p string) string {
	out := make([]rune, 0, len(p))
	for _, r := range p {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			out = append(out, r)
		case r == '-' || r == '_':
			out = append(out, r)
		default:
			out = append(out, '-')
		}
	}
	s := strings.Trim(string(out), "-")
	if s == "" {
		s = "entity"
	}
	return s
}
