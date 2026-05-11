// Package triage runs a fast qwen-MoE pass to rank which canonical
// entities need editorial attention tonight. It reads the activity digest
// and tonight's evidence log entries and returns a ranked list of up to
// 15 entities with reasons. No tool calls — pure text reasoning, single
// POST to slopgate.
package triage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/brain/backend"
	"github.com/sloppy-org/sloptools/internal/brain/evidence"
	"github.com/sloppy-org/sloptools/internal/brain/routing"
)

// Item is one entity selected for editorial attention tonight.
type Item struct {
	Entity      string   `json:"entity"`       // vault-relative path
	Reason      string   `json:"reason"`       // one-line explanation
	Priority    int      `json:"priority"`     // 1-10, higher is more urgent
	EvidenceIDs []string `json:"evidence_ids"` // evidence entry identifiers
}

// Opts configures the triage pass.
type Opts struct {
	BrainRoot      string
	RunID          string
	ActivityDigest string           // from activity.Digest.Format()
	Entries        []evidence.Entry // tonight's unapplied evidence entries
	EntityPaths    []string         // candidate entity paths (from picker)
	Now            time.Time
}

// Run executes the triage pass and returns a ranked list of entities to
// edit tonight, capped at 15.
func Run(ctx context.Context, opts Opts) ([]Item, error) {
	if opts.Now.IsZero() {
		opts.Now = time.Now().UTC()
	}
	packet := buildPacket(opts)
	resp, err := runMoE(ctx, opts.RunID, packet)
	if err != nil {
		return fallback(opts.Entries, opts.EntityPaths), nil
	}
	items, err := parseResponse(resp)
	if err != nil {
		// Retry once with explicit format reminder.
		retry := packet + "\n\nIMPORTANT: Output ONLY a valid JSON array. No text before or after the array."
		resp2, err2 := runMoE(ctx, opts.RunID+"-retry", retry)
		if err2 != nil {
			return fallback(opts.Entries, opts.EntityPaths), nil
		}
		items, err = parseResponse(resp2)
		if err != nil {
			return fallback(opts.Entries, opts.EntityPaths), nil
		}
	}
	if len(items) > 15 {
		items = items[:15]
	}
	return items, nil
}

func buildPacket(opts Opts) string {
	var b strings.Builder
	b.WriteString("You are ranking which brain vault entities need editorial attention tonight.\n\n")
	b.WriteString("Output ONLY a JSON array, max 15 items:\n")
	b.WriteString(`[{"entity":"people/X.md","reason":"one sentence","priority":8,"evidence_ids":[]}]`)
	b.WriteString("\n\n")
	b.WriteString("Rank by: conflicting evidence > new person from activity > verified outdated info > stale entity with meetings today.\n")
	b.WriteString("Skip entities with no actionable signal. Output the JSON array only — no markdown, no explanation.\n\n")

	if opts.ActivityDigest != "" {
		b.WriteString(opts.ActivityDigest)
		b.WriteString("\n")
	}

	if len(opts.Entries) > 0 {
		b.WriteString("## Tonight's scout evidence (unapplied)\n")
		count := 0
		for _, e := range opts.Entries {
			if e.Verdict == evidence.VerdictSkipped {
				continue
			}
			fmt.Fprintf(&b, "- [%s] %s: %s\n", e.Verdict, e.Entity, trunc(e.Claim, 120))
			count++
			if count >= 30 {
				break
			}
		}
		b.WriteString("\n")
	}

	if len(opts.EntityPaths) > 0 {
		b.WriteString("## Candidate entities (by attention score)\n")
		for i, p := range opts.EntityPaths {
			if i >= 50 {
				break
			}
			fmt.Fprintf(&b, "- %s\n", p)
		}
	}

	out := b.String()
	// Hard cap at 3KB: truncate entity list first, then evidence, then digest.
	const maxBytes = 3 * 1024
	if len(out) > maxBytes {
		if idx := strings.Index(out, "## Candidate entities"); idx > 0 {
			prefix := out[:idx]
			if len(prefix) > maxBytes {
				// Even without entity list it's too big — truncate evidence.
				if idx2 := strings.Index(prefix, "## Tonight's"); idx2 > 0 {
					prefix = prefix[:idx2] + "## Tonight's scout evidence\n(truncated for context limit)\n"
				}
			}
			out = prefix + "## Candidate entities\n(truncated for context limit)\n"
		}
		// Final safety truncation.
		if len(out) > maxBytes {
			out = out[:maxBytes-50] + "\n...(truncated)\n"
		}
	}
	return out
}

const triageSysPrompt = "You are a brain-vault triage assistant for Christopher Albert. " +
	"Given an activity digest and scout evidence, output a JSON array ranking entities by editorial urgency. " +
	"Output only valid JSON. No prose, no markdown fences."

func runMoE(ctx context.Context, runID, packet string) (string, error) {
	pick := routing.LlamacppMoEBulk()
	be := backend.LlamacppBackend{}

	// Write system prompt to a temp file for the sandbox.
	dir, err := os.MkdirTemp("", "sloptools-triage-")
	if err != nil {
		return "", fmt.Errorf("triage: mktemp: %w", err)
	}
	defer os.RemoveAll(dir)
	promptPath := filepath.Join(dir, "triage.md")
	if err := os.WriteFile(promptPath, []byte(triageSysPrompt), 0o600); err != nil {
		return "", fmt.Errorf("triage: write prompt: %w", err)
	}

	sb, err := backend.NewSandbox(runID, "triage", promptPath, backend.MCPConfig{})
	if err != nil {
		return "", fmt.Errorf("triage: sandbox: %w", err)
	}
	defer sb.Cleanup()

	resp, err := be.Run(ctx, backend.Request{
		Stage:            "triage",
		Packet:           packet,
		SystemPromptPath: sb.SystemPromptIn,
		Model:            pick.Model,
		Reasoning:        pick.Reasoning,
		AllowEdits:       false,
		MCPAllowList:     nil, // no tool calls
		Affinity:         backend.AffinityForPick(runID, "triage", "triage"),
		Sandbox:          sb,
	})
	if err != nil {
		return "", err
	}
	return resp.Output, nil
}

func parseResponse(raw string) ([]Item, error) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)
	start := strings.Index(raw, "[")
	end := strings.LastIndex(raw, "]")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("no JSON array in triage output: %q", trunc(raw, 100))
	}
	var items []Item
	if err := json.Unmarshal([]byte(raw[start:end+1]), &items); err != nil {
		return nil, fmt.Errorf("parse triage JSON: %w", err)
	}
	return items, nil
}

// fallback returns up to 5 entities from evidence + paths when triage fails.
func fallback(entries []evidence.Entry, paths []string) []Item {
	counts := map[string]int{}
	for _, e := range entries {
		if e.Verdict != evidence.VerdictSkipped {
			counts[e.Entity]++
		}
	}
	seen := map[string]bool{}
	var items []Item
	for _, e := range entries {
		if seen[e.Entity] || e.Verdict == evidence.VerdictSkipped {
			continue
		}
		seen[e.Entity] = true
		items = append(items, Item{Entity: e.Entity, Reason: "has scout evidence", Priority: counts[e.Entity]})
		if len(items) >= 5 {
			break
		}
	}
	for _, p := range paths {
		if seen[p] || len(items) >= 5 {
			break
		}
		seen[p] = true
		items = append(items, Item{Entity: p, Reason: "top attention score", Priority: 1})
	}
	return items
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}
