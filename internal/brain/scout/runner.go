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
}

// RunReport summarises the scout pass.
type RunReport struct {
	RunID       string         `json:"run_id"`
	StartedAt   time.Time      `json:"started_at"`
	EndedAt     time.Time      `json:"ended_at"`
	Picked      int            `json:"picked"`
	ReportsDir  string         `json:"reports_dir"`
	Reports     []ReportEntry  `json:"reports"`
	Skipped     int            `json:"skipped"`
	Errors      []string       `json:"errors,omitempty"`
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
	}
	for _, p := range opts.Picks {
		entry := runOnePick(ctx, opts, dir, stagePrompt, p)
		if entry.Skipped {
			report.Skipped++
		}
		if entry.ReportPath != "" {
			report.Picked++
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
	body := strings.TrimSpace(resp.Output)
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
	return entry
}

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
	b.WriteString("## Your task\n\n")
	b.WriteString(strings.Join([]string{
		"Verify this entity against external evidence.",
		"Use helpy `web_search`, `web_fetch`, `zotero_packets`, `tugonline_*` for external lookups.",
		"Use sloppy `brain_search`, `brain_backlinks`, `contact_search`, `calendar_events` for vault and groupware cross-checks.",
		"Never edit canonical Markdown. Write only an evidence report.",
		"Never invent facts; if a claim has no source, say so explicitly.",
		"Never register slopshell as an MCP server.",
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
