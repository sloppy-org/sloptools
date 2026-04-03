package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"strings"
)

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
	}{
		{column: "review_target", sql: `ALTER TABLE items ADD COLUMN review_target TEXT CHECK (review_target IN ('agent', 'github', 'email'))`},
		{column: "reviewer", sql: `ALTER TABLE items ADD COLUMN reviewer TEXT`},
		{column: "reviewed_at", sql: `ALTER TABLE items ADD COLUMN reviewed_at TEXT`},
	}
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

func (s *Store) migrateActorContactSupport() error {
	columns, err := s.tableColumnNames("actors")
	if err != nil {
		return err
	}
	hasColumn := func(target string) bool {
		for _, column := range columns {
			if column == target {
				return true
			}
		}
		return false
	}
	for _, migration := range []struct {
		column string
		sql    string
	}{
		{column: "email", sql: `ALTER TABLE actors ADD COLUMN email TEXT`},
		{column: "provider", sql: `ALTER TABLE actors ADD COLUMN provider TEXT`},
		{column: "provider_ref", sql: `ALTER TABLE actors ADD COLUMN provider_ref TEXT`},
		{column: "meta_json", sql: `ALTER TABLE actors ADD COLUMN meta_json TEXT`},
	} {
		if hasColumn(migration.column) {
			continue
		}
		if _, err := s.db.Exec(migration.sql); err != nil {
			return err
		}
	}
	if _, err := s.db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_actors_email
		ON actors(lower(email))
		WHERE email IS NOT NULL AND trim(email) <> ''`); err != nil {
		return err
	}
	_, err = s.db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_actors_provider_ref
		ON actors(lower(provider), provider_ref)
		WHERE provider IS NOT NULL AND trim(provider) <> '' AND provider_ref IS NOT NULL AND trim(provider_ref) <> ''`)
	return err
}

func normalizeSphere(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case SphereWork:
		return SphereWork
	case "", SpherePrivate:
		return SpherePrivate
	default:
		return ""
	}
}

func normalizeRequiredSphere(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	return normalizeSphere(raw)
}

func normalizeOptionalSphereFilter(raw string) (string, error) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return "", nil
	}
	sphere := normalizeRequiredSphere(clean)
	if sphere == "" {
		return "", errors.New("sphere must be work or private")
	}
	return sphere, nil
}

func normalizeOptionalString(v *string) any {
	if v == nil {
		return nil
	}
	clean := strings.TrimSpace(*v)
	if clean == "" {
		return nil
	}
	return clean
}

func normalizeOptionalSourceFilter(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func normalizeBatchStatus(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func normalizeBatchConfigJSON(raw string) (string, error) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return "{}", nil
	}
	if !json.Valid([]byte(clean)) {
		return "", errors.New("config_json must be valid JSON")
	}
	return clean, nil
}

func normalizeWorkspaceWatchPollIntervalSeconds(raw int) int {
	if raw <= 0 {
		return 300
	}
	return raw
}

func normalizeArtifactKind(kind ArtifactKind) ArtifactKind {
	return ArtifactKind(strings.TrimSpace(string(kind)))
}

func normalizeItemState(state string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "", ItemStateInbox:
		return ItemStateInbox
	case ItemStateWaiting:
		return ItemStateWaiting
	case ItemStateSomeday:
		return ItemStateSomeday
	case ItemStateDone:
		return ItemStateDone
	default:
		return ""
	}
}

func (s *Store) ActiveSphere() (string, error) {
	value, err := s.AppState("active_sphere")
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SpherePrivate, nil
		}
		return "", err
	}
	if strings.TrimSpace(value) == "" {
		return SpherePrivate, nil
	}
	sphere := normalizeSphere(value)
	if sphere == "" {
		return "", errors.New("active sphere must be work or private")
	}
	return sphere, nil
}

func (s *Store) SetActiveSphere(sphere string) error {
	cleanSphere := normalizeRequiredSphere(sphere)
	if cleanSphere == "" {
		return errors.New("active sphere must be work or private")
	}
	return s.SetAppState("active_sphere", cleanSphere)
}

func validateItemTransition(current, next string) error {
	if next == "" {
		return errors.New("item state is required")
	}
	if current == ItemStateDone && next != ItemStateDone && next != ItemStateInbox {
		return fmt.Errorf("cannot transition item from %s to %s", current, next)
	}
	return nil
}

func (s *Store) migrateItemTableStateSupport() error {
	var schema sql.NullString
	if err := s.db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'items'`).Scan(&schema); err != nil {
		return err
	}
	if strings.Contains(strings.ToLower(schema.String), "'someday'") {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`ALTER TABLE items RENAME TO items_legacy`); err != nil {
		return err
	}
	if _, err := tx.Exec(strings.Replace(itemsTableSchema, "IF NOT EXISTS ", "", 1)); err != nil {
		return err
	}
	if _, err := tx.Exec(`
INSERT INTO items (
	id, title, state, workspace_id, artifact_id, actor_id, visible_after, follow_up_at, source, source_ref, created_at, updated_at
)
SELECT
	id, title, state, workspace_id, artifact_id, actor_id, visible_after, follow_up_at, source, source_ref, created_at, updated_at
FROM items_legacy
`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DROP TABLE items_legacy`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DROP TABLE IF EXISTS context_items`); err != nil {
		return err
	}
	if _, err := tx.Exec(`CREATE TABLE context_items (
  context_id INTEGER NOT NULL REFERENCES contexts(id) ON DELETE CASCADE,
  item_id INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  PRIMARY KEY (context_id, item_id)
)`); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) migrateItemSphereSupport() error {
	return nil
}

func scanWorkspace(
	row interface {
		Scan(dest ...any) error
	},
) (Workspace, error) {
	var out Workspace
	var isActive, isDaily int
	var dailyDate sql.NullString
	err := row.Scan(
		&out.ID,
		&out.Name,
		&out.DirPath,
		&out.Sphere,
		&isActive,
		&isDaily,
		&dailyDate,
		&out.MCPURL,
		&out.CanvasSessionID,
		&out.ChatModel,
		&out.ChatModelReasoningEffort,
		&out.CompanionConfigJSON,
		&out.CreatedAt,
		&out.UpdatedAt,
	)
	if err != nil {
		return Workspace{}, err
	}
	out.Name = normalizeWorkspaceName(out.Name)
	out.DirPath = normalizeWorkspacePath(out.DirPath)
	out.Sphere = normalizeSphere(out.Sphere)
	out.MCPURL = strings.TrimSpace(out.MCPURL)
	out.CanvasSessionID = strings.TrimSpace(out.CanvasSessionID)
	out.ChatModel = normalizeWorkspaceChatModel(out.ChatModel)
	out.ChatModelReasoningEffort = normalizeWorkspaceChatModelReasoningEffort(out.ChatModelReasoningEffort)
	out.CompanionConfigJSON = strings.TrimSpace(out.CompanionConfigJSON)
	out.IsActive = isActive != 0
	out.IsDaily = isDaily != 0
	out.DailyDate = nullStringPointer(dailyDate)
	return out, nil
}

func scanActor(
	row interface {
		Scan(dest ...any) error
	},
) (Actor, error) {
	var out Actor
	var email, provider, providerRef, metaJSON sql.NullString
	err := row.Scan(&out.ID, &out.Name, &out.Kind, &email, &provider, &providerRef, &metaJSON, &out.CreatedAt)
	if err != nil {
		return Actor{}, err
	}
	out.Name = normalizeActorName(out.Name)
	out.Kind = normalizeActorKind(out.Kind)
	out.Email = nullStringPointer(email)
	if out.Email != nil {
		clean := normalizeActorEmail(*out.Email)
		out.Email = &clean
	}
	out.Provider = nullStringPointer(provider)
	if out.Provider != nil {
		clean := normalizeActorProvider(*out.Provider)
		if clean == "" {
			out.Provider = nil
		} else {
			out.Provider = &clean
		}
	}
	out.ProviderRef = nullStringPointer(providerRef)
	out.MetaJSON = nullStringPointer(metaJSON)
	return out, nil
}

func scanArtifact(
	row interface {
		Scan(dest ...any) error
	},
) (Artifact, error) {
	var (
		out                              Artifact
		refPath, refURL, title, metaJSON sql.NullString
	)
	err := row.Scan(&out.ID, &out.Kind, &refPath, &refURL, &title, &metaJSON, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		return Artifact{}, err
	}
	out.Kind = normalizeArtifactKind(out.Kind)
	out.RefPath = nullStringPointer(refPath)
	out.RefURL = nullStringPointer(refURL)
	out.Title = nullStringPointer(title)
	out.MetaJSON = nullStringPointer(metaJSON)
	return out, nil
}

func scanItem(
	row interface {
		Scan(dest ...any) error
	},
) (Item, error) {
	var (
		out                                Item
		workspaceID, artifactID, actorID   sql.NullInt64
		visibleAfter, followUpAt           sql.NullString
		sphere                             string
		source, sourceRef                  sql.NullString
		reviewTarget, reviewer, reviewedAt sql.NullString
	)
	err := row.Scan(
		&out.ID,
		&out.Title,
		&out.State,
		&workspaceID,
		&sphere,
		&artifactID,
		&actorID,
		&visibleAfter,
		&followUpAt,
		&source,
		&sourceRef,
		&reviewTarget,
		&reviewer,
		&reviewedAt,
		&out.CreatedAt,
		&out.UpdatedAt,
	)
	if err != nil {
		return Item{}, err
	}
	out.Title = strings.TrimSpace(out.Title)
	out.State = normalizeItemState(out.State)
	out.WorkspaceID = nullInt64Pointer(workspaceID)
	out.Sphere = normalizeSphere(sphere)
	out.ArtifactID = nullInt64Pointer(artifactID)
	out.ActorID = nullInt64Pointer(actorID)
	out.VisibleAfter = nullStringPointer(visibleAfter)
	out.FollowUpAt = nullStringPointer(followUpAt)
	out.Source = nullStringPointer(source)
	out.SourceRef = nullStringPointer(sourceRef)
	out.ReviewTarget = nullStringPointer(reviewTarget)
	if out.ReviewTarget != nil {
		*out.ReviewTarget = normalizeItemReviewTarget(*out.ReviewTarget)
		if *out.ReviewTarget == "" {
			out.ReviewTarget = nil
		}
	}
	out.Reviewer = nullStringPointer(reviewer)
	out.ReviewedAt = nullStringPointer(reviewedAt)
	return out, nil
}

func scanItemSummary(
	row interface {
		Scan(dest ...any) error
	},
) (ItemSummary, error) {
	var (
		out                                    ItemSummary
		workspaceID, artifactID, actorID       sql.NullInt64
		visibleAfter, followUpAt               sql.NullString
		sphere                                 string
		source, sourceRef                      sql.NullString
		reviewTarget, reviewer, reviewedAt     sql.NullString
		artifactTitle, artifactKind, actorName sql.NullString
	)
	err := row.Scan(
		&out.ID,
		&out.Title,
		&out.State,
		&workspaceID,
		&sphere,
		&artifactID,
		&actorID,
		&visibleAfter,
		&followUpAt,
		&source,
		&sourceRef,
		&reviewTarget,
		&reviewer,
		&reviewedAt,
		&out.CreatedAt,
		&out.UpdatedAt,
		&artifactTitle,
		&artifactKind,
		&actorName,
	)
	if err != nil {
		return ItemSummary{}, err
	}
	out.Title = strings.TrimSpace(out.Title)
	out.State = normalizeItemState(out.State)
	out.WorkspaceID = nullInt64Pointer(workspaceID)
	out.Sphere = normalizeSphere(sphere)
	out.ArtifactID = nullInt64Pointer(artifactID)
	out.ActorID = nullInt64Pointer(actorID)
	out.VisibleAfter = nullStringPointer(visibleAfter)
	out.FollowUpAt = nullStringPointer(followUpAt)
	out.Source = nullStringPointer(source)
	out.SourceRef = nullStringPointer(sourceRef)
	out.ReviewTarget = nullStringPointer(reviewTarget)
	if out.ReviewTarget != nil {
		*out.ReviewTarget = normalizeItemReviewTarget(*out.ReviewTarget)
		if *out.ReviewTarget == "" {
			out.ReviewTarget = nil
		}
	}
	out.Reviewer = nullStringPointer(reviewer)
	out.ReviewedAt = nullStringPointer(reviewedAt)
	out.ArtifactTitle = nullStringPointer(artifactTitle)
	if artifactKind.Valid {
		normalized := normalizeArtifactKind(ArtifactKind(artifactKind.String))
		out.ArtifactKind = &normalized
	}
	out.ActorName = nullStringPointer(actorName)
	return out, nil
}

func scanBatchRun(
	row interface {
		Scan(dest ...any) error
	},
) (BatchRun, error) {
	var (
		out        BatchRun
		finishedAt sql.NullString
	)
	err := row.Scan(&out.ID, &out.WorkspaceID, &out.StartedAt, &finishedAt, &out.ConfigJSON, &out.Status)
	if err != nil {
		return BatchRun{}, err
	}
	out.FinishedAt = nullStringPointer(finishedAt)
	out.ConfigJSON = strings.TrimSpace(out.ConfigJSON)
	out.Status = normalizeBatchStatus(out.Status)
	return out, nil
}

func scanBatchRunItem(
	row interface {
		Scan(dest ...any) error
	},
) (BatchRunItem, error) {
	var (
		out                        BatchRunItem
		itemTitle, prURL, errorMsg sql.NullString
		prNumber                   sql.NullInt64
		startedAt, finishedAt      sql.NullString
	)
	err := row.Scan(&out.BatchID, &out.ItemID, &itemTitle, &out.Status, &prNumber, &prURL, &errorMsg, &startedAt, &finishedAt)
	if err != nil {
		return BatchRunItem{}, err
	}
	out.ItemTitle = nullStringPointer(itemTitle)
	out.Status = normalizeBatchStatus(out.Status)
	out.PRNumber = nullInt64Pointer(prNumber)
	out.PRURL = nullStringPointer(prURL)
	out.ErrorMsg = nullStringPointer(errorMsg)
	out.StartedAt = nullStringPointer(startedAt)
	out.FinishedAt = nullStringPointer(finishedAt)
	return out, nil
}

func scanWorkspaceWatch(
	row interface {
		Scan(dest ...any) error
	},
) (WorkspaceWatch, error) {
	var (
		out            WorkspaceWatch
		enabled        int
		currentBatchID sql.NullInt64
	)
	err := row.Scan(
		&out.WorkspaceID,
		&out.ConfigJSON,
		&out.PollIntervalSeconds,
		&enabled,
		&currentBatchID,
		&out.CreatedAt,
		&out.UpdatedAt,
	)
	if err != nil {
		return WorkspaceWatch{}, err
	}
	out.ConfigJSON = strings.TrimSpace(out.ConfigJSON)
	if out.ConfigJSON == "" {
		out.ConfigJSON = "{}"
	}
	out.PollIntervalSeconds = normalizeWorkspaceWatchPollIntervalSeconds(out.PollIntervalSeconds)
	out.Enabled = enabled != 0
	out.CurrentBatchID = nullInt64Pointer(currentBatchID)
	return out, nil
}

func nullStringPointer(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	value := strings.TrimSpace(v.String)
	return &value
}

func nullInt64Pointer(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	value := v.Int64
	return &value
}

func (s *Store) workspaceSphere(id int64) (string, error) {
	workspace, err := s.GetWorkspace(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", errors.New("foreign key constraint failed: workspace_id")
		}
		return "", err
	}
	return workspace.Sphere, nil
}

func (s *Store) resolveItemSphere(workspaceID *int64, requested *string) (string, error) {
	if workspaceID != nil && *workspaceID > 0 {
		return s.workspaceSphere(*workspaceID)
	}
	if requested != nil {
		sphere := normalizeRequiredSphere(*requested)
		if sphere == "" {
			return "", errors.New("item sphere must be work or private")
		}
		return sphere, nil
	}
	return s.ActiveSphere()
}

func normalizeGitHubOwnerRepo(raw string) string {
	clean := strings.TrimSpace(strings.ToLower(raw))
	if clean == "" {
		return ""
	}
	clean = strings.TrimSuffix(clean, ".git")
	if idx := strings.Index(clean, "#"); idx >= 0 {
		clean = clean[:idx]
	}
	clean = strings.Trim(clean, "/")
	switch {
	case strings.HasPrefix(clean, "git@github.com:"):
		clean = strings.TrimPrefix(clean, "git@github.com:")
	case strings.HasPrefix(clean, "ssh://git@github.com/"):
		clean = strings.TrimPrefix(clean, "ssh://git@github.com/")
	case strings.HasPrefix(clean, "https://github.com/"):
		clean = strings.TrimPrefix(clean, "https://github.com/")
	case strings.HasPrefix(clean, "http://github.com/"):
		clean = strings.TrimPrefix(clean, "http://github.com/")
	}
	parts := strings.Split(clean, "/")
	if len(parts) < 2 {
		return ""
	}
	return parts[0] + "/" + parts[1]
}

func githubOwnerRepoFromURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	if !strings.EqualFold(parsed.Host, "github.com") {
		return ""
	}
	return normalizeGitHubOwnerRepo(parsed.Path)
}

func githubOwnerRepoFromMeta(metaJSON string) string {
	var payload map[string]any
	if err := json.Unmarshal([]byte(metaJSON), &payload); err != nil {
		return ""
	}
	for _, key := range []string{"owner_repo", "repo", "source_ref", "url", "html_url"} {
		value, _ := payload[key].(string)
		if repo := normalizeGitHubOwnerRepo(value); repo != "" {
			return repo
		}
		if repo := githubOwnerRepoFromURL(value); repo != "" {
			return repo
		}
	}
	return ""
}

func workspaceGitRemoteOwnerRepo(dirPath string) (string, error) {
	cmd := exec.Command("git", "-C", dirPath, "remote", "get-url", "origin")
	output, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", nil
		}
		return "", err
	}
	return normalizeGitHubOwnerRepo(string(output)), nil
}
