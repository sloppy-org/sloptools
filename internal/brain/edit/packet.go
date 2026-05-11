package edit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sloppy-org/sloptools/internal/brain/evidence"
	"github.com/sloppy-org/sloptools/internal/brain/triage"
)

// buildEntityPacket constructs the focused per-entity prompt packet.
// Target size: ≤ 4KB. If it exceeds that, backlinks are truncated first,
// then evidence, then the note body.
func buildEntityPacket(brainRoot string, item triage.Item, entries []evidence.Entry, activityDigest string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# Entity editorial pass\n\n")
	fmt.Fprintf(&b, "Entity: `%s`\n", item.Entity)
	fmt.Fprintf(&b, "Triage reason: %s (priority %d)\n\n", item.Reason, item.Priority)

	// 1. Current note content.
	notePath := filepath.Join(brainRoot, item.Entity)
	noteBody, _ := os.ReadFile(notePath)
	if len(noteBody) > 0 {
		body := string(noteBody)
		if len(body) > 2000 {
			body = body[:2000] + "\n...(note truncated for context limit)..."
		}
		b.WriteString("## Current note\n\n```markdown\n")
		b.WriteString(body)
		b.WriteString("\n```\n\n")
	} else {
		b.WriteString("## Current note\n\n(note does not exist yet — create it)\n\n")
	}

	// 2. Tonight's evidence entries for this entity.
	if len(entries) > 0 {
		b.WriteString("## Tonight's evidence\n\n")
		for _, e := range entries {
			if e.Verdict == evidence.VerdictSkipped {
				continue
			}
			sourceStr := ""
			if e.Source != "" {
				sourceStr = fmt.Sprintf(" (source: %s)", e.Source)
			}
			fmt.Fprintf(&b, "- [%s] %s%s\n", e.Verdict, e.Claim, sourceStr)
			if e.SuggestedEdit != "" {
				fmt.Fprintf(&b, "  → Suggested: %s\n", e.SuggestedEdit)
			}
		}
		b.WriteString("\n")
	}

	// 3. Activity mentions of this entity.
	if activityDigest != "" {
		entityName := entityShortName(item.Entity)
		mentions := extractMentions(activityDigest, entityName)
		if mentions != "" {
			b.WriteString("## Today's activity mentions\n\n")
			b.WriteString(mentions)
			b.WriteString("\n")
		}
	}

	out := b.String()

	// If still under 4KB, there's room for a backlinks note.
	if len(out) < 3*1024 {
		backlinksHint := "Use sloppy_brain action=backlinks to find related notes if useful."
		out += "## Backlinks\n\n" + backlinksHint + "\n"
	}

	// Hard cap at 4KB — truncate note body if needed.
	if len(out) > 4*1024 {
		// Already truncated note at 2KB above; this is a safety net.
		out = out[:4*1024-100] + "\n...(packet truncated)\n"
	}

	return out
}

// entityShortName extracts the base name without extension from a vault path.
// "people/Alice Smith.md" → "Alice Smith"
func entityShortName(entity string) string {
	base := filepath.Base(entity)
	ext := filepath.Ext(base)
	return strings.TrimSuffix(base, ext)
}

// extractMentions returns lines from the activity digest that mention the entity name.
func extractMentions(digest, name string) string {
	if name == "" {
		return ""
	}
	// Also try first name only for partial matching.
	nameLower := strings.ToLower(name)
	parts := strings.Fields(name)
	firstName := ""
	if len(parts) > 0 {
		firstName = strings.ToLower(parts[0])
	}

	var lines []string
	for _, line := range strings.Split(digest, "\n") {
		lineLower := strings.ToLower(line)
		if strings.Contains(lineLower, nameLower) {
			lines = append(lines, strings.TrimSpace(line))
			continue
		}
		// Match on first name if it's ≥ 4 chars (avoid false positives on "Dr", "JF", etc.)
		if len(firstName) >= 4 && strings.Contains(lineLower, firstName) {
			lines = append(lines, strings.TrimSpace(line))
		}
	}
	return strings.Join(lines, "\n")
}
