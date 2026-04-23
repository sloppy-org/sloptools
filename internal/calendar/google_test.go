package calendar

import (
	"context"
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

func TestEventFromGoogleCalendarItemCarriesMetadata(t *testing.T) {
	item := &gcal.Event{
		Id:               "evt-1",
		Summary:          "Standup",
		Description:      "daily",
		Location:         "Zoom",
		Status:           "confirmed",
		ICalUID:          "ics-uid-1",
		RecurringEventId: "rec-1",
		Organizer:        &gcal.EventOrganizer{Email: "alice@example.com"},
		Start:            &gcal.EventDateTime{DateTime: "2026-03-09T09:00:00Z"},
		End:              &gcal.EventDateTime{DateTime: "2026-03-09T09:30:00Z"},
		Attendees: []*gcal.EventAttendee{
			{Email: "bob@example.com", DisplayName: "Bob", ResponseStatus: "accepted"},
		},
		Reminders: &gcal.EventReminders{
			UseDefault: false,
			Overrides:  []*gcal.EventReminder{{Minutes: 15}},
		},
	}
	got := eventFromGoogleCalendarItem(item, "primary")
	if got.ID != "evt-1" {
		t.Fatalf("ID = %q, want evt-1", got.ID)
	}
	if !got.Recurring {
		t.Fatal("Recurring = false, want true")
	}
	if got.ICSUID != "ics-uid-1" {
		t.Fatalf("ICSUID = %q, want ics-uid-1", got.ICSUID)
	}
	if got.ReminderMinutes == nil || *got.ReminderMinutes != 15 {
		t.Fatalf("ReminderMinutes = %v, want 15", got.ReminderMinutes)
	}
	if len(got.Attendees) != 1 || got.Attendees[0].Email != "bob@example.com" || got.Attendees[0].Response != "accepted" {
		t.Fatalf("Attendees = %+v", got.Attendees)
	}
	if got.Organizer != "alice@example.com" {
		t.Fatalf("Organizer = %q", got.Organizer)
	}
}

func TestEventFromGoogleCalendarItemFallsBackToNoTitle(t *testing.T) {
	got := eventFromGoogleCalendarItem(&gcal.Event{Id: "x"}, "primary")
	if got.Summary != "(No title)" {
		t.Fatalf("Summary = %q, want (No title)", got.Summary)
	}
}

func TestNormalizeInviteStatus(t *testing.T) {
	cases := map[string]string{
		"accepted":  "accepted",
		"Accept":    "accepted",
		"yes":       "accepted",
		"declined":  "declined",
		"No":        "declined",
		"tentative": "tentative",
		"maybe":     "tentative",
		"":          "",
		"garbage":   "",
	}
	for in, want := range cases {
		if got := normalizeInviteStatus(in); got != want {
			t.Fatalf("normalizeInviteStatus(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGoogleProviderProviderName(t *testing.T) {
	g := NewGoogleProvider(nil)
	if g.ProviderName() != ProviderNameGoogle {
		t.Fatalf("ProviderName() = %q, want %q", g.ProviderName(), ProviderNameGoogle)
	}
}

func TestGoogleProviderListCalendarsWithoutSession(t *testing.T) {
	g := NewGoogleProvider(nil)
	if _, err := g.ListCalendars(context.Background()); err == nil {
		t.Fatal("ListCalendars() without session returned nil error")
	}
}
