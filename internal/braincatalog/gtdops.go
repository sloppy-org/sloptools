package braincatalog

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/sloppy-org/sloptools/internal/brain"
	braingtd "github.com/sloppy-org/sloptools/internal/brain/gtd"
)

type MeetingTask struct {
	Line int    `json:"line"`
	Text string `json:"text"`
}

func BuildGTDIndexMarkdown(items []GTDListItem, sphere string) string {
	grouped := groupGTDItems(items, "")
	return buildGTDMarkdown("GTD Organize", sphere, grouped)
}

func BuildGTDDashboardMarkdown(items []GTDListItem, sphere, name string) string {
	grouped := groupGTDItems(items, name)
	return buildGTDMarkdown("GTD Dashboard: "+strings.TrimSpace(name), sphere, grouped)
}

func BuildGTDReviewBatchMarkdown(items []GTDListItem, sphere, query string) string {
	grouped := groupGTDItems(selectGTDReviewBatchItems(items, query), "")
	return buildGTDMarkdown("GTD Review Batch: "+strings.TrimSpace(query), sphere, grouped)
}

func BuildGTDCommitmentMarkdown(commitment braingtd.Commitment) (string, error) {
	if strings.TrimSpace(commitment.Kind) == "" {
		commitment.Kind = "commitment"
	}
	note, diags := brain.ParseMarkdownNote(buildGTDCommitmentTemplate(commitment), brain.MarkdownParseOptions{})
	if len(diags) != 0 {
		return "", fmt.Errorf("gtd commitment template invalid: %s", formatMarkdownDiagnostics(diags))
	}
	if err := writeGTDCommitmentFrontMatter(note, commitment); err != nil {
		return "", err
	}
	if err := braingtd.ApplyCommitment(note, commitment); err != nil {
		return "", err
	}
	return note.Render()
}

func ExtractMeetingTasks(src string) []MeetingTask {
	return ExtractIngestTasks("meetings", src)
}

func ExtractIngestTasks(source, src string) []MeetingTask {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "meetings", "mail", "todoist", "github", "gitlab", "evernote":
		return extractCheckboxTasks(src)
	default:
		return nil
	}
}

func extractCheckboxTasks(src string) []MeetingTask {
	lines := strings.Split(src, "\n")
	out := make([]MeetingTask, 0, len(lines))
	for i, raw := range lines {
		trimmed := strings.TrimSpace(raw)
		if !strings.HasPrefix(trimmed, "- [ ]") && !strings.HasPrefix(trimmed, "* [ ]") {
			continue
		}
		text := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(trimmed, "- [ ]"), "* [ ]"))
		if text == "" {
			continue
		}
		out = append(out, MeetingTask{Line: i + 1, Text: text})
	}
	return out
}

func selectGTDReviewBatchItems(items []GTDListItem, query string) []GTDListItem {
	return selectGTDReviewBatchItemsAt(items, query, time.Now().UTC())
}

func selectGTDReviewBatchItemsAt(items []GTDListItem, query string, now time.Time) []GTDListItem {
	filtered := make([]GTDListItem, 0, len(items))
	for _, item := range items {
		queue, why := gtdReviewBatchQueue(item, now)
		if queue == "done" || queue == "closed" {
			continue
		}
		if query != "" && !gtdItemMatchesQuery(item, query) {
			continue
		}
		item.Status = queue
		item.Why = why
		filtered = append(filtered, item)
	}
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].Status != filtered[j].Status {
			return gtdStatusRank(filtered[i].Status) < gtdStatusRank(filtered[j].Status)
		}
		if filtered[i].Due != filtered[j].Due {
			return filtered[i].Due < filtered[j].Due
		}
		return strings.ToLower(filtered[i].Title) < strings.ToLower(filtered[j].Title)
	})
	return filtered
}

func gtdReviewBatchQueue(item GTDListItem, now time.Time) (string, string) {
	status := strings.ToLower(strings.TrimSpace(item.Status))
	switch status {
	case "done", "closed":
		return status, "status=" + status
	case "maybe_stale":
		return "review", "status=maybe_stale"
	case "deferred":
		if due, ok := parseGTDReviewDate(item.FollowUp); ok && !due.After(now) {
			return "next", "follow_up reached"
		}
		return "deferred", "follow_up future"
	case "waiting":
		return "waiting", "status=waiting"
	case "someday":
		return "someday", "status=someday"
	case "inbox":
		return "inbox", "status=inbox"
	case "next":
		return "next", "status=next"
	}
	if due, ok := parseGTDReviewDate(item.Due); ok && !due.After(now) {
		return "review", "due overdue"
	}
	if status == "" {
		return "inbox", "status=inbox"
	}
	return status, "status=" + status
}

func parseGTDReviewDate(raw string) (time.Time, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}, false
	}
	if len(value) >= len("2006-01-02T15:04:05Z07:00") {
		if t, err := time.Parse(time.RFC3339, value); err == nil {
			return t.UTC(), true
		}
	}
	if len(value) >= len("2006-01-02") {
		if t, err := time.Parse("2006-01-02", value[:10]); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

func groupGTDItems(items []GTDListItem, query string) map[string][]GTDListItem {
	filtered := make([]GTDListItem, 0, len(items))
	for _, item := range items {
		if query != "" && !gtdItemMatchesQuery(item, query) {
			continue
		}
		filtered = append(filtered, item)
	}
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].Status != filtered[j].Status {
			return gtdStatusRank(filtered[i].Status) < gtdStatusRank(filtered[j].Status)
		}
		if filtered[i].Due != filtered[j].Due {
			return filtered[i].Due < filtered[j].Due
		}
		return strings.ToLower(filtered[i].Title) < strings.ToLower(filtered[j].Title)
	})
	grouped := map[string][]GTDListItem{
		"next":     {},
		"inbox":    {},
		"waiting":  {},
		"review":   {},
		"deferred": {},
		"someday":  {},
		"done":     {},
		"closed":   {},
		"other":    {},
	}
	for _, item := range filtered {
		key := normalizeGTDStatus(item.Status)
		if _, ok := grouped[key]; !ok {
			key = "other"
		}
		grouped[key] = append(grouped[key], item)
	}
	return grouped
}

func buildGTDMarkdown(title, sphere string, grouped map[string][]GTDListItem) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("kind: note\n")
	if strings.TrimSpace(sphere) != "" {
		b.WriteString("sphere: " + yamlQuote(sphere) + "\n")
	}
	b.WriteString("title: " + yamlQuote(title) + "\n")
	b.WriteString("---\n")
	b.WriteString("# " + title + "\n")
	for _, section := range []struct {
		key   string
		title string
	}{
		{"next", "Next"},
		{"inbox", "Inbox"},
		{"waiting", "Waiting"},
		{"review", "Review"},
		{"deferred", "Deferred"},
		{"someday", "Someday"},
		{"done", "Done"},
		{"closed", "Closed"},
		{"other", "Other"},
	} {
		items := grouped[section.key]
		if len(items) == 0 {
			continue
		}
		b.WriteString("\n## " + section.title + " (" + fmt.Sprintf("%d", len(items)) + ")\n")
		for _, item := range items {
			b.WriteString("- [" + item.Title + "](" + item.Path + ")")
			if item.Due != "" {
				b.WriteString(" due " + item.Due)
			} else if item.FollowUp != "" {
				b.WriteString(" follow up " + item.FollowUp)
			}
			if item.Project != "" {
				b.WriteString(" project " + item.Project)
			}
			if item.Actor != "" {
				b.WriteString(" actor " + item.Actor)
			}
			if item.Why != "" {
				b.WriteString(" because " + item.Why)
			}
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func gtdItemMatchesQuery(item GTDListItem, query string) bool {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return true
	}
	fields := []string{item.Title, item.Path, item.Status, item.Project, item.Actor, item.WaitingFor, item.Due, item.FollowUp}
	for _, field := range fields {
		if strings.Contains(strings.ToLower(field), q) {
			return true
		}
	}
	for _, label := range item.Labels {
		if strings.Contains(strings.ToLower(label), q) {
			return true
		}
	}
	for _, binding := range item.Bindings {
		if strings.Contains(strings.ToLower(binding), q) {
			return true
		}
	}
	return false
}

func normalizeGTDStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "inbox", "next", "waiting", "review", "deferred", "someday", "done", "closed":
		return strings.ToLower(strings.TrimSpace(status))
	default:
		return "other"
	}
}

func gtdStatusRank(status string) int {
	switch normalizeGTDStatus(status) {
	case "inbox":
		return 0
	case "next":
		return 1
	case "waiting":
		return 2
	case "review":
		return 3
	case "deferred":
		return 4
	case "someday":
		return 5
	case "done":
		return 6
	case "closed":
		return 7
	default:
		return 8
	}
}

func yamlQuote(value string) string {
	if value == "" {
		return `""`
	}
	if needsYAMLQuotes(value) {
		return fmt.Sprintf("%q", value)
	}
	return value
}

func needsYAMLQuotes(value string) bool {
	for _, r := range value {
		if unicode.IsSpace(r) || strings.ContainsRune(`:[]{},&*#?|-<>=!%@\`, r) {
			return true
		}
	}
	return false
}

func buildGTDCommitmentTemplate(commitment braingtd.Commitment) string {
	heading := strings.TrimSpace(commitment.Outcome)
	if heading == "" {
		heading = strings.TrimSpace(commitment.Title)
	}
	if heading == "" {
		heading = "Commitment"
	}
	heading = strings.ReplaceAll(strings.ReplaceAll(heading, "\r", " "), "\n", " ")
	return strings.TrimSpace(fmt.Sprintf(`---
---
# %s

## Summary

## Next Action

## Evidence

## Linked Items

## Review Notes
`, heading))
}

func writeGTDCommitmentFrontMatter(note *brain.MarkdownNote, commitment braingtd.Commitment) error {
	for key, value := range map[string]interface{}{
		"kind":             commitment.Kind,
		"title":            commitment.Title,
		"sphere":           commitment.Sphere,
		"status":           commitment.Status,
		"outcome":          commitment.Outcome,
		"next_action":      commitment.NextAction,
		"context":          commitment.Context,
		"follow_up":        commitment.FollowUp,
		"due":              commitment.Due,
		"actor":            commitment.Actor,
		"waiting_for":      commitment.WaitingFor,
		"project":          commitment.Project,
		"last_evidence_at": commitment.LastEvidenceAt,
		"review_state":     commitment.ReviewState,
		"people":           commitment.People,
		"labels":           commitment.Labels,
	} {
		if err := note.SetFrontMatterField(key, value); err != nil {
			return err
		}
	}
	return nil
}

func formatMarkdownDiagnostics(diags []brain.MarkdownDiagnostic) string {
	parts := make([]string, 0, len(diags))
	for _, diag := range diags {
		if diag.Line > 0 {
			parts = append(parts, fmt.Sprintf("line %d: %s", diag.Line, diag.Message))
			continue
		}
		parts = append(parts, diag.Message)
	}
	return strings.Join(parts, "; ")
}
