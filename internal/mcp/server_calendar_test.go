package mcp

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	tabcalendar "github.com/sloppy-org/sloptools/internal/calendar"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/store"
)

type stubCalendarProvider struct {
	name      string
	calendars []providerdata.Calendar
	events    map[string][]providerdata.Event
	created   providerdata.Event
	lastEvent providerdata.Event
	lastCalID string
}

func (s *stubCalendarProvider) ProviderName() string {
	if s.name == "" {
		return "google_calendar"
	}
	return s.name
}

func (s *stubCalendarProvider) Close() error { return nil }

func (s *stubCalendarProvider) ListCalendars(context.Context) ([]providerdata.Calendar, error) {
	return append([]providerdata.Calendar(nil), s.calendars...), nil
}

func (s *stubCalendarProvider) ListEvents(_ context.Context, calendarID string, _ tabcalendar.TimeRange) ([]providerdata.Event, error) {
	return append([]providerdata.Event(nil), s.events[calendarID]...), nil
}

func (s *stubCalendarProvider) GetEvent(_ context.Context, calendarID, eventID string) (providerdata.Event, error) {
	for _, ev := range s.events[calendarID] {
		if ev.ID == eventID {
			return ev, nil
		}
	}
	return providerdata.Event{}, fmt.Errorf("event %s not found", eventID)
}

func (s *stubCalendarProvider) CreateEvent(_ context.Context, calendarID string, ev providerdata.Event) (providerdata.Event, error) {
	s.lastCalID = calendarID
	s.lastEvent = ev
	created := s.created
	if created.ID == "" {
		created = providerdata.Event{
			ID:          "evt-created",
			CalendarID:  calendarID,
			Summary:     ev.Summary,
			Description: ev.Description,
			Location:    ev.Location,
			Start:       ev.Start,
			End:         ev.End,
			AllDay:      ev.AllDay,
			Attendees:   append([]providerdata.Attendee(nil), ev.Attendees...),
			Status:      "confirmed",
		}
	}
	return created, nil
}

func (s *stubCalendarProvider) UpdateEvent(_ context.Context, calendarID string, ev providerdata.Event) (providerdata.Event, error) {
	ev.CalendarID = calendarID
	return ev, nil
}

func (s *stubCalendarProvider) DeleteEvent(_ context.Context, _, _ string) error { return nil }

var (
	_ tabcalendar.Provider     = (*stubCalendarProvider)(nil)
	_ tabcalendar.EventMutator = (*stubCalendarProvider)(nil)
)

func TestCalendarListUsesGmailFallback(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "sloppy.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := st.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderGmail, "Gmail", map[string]any{}); err != nil {
		t.Fatalf("CreateExternalAccount(gmail): %v", err)
	}

	stub := &stubCalendarProvider{
		calendars: []providerdata.Calendar{{ID: "primary", Name: "Primary", Primary: true}},
	}
	s := NewServerWithStore(t.TempDir(), st)
	s.newCalendarProvider = func(context.Context, store.ExternalAccount) (tabcalendar.Provider, error) {
		return stub, nil
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
	stub := &stubCalendarProvider{
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
	}
	s := NewServerWithStore(t.TempDir(), st)
	s.newCalendarProvider = func(context.Context, store.ExternalAccount) (tabcalendar.Provider, error) {
		return stub, nil
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
	if strFromAny(events[0]["provider"]) != "google_calendar" {
		t.Fatalf("provider = %q, want google_calendar", strFromAny(events[0]["provider"]))
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

	stub := &stubCalendarProvider{
		calendars: []providerdata.Calendar{
			{ID: "work", Name: "Work Calendar"},
			{ID: "family", Name: "Family"},
		},
	}
	s := NewServerWithStore(t.TempDir(), st)
	s.newCalendarProvider = func(context.Context, store.ExternalAccount) (tabcalendar.Provider, error) {
		return stub, nil
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
	if stub.lastCalID != "family" {
		t.Fatalf("CreateEvent calendar = %q, want family", stub.lastCalID)
	}
}

func TestCalendarListRoutesByAccountID(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "sloppy.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	workAcct, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderGoogleCalendar, "Work", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount(work): %v", err)
	}
	privateAcct, err := st.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderGoogleCalendar, "Private", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount(private): %v", err)
	}

	callsByID := make(map[int64]int)
	s := NewServerWithStore(t.TempDir(), st)
	s.newCalendarProvider = func(_ context.Context, account store.ExternalAccount) (tabcalendar.Provider, error) {
		callsByID[account.ID]++
		return &stubCalendarProvider{calendars: []providerdata.Calendar{{ID: fmt.Sprintf("cal-%d", account.ID), Name: account.Label}}}, nil
	}

	if _, err := s.callTool("calendar_list", map[string]interface{}{"account_id": privateAcct.ID}); err != nil {
		t.Fatalf("calendar_list(private) failed: %v", err)
	}
	if callsByID[privateAcct.ID] != 1 || callsByID[workAcct.ID] != 0 {
		t.Fatalf("account_id routing missed: private=%d work=%d", callsByID[privateAcct.ID], callsByID[workAcct.ID])
	}

	callsByID = make(map[int64]int)
	if _, err := s.callTool("calendar_list", map[string]interface{}{}); err != nil {
		t.Fatalf("calendar_list(default) failed: %v", err)
	}
	if callsByID[privateAcct.ID] == 0 || callsByID[workAcct.ID] == 0 {
		t.Fatalf("default calendar_list should visit both accounts, got: private=%d work=%d", callsByID[privateAcct.ID], callsByID[workAcct.ID])
	}
}
