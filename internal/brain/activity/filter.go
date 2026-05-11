package activity

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"time"
)

// --- Calendar filtering ---

var calendarInfraOrganizers = []string{
	"vejnoe.dk",
	"p#weeknum",
	"group.v.calendar",
}

var calendarSummaryNoise = regexp.MustCompile(
	`(?i)(Week \d+ of |°C|°F|backup|NAS|DiskStation|toaster)`,
)

var meetingKeywords = regexp.MustCompile(
	`(?i)(JF|Seminar|Orga|COMPUTOR|Retreat|Kickoff|Defense|Viva|Meeting|Workshop|Conference|Symposium|Colloquium)`,
)

var courseCodePattern = regexp.MustCompile(`[A-Z]{2,4}\.\d{3}[A-Z]{0,2}`)

func parseCalendar(raw string, since, until time.Time) []DayMeetings {
	if raw == "" {
		return nil
	}
	var resp struct {
		Events []struct {
			Summary   string `json:"summary"`
			Start     string `json:"start"`
			End       string `json:"end"`
			AllDay    bool   `json:"all_day"`
			Organizer string `json:"organizer"`
		} `json:"events"`
	}
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		return nil
	}

	byDate := map[string][]Meeting{}
	var dates []string

	for _, ev := range resp.Events {
		if shouldDropCalendar(ev.AllDay, ev.Organizer, ev.Summary) {
			continue
		}
		start, _ := time.Parse(time.RFC3339, ev.Start)
		end, _ := time.Parse(time.RFC3339, ev.End)

		// Filter to window [since, until).
		// For all-day events use the date portion only.
		if !start.IsZero() {
			if start.Before(since) || !start.Before(until) {
				continue
			}
		}

		date := start.Format("2006-01-02")
		if _, seen := byDate[date]; !seen {
			dates = append(dates, date)
		}
		byDate[date] = append(byDate[date], Meeting{
			Start:   start,
			End:     end,
			Summary: ev.Summary,
			AllDay:  ev.AllDay,
		})
	}

	sort.Strings(dates)
	var result []DayMeetings
	for _, d := range dates {
		result = append(result, DayMeetings{Date: d, Events: byDate[d]})
	}
	return result
}

func shouldDropCalendar(allDay bool, organizer, summary string) bool {
	if calendarSummaryNoise.MatchString(summary) {
		return true
	}
	if !allDay {
		return false
	}
	if meetingKeywords.MatchString(summary) || courseCodePattern.MatchString(summary) {
		return false
	}
	for _, frag := range calendarInfraOrganizers {
		if strings.Contains(organizer, frag) {
			return true
		}
	}
	return true
}

// --- Mail filtering ---

var spamLabelSet = map[string]bool{
	"Junk-E-Mail":        true,
	"Blocked":            true,
	"Spam":               true,
	"SPAM":               true,
	"Gelöschte Elemente": true,
	"Trash":              true,
	"TRASH":              true,
	"SPAM_TRASH":         true,
}

var spamSubjectPatterns = regexp.MustCompile(
	`(?i)(\[SUSPICIOUS MESSAGE\]|\[ SPAM\? \]|Call for Papers|Invitation to Publish|Keynote Speaker|Cut your manuscript|Author Services)`,
)

var signalLabels = map[string]string{
	"Later":   "later",
	"Archive": "archive",
	"CC":      "cc",
	"INBOX":   "inbox",
	"Inbox":   "inbox",
}

// signalPriority ranks signal categories for deduplication.
var signalPriority = map[string]int{"later": 3, "archive": 2, "cc": 1, "inbox": 0}

func parseMail(raw string, gapDays float64) []MailSignal {
	if raw == "" {
		return nil
	}
	var resp struct {
		Messages []struct {
			Subject string   `json:"subject"`
			Sender  string   `json:"sender"`
			Snippet string   `json:"snippet"`
			Labels  []string `json:"labels"`
			Date    string   `json:"date"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		return nil
	}

	// Filter to signal items.
	var kept []MailSignal
	for _, msg := range resp.Messages {
		signal, ok := mailSignal(msg.Labels, msg.Subject)
		if !ok {
			continue
		}
		date, _ := time.Parse(time.RFC3339, msg.Date)
		snippet := strings.TrimSpace(msg.Snippet)
		if len(snippet) > 100 {
			snippet = snippet[:97] + "..."
		}
		kept = append(kept, MailSignal{
			Date:    date,
			Subject: msg.Subject,
			Sender:  cleanSender(msg.Sender),
			Signal:  signal,
			Snippet: snippet,
		})
	}

	// For multi-day gaps: compact by deduplicating same-sender same-subject.
	// Keep the highest-priority signal per sender×subject.
	if gapDays >= 1.5 && len(kept) > 20 {
		kept = compactMail(kept)
	}

	// Hard cap: 30 items max.
	if len(kept) > 30 {
		kept = kept[:30]
	}
	return kept
}

// compactMail deduplicates mail by sender×subject, keeping highest priority.
func compactMail(msgs []MailSignal) []MailSignal {
	type key struct{ sender, subject string }
	best := map[key]MailSignal{}
	for _, m := range msgs {
		k := key{m.Sender, m.Subject}
		if prev, ok := best[k]; !ok || signalPriority[m.Signal] > signalPriority[prev.Signal] {
			best[k] = m
		}
	}
	result := make([]MailSignal, 0, len(best))
	for _, m := range best {
		result = append(result, m)
	}
	// Sort: priority desc, then date desc.
	sort.Slice(result, func(i, j int) bool {
		pi, pj := signalPriority[result[i].Signal], signalPriority[result[j].Signal]
		if pi != pj {
			return pi > pj
		}
		return result[i].Date.After(result[j].Date)
	})
	return result
}

func mailSignal(labels []string, subject string) (string, bool) {
	for _, l := range labels {
		if spamLabelSet[l] {
			return "", false
		}
	}
	if spamSubjectPatterns.MatchString(subject) {
		return "", false
	}
	for _, l := range labels {
		if cat, ok := signalLabels[l]; ok {
			return cat, true
		}
	}
	return "", false
}

func cleanSender(s string) string {
	if idx := strings.Index(s, "<"); idx > 0 {
		return strings.TrimSpace(s[:idx])
	}
	return s
}
