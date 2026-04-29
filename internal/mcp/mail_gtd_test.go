package mcp

import (
	"context"
	"os"
	"path/filepath"
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
	projectConfig, vaultConfig := writeMailCommitmentConfigs(t)
	askBody := "Could you review the draft by 2026-04-24?"
	newsletterBody := "Project newsletter and release notes."
	provider.listIDs = []string{"m1", "m2", "m3", "m4"}
	provider.pageIDs = []string{"m1", "m2", "m3", "m4"}
	provider.messages = map[string]*providerdata.EmailMessage{
		"m1": {
			ID:       "m1",
			Subject:  "Draft",
			Sender:   "Ada Example <ada@example.com>",
			Date:     day0,
			Snippet:  "See below.",
			BodyText: &askBody,
			Labels:   []string{"Inbox"},
		},
		"m2": {
			ID:         "m2",
			Subject:    "Please send the recap by 2026-04-25",
			Sender:     "Work Account <me@example.com>",
			Recipients: []string{"bob@example.com"},
			Date:       day0.Add(1 * time.Hour),
			Snippet:    "Follow up on the recap.",
			Labels:     []string{"Sent"},
		},
		"m3": {
			ID:       "m3",
			Subject:  "Weekly newsletter",
			Sender:   "newsletter@example.com",
			Date:     day0.Add(2 * time.Hour),
			Snippet:  "Project news",
			BodyText: &newsletterBody,
			Labels:   []string{"Inbox"},
		},
		"m4": {
			ID:      "m4",
			Subject: "Auto-reply",
			Sender:  "auto-reply@example.com",
			Date:    day0.Add(3 * time.Hour),
			Snippet: "I am out of office.",
			Labels:  []string{"Inbox"},
		},
	}
	s.newEmailProvider = func(context.Context, store.ExternalAccount) (email.EmailProvider, error) {
		return provider, nil
	}

	got, err := s.callTool("mail_commitment_list", map[string]interface{}{"account_id": account.ID, "limit": 4, "body_limit": 1, "project_config": projectConfig, "vault_config": vaultConfig, "writeable": true})
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
	if len(records) != 2 {
		t.Fatalf("commitments len = %d, want 2", len(records))
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
		if len(record.Commitment.SourceBindings) != 1 || !record.Commitment.SourceBindings[0].Writeable {
			t.Fatalf("record %s source binding = %#v", record.SourceID, record.Commitment.SourceBindings)
		}
	}

	m1 := byID["m1"]
	if m1.Commitment.Status != "next" {
		t.Fatalf("m1 status = %q, want next", m1.Commitment.Status)
	}
	if m1.Commitment.FollowUp != "2026-04-24" {
		t.Fatalf("m1 follow_up = %q, want 2026-04-24", m1.Commitment.FollowUp)
	}
	if m1.Commitment.Project != "[[projects/Paper]]" {
		t.Fatalf("m1 project = %q", m1.Commitment.Project)
	}
	if m1.Commitment.NextAction == "" || !strings.Contains(m1.Commitment.NextAction, "Ada Example") {
		t.Fatalf("m1 next_action = %q", m1.Commitment.NextAction)
	}
	if !containsAny(m1.Diagnostics, "needs_person_note: Ada Example") {
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

	if _, ok := byID["m3"]; ok {
		t.Fatal("newsletter produced a commitment")
	}
	if _, ok := byID["m4"]; ok {
		t.Fatal("auto-reply produced a commitment")
	}
}

func TestMailCommitmentCloseRequiresWriteableAndAppliesMailAction(t *testing.T) {
	s, account, provider := newMailToolsFixture(t)
	provider.messages = map[string]*providerdata.EmailMessage{"m1": {ID: "m1", Subject: "Request", Date: time.Date(2026, time.April, 20, 9, 0, 0, 0, time.UTC)}}
	s.newEmailProvider = func(context.Context, store.ExternalAccount) (email.EmailProvider, error) {
		return provider, nil
	}
	if _, err := s.callTool("mail_commitment_close", map[string]interface{}{"account_id": account.ID, "message_id": "m1"}); err == nil {
		t.Fatal("mail_commitment_close without writeable succeeded")
	}
	got, err := s.callTool("mail_commitment_close", map[string]interface{}{"account_id": account.ID, "message_id": "m1", "writeable": true})
	if err != nil {
		t.Fatalf("mail_commitment_close failed: %v", err)
	}
	if provider.lastAction != "archive" {
		t.Fatalf("lastAction = %q, want archive", provider.lastAction)
	}
	if len(provider.lastIDs) != 1 || provider.lastIDs[0] != "m1" {
		t.Fatalf("lastIDs = %#v", provider.lastIDs)
	}
	if got["closed"] != true {
		t.Fatalf("closed = %#v", got["closed"])
	}
}

func writeMailCommitmentConfigs(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "vault", "brain", "people"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	projectConfig := filepath.Join(root, "projects.toml")
	if err := os.WriteFile(projectConfig, []byte(`
[[project]]
name = "Paper"
keywords = ["draft"]
people = ["Ada Example"]
`), 0o644); err != nil {
		t.Fatalf("WriteFile(projects): %v", err)
	}
	vaultConfig := filepath.Join(root, "vaults.toml")
	if err := os.WriteFile(vaultConfig, []byte(`
[[vault]]
sphere = "work"
root = "`+filepath.ToSlash(filepath.Join(root, "vault"))+`"
brain = "brain"
`), 0o644); err != nil {
		t.Fatalf("WriteFile(vaults): %v", err)
	}
	return projectConfig, vaultConfig
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
