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
	affected := requireSingleAffectedRef(t, got)
	if affected.Domain != "brain" || affected.Kind != "gtd_commitment" || affected.Path != "brain/gtd/sync.md" || affected.Sphere != "work" {
		t.Fatalf("affected = %#v", affected)
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
	affected := requireSingleAffectedRef(t, got)
	if affected.Domain != "brain" || affected.Kind != "gtd_commitment" || affected.Path != "brain/gtd/github.md" || affected.Sphere != "work" {
		t.Fatalf("affected = %#v", affected)
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
		call := name + " " + strings.Join(args, " ")
		calls = append(calls, call)
		if strings.Contains(call, " view ") {
			return []byte(`{"state":"open"}`), nil
		}
		return nil, nil
	})
	defer restore()

	got, err := s.callTool("brain.gtd.sync", map[string]interface{}{"config_path": configPath, "sources_config": sourcesPath, "sphere": "work"})
	if err != nil {
		t.Fatalf("brain.gtd.sync issue push: %v", err)
	}
	if got["reconciled"] != 2 {
		t.Fatalf("reconciled = %#v, want 2: %#v", got["reconciled"], got)
	}
	affected := requireAffectedRefs(t, got)
	if len(affected) != 2 {
		t.Fatalf("len(affected) = %d, want 2: %#v", len(affected), affected)
	}
	if affected[0].Path != "brain/gtd/github.md" || affected[1].Path != "brain/gtd/gitlab.md" {
		t.Fatalf("affected paths = %#v", affected)
	}
	actions := got["actions"].([]gtdSyncAction)
	if !hasGTDSyncAction(actions, "manual:local-note", "manual_noop") {
		t.Fatalf("actions missing manual no-op: %#v", actions)
	}
	wantCalls := []string{
		"gh issue view 7 -R sloppy-org/sloptools --json state,closedAt",
		"gh issue close 7 -R sloppy-org/sloptools",
		"glab issue view 8 -R group/project --output json",
		"glab issue close 8 -R group/project",
	}
	if strings.Join(calls, "\n") != strings.Join(wantCalls, "\n") {
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
sphere: work
title: Send alpha budget
status: next
context: review
next_action: Review the budget
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
# Send alpha budget

## Summary
Review the budget.

## Next Action
- [ ] Review the budget

## Evidence
- meetings:gtd:alpha

## Linked Items
- None.

## Review Notes
- None.
`
	writeMCPBrainFile(t, filepath.Join(root, "work", "brain", "gtd", "sync.md"), body)
}

func writeGTDSyncMeetingCommitment(t *testing.T, root, overlayStatus string) {
	t.Helper()
	body := `---
kind: commitment
sphere: work
title: Send alpha budget
status: next
context: review
next_action: Review the budget
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
# Send alpha budget

## Summary
Review the budget.

## Next Action
- [ ] Review the budget

## Evidence
- meetings:gtd:alpha

## Linked Items
- None.

## Review Notes
- None.
`
	writeMCPBrainFile(t, filepath.Join(root, "work", "brain", "gtd", "sync.md"), body)
}

func writeGTDSyncIssueCommitments(t *testing.T, root, githubStatus, gitlabStatus string) {
	t.Helper()
	github := `---
kind: commitment
sphere: work
title: Close GitHub issue
status: next
context: review
next_action: Review the issue
outcome: Close GitHub issue
source_bindings:
  - provider: github
    ref: "sloppy-org/sloptools#7"
local_overlay:
  status: ` + githubStatus + `
---
# Close GitHub issue

## Summary
Review the issue.

## Next Action
- [ ] Review the issue

## Evidence
- github:sloppy-org/sloptools#7

## Linked Items
- None.

## Review Notes
- None.
`
	gitlab := `---
kind: commitment
sphere: work
title: Close GitLab issue
status: next
context: review
next_action: Review the issue
outcome: Close GitLab issue
source_bindings:
  - provider: gitlab
    ref: "group/project#8"
local_overlay:
  status: ` + gitlabStatus + `
---
# Close GitLab issue

## Summary
Review the issue.

## Next Action
- [ ] Review the issue

## Evidence
- gitlab:group/project#8

## Linked Items
- None.

## Review Notes
- None.
`
	writeMCPBrainFile(t, filepath.Join(root, "work", "brain", "gtd", "github.md"), github)
	writeMCPBrainFile(t, filepath.Join(root, "work", "brain", "gtd", "gitlab.md"), gitlab)
}

func writeGTDSyncManualCommitment(t *testing.T, root string) {
	t.Helper()
	body := `---
kind: commitment
sphere: work
title: Local note
status: next
context: review
next_action: Review the note
outcome: Local note
source_bindings:
  - provider: manual
    ref: "local-note"
local_overlay:
  status: closed
---
# Local note

## Summary
Review the note.

## Next Action
- [ ] Review the note

## Evidence
- manual:local-note

## Linked Items
- None.

## Review Notes
- None.
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
