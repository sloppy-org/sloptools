package store

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestStorePushRegistrationCRUD(t *testing.T) {
	s := newTestStore(t)

	first, err := s.UpsertPushRegistration(PushRegistration{
		SessionID:   "chat-1",
		WorkspaceID: 7,
		Platform:    "apns",
		DeviceToken: "device-1",
		DeviceLabel: "iPhone",
	})
	if err != nil {
		t.Fatalf("UpsertPushRegistration(create) error: %v", err)
	}
	if first.ID <= 0 {
		t.Fatalf("registration id = %d, want positive", first.ID)
	}

	updated, err := s.UpsertPushRegistration(PushRegistration{
		SessionID:   "chat-1",
		WorkspaceID: 7,
		Platform:    "apns",
		DeviceToken: "device-1",
		DeviceLabel: "iPad",
	})
	if err != nil {
		t.Fatalf("UpsertPushRegistration(update) error: %v", err)
	}
	if updated.ID != first.ID {
		t.Fatalf("updated id = %d, want %d", updated.ID, first.ID)
	}
	if updated.DeviceLabel != "iPad" {
		t.Fatalf("device label = %q, want %q", updated.DeviceLabel, "iPad")
	}

	if _, err := s.UpsertPushRegistration(PushRegistration{
		WorkspaceID: 7,
		Platform:    "fcm",
		DeviceToken: "device-2",
	}); err != nil {
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
