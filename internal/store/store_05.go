package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
)

func (s *Store) ListWorkspacesByContextPrefix(prefix string) ([]Workspace, error) {
	cleanPrefix := normalizeOptionalContextQuery(prefix)
	if cleanPrefix == "" {
		return nil, errors.New("context is required")
	}
	terms := splitContextQueryTerms(cleanPrefix)
	clauses := make([]string, 0, len(terms))
	args := []any{}
	for _, term := range terms {
		contextIDs, err := s.resolveContextQueryIDs(term)
		if err != nil {
			return nil, err
		}
		if len(contextIDs) == 0 {
			return []Workspace{}, nil
		}
		clause, clauseArgs := contextLinkExistsClause("context_workspaces", "workspace_id", "workspaces.id", contextIDs)
		clauses = append(clauses, clause)
		args = append(args, clauseArgs...)
	}
	rows, err := s.db.Query(`SELECT id, name, dir_path, `+scopedContextSelect("context_workspaces", "workspace_id", "workspaces.id")+` AS sphere, is_active, is_daily, daily_date, mcp_url, canvas_session_id, chat_model, chat_model_reasoning_effort, companion_config_json, created_at, updated_at
		 FROM workspaces
		 WHERE `+strings.Join(clauses, ` AND `), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Workspace
	for rows.Next() {
		workspace, err := scanWorkspace(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, workspace)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsActive != out[j].IsActive {
			return out[i].IsActive
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

func (s *Store) ListItemsByContextPrefix(prefix string) ([]Item, error) {
	cleanPrefix := normalizeOptionalContextQuery(prefix)
	if cleanPrefix == "" {
		return nil, errors.New("context is required")
	}
	return s.ListItemsFiltered(ItemListFilter{Label: cleanPrefix})
}

func (s *Store) filterArtifactsByContextIDs(artifacts []Artifact, contextIDs []int64) ([]Artifact, error) {
	if len(contextIDs) == 0 {
		return []Artifact{}, nil
	}
	out := make([]Artifact, 0, len(artifacts))
	for _, artifact := range artifacts {
		matched, err := s.entityHasAnyContext("context_artifacts", "artifact_id", artifact.ID, contextIDs)
		if err != nil {
			return nil, err
		}
		if matched {
			out = append(out, artifact)
			continue
		}
		homeWorkspaceID, err := s.InferWorkspaceForArtifact(artifact)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		if homeWorkspaceID != nil {
			matched, err = s.workspaceHasAnyContext(*homeWorkspaceID, contextIDs)
			if err != nil {
				return nil, err
			}
			if matched {
				out = append(out, artifact)
				continue
			}
		}
		workspaces, err := s.ListArtifactLinkWorkspaces(artifact.ID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		for _, workspace := range workspaces {
			matched, err = s.workspaceHasAnyContext(workspace.ID, contextIDs)
			if err != nil {
				return nil, err
			}
			if matched {
				out = append(out, artifact)
				break
			}
		}
	}
	sortArtifactsNewestFirst(out)
	return out, nil
}

func (s *Store) ListArtifactsByContextPrefix(prefix string) ([]Artifact, error) {
	cleanPrefix := normalizeOptionalContextQuery(prefix)
	if cleanPrefix == "" {
		return nil, errors.New("context is required")
	}
	artifacts, err := s.ListArtifacts()
	if err != nil {
		return nil, err
	}
	terms := splitContextQueryTerms(cleanPrefix)
	filtered := artifacts
	for _, term := range terms {
		contextIDs, err := s.resolveContextQueryIDs(term)
		if err != nil {
			return nil, err
		}
		filtered, err = s.filterArtifactsByContextIDs(filtered, contextIDs)
		if err != nil {
			return nil, err
		}
		if len(filtered) == 0 {
			return []Artifact{}, nil
		}
	}
	return filtered, nil
}

const itemsTableSchema = `CREATE TABLE IF NOT EXISTS items (
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
);`

func (s *Store) migrateDomainTables() error {
	schema := `
CREATE TABLE IF NOT EXISTS workspaces (
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
CREATE TABLE IF NOT EXISTS contexts (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  color TEXT NOT NULL DEFAULT '',
  parent_id INTEGER REFERENCES contexts(id) ON DELETE SET NULL,
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_contexts_name_lower
  ON contexts(lower(name));
CREATE TABLE IF NOT EXISTS context_items (
  context_id INTEGER NOT NULL REFERENCES contexts(id) ON DELETE CASCADE,
  item_id INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  PRIMARY KEY (context_id, item_id)
);
CREATE TABLE IF NOT EXISTS context_artifacts (
  context_id INTEGER NOT NULL REFERENCES contexts(id) ON DELETE CASCADE,
  artifact_id INTEGER NOT NULL REFERENCES artifacts(id) ON DELETE CASCADE,
  PRIMARY KEY (context_id, artifact_id)
);
CREATE TABLE IF NOT EXISTS context_workspaces (
  context_id INTEGER NOT NULL REFERENCES contexts(id) ON DELETE CASCADE,
  workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  PRIMARY KEY (context_id, workspace_id)
);
CREATE TABLE IF NOT EXISTS actors (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  kind TEXT NOT NULL CHECK (kind IN ('human', 'agent')),
  email TEXT,
  provider TEXT,
  provider_ref TEXT,
  meta_json TEXT,
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE TABLE IF NOT EXISTS artifacts (
  id INTEGER PRIMARY KEY,
  kind TEXT NOT NULL,
  ref_path TEXT,
  ref_url TEXT,
  title TEXT,
  meta_json TEXT,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE TABLE IF NOT EXISTS external_accounts (
  id INTEGER PRIMARY KEY,
  provider TEXT NOT NULL,
  label TEXT NOT NULL,
  config_json TEXT NOT NULL DEFAULT '{}',
  enabled INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_external_accounts_identity
  ON external_accounts(lower(provider), lower(label));
CREATE TABLE IF NOT EXISTS external_container_mappings (
  id INTEGER PRIMARY KEY,
  provider TEXT NOT NULL,
  container_type TEXT NOT NULL,
  container_ref TEXT NOT NULL,
  workspace_id INTEGER REFERENCES workspaces(id) ON DELETE SET NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_external_container_mappings_identity
  ON external_container_mappings(lower(provider), lower(container_type), lower(container_ref));
CREATE TABLE IF NOT EXISTS workspace_artifact_links (
  workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  artifact_id INTEGER NOT NULL REFERENCES artifacts(id) ON DELETE CASCADE,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  PRIMARY KEY (workspace_id, artifact_id)
);
CREATE TABLE IF NOT EXISTS external_bindings (
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
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_external_bindings_identity
  ON external_bindings(account_id, provider, object_type, remote_id);
CREATE INDEX IF NOT EXISTS idx_external_bindings_stale
  ON external_bindings(provider, last_synced_at);
CREATE TABLE IF NOT EXISTS batch_runs (
  id INTEGER PRIMARY KEY,
  workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  started_at TEXT NOT NULL DEFAULT (datetime('now')),
  finished_at TEXT,
  config_json TEXT NOT NULL DEFAULT '{}',
  status TEXT NOT NULL DEFAULT 'running'
);
CREATE INDEX IF NOT EXISTS idx_batch_runs_workspace_started
  ON batch_runs(workspace_id, datetime(started_at) DESC, id DESC);
CREATE TABLE IF NOT EXISTS batch_run_items (
  batch_id INTEGER NOT NULL REFERENCES batch_runs(id) ON DELETE CASCADE,
  item_id INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  status TEXT NOT NULL DEFAULT 'pending',
  pr_number INTEGER,
  pr_url TEXT,
  error_msg TEXT,
  started_at TEXT,
  finished_at TEXT,
  PRIMARY KEY (batch_id, item_id)
);
CREATE INDEX IF NOT EXISTS idx_batch_run_items_batch_status
  ON batch_run_items(batch_id, status, item_id);
CREATE TABLE IF NOT EXISTS workspace_watches (
  workspace_id INTEGER PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE,
  config_json TEXT NOT NULL DEFAULT '{}',
  poll_interval_seconds INTEGER NOT NULL DEFAULT 300,
  enabled INTEGER NOT NULL DEFAULT 0,
  current_batch_id INTEGER REFERENCES batch_runs(id) ON DELETE SET NULL,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE TABLE IF NOT EXISTS mail_triage_reviews (
  id INTEGER PRIMARY KEY,
  account_id INTEGER NOT NULL REFERENCES external_accounts(id) ON DELETE CASCADE,
  provider TEXT NOT NULL,
  message_id TEXT NOT NULL,
  folder TEXT NOT NULL DEFAULT '',
  subject TEXT NOT NULL DEFAULT '',
  sender TEXT NOT NULL DEFAULT '',
  action TEXT NOT NULL CHECK (action IN ('inbox', 'cc', 'archive', 'trash')),
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_mail_triage_reviews_account_created
  ON mail_triage_reviews(account_id, datetime(created_at) DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_mail_triage_reviews_message
  ON mail_triage_reviews(account_id, message_id);
CREATE TABLE IF NOT EXISTS mail_action_logs (
  id INTEGER PRIMARY KEY,
  account_id INTEGER NOT NULL REFERENCES external_accounts(id) ON DELETE CASCADE,
  provider TEXT NOT NULL,
  message_id TEXT NOT NULL,
  resolved_message_id TEXT NOT NULL DEFAULT '',
  action TEXT NOT NULL,
  folder_from TEXT NOT NULL DEFAULT '',
  folder_to TEXT NOT NULL DEFAULT '',
  subject TEXT NOT NULL DEFAULT '',
  sender TEXT NOT NULL DEFAULT '',
  request_json TEXT NOT NULL DEFAULT '{}',
  status TEXT NOT NULL CHECK (status IN ('pending', 'applied', 'failed', 'reconcile_failed')),
  error_text TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_mail_action_logs_account_created
  ON mail_action_logs(account_id, datetime(created_at) DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_mail_action_logs_message
  ON mail_action_logs(account_id, message_id);
CREATE TABLE IF NOT EXISTS push_registrations (
  id INTEGER PRIMARY KEY,
  session_id TEXT NOT NULL DEFAULT '',
  workspace_id INTEGER NOT NULL DEFAULT 0,
  platform TEXT NOT NULL CHECK (platform IN ('apns', 'fcm')),
  device_token TEXT NOT NULL,
  device_label TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_push_registrations_identity
  ON push_registrations(platform, device_token, session_id, workspace_id);
`
	if _, err := s.db.Exec(schema); err != nil {
		return err
	}
	if _, err := s.db.Exec(itemsTableSchema); err != nil {
		return err
	}
	if _, err := s.db.Exec(timeEntriesTableSchema); err != nil {
		return err
	}
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS context_external_accounts (
  context_id INTEGER NOT NULL REFERENCES contexts(id) ON DELETE CASCADE,
  account_id INTEGER NOT NULL REFERENCES external_accounts(id) ON DELETE CASCADE,
  PRIMARY KEY (context_id, account_id)
);
CREATE TABLE IF NOT EXISTS context_external_container_mappings (
  context_id INTEGER NOT NULL REFERENCES contexts(id) ON DELETE CASCADE,
  mapping_id INTEGER NOT NULL REFERENCES external_container_mappings(id) ON DELETE CASCADE,
  PRIMARY KEY (context_id, mapping_id)
);
CREATE TABLE IF NOT EXISTS context_time_entries (
  context_id INTEGER NOT NULL REFERENCES contexts(id) ON DELETE CASCADE,
  time_entry_id INTEGER NOT NULL REFERENCES time_entries(id) ON DELETE CASCADE,
  PRIMARY KEY (context_id, time_entry_id)
);`); err != nil {
		return err
	}
	if err := s.migrateItemTableStateSupport(); err != nil {
		return err
	}
	if err := s.migrateProjectRemovalSupport(); err != nil {
		return err
	}
	if err := s.migrateWorkspaceConfigSupport(); err != nil {
		return err
	}
	if err := s.migrateSphereToContextSupport(); err != nil {
		return err
	}
	if err := s.migrateWorkspaceDailySupport(); err != nil {
		return err
	}
	if err := s.migrateActorContactSupport(); err != nil {
		return err
	}
	if err := s.migrateItemReviewDispatchSupport(); err != nil {
		return err
	}
	if err := s.migrateMailTriageReviewActionSupport(); err != nil {
		return err
	}
	return s.migrateItemArtifactLinkSupport()
}

func (s *Store) migrateMailTriageReviewActionSupport() error {
	containsInbox, err := s.tableDefinitionContains("mail_triage_reviews", "'inbox'")
	if err != nil {
		return err
	}
	containsKeep, err := s.tableDefinitionContains("mail_triage_reviews", "'keep'")
	if err != nil {
		return err
	}
	if containsInbox && !containsKeep {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`ALTER TABLE mail_triage_reviews RENAME TO mail_triage_reviews_legacy_action`); err != nil {
		return err
	}
	if _, err := tx.Exec(`CREATE TABLE mail_triage_reviews (
  id INTEGER PRIMARY KEY,
  account_id INTEGER NOT NULL REFERENCES external_accounts(id) ON DELETE CASCADE,
  provider TEXT NOT NULL,
  message_id TEXT NOT NULL,
  folder TEXT NOT NULL DEFAULT '',
  subject TEXT NOT NULL DEFAULT '',
  sender TEXT NOT NULL DEFAULT '',
  action TEXT NOT NULL CHECK (action IN ('inbox', 'cc', 'archive', 'trash')),
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
)`); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO mail_triage_reviews (id, account_id, provider, message_id, folder, subject, sender, action, created_at)
SELECT id, account_id, provider, message_id, folder, subject, sender,
CASE
  WHEN lower(action) IN ('keep', 'rescue') THEN 'inbox'
  ELSE action
END,
created_at
FROM mail_triage_reviews_legacy_action`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DROP TABLE mail_triage_reviews_legacy_action`); err != nil {
		return err
	}
	if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_mail_triage_reviews_account_created
  ON mail_triage_reviews(account_id, datetime(created_at) DESC, id DESC)`); err != nil {
		return err
	}
	if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_mail_triage_reviews_message
  ON mail_triage_reviews(account_id, message_id)`); err != nil {
		return err
	}
	return tx.Commit()
}

func normalizeItemReviewTarget(target string) string {
	switch strings.ToLower(strings.TrimSpace(target)) {
	case ItemReviewTargetAgent:
		return ItemReviewTargetAgent
	case ItemReviewTargetGitHub:
		return ItemReviewTargetGitHub
	case ItemReviewTargetEmail:
		return ItemReviewTargetEmail
	default:
		return ""
	}
}

func (s *Store) migrateItemReviewDispatchSupport() error {
	tableColumns, err := s.tableColumnSet("items")
	if err != nil {
		return err
	}
	changes := []struct {
		column string
		sql    string
	}{{column: "review_target", sql: `ALTER TABLE items ADD COLUMN review_target TEXT CHECK (review_target IN ('agent', 'github', 'email'))`}, {column: "reviewer", sql: `ALTER TABLE items ADD COLUMN reviewer TEXT`}, {column: "reviewed_at", sql: `ALTER TABLE items ADD COLUMN reviewed_at TEXT`}}
	for _, change := range changes {
		if tableColumns["items"][change.column] {
			continue
		}
		if _, err := s.db.Exec(change.sql); err != nil {
			return err
		}
	}
	return nil
}

func normalizeOptionalJSON(metaJSON *string) (any, error) {
	if metaJSON == nil {
		return nil, nil
	}
	clean := strings.TrimSpace(*metaJSON)
	if clean == "" {
		return nil, nil
	}
	if !json.Valid([]byte(clean)) {
		return nil, errors.New("meta_json must be valid JSON")
	}
	return clean, nil
}
