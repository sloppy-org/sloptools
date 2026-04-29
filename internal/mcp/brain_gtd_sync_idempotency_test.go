package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"
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
