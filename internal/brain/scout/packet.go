package scout

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sloppy-org/sloptools/internal/brain/backend"
	"github.com/sloppy-org/sloptools/internal/brain/glossary"
	"github.com/sloppy-org/sloptools/internal/brain/routing"
)

// backendForID maps the routing.Pick BackendID to the concrete backend
// implementation. Kept here (and not in routing) so the scout package
// owns its own mapping; routing only knows the string ids.
func backendForID(id string) (backend.Backend, error) {
	switch id {
	case "claude":
		return backend.ClaudeBackend{}, nil
	case "codex":
		return backend.CodexBackend{}, nil
	case "llamacpp":
		return backend.LlamacppBackend{}, nil
	}
	return nil, fmt.Errorf("scout: unknown backend id %q", id)
}

// backendForPick returns the Backend for a Pick without error.
func backendForPick(pick routing.Pick) backend.Backend {
	switch pick.BackendID {
	case "codex":
		return backend.CodexBackend{}
	case "claude":
		return backend.ClaudeBackend{}
	default:
		return backend.LlamacppBackend{}
	}
}

// buildScoutPacket renders the packet sent to the scout agent. It names
// the entity, its current frontmatter, recent vault context, glossary
// hits for any local-vocabulary terms in the body (so the agent does
// not e.g. confuse "1/ν transport" with "neutrino transport"), and the
// allowed evidence sources.
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
	if section := glossary.FormatPacketSection(glossary.RelevantTerms(brainRoot, string(body))); section != "" {
		b.WriteString(section)
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
		"Use helpy `web_search`, `web_fetch`, `web_fetch_packet`, `zotero_packets`, `tugonline_*`, `tu4u_*`, `pdf_read` for external lookups; helpy `pdf_read` is the only sanctioned PDF reader (modes: metadata | text with `pages` and `max_bytes` | outline; image-only PDFs return status:image_only). Per-run caps: web_search ≤ 5, web_fetch ≤ 8, zotero ≤ 4, tugonline ≤ 3, tu4u ≤ 3, pdf_read ≤ 6 — a quota-exceeded message means stop, not retry. Use `helpy_tool_help tool_family=<family>` or `action=help` on any helpy_* tool to discover real action names.",
		"Use sloppy `brain_search`, `brain_backlinks`, `contact_search`, `calendar_events` for vault and groupware cross-checks.",
		"Read-only bash is allowed for harmless local file inspection: `ls`, `head`, `tail`, `wc`, `file`, `find`, `rg --files`, `stat`, `pwd`. Everything else (`cat`, `pdftotext`, `curl`, `awk`, etc.) is denied — use the helpy MCP equivalent.",
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

// sanitize rewrites a vault path into a slug safe for filesystem and
// stage-name use. Keeps [a-zA-Z0-9_-] and turns everything else into
// `-`. Trims leading/trailing hyphens; falls back to "entity" when the
// input has no usable characters.
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
