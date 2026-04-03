package store

import (
	"database/sql"
	"errors"
	"strings"
)

var scopedContextNames = []string{SphereWork, SpherePrivate}

func scopedContextSelect(linkTable, entityColumn, entityExpr string) string {
	return `COALESCE((SELECT lower(c.name)
FROM contexts c
JOIN ` + linkTable + ` link ON link.context_id = c.id
WHERE link.` + entityColumn + ` = ` + entityExpr + `
  AND c.parent_id IS NULL
  AND lower(c.name) IN ('work', 'private')
ORDER BY CASE lower(c.name) WHEN 'work' THEN 0 ELSE 1 END, c.id
LIMIT 1), 'private')`
}

func scopedContextFilter(linkTable, entityColumn, entityExpr string) string {
	return `EXISTS (
SELECT 1
FROM contexts c
JOIN ` + linkTable + ` link ON link.context_id = c.id
WHERE link.` + entityColumn + ` = ` + entityExpr + `
  AND c.parent_id IS NULL
  AND lower(c.name) = lower(?)
)`
}

func (s *Store) ensureScopedContexts() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := s.ensureScopedContextsTx(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ensureScopedContextsTx(tx *sql.Tx) error {
	for _, name := range scopedContextNames {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO contexts (name) VALUES (?)`, name); err != nil {
			return err
		}
	}
	return nil
}

func scopeContextIDTx(tx *sql.Tx, scope string) (int64, error) {
	cleanScope := normalizeRequiredSphere(scope)
	if cleanScope == "" {
		return 0, errors.New("scope must be work or private")
	}
	var contextID int64
	if err := tx.QueryRow(
		`SELECT id
		 FROM contexts
		 WHERE parent_id IS NULL
		   AND lower(name) = lower(?)`,
		cleanScope,
	).Scan(&contextID); err != nil {
		return 0, err
	}
	return contextID, nil
}

func (s *Store) syncScopedContextLinkTx(tx *sql.Tx, linkTable, entityColumn string, entityID int64, scope string) error {
	if entityID <= 0 {
		return errors.New("entity id must be positive")
	}
	cleanScope := normalizeRequiredSphere(scope)
	if cleanScope == "" {
		return errors.New("scope must be work or private")
	}
	if err := s.ensureScopedContextsTx(tx); err != nil {
		return err
	}
	contextID, err := scopeContextIDTx(tx, cleanScope)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(
		`DELETE FROM `+linkTable+`
		 WHERE `+entityColumn+` = ?
		   AND context_id IN (
		     SELECT id
		     FROM contexts
		     WHERE parent_id IS NULL
		       AND lower(name) IN ('work', 'private')
		   )`,
		entityID,
	); err != nil {
		return err
	}
	_, err = tx.Exec(
		`INSERT OR IGNORE INTO `+linkTable+` (context_id, `+entityColumn+`)
		 VALUES (?, ?)`,
		contextID,
		entityID,
	)
	return err
}

func (s *Store) syncScopedContextLink(linkTable, entityColumn string, entityID int64, scope string) error {
	if entityID <= 0 {
		return errors.New("entity id must be positive")
	}
	cleanScope := normalizeRequiredSphere(scope)
	if cleanScope == "" {
		return errors.New("scope must be work or private")
	}
	if err := s.ensureScopedContexts(); err != nil {
		return err
	}
	contextID, err := s.contextIDByName(cleanScope)
	if err != nil {
		return err
	}
	if _, err := s.db.Exec(
		`DELETE FROM `+linkTable+`
		 WHERE `+entityColumn+` = ?
		   AND context_id IN (
		     SELECT id
		     FROM contexts
		     WHERE parent_id IS NULL
		       AND lower(name) IN ('work', 'private')
		   )`,
		entityID,
	); err != nil {
		return err
	}
	_, err = s.db.Exec(
		`INSERT OR IGNORE INTO `+linkTable+` (context_id, `+entityColumn+`)
		 VALUES (?, ?)`,
		contextID,
		entityID,
	)
	return err
}

func (s *Store) currentScopedContext(scopeExpr string, args ...any) (string, error) {
	var scope sql.NullString
	if err := s.db.QueryRow(scopeExpr, args...).Scan(&scope); err != nil {
		return "", err
	}
	return normalizeSphere(scope.String), nil
}

func (s *Store) migrateSphereToContextSupport() (err error) {
	if err := s.ensureScopedContexts(); err != nil {
		return err
	}
	tableColumns, err := s.tableColumnSet("workspaces", "items", "time_entries", "external_accounts", "external_container_mappings")
	if err != nil {
		return err
	}
	if !tableColumns["workspaces"]["sphere"] &&
		!tableColumns["items"]["sphere"] &&
		!tableColumns["time_entries"]["sphere"] &&
		!tableColumns["external_accounts"]["sphere"] &&
		!tableColumns["external_container_mappings"]["sphere"] {
		return nil
	}
	// Keep dependent foreign keys pointed at the final table names while legacy
	// sphere tables are renamed out of the way and recreated without that column.
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

	if err := s.ensureScopedContextsTx(tx); err != nil {
		return err
	}

	if tableColumns["workspaces"]["sphere"] {
		if _, err := tx.Exec(`CREATE TEMP TABLE scope_workspaces_migration AS
SELECT id, sphere
FROM workspaces
WHERE lower(trim(sphere)) IN ('work', 'private')`); err != nil {
			return err
		}
		if _, err := tx.Exec(`ALTER TABLE workspaces RENAME TO workspaces_scope_legacy`); err != nil {
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
	id, name, dir_path, is_active, COALESCE(is_daily, 0), daily_date, mcp_url, canvas_session_id, chat_model, chat_model_reasoning_effort, '{}', created_at, updated_at
FROM workspaces_scope_legacy`); err != nil {
			return err
		}
		if _, err := tx.Exec(`DROP TABLE workspaces_scope_legacy`); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO context_workspaces (context_id, workspace_id)
SELECT c.id, m.id
FROM scope_workspaces_migration m
JOIN contexts c ON c.parent_id IS NULL AND lower(c.name) = lower(m.sphere)`); err != nil {
			return err
		}
		if _, err := tx.Exec(`DROP TABLE scope_workspaces_migration`); err != nil {
			return err
		}
	}

	if tableColumns["items"]["sphere"] {
		if _, err := tx.Exec(`CREATE TEMP TABLE scope_items_migration AS
SELECT id, sphere
FROM items
WHERE lower(trim(sphere)) IN ('work', 'private')`); err != nil {
			return err
		}
		if _, err := tx.Exec(`ALTER TABLE items RENAME TO items_scope_legacy`); err != nil {
			return err
		}
		if _, err := tx.Exec(`CREATE TABLE items (
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
)`); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO items (
	id, title, state, workspace_id, artifact_id, actor_id, visible_after, follow_up_at, source, source_ref, review_target, reviewer, reviewed_at, created_at, updated_at
)
SELECT
	id, title, state, workspace_id, artifact_id, actor_id, visible_after, follow_up_at, source, source_ref, review_target, reviewer, reviewed_at, created_at, updated_at
FROM items_scope_legacy`); err != nil {
			return err
		}
		if _, err := tx.Exec(`DROP TABLE items_scope_legacy`); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO context_items (context_id, item_id)
SELECT c.id, m.id
FROM scope_items_migration m
JOIN contexts c ON c.parent_id IS NULL AND lower(c.name) = lower(m.sphere)`); err != nil {
			return err
		}
		if _, err := tx.Exec(`DROP TABLE scope_items_migration`); err != nil {
			return err
		}
	}

	if tableColumns["time_entries"]["sphere"] {
		if _, err := tx.Exec(`CREATE TEMP TABLE scope_time_entries_migration AS
SELECT id, sphere
FROM time_entries
WHERE lower(trim(sphere)) IN ('work', 'private')`); err != nil {
			return err
		}
		if _, err := tx.Exec(`ALTER TABLE time_entries RENAME TO time_entries_scope_legacy`); err != nil {
			return err
		}
		if _, err := tx.Exec(`CREATE TABLE time_entries (
  id INTEGER PRIMARY KEY,
  workspace_id INTEGER REFERENCES workspaces(id) ON DELETE SET NULL,
  started_at TEXT NOT NULL,
  ended_at TEXT,
  activity TEXT NOT NULL DEFAULT '',
  notes TEXT
)`); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO time_entries (
	id, workspace_id, started_at, ended_at, activity, notes
)
SELECT
	id, workspace_id, started_at, ended_at, activity, notes
FROM time_entries_scope_legacy`); err != nil {
			return err
		}
		if _, err := tx.Exec(`DROP TABLE time_entries_scope_legacy`); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO context_time_entries (context_id, time_entry_id)
SELECT c.id, m.id
FROM scope_time_entries_migration m
JOIN contexts c ON c.parent_id IS NULL AND lower(c.name) = lower(m.sphere)`); err != nil {
			return err
		}
		if _, err := tx.Exec(`DROP TABLE scope_time_entries_migration`); err != nil {
			return err
		}
	}

	if tableColumns["external_accounts"]["sphere"] {
		if _, err := tx.Exec(`CREATE TEMP TABLE scope_external_accounts_migration AS
SELECT id, sphere
FROM external_accounts
WHERE lower(trim(sphere)) IN ('work', 'private')`); err != nil {
			return err
		}
		if _, err := tx.Exec(`ALTER TABLE external_accounts RENAME TO external_accounts_scope_legacy`); err != nil {
			return err
		}
		if _, err := tx.Exec(`CREATE TABLE external_accounts (
  id INTEGER PRIMARY KEY,
  provider TEXT NOT NULL,
  label TEXT NOT NULL,
  config_json TEXT NOT NULL DEFAULT '{}',
  enabled INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at TEXT NOT NULL DEFAULT (datetime('now'))
)`); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO external_accounts (
	id, provider, label, config_json, enabled, created_at, updated_at
)
SELECT
	id, provider, label, config_json, enabled, created_at, updated_at
FROM external_accounts_scope_legacy`); err != nil {
			return err
		}
		if _, err := tx.Exec(`DROP TABLE external_accounts_scope_legacy`); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO context_external_accounts (context_id, account_id)
SELECT c.id, m.id
FROM scope_external_accounts_migration m
JOIN contexts c ON c.parent_id IS NULL AND lower(c.name) = lower(m.sphere)`); err != nil {
			return err
		}
		if _, err := tx.Exec(`DROP TABLE scope_external_accounts_migration`); err != nil {
			return err
		}
	}

	if tableColumns["external_container_mappings"]["sphere"] {
		if _, err := tx.Exec(`CREATE TEMP TABLE scope_external_container_mappings_migration AS
SELECT id, sphere
FROM external_container_mappings
WHERE lower(trim(sphere)) IN ('work', 'private')`); err != nil {
			return err
		}
		if _, err := tx.Exec(`ALTER TABLE external_container_mappings RENAME TO external_container_mappings_scope_legacy`); err != nil {
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
FROM external_container_mappings_scope_legacy`); err != nil {
			return err
		}
		if _, err := tx.Exec(`DROP TABLE external_container_mappings_scope_legacy`); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO context_external_container_mappings (context_id, mapping_id)
SELECT c.id, m.id
FROM scope_external_container_mappings_migration m
JOIN contexts c ON c.parent_id IS NULL AND lower(c.name) = lower(m.sphere)`); err != nil {
			return err
		}
		if _, err := tx.Exec(`DROP TABLE scope_external_container_mappings_migration`); err != nil {
			return err
		}
	}

	if _, err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_external_accounts_identity
  ON external_accounts(lower(provider), lower(label))`); err != nil {
		return err
	}
	if _, err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_external_container_mappings_identity
  ON external_container_mappings(lower(provider), lower(container_type), lower(container_ref))`); err != nil {
		return err
	}
	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS context_items (
  context_id INTEGER NOT NULL REFERENCES contexts(id) ON DELETE CASCADE,
  item_id INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  PRIMARY KEY (context_id, item_id)
)`); err != nil {
		return err
	}
	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS context_workspaces (
  context_id INTEGER NOT NULL REFERENCES contexts(id) ON DELETE CASCADE,
  workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  PRIMARY KEY (context_id, workspace_id)
)`); err != nil {
		return err
	}
	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS context_external_accounts (
  context_id INTEGER NOT NULL REFERENCES contexts(id) ON DELETE CASCADE,
  account_id INTEGER NOT NULL REFERENCES external_accounts(id) ON DELETE CASCADE,
  PRIMARY KEY (context_id, account_id)
)`); err != nil {
		return err
	}
	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS context_external_container_mappings (
  context_id INTEGER NOT NULL REFERENCES contexts(id) ON DELETE CASCADE,
  mapping_id INTEGER NOT NULL REFERENCES external_container_mappings(id) ON DELETE CASCADE,
  PRIMARY KEY (context_id, mapping_id)
)`); err != nil {
		return err
	}
	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS context_time_entries (
  context_id INTEGER NOT NULL REFERENCES contexts(id) ON DELETE CASCADE,
  time_entry_id INTEGER NOT NULL REFERENCES time_entries(id) ON DELETE CASCADE,
  PRIMARY KEY (context_id, time_entry_id)
)`); err != nil {
		return err
	}

	return tx.Commit()
}

func scopedContextFromName(scope *string) string {
	if scope == nil {
		return ""
	}
	return normalizeSphere(strings.TrimSpace(*scope))
}
