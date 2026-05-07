// Package cleanup holds deterministic post-processors that strip
// narration from raw model output before it is persisted as a report.
// The package is intentionally tiny and zero-dep so any brain-night
// stage can call it without fanning out imports.
package cleanup

import "strings"

// CleanReport removes preamble and footer narration that the model
// sometimes glues around a structured scout / sleep / triage report.
// Behaviour is deterministic: trim leading content before the first
// column-zero `# ` ATX h1, and at the trailing end strip any block
// that begins with a `---` horizontal rule whose next non-blank line
// is a `**...**`-style narration label (Note, Methodology, Disclaimer,
// etc.). Tighter prompts reduce the frequency, but a deterministic
// trim is the only thing that guarantees the saved artifact is the
// report alone.
func CleanReport(body string) string {
	body = strings.TrimSpace(body)
	body = trimPreambleBeforeFirstH1(body)
	body = trimTrailingFooter(body)
	return strings.TrimSpace(body)
}

// trimPreambleBeforeFirstH1 finds the first column-zero `# ` ATX h1 and
// drops everything before it. Returns body unchanged if no h1 exists,
// so non-report outputs are not silently emptied.
func trimPreambleBeforeFirstH1(body string) string {
	lines := strings.Split(body, "\n")
	for i, ln := range lines {
		if strings.HasPrefix(ln, "# ") {
			return strings.Join(lines[i:], "\n")
		}
	}
	return body
}

// trimTrailingFooter looks for the LAST column-zero `---` horizontal
// rule. If the next non-blank line begins with `**` and is one of the
// known footer labels, it trims from the rule onward. The bold-prefix
// check keeps inline rules between sections from being cut. The label
// allowlist keeps a legitimate "**Some bold note**" inside the report
// (e.g., a Conflicting bullet that ends with bold text) from being
// mistaken for a footer.
func trimTrailingFooter(body string) string {
	lines := strings.Split(body, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "---" {
			continue
		}
		next := nextNonBlank(lines, i+1)
		if next < 0 {
			continue
		}
		stripped := strings.TrimSpace(lines[next])
		if !strings.HasPrefix(stripped, "**") {
			return body
		}
		if !isFooterLabel(stripped) {
			return body
		}
		return strings.Join(lines[:i], "\n")
	}
	return body
}

func nextNonBlank(lines []string, start int) int {
	for j := start; j < len(lines); j++ {
		if strings.TrimSpace(lines[j]) != "" {
			return j
		}
	}
	return -1
}

// isFooterLabel reports whether a line begins with a `**...**` bold
// prefix that names a known narration footer. Matched case-insensitively
// against the leading bold span up to the closing `**`.
func isFooterLabel(line string) bool {
	if !strings.HasPrefix(line, "**") {
		return false
	}
	rest := line[2:]
	end := strings.Index(rest, "**")
	if end < 0 {
		return false
	}
	label := strings.ToLower(strings.TrimSpace(rest[:end]))
	switch label {
	case "note",
		"note on methodology",
		"note on tools",
		"note on permissions",
		"note on access",
		"methodology",
		"methodology note",
		"disclaimer",
		"summary of resolution",
		"summary of changes":
		return true
	}
	if strings.HasPrefix(label, "note on ") {
		return true
	}
	return false
}
