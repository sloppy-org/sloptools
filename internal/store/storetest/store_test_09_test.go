package store_test

import (
	"database/sql"
	"errors"
	. "github.com/sloppy-org/sloptools/internal/store"
	_ "modernc.org/sqlite"
	"path/filepath"
	"testing"
)

var _ *Store

func TestStoreItemArtifactLinkLifecycle(t *testing.T) {
	s := newTestStore(t)
	sourceTitle := "Source brief"
	sourceArtifact, err := s.CreateArtifact(ArtifactKindMarkdown, nil, nil, &sourceTitle, nil)
	if err != nil {
		t.Fatalf("CreateArtifact(source) error: %v", err)
	}
	relatedTitle := "Related email"
	relatedArtifact, err := s.CreateArtifact(ArtifactKindEmail, nil, nil, &relatedTitle, nil)
	if err != nil {
		t.Fatalf("CreateArtifact(related) error: %v", err)
	}
	outputTitle := "Output PDF"
	outputArtifact, err := s.CreateArtifact(ArtifactKindPDF, nil, nil, &outputTitle, nil)
	if err != nil {
		t.Fatalf("CreateArtifact(output) error: %v", err)
	}
	item, err := s.CreateItem("Review supporting material", ItemOptions{ArtifactID: &sourceArtifact.ID})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}
	reviewTarget := ItemReviewTargetGitHub
	reviewer := "octocat"
	if err := s.UpdateItemReviewDispatch(item.ID, &reviewTarget, &reviewer); err != nil {
		t.Fatalf("UpdateItemReviewDispatch() error: %v", err)
	}
	initialArtifacts, err := s.ListItemArtifacts(item.ID)
	if err != nil {
		t.Fatalf("ListItemArtifacts(initial) error: %v", err)
	}
	if len(initialArtifacts) != 1 || initialArtifacts[0].ArtifactID != sourceArtifact.ID || initialArtifacts[0].Role != "source" {
		t.Fatalf("ListItemArtifacts(initial) = %+v, want source artifact %d", initialArtifacts, sourceArtifact.ID)
	}
	if err := s.LinkItemArtifact(item.ID, relatedArtifact.ID, "related"); err != nil {
		t.Fatalf("LinkItemArtifact(related) error: %v", err)
	}
	if err := s.LinkItemArtifact(item.ID, outputArtifact.ID, "source"); err != nil {
		t.Fatalf("LinkItemArtifact(source) error: %v", err)
	}
	updatedItem, err := s.GetItem(item.ID)
	if err != nil {
		t.Fatalf("GetItem(updated) error: %v", err)
	}
	if updatedItem.ArtifactID == nil || *updatedItem.ArtifactID != outputArtifact.ID {
		t.Fatalf("GetItem(updated).ArtifactID = %v, want %d", updatedItem.ArtifactID, outputArtifact.ID)
	}
	artifacts, err := s.ListItemArtifacts(item.ID)
	if err != nil {
		t.Fatalf("ListItemArtifacts(updated) error: %v", err)
	}
	if len(artifacts) != 3 {
		t.Fatalf("ListItemArtifacts(updated) len = %d, want 3", len(artifacts))
	}
	if artifacts[0].ArtifactID != outputArtifact.ID || artifacts[0].Role != "source" {
		t.Fatalf("primary artifact = %+v, want output artifact as source", artifacts[0])
	}
	itemsForRelated, err := s.ListArtifactItems(relatedArtifact.ID)
	if err != nil {
		t.Fatalf("ListArtifactItems() error: %v", err)
	}
	if len(itemsForRelated) != 1 || itemsForRelated[0].ID != item.ID {
		t.Fatalf("ListArtifactItems() = %+v, want item %d", itemsForRelated, item.ID)
	}
	if itemsForRelated[0].ReviewTarget == nil || *itemsForRelated[0].ReviewTarget != reviewTarget {
		t.Fatalf("ListArtifactItems().ReviewTarget = %v, want %q", itemsForRelated[0].ReviewTarget, reviewTarget)
	}
	if itemsForRelated[0].Reviewer == nil || *itemsForRelated[0].Reviewer != reviewer {
		t.Fatalf("ListArtifactItems().Reviewer = %v, want %q", itemsForRelated[0].Reviewer, reviewer)
	}
	if itemsForRelated[0].ReviewedAt == nil || *itemsForRelated[0].ReviewedAt == "" {
		t.Fatalf("ListArtifactItems().ReviewedAt = %v, want timestamp", itemsForRelated[0].ReviewedAt)
	}
	if err := s.UnlinkItemArtifact(item.ID, outputArtifact.ID); err != nil {
		t.Fatalf("UnlinkItemArtifact(primary) error: %v", err)
	}
	restoredItem, err := s.GetItem(item.ID)
	if err != nil {
		t.Fatalf("GetItem(restored) error: %v", err)
	}
	if restoredItem.ArtifactID == nil || *restoredItem.ArtifactID != sourceArtifact.ID {
		t.Fatalf("GetItem(restored).ArtifactID = %v, want %d", restoredItem.ArtifactID, sourceArtifact.ID)
	}
	restoredArtifacts, err := s.ListItemArtifacts(item.ID)
	if err != nil {
		t.Fatalf("ListItemArtifacts(restored) error: %v", err)
	}
	if len(restoredArtifacts) != 2 {
		t.Fatalf("ListItemArtifacts(restored) len = %d, want 2", len(restoredArtifacts))
	}
	if restoredArtifacts[0].ArtifactID != sourceArtifact.ID || restoredArtifacts[0].Role != "source" {
		t.Fatalf("restored primary artifact = %+v, want source artifact %d", restoredArtifacts[0], sourceArtifact.ID)
	}
}

func TestCreateAndListMailTriageReviews(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "triage.db"))
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer s.Close()
	account, err := s.CreateExternalAccount(SphereWork, ExternalProviderExchangeEWS, "TU Graz", nil)
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}
	first, err := s.CreateMailTriageReview(MailTriageReviewInput{AccountID: account.ID, Provider: account.Provider, MessageID: "m1", Folder: "Posteingang", Subject: "One", Sender: "alice@example.com", Action: "inbox"})
	if err != nil {
		t.Fatalf("CreateMailTriageReview(first) error: %v", err)
	}
	second, err := s.CreateMailTriageReview(MailTriageReviewInput{AccountID: account.ID, Provider: account.Provider, MessageID: "m2", Folder: "Junk-E-Mail", Subject: "Two", Sender: "spam@example.com", Action: "trash"})
	if err != nil {
		t.Fatalf("CreateMailTriageReview(second) error: %v", err)
	}
	third, err := s.CreateMailTriageReview(MailTriageReviewInput{AccountID: account.ID, Provider: account.Provider, MessageID: "m3", Folder: "Posteingang", Subject: "Three", Sender: "list@example.com", Action: "cc"})
	if err != nil {
		t.Fatalf("CreateMailTriageReview(third) error: %v", err)
	}
	if first.ID <= 0 || second.ID <= 0 || third.ID <= 0 {
		t.Fatalf("review ids = %d, %d, %d", first.ID, second.ID, third.ID)
	}
	reviews, err := s.ListMailTriageReviews(account.ID, 10)
	if err != nil {
		t.Fatalf("ListMailTriageReviews() error: %v", err)
	}
	if len(reviews) != 3 {
		t.Fatalf("reviews len = %d, want 3", len(reviews))
	}
	if reviews[0].MessageID != "m3" || reviews[0].Action != "cc" {
		t.Fatalf("reviews[0] = %#v", reviews[0])
	}
	if reviews[1].MessageID != "m2" || reviews[1].Action != "trash" {
		t.Fatalf("reviews[1] = %#v", reviews[1])
	}
	if reviews[2].MessageID != "m1" || reviews[2].Action != "inbox" {
		t.Fatalf("reviews[2] = %#v", reviews[2])
	}
	ids, err := s.ListMailTriageReviewedMessageIDs(account.ID, "Junk-E-Mail", 10)
	if err != nil {
		t.Fatalf("ListMailTriageReviewedMessageIDs() error: %v", err)
	}
	if len(ids) != 1 || ids[0] != "m2" {
		t.Fatalf("reviewed ids = %#v, want [m2]", ids)
	}
}

func createParticipantTestProject(t *testing.T, s *Store, key string) Workspace {
	t.Helper()
	project, err := s.CreateEnrichedWorkspace("Participant "+key, key, filepath.Join(t.TempDir(), key), "managed", "", "", false)
	if err != nil {
		t.Fatalf("CreateProject(%q) error: %v", key, err)
	}
	return project
}

func TestParticipantSessionLifecycle(t *testing.T) {
	s := newTestStore(t)
	project := createParticipantTestProject(t, s, "proj-1")
	sess, err := s.AddParticipantSession(project.WorkspacePath, `{"language":"en"}`)
	if err != nil {
		t.Fatalf("add session: %v", err)
	}
	if sess.ID == "" {
		t.Fatal("session id is empty")
	}
	if sess.WorkspacePath != project.WorkspacePath {
		t.Fatalf("project key = %q, want %q", sess.WorkspacePath, project.WorkspacePath)
	}
	if sess.WorkspaceID == 0 {
		t.Fatal("workspace id is zero")
	}
	if sess.StartedAt == 0 {
		t.Fatal("started_at is zero")
	}
	if sess.EndedAt != 0 {
		t.Fatalf("ended_at = %d, want 0", sess.EndedAt)
	}
	got, err := s.GetParticipantSession(sess.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if got.ID != sess.ID {
		t.Fatalf("get returned id = %q, want %q", got.ID, sess.ID)
	}
	sess2, err := s.AddParticipantSession(project.WorkspacePath, "{}")
	if err != nil {
		t.Fatalf("add second session: %v", err)
	}
	list, err := s.ListParticipantSessions(project.WorkspacePath)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("list length = %d, want 2", len(list))
	}
	allList, err := s.ListParticipantSessions("")
	if err != nil {
		t.Fatalf("list all sessions: %v", err)
	}
	if len(allList) != 2 {
		t.Fatalf("all list length = %d, want 2", len(allList))
	}
	if err := s.EndParticipantSession(sess2.ID); err != nil {
		t.Fatalf("end session: %v", err)
	}
	ended, err := s.GetParticipantSession(sess2.ID)
	if err != nil {
		t.Fatalf("get ended session: %v", err)
	}
	if ended.EndedAt == 0 {
		t.Fatal("ended_at should be non-zero after end")
	}
}

func TestParticipantSegmentCRUD(t *testing.T) {
	s := newTestStore(t)
	project := createParticipantTestProject(t, s, "proj-seg")
	sess, err := s.AddParticipantSession(project.WorkspacePath, "{}")
	if err != nil {
		t.Fatalf("add session: %v", err)
	}
	seg1, err := s.AddParticipantSegment(ParticipantSegment{SessionID: sess.ID, StartTS: 100, EndTS: 110, Speaker: "user-a", Text: "hello meeting", Model: "whisper-1", LatencyMS: 200})
	if err != nil {
		t.Fatalf("add segment: %v", err)
	}
	if seg1.ID == 0 {
		t.Fatal("segment id is zero")
	}
	if seg1.Status != "final" {
		t.Fatalf("status = %q, want final", seg1.Status)
	}
	seg2, err := s.AddParticipantSegment(ParticipantSegment{SessionID: sess.ID, StartTS: 200, EndTS: 210, Speaker: "user-b", Text: "world response"})
	if err != nil {
		t.Fatalf("add second segment: %v", err)
	}
	all, err := s.ListParticipantSegments(sess.ID, 0, 0)
	if err != nil {
		t.Fatalf("list segments: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("segment count = %d, want 2", len(all))
	}
	filtered, err := s.ListParticipantSegments(sess.ID, 150, 0)
	if err != nil {
		t.Fatalf("list segments with from: %v", err)
	}
	if len(filtered) != 1 {
		t.Fatalf("filtered count = %d, want 1", len(filtered))
	}
	if filtered[0].ID != seg2.ID {
		t.Fatalf("filtered segment id = %d, want %d", filtered[0].ID, seg2.ID)
	}
	results, err := s.SearchParticipantSegments(sess.ID, "meeting")
	if err != nil {
		t.Fatalf("search segments: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("search count = %d, want 1", len(results))
	}
	if results[0].Text != "hello meeting" {
		t.Fatalf("search text = %q", results[0].Text)
	}
}

func TestParticipantSegmentRejectsEndedSession(t *testing.T) {
	s := newTestStore(t)
	project := createParticipantTestProject(t, s, "proj-ended")
	sess, err := s.AddParticipantSession(project.WorkspacePath, "{}")
	if err != nil {
		t.Fatalf("add session: %v", err)
	}
	if err := s.EndParticipantSession(sess.ID); err != nil {
		t.Fatalf("end session: %v", err)
	}
	_, err = s.AddParticipantSegment(ParticipantSegment{SessionID: sess.ID, StartTS: 100, EndTS: 110, Text: "late transcript"})
	if !errors.Is(err, ErrParticipantSessionEnded) {
		t.Fatalf("AddParticipantSegment() error = %v, want %v", err, ErrParticipantSessionEnded)
	}
}

func TestParticipantEventCRUD(t *testing.T) {
	s := newTestStore(t)
	project := createParticipantTestProject(t, s, "proj-ev")
	sess, err := s.AddParticipantSession(project.WorkspacePath, "{}")
	if err != nil {
		t.Fatalf("add session: %v", err)
	}
	if err := s.AddParticipantEvent(sess.ID, 0, "session_started", `{"reason":"manual"}`); err != nil {
		t.Fatalf("add event: %v", err)
	}
	if err := s.AddParticipantEvent(sess.ID, 1, "segment_committed", `{"seg_id":1}`); err != nil {
		t.Fatalf("add event 2: %v", err)
	}
	events, err := s.ListParticipantEvents(sess.ID)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("event count = %d, want 2", len(events))
	}
	if events[0].EventType != "session_started" {
		t.Fatalf("event type = %q, want session_started", events[0].EventType)
	}
}

func TestParticipantRoomStateUpsert(t *testing.T) {
	s := newTestStore(t)
	project := createParticipantTestProject(t, s, "proj-room")
	sess, err := s.AddParticipantSession(project.WorkspacePath, "{}")
	if err != nil {
		t.Fatalf("add session: %v", err)
	}
	if err := s.UpsertParticipantRoomState(sess.ID, "initial summary", `["entity-a"]`, `["topic-1"]`); err != nil {
		t.Fatalf("upsert room state: %v", err)
	}
	state, err := s.GetParticipantRoomState(sess.ID)
	if err != nil {
		t.Fatalf("get room state: %v", err)
	}
	if state.SummaryText != "initial summary" {
		t.Fatalf("summary = %q", state.SummaryText)
	}
	if state.EntitiesJSON != `["entity-a"]` {
		t.Fatalf("entities = %q", state.EntitiesJSON)
	}
	if err := s.UpsertParticipantRoomState(sess.ID, "updated summary", `["entity-b"]`, `["topic-2"]`); err != nil {
		t.Fatalf("upsert overwrite: %v", err)
	}
	state2, err := s.GetParticipantRoomState(sess.ID)
	if err != nil {
		t.Fatalf("get updated room state: %v", err)
	}
	if state2.SummaryText != "updated summary" {
		t.Fatalf("updated summary = %q", state2.SummaryText)
	}
	if state2.ID != state.ID {
		t.Fatalf("upsert should keep same id: got %d, want %d", state2.ID, state.ID)
	}
}

func TestParticipantSessionValidation(t *testing.T) {
	s := newTestStore(t)
	_, err := s.AddParticipantSession("", "{}")
	if err == nil {
		t.Fatal("expected error for empty project key")
	}
	_, err = s.AddParticipantSegment(ParticipantSegment{SessionID: ""})
	if err == nil {
		t.Fatal("expected error for empty session id in segment")
	}
	err = s.AddParticipantEvent("", 0, "test", "{}")
	if err == nil {
		t.Fatal("expected error for empty session id in event")
	}
	err = s.UpsertParticipantRoomState("", "summary", "[]", "[]")
	if err == nil {
		t.Fatal("expected error for empty session id in room state")
	}
}

func TestParticipantSchemaMigrationAddsMissingColumns(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	legacyDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	legacySchema := `
CREATE TABLE participant_sessions (
  id TEXT PRIMARY KEY,
  workspace_path TEXT NOT NULL,
  started_at INTEGER NOT NULL,
  ended_at INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE participant_segments (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT NOT NULL,
  start_ts INTEGER NOT NULL
);
CREATE TABLE participant_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT NOT NULL,
  event_type TEXT NOT NULL,
  created_at INTEGER NOT NULL
);
CREATE TABLE participant_room_state (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT NOT NULL UNIQUE,
  updated_at INTEGER NOT NULL
);
`
	if _, err := legacyDB.Exec(legacySchema); err != nil {
		t.Fatalf("seed legacy schema: %v", err)
	}
	if err := legacyDB.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}
	s, err := New(dbPath)
	if err != nil {
		t.Fatalf("store.New() migration error: %v", err)
	}
	t.Cleanup(func() {
		_ = s.Close()
	})
	columns, err := s.TableColumns()
	if err != nil {
		t.Fatalf("TableColumns() error: %v", err)
	}
	assertColumnsPresent(t, columns, "participant_sessions", "id", "workspace_id", "started_at", "ended_at", "config_json")
	assertColumnsPresent(t, columns, "participant_segments", "id", "session_id", "start_ts", "end_ts", "speaker", "text", "model", "latency_ms", "committed_at", "status")
	assertColumnsPresent(t, columns, "participant_events", "id", "session_id", "segment_id", "event_type", "payload_json", "created_at")
	assertColumnsPresent(t, columns, "participant_room_state", "id", "session_id", "summary_text", "entities_json", "topic_timeline_json", "updated_at")
}
