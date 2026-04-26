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
	got, err := s.callTool("calendar_events", map[string]interface{}{"calendar_id": "work", "days": 7})
	if err != nil {
		t.Fatalf("calendar_events failed: %v", err)
	}
	listed, _ := got["events"].([]map[string]interface{})
	if len(listed) != compactListLimit {
		t.Fatalf("default event count = %d, want %d", len(listed), compactListLimit)
	}
}
