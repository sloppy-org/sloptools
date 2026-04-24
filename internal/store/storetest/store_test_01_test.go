package store_test

import (
	"database/sql"
	. "github.com/sloppy-org/sloptools/internal/store"
	_ "modernc.org/sqlite"
	"path/filepath"
	"testing"
)

var _ *Store

func foreignKeyTargetSet(t *testing.T, db *sql.DB, table string) map[string]bool {
	t.Helper()
	rows, err := db.Query(`PRAGMA foreign_key_list(` + table + `)`)
	if err != nil {
		t.Fatalf("PRAGMA foreign_key_list(%s) error: %v", table, err)
	}
	defer rows.Close()
	targets := map[string]bool{}
	for rows.Next() {
		var (
			id, seq                                     int
			target, from, to, onUpdate, onDelete, match string
		)
		if err := rows.Scan(&id, &seq, &target, &from, &to, &onUpdate, &onDelete, &match); err != nil {
			t.Fatalf("scan foreign key for %s: %v", table, err)
		}
		targets[target] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate foreign keys for %s: %v", table, err)
	}
	return targets
}

func TestStoreMigratesDomainTablesOnFreshDatabase(t *testing.T) {
	s := newTestStore(t)
	var foreignKeys int
	if err := s.DB().QueryRow(`PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
		t.Fatalf("read PRAGMA foreign_keys: %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("PRAGMA foreign_keys = %d, want 1", foreignKeys)
	}
	columns, err := s.TableColumns()
	if err != nil {
		t.Fatalf("TableColumns() error: %v", err)
	}
	for table, want := range map[string][]string{"workspaces": {"id", "name", "dir_path", "is_active", "is_daily", "daily_date", "mcp_url", "canvas_session_id", "chat_model", "chat_model_reasoning_effort", "created_at", "updated_at"}, "contexts": {"id", "name", "color", "parent_id", "created_at"}, "context_items": {"context_id", "item_id"}, "context_artifacts": {"context_id", "artifact_id"}, "context_workspaces": {"context_id", "workspace_id"}, "context_external_accounts": {"context_id", "account_id"}, "context_external_container_mappings": {"context_id", "mapping_id"}, "context_time_entries": {"context_id", "time_entry_id"}, "actors": {"id", "name", "kind", "email", "provider", "provider_ref", "meta_json", "created_at"}, "artifacts": {"id", "kind", "ref_path", "ref_url", "title", "meta_json", "created_at", "updated_at"}, "external_accounts": {"id", "provider", "label", "config_json", "enabled", "created_at", "updated_at"}, "external_container_mappings": {"id", "provider", "container_type", "container_ref", "workspace_id"}, "item_artifacts": {"item_id", "artifact_id", "role", "created_at"}, "workspace_artifact_links": {"workspace_id", "artifact_id", "created_at"}, "external_bindings": {"id", "account_id", "provider", "object_type", "remote_id", "item_id", "artifact_id", "container_ref", "remote_updated_at", "last_synced_at"}, "batch_runs": {"id", "workspace_id", "started_at", "finished_at", "config_json", "status"}, "batch_run_items": {"batch_id", "item_id", "status", "pr_number", "pr_url", "error_msg", "started_at", "finished_at"}, "workspace_watches": {"workspace_id", "config_json", "poll_interval_seconds", "enabled", "current_batch_id", "created_at", "updated_at"}, "items": {"id", "title", "state", "workspace_id", "artifact_id", "actor_id", "visible_after", "follow_up_at", "source", "source_ref", "review_target", "reviewer", "reviewed_at", "created_at", "updated_at"}, "time_entries": {"id", "workspace_id", "started_at", "ended_at", "activity", "notes"}} {
		got := make(map[string]bool, len(columns[table]))
		for _, name := range columns[table] {
			got[name] = true
		}
		for _, name := range want {
			if !got[name] {
				t.Fatalf("table %s missing column %s: %#v", table, name, columns[table])
			}
		}
	}
	targets := map[string]bool{}
	rows, err := s.DB().Query(`PRAGMA foreign_key_list(items)`)
	if err != nil {
		t.Fatalf("PRAGMA foreign_key_list(items) error: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id, seq                                    int
			table, from, to, onUpdate, onDelete, match string
		)
		if err := rows.Scan(&id, &seq, &table, &from, &to, &onUpdate, &onDelete, &match); err != nil {
			t.Fatalf("scan foreign key: %v", err)
		}
		targets[table] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate foreign keys: %v", err)
	}
	for _, table := range []string{"workspaces", "artifacts", "actors"} {
		if !targets[table] {
			t.Fatalf("items missing foreign key to %s", table)
		}
	}
}

func TestStoreMigratesDomainTablesOnExistingDatabase(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "slopshell.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error: %v", err)
	}
	legacySchema := `
CREATE TABLE projects (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  workspace_path TEXT NOT NULL UNIQUE,
  root_path TEXT NOT NULL UNIQUE,
  kind TEXT NOT NULL DEFAULT 'managed',
  is_default INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE TABLE chat_messages (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT NOT NULL,
  role TEXT NOT NULL,
  content_markdown TEXT NOT NULL DEFAULT '',
  content_plain TEXT NOT NULL DEFAULT '',
  render_format TEXT NOT NULL DEFAULT 'markdown',
  created_at INTEGER NOT NULL
);
`
	if _, err := db.Exec(legacySchema); err != nil {
		_ = db.Close()
		t.Fatalf("seed legacy schema: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}
	s, err := New(dbPath)
	if err != nil {
		t.Fatalf("store.New() on legacy db error: %v", err)
	}
	t.Cleanup(func() {
		_ = s.Close()
	})
	columns, err := s.TableColumns()
	if err != nil {
		t.Fatalf("TableColumns() error: %v", err)
	}
	for _, table := range []string{"workspaces", "contexts", "context_items", "context_artifacts", "context_workspaces", "context_external_accounts", "context_external_container_mappings", "context_time_entries", "actors", "artifacts", "external_accounts", "external_container_mappings", "item_artifacts", "workspace_artifact_links", "external_bindings", "batch_runs", "batch_run_items", "items", "time_entries"} {
		if _, ok := columns[table]; !ok {
			t.Fatalf("expected migrated table %s to exist", table)
		}
	}
}

func TestStoreMigratesExistingItemsTableToAllowSomeday(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "slopshell.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error: %v", err)
	}
	schema := `
CREATE TABLE items (
  id INTEGER PRIMARY KEY,
  title TEXT NOT NULL,
  state TEXT NOT NULL DEFAULT 'inbox' CHECK (state IN ('inbox', 'waiting', 'done')),
  workspace_id INTEGER,
  artifact_id INTEGER,
  actor_id INTEGER,
  visible_after TEXT,
  follow_up_at TEXT,
  source TEXT,
  source_ref TEXT,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
INSERT INTO items (title, state) VALUES ('legacy waiting', 'waiting');
`
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		t.Fatalf("seed legacy items schema: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}
	s, err := New(dbPath)
	if err != nil {
		t.Fatalf("store.New() on legacy items db error: %v", err)
	}
	t.Cleanup(func() {
		_ = s.Close()
	})
	item, err := s.GetItem(1)
	if err != nil {
		t.Fatalf("GetItem(legacy row) error: %v", err)
	}
	if item.State != ItemStateWaiting {
		t.Fatalf("legacy item state = %q, want %q", item.State, ItemStateWaiting)
	}
	if _, err := s.CreateItem("someday migration", ItemOptions{State: ItemStateSomeday}); err != nil {
		t.Fatalf("CreateItem(someday) after migration error: %v", err)
	}
}

func TestStoreMigratesProjectRemovalWithoutLegacyForeignKeys(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "slopshell.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error: %v", err)
	}
	legacySchema := `
CREATE TABLE projects (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL
);
CREATE TABLE workspaces (
  id INTEGER PRIMARY KEY,
  project_id TEXT REFERENCES projects(id) ON DELETE SET NULL,
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
CREATE TABLE items (
  id INTEGER PRIMARY KEY,
  project_id TEXT REFERENCES projects(id) ON DELETE SET NULL,
  title TEXT NOT NULL,
  state TEXT NOT NULL DEFAULT 'inbox',
  workspace_id INTEGER REFERENCES workspaces(id) ON DELETE SET NULL,
  artifact_id INTEGER,
  actor_id INTEGER,
  visible_after TEXT,
  follow_up_at TEXT,
  source TEXT,
  source_ref TEXT,
  review_target TEXT,
  reviewer TEXT,
  reviewed_at TEXT,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE TABLE time_entries (
  id INTEGER PRIMARY KEY,
  project_id TEXT REFERENCES projects(id) ON DELETE SET NULL,
  workspace_id INTEGER REFERENCES workspaces(id) ON DELETE SET NULL,
  started_at TEXT NOT NULL,
  ended_at TEXT,
  activity TEXT NOT NULL DEFAULT '',
  notes TEXT
);
CREATE TABLE external_container_mappings (
  id INTEGER PRIMARY KEY,
  project_id TEXT REFERENCES projects(id) ON DELETE SET NULL,
  provider TEXT NOT NULL,
  container_type TEXT NOT NULL,
  container_ref TEXT NOT NULL,
  workspace_id INTEGER REFERENCES workspaces(id) ON DELETE SET NULL
);`
	if _, err := db.Exec(legacySchema); err != nil {
		_ = db.Close()
		t.Fatalf("seed legacy project-removal schema: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}
	s, err := New(dbPath)
	if err != nil {
		t.Fatalf("store.New() on project-removal legacy db error: %v", err)
	}
	t.Cleanup(func() {
		_ = s.Close()
	})
	for table, expected := range map[string][]string{"chat_sessions": {"workspaces"}, "context_items": {"contexts", "items"}, "context_workspaces": {"contexts", "workspaces"}, "context_external_container_mappings": {"contexts", "external_container_mappings"}, "context_time_entries": {"contexts", "time_entries"}} {
		targets := foreignKeyTargetSet(t, s.DB(), table)
		for _, want := range expected {
			if !targets[want] {
				t.Fatalf("%s foreign keys = %#v, want %s", table, targets, want)
			}
		}
		if targets["items_project_legacy"] || targets["workspaces_project_legacy"] || targets["time_entries_project_legacy"] || targets["external_container_mappings_project_legacy"] {
			t.Fatalf("%s retained legacy foreign keys: %#v", table, targets)
		}
	}
}

func TestStoreRepairsBrokenProjectLegacyForeignKeys(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "slopshell.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error: %v", err)
	}
	brokenSchema := `
PRAGMA foreign_keys = OFF;
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
CREATE TABLE artifacts (
  id INTEGER PRIMARY KEY,
  kind TEXT NOT NULL,
  ref_path TEXT,
  ref_url TEXT,
  title TEXT,
  meta_json TEXT,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE TABLE actors (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  kind TEXT NOT NULL,
  email TEXT,
  provider TEXT,
  provider_ref TEXT,
  meta_json TEXT,
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE TABLE external_accounts (
  id INTEGER PRIMARY KEY,
  provider TEXT NOT NULL,
  label TEXT NOT NULL,
  config_json TEXT NOT NULL DEFAULT '{}',
  enabled INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE TABLE contexts (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  color TEXT NOT NULL DEFAULT '',
  parent_id INTEGER REFERENCES contexts(id) ON DELETE SET NULL,
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE TABLE external_container_mappings (
  id INTEGER PRIMARY KEY,
  provider TEXT NOT NULL,
  container_type TEXT NOT NULL,
  container_ref TEXT NOT NULL,
  workspace_id INTEGER REFERENCES workspaces(id) ON DELETE SET NULL
);
CREATE TABLE items (
  id INTEGER PRIMARY KEY,
  title TEXT NOT NULL,
  state TEXT NOT NULL DEFAULT 'inbox' CHECK (state IN ('inbox', 'waiting', 'someday', 'done')),
  workspace_id INTEGER REFERENCES workspaces(id) ON DELETE SET NULL,
  artifact_id INTEGER REFERENCES artifacts(id) ON DELETE SET NULL,
  actor_id INTEGER REFERENCES actors(id) ON DELETE SET NULL,
  visible_after TEXT,
  follow_up_at TEXT,
  source TEXT,
  source_ref TEXT,
  review_target TEXT CHECK (review_target IN ('agent', 'github', 'email')),
  reviewer TEXT,
  reviewed_at TEXT,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE TABLE time_entries (
  id INTEGER PRIMARY KEY,
  workspace_id INTEGER REFERENCES workspaces(id) ON DELETE SET NULL,
  started_at TEXT NOT NULL,
  ended_at TEXT,
  activity TEXT NOT NULL DEFAULT '',
  notes TEXT
);
CREATE TABLE item_artifacts (
  item_id INTEGER NOT NULL REFERENCES "items_project_legacy"(id) ON DELETE CASCADE,
  artifact_id INTEGER NOT NULL REFERENCES artifacts(id) ON DELETE CASCADE,
  role TEXT NOT NULL DEFAULT 'related' CHECK (role IN ('source', 'related', 'output')),
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  PRIMARY KEY (item_id, artifact_id)
);
CREATE INDEX idx_item_artifacts_artifact_id
  ON item_artifacts(artifact_id);
CREATE TABLE context_items (
  context_id INTEGER NOT NULL REFERENCES contexts(id) ON DELETE CASCADE,
  item_id INTEGER NOT NULL REFERENCES "items_project_legacy"(id) ON DELETE CASCADE,
  PRIMARY KEY (context_id, item_id)
);
CREATE TABLE chat_sessions (
  id TEXT PRIMARY KEY,
  workspace_id INTEGER NOT NULL UNIQUE REFERENCES "workspaces_project_legacy"(id) ON DELETE CASCADE,
  app_thread_id TEXT NOT NULL DEFAULT '',
  mode TEXT NOT NULL DEFAULT 'chat',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE TABLE app_state (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
INSERT INTO workspaces (id, name, dir_path, is_active) VALUES (1, 'main', '/tmp/main', 1);
INSERT INTO artifacts (id, kind, title) VALUES (1, 'file', 'artifact');
INSERT INTO actors (id, name, kind) VALUES (1, 'Ada', 'human');
INSERT INTO external_accounts (id, provider, label) VALUES (1, 'github', 'main');
INSERT INTO contexts (id, name) VALUES (1, 'work');
INSERT INTO external_container_mappings (id, provider, container_type, container_ref, workspace_id) VALUES (1, 'github', 'repo', 'owner/repo', 1);
INSERT INTO items (id, title, workspace_id, artifact_id, actor_id) VALUES (1, 'broken ref', 1, 1, 1);
INSERT INTO time_entries (id, workspace_id, started_at, activity) VALUES (1, 1, datetime('now'), 'focus');
INSERT INTO item_artifacts (item_id, artifact_id, role) VALUES (1, 1, 'source');
INSERT INTO context_items (context_id, item_id) VALUES (1, 1);
INSERT INTO chat_sessions (id, workspace_id, created_at, updated_at) VALUES ('session-1', 1, 1, 1);
PRAGMA foreign_keys = ON;`
	if _, err := db.Exec(brokenSchema); err != nil {
		_ = db.Close()
		t.Fatalf("seed broken legacy foreign key schema: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close broken db: %v", err)
	}
	s, err := New(dbPath)
	if err != nil {
		t.Fatalf("store.New() on broken legacy foreign key db error: %v", err)
	}
	t.Cleanup(func() {
		_ = s.Close()
	})
	for table, expected := range map[string][]string{"item_artifacts": {"items", "artifacts"}, "context_items": {"contexts", "items"}, "chat_sessions": {"workspaces"}} {
		targets := foreignKeyTargetSet(t, s.DB(), table)
		for _, want := range expected {
			if !targets[want] {
				t.Fatalf("%s foreign keys = %#v, want %s", table, targets, want)
			}
		}
		if targets["items_project_legacy"] || targets["workspaces_project_legacy"] {
			t.Fatalf("%s retained broken legacy foreign keys: %#v", table, targets)
		}
	}
	var count int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM item_artifacts WHERE item_id = 1 AND artifact_id = 1`).Scan(&count); err != nil {
		t.Fatalf("count repaired item_artifacts rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("item_artifacts row count = %d, want 1", count)
	}
}

func TestItemSchemaAllowsNilOptionalFields(t *testing.T) {
	s := newTestStore(t)
	res, err := s.DB().Exec(`INSERT INTO items (title) VALUES ('triage me')`)
	if err != nil {
		t.Fatalf("insert item without optional fields: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId() error: %v", err)
	}
	var (
		title                            string
		sphere                           string
		workspaceID, artifactID, actorID sql.NullInt64
		visibleAfter, followUpAt         sql.NullString
		source, sourceRef                sql.NullString
	)
	err = s.DB().QueryRow(`
SELECT title, workspace_id,
  `+ScopedContextSelect("context_items", "item_id", "items.id")+`,
  artifact_id, actor_id, visible_after, follow_up_at, source, source_ref
FROM items
WHERE id = ?
`, id).Scan(&title, &workspaceID, &sphere, &artifactID, &actorID, &visibleAfter, &followUpAt, &source, &sourceRef)
	if err != nil {
		t.Fatalf("query item: %v", err)
	}
	if title != "triage me" {
		t.Fatalf("title = %q, want triage me", title)
	}
	if sphere != SpherePrivate {
		t.Fatalf("sphere = %q, want %q", sphere, SpherePrivate)
	}
	if workspaceID.Valid || artifactID.Valid || actorID.Valid || visibleAfter.Valid || followUpAt.Valid || source.Valid || sourceRef.Valid {
		t.Fatalf("expected optional fields to remain NULL, got workspace=%v artifact=%v actor=%v visible_after=%v follow_up_at=%v source=%v source_ref=%v", workspaceID, artifactID, actorID, visibleAfter, followUpAt, source, sourceRef)
	}
}
