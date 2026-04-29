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
	if zero.Participant != "" || !zero.Start.IsZero() || !zero.End.IsZero() || zero.Status != "" {
		t.Fatalf("zero FreeBusySlot has populated fields: %+v", zero)
	}
}

func TestFreeBusySlotJSONRoundTrip(t *testing.T) {
	start := time.Date(2026, time.April, 23, 9, 0, 0, 0, time.UTC)
	in := FreeBusySlot{Participant: "alice@example.com", Start: start, End: start.Add(30 * time.Minute), Status: "busy"}
	got := roundTripJSON(t, in)
	if got.Participant != in.Participant || !got.Start.Equal(in.Start) || !got.End.Equal(in.End) || got.Status != in.Status {
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
	if zero.ID != "" || zero.Name != "" || zero.Primary || zero.Description != "" ||
		zero.Color != "" || zero.Order != 0 || zero.ParentID != nil || zero.IsShared ||
		zero.IsFavorite || zero.IsInboxProject || zero.IsTeamInbox || zero.ViewStyle != "" ||
		zero.ProviderURL != "" {
		t.Fatalf("zero TaskList has populated fields: %+v", zero)
	}
}

func TestTaskListJSONRoundTrip(t *testing.T) {
	parentID := "tl-parent"
	in := TaskList{
		ID:             "tl-1",
		Name:           "Inbox",
		Primary:        true,
		Description:    "Project inbox",
		Color:          "berry",
		Order:          7,
		ParentID:       &parentID,
		IsShared:       true,
		IsFavorite:     true,
		IsInboxProject: true,
		IsTeamInbox:    true,
		ViewStyle:      "board",
		ProviderURL:    "https://todoist.com/app/project/tl-1",
	}
	got := roundTripJSON(t, in)
	if !reflect.DeepEqual(in, got) {
		t.Fatalf("round trip mismatch: in=%+v got=%+v", in, got)
	}
}

func TestTaskItemZeroValue(t *testing.T) {
	var zero TaskItem
	if zero.ID != "" || zero.ListID != "" || zero.Title != "" || zero.Notes != "" ||
		zero.Description != "" || zero.ProjectID != "" || zero.SectionID != "" ||
		zero.ParentID != "" || zero.AssigneeID != "" || zero.AssignerID != "" ||
		zero.AssigneeName != "" || zero.Completed || zero.Priority != "" ||
		zero.ProviderRef != "" || zero.ProviderURL != "" {
		t.Fatalf("zero TaskItem has populated scalars: %+v", zero)
	}
	if len(zero.Labels) != 0 || len(zero.Comments) != 0 {
		t.Fatalf("zero TaskItem has populated slices: %+v", zero)
	}
	if zero.StartAt != nil || zero.EndAt != nil || zero.Due != nil || zero.CompletedAt != nil {
		t.Fatalf("zero TaskItem has populated pointers: %+v", zero)
	}
}

func TestTaskItemJSONRoundTrip(t *testing.T) {
	start := time.Date(2026, time.April, 29, 9, 0, 0, 0, time.UTC)
	end := start.Add(30 * time.Minute)
	due := time.Date(2026, time.April, 30, 17, 0, 0, 0, time.UTC)
	completed := due.Add(-time.Hour)
	postedAt := time.Date(2026, time.April, 29, 10, 0, 0, 0, time.UTC)
	in := TaskItem{
		ID:           "t-1",
		ListID:       "tl-1",
		Title:        "ship release",
		Notes:        "cut the tag and push",
		Description:  "cut the tag and push",
		ProjectID:    "proj-1",
		SectionID:    "sec-1",
		ParentID:     "parent-1",
		Labels:       []string{"waiting", "review"},
		AssigneeID:   "user-1",
		AssignerID:   "user-2",
		AssigneeName: "Alice",
		Comments: []TaskComment{{
			ID:        "c-1",
			TaskID:    "t-1",
			ProjectID: "proj-1",
			Content:   "note",
			PostedAt:  postedAt,
			Attachment: &TaskCommentAttachment{
				FileName:     "notes.txt",
				FileType:     "text/plain",
				FileURL:      "https://example.test/notes.txt",
				ResourceType: "file",
			},
		}},
		StartAt:     &start,
		EndAt:       &end,
		Due:         &due,
		CompletedAt: &completed,
		Completed:   true,
		Priority:    "high",
		ProviderRef: "google:tasks/1",
		ProviderURL: "https://tasks.google.com/task/1",
	}
	got := roundTripJSON(t, in)
	if got.ID != in.ID || got.ListID != in.ListID || got.Title != in.Title ||
		got.Notes != in.Notes || got.Description != in.Description ||
		got.Completed != in.Completed || got.Priority != in.Priority ||
		got.ProviderRef != in.ProviderRef || got.ProviderURL != in.ProviderURL {
		t.Fatalf("scalar mismatch: in=%+v got=%+v", in, got)
	}
	if got.ProjectID != in.ProjectID || got.SectionID != in.SectionID ||
		got.ParentID != in.ParentID || got.AssigneeID != in.AssigneeID ||
		got.AssignerID != in.AssignerID || got.AssigneeName != in.AssigneeName {
		t.Fatalf("metadata mismatch: in=%+v got=%+v", in, got)
	}
	if !reflect.DeepEqual(got.Labels, in.Labels) || !reflect.DeepEqual(got.Comments, in.Comments) {
		t.Fatalf("slice mismatch: in=%+v got=%+v", in, got)
	}
	if got.Due == nil || !got.Due.Equal(due) {
		t.Fatalf("Due mismatch: %+v", got.Due)
	}
	if got.StartAt == nil || !got.StartAt.Equal(start) {
		t.Fatalf("StartAt mismatch: %+v", got.StartAt)
	}
	if got.EndAt == nil || !got.EndAt.Equal(end) {
		t.Fatalf("EndAt mismatch: %+v", got.EndAt)
	}
	if got.CompletedAt == nil || !got.CompletedAt.Equal(completed) {
		t.Fatalf("CompletedAt mismatch: %+v", got.CompletedAt)
	}
}

func TestTaskCommentZeroValue(t *testing.T) {
	var zero TaskComment
	if zero.ID != "" || zero.TaskID != "" || zero.ProjectID != "" || zero.Content != "" || !zero.PostedAt.IsZero() {
		t.Fatalf("zero TaskComment has populated fields: %+v", zero)
	}
	if zero.Attachment != nil {
		t.Fatalf("zero TaskComment has populated attachment: %+v", zero.Attachment)
	}
}

func TestTaskCommentJSONRoundTrip(t *testing.T) {
	postedAt := time.Date(2026, time.May, 1, 12, 0, 0, 0, time.UTC)
	in := TaskComment{
		ID:        "c-1",
		TaskID:    "t-1",
		ProjectID: "proj-1",
		Content:   "follow up",
		PostedAt:  postedAt,
		Attachment: &TaskCommentAttachment{
			FileName:     "notes.txt",
			FileType:     "text/plain",
			FileURL:      "https://example.test/notes.txt",
			ResourceType: "file",
		},
	}
	got := roundTripJSON(t, in)
	if got.ID != in.ID || got.TaskID != in.TaskID || got.ProjectID != in.ProjectID ||
		got.Content != in.Content || !got.PostedAt.Equal(in.PostedAt) ||
		!reflect.DeepEqual(got.Attachment, in.Attachment) {
		t.Fatalf("round trip mismatch: in=%+v got=%+v", in, got)
	}
}

func TestSourceItemZeroValue(t *testing.T) {
	var zero SourceItem
	if zero.Provider != "" || zero.Kind != "" || zero.Container != "" || zero.Number != 0 ||
		zero.Title != "" || zero.URL != "" || zero.State != "" || zero.Author != "" ||
		zero.ReviewStatus != "" || zero.SourceRef != "" {
		t.Fatalf("zero SourceItem has populated fields: %+v", zero)
	}
	if len(zero.Labels) != 0 || len(zero.Assignees) != 0 || len(zero.Reviewers) != 0 {
		t.Fatalf("zero SourceItem has populated slices: %+v", zero)
	}
	if zero.UpdatedAt != nil || zero.ClosedAt != nil {
		t.Fatalf("zero SourceItem has populated timestamps: %+v", zero)
	}
}

func TestSourceItemJSONRoundTrip(t *testing.T) {
	updated := time.Date(2026, time.April, 29, 11, 30, 0, 0, time.UTC)
	closed := time.Date(2026, time.April, 29, 12, 0, 0, 0, time.UTC)
	in := SourceItem{
		Provider:     "github",
		Kind:         "pull_request",
		Container:    "sloppy-org/slopshell",
		Number:       51,
		Title:        "Add GitHub and GitLab source adapters",
		URL:          "https://github.com/sloppy-org/slopshell/pull/51",
		State:        "open",
		Labels:       []string{"gtd", "review"},
		Assignees:    []string{"ada"},
		Author:       "grace",
		ReviewStatus: "review_requested",
		Reviewers:    []string{"octocat"},
		SourceRef:    "github:sloppy-org/slopshell#51",
		UpdatedAt:    &updated,
		ClosedAt:     &closed,
	}
	got := roundTripJSON(t, in)
	if got.Provider != in.Provider || got.Kind != in.Kind || got.Container != in.Container ||
		got.Number != in.Number || got.Title != in.Title || got.URL != in.URL ||
		got.State != in.State || got.Author != in.Author || got.ReviewStatus != in.ReviewStatus ||
		got.SourceRef != in.SourceRef {
		t.Fatalf("scalar mismatch: in=%+v got=%+v", in, got)
	}
	if !reflect.DeepEqual(got.Labels, in.Labels) || !reflect.DeepEqual(got.Assignees, in.Assignees) || !reflect.DeepEqual(got.Reviewers, in.Reviewers) {
		t.Fatalf("slice mismatch: in=%+v got=%+v", in, got)
	}
	if got.UpdatedAt == nil || !got.UpdatedAt.Equal(updated) {
		t.Fatalf("UpdatedAt mismatch: %+v", got.UpdatedAt)
	}
	if got.ClosedAt == nil || !got.ClosedAt.Equal(closed) {
		t.Fatalf("ClosedAt mismatch: %+v", got.ClosedAt)
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
