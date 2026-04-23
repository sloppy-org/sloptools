package mcp

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	tabcalendar "github.com/sloppy-org/sloptools/internal/calendar"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/store"
)

type stubCalendarReader struct {
	calendars  []providerdata.Calendar
	events     map[string][]providerdata.Event
	created    providerdata.Event
	lastCreate tabcalendar.CreateEventOptions
}

func (s *stubCalendarReader) ListCalendars(context.Context) ([]providerdata.Calendar, error) {
	return append([]providerdata.Calendar(nil), s.calendars...), nil
}

func (s *stubCalendarReader) GetEvents(_ context.Context, opts tabcalendar.GetEventsOptions) ([]providerdata.Event, error) {
	return append([]providerdata.Event(nil), s.events[opts.CalendarID]...), nil
}

func (s *stubCalendarReader) CreateEvent(_ context.Context, opts tabcalendar.CreateEventOptions) (providerdata.Event, error) {
	s.lastCreate = opts
	created := s.created
	if created.ID == "" {
		attendees := make([]providerdata.Attendee, 0, len(opts.Attendees))
		for _, email := range opts.Attendees {
			attendees = append(attendees, providerdata.Attendee{Email: email, Response: "needsAction"})
		}
		created = providerdata.Event{
			ID:          "evt-created",
			CalendarID:  opts.CalendarID,
			Summary:     opts.Summary,
			Description: opts.Description,
			Location:    opts.Location,
			Start:       opts.Start,
			End:         opts.End,
			AllDay:      opts.AllDay,
			Attendees:   attendees,
			Status:      "confirmed",
		}
	}
	return created, nil
}

func TestCalendarListUsesGmailFallback(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "sloppy.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := st.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderGmail, "Gmail", map[string]any{}); err != nil {
		t.Fatalf("CreateExternalAccount(gmail): %v", err)
	}

	s := NewServerWithStore(t.TempDir(), st)
	s.newGoogleCalendarReader = func(context.Context) (googleCalendarReader, error) {
		return &stubCalendarReader{
			calendars: []providerdata.Calendar{{ID: "primary", Name: "Primary", Primary: true}},
		}, nil
	}
	got, err := s.callTool("calendar_list", map[string]interface{}{})
	if err != nil {
		t.Fatalf("calendar_list failed: %v", err)
	}
	calendars, _ := got["calendars"].([]map[string]interface{})
	if len(calendars) != 1 {
		t.Fatalf("calendar count = %d, want 1", len(calendars))
	}
	if strFromAny(calendars[0]["sphere"]) != store.SpherePrivate {
		t.Fatalf("sphere = %q, want %q", strFromAny(calendars[0]["sphere"]), store.SpherePrivate)
	}
}

func TestCalendarEventsReturnsStructuredEvents(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "sloppy.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderGoogleCalendar, "Work", map[string]any{}); err != nil {
		t.Fatalf("CreateExternalAccount(calendar): %v", err)
	}

	start := time.Date(2026, time.March, 16, 9, 0, 0, 0, time.UTC)
	s := NewServerWithStore(t.TempDir(), st)
	s.newGoogleCalendarReader = func(context.Context) (googleCalendarReader, error) {
		return &stubCalendarReader{
			calendars: []providerdata.Calendar{{ID: "work", Name: "Work"}},
			events: map[string][]providerdata.Event{
				"work": {{
					ID:         "evt-1",
					CalendarID: "work",
					Summary:    "Standup",
					Start:      start,
					End:        start.Add(time.Hour),
					Organizer:  "alice@example.com",
				}},
			},
		}, nil
	}
	got, err := s.callTool("calendar_events", map[string]interface{}{
		"calendar_id": "work",
		"days":        7,
		"limit":       10,
	})
	if err != nil {
		t.Fatalf("calendar_events failed: %v", err)
	}
	events, _ := got["events"].([]map[string]interface{})
	if len(events) != 1 {
		t.Fatalf("event count = %d, want 1", len(events))
	}
	if strFromAny(events[0]["summary"]) != "Standup" {
		t.Fatalf("summary = %q, want Standup", strFromAny(events[0]["summary"]))
	}
	if strFromAny(events[0]["provider"]) != store.ExternalProviderGoogleCalendar {
		t.Fatalf("provider = %q, want %q", strFromAny(events[0]["provider"]), store.ExternalProviderGoogleCalendar)
	}
	if strFromAny(events[0]["calendar_name"]) != "Work" {
		t.Fatalf("calendar_name = %q, want Work", strFromAny(events[0]["calendar_name"]))
	}
}

func TestCalendarEventCreateUsesPreferredSphereCalendar(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "sloppy.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderGoogleCalendar, "Work Calendar", map[string]any{}); err != nil {
		t.Fatalf("CreateExternalAccount(work): %v", err)
	}
	if _, err := st.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderGoogleCalendar, "Family", map[string]any{}); err != nil {
		t.Fatalf("CreateExternalAccount(private): %v", err)
	}

	reader := &stubCalendarReader{
		calendars: []providerdata.Calendar{
			{ID: "work", Name: "Work Calendar"},
			{ID: "family", Name: "Family"},
		},
	}
	s := NewServerWithStore(t.TempDir(), st)
	s.newGoogleCalendarReader = func(context.Context) (googleCalendarReader, error) {
		return reader, nil
	}

	got, err := s.callTool("calendar_event_create", map[string]interface{}{
		"sphere":           store.SpherePrivate,
		"summary":          "Masterprüfung David Obermeier",
		"start":            "2026-04-20T16:00:00+02:00",
		"duration_minutes": 60,
	})
	if err != nil {
		t.Fatalf("calendar_event_create failed: %v", err)
	}
	event, _ := got["event"].(map[string]interface{})
	if strFromAny(event["calendar_id"]) != "family" {
		t.Fatalf("calendar_id = %q, want family", strFromAny(event["calendar_id"]))
	}
	if strFromAny(event["summary"]) != "Masterprüfung David Obermeier" {
		t.Fatalf("summary = %q", strFromAny(event["summary"]))
	}
	if reader.lastCreate.CalendarID != "family" {
		t.Fatalf("CreateEvent calendar = %q, want family", reader.lastCreate.CalendarID)
	}
}
