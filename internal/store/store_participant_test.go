package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

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

	seg1, err := s.AddParticipantSegment(ParticipantSegment{
		SessionID: sess.ID,
		StartTS:   100,
		EndTS:     110,
		Speaker:   "user-a",
		Text:      "hello meeting",
		Model:     "whisper-1",
		LatencyMS: 200,
	})
	if err != nil {
		t.Fatalf("add segment: %v", err)
	}
	if seg1.ID == 0 {
		t.Fatal("segment id is zero")
	}
	if seg1.Status != "final" {
		t.Fatalf("status = %q, want final", seg1.Status)
	}

	seg2, err := s.AddParticipantSegment(ParticipantSegment{
		SessionID: sess.ID,
		StartTS:   200,
		EndTS:     210,
		Speaker:   "user-b",
		Text:      "world response",
	})
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

	_, err = s.AddParticipantSegment(ParticipantSegment{
		SessionID: sess.ID,
		StartTS:   100,
		EndTS:     110,
		Text:      "late transcript",
	})
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

func TestParticipantSchemaMigrationMovesWorkspacePathSessionsToWorkspaceID(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy-migrate.db")
	legacyDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}

	legacySchema := `
CREATE TABLE workspaces (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  dir_path TEXT NOT NULL UNIQUE,
  is_active INTEGER NOT NULL DEFAULT 0,
  is_daily INTEGER NOT NULL DEFAULT 0,
  daily_date TEXT,
  mcp_url TEXT NOT NULL DEFAULT '',
  canvas_session_id TEXT NOT NULL DEFAULT '',
  chat_model TEXT NOT NULL DEFAULT '',
  chat_model_reasoning_effort TEXT NOT NULL DEFAULT '',
  companion_config_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE TABLE participant_sessions (
  id TEXT PRIMARY KEY,
  workspace_path TEXT NOT NULL,
  started_at INTEGER NOT NULL,
  ended_at INTEGER NOT NULL DEFAULT 0,
  config_json TEXT NOT NULL DEFAULT '{}'
);
`
	if _, err := legacyDB.Exec(legacySchema); err != nil {
		t.Fatalf("seed legacy schema: %v", err)
	}
	rootPath := filepath.Join(t.TempDir(), "legacy-project")
	if _, err := legacyDB.Exec(
		`INSERT INTO workspaces (id, name, dir_path) VALUES (?,?,?)`,
		1, "Legacy", rootPath,
	); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	if _, err := legacyDB.Exec(
		`INSERT INTO participant_sessions (id, workspace_path, started_at, ended_at, config_json) VALUES (?,?,?,?,?)`,
		"psess-legacy", rootPath, 100, 0, `{"language":"en"}`,
	); err != nil {
		t.Fatalf("insert participant session: %v", err)
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

	session, err := s.GetParticipantSession("psess-legacy")
	if err != nil {
		t.Fatalf("GetParticipantSession() error: %v", err)
	}
	if session.WorkspaceID == 0 {
		t.Fatal("workspace id is zero after migration")
	}
	if session.WorkspacePath != rootPath {
		t.Fatalf("project key = %q, want %q", session.WorkspacePath, rootPath)
	}

	columns, err := s.TableColumns()
	if err != nil {
		t.Fatalf("TableColumns() error: %v", err)
	}
	if containsString(columns["participant_sessions"], "workspace_path") {
		t.Fatalf("participant_sessions columns still include workspace_path: %v", columns["participant_sessions"])
	}
}

func TestParticipantSchemaOmitsAudioPersistenceColumns(t *testing.T) {
	s := newTestStore(t)

	columns, err := s.TableColumns()
	if err != nil {
		t.Fatalf("TableColumns() error: %v", err)
	}

	disallowed := []string{"audio", "blob", "path", "hash", "fingerprint"}
	for _, table := range []string{
		"participant_sessions",
		"participant_segments",
		"participant_events",
		"participant_room_state",
	} {
		for _, col := range columns[table] {
			for _, bad := range disallowed {
				if strings.Contains(col, bad) {
					t.Fatalf("%s should not contain %q column, got %q", table, bad, col)
				}
			}
		}
	}
}

func assertColumnsPresent(t *testing.T, columns map[string][]string, table string, want ...string) {
	t.Helper()

	got := make(map[string]bool, len(columns[table]))
	for _, col := range columns[table] {
		got[col] = true
	}
	for _, name := range want {
		if !got[name] {
			t.Fatalf("%s is missing column %q: got %v", table, name, columns[table])
		}
	}
}
