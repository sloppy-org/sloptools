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

func TestCalendarEventRespondPassesInviteResponse(t *testing.T) {
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
	stub := &stubInviteResponder{stubCalendarProvider: &stubCalendarProvider{calendars: []providerdata.Calendar{{ID: "work", Name: "Work"}}}}
	s := NewServerWithStore(t.TempDir(), st)
	s.newCalendarProvider = func(context.Context, store.ExternalAccount) (tabcalendar.Provider, error) {
		return stub, nil
	}
	got, err := s.callTool("sloppy_calendar", map[string]interface{}{"action": "event_respond", 
		"event_id": "evt-1", "response": "accepted", "comment": "I'll be there",
	})
	if err != nil {
		t.Fatalf("calendar_event_respond failed: %v", err)
	}
	if !got["responded"].(bool) {
		t.Fatalf("responded = %v, want true", got["responded"])
	}
	if strFromAny(got["status"]) != "accepted" {
		t.Fatalf("status = %q, want accepted", strFromAny(got["status"]))
	}
	if stub.lastEventID != "evt-1" {
		t.Fatalf("lastEventID = %q, want evt-1", stub.lastEventID)
	}
	if stub.lastResp.Status != "accepted" || stub.lastResp.Comment != "I'll be there" {
		t.Fatalf("lastResp = %+v, want Status=accepted Comment=I'll be there", stub.lastResp)
	}
}

func TestCalendarEventRespondCapabilityUnsupported(t *testing.T) {
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
	_, err = s.callTool("sloppy_calendar", map[string]interface{}{"action": "event_respond", 
		"event_id": "evt-1", "response": "accepted",
	})
	if err == nil {
		t.Fatalf("expected error for unsupported capability, got nil")
	}
	if !strings.Contains(err.Error(), "does not support invite responses") {
		t.Fatalf("error = %q, want \"does not support invite responses\"", err.Error())
	}
}

func TestCalendarEventRespondInvalidResponse(t *testing.T) {
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
	stub := &stubInviteResponder{stubCalendarProvider: &stubCalendarProvider{calendars: []providerdata.Calendar{{ID: "work", Name: "Work"}}}}
	s := NewServerWithStore(t.TempDir(), st)
	s.newCalendarProvider = func(context.Context, store.ExternalAccount) (tabcalendar.Provider, error) {
		return stub, nil
	}
	_, err = s.callTool("sloppy_calendar", map[string]interface{}{"action": "event_respond", 
		"event_id": "evt-1", "response": "maybe",
	})
	if err == nil {
		t.Fatalf("expected error for invalid response, got nil")
	}
	if !strings.Contains(err.Error(), "must be one of") {
		t.Fatalf("error = %q, want \"must be one of\"", err.Error())
	}
}

func TestCalendarEventIcsExportUsesCapabilityWhenPresent(t *testing.T) {
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
	stub := &stubICSExporter{stubCalendarProvider: &stubCalendarProvider{calendars: []providerdata.Calendar{{ID: "work", Name: "Work"}}}}
	s := NewServerWithStore(t.TempDir(), st)
	s.newCalendarProvider = func(context.Context, store.ExternalAccount) (tabcalendar.Provider, error) {
		return stub, nil
	}
	got, err := s.callTool("sloppy_calendar", map[string]interface{}{"action": "event_ics_export", "event_id": "evt-1"})
	if err != nil {
		t.Fatalf("calendar_event_ics_export failed: %v", err)
	}
	ics, ok := got["ics"].(string)
	if !ok {
		t.Fatalf("ics not a string: %T", got["ics"])
	}
	if !strings.Contains(ics, "evt-1") {
		t.Fatalf("ics = %q, expected to contain evt-1", ics)
	}
}

func TestCalendarEventIcsExportSyntheticFallback(t *testing.T) {
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
	start := time.Date(2026, time.April, 20, 10, 0, 0, 0, time.UTC)
	stub := &stubCalendarProvider{
		calendars: []providerdata.Calendar{{ID: "work", Name: "Work"}},
		events: map[string][]providerdata.Event{"work": {{
			ID: "evt-1", CalendarID: "work", Summary: "Team Sync",
			Start: start, End: start.Add(time.Hour),
			Attendees: []providerdata.Attendee{{Email: "bob@example.com", Name: "Bob"}},
		}}},
	}
	s := NewServerWithStore(t.TempDir(), st)
	s.newCalendarProvider = func(context.Context, store.ExternalAccount) (tabcalendar.Provider, error) {
		return stub, nil
	}
	got, err := s.callTool("sloppy_calendar", map[string]interface{}{"action": "event_ics_export", "event_id": "evt-1"})
	if err != nil {
		t.Fatalf("calendar_event_ics_export synthetic fallback failed: %v", err)
	}
	ics, ok := got["ics"].(string)
	if !ok {
		t.Fatalf("ics not a string: %T", got["ics"])
	}
	if !strings.HasPrefix(ics, "BEGIN:VCALENDAR") {
		t.Fatalf("ics = %q, expected BEGIN:VCALENDAR prefix", ics)
	}
	if !strings.Contains(ics, "SUMMARY:Team Sync") {
		t.Fatalf("ics = %q, expected SUMMARY:Team Sync", ics)
	}
	if !strings.Contains(ics, "ATTENDEE") {
		t.Fatalf("ics = %q, expected ATTENDEE line", ics)
	}
	if !strings.Contains(ics, "END:VCALENDAR") {
		t.Fatalf("ics = %q, expected END:VCALENDAR suffix", ics)
	}
}

func TestCalendarEventIcsExportMissingEventID(t *testing.T) {
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
	_, err = s.callTool("sloppy_calendar", map[string]interface{}{"action": "event_ics_export"})
	if err == nil {
		t.Fatalf("expected error for missing event_id, got nil")
	}
	if !strings.Contains(err.Error(), "event_id is required") {
		t.Fatalf("error = %q, want \"event_id is required\"", err.Error())
	}
}

func TestBuildICSFromEventTimedEvent(t *testing.T) {
	start := time.Date(2026, time.April, 20, 10, 0, 0, 0, time.UTC)
	end := time.Date(2026, time.April, 20, 11, 0, 0, 0, time.UTC)
	ev := providerdata.Event{
		ID:        "evt-1",
		Summary:   "Team Sync",
		Location:  "Room A",
		Start:     start,
		End:       end,
		Organizer: "alice@example.com",
		Attendees: []providerdata.Attendee{{Email: "bob@example.com", Name: "Bob"}},
		Status:    "confirmed",
	}
	ics, err := buildICSFromEvent(ev, "Work")
	if err != nil {
		t.Fatalf("buildICSFromEvent: %v", err)
	}
	icsStr := string(ics)
	if !strings.HasPrefix(icsStr, "BEGIN:VCALENDAR") {
		t.Fatalf("ics = %q, expected BEGIN:VCALENDAR prefix", icsStr)
	}
	if !strings.Contains(icsStr, "SUMMARY:Team Sync") {
		t.Fatalf("ics = %q, expected SUMMARY:Team Sync", icsStr)
	}
	if !strings.Contains(icsStr, "LOCATION:Room A") {
		t.Fatalf("ics = %q, expected LOCATION:Room A", icsStr)
	}
	if !strings.Contains(icsStr, "DTSTART:") {
		t.Fatalf("ics = %q, expected DTSTART line", icsStr)
	}
	if !strings.Contains(icsStr, "DTEND:") {
		t.Fatalf("ics = %q, expected DTEND line", icsStr)
	}
	if !strings.Contains(icsStr, "ATTENDEE") {
		t.Fatalf("ics = %q, expected ATTENDEE line", icsStr)
	}
	if !strings.Contains(icsStr, "ORGANIZER") {
		t.Fatalf("ics = %q, expected ORGANIZER line", icsStr)
	}
	if !strings.Contains(icsStr, "END:VCALENDAR") {
		t.Fatalf("ics = %q, expected END:VCALENDAR suffix", icsStr)
	}
}

func TestBuildICSFromEventAllDay(t *testing.T) {
	start := time.Date(2026, time.May, 15, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, time.May, 16, 0, 0, 0, 0, time.UTC)
	ev := providerdata.Event{
		ID:      "evt-2",
		Summary: "Holiday",
		Start:   start,
		End:     end,
		AllDay:  true,
	}
	ics, err := buildICSFromEvent(ev, "Personal")
	if err != nil {
		t.Fatalf("buildICSFromEvent: %v", err)
	}
	icsStr := string(ics)
	if !strings.Contains(icsStr, "DTSTART;VALUE=DATE:20260515") {
		t.Fatalf("ics = %q, expected DTSTART;VALUE=DATE:20260515", icsStr)
	}
	if !strings.Contains(icsStr, "DTEND;VALUE=DATE:20260516") {
		t.Fatalf("ics = %q, expected DTEND;VALUE=DATE:20260516", icsStr)
	}
}

func TestBuildICSFromEventWithEscapedChars(t *testing.T) {
	start := time.Date(2026, time.June, 1, 9, 0, 0, 0, time.UTC)
	ev := providerdata.Event{
		ID:          "evt-3",
		Summary:     "Meeting; with comma, and backslash",
		Description: "Note: test\\item",
		Start:       start,
		End:         start.Add(time.Hour),
	}
	ics, err := buildICSFromEvent(ev, "Work")
	if err != nil {
		t.Fatalf("buildICSFromEvent: %v", err)
	}
	icsStr := string(ics)
	if !strings.Contains(icsStr, `SUMMARY:Meeting\; with comma\, and backslash`) {
		t.Fatalf("ics = %q, expected escaped semicolons and commas", icsStr)
	}
	if !strings.Contains(icsStr, "DESCRIPTION:Note: test\\\\item") {
		t.Fatalf("ics = %q, expected escaped backslash", icsStr)
	}
}

func TestCalendarEventRespondMissingFields(t *testing.T) {
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
	stub := &stubInviteResponder{stubCalendarProvider: &stubCalendarProvider{calendars: []providerdata.Calendar{{ID: "work", Name: "Work"}}}}
	s := NewServerWithStore(t.TempDir(), st)
	s.newCalendarProvider = func(context.Context, store.ExternalAccount) (tabcalendar.Provider, error) {
		return stub, nil
	}
	_, err = s.callTool("sloppy_calendar", map[string]interface{}{"action": "event_respond", "event_id": "evt-1"})
	if err == nil {
		t.Fatalf("expected error for missing response, got nil")
	}
	if !strings.Contains(err.Error(), "response is required") {
		t.Fatalf("error = %q, want \"response is required\"", err.Error())
	}
}

func TestICSExporterReturnsError(t *testing.T) {
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
	stub := &stubICSExporter{stubCalendarProvider: &stubCalendarProvider{calendars: []providerdata.Calendar{{ID: "work", Name: "Work"}}}, err: fmt.Errorf("export failed")}
	s := NewServerWithStore(t.TempDir(), st)
	s.newCalendarProvider = func(context.Context, store.ExternalAccount) (tabcalendar.Provider, error) {
		return stub, nil
	}
	_, err = s.callTool("sloppy_calendar", map[string]interface{}{"action": "event_ics_export", "event_id": "evt-1"})
	if err == nil {
		t.Fatalf("expected error from ExportICS, got nil")
	}
	if !strings.Contains(err.Error(), "export failed") {
		t.Fatalf("error = %q, want \"export failed\"", err.Error())
	}
}
