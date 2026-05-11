package activity

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var (
	baseTime  = mustParseRFC("2026-05-11T10:00:00+02:00")
	yesterday = mustParseRFC("2026-05-10T10:00:00+02:00")
)

func TestShouldDropCalendar(t *testing.T) {
	cases := []struct {
		allDay    bool
		organizer string
		summary   string
		drop      bool
	}{
		{true, "p#weeknum@group.v.calendar.google.com", "Week 20 of 2026", true},
		{true, "vejnoe.dk", "🌦️ 18°C", true},
		{true, "chr.albert@gmail.com", "Alexander Lassnig's birthday", true},
		{true, "chr.albert@gmail.com", "Plasma Seminar", false},
		{true, "chr.albert@gmail.com", "PhD Defense Markus", false},
		{false, "chr.albert@gmail.com", "Vito JF", false},
		{false, "chr.albert@gmail.com", "PHT.530UF Fusion Reactor Design UE", false},
		{false, "chr.albert@gmail.com", "NAS Backup", true},
	}
	for _, tc := range cases {
		got := shouldDropCalendar(tc.allDay, tc.organizer, tc.summary)
		if got != tc.drop {
			t.Errorf("shouldDropCalendar(allDay=%v, org=%q, sum=%q) = %v, want %v",
				tc.allDay, tc.organizer, tc.summary, got, tc.drop)
		}
	}
}

func TestMailSignal(t *testing.T) {
	cases := []struct {
		labels  []string
		subject string
		want    string
		ok      bool
	}{
		{[]string{"Junk-E-Mail"}, "win a prize", "", false},
		{[]string{"Blocked"}, "invitation to publish", "", false},
		{[]string{"Gelöschte Elemente"}, "Fortran Discourse", "", false},
		{[]string{"Later"}, "Postdoc application", "later", true},
		{[]string{"Archive"}, "IAEA webinar", "archive", true},
		{[]string{"CC"}, "gcc-patches discussion", "cc", true},
		{[]string{"INBOX"}, "Meeting tomorrow", "inbox", true},
		{[]string{"Archive"}, "[SUSPICIOUS MESSAGE] please read", "", false},
		{[]string{"CATEGORY_UPDATES"}, "newsletter", "", false},
	}
	for _, tc := range cases {
		got, ok := mailSignal(tc.labels, tc.subject)
		if ok != tc.ok || got != tc.want {
			t.Errorf("mailSignal(labels=%v, subj=%q) = (%q,%v), want (%q,%v)",
				tc.labels, tc.subject, got, ok, tc.want, tc.ok)
		}
	}
}

func TestParseCalendarWindowFilter(t *testing.T) {
	// Only events within [since, until) should be kept.
	since := mustParseRFC("2026-05-10T00:00:00Z")
	until := mustParseRFC("2026-05-11T00:00:00Z")

	raw := `{"events":[
		{"summary":"Vito JF","start":"2026-05-10T09:00:00+02:00","end":"2026-05-10T09:30:00+02:00","all_day":false,"organizer":"chr.albert@gmail.com"},
		{"summary":"Plasma Seminar","start":"2026-05-11T10:00:00+02:00","end":"2026-05-11T11:00:00+02:00","all_day":false,"organizer":"chr.albert@gmail.com"},
		{"summary":"Week 20 of 2026","start":"2026-05-10T00:00:00Z","end":"2026-05-11T00:00:00Z","all_day":true,"organizer":"p#weeknum@group.v.calendar.google.com"}
	]}`
	days := parseCalendar(raw, since, until)

	// Vito JF is in window (UTC: 07:00 May 10 = within May 10 UTC)
	// Plasma Seminar is outside window (May 11)
	// Week 20 dropped by noise filter
	if len(days) != 1 {
		t.Fatalf("expected 1 day with meetings, got %d: %v", len(days), days)
	}
	if len(days[0].Events) != 1 || days[0].Events[0].Summary != "Vito JF" {
		t.Errorf("expected Vito JF, got: %v", days[0].Events)
	}
}

func TestCompactMail(t *testing.T) {
	msgs := []MailSignal{
		{Subject: "Re: proposal", Sender: "Alice", Signal: "cc", Date: yesterday},
		{Subject: "Re: proposal", Sender: "Alice", Signal: "archive", Date: baseTime}, // higher priority
		{Subject: "Other", Sender: "Bob", Signal: "later", Date: baseTime},
	}
	result := compactMail(msgs)
	// Alice's "Re: proposal" should appear once with "archive" signal.
	aliceCount := 0
	for _, m := range result {
		if m.Sender == "Alice" {
			aliceCount++
			if m.Signal != "archive" {
				t.Errorf("Alice's signal should be 'archive' (higher priority), got %q", m.Signal)
			}
		}
	}
	if aliceCount != 1 {
		t.Errorf("Alice should appear once after dedup, appeared %d times", aliceCount)
	}
}

func TestDigestFormatSingleDay(t *testing.T) {
	d := &Digest{
		Window: Window{
			Since:   yesterday,
			Until:   baseTime,
			GapDays: 1.0,
		},
		Meetings: []DayMeetings{{
			Date: "2026-05-10",
			Events: []Meeting{
				{Start: mustParseRFC("2026-05-10T09:00:00+02:00"), End: mustParseRFC("2026-05-10T09:30:00+02:00"), Summary: "Vito JF"},
			},
		}},
		Mail: []MailSignal{
			{Subject: "Postdoc application", Sender: "Dr. Amir Sohail", Signal: "later", Date: yesterday},
		},
	}
	out := d.Format()
	for _, want := range []string{"Vito JF", "LATER", "Postdoc application"} {
		if !containsStr(out, want) {
			t.Errorf("Format() missing %q\nGot:\n%s", want, out)
		}
	}
	// Single day: no date prefixes on meetings.
	if containsStr(out, "2026-05-10:") {
		t.Error("single-day format should not show date prefix for meetings")
	}
}

func TestDigestFormatMultiDay(t *testing.T) {
	threeDaysAgo := baseTime.Add(-72 * time.Hour)
	d := &Digest{
		Window: Window{
			Since:   threeDaysAgo,
			Until:   baseTime,
			GapDays: 3.0,
		},
		Meetings: []DayMeetings{
			{Date: "2026-05-08", Events: []Meeting{{Summary: "Workshop", Start: threeDaysAgo}}},
			{Date: "2026-05-10", Events: []Meeting{{Summary: "Plasma Seminar", Start: yesterday}}},
		},
		Mail: []MailSignal{
			{Subject: "Report", Sender: "Alice", Signal: "archive", Date: threeDaysAgo},
		},
	}
	out := d.Format()
	// Multi-day: should show date grouping.
	if !containsStr(out, "2026-05-08") {
		t.Errorf("multi-day format should show dates\nGot:\n%s", out)
	}
	if !containsStr(out, "3 days") {
		t.Errorf("multi-day format should mention gap\nGot:\n%s", out)
	}
}

func TestDigestFormatSizeCap(t *testing.T) {
	// Build a very large digest and check it stays under 4KB.
	var mails []MailSignal
	for i := 0; i < 50; i++ {
		mails = append(mails, MailSignal{
			Subject: "Very long subject line that takes up a lot of space in the output buffer indeed",
			Sender:  "sender@example.com",
			Signal:  "archive",
		})
	}
	d := &Digest{
		Window: Window{Since: yesterday, Until: baseTime, GapDays: 1.0},
		Mail:   mails,
	}
	out := d.Format()
	if len(out) > 4*1024+200 {
		t.Errorf("digest too large: %d bytes (want ≤ ~4KB)", len(out))
	}
}

func TestLoadStateFallback(t *testing.T) {
	// With no state file, should return 48h ago fallback.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	s := LoadState("test-sphere")
	if s.LastSyncUntil.IsZero() {
		t.Fatal("expected non-zero fallback time")
	}
	diff := time.Since(s.LastSyncUntil)
	if diff < 47*time.Hour || diff > 49*time.Hour {
		t.Errorf("expected ~48h fallback, got %v", diff)
	}
}

func TestSaveLoadState(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	now := time.Now().UTC().Truncate(time.Second)
	s := State{LastSyncUntil: now, LastRunID: "run123"}
	if err := SaveState("work", s); err != nil {
		t.Fatal(err)
	}
	loaded := LoadState("work")
	if !loaded.LastSyncUntil.Equal(now) {
		t.Errorf("got %v, want %v", loaded.LastSyncUntil, now)
	}
	if loaded.LastRunID != "run123" {
		t.Errorf("got run_id %q", loaded.LastRunID)
	}
}

func TestStatePathEnvOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	p := statePath("private")
	if !containsStr(p, dir) {
		t.Errorf("state path %q does not contain XDG_CONFIG_HOME %q", p, dir)
	}
	// Should be deterministic.
	expected := filepath.Join(dir, "sloptools", "activity-sync-private.json")
	if p != expected {
		t.Errorf("got %q, want %q", p, expected)
	}
}

func TestLoadStateSevenDayCap(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	// Write a state with a very old timestamp.
	veryOld := time.Now().UTC().Add(-30 * 24 * time.Hour)
	s := State{LastSyncUntil: veryOld}
	_ = SaveState("work", s)

	loaded := LoadState("work")
	diff := time.Since(loaded.LastSyncUntil)
	if diff > 8*24*time.Hour {
		t.Errorf("state should be capped to 7 days, got %v old", diff)
	}
}

func TestCompactGitHistorySingleDay(t *testing.T) {
	// Use a temp git repo to test.
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		_ = cmd.Run()
	}
	run("init")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "Test")
	_ = os.WriteFile(filepath.Join(dir, "a.md"), []byte("hi"), 0644)
	run("add", "a.md")
	run("commit", "-m", "brain edit: people/X.md — test")

	since := time.Now().Add(-1 * time.Hour)
	until := time.Now().Add(1 * time.Hour)
	summary := compactGitHistory(dir, since, until)
	if summary == "" {
		t.Error("expected non-empty git summary")
	}
	if !containsStr(summary, "brain edit") {
		t.Errorf("expected commit message in summary, got: %q", summary)
	}
}

func mustParseRFC(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func containsStr(s, sub string) bool {
	return strings.Contains(s, sub)
}
