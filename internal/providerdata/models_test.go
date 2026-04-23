package providerdata

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

// roundTripJSON serialises a value and decodes it back into a zero value of the same type.
func roundTripJSON[T any](t *testing.T, in T) T {
	t.Helper()
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return out
}

func TestAttendeeZeroValue(t *testing.T) {
	var zero Attendee
	if zero.Email != "" || zero.Name != "" || zero.Response != "" {
		t.Fatalf("zero Attendee has populated fields: %+v", zero)
	}
}

func TestAttendeeJSONRoundTrip(t *testing.T) {
	in := Attendee{Email: "a@b.test", Name: "Alice", Response: "accepted"}
	got := roundTripJSON(t, in)
	if !reflect.DeepEqual(in, got) {
		t.Fatalf("round trip mismatch: in=%+v got=%+v", in, got)
	}
}

func TestInviteResponseZeroValue(t *testing.T) {
	var zero InviteResponse
	if zero.Status != "" || zero.Comment != "" {
		t.Fatalf("zero InviteResponse has populated fields: %+v", zero)
	}
}

func TestInviteResponseJSONRoundTrip(t *testing.T) {
	in := InviteResponse{Status: "tentative", Comment: "might be late"}
	got := roundTripJSON(t, in)
	if !reflect.DeepEqual(in, got) {
		t.Fatalf("round trip mismatch: in=%+v got=%+v", in, got)
	}
}

func TestFreeBusySlotZeroValue(t *testing.T) {
	var zero FreeBusySlot
	if !zero.Start.IsZero() || !zero.End.IsZero() || zero.Status != "" {
		t.Fatalf("zero FreeBusySlot has populated fields: %+v", zero)
	}
}

func TestFreeBusySlotJSONRoundTrip(t *testing.T) {
	start := time.Date(2026, time.April, 23, 9, 0, 0, 0, time.UTC)
	in := FreeBusySlot{Start: start, End: start.Add(30 * time.Minute), Status: "busy"}
	got := roundTripJSON(t, in)
	if !got.Start.Equal(in.Start) || !got.End.Equal(in.End) || got.Status != in.Status {
		t.Fatalf("round trip mismatch: in=%+v got=%+v", in, got)
	}
}

func TestOOFSettingsZeroValue(t *testing.T) {
	var zero OOFSettings
	if zero.Enabled || zero.Scope != "" || zero.InternalReply != "" || zero.ExternalReply != "" {
		t.Fatalf("zero OOFSettings has populated scalars: %+v", zero)
	}
	if zero.StartAt != nil || zero.EndAt != nil {
		t.Fatalf("zero OOFSettings has populated pointers: %+v", zero)
	}
}

func TestOOFSettingsJSONRoundTrip(t *testing.T) {
	start := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(72 * time.Hour)
	in := OOFSettings{
		Enabled:       true,
		Scope:         "external",
		InternalReply: "on leave",
		ExternalReply: "contact the team alias",
		StartAt:       &start,
		EndAt:         &end,
	}
	got := roundTripJSON(t, in)
	if got.Enabled != in.Enabled || got.Scope != in.Scope ||
		got.InternalReply != in.InternalReply || got.ExternalReply != in.ExternalReply {
		t.Fatalf("scalar mismatch: in=%+v got=%+v", in, got)
	}
	if got.StartAt == nil || !got.StartAt.Equal(start) {
		t.Fatalf("StartAt mismatch: %+v", got.StartAt)
	}
	if got.EndAt == nil || !got.EndAt.Equal(end) {
		t.Fatalf("EndAt mismatch: %+v", got.EndAt)
	}
}

func TestTaskListZeroValue(t *testing.T) {
	var zero TaskList
	if zero.ID != "" || zero.Name != "" || zero.Primary {
		t.Fatalf("zero TaskList has populated fields: %+v", zero)
	}
}

func TestTaskListJSONRoundTrip(t *testing.T) {
	in := TaskList{ID: "tl-1", Name: "Inbox", Primary: true}
	got := roundTripJSON(t, in)
	if !reflect.DeepEqual(in, got) {
		t.Fatalf("round trip mismatch: in=%+v got=%+v", in, got)
	}
}

func TestTaskItemZeroValue(t *testing.T) {
	var zero TaskItem
	if zero.ID != "" || zero.ListID != "" || zero.Title != "" || zero.Notes != "" ||
		zero.Completed || zero.Priority != "" || zero.ProviderRef != "" {
		t.Fatalf("zero TaskItem has populated scalars: %+v", zero)
	}
	if zero.Due != nil || zero.CompletedAt != nil {
		t.Fatalf("zero TaskItem has populated pointers: %+v", zero)
	}
}

func TestTaskItemJSONRoundTrip(t *testing.T) {
	due := time.Date(2026, time.April, 30, 17, 0, 0, 0, time.UTC)
	completed := due.Add(-time.Hour)
	in := TaskItem{
		ID:          "t-1",
		ListID:      "tl-1",
		Title:       "ship release",
		Notes:       "cut the tag and push",
		Due:         &due,
		CompletedAt: &completed,
		Completed:   true,
		Priority:    "high",
		ProviderRef: "google:tasks/1",
	}
	got := roundTripJSON(t, in)
	if got.ID != in.ID || got.ListID != in.ListID || got.Title != in.Title ||
		got.Notes != in.Notes || got.Completed != in.Completed ||
		got.Priority != in.Priority || got.ProviderRef != in.ProviderRef {
		t.Fatalf("scalar mismatch: in=%+v got=%+v", in, got)
	}
	if got.Due == nil || !got.Due.Equal(due) {
		t.Fatalf("Due mismatch: %+v", got.Due)
	}
	if got.CompletedAt == nil || !got.CompletedAt.Equal(completed) {
		t.Fatalf("CompletedAt mismatch: %+v", got.CompletedAt)
	}
}

func TestPostalAddressZeroValue(t *testing.T) {
	var zero PostalAddress
	if zero.Type != "" || zero.Street != "" || zero.City != "" ||
		zero.Region != "" || zero.Postal != "" || zero.Country != "" {
		t.Fatalf("zero PostalAddress has populated fields: %+v", zero)
	}
}

func TestPostalAddressJSONRoundTrip(t *testing.T) {
	in := PostalAddress{
		Type:    "home",
		Street:  "Rathausplatz 1",
		City:    "Graz",
		Region:  "Styria",
		Postal:  "8010",
		Country: "AT",
	}
	got := roundTripJSON(t, in)
	if !reflect.DeepEqual(in, got) {
		t.Fatalf("round trip mismatch: in=%+v got=%+v", in, got)
	}
}

func TestPhotoRefZeroValue(t *testing.T) {
	var zero PhotoRef
	if zero.URL != "" || zero.ContentType != "" || len(zero.Bytes) != 0 {
		t.Fatalf("zero PhotoRef has populated fields: %+v", zero)
	}
}

func TestPhotoRefJSONRoundTrip(t *testing.T) {
	in := PhotoRef{
		URL:         "https://example.test/a.png",
		ContentType: "image/png",
		Bytes:       []byte{0x89, 0x50, 0x4e, 0x47},
	}
	got := roundTripJSON(t, in)
	if got.URL != in.URL || got.ContentType != in.ContentType {
		t.Fatalf("scalar mismatch: in=%+v got=%+v", in, got)
	}
	if !reflect.DeepEqual(got.Bytes, in.Bytes) {
		t.Fatalf("bytes mismatch: in=%x got=%x", in.Bytes, got.Bytes)
	}
}

func TestContactZeroValue(t *testing.T) {
	var zero Contact
	if zero.ProviderRef != "" || zero.Name != "" || zero.Email != "" ||
		zero.Organization != "" || zero.Notes != "" {
		t.Fatalf("zero Contact has populated scalars: %+v", zero)
	}
	if len(zero.Phones) != 0 || len(zero.Addresses) != 0 || len(zero.Photos) != 0 {
		t.Fatalf("zero Contact has populated slices: %+v", zero)
	}
	if zero.Birthday != nil {
		t.Fatalf("zero Contact has populated birthday: %+v", zero.Birthday)
	}
}

func TestContactJSONRoundTrip(t *testing.T) {
	birthday := time.Date(1990, time.January, 2, 0, 0, 0, 0, time.UTC)
	in := Contact{
		ProviderRef:  "people/1",
		Name:         "Alice",
		Email:        "alice@example.test",
		Organization: "Contoso",
		Phones:       []string{"+43 1 234"},
		Addresses: []PostalAddress{{
			Type: "home", Street: "Main 1", City: "Vienna", Country: "AT",
		}},
		Birthday: &birthday,
		Notes:    "prefers Signal",
		Photos: []PhotoRef{{
			URL: "https://example.test/a.png", ContentType: "image/png",
		}},
	}
	got := roundTripJSON(t, in)
	if got.ProviderRef != in.ProviderRef || got.Name != in.Name ||
		got.Email != in.Email || got.Organization != in.Organization ||
		got.Notes != in.Notes {
		t.Fatalf("scalar mismatch: in=%+v got=%+v", in, got)
	}
	if !reflect.DeepEqual(got.Phones, in.Phones) ||
		!reflect.DeepEqual(got.Addresses, in.Addresses) ||
		!reflect.DeepEqual(got.Photos, in.Photos) {
		t.Fatalf("slice mismatch: in=%+v got=%+v", in, got)
	}
	if got.Birthday == nil || !got.Birthday.Equal(birthday) {
		t.Fatalf("Birthday mismatch: %+v", got.Birthday)
	}
}

func TestEventZeroValue(t *testing.T) {
	var zero Event
	if zero.ID != "" || zero.Summary != "" || zero.ICSUID != "" {
		t.Fatalf("zero Event has populated scalars: %+v", zero)
	}
	if len(zero.Attendees) != 0 {
		t.Fatalf("zero Event has attendees: %+v", zero.Attendees)
	}
	if zero.ReminderMinutes != nil {
		t.Fatalf("zero Event has reminder: %+v", zero.ReminderMinutes)
	}
}

func TestEventJSONRoundTripWithStructuredAttendees(t *testing.T) {
	start := time.Date(2026, time.April, 23, 14, 0, 0, 0, time.UTC)
	minutes := 15
	in := Event{
		ID:         "evt-1",
		CalendarID: "primary",
		Summary:    "Sync",
		Start:      start,
		End:        start.Add(30 * time.Minute),
		Status:     "confirmed",
		Organizer:  "alice@example.test",
		Attendees: []Attendee{
			{Email: "alice@example.test", Name: "Alice", Response: "accepted"},
			{Email: "bob@example.test", Name: "Bob", Response: "needsAction"},
		},
		ReminderMinutes: &minutes,
		ICSUID:          "abc-123@example.test",
	}
	got := roundTripJSON(t, in)
	if got.ID != in.ID || got.Summary != in.Summary || got.ICSUID != in.ICSUID {
		t.Fatalf("scalar mismatch: in=%+v got=%+v", in, got)
	}
	if !reflect.DeepEqual(got.Attendees, in.Attendees) {
		t.Fatalf("attendees mismatch: in=%+v got=%+v", in.Attendees, got.Attendees)
	}
	if got.ReminderMinutes == nil || *got.ReminderMinutes != minutes {
		t.Fatalf("ReminderMinutes mismatch: %+v", got.ReminderMinutes)
	}
}
