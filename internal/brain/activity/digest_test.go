package activity

import (
	"testing"
	"time"
)

func TestShouldDropCalendar(t *testing.T) {
	cases := []struct {
		allDay    bool
		organizer string
		summary   string
		drop      bool
	}{
		// Noise: all-day infrastructure
		{true, "p#weeknum@group.v.calendar.google.com", "Week 20 of 2026", true},
		{true, "l74cfbnvjcckuvpsvmsauei786ti3h4b@import.calendar.google.com", "🌦️ 18°", true},
		{true, "chr.albert@gmail.com", "Alexander Lassnig's birthday", true}, // no keyword match

		// Keep: all-day with keywords
		{true, "chr.albert@gmail.com", "Plasma Seminar", false},
		{true, "chr.albert@gmail.com", "PhD Defense Markus", false},
		{true, "chr.albert@gmail.com", "Workshop Retreat", false},

		// Keep: timed events always kept
		{false, "chr.albert@gmail.com", "Vito JF", false},
		{false, "chr.albert@gmail.com", "PHT.530UF Fusion Reactor Design UE", false},

		// Noise: summary pattern regardless of all-day
		{false, "chr.albert@gmail.com", "NAS Backup 2026-05-11", true},
		{true, "chr.albert@gmail.com", "18°C sunny", true},
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
		// Spam subject even with Archive label
		{[]string{"Archive"}, "[SUSPICIOUS MESSAGE] please read", "", false},
		{[]string{"Archive"}, "[ SPAM? ] offer", "", false},
		// No label → drop
		{[]string{"CATEGORY_UPDATES"}, "newsletter", "", false},
	}
	for _, tc := range cases {
		got, ok := mailSignal(tc.labels, tc.subject)
		if ok != tc.ok || got != tc.want {
			t.Errorf("mailSignal(labels=%v, subj=%q) = (%q, %v), want (%q, %v)",
				tc.labels, tc.subject, got, ok, tc.want, tc.ok)
		}
	}
}

func TestCleanSender(t *testing.T) {
	if got := cleanSender("Dr. Alice <alice@example.com>"); got != "Dr. Alice" {
		t.Errorf("got %q", got)
	}
	if got := cleanSender("alice@example.com"); got != "alice@example.com" {
		t.Errorf("got %q", got)
	}
}

func TestDigestFormat(t *testing.T) {
	d := &Digest{
		Date: "2026-05-11",
		Meetings: []Meeting{
			{Start: mustParse("2026-05-11T09:00:00+02:00"), End: mustParse("2026-05-11T09:30:00+02:00"), Summary: "Vito JF"},
		},
		Mail: []MailSignal{
			{Subject: "Postdoc application", Sender: "Dr. Amir Sohail", Signal: "later"},
		},
	}
	out := d.Format()
	if out == "" {
		t.Fatal("expected non-empty format output")
	}
	for _, want := range []string{"2026-05-11", "Vito JF", "LATER", "Postdoc application"} {
		if !contains(out, want) {
			t.Errorf("Format() missing %q\nGot:\n%s", want, out)
		}
	}
}

func TestDigestFormatEmpty(t *testing.T) {
	d := &Digest{Date: "2026-05-11"}
	out := d.Format()
	if !contains(out, "no signal") {
		t.Errorf("expected 'no signal' in empty digest, got: %s", out)
	}
}

func TestParseCalendarJSON(t *testing.T) {
	raw := `{"events":[
		{"summary":"Vito JF","start":"2026-05-11T09:00:00+02:00","end":"2026-05-11T09:30:00+02:00","all_day":false,"organizer":"chr.albert@gmail.com"},
		{"summary":"Week 20 of 2026","start":"2026-05-11T00:00:00Z","end":"2026-05-12T00:00:00Z","all_day":true,"organizer":"p#weeknum@group.v.calendar.google.com"},
		{"summary":"PHT.530UF Fusion Reactor Design UE","start":"2026-05-11T10:00:00+02:00","end":"2026-05-11T18:00:00+02:00","all_day":false,"organizer":"tugraz"}
	]}`
	meetings := parseCalendar(raw, time.Now())
	if len(meetings) != 2 {
		t.Fatalf("expected 2 meetings (Week 20 dropped), got %d", len(meetings))
	}
}

func mustParse(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
