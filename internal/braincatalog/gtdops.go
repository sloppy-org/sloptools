package braincatalog

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
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
	grouped := groupGTDItems(items, query)
	return buildGTDMarkdown("GTD Review Batch: "+strings.TrimSpace(query), sphere, grouped)
}

func ExtractMeetingTasks(src string) []MeetingTask {
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
		"waiting":  {},
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
		{"waiting", "Waiting"},
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
	case "next", "waiting", "deferred", "someday", "done", "closed":
		return strings.ToLower(strings.TrimSpace(status))
	default:
		return "other"
	}
}

func gtdStatusRank(status string) int {
	switch normalizeGTDStatus(status) {
	case "next":
		return 0
	case "waiting":
		return 1
	case "deferred":
		return 2
	case "someday":
		return 3
	case "done":
		return 4
	case "closed":
		return 5
	default:
		return 6
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
