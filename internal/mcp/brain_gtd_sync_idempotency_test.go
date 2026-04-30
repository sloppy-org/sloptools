package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"

	braingtd "github.com/sloppy-org/sloptools/internal/brain/gtd"
)

func TestBrainGTDSyncRerunNoopsAlreadyClosedUpstream(t *testing.T) {
	s, configPath, sourcesPath, root, mailProvider, taskProvider := newGTDSyncFixture(t)
	writeGTDSyncMeeting(t, root, "[ ] Send alpha budget <!-- gtd:alpha -->")
	writeGTDSyncCommitment(t, root, "closed")
	writeGTDSyncIssueCommitments(t, root, "closed", "closed")
	writeGTDSyncManualCommitment(t, root)
	issueStates := map[string]string{"gh": "open", "glab": "opened"}
	var closeCalls []string
	restore := stubGTDSyncCommand(t, func(_ context.Context, name string, args ...string) ([]byte, error) {
		call := name + " " + strings.Join(args, " ")
		if strings.Contains(call, " close ") {
			closeCalls = append(closeCalls, call)
			switch name {
			case "gh":
				issueStates["gh"] = "closed"
			case "glab":
				issueStates["glab"] = "closed"
			}
			return nil, nil
		}
		switch name {
		case "gh":
			if issueStates["gh"] == "closed" {
				return []byte(`{"state":"CLOSED","closedAt":"2026-04-29T15:00:00Z"}`), nil
			}
			return []byte(`{"state":"OPEN"}`), nil
		case "glab":
			if issueStates["glab"] == "closed" {
				return []byte(`{"state":"closed","closed_at":"2026-04-29T15:00:00Z"}`), nil
			}
			return []byte(`{"state":"opened"}`), nil
		default:
			return nil, errors.New("unexpected command")
		}
	})
	defer restore()

	first, err := s.callTool("brain.gtd.sync", map[string]interface{}{"config_path": configPath, "sources_config": sourcesPath, "sphere": "work"})
	if err != nil {
		t.Fatalf("first brain.gtd.sync: %v", err)
	}
	if first["reconciled"] != 5 || len(closeCalls) != 2 {
		t.Fatalf("first sync got reconciled=%#v closeCalls=%#v", first["reconciled"], closeCalls)
	}
	mailProvider.messages["m1"].IsRead = true
	mailProvider.messages["m1"].Labels = []string{"ARCHIVE"}
	task := taskProvider.getTaskByID["task-1"]
	task.Completed = true
	taskProvider.getTaskByID["task-1"] = task

	second, err := s.callTool("brain.gtd.sync", map[string]interface{}{"config_path": configPath, "sources_config": sourcesPath, "sphere": "work"})
	if err != nil {
		t.Fatalf("second brain.gtd.sync: %v", err)
	}
	if second["reconciled"] != 0 || second["skipped"] != 6 {
		t.Fatalf("second sync got reconciled=%#v skipped=%#v: %#v", second["reconciled"], second["skipped"], second)
	}
	if len(closeCalls) != 2 || mailProvider.markReadCalls != 1 || taskProvider.completeCalls != 1 {
		t.Fatalf("rerun dispatched close work: closeCalls=%#v mail=%d todoist=%d", closeCalls, mailProvider.markReadCalls, taskProvider.completeCalls)
	}
	actions := second["actions"].([]gtdSyncAction)
	for _, binding := range []string{"meetings:gtd:alpha", "gmail:m1", "todoist:project/task-1", "github:sloppy-org/sloptools#7", "gitlab:group/project#8"} {
		if !hasGTDSyncAction(actions, binding, "upstream_already_closed") {
			t.Fatalf("actions missing already-closed no-op for %s: %#v", binding, actions)
		}
	}
	if !hasGTDSyncAction(actions, "manual:local-note", "manual_noop") {
		t.Fatalf("actions missing manual no-op: %#v", actions)
	}
}

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
	if result := braingtd.ParseAndValidate(readGTDSyncFile(t, root, "work/brain/gtd/sync.md")); len(result.Diagnostics) != 0 {
		t.Fatalf("synced commitment invalid: %#v", result.Diagnostics)
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
