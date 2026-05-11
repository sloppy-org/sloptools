// Package activity builds a compact, noise-filtered activity digest from
// groupware (calendar events + mail) and brain git history for the time
// window since the previous brain night run.
//
// The window is always [last_sync_until, run_start_time). Both bounds are
// fixed at run start so activity during a long run is never double-counted
// or lost. The window is persisted in a state file so multi-day gaps are
// correctly captured.
//
// Noise filtering is entirely deterministic (no LLM). Multi-day gaps
// trigger compact grouping so the digest stays ≤ 1000 tokens regardless
// of gap size.
package activity

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Window is the bounded time range for one activity digest.
type Window struct {
	Since   time.Time // exclusive lower bound (last run's Until)
	Until   time.Time // inclusive upper bound (this run's start)
	GapDays float64   // Until - Since in days
}

// Digest is the compact activity summary for the window.
type Digest struct {
	Window     Window
	Meetings   []DayMeetings // grouped by date
	Mail       []MailSignal
	GitSummary string // compact git activity summary
}

// DayMeetings groups calendar events by date.
type DayMeetings struct {
	Date   string // YYYY-MM-DD
	Events []Meeting
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
	Date    time.Time
	Subject string
	Sender  string
	Signal  string // "later", "archive", "cc", "inbox"
	Snippet string
}

// --- State management ---

// State is persisted between runs to track the last sync window.
type State struct {
	LastSyncUntil time.Time `json:"last_sync_until"`
	LastRunID     string    `json:"last_run_id,omitempty"`
}

func statePath(sphere string) string {
	xdg := os.Getenv("XDG_CONFIG_HOME")
	if xdg == "" {
		home, _ := os.UserHomeDir()
		xdg = filepath.Join(home, ".config")
	}
	return filepath.Join(xdg, "sloptools", "activity-sync-"+sphere+".json")
}

// LoadState returns the persisted activity sync state. If no state exists,
// returns a state with LastSyncUntil = 48h ago (safe first-run fallback).
func LoadState(sphere string) State {
	body, err := os.ReadFile(statePath(sphere))
	if err != nil {
		return State{LastSyncUntil: time.Now().UTC().Add(-48 * time.Hour)}
	}
	var s State
	if err := json.Unmarshal(body, &s); err != nil {
		return State{LastSyncUntil: time.Now().UTC().Add(-48 * time.Hour)}
	}
	// Never look back more than 7 days to keep packets bounded.
	cutoff := time.Now().UTC().Add(-7 * 24 * time.Hour)
	if s.LastSyncUntil.Before(cutoff) {
		s.LastSyncUntil = cutoff
	}
	return s
}

// SaveState persists the sync state after a successful build.
func SaveState(sphere string, s State) error {
	path := statePath(sphere)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	buf, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, buf, 0o600)
}

// --- Build ---

// Build pulls calendar+mail for the window [since, until) via sloppy MCP,
// applies noise filters, compacts multi-day gaps, and returns a digest.
// It does NOT save state — the caller does that after confirming success.
func Build(sphere string, since, until time.Time) (*Digest, error) {
	if until.IsZero() {
		until = time.Now().UTC()
	}
	if since.IsZero() {
		since = until.Add(-24 * time.Hour)
	}
	gapDays := until.Sub(since).Hours() / 24.0

	w := Window{Since: since, Until: until, GapDays: gapDays}

	// Calendar: query enough days to cover the gap, filter client-side.
	// The sloppy calendar API uses "days forward from now"; we add 1 as buffer.
	// For past events we include them if they fall within [since, until).
	calDays := int(gapDays) + 2
	if calDays < 2 {
		calDays = 2
	}
	if calDays > 8 {
		calDays = 8
	}
	calRaw, err := callSloppy("calendar_events", map[string]interface{}{
		"days":  calDays,
		"limit": 50,
	})
	if err != nil {
		// Non-fatal: digest without calendar is still useful.
		calRaw = ""
	}
	meetings := parseCalendar(calRaw, since, until)

	// Mail: use exact date bounds from the API.
	mailRaw, err := callSloppy("mail_message_list", map[string]interface{}{
		"sphere":             sphere,
		"after":              since.Format(time.RFC3339),
		"before":             until.Format(time.RFC3339),
		"limit":              60, // fetch more, then trim after filtering
		"include_spam_trash": true,
	})
	if err != nil {
		mailRaw = ""
	}
	mail := parseMail(mailRaw, gapDays)

	return &Digest{Window: w, Meetings: meetings, Mail: mail}, nil
}

// BuildWithGit builds the digest and also fetches a compact git activity
// summary from the brain root for the same window.
func BuildWithGit(sphere, brainRoot string, since, until time.Time) (*Digest, error) {
	d, err := Build(sphere, since, until)
	if err != nil {
		return nil, err
	}
	if brainRoot != "" {
		d.GitSummary = compactGitHistory(brainRoot, since, until)
	}
	return d, nil
}

// Format renders the digest as compact plain text for inclusion in LLM packets.
// Output is ≤ 1000 tokens regardless of gap size.
func (d *Digest) Format() string {
	if len(d.Meetings) == 0 && len(d.Mail) == 0 && d.GitSummary == "" {
		return fmt.Sprintf("## Activity digest (%s to %s)\n(no signal)\n",
			d.Window.Since.Format("2006-01-02 15:04"),
			d.Window.Until.Format("2006-01-02 15:04"))
	}

	var b strings.Builder
	fmt.Fprintf(&b, "## Activity digest (%s → %s",
		d.Window.Since.Format("2006-01-02 15:04Z"),
		d.Window.Until.Format("2006-01-02 15:04Z"))
	if d.Window.GapDays >= 1.5 {
		fmt.Fprintf(&b, ", %.0f days", d.Window.GapDays)
	}
	b.WriteString(")\n")

	if len(d.Meetings) > 0 {
		b.WriteString("### Meetings\n")
		multiDay := d.Window.GapDays >= 1.5
		for _, day := range d.Meetings {
			if multiDay {
				fmt.Fprintf(&b, "**%s:**\n", day.Date)
			}
			for _, m := range day.Events {
				if m.AllDay {
					if multiDay {
						fmt.Fprintf(&b, "  - (all day) %s\n", m.Summary)
					} else {
						fmt.Fprintf(&b, "- (all day) %s\n", m.Summary)
					}
				} else {
					if multiDay {
						fmt.Fprintf(&b, "  - %s–%s %s\n",
							m.Start.Format("15:04"), m.End.Format("15:04"), m.Summary)
					} else {
						fmt.Fprintf(&b, "- %s–%s %s\n",
							m.Start.Format("15:04"), m.End.Format("15:04"), m.Summary)
					}
				}
			}
		}
	}

	if len(d.Mail) > 0 {
		b.WriteString("### Mail signal\n")
		// Show date prefix only for multi-day windows.
		multiDay := d.Window.GapDays >= 1.5
		for _, m := range d.Mail {
			label := strings.ToUpper(m.Signal)
			if multiDay {
				fmt.Fprintf(&b, "- %s [%s] %s — %s",
					m.Date.Format("01-02"), label, m.Subject, m.Sender)
			} else {
				fmt.Fprintf(&b, "- [%s] %s — %s", label, m.Subject, m.Sender)
			}
			if m.Snippet != "" {
				fmt.Fprintf(&b, " — %s", m.Snippet)
			}
			b.WriteString("\n")
		}
	}

	if d.GitSummary != "" {
		b.WriteString("### Brain git activity\n")
		b.WriteString(d.GitSummary)
		b.WriteString("\n")
	}

	// Hard cap: truncate at ~1000 tokens (4KB).
	out := b.String()
	if len(out) > 4*1024 {
		out = out[:4*1024-100] + "\n...(digest truncated at 4KB)\n"
	}
	return out
}
