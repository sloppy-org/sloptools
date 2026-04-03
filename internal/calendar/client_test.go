package calendar

import (
	"testing"
	"time"

	gcal "google.golang.org/api/calendar/v3"
)

func TestParseEventTimeDateTime(t *testing.T) {
	got, allDay := parseEventTime(&gcal.EventDateTime{DateTime: "2026-03-09T10:30:00Z"})
	if allDay {
		t.Fatal("parseEventTime() marked RFC3339 datetime as all-day")
	}
	if got.UTC().Format(time.RFC3339) != "2026-03-09T10:30:00Z" {
		t.Fatalf("parseEventTime() = %s", got.UTC().Format(time.RFC3339))
	}
}

func TestParseEventTimeDateOnly(t *testing.T) {
	got, allDay := parseEventTime(&gcal.EventDateTime{Date: "2026-03-09"})
	if !allDay {
		t.Fatal("parseEventTime() did not mark date-only event as all-day")
	}
	if got.Format("2006-01-02") != "2026-03-09" {
		t.Fatalf("parseEventTime() = %s", got.Format("2006-01-02"))
	}
}
