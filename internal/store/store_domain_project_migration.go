package store

import (
	"database/sql"
	"strings"
)

func (s *Store) migrateProjectRemovalSupport() error {
	tableColumns, err := s.tableColumnSet("workspaces", "items", "time_entries", "external_container_mappings")
	if err != nil {
		return err
	}
	if !tableColumns["workspaces"]["project_id"] &&
		!tableColumns["items"]["project_id"] &&
		!tableColumns["time_entries"]["project_id"] &&
		!tableColumns["external_container_mappings"]["project_id"] {
		_, _ = s.db.Exec(`DROP TABLE IF EXISTS projects`)
		_, _ = s.db.Exec(`DELETE FROM app_state WHERE key = 'active_project_id'`)
		return s.repairProjectLegacyForeignKeyTargets()
	}
	if _, err := s.db.Exec(`PRAGMA foreign_keys = OFF`); err != nil {
		return err
	}
	defer func() {
		_, _ = s.db.Exec(`PRAGMA legacy_alter_table = OFF`)
		_, _ = s.db.Exec(`PRAGMA foreign_keys = ON`)
	}()
	if _, err := s.db.Exec(`PRAGMA legacy_alter_table = ON`); err != nil {
		return err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`ALTER TABLE workspaces RENAME TO workspaces_project_legacy`); err != nil {
		return err
	}
	if _, err := tx.Exec(`CREATE TABLE workspaces (
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
)`); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO workspaces (
id, name, dir_path, is_active, is_daily, daily_date, mcp_url, canvas_session_id, chat_model, chat_model_reasoning_effort, companion_config_json, created_at, updated_at
)
SELECT
id, name, dir_path, is_active, COALESCE(is_daily, 0), daily_date, COALESCE(mcp_url, ''), COALESCE(canvas_session_id, ''), COALESCE(chat_model, ''), COALESCE(chat_model_reasoning_effort, ''), COALESCE(companion_config_json, '{}'), created_at, updated_at
FROM workspaces_project_legacy`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DROP TABLE workspaces_project_legacy`); err != nil {
		return err
	}

	if _, err := tx.Exec(`ALTER TABLE items RENAME TO items_project_legacy`); err != nil {
		return err
	}
	if _, err := tx.Exec(strings.Replace(itemsTableSchema, "IF NOT EXISTS ", "", 1)); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO items (
id, title, state, workspace_id, artifact_id, actor_id, visible_after, follow_up_at, source, source_ref, review_target, reviewer, reviewed_at, created_at, updated_at
)
SELECT
id, title, state, workspace_id, artifact_id, actor_id, visible_after, follow_up_at, source, source_ref, review_target, reviewer, reviewed_at, created_at, updated_at
FROM items_project_legacy`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DROP TABLE items_project_legacy`); err != nil {
		return err
	}

	if _, err := tx.Exec(`ALTER TABLE time_entries RENAME TO time_entries_project_legacy`); err != nil {
		return err
	}
	if _, err := tx.Exec(strings.Replace(timeEntriesTableSchema, "IF NOT EXISTS ", "", 1)); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO time_entries (
id, workspace_id, started_at, ended_at, activity, notes
)
SELECT
id, workspace_id, started_at, ended_at, activity, notes
FROM time_entries_project_legacy`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DROP TABLE time_entries_project_legacy`); err != nil {
		return err
	}

	if _, err := tx.Exec(`ALTER TABLE external_container_mappings RENAME TO external_container_mappings_project_legacy`); err != nil {
		return err
	}
	if _, err := tx.Exec(`CREATE TABLE external_container_mappings (
  id INTEGER PRIMARY KEY,
  provider TEXT NOT NULL,
  container_type TEXT NOT NULL,
  container_ref TEXT NOT NULL,
  workspace_id INTEGER REFERENCES workspaces(id) ON DELETE SET NULL
)`); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO external_container_mappings (
id, provider, container_type, container_ref, workspace_id
)
SELECT
id, provider, container_type, container_ref, workspace_id
FROM external_container_mappings_project_legacy`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DROP TABLE external_container_mappings_project_legacy`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DROP TABLE IF EXISTS projects`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM app_state WHERE key = 'active_project_id'`); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return s.repairProjectLegacyForeignKeyTargets()
}

type projectLegacyForeignKeyRepairSpec struct {
	table          string
	createTableSQL string
	postCopySQL    []string
	columns        []string
}

func (s *Store) repairProjectLegacyForeignKeyTargets() error {
	specs := []projectLegacyForeignKeyRepairSpec{
		{
			table: "workspace_artifact_links",
			createTableSQL: `CREATE TABLE workspace_artifact_links (
  workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  artifact_id INTEGER NOT NULL REFERENCES artifacts(id) ON DELETE CASCADE,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  PRIMARY KEY (workspace_id, artifact_id)
)`,
			columns: []string{"workspace_id", "artifact_id", "created_at"},
		},
		{
			table: "external_bindings",
			createTableSQL: `CREATE TABLE external_bindings (
  id INTEGER PRIMARY KEY,
  account_id INTEGER NOT NULL REFERENCES external_accounts(id) ON DELETE CASCADE,
  provider TEXT NOT NULL,
  object_type TEXT NOT NULL,
  remote_id TEXT NOT NULL,
  item_id INTEGER REFERENCES items(id) ON DELETE SET NULL,
  artifact_id INTEGER REFERENCES artifacts(id) ON DELETE SET NULL,
  container_ref TEXT,
  remote_updated_at TEXT,
  last_synced_at TEXT NOT NULL DEFAULT (datetime('now'))
);`,
			postCopySQL: []string{
				`CREATE UNIQUE INDEX idx_external_bindings_identity
  ON external_bindings(account_id, provider, object_type, remote_id)`,
				`CREATE INDEX idx_external_bindings_stale
  ON external_bindings(provider, last_synced_at)`,
			},
			columns: []string{"id", "account_id", "provider", "object_type", "remote_id", "item_id", "artifact_id", "container_ref", "remote_updated_at", "last_synced_at"},
		},
		{
			table: "item_artifacts",
			createTableSQL: `CREATE TABLE item_artifacts (
  item_id INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  artifact_id INTEGER NOT NULL REFERENCES artifacts(id) ON DELETE CASCADE,
  role TEXT NOT NULL DEFAULT 'related' CHECK (role IN ('source', 'related', 'output')),
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  PRIMARY KEY (item_id, artifact_id)
);`,
			postCopySQL: []string{
				`CREATE INDEX idx_item_artifacts_artifact_id
  ON item_artifacts(artifact_id)`,
			},
			columns: []string{"item_id", "artifact_id", "role", "created_at"},
		},
		{
			table: "batch_runs",
			createTableSQL: `CREATE TABLE batch_runs (
  id INTEGER PRIMARY KEY,
  workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  started_at TEXT NOT NULL DEFAULT (datetime('now')),
  finished_at TEXT,
  config_json TEXT NOT NULL DEFAULT '{}',
  status TEXT NOT NULL DEFAULT 'running'
);`,
			postCopySQL: []string{
				`CREATE INDEX idx_batch_runs_workspace_started
  ON batch_runs(workspace_id, datetime(started_at) DESC, id DESC)`,
			},
			columns: []string{"id", "workspace_id", "started_at", "finished_at", "config_json", "status"},
		},
		{
			table: "batch_run_items",
			createTableSQL: `CREATE TABLE batch_run_items (
  batch_id INTEGER NOT NULL REFERENCES batch_runs(id) ON DELETE CASCADE,
  item_id INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  status TEXT NOT NULL DEFAULT 'pending',
  pr_number INTEGER,
  pr_url TEXT,
  error_msg TEXT,
  started_at TEXT,
  finished_at TEXT,
  PRIMARY KEY (batch_id, item_id)
);`,
			postCopySQL: []string{
				`CREATE INDEX idx_batch_run_items_batch_status
  ON batch_run_items(batch_id, status, item_id)`,
			},
			columns: []string{"batch_id", "item_id", "status", "pr_number", "pr_url", "error_msg", "started_at", "finished_at"},
		},
		{
			table: "workspace_watches",
			createTableSQL: `CREATE TABLE workspace_watches (
  workspace_id INTEGER PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE,
  config_json TEXT NOT NULL DEFAULT '{}',
  poll_interval_seconds INTEGER NOT NULL DEFAULT 300,
  enabled INTEGER NOT NULL DEFAULT 0,
  current_batch_id INTEGER REFERENCES batch_runs(id) ON DELETE SET NULL,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at TEXT NOT NULL DEFAULT (datetime('now'))
)`,
			columns: []string{"workspace_id", "config_json", "poll_interval_seconds", "enabled", "current_batch_id", "created_at", "updated_at"},
		},
		{
			table: "context_items",
			createTableSQL: `CREATE TABLE context_items (
  context_id INTEGER NOT NULL REFERENCES contexts(id) ON DELETE CASCADE,
  item_id INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  PRIMARY KEY (context_id, item_id)
)`,
			columns: []string{"context_id", "item_id"},
		},
		{
			table: "context_workspaces",
			createTableSQL: `CREATE TABLE context_workspaces (
  context_id INTEGER NOT NULL REFERENCES contexts(id) ON DELETE CASCADE,
  workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  PRIMARY KEY (context_id, workspace_id)
)`,
			columns: []string{"context_id", "workspace_id"},
		},
		{
			table: "context_external_container_mappings",
			createTableSQL: `CREATE TABLE context_external_container_mappings (
  context_id INTEGER NOT NULL REFERENCES contexts(id) ON DELETE CASCADE,
  mapping_id INTEGER NOT NULL REFERENCES external_container_mappings(id) ON DELETE CASCADE,
  PRIMARY KEY (context_id, mapping_id)
)`,
			columns: []string{"context_id", "mapping_id"},
		},
		{
			table: "context_time_entries",
			createTableSQL: `CREATE TABLE context_time_entries (
  context_id INTEGER NOT NULL REFERENCES contexts(id) ON DELETE CASCADE,
  time_entry_id INTEGER NOT NULL REFERENCES time_entries(id) ON DELETE CASCADE,
  PRIMARY KEY (context_id, time_entry_id)
)`,
			columns: []string{"context_id", "time_entry_id"},
		},
		{
			table: "participant_sessions",
			createTableSQL: `CREATE TABLE participant_sessions (
  id TEXT PRIMARY KEY,
  workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  started_at INTEGER NOT NULL,
  ended_at INTEGER NOT NULL DEFAULT 0,
  config_json TEXT NOT NULL DEFAULT '{}'
)`,
			columns: []string{"id", "workspace_id", "started_at", "ended_at", "config_json"},
		},
		{
			table: "chat_sessions",
			createTableSQL: `CREATE TABLE chat_sessions (
  id TEXT PRIMARY KEY,
  workspace_id INTEGER NOT NULL UNIQUE REFERENCES workspaces(id) ON DELETE CASCADE,
  app_thread_id TEXT NOT NULL DEFAULT '',
  mode TEXT NOT NULL DEFAULT 'chat',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
)`,
			columns: []string{"id", "workspace_id", "app_thread_id", "mode", "created_at", "updated_at"},
		},
	}

	repairNeeded := make([]projectLegacyForeignKeyRepairSpec, 0, len(specs))
	for _, spec := range specs {
		needsRepair, err := s.tableDefinitionContains(spec.table, "_project_legacy")
		if err != nil {
			return err
		}
		if needsRepair {
			repairNeeded = append(repairNeeded, spec)
		}
	}
	if len(repairNeeded) == 0 {
		return nil
	}

	if _, err := s.db.Exec(`PRAGMA foreign_keys = OFF`); err != nil {
		return err
	}
	defer func() {
		_, _ = s.db.Exec(`PRAGMA legacy_alter_table = OFF`)
		_, _ = s.db.Exec(`PRAGMA foreign_keys = ON`)
	}()
	if _, err := s.db.Exec(`PRAGMA legacy_alter_table = ON`); err != nil {
		return err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, spec := range repairNeeded {
		if err := rebuildTableWithCurrentForeignKeys(tx, spec); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) tableDefinitionContains(table, needle string) (bool, error) {
	var sqlText sql.NullString
	if err := s.db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&sqlText); err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	return strings.Contains(strings.ToLower(sqlText.String), strings.ToLower(needle)), nil
}

func rebuildTableWithCurrentForeignKeys(tx *sql.Tx, spec projectLegacyForeignKeyRepairSpec) error {
	legacyName := spec.table + "_project_fk_legacy"
	if _, err := tx.Exec(`ALTER TABLE ` + spec.table + ` RENAME TO ` + legacyName); err != nil {
		return err
	}
	if _, err := tx.Exec(spec.createTableSQL); err != nil {
		return err
	}
	columnList := strings.Join(spec.columns, ", ")
	if _, err := tx.Exec(`INSERT INTO ` + spec.table + ` (` + columnList + `)
SELECT ` + columnList + `
FROM ` + legacyName); err != nil {
		return err
	}
	if _, err := tx.Exec(`DROP TABLE ` + legacyName); err != nil {
		return err
	}
	for _, stmt := range spec.postCopySQL {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}
