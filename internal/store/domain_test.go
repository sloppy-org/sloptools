package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

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
	if err := s.db.QueryRow(`PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
		t.Fatalf("read PRAGMA foreign_keys: %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("PRAGMA foreign_keys = %d, want 1", foreignKeys)
	}

	columns, err := s.TableColumns()
	if err != nil {
		t.Fatalf("TableColumns() error: %v", err)
	}
	for table, want := range map[string][]string{
		"workspaces":                          {"id", "name", "dir_path", "is_active", "is_daily", "daily_date", "mcp_url", "canvas_session_id", "chat_model", "chat_model_reasoning_effort", "created_at", "updated_at"},
		"contexts":                            {"id", "name", "color", "parent_id", "created_at"},
		"context_items":                       {"context_id", "item_id"},
		"context_artifacts":                   {"context_id", "artifact_id"},
		"context_workspaces":                  {"context_id", "workspace_id"},
		"context_external_accounts":           {"context_id", "account_id"},
		"context_external_container_mappings": {"context_id", "mapping_id"},
		"context_time_entries":                {"context_id", "time_entry_id"},
		"actors":                              {"id", "name", "kind", "email", "provider", "provider_ref", "meta_json", "created_at"},
		"artifacts":                           {"id", "kind", "ref_path", "ref_url", "title", "meta_json", "created_at", "updated_at"},
		"external_accounts":                   {"id", "provider", "label", "config_json", "enabled", "created_at", "updated_at"},
		"external_container_mappings":         {"id", "provider", "container_type", "container_ref", "workspace_id"},
		"item_artifacts":                      {"item_id", "artifact_id", "role", "created_at"},
		"workspace_artifact_links":            {"workspace_id", "artifact_id", "created_at"},
		"external_bindings":                   {"id", "account_id", "provider", "object_type", "remote_id", "item_id", "artifact_id", "container_ref", "remote_updated_at", "last_synced_at"},
		"batch_runs":                          {"id", "workspace_id", "started_at", "finished_at", "config_json", "status"},
		"batch_run_items":                     {"batch_id", "item_id", "status", "pr_number", "pr_url", "error_msg", "started_at", "finished_at"},
		"workspace_watches":                   {"workspace_id", "config_json", "poll_interval_seconds", "enabled", "current_batch_id", "created_at", "updated_at"},
		"items":                               {"id", "title", "state", "workspace_id", "artifact_id", "actor_id", "visible_after", "follow_up_at", "source", "source_ref", "review_target", "reviewer", "reviewed_at", "created_at", "updated_at"},
		"time_entries":                        {"id", "workspace_id", "started_at", "ended_at", "activity", "notes"},
	} {
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
	rows, err := s.db.Query(`PRAGMA foreign_key_list(items)`)
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

	for table, expected := range map[string][]string{
		"chat_sessions":                       {"workspaces"},
		"context_items":                       {"contexts", "items"},
		"context_workspaces":                  {"contexts", "workspaces"},
		"context_external_container_mappings": {"contexts", "external_container_mappings"},
		"context_time_entries":                {"contexts", "time_entries"},
	} {
		targets := foreignKeyTargetSet(t, s.db, table)
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

	for table, expected := range map[string][]string{
		"item_artifacts": {"items", "artifacts"},
		"context_items":  {"contexts", "items"},
		"chat_sessions":  {"workspaces"},
	} {
		targets := foreignKeyTargetSet(t, s.db, table)
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
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM item_artifacts WHERE item_id = 1 AND artifact_id = 1`).Scan(&count); err != nil {
		t.Fatalf("count repaired item_artifacts rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("item_artifacts row count = %d, want 1", count)
	}
}

func TestItemSchemaAllowsNilOptionalFields(t *testing.T) {
	s := newTestStore(t)

	res, err := s.db.Exec(`INSERT INTO items (title) VALUES ('triage me')`)
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
	err = s.db.QueryRow(`
SELECT title, workspace_id,
  `+scopedContextSelect("context_items", "item_id", "items.id")+`,
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
		t.Fatalf("expected optional fields to remain NULL, got workspace=%v artifact=%v actor=%v visible_after=%v follow_up_at=%v source=%v source_ref=%v",
			workspaceID, artifactID, actorID, visibleAfter, followUpAt, source, sourceRef)
	}
}

func TestItemSchemaEnforcesForeignKeys(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.db.Exec(`INSERT INTO items (title, workspace_id) VALUES ('invalid', 999)`); err == nil {
		t.Fatal("expected foreign key violation for missing workspace")
	}
	if _, err := s.db.Exec(`INSERT INTO items (title, artifact_id) VALUES ('invalid', 999)`); err == nil {
		t.Fatal("expected foreign key violation for missing artifact")
	}
	if _, err := s.db.Exec(`INSERT INTO items (title, actor_id) VALUES ('invalid', 999)`); err == nil {
		t.Fatal("expected foreign key violation for missing actor")
	}
}

func TestDomainTypesExposeJSONTags(t *testing.T) {
	for _, tc := range []struct {
		name string
		typ  reflect.Type
	}{
		{name: "Workspace", typ: reflect.TypeOf(Workspace{})},
		{name: "Actor", typ: reflect.TypeOf(Actor{})},
		{name: "Artifact", typ: reflect.TypeOf(Artifact{})},
		{name: "ArtifactWorkspaceLink", typ: reflect.TypeOf(ArtifactWorkspaceLink{})},
		{name: "ItemArtifactLink", typ: reflect.TypeOf(ItemArtifactLink{})},
		{name: "ItemArtifact", typ: reflect.TypeOf(ItemArtifact{})},
		{name: "Item", typ: reflect.TypeOf(Item{})},
	} {
		for i := 0; i < tc.typ.NumField(); i++ {
			field := tc.typ.Field(i)
			if field.PkgPath != "" {
				continue
			}
			if tag := field.Tag.Get("json"); tag == "" || tag == "-" {
				t.Fatalf("%s.%s missing json tag", tc.name, field.Name)
			}
		}
	}
}

func TestDomainCRUDRoundTrip(t *testing.T) {
	s := newTestStore(t)

	workspaceAPath := filepath.Join(t.TempDir(), "workspace-a")
	workspaceBPath := filepath.Join(t.TempDir(), "workspace-b")

	workspaceA, err := s.CreateWorkspace("Workspace A", workspaceAPath)
	if err != nil {
		t.Fatalf("CreateWorkspace(workspace-a) error: %v", err)
	}
	workspaceB, err := s.CreateWorkspace(" Workspace B ", workspaceBPath)
	if err != nil {
		t.Fatalf("CreateWorkspace(workspace-b) error: %v", err)
	}
	if workspaceA.Sphere != SpherePrivate || workspaceB.Sphere != SpherePrivate {
		t.Fatalf("default workspace spheres = %q/%q, want private/private", workspaceA.Sphere, workspaceB.Sphere)
	}
	workWorkspace, err := s.CreateWorkspace("Workspace Work", filepath.Join(t.TempDir(), "workspace-work"), SphereWork)
	if err != nil {
		t.Fatalf("CreateWorkspace(workspace-work) error: %v", err)
	}
	if workWorkspace.Sphere != SphereWork {
		t.Fatalf("CreateWorkspace(workspace-work).Sphere = %q, want %q", workWorkspace.Sphere, SphereWork)
	}
	gotByPath, err := s.GetWorkspaceByPath(workspaceBPath)
	if err != nil {
		t.Fatalf("GetWorkspaceByPath() error: %v", err)
	}
	if gotByPath.ID != workspaceB.ID {
		t.Fatalf("GetWorkspaceByPath() ID = %d, want %d", gotByPath.ID, workspaceB.ID)
	}
	duplicate, err := s.CreateWorkspace("Duplicate", workspaceAPath)
	if err != nil {
		t.Fatalf("CreateWorkspace(duplicate) error: %v", err)
	}
	if duplicate.ID != workspaceA.ID {
		t.Fatalf("duplicate workspace id = %d, want %d", duplicate.ID, workspaceA.ID)
	}
	if err := s.SetActiveWorkspace(workspaceB.ID); err != nil {
		t.Fatalf("SetActiveWorkspace() error: %v", err)
	}
	workspaces, err := s.ListWorkspaces()
	if err != nil {
		t.Fatalf("ListWorkspaces() error: %v", err)
	}
	if len(workspaces) != 3 {
		t.Fatalf("ListWorkspaces() len = %d, want 3", len(workspaces))
	}
	if !workspaces[0].IsActive || workspaces[0].ID != workspaceB.ID {
		t.Fatalf("ListWorkspaces() active workspace mismatch: %+v", workspaces)
	}
	workspaceA, err = s.GetWorkspace(workspaceA.ID)
	if err != nil {
		t.Fatalf("GetWorkspace(workspace-a) error: %v", err)
	}
	if workspaceA.IsActive {
		t.Fatal("expected inactive workspace after SetActiveWorkspace")
	}
	workspaceA, err = s.UpdateWorkspaceName(workspaceA.ID, " Workspace Alpha ")
	if err != nil {
		t.Fatalf("UpdateWorkspaceName() error: %v", err)
	}
	if workspaceA.Name != "Workspace Alpha" {
		t.Fatalf("UpdateWorkspaceName().Name = %q, want %q", workspaceA.Name, "Workspace Alpha")
	}
	if _, err := s.UpdateWorkspaceName(999999, "Missing"); err == nil {
		t.Fatal("expected missing workspace rename error")
	}

	humanEmail := "alice@example.com"
	humanProvider := "manual"
	humanMetaJSON := `{"organization":"Acme"}`
	human, err := s.CreateActorWithOptions("Alice", ActorKindHuman, ActorOptions{
		Email:    &humanEmail,
		Provider: &humanProvider,
		MetaJSON: &humanMetaJSON,
	})
	if err != nil {
		t.Fatalf("CreateActorWithOptions(Alice) error: %v", err)
	}
	agent, err := s.CreateActor("Codex", ActorKindAgent)
	if err != nil {
		t.Fatalf("CreateActor(Codex) error: %v", err)
	}
	if _, err := s.CreateActor("Nobody", "robot"); err == nil {
		t.Fatal("expected invalid actor kind error")
	}
	actors, err := s.ListActors()
	if err != nil {
		t.Fatalf("ListActors() error: %v", err)
	}
	if len(actors) != 2 {
		t.Fatalf("ListActors() len = %d, want 2", len(actors))
	}
	if actors[0].Email == nil || *actors[0].Email != "alice@example.com" {
		t.Fatalf("ListActors()[0].Email = %v, want alice@example.com", actors[0].Email)
	}
	if actors[0].Provider == nil || *actors[0].Provider != "manual" {
		t.Fatalf("ListActors()[0].Provider = %v, want manual", actors[0].Provider)
	}
	if actors[0].MetaJSON == nil || *actors[0].MetaJSON != humanMetaJSON {
		t.Fatalf("ListActors()[0].MetaJSON = %v, want %q", actors[0].MetaJSON, humanMetaJSON)
	}
	if actors[0].Name != "Alice" || actors[1].Name != "Codex" {
		t.Fatalf("ListActors() names = %#v, want Alice/Codex", []string{actors[0].Name, actors[1].Name})
	}
	gotActor, err := s.GetActor(agent.ID)
	if err != nil {
		t.Fatalf("GetActor() error: %v", err)
	}
	if gotActor.Kind != ActorKindAgent {
		t.Fatalf("GetActor().Kind = %q, want %q", gotActor.Kind, ActorKindAgent)
	}
	contactMetaJSON := `{"organization":"Example Corp","phones":["+1 555 0100"]}`
	contact, err := s.UpsertActorContact("Alice Example", "alice@example.com", ExternalProviderGmail, "people/c123", &contactMetaJSON)
	if err != nil {
		t.Fatalf("UpsertActorContact(create) error: %v", err)
	}
	if contact.Email == nil || *contact.Email != "alice@example.com" {
		t.Fatalf("UpsertActorContact(create).Email = %v, want alice@example.com", contact.Email)
	}
	if contact.ID != human.ID {
		t.Fatalf("UpsertActorContact(create).ID = %d, want %d", contact.ID, human.ID)
	}
	if contact.Provider == nil || *contact.Provider != ExternalProviderGmail {
		t.Fatalf("UpsertActorContact(create).Provider = %v, want %q", contact.Provider, ExternalProviderGmail)
	}
	updatedContact, err := s.UpsertActorContact("Alice Updated", "Alice@Example.com", ExternalProviderExchange, "exchange-7", nil)
	if err != nil {
		t.Fatalf("UpsertActorContact(update) error: %v", err)
	}
	if updatedContact.ID != contact.ID {
		t.Fatalf("UpsertActorContact(update).ID = %d, want %d", updatedContact.ID, contact.ID)
	}
	if updatedContact.ProviderRef == nil || *updatedContact.ProviderRef != "exchange-7" {
		t.Fatalf("UpsertActorContact(update).ProviderRef = %v, want exchange-7", updatedContact.ProviderRef)
	}
	if _, err := s.GetActorByEmail("ALICE@example.com"); err != nil {
		t.Fatalf("GetActorByEmail() error: %v", err)
	}
	if _, err := s.GetActorByProviderRef(ExternalProviderExchange, "exchange-7"); err != nil {
		t.Fatalf("GetActorByProviderRef() error: %v", err)
	}

	refPath := filepath.Join(t.TempDir(), "artifact.md")
	refURL := "https://example.invalid/item/1"
	title := "Plan draft"
	metaJSON := `{"source":"unit"}`
	artifact, err := s.CreateArtifact(ArtifactKindMarkdown, &refPath, &refURL, &title, &metaJSON)
	if err != nil {
		t.Fatalf("CreateArtifact() error: %v", err)
	}
	gotArtifact, err := s.GetArtifact(artifact.ID)
	if err != nil {
		t.Fatalf("GetArtifact() error: %v", err)
	}
	if gotArtifact.Kind != ArtifactKindMarkdown || gotArtifact.Title == nil || *gotArtifact.Title != title {
		t.Fatalf("GetArtifact() = %+v", gotArtifact)
	}
	updatedTitle := "Plan draft v2"
	clearRefURL := ""
	updatedKind := ArtifactKindDocument
	if err := s.UpdateArtifact(artifact.ID, ArtifactUpdate{
		Kind:   &updatedKind,
		Title:  &updatedTitle,
		RefURL: &clearRefURL,
	}); err != nil {
		t.Fatalf("UpdateArtifact() error: %v", err)
	}
	gotArtifact, err = s.GetArtifact(artifact.ID)
	if err != nil {
		t.Fatalf("GetArtifact(updated) error: %v", err)
	}
	if gotArtifact.Kind != ArtifactKindDocument {
		t.Fatalf("GetArtifact(updated).Kind = %q, want %q", gotArtifact.Kind, ArtifactKindDocument)
	}
	if gotArtifact.RefURL != nil {
		t.Fatalf("GetArtifact(updated).RefURL = %v, want nil", *gotArtifact.RefURL)
	}
	if gotArtifact.Title == nil || *gotArtifact.Title != updatedTitle {
		t.Fatalf("GetArtifact(updated).Title = %v, want %q", gotArtifact.Title, updatedTitle)
	}
	artifacts, err := s.ListArtifactsByKind(ArtifactKindDocument)
	if err != nil {
		t.Fatalf("ListArtifactsByKind() error: %v", err)
	}
	if len(artifacts) != 1 || artifacts[0].ID != artifact.ID {
		t.Fatalf("ListArtifactsByKind() = %+v, want artifact %d", artifacts, artifact.ID)
	}

	source := "github"
	sourceRef := "issue-174"
	visibleAfter := "2026-03-09T10:00:00Z"
	followUpAt := "2026-03-10T11:00:00Z"

	inboxItem, err := s.CreateItem("Inbox item", ItemOptions{})
	if err != nil {
		t.Fatalf("CreateItem(inbox) error: %v", err)
	}
	artifactItem, err := s.CreateItem("Artifact item", ItemOptions{
		ArtifactID: &artifact.ID,
		Source:     &source,
		SourceRef:  &sourceRef,
	})
	if err != nil {
		t.Fatalf("CreateItem(artifact) error: %v", err)
	}
	workspaceItem, err := s.CreateItem("Workspace item", ItemOptions{
		WorkspaceID: &workWorkspace.ID,
	})
	if err != nil {
		t.Fatalf("CreateItem(workspace) error: %v", err)
	}
	if workspaceItem.Sphere != SphereWork {
		t.Fatalf("CreateItem(workspace).Sphere = %q, want %q", workspaceItem.Sphere, SphereWork)
	}
	assignedItem, err := s.CreateItem("Assigned item", ItemOptions{
		State:        ItemStateWaiting,
		WorkspaceID:  &workspaceB.ID,
		ArtifactID:   &artifact.ID,
		ActorID:      &human.ID,
		VisibleAfter: &visibleAfter,
		FollowUpAt:   &followUpAt,
	})
	if err != nil {
		t.Fatalf("CreateItem(assigned) error: %v", err)
	}
	if assignedItem.WorkspaceID == nil || *assignedItem.WorkspaceID != workspaceB.ID {
		t.Fatalf("CreateItem(assigned).WorkspaceID = %v, want %d", assignedItem.WorkspaceID, workspaceB.ID)
	}
	if assignedItem.ArtifactID == nil || *assignedItem.ArtifactID != artifact.ID {
		t.Fatalf("CreateItem(assigned).ArtifactID = %v, want %d", assignedItem.ArtifactID, artifact.ID)
	}
	if assignedItem.ActorID == nil || *assignedItem.ActorID != human.ID {
		t.Fatalf("CreateItem(assigned).ActorID = %v, want %d", assignedItem.ActorID, human.ID)
	}
	if assignedItem.Sphere != SpherePrivate {
		t.Fatalf("CreateItem(assigned).Sphere = %q, want %q", assignedItem.Sphere, SpherePrivate)
	}
	sourceCompleteRef := "issue-183"
	sourceItem, err := s.CreateItem("Source completion item", ItemOptions{
		State:     ItemStateWaiting,
		Source:    &source,
		SourceRef: &sourceCompleteRef,
	})
	if err != nil {
		t.Fatalf("CreateItem(source completion) error: %v", err)
	}

	if err := s.AssignItem(artifactItem.ID, agent.ID); err != nil {
		t.Fatalf("AssignItem() error: %v", err)
	}
	gotItem, err := s.GetItem(artifactItem.ID)
	if err != nil {
		t.Fatalf("GetItem(artifact assigned) error: %v", err)
	}
	if gotItem.ActorID == nil || *gotItem.ActorID != agent.ID {
		t.Fatalf("GetItem(artifact assigned).ActorID = %v, want %d", gotItem.ActorID, agent.ID)
	}
	if err := s.AssignItem(artifactItem.ID, 9999); err == nil {
		t.Fatal("expected assign to nonexistent actor error")
	}
	if err := s.AssignItem(artifactItem.ID, human.ID); err != nil {
		t.Fatalf("AssignItem(reassign) error: %v", err)
	}
	gotItem, err = s.GetItem(artifactItem.ID)
	if err != nil {
		t.Fatalf("GetItem(artifact reassigned) error: %v", err)
	}
	if gotItem.ActorID == nil || *gotItem.ActorID != human.ID {
		t.Fatalf("GetItem(artifact reassigned).ActorID = %v, want %d", gotItem.ActorID, human.ID)
	}
	if gotItem.State != ItemStateWaiting {
		t.Fatalf("GetItem(artifact reassigned).State = %q, want %q", gotItem.State, ItemStateWaiting)
	}
	if err := s.UnassignItem(artifactItem.ID); err != nil {
		t.Fatalf("UnassignItem() error: %v", err)
	}
	gotItem, err = s.GetItem(artifactItem.ID)
	if err != nil {
		t.Fatalf("GetItem(artifact unassigned) error: %v", err)
	}
	if gotItem.ActorID != nil {
		t.Fatalf("GetItem(artifact unassigned).ActorID = %v, want nil", gotItem.ActorID)
	}
	if gotItem.State != ItemStateInbox {
		t.Fatalf("GetItem(artifact unassigned).State = %q, want %q", gotItem.State, ItemStateInbox)
	}
	if err := s.UnassignItem(artifactItem.ID); err == nil {
		t.Fatal("expected unassign on unassigned item error")
	}
	if err := s.AssignItem(artifactItem.ID, agent.ID); err != nil {
		t.Fatalf("AssignItem(reassign to agent) error: %v", err)
	}
	if err := s.CompleteItemByActor(artifactItem.ID, human.ID); err == nil {
		t.Fatal("expected complete with wrong actor error")
	}
	if err := s.CompleteItemByActor(artifactItem.ID, agent.ID); err != nil {
		t.Fatalf("CompleteItemByActor() error: %v", err)
	}
	gotItem, err = s.GetItem(artifactItem.ID)
	if err != nil {
		t.Fatalf("GetItem(artifact completed) error: %v", err)
	}
	if gotItem.State != ItemStateDone {
		t.Fatalf("GetItem(artifact completed).State = %q, want %q", gotItem.State, ItemStateDone)
	}
	if err := s.CompleteItemByActor(artifactItem.ID, agent.ID); err == nil {
		t.Fatal("expected double complete error")
	}
	if err := s.AssignItem(artifactItem.ID, human.ID); err == nil {
		t.Fatal("expected assign on done item error")
	}
	if err := s.ReturnItemToInbox(assignedItem.ID); err != nil {
		t.Fatalf("ReturnItemToInbox() error: %v", err)
	}
	gotItem, err = s.GetItem(assignedItem.ID)
	if err != nil {
		t.Fatalf("GetItem(returned to inbox) error: %v", err)
	}
	if gotItem.State != ItemStateInbox {
		t.Fatalf("GetItem(returned to inbox).State = %q, want %q", gotItem.State, ItemStateInbox)
	}
	if gotItem.ActorID == nil || *gotItem.ActorID != human.ID {
		t.Fatalf("GetItem(returned to inbox).ActorID = %v, want %d", gotItem.ActorID, human.ID)
	}
	if err := s.ReturnItemToInbox(artifactItem.ID); err == nil {
		t.Fatal("expected return on done item error")
	}
	if err := s.CompleteItemBySource(source, sourceCompleteRef); err != nil {
		t.Fatalf("CompleteItemBySource() error: %v", err)
	}
	gotItem, err = s.GetItem(sourceItem.ID)
	if err != nil {
		t.Fatalf("GetItem(source completed) error: %v", err)
	}
	if gotItem.State != ItemStateDone {
		t.Fatalf("GetItem(source completed).State = %q, want %q", gotItem.State, ItemStateDone)
	}
	if err := s.CompleteItemBySource(source, sourceCompleteRef); err == nil {
		t.Fatal("expected double source complete error")
	}
	if err := s.CompleteItemBySource("github", "missing"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("CompleteItemBySource(missing) error = %v, want sql.ErrNoRows", err)
	}

	if err := s.UpdateItemTimes(inboxItem.ID, &visibleAfter, &followUpAt); err != nil {
		t.Fatalf("UpdateItemTimes() error: %v", err)
	}
	gotItem, err = s.GetItem(inboxItem.ID)
	if err != nil {
		t.Fatalf("GetItem(updated times) error: %v", err)
	}
	if gotItem.VisibleAfter == nil || *gotItem.VisibleAfter != visibleAfter {
		t.Fatalf("VisibleAfter = %v, want %q", gotItem.VisibleAfter, visibleAfter)
	}
	if gotItem.FollowUpAt == nil || *gotItem.FollowUpAt != followUpAt {
		t.Fatalf("FollowUpAt = %v, want %q", gotItem.FollowUpAt, followUpAt)
	}

	if err := s.UpdateItemState(inboxItem.ID, ItemStateWaiting); err != nil {
		t.Fatalf("UpdateItemState(waiting) error: %v", err)
	}
	if err := s.UpdateItemState(workspaceItem.ID, ItemStateSomeday); err != nil {
		t.Fatalf("UpdateItemState(someday) error: %v", err)
	}
	if err := s.UpdateItemState(workspaceItem.ID, ItemStateInbox); err != nil {
		t.Fatalf("UpdateItemState(inbox from someday) error: %v", err)
	}
	if err := s.UpdateItemState(inboxItem.ID, ItemStateDone); err != nil {
		t.Fatalf("UpdateItemState(done) error: %v", err)
	}
	if err := s.UpdateItemState(inboxItem.ID, ItemStateInbox); err != nil {
		t.Fatalf("UpdateItemState(inbox from done) error: %v", err)
	}
	if err := s.UpdateItemState(inboxItem.ID, "paused"); err == nil {
		t.Fatal("expected invalid item state error")
	}

	waitingItems, err := s.ListItemsByState(ItemStateWaiting)
	if err != nil {
		t.Fatalf("ListItemsByState(waiting) error: %v", err)
	}
	if len(waitingItems) != 0 {
		t.Fatalf("ListItemsByState(waiting) len = %d, want 0", len(waitingItems))
	}
	doneItems, err := s.ListItemsByState(ItemStateDone)
	if err != nil {
		t.Fatalf("ListItemsByState(done) error: %v", err)
	}
	if len(doneItems) != 2 {
		t.Fatalf("ListItemsByState(done) len = %d, want 2", len(doneItems))
	}
	doneIDs := map[int64]bool{}
	for _, item := range doneItems {
		doneIDs[item.ID] = true
	}
	for _, id := range []int64{artifactItem.ID, sourceItem.ID} {
		if !doneIDs[id] {
			t.Fatalf("ListItemsByState(done) missing item %d: %+v", id, doneItems)
		}
	}
	if _, err := s.ListItemsByState("paused"); err == nil {
		t.Fatal("expected invalid ListItemsByState error")
	}

	if err := s.DeleteWorkspace(workWorkspace.ID); err != nil {
		t.Fatalf("DeleteWorkspace() error: %v", err)
	}
	workspaceItem, err = s.GetItem(workspaceItem.ID)
	if err != nil {
		t.Fatalf("GetItem(workspace item after workspace delete) error: %v", err)
	}
	if workspaceItem.WorkspaceID != nil {
		t.Fatalf("workspace item WorkspaceID = %v, want nil", *workspaceItem.WorkspaceID)
	}
	if workspaceItem.Sphere != SphereWork {
		t.Fatalf("workspace item sphere after workspace delete = %q, want %q", workspaceItem.Sphere, SphereWork)
	}
	if err := s.DeleteArtifact(artifact.ID); err != nil {
		t.Fatalf("DeleteArtifact() error: %v", err)
	}
	artifactItem, err = s.GetItem(artifactItem.ID)
	if err != nil {
		t.Fatalf("GetItem(artifact item after artifact delete) error: %v", err)
	}
	if artifactItem.ArtifactID != nil {
		t.Fatalf("artifact item ArtifactID = %v, want nil", *artifactItem.ArtifactID)
	}
	if err := s.DeleteActor(agent.ID); err != nil {
		t.Fatalf("DeleteActor() error: %v", err)
	}
	artifactItem, err = s.GetItem(artifactItem.ID)
	if err != nil {
		t.Fatalf("GetItem(artifact item after actor delete) error: %v", err)
	}
	if artifactItem.ActorID != nil {
		t.Fatalf("artifact item ActorID = %v, want nil", *artifactItem.ActorID)
	}

	if err := s.DeleteItem(assignedItem.ID); err != nil {
		t.Fatalf("DeleteItem() error: %v", err)
	}
	if _, err := s.GetItem(assignedItem.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetItem(deleted) error = %v, want sql.ErrNoRows", err)
	}
}

func TestSphereInheritanceAndMutators(t *testing.T) {
	s := newTestStore(t)

	if got, err := s.ActiveSphere(); err != nil {
		t.Fatalf("ActiveSphere() error: %v", err)
	} else if got != SpherePrivate {
		t.Fatalf("default ActiveSphere() = %q, want %q", got, SpherePrivate)
	}
	if err := s.SetActiveSphere(SphereWork); err != nil {
		t.Fatalf("SetActiveSphere() error: %v", err)
	}
	workspace, err := s.CreateWorkspace("Work", filepath.Join(t.TempDir(), "work"), SphereWork)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	if _, err := s.CreateWorkspace("Bad", filepath.Join(t.TempDir(), "bad"), "office"); err == nil {
		t.Fatal("expected invalid workspace sphere error")
	}

	item, err := s.CreateItem("Capture", ItemOptions{})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}
	if item.Sphere != SphereWork {
		t.Fatalf("CreateItem().Sphere = %q, want %q", item.Sphere, SphereWork)
	}

	workspaceItem, err := s.CreateItem("Workspace item", ItemOptions{WorkspaceID: &workspace.ID})
	if err != nil {
		t.Fatalf("CreateItem(workspace) error: %v", err)
	}
	if workspaceItem.Sphere != SphereWork {
		t.Fatalf("CreateItem(workspace).Sphere = %q, want %q", workspaceItem.Sphere, SphereWork)
	}

	if err := s.SetItemSphere(item.ID, SpherePrivate); err != nil {
		t.Fatalf("SetItemSphere() error: %v", err)
	}
	if updated, err := s.GetItem(item.ID); err != nil {
		t.Fatalf("GetItem(updated) error: %v", err)
	} else if updated.Sphere != SpherePrivate {
		t.Fatalf("SetItemSphere().Sphere = %q, want %q", updated.Sphere, SpherePrivate)
	}
	if err := s.SetItemSphere(workspaceItem.ID, SpherePrivate); err == nil {
		t.Fatal("expected workspace-backed item sphere error")
	}

	if _, err := s.SetWorkspaceSphere(workspace.ID, SpherePrivate); err != nil {
		t.Fatalf("SetWorkspaceSphere() error: %v", err)
	}
	if refreshed, err := s.GetItem(workspaceItem.ID); err != nil {
		t.Fatalf("GetItem(workspaceItem) error: %v", err)
	} else if refreshed.Sphere != SpherePrivate {
		t.Fatalf("workspace-backed item sphere = %q, want %q", refreshed.Sphere, SpherePrivate)
	}

	if _, err := s.AddSphereAccount(SphereWork, ExternalProviderGmail, "Work Gmail", map[string]any{"username": "alice@example.com"}); err != nil {
		t.Fatalf("AddSphereAccount() error: %v", err)
	}
	accounts, err := s.ListSphereAccounts(SphereWork)
	if err != nil {
		t.Fatalf("ListSphereAccounts() error: %v", err)
	}
	if len(accounts) != 1 {
		t.Fatalf("ListSphereAccounts() len = %d, want 1", len(accounts))
	}
	if err := s.RemoveSphereAccount(accounts[0].ID); err != nil {
		t.Fatalf("RemoveSphereAccount() error: %v", err)
	}
}

func TestDomainConcurrentWorkspaceCreates(t *testing.T) {
	s := newTestStore(t)

	const count = 12
	baseDir := t.TempDir()
	errCh := make(chan error, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.CreateWorkspace(
				"Workspace",
				filepath.Join(baseDir, fmt.Sprintf("workspace-%02d", i)),
			)
			errCh <- err
		}()
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatalf("CreateWorkspace() concurrent error: %v", err)
		}
	}
	workspaces, err := s.ListWorkspaces()
	if err != nil {
		t.Fatalf("ListWorkspaces() error: %v", err)
	}
	if len(workspaces) != count {
		t.Fatalf("ListWorkspaces() len = %d, want %d", len(workspaces), count)
	}
}

func TestItemTriageOperations(t *testing.T) {
	s := newTestStore(t)

	actor, err := s.CreateActor("Codex", ActorKindAgent)
	if err != nil {
		t.Fatalf("CreateActor() error: %v", err)
	}

	laterItem, err := s.CreateItem("Later item", ItemOptions{})
	if err != nil {
		t.Fatalf("CreateItem(later) error: %v", err)
	}
	visibleAfter := "2026-03-10T09:00:00Z"
	if err := s.TriageItemLater(laterItem.ID, visibleAfter); err != nil {
		t.Fatalf("TriageItemLater() error: %v", err)
	}
	gotLater, err := s.GetItem(laterItem.ID)
	if err != nil {
		t.Fatalf("GetItem(later) error: %v", err)
	}
	if gotLater.State != ItemStateWaiting {
		t.Fatalf("later state = %q, want %q", gotLater.State, ItemStateWaiting)
	}
	if gotLater.VisibleAfter == nil || *gotLater.VisibleAfter != visibleAfter {
		t.Fatalf("later visible_after = %v, want %q", gotLater.VisibleAfter, visibleAfter)
	}

	delegateItem, err := s.CreateItem("Delegate item", ItemOptions{
		VisibleAfter: &visibleAfter,
	})
	if err != nil {
		t.Fatalf("CreateItem(delegate) error: %v", err)
	}
	if err := s.TriageItemDelegate(delegateItem.ID, actor.ID); err != nil {
		t.Fatalf("TriageItemDelegate() error: %v", err)
	}
	gotDelegate, err := s.GetItem(delegateItem.ID)
	if err != nil {
		t.Fatalf("GetItem(delegate) error: %v", err)
	}
	if gotDelegate.State != ItemStateWaiting {
		t.Fatalf("delegate state = %q, want %q", gotDelegate.State, ItemStateWaiting)
	}
	if gotDelegate.ActorID == nil || *gotDelegate.ActorID != actor.ID {
		t.Fatalf("delegate actor = %v, want %d", gotDelegate.ActorID, actor.ID)
	}
	if gotDelegate.VisibleAfter != nil {
		t.Fatalf("delegate visible_after = %v, want nil", gotDelegate.VisibleAfter)
	}

	somedayItem, err := s.CreateItem("Someday item", ItemOptions{
		ActorID:      &actor.ID,
		VisibleAfter: &visibleAfter,
		FollowUpAt:   &visibleAfter,
	})
	if err != nil {
		t.Fatalf("CreateItem(someday) error: %v", err)
	}
	if err := s.TriageItemSomeday(somedayItem.ID); err != nil {
		t.Fatalf("TriageItemSomeday() error: %v", err)
	}
	gotSomeday, err := s.GetItem(somedayItem.ID)
	if err != nil {
		t.Fatalf("GetItem(someday) error: %v", err)
	}
	if gotSomeday.State != ItemStateSomeday {
		t.Fatalf("someday state = %q, want %q", gotSomeday.State, ItemStateSomeday)
	}
	if gotSomeday.ActorID == nil || *gotSomeday.ActorID != actor.ID {
		t.Fatalf("someday actor = %v, want %d", gotSomeday.ActorID, actor.ID)
	}
	if gotSomeday.VisibleAfter != nil || gotSomeday.FollowUpAt != nil {
		t.Fatalf("someday timestamps = visible_after:%v follow_up_at:%v, want nil", gotSomeday.VisibleAfter, gotSomeday.FollowUpAt)
	}

	doneItem, err := s.CreateItem("Done item", ItemOptions{})
	if err != nil {
		t.Fatalf("CreateItem(done) error: %v", err)
	}
	if err := s.TriageItemDone(doneItem.ID); err != nil {
		t.Fatalf("TriageItemDone() error: %v", err)
	}
	gotDone, err := s.GetItem(doneItem.ID)
	if err != nil {
		t.Fatalf("GetItem(done) error: %v", err)
	}
	if gotDone.State != ItemStateDone {
		t.Fatalf("done state = %q, want %q", gotDone.State, ItemStateDone)
	}

	deleteItem, err := s.CreateItem("Delete me", ItemOptions{})
	if err != nil {
		t.Fatalf("CreateItem(delete) error: %v", err)
	}
	if err := s.TriageItemDelete(deleteItem.ID); err != nil {
		t.Fatalf("TriageItemDelete() error: %v", err)
	}
	if _, err := s.GetItem(deleteItem.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetItem(deleted) error = %v, want sql.ErrNoRows", err)
	}

	if err := s.TriageItemLater(laterItem.ID, "tomorrow morning"); err == nil {
		t.Fatal("expected invalid visible_after error")
	}
	if err := s.TriageItemDelegate(999999, actor.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("TriageItemDelegate(missing item) error = %v, want sql.ErrNoRows", err)
	}
	if err := s.TriageItemDelegate(laterItem.ID, 999999); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("TriageItemDelegate(missing actor) error = %v, want sql.ErrNoRows", err)
	}
	if err := s.TriageItemSomeday(doneItem.ID); err == nil {
		t.Fatal("expected done item triage rejection")
	}
}

func TestUpdateItemStateInboxClearsDeferredTimes(t *testing.T) {
	s := newTestStore(t)

	visibleAfter := "2026-03-10T09:00:00Z"
	followUpAt := "2026-03-10T10:00:00Z"
	item, err := s.CreateItem("Deferred item", ItemOptions{
		State:        ItemStateWaiting,
		VisibleAfter: &visibleAfter,
		FollowUpAt:   &followUpAt,
	})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}

	if err := s.UpdateItemState(item.ID, ItemStateInbox); err != nil {
		t.Fatalf("UpdateItemState(inbox) error: %v", err)
	}

	got, err := s.GetItem(item.ID)
	if err != nil {
		t.Fatalf("GetItem() error: %v", err)
	}
	if got.State != ItemStateInbox {
		t.Fatalf("state = %q, want %q", got.State, ItemStateInbox)
	}
	if got.VisibleAfter != nil || got.FollowUpAt != nil {
		t.Fatalf("timestamps after reopen = visible_after:%v follow_up_at:%v, want nil", got.VisibleAfter, got.FollowUpAt)
	}
}

func TestUpdateItemInboxClearsDeferredTimesByDefault(t *testing.T) {
	s := newTestStore(t)

	visibleAfter := "2026-03-10T09:00:00Z"
	followUpAt := "2026-03-10T10:00:00Z"
	item, err := s.CreateItem("Deferred item", ItemOptions{
		State:        ItemStateWaiting,
		VisibleAfter: &visibleAfter,
		FollowUpAt:   &followUpAt,
	})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}

	state := ItemStateInbox
	if err := s.UpdateItem(item.ID, ItemUpdate{State: &state}); err != nil {
		t.Fatalf("UpdateItem(inbox) error: %v", err)
	}

	got, err := s.GetItem(item.ID)
	if err != nil {
		t.Fatalf("GetItem() error: %v", err)
	}
	if got.State != ItemStateInbox {
		t.Fatalf("state = %q, want %q", got.State, ItemStateInbox)
	}
	if got.VisibleAfter != nil || got.FollowUpAt != nil {
		t.Fatalf("timestamps after update = visible_after:%v follow_up_at:%v, want nil", got.VisibleAfter, got.FollowUpAt)
	}
}

func TestResurfaceDueItems(t *testing.T) {
	s := newTestStore(t)

	now := time.Date(2026, time.March, 8, 10, 0, 0, 0, time.UTC)
	past := now.Add(-30 * time.Minute).Format(time.RFC3339)
	future := now.Add(30 * time.Minute).Format(time.RFC3339)

	pastVisible, err := s.CreateItem("past visible_after", ItemOptions{
		State:        ItemStateWaiting,
		VisibleAfter: &past,
	})
	if err != nil {
		t.Fatalf("CreateItem(past visible_after) error: %v", err)
	}
	pastFollowUp, err := s.CreateItem("past follow_up_at", ItemOptions{
		State:      ItemStateWaiting,
		FollowUpAt: &past,
	})
	if err != nil {
		t.Fatalf("CreateItem(past follow_up_at) error: %v", err)
	}
	futureWaiting, err := s.CreateItem("future waiting", ItemOptions{
		State:        ItemStateWaiting,
		VisibleAfter: &future,
		FollowUpAt:   &future,
	})
	if err != nil {
		t.Fatalf("CreateItem(future waiting) error: %v", err)
	}
	bothTimes, err := s.CreateItem("both timestamps", ItemOptions{
		State:        ItemStateWaiting,
		VisibleAfter: &future,
		FollowUpAt:   &past,
	})
	if err != nil {
		t.Fatalf("CreateItem(both timestamps) error: %v", err)
	}
	inboxItem, err := s.CreateItem("already inbox", ItemOptions{
		State:        ItemStateInbox,
		VisibleAfter: &past,
	})
	if err != nil {
		t.Fatalf("CreateItem(already inbox) error: %v", err)
	}
	doneItem, err := s.CreateItem("already done", ItemOptions{
		State:        ItemStateDone,
		VisibleAfter: &past,
	})
	if err != nil {
		t.Fatalf("CreateItem(already done) error: %v", err)
	}

	count, err := s.ResurfaceDueItems(now)
	if err != nil {
		t.Fatalf("ResurfaceDueItems() error: %v", err)
	}
	if count != 3 {
		t.Fatalf("ResurfaceDueItems() count = %d, want 3", count)
	}

	for _, tc := range []struct {
		name string
		id   int64
		want string
	}{
		{name: "past visible_after", id: pastVisible.ID, want: ItemStateInbox},
		{name: "past follow_up_at", id: pastFollowUp.ID, want: ItemStateInbox},
		{name: "both timestamps", id: bothTimes.ID, want: ItemStateInbox},
		{name: "future waiting", id: futureWaiting.ID, want: ItemStateWaiting},
		{name: "already inbox", id: inboxItem.ID, want: ItemStateInbox},
		{name: "already done", id: doneItem.ID, want: ItemStateDone},
	} {
		item, err := s.GetItem(tc.id)
		if err != nil {
			t.Fatalf("GetItem(%s) error: %v", tc.name, err)
		}
		if item.State != tc.want {
			t.Fatalf("%s state = %q, want %q", tc.name, item.State, tc.want)
		}
	}
}

func TestItemStateSummariesAndCounts(t *testing.T) {
	s := newTestStore(t)

	now := time.Date(2026, time.March, 8, 10, 0, 0, 0, time.UTC)
	past := now.Add(-1 * time.Hour).Format(time.RFC3339)
	future := now.Add(2 * time.Hour).Format(time.RFC3339)
	workspace, err := s.CreateWorkspace("Alpha", filepath.Join(t.TempDir(), "alpha"))
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	actor, err := s.CreateActor("Alice", ActorKindHuman)
	if err != nil {
		t.Fatalf("CreateActor() error: %v", err)
	}
	artifactTitle := "Inbox plan"
	artifact, err := s.CreateArtifact(ArtifactKindIdeaNote, nil, nil, &artifactTitle, nil)
	if err != nil {
		t.Fatalf("CreateArtifact() error: %v", err)
	}
	visibleInbox, err := s.CreateItem("Visible inbox", ItemOptions{
		State:        ItemStateInbox,
		WorkspaceID:  &workspace.ID,
		ArtifactID:   &artifact.ID,
		ActorID:      &actor.ID,
		VisibleAfter: &past,
	})
	if err != nil {
		t.Fatalf("CreateItem(visible inbox) error: %v", err)
	}
	if _, err := s.CreateItem("Hidden inbox", ItemOptions{
		State:        ItemStateInbox,
		VisibleAfter: &future,
	}); err != nil {
		t.Fatalf("CreateItem(hidden inbox) error: %v", err)
	}
	waitingItem, err := s.CreateItem("Waiting item", ItemOptions{State: ItemStateWaiting})
	if err != nil {
		t.Fatalf("CreateItem(waiting) error: %v", err)
	}
	somedayItem, err := s.CreateItem("Someday item", ItemOptions{State: ItemStateSomeday})
	if err != nil {
		t.Fatalf("CreateItem(someday) error: %v", err)
	}
	doneItem, err := s.CreateItem("Done item", ItemOptions{State: ItemStateDone})
	if err != nil {
		t.Fatalf("CreateItem(done) error: %v", err)
	}

	inboxItems, err := s.ListInboxItems(now)
	if err != nil {
		t.Fatalf("ListInboxItems() error: %v", err)
	}
	if len(inboxItems) != 1 {
		t.Fatalf("ListInboxItems() len = %d, want 1", len(inboxItems))
	}
	if inboxItems[0].ID != visibleInbox.ID {
		t.Fatalf("ListInboxItems() ID = %d, want %d", inboxItems[0].ID, visibleInbox.ID)
	}
	if inboxItems[0].ArtifactTitle == nil || *inboxItems[0].ArtifactTitle != artifactTitle {
		t.Fatalf("ListInboxItems() ArtifactTitle = %v, want %q", inboxItems[0].ArtifactTitle, artifactTitle)
	}
	if inboxItems[0].ArtifactKind == nil || *inboxItems[0].ArtifactKind != ArtifactKindIdeaNote {
		t.Fatalf("ListInboxItems() ArtifactKind = %v, want %q", inboxItems[0].ArtifactKind, ArtifactKindIdeaNote)
	}
	if inboxItems[0].ActorName == nil || *inboxItems[0].ActorName != "Alice" {
		t.Fatalf("ListInboxItems() ActorName = %v, want Alice", inboxItems[0].ActorName)
	}

	waitingItems, err := s.ListWaitingItems()
	if err != nil {
		t.Fatalf("ListWaitingItems() error: %v", err)
	}
	if len(waitingItems) != 1 || waitingItems[0].ID != waitingItem.ID {
		t.Fatalf("ListWaitingItems() = %+v, want waiting item %d", waitingItems, waitingItem.ID)
	}

	somedayItems, err := s.ListSomedayItems()
	if err != nil {
		t.Fatalf("ListSomedayItems() error: %v", err)
	}
	if len(somedayItems) != 1 || somedayItems[0].ID != somedayItem.ID {
		t.Fatalf("ListSomedayItems() = %+v, want someday item %d", somedayItems, somedayItem.ID)
	}

	doneItems, err := s.ListDoneItems(1)
	if err != nil {
		t.Fatalf("ListDoneItems() error: %v", err)
	}
	if len(doneItems) != 1 || doneItems[0].ID != doneItem.ID {
		t.Fatalf("ListDoneItems() = %+v, want done item %d", doneItems, doneItem.ID)
	}

	counts, err := s.CountItemsByState(now)
	if err != nil {
		t.Fatalf("CountItemsByState() error: %v", err)
	}
	if got := counts[ItemStateInbox]; got != 1 {
		t.Fatalf("CountItemsByState()[inbox] = %d, want 1", got)
	}
	if got := counts[ItemStateWaiting]; got != 1 {
		t.Fatalf("CountItemsByState()[waiting] = %d, want 1", got)
	}
	if got := counts[ItemStateSomeday]; got != 1 {
		t.Fatalf("CountItemsByState()[someday] = %d, want 1", got)
	}
	if got := counts[ItemStateDone]; got != 1 {
		t.Fatalf("CountItemsByState()[done] = %d, want 1", got)
	}
}

func TestItemSummaryFilters(t *testing.T) {
	s := newTestStore(t)

	now := time.Date(2026, time.March, 8, 10, 0, 0, 0, time.UTC)
	past := now.Add(-1 * time.Hour).Format(time.RFC3339)
	workspace, err := s.CreateWorkspace("Alpha", filepath.Join(t.TempDir(), "alpha"))
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	sourceTodoist := ExternalProviderTodoist
	sourceExchange := ExternalProviderExchange

	unassignedItem, err := s.CreateItem("Unassigned todoist item", ItemOptions{
		State:        ItemStateInbox,
		VisibleAfter: &past,
		Source:       &sourceTodoist,
	})
	if err != nil {
		t.Fatalf("CreateItem(unassigned) error: %v", err)
	}
	if _, err := s.CreateItem("Workspace todoist item", ItemOptions{
		State:        ItemStateInbox,
		WorkspaceID:  &workspace.ID,
		VisibleAfter: &past,
		Source:       &sourceTodoist,
	}); err != nil {
		t.Fatalf("CreateItem(workspace todoist) error: %v", err)
	}
	if _, err := s.CreateItem("Workspace exchange item", ItemOptions{
		State:        ItemStateInbox,
		WorkspaceID:  &workspace.ID,
		VisibleAfter: &past,
		Source:       &sourceExchange,
	}); err != nil {
		t.Fatalf("CreateItem(workspace exchange) error: %v", err)
	}

	todoistItems, err := s.ListInboxItemsFiltered(now, ItemListFilter{Source: ExternalProviderTodoist})
	if err != nil {
		t.Fatalf("ListInboxItemsFiltered(todoist) error: %v", err)
	}
	if len(todoistItems) != 2 {
		t.Fatalf("ListInboxItemsFiltered(todoist) len = %d, want 2", len(todoistItems))
	}

	unassignedItems, err := s.ListInboxItemsFiltered(now, ItemListFilter{WorkspaceUnassigned: true})
	if err != nil {
		t.Fatalf("ListInboxItemsFiltered(unassigned) error: %v", err)
	}
	if len(unassignedItems) != 1 || unassignedItems[0].ID != unassignedItem.ID {
		t.Fatalf("ListInboxItemsFiltered(unassigned) = %+v, want only item %d", unassignedItems, unassignedItem.ID)
	}

	workspaceItems, err := s.ListInboxItemsFiltered(now, ItemListFilter{WorkspaceID: &workspace.ID})
	if err != nil {
		t.Fatalf("ListInboxItemsFiltered(workspace) error: %v", err)
	}
	if len(workspaceItems) != 2 {
		t.Fatalf("ListInboxItemsFiltered(workspace) len = %d, want 2", len(workspaceItems))
	}

	counts, err := s.CountItemsByStateFiltered(now, ItemListFilter{Source: ExternalProviderTodoist})
	if err != nil {
		t.Fatalf("CountItemsByStateFiltered(todoist) error: %v", err)
	}
	if got := counts[ItemStateInbox]; got != 2 {
		t.Fatalf("CountItemsByStateFiltered(todoist)[inbox] = %d, want 2", got)
	}
}

func TestItemSummaryFiltersByContextIncludingDescendants(t *testing.T) {
	s := newTestStore(t)

	parent, err := s.CreateLabel("Work", nil)
	if err != nil {
		t.Fatalf("CreateLabel(parent) error: %v", err)
	}
	child, err := s.CreateLabel("W7x", &parent.ID)
	if err != nil {
		t.Fatalf("CreateLabel(child) error: %v", err)
	}
	privateCtx, err := s.CreateLabel("Private", nil)
	if err != nil {
		t.Fatalf("CreateLabel(private) error: %v", err)
	}

	workspace, err := s.CreateWorkspace("Alpha", filepath.Join(t.TempDir(), "alpha"))
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	if err := s.LinkLabelToWorkspace(child.ID, workspace.ID); err != nil {
		t.Fatalf("LinkLabelToWorkspace() error: %v", err)
	}

	now := time.Date(2026, time.March, 8, 10, 0, 0, 0, time.UTC)
	past := now.Add(-1 * time.Hour).Format(time.RFC3339)
	workspaceItem, err := s.CreateItem("Workspace child context item", ItemOptions{
		State:        ItemStateInbox,
		WorkspaceID:  &workspace.ID,
		VisibleAfter: &past,
	})
	if err != nil {
		t.Fatalf("CreateItem(workspace) error: %v", err)
	}
	privateItem, err := s.CreateItem("Private context item", ItemOptions{
		State:        ItemStateInbox,
		VisibleAfter: &past,
	})
	if err != nil {
		t.Fatalf("CreateItem(private) error: %v", err)
	}
	if err := s.LinkLabelToItem(privateCtx.ID, privateItem.ID); err != nil {
		t.Fatalf("LinkLabelToItem() error: %v", err)
	}
	directChildItem, err := s.CreateItem("Direct child context item", ItemOptions{
		State:        ItemStateInbox,
		VisibleAfter: &past,
	})
	if err != nil {
		t.Fatalf("CreateItem(direct child) error: %v", err)
	}
	if err := s.LinkLabelToItem(child.ID, directChildItem.ID); err != nil {
		t.Fatalf("LinkLabelToItem(child) error: %v", err)
	}

	parentFilter := ItemListFilter{LabelID: &parent.ID}
	parentItems, err := s.ListInboxItemsFiltered(now, parentFilter)
	if err != nil {
		t.Fatalf("ListInboxItemsFiltered(parent) error: %v", err)
	}
	if len(parentItems) != 2 {
		t.Fatalf("ListInboxItemsFiltered(parent) len = %d, want 2", len(parentItems))
	}
	gotIDs := map[int64]bool{}
	for _, item := range parentItems {
		gotIDs[item.ID] = true
	}
	if !gotIDs[workspaceItem.ID] || !gotIDs[directChildItem.ID] {
		t.Fatalf("ListInboxItemsFiltered(parent) = %+v, want items %d and %d", parentItems, workspaceItem.ID, directChildItem.ID)
	}

	counts, err := s.CountItemsByStateFiltered(now, parentFilter)
	if err != nil {
		t.Fatalf("CountItemsByStateFiltered(parent) error: %v", err)
	}
	if got := counts[ItemStateInbox]; got != 2 {
		t.Fatalf("CountItemsByStateFiltered(parent)[inbox] = %d, want 2", got)
	}
}

func TestFindWorkspaceContainingPathPrefersDeepestMatch(t *testing.T) {
	s := newTestStore(t)

	rootDir := filepath.Join(t.TempDir(), "workspace-root")
	nestedDir := filepath.Join(rootDir, "nested")
	rootWorkspace, err := s.CreateWorkspace("Root", rootDir)
	if err != nil {
		t.Fatalf("CreateWorkspace(root) error: %v", err)
	}
	nestedWorkspace, err := s.CreateWorkspace("Nested", nestedDir)
	if err != nil {
		t.Fatalf("CreateWorkspace(nested) error: %v", err)
	}

	insideNested := filepath.Join(nestedDir, "docs", "note.md")
	gotID, err := s.FindWorkspaceContainingPath(insideNested)
	if err != nil {
		t.Fatalf("FindWorkspaceContainingPath(inside nested) error: %v", err)
	}
	if gotID == nil || *gotID != nestedWorkspace.ID {
		t.Fatalf("FindWorkspaceContainingPath(inside nested) = %v, want %d", gotID, nestedWorkspace.ID)
	}

	insideRootOnly := filepath.Join(rootDir, "readme.md")
	gotID, err = s.FindWorkspaceContainingPath(insideRootOnly)
	if err != nil {
		t.Fatalf("FindWorkspaceContainingPath(inside root) error: %v", err)
	}
	if gotID == nil || *gotID != rootWorkspace.ID {
		t.Fatalf("FindWorkspaceContainingPath(inside root) = %v, want %d", gotID, rootWorkspace.ID)
	}

	gotID, err = s.FindWorkspaceContainingPath(filepath.Join(t.TempDir(), "outside.md"))
	if err != nil {
		t.Fatalf("FindWorkspaceContainingPath(outside) error: %v", err)
	}
	if gotID != nil {
		t.Fatalf("FindWorkspaceContainingPath(outside) = %v, want nil", *gotID)
	}
}

func TestFindWorkspaceByGitRemoteMatchesUniqueWorkspace(t *testing.T) {
	s := newTestStore(t)

	repoA := filepath.Join(t.TempDir(), "workspace-a")
	repoB := filepath.Join(t.TempDir(), "workspace-b")
	repoC := filepath.Join(t.TempDir(), "workspace-c")
	initGitRepoWithRemote(t, repoA, "git@github.com:owner/alpha.git")
	initGitRepoWithRemote(t, repoB, "https://github.com/owner/beta.git")
	initGitRepoWithRemote(t, repoC, "ssh://git@github.com/owner/alpha.git")

	workspaceA, err := s.CreateWorkspace("Alpha A", repoA)
	if err != nil {
		t.Fatalf("CreateWorkspace(alpha a) error: %v", err)
	}
	if _, err := s.CreateWorkspace("Beta", repoB); err != nil {
		t.Fatalf("CreateWorkspace(beta) error: %v", err)
	}
	if _, err := s.CreateWorkspace("Alpha C", repoC); err != nil {
		t.Fatalf("CreateWorkspace(alpha c) error: %v", err)
	}

	gotID, err := s.FindWorkspaceByGitRemote("owner/beta")
	if err != nil {
		t.Fatalf("FindWorkspaceByGitRemote(beta) error: %v", err)
	}
	if gotID == nil {
		t.Fatal("FindWorkspaceByGitRemote(beta) = nil, want workspace ID")
	}
	gotWorkspace, err := s.GetWorkspace(*gotID)
	if err != nil {
		t.Fatalf("GetWorkspace(beta) error: %v", err)
	}
	if gotWorkspace.Name != "Beta" {
		t.Fatalf("FindWorkspaceByGitRemote(beta) picked %q, want Beta", gotWorkspace.Name)
	}

	gotID, err = s.FindWorkspaceByGitRemote("owner/alpha")
	if err != nil {
		t.Fatalf("FindWorkspaceByGitRemote(alpha) error: %v", err)
	}
	if gotID != nil {
		t.Fatalf("FindWorkspaceByGitRemote(alpha) = %v, want nil for ambiguous match", *gotID)
	}

	gotID, err = s.FindWorkspaceByGitRemote("owner/missing")
	if err != nil {
		t.Fatalf("FindWorkspaceByGitRemote(missing) error: %v", err)
	}
	if gotID != nil {
		t.Fatalf("FindWorkspaceByGitRemote(missing) = %v, want nil", *gotID)
	}

	if workspaceA.ID == 0 {
		t.Fatal("expected created workspace ID")
	}
}

func TestGitHubRepoForWorkspace(t *testing.T) {
	s := newTestStore(t)

	repoDir := filepath.Join(t.TempDir(), "repo")
	initGitRepoWithRemote(t, repoDir, "https://github.com/owner/tabula.git")
	workspace, err := s.CreateWorkspace("Repo", repoDir)
	if err != nil {
		t.Fatalf("CreateWorkspace(repo) error: %v", err)
	}

	repo, err := s.GitHubRepoForWorkspace(workspace.ID)
	if err != nil {
		t.Fatalf("GitHubRepoForWorkspace() error: %v", err)
	}
	if repo != "owner/tabula" {
		t.Fatalf("GitHubRepoForWorkspace() = %q, want %q", repo, "owner/tabula")
	}

	missingRemoteDir := filepath.Join(t.TempDir(), "no-remote")
	if err := exec.Command("git", "init", missingRemoteDir).Run(); err != nil {
		t.Fatalf("git init %s: %v", missingRemoteDir, err)
	}
	noRemoteWorkspace, err := s.CreateWorkspace("No Remote", missingRemoteDir)
	if err != nil {
		t.Fatalf("CreateWorkspace(no remote) error: %v", err)
	}
	repo, err = s.GitHubRepoForWorkspace(noRemoteWorkspace.ID)
	if err != nil {
		t.Fatalf("GitHubRepoForWorkspace(no remote) error: %v", err)
	}
	if repo != "" {
		t.Fatalf("GitHubRepoForWorkspace(no remote) = %q, want empty", repo)
	}
}

func TestSourceItemUpsertAndSyncState(t *testing.T) {
	s := newTestStore(t)

	workspaceDir := filepath.Join(t.TempDir(), "workspace")
	workspace, err := s.CreateWorkspace("Workspace", workspaceDir)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	artifactTitle := "Issue #12"
	artifactURL := "https://github.com/owner/tabula/issues/12"
	artifact, err := s.CreateArtifact(ArtifactKindGitHubIssue, nil, &artifactURL, &artifactTitle, nil)
	if err != nil {
		t.Fatalf("CreateArtifact() error: %v", err)
	}

	item, err := s.UpsertItemFromSource("github", "owner/tabula#12", "Initial issue title", &workspace.ID)
	if err != nil {
		t.Fatalf("UpsertItemFromSource(create) error: %v", err)
	}
	if item.State != ItemStateInbox {
		t.Fatalf("created item state = %q, want %q", item.State, ItemStateInbox)
	}
	if err := s.UpdateItemArtifact(item.ID, &artifact.ID); err != nil {
		t.Fatalf("UpdateItemArtifact() error: %v", err)
	}

	gotBySource, err := s.GetItemBySource("github", "owner/tabula#12")
	if err != nil {
		t.Fatalf("GetItemBySource() error: %v", err)
	}
	if gotBySource.ID != item.ID {
		t.Fatalf("GetItemBySource() ID = %d, want %d", gotBySource.ID, item.ID)
	}
	if gotBySource.ArtifactID == nil || *gotBySource.ArtifactID != artifact.ID {
		t.Fatalf("GetItemBySource().ArtifactID = %v, want %d", gotBySource.ArtifactID, artifact.ID)
	}

	updatedItem, err := s.UpsertItemFromSource("github", "owner/tabula#12", "Renamed issue title", nil)
	if err != nil {
		t.Fatalf("UpsertItemFromSource(update) error: %v", err)
	}
	if updatedItem.ID != item.ID {
		t.Fatalf("updated item ID = %d, want %d", updatedItem.ID, item.ID)
	}
	if updatedItem.Title != "Renamed issue title" {
		t.Fatalf("updated title = %q, want %q", updatedItem.Title, "Renamed issue title")
	}
	if updatedItem.WorkspaceID != nil {
		t.Fatalf("updated WorkspaceID = %v, want nil", updatedItem.WorkspaceID)
	}
	items, err := s.ListItemsByState(ItemStateInbox)
	if err != nil {
		t.Fatalf("ListItemsByState(inbox) error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("ListItemsByState(inbox) len = %d, want 1", len(items))
	}
	visibleAfter := "2026-03-10T09:00:00Z"
	followUpAt := "2026-03-10T10:00:00Z"
	if err := s.UpdateItemTimes(item.ID, &visibleAfter, &followUpAt); err != nil {
		t.Fatalf("UpdateItemTimes() error: %v", err)
	}

	if err := s.SyncItemStateBySource("github", "owner/tabula#12", ItemStateDone); err != nil {
		t.Fatalf("SyncItemStateBySource(done) error: %v", err)
	}
	doneItem, err := s.GetItem(item.ID)
	if err != nil {
		t.Fatalf("GetItem(done) error: %v", err)
	}
	if doneItem.State != ItemStateDone {
		t.Fatalf("done item state = %q, want %q", doneItem.State, ItemStateDone)
	}

	if err := s.SyncItemStateBySource("github", "owner/tabula#12", ItemStateInbox); err != nil {
		t.Fatalf("SyncItemStateBySource(reopen) error: %v", err)
	}
	reopenedItem, err := s.GetItem(item.ID)
	if err != nil {
		t.Fatalf("GetItem(reopened) error: %v", err)
	}
	if reopenedItem.State != ItemStateInbox {
		t.Fatalf("reopened item state = %q, want %q", reopenedItem.State, ItemStateInbox)
	}
	if reopenedItem.VisibleAfter != nil || reopenedItem.FollowUpAt != nil {
		t.Fatalf("reopened item timestamps = visible_after:%v follow_up_at:%v, want nil", reopenedItem.VisibleAfter, reopenedItem.FollowUpAt)
	}

	if _, err := s.GetItemBySource("github", "owner/tabula#404"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetItemBySource(missing) error = %v, want sql.ErrNoRows", err)
	}
	if err := s.SyncItemStateBySource("github", "owner/tabula#404", ItemStateDone); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("SyncItemStateBySource(missing) error = %v, want sql.ErrNoRows", err)
	}
}

func TestUpdateItemSource(t *testing.T) {
	s := newTestStore(t)

	item, err := s.CreateItem("Promote me", ItemOptions{})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}
	if err := s.UpdateItemSource(item.ID, "github", "owner/tabula#77"); err != nil {
		t.Fatalf("UpdateItemSource() error: %v", err)
	}

	updated, err := s.GetItem(item.ID)
	if err != nil {
		t.Fatalf("GetItem() error: %v", err)
	}
	if updated.Source == nil || *updated.Source != "github" {
		t.Fatalf("updated.Source = %v, want github", updated.Source)
	}
	if updated.SourceRef == nil || *updated.SourceRef != "owner/tabula#77" {
		t.Fatalf("updated.SourceRef = %v, want owner/tabula#77", updated.SourceRef)
	}

	other, err := s.CreateItem("Other item", ItemOptions{})
	if err != nil {
		t.Fatalf("CreateItem(other) error: %v", err)
	}
	if err := s.UpdateItemSource(other.ID, "github", "owner/tabula#77"); err == nil {
		t.Fatal("expected duplicate source/source_ref error")
	}
	if err := s.UpdateItemSource(9999, "github", "owner/tabula#88"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("UpdateItemSource(missing) error = %v, want sql.ErrNoRows", err)
	}
}

func TestInferWorkspaceForArtifact(t *testing.T) {
	s := newTestStore(t)

	docWorkspaceDir := filepath.Join(t.TempDir(), "docs")
	docWorkspace, err := s.CreateWorkspace("Docs", docWorkspaceDir)
	if err != nil {
		t.Fatalf("CreateWorkspace(docs) error: %v", err)
	}
	repoDir := filepath.Join(t.TempDir(), "repo")
	initGitRepoWithRemote(t, repoDir, "https://github.com/owner/tabula.git")
	repoWorkspace, err := s.CreateWorkspace("Repo", repoDir)
	if err != nil {
		t.Fatalf("CreateWorkspace(repo) error: %v", err)
	}

	docPath := filepath.Join(docWorkspaceDir, "notes", "draft.md")
	docArtifact, err := s.CreateArtifact(ArtifactKindMarkdown, &docPath, nil, nil, nil)
	if err != nil {
		t.Fatalf("CreateArtifact(doc) error: %v", err)
	}
	inferredDoc, err := s.InferWorkspaceForArtifact(docArtifact)
	if err != nil {
		t.Fatalf("InferWorkspaceForArtifact(doc) error: %v", err)
	}
	if inferredDoc == nil || *inferredDoc != docWorkspace.ID {
		t.Fatalf("InferWorkspaceForArtifact(doc) = %v, want %d", inferredDoc, docWorkspace.ID)
	}

	issueURL := "https://github.com/owner/tabula/issues/214"
	ghArtifact, err := s.CreateArtifact(ArtifactKindGitHubIssue, nil, &issueURL, nil, nil)
	if err != nil {
		t.Fatalf("CreateArtifact(github) error: %v", err)
	}
	inferredGitHub, err := s.InferWorkspaceForArtifact(ghArtifact)
	if err != nil {
		t.Fatalf("InferWorkspaceForArtifact(github) error: %v", err)
	}
	if inferredGitHub == nil || *inferredGitHub != repoWorkspace.ID {
		t.Fatalf("InferWorkspaceForArtifact(github) = %v, want %d", inferredGitHub, repoWorkspace.ID)
	}

	metaJSON := `{"source_ref":"owner/tabula#PR-214"}`
	prArtifact, err := s.CreateArtifact(ArtifactKindGitHubPR, nil, nil, nil, &metaJSON)
	if err != nil {
		t.Fatalf("CreateArtifact(github pr) error: %v", err)
	}
	inferredPR, err := s.InferWorkspaceForArtifact(prArtifact)
	if err != nil {
		t.Fatalf("InferWorkspaceForArtifact(github pr) error: %v", err)
	}
	if inferredPR == nil || *inferredPR != repoWorkspace.ID {
		t.Fatalf("InferWorkspaceForArtifact(github pr) = %v, want %d", inferredPR, repoWorkspace.ID)
	}

	unknownURL := "https://github.com/owner/unknown/issues/1"
	unknownArtifact, err := s.CreateArtifact(ArtifactKindGitHubIssue, nil, &unknownURL, nil, nil)
	if err != nil {
		t.Fatalf("CreateArtifact(unknown github) error: %v", err)
	}
	inferredUnknown, err := s.InferWorkspaceForArtifact(unknownArtifact)
	if err != nil {
		t.Fatalf("InferWorkspaceForArtifact(unknown github) error: %v", err)
	}
	if inferredUnknown != nil {
		t.Fatalf("InferWorkspaceForArtifact(unknown github) = %v, want nil", *inferredUnknown)
	}
}

func TestWorkspaceArtifactLinksIncludeLinkedArtifactsInWorkspaceListings(t *testing.T) {
	s := newTestStore(t)

	sourceDir := filepath.Join(t.TempDir(), "source")
	targetDir := filepath.Join(t.TempDir(), "target")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	sourceWorkspace, err := s.CreateWorkspace("Source", sourceDir)
	if err != nil {
		t.Fatalf("CreateWorkspace(source) error: %v", err)
	}
	targetWorkspace, err := s.CreateWorkspace("Target", targetDir)
	if err != nil {
		t.Fatalf("CreateWorkspace(target) error: %v", err)
	}
	sourcePath := filepath.Join(sourceDir, "results.pdf")
	if err := os.WriteFile(sourcePath, []byte("pdf"), 0o644); err != nil {
		t.Fatalf("write results.pdf: %v", err)
	}
	sourceTitle := "results.pdf"
	sourceArtifact, err := s.CreateArtifact(ArtifactKindPDF, &sourcePath, nil, &sourceTitle, nil)
	if err != nil {
		t.Fatalf("CreateArtifact(source) error: %v", err)
	}
	targetPath := filepath.Join(targetDir, "notes.md")
	if err := os.WriteFile(targetPath, []byte("# notes\n"), 0o644); err != nil {
		t.Fatalf("write notes.md: %v", err)
	}
	targetTitle := "notes.md"
	targetArtifact, err := s.CreateArtifact(ArtifactKindMarkdown, &targetPath, nil, &targetTitle, nil)
	if err != nil {
		t.Fatalf("CreateArtifact(target) error: %v", err)
	}

	if err := s.LinkArtifactToWorkspace(targetWorkspace.ID, sourceArtifact.ID); err != nil {
		t.Fatalf("LinkArtifactToWorkspace() error: %v", err)
	}
	if err := s.LinkArtifactToWorkspace(targetWorkspace.ID, sourceArtifact.ID); err != nil {
		t.Fatalf("LinkArtifactToWorkspace(duplicate) error: %v", err)
	}

	links, err := s.ListArtifactWorkspaceLinks(targetWorkspace.ID)
	if err != nil {
		t.Fatalf("ListArtifactWorkspaceLinks() error: %v", err)
	}
	if len(links) != 1 || links[0].ArtifactID != sourceArtifact.ID {
		t.Fatalf("ListArtifactWorkspaceLinks() = %+v, want source artifact %d", links, sourceArtifact.ID)
	}

	linked, err := s.ListLinkedArtifacts(targetWorkspace.ID)
	if err != nil {
		t.Fatalf("ListLinkedArtifacts() error: %v", err)
	}
	if len(linked) != 1 || linked[0].ID != sourceArtifact.ID {
		t.Fatalf("ListLinkedArtifacts() = %+v, want source artifact %d", linked, sourceArtifact.ID)
	}

	targetArtifacts, err := s.ListArtifactsForWorkspace(targetWorkspace.ID)
	if err != nil {
		t.Fatalf("ListArtifactsForWorkspace(target) error: %v", err)
	}
	if len(targetArtifacts) != 2 {
		t.Fatalf("ListArtifactsForWorkspace(target) len = %d, want 2", len(targetArtifacts))
	}
	targetSeen := map[int64]bool{}
	for _, artifact := range targetArtifacts {
		targetSeen[artifact.ID] = true
	}
	if !targetSeen[sourceArtifact.ID] || !targetSeen[targetArtifact.ID] {
		t.Fatalf("ListArtifactsForWorkspace(target) ids = %#v", targetSeen)
	}

	sourceArtifacts, err := s.ListArtifactsForWorkspace(sourceWorkspace.ID)
	if err != nil {
		t.Fatalf("ListArtifactsForWorkspace(source) error: %v", err)
	}
	if len(sourceArtifacts) != 1 || sourceArtifacts[0].ID != sourceArtifact.ID {
		t.Fatalf("ListArtifactsForWorkspace(source) = %+v, want source artifact only", sourceArtifacts)
	}
}

func TestLinkArtifactToWorkspaceRejectsHomeWorkspace(t *testing.T) {
	s := newTestStore(t)

	workspaceDir := filepath.Join(t.TempDir(), "alpha")
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		t.Fatalf("mkdir alpha: %v", err)
	}
	workspace, err := s.CreateWorkspace("Alpha", workspaceDir)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	refPath := filepath.Join(workspaceDir, "spec.md")
	if err := os.WriteFile(refPath, []byte("# spec\n"), 0o644); err != nil {
		t.Fatalf("write spec.md: %v", err)
	}
	title := "spec.md"
	artifact, err := s.CreateArtifact(ArtifactKindMarkdown, &refPath, nil, &title, nil)
	if err != nil {
		t.Fatalf("CreateArtifact() error: %v", err)
	}

	if err := s.LinkArtifactToWorkspace(workspace.ID, artifact.ID); err == nil || err.Error() != "artifact already belongs to workspace" {
		t.Fatalf("LinkArtifactToWorkspace(home) error = %v, want home-workspace rejection", err)
	}
}

func TestCreateItemInfersWorkspaceFromArtifactWithoutOverridingExplicitWorkspace(t *testing.T) {
	s := newTestStore(t)

	artifactWorkspaceDir := filepath.Join(t.TempDir(), "artifact-workspace")
	explicitWorkspaceDir := filepath.Join(t.TempDir(), "explicit-workspace")
	artifactWorkspace, err := s.CreateWorkspace("Artifact Workspace", artifactWorkspaceDir)
	if err != nil {
		t.Fatalf("CreateWorkspace(artifact) error: %v", err)
	}
	explicitWorkspace, err := s.CreateWorkspace("Explicit Workspace", explicitWorkspaceDir)
	if err != nil {
		t.Fatalf("CreateWorkspace(explicit) error: %v", err)
	}

	docPath := filepath.Join(artifactWorkspaceDir, "docs", "task.md")
	artifact, err := s.CreateArtifact(ArtifactKindDocument, &docPath, nil, nil, nil)
	if err != nil {
		t.Fatalf("CreateArtifact() error: %v", err)
	}

	inferredItem, err := s.CreateItem("Infer from artifact", ItemOptions{ArtifactID: &artifact.ID})
	if err != nil {
		t.Fatalf("CreateItem(inferred) error: %v", err)
	}
	if inferredItem.WorkspaceID == nil || *inferredItem.WorkspaceID != artifactWorkspace.ID {
		t.Fatalf("CreateItem(inferred).WorkspaceID = %v, want %d", inferredItem.WorkspaceID, artifactWorkspace.ID)
	}

	explicitItem, err := s.CreateItem("Keep explicit workspace", ItemOptions{
		ArtifactID:  &artifact.ID,
		WorkspaceID: &explicitWorkspace.ID,
	})
	if err != nil {
		t.Fatalf("CreateItem(explicit) error: %v", err)
	}
	if explicitItem.WorkspaceID == nil || *explicitItem.WorkspaceID != explicitWorkspace.ID {
		t.Fatalf("CreateItem(explicit).WorkspaceID = %v, want %d", explicitItem.WorkspaceID, explicitWorkspace.ID)
	}
}

func initGitRepoWithRemote(t *testing.T, dirPath, remoteURL string) {
	t.Helper()
	if err := exec.Command("git", "init", dirPath).Run(); err != nil {
		t.Fatalf("git init %s: %v", dirPath, err)
	}
	if err := exec.Command("git", "-C", dirPath, "remote", "add", "origin", remoteURL).Run(); err != nil {
		t.Fatalf("git remote add origin %s: %v", dirPath, err)
	}
}
