package email

import (
	"testing"
	"time"

	imap "github.com/emersion/go-imap/v2"
	imapclient "github.com/emersion/go-imap/v2/imapclient"
	"github.com/sloppy-org/sloptools/internal/ews"
)

func TestDecodeExchangeEWSMessagePopulatesFolderAndFollowUp(t *testing.T) {
	due := time.Date(2026, time.May, 12, 9, 0, 0, 0, time.UTC)
	folders := map[string]ews.Folder{
		"folder-inbox":   {ID: "folder-inbox", Name: "Inbox"},
		"folder-archive": {ID: "folder-archive", Name: "Archive"},
		"folder-team":    {ID: "folder-team", Name: "Team"},
	}
	cases := []struct {
		name        string
		message     ews.Message
		wantFolder  string
		wantFollow  *time.Time
		wantFlagged bool
	}{
		{
			name:       "inbox-folder-resolves-to-INBOX",
			message:    ews.Message{ID: "m1", ParentFolderID: "folder-inbox"},
			wantFolder: "INBOX",
		},
		{
			name:       "archive-folder-keeps-display-name",
			message:    ews.Message{ID: "m2", ParentFolderID: "folder-archive"},
			wantFolder: "Archive",
		},
		{
			name:       "team-folder-keeps-display-name",
			message:    ews.Message{ID: "m3", ParentFolderID: "folder-team"},
			wantFolder: "Team",
		},
		{
			name:        "flag-due-date-becomes-followup",
			message:     ews.Message{ID: "m4", ParentFolderID: "folder-inbox", FlagStatus: "Flagged", FlagDueAt: due},
			wantFolder:  "INBOX",
			wantFollow:  &due,
			wantFlagged: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := decodeExchangeEWSMessage(tc.message, folders, "metadata")
			if got.Folder != tc.wantFolder {
				t.Fatalf("Folder = %q, want %q", got.Folder, tc.wantFolder)
			}
			if got.IsFlagged != tc.wantFlagged {
				t.Fatalf("IsFlagged = %v, want %v", got.IsFlagged, tc.wantFlagged)
			}
			switch {
			case tc.wantFollow == nil && got.FollowUpAt != nil:
				t.Fatalf("FollowUpAt = %v, want nil", got.FollowUpAt)
			case tc.wantFollow != nil && got.FollowUpAt == nil:
				t.Fatalf("FollowUpAt = nil, want %v", tc.wantFollow)
			case tc.wantFollow != nil && !got.FollowUpAt.Equal(*tc.wantFollow):
				t.Fatalf("FollowUpAt = %v, want %v", got.FollowUpAt, tc.wantFollow)
			}
		})
	}
}

func TestDecodeExchangeGraphMessagePopulatesFolderAndFollowUp(t *testing.T) {
	folders := []Folder{
		{ID: "folder-inbox", DisplayName: "Inbox", WellKnownName: "inbox"},
		{ID: "folder-archive", DisplayName: "Archive", WellKnownName: "archive"},
		{ID: "folder-team", DisplayName: "Team Discussion"},
	}
	cases := []struct {
		name       string
		message    Message
		wantFolder string
		wantFollow string
	}{
		{
			name:       "wellknown-inbox",
			message:    Message{ID: "m1", ParentFolderID: "folder-inbox"},
			wantFolder: "INBOX",
		},
		{
			name:       "display-name-archive",
			message:    Message{ID: "m2", ParentFolderID: "folder-archive"},
			wantFolder: "Archive",
		},
		{
			name:       "team-display-name",
			message:    Message{ID: "m3", ParentFolderID: "folder-team"},
			wantFolder: "Team Discussion",
		},
		{
			name:       "flag-due-becomes-followup",
			message:    Message{ID: "m4", ParentFolderID: "folder-inbox", Flag: MessageFlag{FlagStatus: "flagged", DueDateTime: &DateTimeValue{DateTime: "2026-05-12T09:00:00.0000000", TimeZone: "UTC"}}},
			wantFolder: "INBOX",
			wantFollow: "2026-05-12T09:00:00Z",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := decodeExchangeEmailMessage(tc.message, folders)
			if got.Folder != tc.wantFolder {
				t.Fatalf("Folder = %q, want %q", got.Folder, tc.wantFolder)
			}
			if tc.wantFollow == "" {
				if got.FollowUpAt != nil {
					t.Fatalf("FollowUpAt = %v, want nil", got.FollowUpAt)
				}
			} else {
				if got.FollowUpAt == nil {
					t.Fatalf("FollowUpAt = nil, want %s", tc.wantFollow)
				}
				if got.FollowUpAt.Format(time.RFC3339) != tc.wantFollow {
					t.Fatalf("FollowUpAt = %s, want %s", got.FollowUpAt.Format(time.RFC3339), tc.wantFollow)
				}
			}
		})
	}
}

func TestParseIMAPMessageSetsFolder(t *testing.T) {
	for _, folder := range []string{"INBOX", "INBOX/RT-08", "Archive"} {
		buf := &imapclient.FetchMessageBuffer{UID: imap.UID(42)}
		got, err := parseIMAPMessage(folder, buf, false)
		if err != nil {
			t.Fatalf("parseIMAPMessage %s: %v", folder, err)
		}
		if got.Folder != folder {
			t.Fatalf("Folder = %q, want %q", got.Folder, folder)
		}
		if got.FollowUpAt != nil {
			t.Fatalf("FollowUpAt = %v, want nil for IMAP", got.FollowUpAt)
		}
	}
}

func TestGmailFolderFromLabels(t *testing.T) {
	cases := []struct {
		name   string
		labels []string
		want   string
	}{
		{name: "inbox", labels: []string{"INBOX", "UNREAD"}, want: "INBOX"},
		{name: "sent", labels: []string{"SENT"}, want: "SENT"},
		{name: "draft", labels: []string{"DRAFT"}, want: "DRAFT"},
		{name: "spam", labels: []string{"SPAM"}, want: "SPAM"},
		{name: "trash", labels: []string{"TRASH"}, want: "TRASH"},
		{name: "archived-no-system", labels: []string{"Label_123"}, want: ""},
		{name: "user-path-label", labels: []string{"INBOX/Teaching"}, want: "INBOX/Teaching"},
		{name: "user-path-without-system", labels: []string{"Folders/Projects/Alpha"}, want: "Folders/Projects/Alpha"},
		{name: "inbox-and-flat-user-label", labels: []string{"Important", "INBOX"}, want: "INBOX"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := gmailFolderFromLabels(tc.labels); got != tc.want {
				t.Fatalf("gmailFolderFromLabels(%v) = %q, want %q", tc.labels, got, tc.want)
			}
		})
	}
}
