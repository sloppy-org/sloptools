package brainprojects

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/brain"
	braingtd "github.com/sloppy-org/sloptools/internal/brain/gtd"
)

func commitmentBucket(c braingtd.Commitment, now time.Time, recentOnly bool) string {
	status := effectiveStatus(c)
	switch {
	case status == "closed" || status == "done":
		if recentOnly && !commitmentClosedRecently(c, now) {
			return ""
		}
		return "closed"
	case strings.TrimSpace(c.WaitingFor) != "" || status == "waiting" || status == "deferred":
		return "waiting"
	case status == "" || status == "next" || status == "inbox":
		return "next"
	default:
		return ""
	}
}

func commitmentClosedRecently(c braingtd.Commitment, now time.Time) bool {
	closed := closedAt(c)
	if closed == "" {
		closed = c.LastEvidenceAt
	}
	t, ok := parseDate(closed)
	return ok && !t.Before(now.AddDate(0, 0, -14)) && !t.After(now.Add(24*time.Hour))
}

func closedAt(c braingtd.Commitment) string {
	return strings.TrimSpace(c.LocalOverlay.ClosedAt)
}

func effectiveStatus(c braingtd.Commitment) string {
	status := strings.TrimSpace(c.LocalOverlay.Status)
	if status == "" {
		status = strings.TrimSpace(c.Status)
	}
	return strings.ToLower(status)
}

func commitmentOutcome(c braingtd.Commitment) string {
	for _, value := range []string{c.Outcome, c.Title, c.NextAction} {
		if clean := strings.TrimSpace(value); clean != "" {
			return clean
		}
	}
	return "Untitled commitment"
}

func commitmentPeople(c braingtd.Commitment) []string {
	values := append([]string{}, c.People...)
	values = append(values, c.Actor, c.WaitingFor)
	return compact(values)
}

func sortProjectItems(items ProjectBuckets) {
	sortItems(items.Next)
	sortItems(items.Waiting)
	sortItems(items.Closed)
}

func sortItems(items []ProjectCommitment) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].SortDue != items[j].SortDue {
			return emptyLast(items[i].SortDue, items[j].SortDue)
		}
		if items[i].SortFollow != items[j].SortFollow {
			return emptyLast(items[i].SortFollow, items[j].SortFollow)
		}
		if items[i].Outcome != items[j].Outcome {
			return items[i].Outcome < items[j].Outcome
		}
		return items[i].Path < items[j].Path
	})
}

func emptyLast(a, b string) bool {
	if a == "" {
		return false
	}
	if b == "" {
		return true
	}
	return a < b
}

func sameProject(raw, project string) bool {
	return normalizeProject(raw) == normalizeProject(project)
}

func normalizeProject(value string) string {
	clean := strings.TrimSpace(value)
	clean = strings.TrimPrefix(strings.TrimSuffix(clean, "]]"), "[[")
	if i := strings.IndexByte(clean, '|'); i >= 0 {
		clean = clean[:i]
	}
	clean = strings.TrimSuffix(clean, ".md")
	return strings.ToLower(strings.TrimSpace(clean))
}

func projectLink(vault brain.Vault, hub brain.ResolvedPath) string {
	target := brainRelative(vault, hub)
	return "[[" + strings.TrimSuffix(target, ".md") + "]]"
}

func noteLink(vault brain.Vault, source brain.ResolvedPath, text string) string {
	target := strings.TrimSuffix(brainRelative(vault, source), ".md")
	return "[[" + target + "|" + strings.TrimSpace(text) + "]]"
}

func brainRelative(vault brain.Vault, source brain.ResolvedPath) string {
	rel := filepath.ToSlash(source.Rel)
	prefix := filepath.ToSlash(vault.Brain)
	if prefix != "" && strings.HasPrefix(rel, prefix+"/") {
		return strings.TrimPrefix(rel, prefix+"/")
	}
	return rel
}

func personLink(person string) string {
	clean := strings.TrimSpace(person)
	return "[[people/" + clean + "|" + clean + "]]"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if clean := strings.TrimSpace(value); clean != "" {
			return clean
		}
	}
	return ""
}

func shortDate(value string) string {
	t, ok := parseDate(value)
	if !ok {
		return strings.TrimSpace(value)
	}
	return t.Format("2006-01-02")
}

func parseDate(value string) (time.Time, bool) {
	clean := strings.TrimSpace(value)
	if clean == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339, clean); err == nil {
		return t.UTC(), true
	}
	if t, err := time.Parse("2006-01-02", clean); err == nil {
		return t, true
	}
	return time.Time{}, false
}

func searchableText(note commitmentNote) string {
	c := note.Commitment
	parts := []string{note.Source.Rel, c.Title, c.Outcome, c.NextAction, c.Context, c.Actor, c.WaitingFor}
	parts = append(parts, c.People...)
	parts = append(parts, c.Labels...)
	return strings.Join(parts, "\n")
}

func containsFold(values []string, want string) bool {
	canonical := normalizeName(want)
	for _, value := range values {
		if normalizeName(value) == canonical {
			return true
		}
	}
	return false
}

func normalizeName(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

func compact(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		key := normalizeName(value)
		if value == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, value)
	}
	return out
}

func ruleKeys(rules []compiledRule) []string {
	out := make([]string, 0, len(rules))
	for _, rule := range rules {
		out = append(out, rule.Key)
	}
	sort.Strings(out)
	return out
}

func validateMarkdown(src string) error {
	if diags := brain.ValidateMarkdownNote(src, brain.MarkdownParseOptions{}); len(diags) != 0 {
		return diags[0]
	}
	return nil
}

func isH2(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "## ") && !strings.HasPrefix(trimmed, "### ")
}

func linesAt(src string) []string {
	if src == "" {
		return []string{""}
	}
	return strings.SplitAfter(src, "\n")
}

func expandPath(path string) string {
	clean := strings.TrimSpace(path)
	if clean != "~" && !strings.HasPrefix(clean, "~/") {
		return clean
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return clean
	}
	if clean == "~" {
		return home
	}
	return filepath.Join(home, strings.TrimPrefix(clean, "~/"))
}
