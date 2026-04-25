package mcp

import (
	"context"
	tabcalendar "github.com/sloppy-org/sloptools/internal/calendar"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/store"
	"path/filepath"
	"testing"
	"time"
)

type stubFreeBusyProvider struct {
	*stubCalendarProvider
	slots []providerdata.FreeBusySlot
}

func (s *stubFreeBusyProvider) QueryFreeBusy(_ context.Context, participants []string, _ tabcalendar.TimeRange) ([]providerdata.FreeBusySlot, error) {
	slots := make([]providerdata.FreeBusySlot, 0, len(s.slots))
	for _, slot := range s.slots {
		found := false
		for _, p := range participants {
			if slot.Participant == p {
				found = true
				break
			}
		}
		if found {
			slots = append(slots, slot)
		}
	}
	return slots, nil
}

type stubNoFreeBusyProvider struct {
	*stubCalendarProvider
}

func TestCalendarFreeBusyReturnsPerParticipantSlots(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "sloppy.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
	})
	if _, err := st.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderGoogleCalendar, "Private", map[string]any{}); err != nil {
		t.Fatalf("CreateExternalAccount(calendar): %v", err)
	}
	start := time.Date(2026, time.April, 23, 9, 0, 0, 0, time.UTC)
	end := start.Add(30 * time.Minute)
	stub := &stubFreeBusyProvider{
		stubCalendarProvider: &stubCalendarProvider{calendars: []providerdata.Calendar{{ID: "primary", Name: "Primary"}}},
		slots: []providerdata.FreeBusySlot{
			{Participant: "alice@example.com", Start: start, End: end, Status: "busy"},
			{Participant: "bob@example.com", Start: start.Add(time.Hour), End: start.Add(2 * time.Hour), Status: "busy"},
		},
	}
	s := NewServerWithStore(t.TempDir(), st)
	s.newCalendarProvider = func(context.Context, store.ExternalAccount) (tabcalendar.Provider, error) {
		return stub, nil
	}
	got, err := s.callTool("calendar_freebusy", map[string]interface{}{
		"participants": []string{"alice@example.com", "bob@example.com"},
		"start":        start.Format(time.RFC3339),
		"end":          end.Add(2 * time.Hour).Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("calendar_freebusy failed: %v", err)
	}
	slots, ok := got["slots"].([]map[string]interface{})
	if !ok {
		t.Fatalf("slots type = %T, want []map[string]interface{}", got["slots"])
	}
	if len(slots) != 2 {
		t.Fatalf("slot count = %d, want 2", len(slots))
	}
	if strFromAny(slots[0]["participant"]) != "alice@example.com" {
		t.Fatalf("slot[0] participant = %q, want alice@example.com", strFromAny(slots[0]["participant"]))
	}
	if strFromAny(slots[0]["status"]) != "busy" {
		t.Fatalf("slot[0] status = %q, want busy", strFromAny(slots[0]["status"]))
	}
	if strFromAny(slots[1]["participant"]) != "bob@example.com" {
		t.Fatalf("slot[1] participant = %q, want bob@example.com", strFromAny(slots[1]["participant"]))
	}
}

func TestCalendarFreeBusyRejectsEmptyParticipants(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "sloppy.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
	})
	if _, err := st.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderGoogleCalendar, "Private", map[string]any{}); err != nil {
		t.Fatalf("CreateExternalAccount(calendar): %v", err)
	}
	s := NewServerWithStore(t.TempDir(), st)
	s.newCalendarProvider = func(context.Context, store.ExternalAccount) (tabcalendar.Provider, error) {
		return &stubFreeBusyProvider{stubCalendarProvider: &stubCalendarProvider{}}, nil
	}
	_, err = s.callTool("calendar_freebusy", map[string]interface{}{
		"participants": []string{},
		"start":        "2026-04-23T09:00:00Z",
		"end":          "2026-04-23T10:00:00Z",
	})
	if err == nil {
		t.Fatal("calendar_freebusy with empty participants should fail")
	}
}

func TestCalendarFreeBusyCapabilityUnsupported(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "sloppy.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
	})
	if _, err := st.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderGoogleCalendar, "Private", map[string]any{}); err != nil {
		t.Fatalf("CreateExternalAccount(calendar): %v", err)
	}
	s := NewServerWithStore(t.TempDir(), st)
	s.newCalendarProvider = func(context.Context, store.ExternalAccount) (tabcalendar.Provider, error) {
		return &stubNoFreeBusyProvider{&stubCalendarProvider{}}, nil
	}
	_, err = s.callTool("calendar_freebusy", map[string]interface{}{
		"participants": []string{"alice@example.com"},
		"start":        "2026-04-23T09:00:00Z",
		"end":          "2026-04-23T10:00:00Z",
	})
	if err == nil {
		t.Fatal("calendar_freebusy with unsupported provider should fail")
	}
}

func TestCalendarFreeBusyRejectsEndBeforeStart(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "sloppy.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
	})
	if _, err := st.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderGoogleCalendar, "Private", map[string]any{}); err != nil {
		t.Fatalf("CreateExternalAccount(calendar): %v", err)
	}
	s := NewServerWithStore(t.TempDir(), st)
	s.newCalendarProvider = func(context.Context, store.ExternalAccount) (tabcalendar.Provider, error) {
		return &stubFreeBusyProvider{stubCalendarProvider: &stubCalendarProvider{}}, nil
	}
	_, err = s.callTool("calendar_freebusy", map[string]interface{}{
		"participants": []string{"alice@example.com"},
		"start":        "2026-04-23T10:00:00Z",
		"end":          "2026-04-23T09:00:00Z",
	})
	if err == nil {
		t.Fatal("calendar_freebusy with end before start should fail")
	}
}
