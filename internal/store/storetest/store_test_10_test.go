package store_test

import (
	"database/sql"
	"errors"
	. "github.com/sloppy-org/sloptools/internal/store"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var _ *Store

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
	if _, err := legacyDB.Exec(`INSERT INTO workspaces (id, name, dir_path) VALUES (?,?,?)`, 1, "Legacy", rootPath); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	if _, err := legacyDB.Exec(`INSERT INTO participant_sessions (id, workspace_path, started_at, ended_at, config_json) VALUES (?,?,?,?,?)`, "psess-legacy", rootPath, 100, 0, `{"language":"en"}`); err != nil {
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
	for _, table := range []string{"participant_sessions", "participant_segments", "participant_events", "participant_room_state"} {
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

func TestStorePushRegistrationCRUD(t *testing.T) {
	s := newTestStore(t)
	first, err := s.UpsertPushRegistration(PushRegistration{SessionID: "chat-1", WorkspaceID: 7, Platform: "apns", DeviceToken: "device-1", DeviceLabel: "iPhone"})
	if err != nil {
		t.Fatalf("UpsertPushRegistration(create) error: %v", err)
	}
	if first.ID <= 0 {
		t.Fatalf("registration id = %d, want positive", first.ID)
	}
	updated, err := s.UpsertPushRegistration(PushRegistration{SessionID: "chat-1", WorkspaceID: 7, Platform: "apns", DeviceToken: "device-1", DeviceLabel: "iPad"})
	if err != nil {
		t.Fatalf("UpsertPushRegistration(update) error: %v", err)
	}
	if updated.ID != first.ID {
		t.Fatalf("updated id = %d, want %d", updated.ID, first.ID)
	}
	if updated.DeviceLabel != "iPad" {
		t.Fatalf("device label = %q, want %q", updated.DeviceLabel, "iPad")
	}
	if _, err := s.UpsertPushRegistration(PushRegistration{WorkspaceID: 7, Platform: "fcm", DeviceToken: "device-2"}); err != nil {
		t.Fatalf("UpsertPushRegistration(global fcm) error: %v", err)
	}
	all, err := s.ListPushRegistrations()
	if err != nil {
		t.Fatalf("ListPushRegistrations() error: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("ListPushRegistrations() len = %d, want 2", len(all))
	}
	sessionRegs, err := s.ListPushRegistrationsForChatSession("chat-1", 7)
	if err != nil {
		t.Fatalf("ListPushRegistrationsForChatSession() error: %v", err)
	}
	if len(sessionRegs) != 2 {
		t.Fatalf("ListPushRegistrationsForChatSession() len = %d, want 2", len(sessionRegs))
	}
}

func TestStoreMigratesPushRegistrationsOnExistingDatabase(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "slopshell.db")
	legacy := openLegacySQLiteDB(t, dbPath, `
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
`)
	_ = legacy.Close()
	s, err := New(dbPath)
	if err != nil {
		t.Fatalf("store.New() error: %v", err)
	}
	t.Cleanup(func() {
		_ = s.Close()
	})
	columns, err := s.TableColumns()
	if err != nil {
		t.Fatalf("TableColumns() error: %v", err)
	}
	for _, name := range []string{"id", "session_id", "workspace_id", "platform", "device_token", "device_label", "created_at", "updated_at"} {
		if !containsString(columns["push_registrations"], name) {
			t.Fatalf("push_registrations missing %q: %#v", name, columns["push_registrations"])
		}
	}
}

func openLegacySQLiteDB(t *testing.T, path string, schema string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open() error: %v", err)
	}
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		t.Fatalf("seed legacy schema: %v", err)
	}
	return db
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(filepath.Join(t.TempDir(), "slopshell.db"))
	if err != nil {
		t.Fatalf("store.New() error: %v", err)
	}
	t.Cleanup(func() {
		_ = s.Close()
	})
	return s
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(target)) {
			return true
		}
	}
	return false
}

func TestStoreAdminPasswordAndAuthLifecycle(t *testing.T) {
	s := newTestStore(t)
	if s.HasAdminPassword() {
		t.Fatalf("HasAdminPassword() = true, want false")
	}
	if err := s.SetAdminPassword("short"); err == nil {
		t.Fatalf("expected short password error")
	}
	if err := s.SetAdminPassword("very-strong-pass"); err != nil {
		t.Fatalf("SetAdminPassword() error: %v", err)
	}
	if !s.HasAdminPassword() {
		t.Fatalf("HasAdminPassword() = false, want true")
	}
	if !s.VerifyAdminPassword("very-strong-pass") {
		t.Fatalf("VerifyAdminPassword(correct) = false, want true")
	}
	if s.VerifyAdminPassword("wrong-password") {
		t.Fatalf("VerifyAdminPassword(wrong) = true, want false")
	}
	if err := s.AddAuthSession("tok-1"); err != nil {
		t.Fatalf("AddAuthSession() error: %v", err)
	}
	if !s.HasAuthSession("tok-1") {
		t.Fatalf("HasAuthSession(tok-1) = false, want true")
	}
	if err := s.SetAdminPassword("another-strong-pass"); err != nil {
		t.Fatalf("SetAdminPassword(second) error: %v", err)
	}
	if s.HasAuthSession("tok-1") {
		t.Fatalf("expected auth sessions to be cleared when admin password changes")
	}
	if err := s.AddAuthSession(""); err == nil {
		t.Fatalf("expected AddAuthSession(empty) to fail")
	}
	if err := s.DeleteAuthSession(""); err != nil {
		t.Fatalf("DeleteAuthSession(empty) should be noop: %v", err)
	}
}

func TestStoreHostAndRemoteSessionCRUD(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.AddHost(HostConfig{}); err == nil {
		t.Fatalf("expected AddHost() validation error")
	}
	h2, err := s.AddHost(HostConfig{Name: "zeta", Hostname: "zeta.local", Port: 0, Username: "u2", KeyPath: "/tmp/key2", ProjectDir: "/tmp/p2"})
	if err != nil {
		t.Fatalf("AddHost(zeta) error: %v", err)
	}
	if h2.Port != 22 {
		t.Fatalf("default port = %d, want 22", h2.Port)
	}
	h1, err := s.AddHost(HostConfig{Name: "alpha", Hostname: "alpha.local", Port: 2202, Username: "u1", KeyPath: "/tmp/key1", ProjectDir: "/tmp/p1"})
	if err != nil {
		t.Fatalf("AddHost(alpha) error: %v", err)
	}
	list, err := s.ListHosts()
	if err != nil {
		t.Fatalf("ListHosts() error: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("ListHosts() len = %d, want 2", len(list))
	}
	if list[0].Name != "alpha" || list[1].Name != "zeta" {
		t.Fatalf("ListHosts() should be name-sorted, got %#v", []string{list[0].Name, list[1].Name})
	}
	updated, err := s.UpdateHost(h1.ID, map[string]interface{}{"username": "updated-user", "port": 2222})
	if err != nil {
		t.Fatalf("UpdateHost() error: %v", err)
	}
	if updated.Username != "updated-user" || updated.Port != 2222 {
		t.Fatalf("UpdateHost() did not apply fields: %+v", updated)
	}
	if err := s.AddRemoteSession("sid-1", h1.ID); err != nil {
		t.Fatalf("AddRemoteSession(sid-1) error: %v", err)
	}
	if err := s.AddRemoteSession("sid-2", h2.ID); err != nil {
		t.Fatalf("AddRemoteSession(sid-2) error: %v", err)
	}
	remote, err := s.ListRemoteSessions()
	if err != nil {
		t.Fatalf("ListRemoteSessions() error: %v", err)
	}
	if len(remote) != 2 {
		t.Fatalf("ListRemoteSessions() len = %d, want 2", len(remote))
	}
	if err := s.DeleteRemoteSession("sid-1"); err != nil {
		t.Fatalf("DeleteRemoteSession() error: %v", err)
	}
	remote, err = s.ListRemoteSessions()
	if err != nil {
		t.Fatalf("ListRemoteSessions(after delete) error: %v", err)
	}
	if len(remote) != 1 {
		t.Fatalf("ListRemoteSessions() len after delete = %d, want 1", len(remote))
	}
	if err := s.DeleteHost(h1.ID); err != nil {
		t.Fatalf("DeleteHost() error: %v", err)
	}
	if _, err := s.GetHost(h1.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetHost(deleted) error = %v, want sql.ErrNoRows", err)
	}
}

func TestStoreProjectCompanionConfigPersistsAcrossReopen(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "slopshell.db")
	s, err := New(dbPath)
	if err != nil {
		t.Fatalf("store.New() error: %v", err)
	}
	root := filepath.Join(t.TempDir(), "workspace")
	project, err := s.CreateEnrichedWorkspace("Workspace A", "key-a", root, "managed", "", "", false)
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}
	if err := s.UpdateEnrichedWorkspaceCompanionConfig(WorkspaceIDString(project.ID), `{"companion_enabled":false,"language":"de","idle_surface":"black"}`); err != nil {
		t.Fatalf("UpdateProjectCompanionConfig() error: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}
	reopened, err := New(dbPath)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer func() {
		_ = reopened.Close()
	}()
	got, err := reopened.GetEnrichedWorkspace(WorkspaceIDString(project.ID))
	if err != nil {
		t.Fatalf("GetProject() after reopen error: %v", err)
	}
	if strings.TrimSpace(got.CompanionConfigJSON) != `{"companion_enabled":false,"language":"de","idle_surface":"black"}` {
		t.Fatalf("CompanionConfigJSON after reopen = %q", got.CompanionConfigJSON)
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("expected db at %s: %v", dbPath, err)
	}
}
