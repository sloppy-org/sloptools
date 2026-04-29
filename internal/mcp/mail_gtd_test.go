package mcp

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sloppy-org/sloptools/internal/email"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/store"
)

func TestMailCommitmentListDerivesCommitmentsWithBoundedBodyFetches(t *testing.T) {
	s, account, provider := newMailToolsFixture(t)
	day0 := time.Date(2026, time.April, 20, 9, 0, 0, 0, time.UTC)
	bodyText := "Could you confirm the next step by 2026-04-30?"
	provider.listIDs = []string{"m1", "m2", "m3"}
	provider.pageIDs = []string{"m1", "m2", "m3"}
	provider.messages = map[string]*providerdata.EmailMessage{
		"m1": {
			ID:      "m1",
			Subject: "Could you review the draft by 2026-04-24?",
			Sender:  "ada@example.com",
			Date:    day0,
			Snippet: "Please review the draft.",
			Labels:  []string{"Inbox"},
		},
		"m2": {
			ID:         "m2",
			Subject:    "Please send the recap by 2026-04-25",
			Sender:     "TU Graz",
			Recipients: []string{"bob@example.com"},
			Date:       day0.Add(1 * time.Hour),
			Snippet:    "Follow up on the recap.",
			Labels:     []string{"Sent"},
		},
		"m3": {
			ID:       "m3",
			Subject:  "Quick check",
			Sender:   "Pat <pat@example.com>",
			Date:     day0.Add(2 * time.Hour),
			Snippet:  "Status update",
			BodyText: &bodyText,
			Labels:   []string{"Inbox"},
		},
	}
	s.newEmailProvider = func(context.Context, store.ExternalAccount) (email.EmailProvider, error) {
		return provider, nil
	}

	got, err := s.callTool("mail_commitment_list", map[string]interface{}{"account_id": account.ID, "limit": 3, "body_limit": 1})
	if err != nil {
		t.Fatalf("mail_commitment_list failed: %v", err)
	}
	if got["body_fetch_count"].(int) != 1 {
		t.Fatalf("body_fetch_count = %#v, want 1", got["body_fetch_count"])
	}
	if !equalStringSlices(provider.getMessagesFormats, []string{"metadata", "full"}) {
		t.Fatalf("getMessagesFormats = %#v", provider.getMessagesFormats)
	}

	records, ok := got["commitments"].([]mailCommitmentRecord)
	if !ok {
		t.Fatalf("commitments type = %T", got["commitments"])
	}
	if len(records) != 3 {
		t.Fatalf("commitments len = %d, want 3", len(records))
	}
	byID := map[string]mailCommitmentRecord{}
	for _, record := range records {
		byID[record.SourceID] = record
		if record.Commitment.Kind != "commitment" {
			t.Fatalf("record %s kind = %q", record.SourceID, record.Commitment.Kind)
		}
		if record.Artifact.Kind != store.ArtifactKindEmail {
			t.Fatalf("record %s artifact kind = %q", record.SourceID, record.Artifact.Kind)
		}
		if record.SourceURL == "" || !strings.HasPrefix(record.SourceURL, "sloptools://mail/") {
			t.Fatalf("record %s source_url = %q", record.SourceID, record.SourceURL)
		}
		if record.Artifact.RefURL == nil || !strings.HasPrefix(*record.Artifact.RefURL, "sloptools://mail/") {
			t.Fatalf("record %s artifact ref_url = %#v", record.SourceID, record.Artifact.RefURL)
		}
	}

	m1 := byID["m1"]
	if m1.Commitment.Status != "next" {
		t.Fatalf("m1 status = %q, want next", m1.Commitment.Status)
	}
	if m1.Commitment.FollowUp != "2026-04-24" {
		t.Fatalf("m1 follow_up = %q, want 2026-04-24", m1.Commitment.FollowUp)
	}
	if m1.Commitment.NextAction == "" || !strings.Contains(m1.Commitment.NextAction, "ada@example.com") {
		t.Fatalf("m1 next_action = %q", m1.Commitment.NextAction)
	}
	if !containsAny(m1.Diagnostics, "person stub") {
		t.Fatalf("m1 diagnostics = %#v", m1.Diagnostics)
	}

	m2 := byID["m2"]
	if m2.Commitment.Status != "waiting" {
		t.Fatalf("m2 status = %q, want waiting", m2.Commitment.Status)
	}
	if m2.Commitment.WaitingFor != "bob@example.com" {
		t.Fatalf("m2 waiting_for = %q", m2.Commitment.WaitingFor)
	}
	if m2.Commitment.FollowUp != "2026-04-25" {
		t.Fatalf("m2 follow_up = %q, want 2026-04-25", m2.Commitment.FollowUp)
	}

	m3 := byID["m3"]
	if m3.Commitment.Status != "next" {
		t.Fatalf("m3 status = %q, want next", m3.Commitment.Status)
	}
	if m3.Commitment.FollowUp != "2026-04-30" {
		t.Fatalf("m3 follow_up = %q, want 2026-04-30", m3.Commitment.FollowUp)
	}
	if !m3.BodyFetched {
		t.Fatal("m3 BodyFetched = false, want true")
	}
}

func equalStringSlices(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func containsAny(values []string, needle string) bool {
	for _, value := range values {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}
