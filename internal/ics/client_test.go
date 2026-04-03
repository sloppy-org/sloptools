package ics

import (
	"testing"
	"time"
)

func TestParseICSEventsFiltersAndSorts(t *testing.T) {
	content := "BEGIN:VCALENDAR\r\n" +
		"BEGIN:VEVENT\r\n" +
		"SUMMARY:Later\r\n" +
		"DTSTART:20260310T120000Z\r\n" +
		"DTEND:20260310T130000Z\r\n" +
		"END:VEVENT\r\n" +
		"BEGIN:VEVENT\r\n" +
		"SUMMARY:Sooner\r\n" +
		"DTSTART:20260309T120000Z\r\n" +
		"DTEND:20260309T130000Z\r\n" +
		"END:VEVENT\r\n" +
		"END:VCALENDAR\r\n"

	events, err := parseICSEvents(content, "demo", mustParseRFC3339(t, "2026-03-09T00:00:00Z"), mustParseRFC3339(t, "2026-03-11T00:00:00Z"))
	if err != nil {
		t.Fatalf("parseICSEvents() error: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want 2", len(events))
	}
	if events[0].Summary != "Sooner" || events[1].Summary != "Later" {
		t.Fatalf("event order = %#v", events)
	}
}

func TestParseICSDateTimeAllDay(t *testing.T) {
	got, allDay := parseICSDateTime("20260309")
	if !allDay {
		t.Fatal("parseICSDateTime() did not mark date-only value as all-day")
	}
	if got.Format("2006-01-02") != "2026-03-09" {
		t.Fatalf("parseICSDateTime() = %s", got.Format("2006-01-02"))
	}
}

func mustParseRFC3339(t *testing.T, raw string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		t.Fatalf("time.Parse(%q) error: %v", raw, err)
	}
	return parsed
}
