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
	s, configPath, sourcesPath, root, mailProvider, taskProvider := newGTDSyncFixture(t)
	writeGTDSyncMeeting(t, root, "[ ] Send alpha budget <!-- gtd:alpha -->")
	writeGTDSyncCommitment(t, root, "closed")

	got, err := s.callTool("brain.gtd.sync", map[string]interface{}{"config_path": configPath, "sources_config": sourcesPath, "sphere": "work", "path": "brain/gtd/sync.md"})
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
	s, configPath, sourcesPath, root, mailProvider, taskProvider := newGTDSyncFixture(t)
	writeGTDSyncMeeting(t, root, "[ ] Send alpha budget <!-- gtd:alpha -->")
	writeGTDSyncCommitment(t, root, "closed")

	got, err := s.callTool("brain.gtd.sync", map[string]interface{}{"config_path": configPath, "sources_config": sourcesPath, "sphere": "work", "dry_run": true})
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
	s, configPath, sourcesPath, root, _, _ := newGTDSyncFixture(t)
	writeGTDSyncMeeting(t, root, "[x] Send alpha budget <!-- gtd:alpha -->")
	writeGTDSyncMeetingCommitment(t, root, "next")

	got, err := s.callTool("brain.gtd.sync", map[string]interface{}{"config_path": configPath, "sources_config": sourcesPath, "sphere": "work", "periodic": true})
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
	s, configPath, sourcesPath, root, _, _ := newGTDSyncFixture(t)
	writeGTDSyncMeeting(t, root, "[ ] Send alpha budget <!-- gtd:alpha -->")
	writeGTDSyncMeetingCommitment(t, root, "closed")

	got, err := s.callTool("brain.gtd.sync", map[string]interface{}{"config_path": configPath, "sources_config": sourcesPath, "sphere": "work", "periodic": true})
	if err != nil {
		t.Fatalf("brain.gtd.sync periodic: %v", err)
	}
	drifts := got["drifted"].([]gtdSyncDrift)
	if len(drifts) != 1 || drifts[0].Local != "closed" || drifts[0].Remote != "open" {
		t.Fatalf("drifted = %#v", drifts)
	}
}

func TestBrainGTDSyncContinuesAfterProviderError(t *testing.T) {
	s, configPath, sourcesPath, root, mailProvider, _ := newGTDSyncFixture(t)
	mailProvider.markReadErr = errors.New("upstream 500")
	writeGTDSyncMeeting(t, root, "[ ] Send alpha budget <!-- gtd:alpha -->")
	writeGTDSyncCommitment(t, root, "closed")

	got, err := s.callTool("brain.gtd.sync", map[string]interface{}{"config_path": configPath, "sources_config": sourcesPath, "sphere": "work"})
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

func TestBrainGTDSetStatusAutomaticallySyncsCommitment(t *testing.T) {
	s, configPath, sourcesPath, root, mailProvider, taskProvider := newGTDSyncFixture(t)
	writeGTDSyncMeeting(t, root, "[ ] Send alpha budget <!-- gtd:alpha -->")
	writeGTDSyncCommitment(t, root, "next")

	got, err := s.callTool("brain.gtd.set_status", map[string]interface{}{"config_path": configPath, "sources_config": sourcesPath, "sphere": "work", "path": "brain/gtd/sync.md", "status": "closed", "closed_at": "2026-04-29T14:00:00Z"})
	if err != nil {
		t.Fatalf("brain.gtd.set_status: %v", err)
	}
	syncResult := got["sync"].(map[string]interface{})
	if syncResult["reconciled"] != 3 {
		t.Fatalf("sync reconciled = %#v, want 3: %#v", syncResult["reconciled"], syncResult)
	}
	if !strings.Contains(readGTDSyncFile(t, root, "work/brain/meetings/standup.md"), "[x] Send alpha budget <!-- gtd:alpha -->") {
		t.Fatal("set_status did not close meeting binding")
	}
	if mailProvider.markReadCalls != 1 || taskProvider.completeCalls != 1 {
		t.Fatalf("provider calls mail=%d todoist=%d, want 1 each", mailProvider.markReadCalls, taskProvider.completeCalls)
	}
	parsed, err := s.callTool("brain.note.parse", map[string]interface{}{"config_path": configPath, "sphere": "work", "path": "brain/gtd/sync.md"})
	if err != nil {
		t.Fatalf("parse status commitment: %v", err)
	}
	commitment := parsed["commitment"].(*braingtd.Commitment)
	if commitment.LocalOverlay.Status != "closed" || commitment.LocalOverlay.ClosedAt != "2026-04-29T14:00:00Z" {
		t.Fatalf("local overlay = %#v", commitment.LocalOverlay)
	}
}

func TestBrainGTDSyncRequiresSourcesConfigWriteableOptIn(t *testing.T) {
	s, configPath, _, root, mailProvider, taskProvider := newGTDSyncFixture(t)
	writeGTDSyncMeeting(t, root, "[ ] Send alpha budget <!-- gtd:alpha -->")
	writeGTDSyncCommitment(t, root, "closed")

	got, err := s.callTool("brain.gtd.sync", map[string]interface{}{"config_path": configPath, "sphere": "work"})
	if err != nil {
		t.Fatalf("brain.gtd.sync without sources config: %v", err)
	}
	if got["reconciled"] != 0 || got["skipped"] != 3 {
		t.Fatalf("got = %#v, want all front-matter writeable bindings skipped", got)
	}
	if strings.Contains(readGTDSyncFile(t, root, "work/brain/meetings/standup.md"), "[x]") {
		t.Fatal("front-matter writeable flag closed meeting without sources.toml opt-in")
	}
	if mailProvider.markReadCalls != 0 || taskProvider.completeCalls != 0 {
		t.Fatalf("provider calls mail=%d todoist=%d, want none", mailProvider.markReadCalls, taskProvider.completeCalls)
	}
}

func TestBrainGTDSyncPeriodicReadsGitHubAndGitLabBindings(t *testing.T) {
	s, configPath, sourcesPath, root, _, _ := newGTDSyncFixture(t)
	writeGTDSyncIssueCommitments(t, root, "next", "closed")
	var calls []string
	restore := stubGTDSyncCommand(t, func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		switch name {
		case "gh":
			return []byte(`{"state":"CLOSED","closedAt":"2026-04-29T13:00:00Z"}`), nil
		case "glab":
			return []byte(`{"state":"opened"}`), nil
		default:
			return nil, errors.New("unexpected command")
		}
	})
	defer restore()

	got, err := s.callTool("brain.gtd.sync", map[string]interface{}{"config_path": configPath, "sources_config": sourcesPath, "sphere": "work", "periodic": true})
	if err != nil {
		t.Fatalf("brain.gtd.sync periodic issue bindings: %v", err)
	}
	if got["reconciled"] != 1 {
		t.Fatalf("reconciled = %#v, want 1: %#v", got["reconciled"], got)
	}
	drifts := got["drifted"].([]gtdSyncDrift)
	if len(drifts) != 1 || drifts[0].Binding != "gitlab:group/project#8" || drifts[0].Remote != "open" {
		t.Fatalf("drifted = %#v", drifts)
	}
	if len(calls) != 2 || !strings.Contains(calls[0], "issue view 7") || !strings.Contains(calls[1], "issue view 8") {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestBrainGTDSyncPushesIssueBindingsAndManualNoop(t *testing.T) {
	s, configPath, sourcesPath, root, _, _ := newGTDSyncFixture(t)
	writeGTDSyncIssueCommitments(t, root, "closed", "closed")
	writeGTDSyncManualCommitment(t, root)
	var calls []string
	restore := stubGTDSyncCommand(t, func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return nil, nil
	})
	defer restore()

	got, err := s.callTool("brain.gtd.sync", map[string]interface{}{"config_path": configPath, "sources_config": sourcesPath, "sphere": "work"})
	if err != nil {
		t.Fatalf("brain.gtd.sync issue push: %v", err)
	}
	if got["reconciled"] != 3 {
		t.Fatalf("reconciled = %#v, want 3: %#v", got["reconciled"], got)
	}
	actions := got["actions"].([]gtdSyncAction)
	if !hasGTDSyncAction(actions, "manual:local-note", "manual_noop") {
		t.Fatalf("actions missing manual no-op: %#v", actions)
	}
	if len(calls) != 2 || calls[0] != "gh issue close 7 -R sloppy-org/sloptools" || calls[1] != "glab issue close 8 -R group/project" {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestGTDSyncClosesGitHubPullsAndGitLabMergeRequests(t *testing.T) {
	var calls []string
	restore := stubGTDSyncCommand(t, func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return nil, nil
	})
	defer restore()

	if err := closeGitHubBinding(braingtd.SourceBinding{Provider: "github", Ref: "org/repo#12", URL: "https://github.com/org/repo/pull/12"}); err != nil {
		t.Fatalf("close github pull: %v", err)
	}
	if err := closeGitLabBinding(braingtd.SourceBinding{Provider: "gitlab", Ref: "group/project!9"}); err != nil {
		t.Fatalf("close gitlab merge request: %v", err)
	}
	if len(calls) != 2 || calls[0] != "gh pr close 12 -R org/repo" || calls[1] != "glab mr close 9 -R group/project" {
		t.Fatalf("calls = %#v", calls)
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

func newGTDSyncFixture(t *testing.T) (*Server, string, string, string, *gtdSyncMailProvider, *fakeTasksProvider) {
	t.Helper()
	root := t.TempDir()
	configPath := writeMCPBrainConfig(t, root)
	sourcesPath := writeGTDSyncSources(t, root)
	st, err := store.New(filepath.Join(t.TempDir(), "sloppy.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderIMAP, "IMAP", map[string]any{}); err != nil {
		t.Fatalf("CreateExternalAccount(imap): %v", err)
	}
	if _, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderGmail, "Gmail", map[string]any{}); err != nil {
		t.Fatalf("CreateExternalAccount(gmail): %v", err)
	}
	if _, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderTodoist, "Todoist", map[string]any{}); err != nil {
		t.Fatalf("CreateExternalAccount(todoist): %v", err)
	}
	mailProvider := &gtdSyncMailProvider{fakeMailProvider: &fakeMailProvider{messages: map[string]*providerdata.EmailMessage{
		"m1": {ID: "m1", Subject: "Budget", IsRead: false, Labels: []string{"INBOX"}},
	}}}
	decoyProvider := &gtdSyncMailProvider{fakeMailProvider: &fakeMailProvider{messages: map[string]*providerdata.EmailMessage{
		"m1": {ID: "m1", Subject: "Budget", IsRead: false, Labels: []string{"INBOX"}},
	}}}
	taskProvider := &fakeTasksProvider{name: "todoist", hasCompleter: true, getTaskByID: map[string]providerdata.TaskItem{
		"task-1": {ID: "task-1", ListID: "project", Completed: false},
	}}
	s := NewServerWithStoreAndBrainConfig(t.TempDir(), st, configPath)
	s.newEmailProvider = func(_ context.Context, account store.ExternalAccount) (email.EmailProvider, error) {
		if account.Provider == store.ExternalProviderIMAP {
			return decoyProvider, nil
		}
		return mailProvider, nil
	}
	s.newTasksProvider = func(context.Context, store.ExternalAccount) (tasks.Provider, error) {
		return taskProvider, nil
	}
	return s, configPath, sourcesPath, root, mailProvider, taskProvider
}

func writeGTDSyncSources(t *testing.T, root string) string {
	t.Helper()
	path := filepath.Join(root, "sources.toml")
	body := `[[source]]
sphere = "work"
provider = "meetings"
ref = "gtd:alpha"
writeable = true

[[source]]
sphere = "work"
provider = "mail"
ref = "m1"
writeable = true

[[source]]
sphere = "work"
provider = "todoist"
ref = "project/task-1"
writeable = true

[[source]]
sphere = "work"
provider = "github"
ref = "sloppy-org/sloptools#7"
writeable = true

[[source]]
sphere = "work"
provider = "gitlab"
ref = "group/project#8"
writeable = true

[[source]]
sphere = "work"
provider = "manual"
ref = "local-note"
writeable = true
`
	writeMCPBrainFile(t, path, body)
	return path
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
  - provider: gmail
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

func writeGTDSyncIssueCommitments(t *testing.T, root, githubStatus, gitlabStatus string) {
	t.Helper()
	github := `---
kind: commitment
title: Close GitHub issue
status: next
outcome: Close GitHub issue
source_bindings:
  - provider: github
    ref: "sloppy-org/sloptools#7"
local_overlay:
  status: ` + githubStatus + `
---
Body.
`
	gitlab := `---
kind: commitment
title: Close GitLab issue
status: next
outcome: Close GitLab issue
source_bindings:
  - provider: gitlab
    ref: "group/project#8"
local_overlay:
  status: ` + gitlabStatus + `
---
Body.
`
	writeMCPBrainFile(t, filepath.Join(root, "work", "brain", "gtd", "github.md"), github)
	writeMCPBrainFile(t, filepath.Join(root, "work", "brain", "gtd", "gitlab.md"), gitlab)
}

func writeGTDSyncManualCommitment(t *testing.T, root string) {
	t.Helper()
	body := `---
kind: commitment
title: Local note
status: next
outcome: Local note
source_bindings:
  - provider: manual
    ref: "local-note"
local_overlay:
  status: closed
---
Body.
`
	writeMCPBrainFile(t, filepath.Join(root, "work", "brain", "gtd", "manual.md"), body)
}

func hasGTDSyncAction(actions []gtdSyncAction, binding, action string) bool {
	for _, got := range actions {
		if got.Binding == binding && got.Action == action {
			return true
		}
	}
	return false
}

func stubGTDSyncCommand(t *testing.T, fn func(context.Context, string, ...string) ([]byte, error)) func() {
	t.Helper()
	old := runGTDSyncCommandOutput
	runGTDSyncCommandOutput = fn
	return func() {
		runGTDSyncCommandOutput = old
	}
}

func readGTDSyncFile(t *testing.T, root, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(data)
}
