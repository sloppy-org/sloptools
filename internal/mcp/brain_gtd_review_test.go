package mcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sloppy-org/sloptools/internal/email"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/store"
	"github.com/sloppy-org/sloptools/internal/tasks"
)

func TestBrainGTDReviewListMailBranchIsLive(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	if err := os.MkdirAll(filepath.Join(tmp, "work", "brain", "projects"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	s, st, _ := newDomainServerForTest(t)
	s.brainConfigPath = configPath
	account, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderExchangeEWS, "Work Mail", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	day0 := time.Date(2026, time.May, 4, 9, 0, 0, 0, time.UTC)
	due := day0.AddDate(0, 0, 7)
	askBody := "Could you send the report by 2026-05-08?"
	provider := &fakeMailProvider{
		listIDs: []string{"unread-inbox", "read-inbox", "deferred-flag", "waiting", "archived"},
		messages: map[string]*providerdata.EmailMessage{
			"unread-inbox":  {ID: "unread-inbox", Subject: "Unread", Sender: "Ada <ada@example.com>", Date: day0, Folder: "INBOX", BodyText: &askBody, Snippet: "ask"},
			"read-inbox":    {ID: "read-inbox", Subject: "Read", Sender: "Ada <ada@example.com>", Date: day0.Add(time.Hour), Folder: "INBOX", IsRead: true, BodyText: &askBody, Snippet: "ask"},
			"deferred-flag": {ID: "deferred-flag", Subject: "Deferred", Sender: "Ada <ada@example.com>", Date: day0.Add(2 * time.Hour), Folder: "INBOX", IsRead: true, IsFlagged: true, FollowUpAt: &due, BodyText: &askBody, Snippet: "ask"},
			"waiting":       {ID: "waiting", Subject: "Waiting", Sender: "Ada <ada@example.com>", Date: day0.Add(3 * time.Hour), Folder: "Waiting", IsRead: true, BodyText: &askBody, Snippet: "ask"},
			"archived":      {ID: "archived", Subject: "Archived", Sender: "Ada <ada@example.com>", Date: day0.Add(4 * time.Hour), Folder: "Archive", IsRead: true, BodyText: &askBody, Snippet: "ask"},
		},
	}
	s.newEmailProvider = func(context.Context, store.ExternalAccount) (email.EmailProvider, error) { return provider, nil }
	got, err := s.callTool("brain.gtd.review_list", map[string]interface{}{
		"sphere":  "work",
		"sources": []interface{}{"mail"},
	})
	if err != nil {
		t.Fatalf("review_list mail: %v", err)
	}
	items, _ := got["items"].([]gtdReviewItem)
	byID := map[string]gtdReviewItem{}
	for _, item := range items {
		if item.Source != "mail" {
			t.Fatalf("non-mail item leaked into mail-only request: %+v", item)
		}
		byID[fmt.Sprintf("mail:work:%d:%s", account.ID, mailRecordSourceID(item.SourceRef))] = item
	}
	wantStatus := map[string]string{
		"unread-inbox":  "inbox",
		"read-inbox":    "next",
		"deferred-flag": "deferred",
		"waiting":       "waiting",
		"archived":      "closed",
	}
	for messageID, want := range wantStatus {
		ref := fmt.Sprintf("mail:work:%d:%s", account.ID, messageID)
		got, ok := byID[ref]
		if !ok {
			t.Fatalf("missing mail item %s", ref)
			continue
		}
		if got.Status != want {
			t.Fatalf("status for %s = %q, want %q", messageID, got.Status, want)
		}
	}
}

func mailRecordSourceID(ref string) string {
	idx := -1
	parts := []byte(ref)
	colons := 0
	for i, b := range parts {
		if b == ':' {
			colons++
			if colons == 3 {
				idx = i + 1
				break
			}
		}
	}
	if idx < 0 || idx >= len(ref) {
		return ref
	}
	return ref[idx:]
}

func TestBrainGTDReviewListDefaultSourcesExcludeTasks(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	if err := os.MkdirAll(filepath.Join(tmp, "work", "brain", "projects"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	s, st, _ := newDomainServerForTest(t)
	s.brainConfigPath = configPath
	if _, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderTodoist, "Todoist", map[string]any{}); err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	called := 0
	s.newTasksProvider = func(context.Context, store.ExternalAccount) (tasks.Provider, error) {
		called++
		return nil, fmt.Errorf("tasks provider must not be called")
	}
	got, err := s.callTool("brain.gtd.review_list", map[string]interface{}{
		"config_path": configPath,
		"sphere":      "work",
	})
	if err != nil {
		t.Fatalf("review_list default: %v", err)
	}
	if called != 0 {
		t.Fatalf("tasks provider called %d times for default review_list", called)
	}
	errs, _ := got["errors"].([]string)
	for _, msg := range errs {
		if msg == "tasks source is deprecated as a default; pass explicit sources=['tasks'] to opt in" {
			t.Fatalf("default review_list should not emit tasks deprecation: %v", errs)
		}
	}
	got, err = s.callTool("brain.gtd.review_list", map[string]interface{}{
		"config_path": configPath,
		"sphere":      "work",
		"sources":     []interface{}{"tasks"},
	})
	if err != nil {
		t.Fatalf("review_list explicit tasks: %v", err)
	}
	errs, _ = got["errors"].([]string)
	foundDeprecation := false
	for _, msg := range errs {
		if msg == "tasks source is deprecated as a default; pass explicit sources=['tasks'] to opt in" {
			foundDeprecation = true
		}
	}
	if !foundDeprecation {
		t.Fatalf("explicit tasks source should emit deprecation, errors=%v", errs)
	}
}

func TestBrainGTDReviewListDedupsIssueShadows(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	writeDedupCommitment(t, tmp, "work", "brain/commitments/github/issue-43.md", "Add GTD tools", "github", "sloppy-org/sloptools#43")
	s := NewServer(t.TempDir())
	build := gtdReviewBuild{bindings: map[string]string{}, seen: map[string]struct{}{}}
	if err := s.addMarkdownGTDItems(map[string]interface{}{"config_path": configPath, "sphere": "work"}, &build); err != nil {
		t.Fatalf("addMarkdownGTDItems: %v", err)
	}
	if len(build.items) != 1 {
		t.Fatalf("markdown items = %d, want 1", len(build.items))
	}
	openIssue := providerdata.SourceItem{Provider: "github", Kind: "issue", Container: "sloppy-org/sloptools", Number: 43, Title: "Add GTD tools", State: "OPEN", SourceRef: "github:sloppy-org/sloptools#43"}
	build.addOrSkipExisting(gtdReviewItemFromSourceItem(openIssue))
	if len(build.items) != 1 {
		t.Fatalf("after open API issue, items = %d, want 1", len(build.items))
	}
	if build.items[0].Source != "github" {
		t.Fatalf("open issue did not displace markdown: %+v", build.items[0])
	}
	build = gtdReviewBuild{bindings: map[string]string{}, seen: map[string]struct{}{}}
	if err := s.addMarkdownGTDItems(map[string]interface{}{"config_path": configPath, "sphere": "work"}, &build); err != nil {
		t.Fatalf("addMarkdownGTDItems re-run: %v", err)
	}
	closedIssue := providerdata.SourceItem{Provider: "github", Kind: "issue", Container: "sloppy-org/sloptools", Number: 43, Title: "Add GTD tools", State: "closed", SourceRef: "github:sloppy-org/sloptools#43"}
	build.addOrSkipExisting(gtdReviewItemFromSourceItem(closedIssue))
	if len(build.items) != 1 {
		t.Fatalf("after closed API issue, items = %d, want 1 (markdown wins)", len(build.items))
	}
	if build.items[0].Source != "markdown" {
		t.Fatalf("closed API issue displaced markdown: %+v", build.items[0])
	}
}

func TestBrainGTDReviewListExcludesShadowMailMarkdown(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	writeDedupCommitment(t, tmp, "work", "brain/commitments/mail/m1.md", "Reply to Ada", "mail", "m1")
	writeDedupCommitment(t, tmp, "work", "brain/gtd/manual.md", "Manual commitment", "manual", "m1")
	s := NewServer(t.TempDir())
	got, err := s.callTool("brain.gtd.review_list", map[string]interface{}{
		"config_path": configPath,
		"sphere":      "work",
		"sources":     []interface{}{"markdown"},
	})
	if err != nil {
		t.Fatalf("review_list markdown only: %v", err)
	}
	items, _ := got["items"].([]gtdReviewItem)
	for _, item := range items {
		if item.Path == "brain/commitments/mail/m1.md" {
			t.Fatalf("shadow mail markdown surfaced: %+v", item)
		}
	}
	if len(items) != 1 {
		t.Fatalf("items = %d, want only the manual commitment: %+v", len(items), items)
	}
}
