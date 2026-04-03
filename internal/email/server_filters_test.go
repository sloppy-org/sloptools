package email

import (
	"context"
	"testing"

	"github.com/krystophny/sloppy/internal/ews"
	"google.golang.org/api/gmail/v1"
)

func TestGmailFilterToServerFilterMapsArchiveAndLabels(t *testing.T) {
	filter := &gmail.Filter{
		Id: "filter-1",
		Criteria: &gmail.FilterCriteria{
			From:    "boss@example.com",
			Query:   "list:physics.example",
			Subject: "status",
		},
		Action: &gmail.FilterAction{
			AddLabelIds:    []string{"Label_1"},
			RemoveLabelIds: []string{"INBOX", "UNREAD"},
			Forward:        "archive@example.com",
		},
	}
	out := gmailFilterToServerFilter(filter, map[string]string{"Label_1": "lists"})
	if out.ID != "filter-1" {
		t.Fatalf("ID = %q, want filter-1", out.ID)
	}
	if !out.Action.Archive {
		t.Fatal("Archive = false, want true")
	}
	if !out.Action.MarkRead {
		t.Fatal("MarkRead = false, want true")
	}
	if out.Action.MoveTo != "lists" {
		t.Fatalf("MoveTo = %q, want lists", out.Action.MoveTo)
	}
}

func TestExchangeEWSRuleToServerFilterMapsArchiveMove(t *testing.T) {
	provider := &ExchangeEWSMailProvider{
		cfg: ExchangeEWSConfig{ArchiveFolder: "Archive"},
	}
	out := provider.ruleToServerFilter(context.Background(), ews.Rule{
		ID:      "rule-1",
		Name:    "Reference",
		Enabled: true,
		Actions: ews.RuleActions{
			MoveToFolderID: "Archive",
		},
	})
	if out.ID != "rule-1" {
		t.Fatalf("ID = %q, want rule-1", out.ID)
	}
	if out.Action.MoveTo != "Archive" {
		t.Fatalf("MoveTo = %q, want Archive", out.Action.MoveTo)
	}
	if !out.Action.Archive {
		t.Fatal("Archive = false, want true")
	}
}
