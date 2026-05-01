package mcp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	braingtd "github.com/sloppy-org/sloptools/internal/brain/gtd"
	"github.com/sloppy-org/sloptools/internal/meetings"
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

const meetingsTestSourceRel = "brain/meetings/standup.md"

const meetingsTestSource = `---
title: Standup
---
# Standup

## Action Checklist

### Ada Lovelace
- [ ] Reply to Ada about benchmarks @due:2026-05-02

### Babbage
- [ ] Send the analytical engine paper @follow:2026-05-10
`

func setupMeetingsIngestVault(t *testing.T) (*Server, string, string) {
	t.Helper()
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	writeMCPBrainFile(t, filepath.Join(tmp, "work", meetingsTestSourceRel), meetingsTestSource)
	return NewServer(t.TempDir()), tmp, configPath
}

func TestBrainGTDIngestMeetingsStampsIDsAndCreatesPerPersonCommitments(t *testing.T) {
	s, root, configPath := setupMeetingsIngestVault(t)
	got, err := s.callTool("brain.gtd.ingest", map[string]interface{}{
		"config_path": configPath,
		"sphere":      "work",
		"source":      "meetings",
		"paths":       []interface{}{meetingsTestSourceRel},
	})
	if err != nil {
		t.Fatalf("brain.gtd.ingest: %v", err)
	}
	if got["count"] != 2 {
		t.Fatalf("count = %v, want 2: %#v", got["count"], got)
	}
	created, _ := got["created"].([]string)
	if len(created) != 2 {
		t.Fatalf("created = %#v", got["created"])
	}
	stamped, _ := got["stamped"].([]string)
	if len(stamped) != 1 || stamped[0] != meetingsTestSourceRel {
		t.Fatalf("stamped = %#v", got["stamped"])
	}
	source := readFile(t, filepath.Join(root, "work", meetingsTestSourceRel))
	if strings.Count(source, "<!-- gtd:") != 2 {
		t.Fatalf("expected 2 stamped IDs, got source:\n%s", source)
	}
	for _, rel := range created {
		data := readFile(t, filepath.Join(root, "work", rel))
		if !strings.Contains(data, "provider: meetings") {
			t.Fatalf("missing meetings binding in %s:\n%s", rel, data)
		}
		if result := braingtd.ParseAndValidate(data); len(result.Diagnostics) != 0 {
			t.Fatalf("invalid ingest output for %s: %#v\n%s", rel, result.Diagnostics, data)
		}
	}
}

func TestBrainGTDIngestMeetingsIsIdempotent(t *testing.T) {
	s, _, configPath := setupMeetingsIngestVault(t)
	args := map[string]interface{}{
		"config_path": configPath,
		"sphere":      "work",
		"source":      "meetings",
		"paths":       []interface{}{meetingsTestSourceRel},
	}
	if _, err := s.callTool("brain.gtd.ingest", args); err != nil {
		t.Fatalf("first ingest: %v", err)
	}
	got, err := s.callTool("brain.gtd.ingest", args)
	if err != nil {
		t.Fatalf("second ingest: %v", err)
	}
	created, _ := got["created"].([]string)
	if len(created) != 0 {
		t.Fatalf("re-ingest must not create commitments, got %#v", got["created"])
	}
	skipped, _ := got["skipped"].([]string)
	if len(skipped) != 2 {
		t.Fatalf("re-ingest must skip both existing commitments, got %#v", got["skipped"])
	}
	if got["updated"] != false {
		t.Fatalf("re-ingest must report updated=false, got %#v", got["updated"])
	}
}

func TestBrainGTDIngestMeetingsClosesCommitmentsOnHandEditedCheckmark(t *testing.T) {
	s, root, configPath := setupMeetingsIngestVault(t)
	args := map[string]interface{}{
		"config_path": configPath,
		"sphere":      "work",
		"source":      "meetings",
		"paths":       []interface{}{meetingsTestSourceRel},
	}
	first, err := s.callTool("brain.gtd.ingest", args)
	if err != nil {
		t.Fatalf("first ingest: %v", err)
	}
	created, _ := first["created"].([]string)
	if len(created) != 2 {
		t.Fatalf("first ingest created = %#v", first["created"])
	}
	sourcePath := filepath.Join(root, "work", meetingsTestSourceRel)
	data := readFile(t, sourcePath)
	flipped := strings.Replace(data, "- [ ] Reply to Ada about benchmarks", "- [x] Reply to Ada about benchmarks", 1)
	if flipped == data {
		t.Fatalf("could not flip checkbox; source was:\n%s", data)
	}
	if err := os.WriteFile(sourcePath, []byte(flipped), 0o644); err != nil {
		t.Fatalf("write flipped: %v", err)
	}
	second, err := s.callTool("brain.gtd.ingest", args)
	if err != nil {
		t.Fatalf("second ingest: %v", err)
	}
	closed, _ := second["closed"].([]string)
	if len(closed) != 1 {
		t.Fatalf("expected one closed commitment, got %#v", second["closed"])
	}
	closedData := readFile(t, filepath.Join(root, "work", closed[0]))
	commitment, _, diags := braingtd.ParseCommitmentMarkdown(closedData)
	if len(diags) != 0 {
		t.Fatalf("parse closed commitment: %#v\n%s", diags, closedData)
	}
	if !strings.EqualFold(commitment.LocalOverlay.Status, "closed") {
		t.Fatalf("local_overlay.status = %q, want closed:\n%s", commitment.LocalOverlay.Status, closedData)
	}
	if commitment.LocalOverlay.ClosedVia != "brain.gtd.ingest" {
		t.Fatalf("local_overlay.closed_via = %q, want brain.gtd.ingest", commitment.LocalOverlay.ClosedVia)
	}
}

func TestBrainGTDSyncFlipsMeetingsCheckboxByStableAnchor(t *testing.T) {
	s, root, configPath := setupMeetingsIngestVault(t)
	sourcesConfig := writeMeetingsSourcesConfig(t, root)
	if _, err := s.callTool("brain.gtd.ingest", map[string]interface{}{
		"config_path": configPath,
		"sphere":      "work",
		"source":      "meetings",
		"paths":       []interface{}{meetingsTestSourceRel},
	}); err != nil {
		t.Fatalf("ingest: %v", err)
	}
	created := listCreatedCommitments(t, filepath.Join(root, "work", "brain", "gtd", "ingest"))
	if len(created) != 2 {
		t.Fatalf("expected 2 commitments in ingest dir, got %d", len(created))
	}
	target := pickAdaCommitment(t, created)
	got, err := s.callTool("brain.gtd.set_status", map[string]interface{}{
		"config_path":    configPath,
		"sphere":         "work",
		"path":           relWorkPath(t, root, target),
		"status":         "closed",
		"sources_config": sourcesConfig,
	})
	if err != nil {
		t.Fatalf("brain.gtd.set_status: %v", err)
	}
	if got["status"] != "closed" {
		t.Fatalf("status = %v, want closed", got["status"])
	}
	source := readFile(t, filepath.Join(root, "work", meetingsTestSourceRel))
	if !strings.Contains(source, "- [x] Reply to Ada about benchmarks") {
		t.Fatalf("expected source line flipped to [x]:\n%s", source)
	}
	if !strings.Contains(source, "- [ ] Send the analytical engine paper") {
		t.Fatalf("Babbage line must remain unchecked:\n%s", source)
	}
}

func writeMeetingsSourcesConfig(t *testing.T, root string) string {
	t.Helper()
	path := filepath.Join(root, "sources.toml")
	body := `[[source]]
sphere = "work"
provider = "meetings"
ref = "*"
writeable = true
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write sources.toml: %v", err)
	}
	return path
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func listCreatedCommitments(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	var out []string
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		out = append(out, filepath.Join(dir, entry.Name()))
	}
	return out
}

func pickAdaCommitment(t *testing.T, paths []string) string {
	t.Helper()
	for _, path := range paths {
		data := readFile(t, path)
		if strings.Contains(data, "Reply to Ada about benchmarks") {
			return path
		}
	}
	t.Fatalf("Ada commitment not found among %v", paths)
	return ""
}

func relWorkPath(t *testing.T, root, abs string) string {
	t.Helper()
	rel, err := filepath.Rel(filepath.Join(root, "work"), abs)
	if err != nil {
		t.Fatalf("rel: %v", err)
	}
	return filepath.ToSlash(rel)
}

func TestMeetingSlugFromRelHandlesMeetingNotesFolder(t *testing.T) {
	cases := map[string]string{
		"brain/meetings/standup.md":                  "standup",
		"MEETINGS/2026-04-29-board/MEETING_NOTES.md": "2026-04-29-board",
	}
	for input, want := range cases {
		if got := meetingSlugFromRel(input); got != want {
			t.Fatalf("meetingSlugFromRel(%q) = %q, want %q", input, got, want)
		}
	}
	// Computed IDs are stable for the same (slug, person, text) tuple.
	if meetings.ComputeID("standup", "Ada Lovelace", "x") != meetings.ComputeID("standup", "Ada Lovelace", "x") {
		t.Fatal("ComputeID must be stable")
	}
}
