package mcp

import (
	"context"
	"fmt"
	tabcalendar "github.com/sloppy-org/sloptools/internal/calendar"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/store"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// stubICSExporter wraps a stubCalendarProvider and implements tabcalendar.ICSExporter.
type stubICSExporter struct {
	*stubCalendarProvider
	exported []byte
	err      error
}

func (s *stubICSExporter) ExportICS(ctx context.Context, calendarID, eventID string) ([]byte, error) {
	s.exported = []byte("BEGIN:VCALENDAR\r\n" + eventID + "\r\nEND:VCALENDAR\r\n")
	return s.exported, s.err
}

var _ tabcalendar.ICSExporter = (*stubICSExporter)(nil)

// stubInviteResponder wraps a stubCalendarProvider and implements tabcalendar.InviteResponder.
type stubInviteResponder struct {
	*stubCalendarProvider
	lastEventID string
	lastResp    providerdata.InviteResponse
	err         error
}

func (s *stubInviteResponder) RespondToInvite(ctx context.Context, eventID string, resp providerdata.InviteResponse) error {
	s.lastEventID = eventID
	s.lastResp = resp
	return s.err
}

var _ tabcalendar.InviteResponder = (*stubInviteResponder)(nil)

// readOnlyCalendarProvider implements only the core Provider interface.
type readOnlyCalendarProvider struct {
	calendars []providerdata.Calendar
	events    map[string][]providerdata.Event
}

func (r *readOnlyCalendarProvider) ListCalendars(context.Context) ([]providerdata.Calendar, error) {
	return r.calendars, nil
}
func (r *readOnlyCalendarProvider) ListEvents(context.Context, string, tabcalendar.TimeRange) ([]providerdata.Event, error) {
	return nil, tabcalendar.ErrUnsupported
}
func (r *readOnlyCalendarProvider) GetEvent(context.Context, string, string) (providerdata.Event, error) {
	return providerdata.Event{}, tabcalendar.ErrUnsupported
}
func (r *readOnlyCalendarProvider) ProviderName() string { return "readonly" }
func (r *readOnlyCalendarProvider) Close() error         { return nil }

func TestCalendarEventGetReturnsPayload(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "sloppy.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
	})
	if _, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderGoogleCalendar, "Work", map[string]any{}); err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	start := time.Date(2026, time.March, 16, 9, 0, 0, 0, time.UTC)
	stub := &stubCalendarProvider{
		calendars: []providerdata.Calendar{{ID: "work", Name: "Work"}},
		events:    map[string][]providerdata.Event{"work": {{ID: "evt-1", CalendarID: "work", Summary: "Standup", Start: start, End: start.Add(time.Hour), Organizer: "alice@example.com"}}},
	}
	s := NewServerWithStore(t.TempDir(), st)
	s.newCalendarProvider = func(context.Context, store.ExternalAccount) (tabcalendar.Provider, error) {
		return stub, nil
	}
	got, err := s.callTool("sloppy_calendar", map[string]interface{}{"action": "event_get", "event_id": "evt-1", "calendar_id": "work"})
	if err != nil {
		t.Fatalf("calendar_event_get failed: %v", err)
	}
	event, ok := got["event"].(map[string]interface{})
	if !ok {
		t.Fatalf("event not a map: %T", got["event"])
	}
	if strFromAny(event["summary"]) != "Standup" {
		t.Fatalf("summary = %q, want Standup", strFromAny(event["summary"]))
	}
	if strFromAny(event["id"]) != "evt-1" {
		t.Fatalf("id = %q, want evt-1", strFromAny(event["id"]))
	}
	if strFromAny(event["organizer"]) != "alice@example.com" {
		t.Fatalf("organizer = %q, want alice@example.com", strFromAny(event["organizer"]))
	}
}

func TestCalendarEventGetMissingEventID(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "sloppy.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
	})
	if _, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderGoogleCalendar, "Work", map[string]any{}); err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	s := NewServerWithStore(t.TempDir(), st)
	s.newCalendarProvider = func(context.Context, store.ExternalAccount) (tabcalendar.Provider, error) {
		return &stubCalendarProvider{}, nil
	}
	_, err = s.callTool("sloppy_calendar", map[string]interface{}{"action": "event_get"})
	if err == nil {
		t.Fatalf("expected error for missing event_id, got nil")
	}
	if !strings.Contains(err.Error(), "event_id is required") {
		t.Fatalf("error = %q, want \"event_id is required\"", err.Error())
	}
}

func TestCalendarEventUpdatePropagatesChanges(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "sloppy.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
	})
	if _, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderGoogleCalendar, "Work", map[string]any{}); err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	stub := &stubCalendarProvider{
		calendars: []providerdata.Calendar{{ID: "work", Name: "Work"}},
		events:    map[string][]providerdata.Event{"work": {{ID: "evt-1", CalendarID: "work", Summary: "Old Title", Start: time.Now(), End: time.Now().Add(time.Hour)}}},
	}
	s := NewServerWithStore(t.TempDir(), st)
	s.newCalendarProvider = func(context.Context, store.ExternalAccount) (tabcalendar.Provider, error) {
		return stub, nil
	}
	got, err := s.callTool("sloppy_calendar", map[string]interface{}{"action": "event_update",
		"event_id": "evt-1", "summary": "Updated Title", "start": "2026-04-20T10:00:00Z", "duration_minutes": 30,
	})
	if err != nil {
		t.Fatalf("calendar_event_update failed: %v", err)
	}
	if !got["updated"].(bool) {
		t.Fatalf("updated = %v, want true", got["updated"])
	}
	event, ok := got["event"].(map[string]interface{})
	if !ok {
		t.Fatalf("event not a map: %T", got["event"])
	}
	if strFromAny(event["summary"]) != "Updated Title" {
		t.Fatalf("summary = %q, want Updated Title", strFromAny(event["summary"]))
	}
	affected := requireSingleAffectedRef(t, got)
	if affected.Domain != "calendar" || affected.Kind != "event" || affected.ID != "evt-1" || affected.ContainerID != "work" || affected.AccountID == 0 {
		t.Fatalf("affected = %#v", affected)
	}
}

func TestCalendarEventUpdateCapabilityUnsupported(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "sloppy.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
	})
	if _, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderGoogleCalendar, "Work", map[string]any{}); err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	readOnly := &readOnlyCalendarProvider{calendars: []providerdata.Calendar{{ID: "work", Name: "Work"}}}
	s := NewServerWithStore(t.TempDir(), st)
	s.newCalendarProvider = func(context.Context, store.ExternalAccount) (tabcalendar.Provider, error) {
		return readOnly, nil
	}
	_, err = s.callTool("sloppy_calendar", map[string]interface{}{"action": "event_update",
		"event_id": "evt-1", "summary": "Updated", "start": "2026-04-20T10:00:00Z",
	})
	if err == nil {
		t.Fatalf("expected error for unsupported capability, got nil")
	}
	if !strings.Contains(err.Error(), "does not support event updates") {
		t.Fatalf("error = %q, want \"does not support event updates\"", err.Error())
	}
}

func TestCalendarEventDeleteReturnsDeletedTrue(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "sloppy.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
	})
	if _, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderGoogleCalendar, "Work", map[string]any{}); err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	stub := &stubCalendarProvider{calendars: []providerdata.Calendar{{ID: "work", Name: "Work"}}}
	s := NewServerWithStore(t.TempDir(), st)
	s.newCalendarProvider = func(context.Context, store.ExternalAccount) (tabcalendar.Provider, error) {
		return stub, nil
	}
	got, err := s.callTool("sloppy_calendar", map[string]interface{}{"action": "event_delete", "event_id": "evt-1"})
	if err != nil {
		t.Fatalf("calendar_event_delete failed: %v", err)
	}
	if !got["deleted"].(bool) {
		t.Fatalf("deleted = %v, want true", got["deleted"])
	}
	if strFromAny(got["id"]) != "evt-1" {
		t.Fatalf("id = %q, want evt-1", strFromAny(got["id"]))
	}
	if strFromAny(got["calendar_id"]) != "work" {
		t.Fatalf("calendar_id = %q, want work", strFromAny(got["calendar_id"]))
	}
	affected := requireSingleAffectedRef(t, got)
	if affected.Domain != "calendar" || affected.Kind != "event" || affected.ID != "evt-1" || affected.ContainerID != "work" || affected.AccountID == 0 {
		t.Fatalf("affected = %#v", affected)
	}
}

func TestCalendarEventDeleteCapabilityUnsupported(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "sloppy.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
	})
	if _, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderGoogleCalendar, "Work", map[string]any{}); err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	readOnly := &readOnlyCalendarProvider{calendars: []providerdata.Calendar{{ID: "work", Name: "Work"}}}
	s := NewServerWithStore(t.TempDir(), st)
	s.newCalendarProvider = func(context.Context, store.ExternalAccount) (tabcalendar.Provider, error) {
		return readOnly, nil
	}
	_, err = s.callTool("sloppy_calendar", map[string]interface{}{"action": "event_delete", "event_id": "evt-1"})
	if err == nil {
		t.Fatalf("expected error for unsupported capability, got nil")
	}
	if !strings.Contains(err.Error(), "does not support event deletion") {
		t.Fatalf("error = %q, want \"does not support event deletion\"", err.Error())
	}
}

func TestCalendarEventGetRoutesByAccountID(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "sloppy.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
	})
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
		calID := fmt.Sprintf("cal-%d", account.ID)
		return &stubCalendarProvider{
			calendars: []providerdata.Calendar{{ID: calID, Name: account.Label}},
			events:    map[string][]providerdata.Event{calID: {{ID: "evt-1", CalendarID: calID, Summary: "Test"}}},
		}, nil
	}
	if _, err := s.callTool("sloppy_calendar", map[string]interface{}{"action": "event_get", "account_id": privateAcct.ID, "event_id": "evt-1", "calendar_id": fmt.Sprintf("cal-%d", privateAcct.ID)}); err != nil {
		t.Fatalf("calendar_event_get(private) failed: %v", err)
	}
	if callsByID[privateAcct.ID] != 1 || callsByID[workAcct.ID] != 0 {
		t.Fatalf("account_id routing missed: private=%d work=%d", callsByID[privateAcct.ID], callsByID[workAcct.ID])
	}
}

func TestCalendarEventUpdateMissingEventID(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "sloppy.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
	})
	if _, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderGoogleCalendar, "Work", map[string]any{}); err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	s := NewServerWithStore(t.TempDir(), st)
	s.newCalendarProvider = func(context.Context, store.ExternalAccount) (tabcalendar.Provider, error) {
		return &stubCalendarProvider{}, nil
	}
	_, err = s.callTool("sloppy_calendar", map[string]interface{}{"action": "event_update",
		"summary": "No ID", "start": "2026-04-20T10:00:00Z",
	})
	if err == nil {
		t.Fatalf("expected error for missing event_id, got nil")
	}
	if !strings.Contains(err.Error(), "event_id is required") {
		t.Fatalf("error = %q, want \"event_id is required\"", err.Error())
	}
}

func TestCalendarEventDeleteMissingEventID(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "sloppy.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
	})
	if _, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderGoogleCalendar, "Work", map[string]any{}); err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	s := NewServerWithStore(t.TempDir(), st)
	s.newCalendarProvider = func(context.Context, store.ExternalAccount) (tabcalendar.Provider, error) {
		return &stubCalendarProvider{}, nil
	}
	_, err = s.callTool("sloppy_calendar", map[string]interface{}{"action": "event_delete"})
	if err == nil {
		t.Fatalf("expected error for missing event_id, got nil")
	}
	if !strings.Contains(err.Error(), "event_id is required") {
		t.Fatalf("error = %q, want \"event_id is required\"", err.Error())
	}
}
func TestCalendarEventsDefaultsToCompactLimit(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "sloppy.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
	})
	if _, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderGoogleCalendar, "Work", map[string]any{}); err != nil {
		t.Fatalf("CreateExternalAccount(calendar): %v", err)
	}
	start := time.Date(2026, time.March, 16, 9, 0, 0, 0, time.UTC)
	events := make([]providerdata.Event, 0, compactListLimit+2)
	for i := 0; i < compactListLimit+2; i++ {
		events = append(events, providerdata.Event{
			ID:         fmt.Sprintf("evt-%02d", i),
			CalendarID: "work",
			Summary:    fmt.Sprintf("Event %02d", i),
			Start:      start.Add(time.Duration(i) * time.Hour),
			End:        start.Add(time.Duration(i+1) * time.Hour),
		})
	}
	stub := &stubCalendarProvider{
		calendars: []providerdata.Calendar{{ID: "work", Name: "Work"}},
		events:    map[string][]providerdata.Event{"work": events},
	}
	s := NewServerWithStore(t.TempDir(), st)
	s.newCalendarProvider = func(context.Context, store.ExternalAccount) (tabcalendar.Provider, error) {
		return stub, nil
	}
	got, err := s.callTool("sloppy_calendar", map[string]interface{}{"action": "events", "calendar_id": "work", "days": 7})
	if err != nil {
		t.Fatalf("calendar_events failed: %v", err)
	}
	listed, _ := got["events"].([]map[string]interface{})
	if len(listed) != compactListLimit {
		t.Fatalf("default event count = %d, want %d", len(listed), compactListLimit)
	}
}
