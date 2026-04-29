package mcp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	braingtd "github.com/sloppy-org/sloptools/internal/brain/gtd"
	"github.com/sloppy-org/sloptools/internal/email"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/store"
	"github.com/sloppy-org/sloptools/internal/tasks"
)

func TestBrainGTDSyncPushesClosedWriteableBindings(t *testing.T) {
	s, configPath, root, mailProvider, taskProvider := newGTDSyncFixture(t)
	writeGTDSyncMeeting(t, root, "[ ] Send alpha budget <!-- gtd:alpha -->")
	writeGTDSyncCommitment(t, root, "closed")

	got, err := s.callTool("brain.gtd.sync", map[string]interface{}{"config_path": configPath, "sphere": "work", "path": "brain/gtd/sync.md"})
	if err != nil {
		t.Fatalf("brain.gtd.sync: %v", err)
	}
	if got["reconciled"] != 3 {
		t.Fatalf("reconciled = %#v, want 3: %#v", got["reconciled"], got)
	}
	meeting := readGTDSyncFile(t, root, "work/brain/meetings/standup.md")
	if !strings.Contains(meeting, "[x] Send alpha budget <!-- gtd:alpha -->") {
		t.Fatalf("meeting checkbox not closed:\n%s", meeting)
	}
	if mailProvider.lastAction != "archive" || len(mailProvider.lastIDs) != 1 || mailProvider.lastIDs[0] != "m1" {
		t.Fatalf("mail action = %q ids=%#v", mailProvider.lastAction, mailProvider.lastIDs)
	}
	if mailProvider.markReadCalls != 1 {
		t.Fatalf("mail mark-read calls = %d, want 1", mailProvider.markReadCalls)
	}
	if taskProvider.completeCalls != 1 {
		t.Fatalf("todoist complete calls = %d, want 1", taskProvider.completeCalls)
	}
	if errs := got["errors"].([]gtdSyncError); len(errs) != 0 {
		t.Fatalf("errors = %#v", errs)
	}
}

func TestBrainGTDSyncDryRunReportsWithoutWrites(t *testing.T) {
	s, configPath, root, mailProvider, taskProvider := newGTDSyncFixture(t)
	writeGTDSyncMeeting(t, root, "[ ] Send alpha budget <!-- gtd:alpha -->")
	writeGTDSyncCommitment(t, root, "closed")

	got, err := s.callTool("brain.gtd.sync", map[string]interface{}{"config_path": configPath, "sphere": "work", "dry_run": true})
	if err != nil {
		t.Fatalf("brain.gtd.sync dry_run: %v", err)
	}
	if got["reconciled"] != 3 {
		t.Fatalf("reconciled = %#v, want 3: %#v", got["reconciled"], got)
	}
	if strings.Contains(readGTDSyncFile(t, root, "work/brain/meetings/standup.md"), "[x]") {
		t.Fatal("dry_run modified meeting checkbox")
	}
	if mailProvider.lastAction != "" || taskProvider.completeCalls != 0 {
		t.Fatalf("dry_run touched providers: mail=%q todoist=%d", mailProvider.lastAction, taskProvider.completeCalls)
	}
	for _, action := range got["actions"].([]gtdSyncAction) {
		if !action.DryRun {
			t.Fatalf("action missing dry_run flag: %#v", action)
		}
	}
}

func TestBrainGTDSyncPeriodicMeetingPullsClosedState(t *testing.T) {
	s, configPath, root, _, _ := newGTDSyncFixture(t)
	writeGTDSyncMeeting(t, root, "[x] Send alpha budget <!-- gtd:alpha -->")
	writeGTDSyncMeetingCommitment(t, root, "next")

	got, err := s.callTool("brain.gtd.sync", map[string]interface{}{"config_path": configPath, "sphere": "work", "periodic": true})
	if err != nil {
		t.Fatalf("brain.gtd.sync periodic: %v", err)
	}
	if got["reconciled"] != 1 {
		t.Fatalf("reconciled = %#v, want 1: %#v", got["reconciled"], got)
	}
	parsed, err := s.callTool("brain.note.parse", map[string]interface{}{"config_path": configPath, "sphere": "work", "path": "brain/gtd/sync.md"})
	if err != nil {
		t.Fatalf("parse synced commitment: %v", err)
	}
	commitment := parsed["commitment"].(*braingtd.Commitment)
	if commitment.LocalOverlay.Status != "closed" || commitment.LocalOverlay.ClosedVia != "brain.gtd.sync" {
		t.Fatalf("local overlay = %#v", commitment.LocalOverlay)
	}
}

func TestBrainGTDSyncPeriodicReportsConflict(t *testing.T) {
	s, configPath, root, _, _ := newGTDSyncFixture(t)
	writeGTDSyncMeeting(t, root, "[ ] Send alpha budget <!-- gtd:alpha -->")
	writeGTDSyncMeetingCommitment(t, root, "closed")

	got, err := s.callTool("brain.gtd.sync", map[string]interface{}{"config_path": configPath, "sphere": "work", "periodic": true})
	if err != nil {
		t.Fatalf("brain.gtd.sync periodic: %v", err)
	}
	drifts := got["drifted"].([]gtdSyncDrift)
	if len(drifts) != 1 || drifts[0].Local != "closed" || drifts[0].Remote != "open" {
		t.Fatalf("drifted = %#v", drifts)
	}
}

func TestBrainGTDSyncContinuesAfterProviderError(t *testing.T) {
	s, configPath, root, mailProvider, _ := newGTDSyncFixture(t)
	mailProvider.markReadErr = errors.New("upstream 500")
	writeGTDSyncMeeting(t, root, "[ ] Send alpha budget <!-- gtd:alpha -->")
	writeGTDSyncCommitment(t, root, "closed")

	got, err := s.callTool("brain.gtd.sync", map[string]interface{}{"config_path": configPath, "sphere": "work"})
	if err != nil {
		t.Fatalf("brain.gtd.sync: %v", err)
	}
	if got["reconciled"] != 2 {
		t.Fatalf("reconciled = %#v, want 2: %#v", got["reconciled"], got)
	}
	errs := got["errors"].([]gtdSyncError)
	if len(errs) != 1 || !strings.Contains(errs[0].Error, "upstream 500") {
		t.Fatalf("errors = %#v", errs)
	}
	if !strings.Contains(readGTDSyncFile(t, root, "work/brain/meetings/standup.md"), "[x]") {
		t.Fatal("meeting binding did not continue after mail error")
	}
}

type gtdSyncMailProvider struct {
	*fakeMailProvider
	markReadCalls int
	markReadErr   error
}

func (p *gtdSyncMailProvider) MarkRead(ctx context.Context, ids []string) (int, error) {
	if p.markReadErr != nil {
		return 0, p.markReadErr
	}
	p.markReadCalls++
	return p.fakeMailProvider.MarkRead(ctx, ids)
}

func newGTDSyncFixture(t *testing.T) (*Server, string, string, *gtdSyncMailProvider, *fakeTasksProvider) {
	t.Helper()
	root := t.TempDir()
	configPath := writeMCPBrainConfig(t, root)
	st, err := store.New(filepath.Join(t.TempDir(), "sloppy.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderGmail, "Gmail", map[string]any{}); err != nil {
		t.Fatalf("CreateExternalAccount(mail): %v", err)
	}
	if _, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderTodoist, "Todoist", map[string]any{}); err != nil {
		t.Fatalf("CreateExternalAccount(todoist): %v", err)
	}
	mailProvider := &gtdSyncMailProvider{fakeMailProvider: &fakeMailProvider{messages: map[string]*providerdata.EmailMessage{
		"m1": {ID: "m1", Subject: "Budget", IsRead: false, Labels: []string{"INBOX"}},
	}}}
	taskProvider := &fakeTasksProvider{name: "todoist", hasCompleter: true, getTaskByID: map[string]providerdata.TaskItem{
		"task-1": {ID: "task-1", ListID: "project", Completed: false},
	}}
	s := NewServerWithStoreAndBrainConfig(t.TempDir(), st, configPath)
	s.newEmailProvider = func(context.Context, store.ExternalAccount) (email.EmailProvider, error) {
		return mailProvider, nil
	}
	s.newTasksProvider = func(context.Context, store.ExternalAccount) (tasks.Provider, error) {
		return taskProvider, nil
	}
	return s, configPath, root, mailProvider, taskProvider
}

func writeGTDSyncMeeting(t *testing.T, root, line string) {
	t.Helper()
	writeMCPBrainFile(t, filepath.Join(root, "work", "brain", "meetings", "standup.md"), "- "+line+"\n")
}

func writeGTDSyncCommitment(t *testing.T, root, overlayStatus string) {
	t.Helper()
	body := `---
kind: commitment
title: Send alpha budget
status: next
outcome: Send alpha budget
source_bindings:
  - provider: meetings
    ref: "gtd:alpha"
    writeable: true
    location:
      path: brain/meetings/standup.md
      anchor: "gtd:alpha"
  - provider: mail
    ref: "m1"
    writeable: true
  - provider: todoist
    ref: "project/task-1"
    writeable: true
local_overlay:
  status: ` + overlayStatus + `
---
Body.
`
	writeMCPBrainFile(t, filepath.Join(root, "work", "brain", "gtd", "sync.md"), body)
}

func writeGTDSyncMeetingCommitment(t *testing.T, root, overlayStatus string) {
	t.Helper()
	body := `---
kind: commitment
title: Send alpha budget
status: next
outcome: Send alpha budget
source_bindings:
  - provider: meetings
    ref: "gtd:alpha"
    writeable: true
    location:
      path: brain/meetings/standup.md
      anchor: "gtd:alpha"
local_overlay:
  status: ` + overlayStatus + `
---
Body.
`
	writeMCPBrainFile(t, filepath.Join(root, "work", "brain", "gtd", "sync.md"), body)
}

func readGTDSyncFile(t *testing.T, root, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(data)
}
