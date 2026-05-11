// Package activity builds a compact, noise-filtered activity digest from
// groupware (calendar events + mail) for the current day. The digest is
// fed into the brain night triage and edit stages so the judge has signal
// about meetings, teaching, and meaningful communications — without
// relying solely on brain git history.
//
// Noise filtering is entirely deterministic (no LLM). Only events and
// mail that pass the filters are included. The output is ≤ 800 tokens of
// plain text.
package activity

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Digest is the compact activity summary for one day.
type Digest struct {
	Date     string        // YYYY-MM-DD
	Meetings []Meeting
	Mail     []MailSignal
}

// Meeting is a kept calendar event.
type Meeting struct {
	Start   time.Time
	End     time.Time
	Summary string
	AllDay  bool
}

// MailSignal is a kept mail item with its signal category.
type MailSignal struct {
	Subject  string
	Sender   string
	Signal   string // "later", "archive", "cc", "inbox"
	Snippet  string
}

// Build pulls today's calendar and mail via sloppy MCP, applies noise
// filters, and returns a compact digest. sphere is "work" or "private".
func Build(sphere string, now time.Time) (*Digest, error) {
	if now.IsZero() {
		now = time.Now()
	}
	date := now.Format("2006-01-02")

	calRaw, err := callSloppy("calendar_events", map[string]interface{}{
		"days":  1,
		"limit": 30,
	})
	if err != nil {
		return nil, fmt.Errorf("activity: calendar_events: %w", err)
	}
	meetings := parseCalendar(calRaw, now)

	mailRaw, err := callSloppy("mail_message_list", map[string]interface{}{
		"sphere":             sphere,
		"days":               1,
		"limit":              30,
		"include_spam_trash": true,
	})
	if err != nil {
		return nil, fmt.Errorf("activity: mail_message_list: %w", err)
	}
	mail := parseMail(mailRaw)

	return &Digest{Date: date, Meetings: meetings, Mail: mail}, nil
}

// Format renders the digest as compact plain text for inclusion in LLM packets.
// Output is ≤ 800 tokens.
func (d *Digest) Format() string {
	if len(d.Meetings) == 0 && len(d.Mail) == 0 {
		return fmt.Sprintf("## Activity digest %s\n(no signal)\n", d.Date)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "## Activity digest %s\n", d.Date)
	if len(d.Meetings) > 0 {
		b.WriteString("### Meetings\n")
		for _, m := range d.Meetings {
			if m.AllDay {
				fmt.Fprintf(&b, "- (all day) %s\n", m.Summary)
			} else {
				fmt.Fprintf(&b, "- %s–%s %s\n",
					m.Start.Format("15:04"), m.End.Format("15:04"), m.Summary)
			}
		}
	}
	if len(d.Mail) > 0 {
		b.WriteString("### Mail signal\n")
		for _, m := range d.Mail {
			label := strings.ToUpper(m.Signal)
			if m.Snippet != "" {
				fmt.Fprintf(&b, "- [%s] %s — %s — %s\n", label, m.Subject, m.Sender, m.Snippet)
			} else {
				fmt.Fprintf(&b, "- [%s] %s — %s\n", label, m.Subject, m.Sender)
			}
		}
	}
	return b.String()
}

// --- calendar filtering ---

// calendarInfraOrganizers is a set of organizer domain/id fragments that
// identify infrastructure calendars (weather, week numbers, NAS, etc.).
var calendarInfraOrganizers = []string{
	"vejnoe.dk",        // weather calendar
	"p#weeknum",        // week number calendar
	"group.v.calendar", // Google utility groups
	"import.calendar",  // ICS import calendars (usually TUGonline room bookings — kept separately below)
}

// calendarSummaryNoise matches summaries that are clearly infrastructure.
var calendarSummaryNoise = regexp.MustCompile(
	`(?i)(Week \d+ of |°C|°F|backup|NAS|DiskStation|toaster)`,
)

// meetingKeywords keeps all-day events whose summary contains known-useful terms.
// Note: birthday reminders are excluded — they are calendar noise, not meetings.
var meetingKeywords = regexp.MustCompile(
	`(?i)(JF|Seminar|Orga|COMPUTOR|Retreat|Kickoff|Defense|Viva|Meeting|Workshop|Conference|Symposium|Colloquium)`,
)

// courseCodePattern detects TUGonline course codes (e.g. PHT.530UF).
var courseCodePattern = regexp.MustCompile(`[A-Z]{2,4}\.\d{3}[A-Z]{0,2}`)

func parseCalendar(raw string, now time.Time) []Meeting {
	var resp struct {
		Events []struct {
			Summary   string `json:"summary"`
			Start     string `json:"start"`
			End       string `json:"end"`
			AllDay    bool   `json:"all_day"`
			Organizer string `json:"organizer"`
			Attendees []struct {
				Email string `json:"email"`
			} `json:"attendees"`
		} `json:"events"`
	}
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		return nil
	}

	var kept []Meeting
	for _, ev := range resp.Events {
		if shouldDropCalendar(ev.AllDay, ev.Organizer, ev.Summary) {
			continue
		}
		start, _ := time.Parse(time.RFC3339, ev.Start)
		end, _ := time.Parse(time.RFC3339, ev.End)
		kept = append(kept, Meeting{
			Start:   start,
			End:     end,
			Summary: ev.Summary,
			AllDay:  ev.AllDay,
		})
	}
	return kept
}

func shouldDropCalendar(allDay bool, organizer, summary string) bool {
	// Always drop pure noise summaries regardless of all-day status.
	if calendarSummaryNoise.MatchString(summary) {
		return true
	}
	if !allDay {
		return false // time-boxed events are always kept
	}
	// All-day events: keep if they match meaningful keywords or course codes.
	if meetingKeywords.MatchString(summary) || courseCodePattern.MatchString(summary) {
		return false
	}
	// All-day events from infra organizers → drop.
	for _, frag := range calendarInfraOrganizers {
		if strings.Contains(organizer, frag) {
			return true
		}
	}
	// All-day events with no useful keywords → drop.
	return true
}

// --- mail filtering ---

// spamLabelSet is the set of mail labels that unconditionally mean noise.
var spamLabelSet = map[string]bool{
	"Junk-E-Mail":       true,
	"Blocked":           true,
	"Spam":              true,
	"SPAM":              true,
	"Gelöschte Elemente": true,
	"Trash":             true,
	"TRASH":             true,
	"SPAM_TRASH":        true,
}

// spamSubjectPatterns matches subject prefixes/patterns that are always noise.
var spamSubjectPatterns = regexp.MustCompile(
	`(?i)(\[SUSPICIOUS MESSAGE\]|\[ SPAM\? \]|Call for Papers|Invitation to Publish|Keynote Speaker|Cut your manuscript|Author Services)`,
)

// signalLabels maps label names to signal categories.
var signalLabels = map[string]string{
	"Later":   "later",
	"Archive": "archive",
	"CC":      "cc",
	"INBOX":   "inbox",
	"Inbox":   "inbox",
}

func parseMail(raw string) []MailSignal {
	var resp struct {
		Messages []struct {
			Subject string   `json:"subject"`
			Sender  string   `json:"sender"`
			Snippet string   `json:"snippet"`
			Labels  []string `json:"labels"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		return nil
	}

	var kept []MailSignal
	for _, msg := range resp.Messages {
		signal, ok := mailSignal(msg.Labels, msg.Subject)
		if !ok {
			continue
		}
		snippet := strings.TrimSpace(msg.Snippet)
		if len(snippet) > 120 {
			snippet = snippet[:117] + "..."
		}
		kept = append(kept, MailSignal{
			Subject: msg.Subject,
			Sender:  cleanSender(msg.Sender),
			Signal:  signal,
			Snippet: snippet,
		})
	}
	return kept
}

func mailSignal(labels []string, subject string) (string, bool) {
	// Drop if any label is a spam label.
	for _, l := range labels {
		if spamLabelSet[l] {
			return "", false
		}
	}
	// Drop if subject matches noise patterns.
	if spamSubjectPatterns.MatchString(subject) {
		return "", false
	}
	// Assign signal category from labels (highest priority wins).
	for _, l := range labels {
		if cat, ok := signalLabels[l]; ok {
			return cat, true
		}
	}
	// No known signal label — drop (don't include generic unread mail).
	return "", false
}

func cleanSender(s string) string {
	// Extract display name from "Name <email>" format.
	if idx := strings.Index(s, "<"); idx > 0 {
		return strings.TrimSpace(s[:idx])
	}
	return s
}

// --- sloppy MCP call ---

// callSloppy calls a sloppy MCP tool via the sloptools mcp-server subprocess.
// This avoids importing the backend package to keep the activity package lean.
func callSloppy(tool string, args map[string]interface{}) (string, error) {
	realHome, _ := os.UserHomeDir()
	dataDir := filepath.Join(realHome, ".local", "share", "sloppy")

	// Use the JSON-RPC approach: write initialize + tools/call to stdin, read stdout.
	// We use exec directly to avoid circular imports with the backend package.
	cmd := exec.Command("sloptools", "mcp-server",
		"--project-dir", realHome,
		"--data-dir", dataDir)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return "", fmt.Errorf("stdin pipe: %w", err)
	}
	var stdout strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start sloptools: %w", err)
	}

	enc := json.NewEncoder(stdin)

	// Initialize.
	if err := enc.Encode(map[string]interface{}{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]interface{}{},
			"clientInfo":      map[string]interface{}{"name": "sloptools-activity", "version": "0.1"},
		},
	}); err != nil {
		return "", err
	}
	// Call tool.
	if err := enc.Encode(map[string]interface{}{
		"jsonrpc": "2.0", "id": 2, "method": "tools/call",
		"params": map[string]interface{}{"name": tool, "arguments": args},
	}); err != nil {
		return "", err
	}
	stdin.Close()

	if err := cmd.Wait(); err != nil {
		// Non-zero exit is OK if we got JSON output.
		_ = err
	}

	// Parse the stdout: find the response with id=2.
	raw := stdout.String()
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var msg map[string]interface{}
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}
		id, _ := msg["id"].(float64)
		if int(id) != 2 {
			continue
		}
		result, _ := msg["result"].(map[string]interface{})
		if result == nil {
			break
		}
		content, _ := result["content"].([]interface{})
		if len(content) == 0 {
			break
		}
		first, _ := content[0].(map[string]interface{})
		text, _ := first["text"].(string)
		return text, nil
	}
	return "", fmt.Errorf("no response for tool %q", tool)
}
