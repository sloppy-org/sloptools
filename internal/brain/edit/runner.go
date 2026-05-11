// Package edit runs focused per-entity editorial passes. For each triaged
// entity it builds a tight 4-6KB packet (canonical note + tonight's
// evidence entries + activity mentions + top backlinks) and runs qwen27b
// with sloppy_brain note_write capability. The model edits the vault
// directly. Each successful edit is committed and the evidence entries
// marked applied.
package edit

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/brain/backend"
	"github.com/sloppy-org/sloptools/internal/brain/evidence"
	"github.com/sloppy-org/sloptools/internal/brain/ledger"
	"github.com/sloppy-org/sloptools/internal/brain/routing"
	"github.com/sloppy-org/sloptools/internal/brain/triage"
)

// Opts configures the edit stage.
type Opts struct {
	BrainRoot      string
	RunID          string
	Sphere         string
	Items          []triage.Item    // from triage.Run
	AllEntries     []evidence.Entry // full tonight's evidence for filtering
	ActivityDigest string
	Router         *routing.Router
	Ledger         *ledger.Ledger
	Now            time.Time
}

// Result records the outcome of one entity edit.
type Result struct {
	Entity    string
	Edited    bool
	Escalated bool
	Skipped   bool
	Reason    string
	WallMS    int64
}

// Report summarises the full edit stage.
type Report struct {
	Results   []Result
	Edited    int
	Escalated int
	Skipped   int
}

// Run executes per-entity edit passes for all triaged items in series.
func Run(ctx context.Context, opts Opts) (*Report, error) {
	if opts.Now.IsZero() {
		opts.Now = time.Now().UTC()
	}
	report := &Report{}
	for _, item := range opts.Items {
		r := runOne(ctx, opts, item)
		report.Results = append(report.Results, r)
		if r.Edited {
			report.Edited++
		}
		if r.Escalated {
			report.Escalated++
		}
		if r.Skipped {
			report.Skipped++
		}
	}
	return report, nil
}

func runOne(ctx context.Context, opts Opts, item triage.Item) Result {
	start := time.Now().UTC()
	res := Result{Entity: item.Entity}

	// Collect evidence entries for this entity.
	var entityEntries []evidence.Entry
	for _, e := range opts.AllEntries {
		if e.Entity == item.Entity && !e.Applied && !e.Reverted {
			entityEntries = append(entityEntries, e)
		}
	}

	// Build the per-entity packet.
	packet := buildEntityPacket(opts.BrainRoot, item, entityEntries, opts.ActivityDigest)

	// Write system prompt.
	promptPath, cleanup, err := writeEntityPrompt(opts.BrainRoot)
	if err != nil {
		res.Skipped = true
		res.Reason = "write prompt: " + err.Error()
		return res
	}
	defer cleanup()

	// Run qwen27b with sloppy_brain.
	pick := routing.LlamacppQwenHigh()
	be := backend.LlamacppBackend{}
	stage := "edit-" + sanitize(item.Entity)
	sb, err := backend.NewSandbox(opts.RunID, stage, promptPath, backend.DefaultMCPConfig())
	if err != nil {
		res.Skipped = true
		res.Reason = "sandbox: " + err.Error()
		return res
	}
	defer sb.Cleanup()

	allowList := []string{"sloppy_brain"} // no web_search — that was scout's job
	if opts.Router != nil {
		// Use router's MCP tools for the sleep-judge stage as the base allowlist.
		if tools := opts.Router.MCPToolsFor(routing.StageSleepJudge); len(tools) > 0 {
			// Filter to sloppy_brain only.
			for _, t := range tools {
				if t == "sloppy_brain" {
					allowList = []string{"sloppy_brain"}
					break
				}
			}
		}
	}

	req := backend.Request{
		Stage:            stage,
		Packet:           packet,
		SystemPromptPath: sb.SystemPromptIn,
		Model:            pick.Model,
		Reasoning:        pick.Reasoning,
		AllowEdits:       true,
		MCPAllowList:     allowList,
		Affinity:         backend.AffinityForPick(opts.RunID, item.Entity, "edit"),
		Sandbox:          sb,
		WorkDir:          opts.BrainRoot,
	}

	resp, err := be.Run(ctx, req)
	res.WallMS = resp.WallMS
	if err != nil || strings.TrimSpace(resp.Output) == "" {
		// Escalate to codex when qwen27b fails.
		escalated, escErr := escalateOne(ctx, opts, item, packet, promptPath)
		res.Escalated = escalated
		if escErr != nil {
			res.Skipped = true
			res.Reason = fmt.Sprintf("qwen27b failed (%v), escalation failed (%v)", err, escErr)
		} else {
			res.Edited = escalated
		}
	} else {
		res.Edited = true
		// Log ledger entry.
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
				Extras:    map[string]string{"entity": item.Entity},
			})
		}
	}

	// Mark evidence entries as applied on success.
	if res.Edited {
		_ = evidence.MarkApplied(opts.BrainRoot, item.Entity, opts.RunID)
		// Commit this entity's edit.
		commitEntityEdit(opts.BrainRoot, item.Entity, item.Reason)
	}
	res.WallMS = time.Since(start).Milliseconds()
	return res
}

func escalateOne(ctx context.Context, opts Opts, item triage.Item, packet, promptPath string) (bool, error) {
	if opts.Router == nil {
		return false, fmt.Errorf("no router for escalation")
	}
	pick, err := opts.Router.Pick(routing.StageSleepJudge)
	if err != nil {
		return false, fmt.Errorf("router: %w", err)
	}
	if pick.Provider == backend.ProviderLocal {
		return false, fmt.Errorf("paid tier saturated")
	}
	be, err := backendForID(pick.BackendID)
	if err != nil {
		return false, err
	}
	stage := "edit-escalate-" + sanitize(item.Entity)
	sb, err := backend.NewSandbox(opts.RunID, stage, promptPath, backend.DefaultMCPConfig())
	if err != nil {
		return false, fmt.Errorf("sandbox: %w", err)
	}
	defer sb.Cleanup()

	resp, err := be.Run(ctx, backend.Request{
		Stage:            stage,
		Packet:           packet,
		SystemPromptPath: sb.SystemPromptIn,
		Model:            pick.Model,
		Reasoning:        pick.Reasoning,
		AllowEdits:       true,
		MCPAllowList:     []string{"sloppy_brain"},
		Sandbox:          sb,
		WorkDir:          opts.BrainRoot,
	})
	if err != nil || strings.TrimSpace(resp.Output) == "" {
		return false, fmt.Errorf("escalation produced no output")
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
			Extras:    map[string]string{"entity": item.Entity, "escalation": "true"},
		})
	}
	return true, nil
}

func backendForID(id string) (backend.Backend, error) {
	switch id {
	case "codex":
		return backend.CodexBackend{}, nil
	case "claude":
		return backend.ClaudeBackend{}, nil
	case "llamacpp":
		return backend.LlamacppBackend{}, nil
	}
	return nil, fmt.Errorf("unknown backend %q", id)
}

func commitEntityEdit(brainRoot, entity, reason string) {
	msg := fmt.Sprintf("brain edit: %s — %s", entity, reason)
	cmd := exec.Command("git", "-C", brainRoot, "add", "-A")
	_ = cmd.Run()
	cmd2 := exec.Command("git", "-C", brainRoot, "commit", "-m", msg)
	_ = cmd2.Run()
}

func sanitize(p string) string {
	out := make([]rune, 0, len(p))
	for _, r := range p {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			out = append(out, r)
		default:
			out = append(out, '-')
		}
	}
	s := strings.Trim(string(out), "-")
	if s == "" {
		s = "entity"
	}
	if len(s) > 60 {
		s = s[:60]
	}
	return s
}

func writeEntityPrompt(brainRoot string) (path string, cleanup func(), err error) {
	// Try reading the conventions for schema guidance.
	conventionHint := ""
	attPath := filepath.Join(brainRoot, "conventions", "attention.md")
	if body, err2 := os.ReadFile(attPath); err2 == nil && len(body) > 0 {
		// Include only the first 500 chars (schema section).
		h := string(body)
		if len(h) > 500 {
			h = h[:500]
		}
		conventionHint = "\n\nVault schema hint (from brain/conventions/attention.md):\n" + h
	}

	dir, err := os.MkdirTemp("", "sloptools-edit-")
	if err != nil {
		return "", nil, err
	}
	p := filepath.Join(dir, "entity-edit.md")
	if err := os.WriteFile(p, []byte(entityEditPrompt+conventionHint), 0o600); err != nil {
		os.RemoveAll(dir)
		return "", nil, err
	}
	return p, func() { os.RemoveAll(dir) }, nil
}

const entityEditPrompt = `You are editing one canonical brain note for Christopher Albert.

You receive:
1. The entity's current note content
2. Tonight's verified or conflicting evidence about this entity
3. Today's activity mentions (meetings, mail)
4. Key backlinks from other notes

Your job:
- Apply the evidence: update facts that are confirmed or corrected with a source
- Apply activity: add dated bullets for today's meetings/interactions
- Update frontmatter: last_seen (YYYY-MM-DD), status if changed by evidence
- Create the note if it does not exist yet (new person from activity)

Use sloppy_brain action=note_write to write the updated note.
Use sloppy_brain action=note_parse to read the current note content first if needed.
Use sloppy_brain action=backlinks to find related notes if needed.
Maximum 3 tool calls total. Be efficient.

Rules:
- Do not invent facts. Only record claims that appear in the evidence or activity.
- Do not add a source you cannot cite.
- Do not touch commitments/, gtd/, glossary/ — those are write-locked.
- Match the existing note style exactly. Do not add headings not already present.
- If no changes are warranted, say so briefly — do not write an empty edit.`
