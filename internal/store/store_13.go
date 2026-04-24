package store

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

func (s *Store) ListBindingsMissingContainerRef(accountID int64, provider, objectType string, limit int) ([]ExternalBinding, error) {
	if _, err := s.validateExternalBindingAccount(accountID, provider); err != nil {
		return nil, err
	}
	cleanObjectType := normalizeExternalBindingObjectType(objectType)
	if cleanObjectType == "" {
		return nil, errors.New("external binding object_type is required")
	}
	query := `SELECT id, account_id, provider, object_type, remote_id, item_id, artifact_id, container_ref, remote_updated_at, last_synced_at
		 FROM external_bindings
		 WHERE account_id = ? AND provider = ? AND object_type = ? AND (container_ref IS NULL OR trim(container_ref) = '')
		 ORDER BY datetime(last_synced_at) ASC, id ASC`
	args := []any{accountID, normalizeExternalAccountProvider(provider), cleanObjectType}
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanExternalBindingRows(rows)
}

func (s *Store) ListStaleBindings(provider string, olderThan time.Time) ([]ExternalBinding, error) {
	cleanProvider := normalizeExternalAccountProvider(provider)
	if cleanProvider == "" {
		return nil, errors.New("external binding provider is required")
	}
	rows, err := s.db.Query(`SELECT id, account_id, provider, object_type, remote_id, item_id, artifact_id, container_ref, remote_updated_at, last_synced_at
		 FROM external_bindings
		 WHERE provider = ? AND datetime(last_synced_at) < datetime(?)
		 ORDER BY datetime(last_synced_at) ASC, id ASC`, cleanProvider, olderThan.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanExternalBindingRows(rows)
}

func (s *Store) DeleteBinding(id int64) error {
	res, err := s.db.Exec(`DELETE FROM external_bindings WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func scanExternalBindingRows(rows *sql.Rows) ([]ExternalBinding, error) {
	var out []ExternalBinding
	for rows.Next() {
		binding, err := scanExternalBinding(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, binding)
	}
	return out, rows.Err()
}

func valueOrZero(id *int64) int64 {
	if id == nil {
		return 0
	}
	return *id
}

type ExternalBindingReconcileUpdate struct {
	ObjectType        string
	OldRemoteID       string
	NewRemoteID       string
	ContainerRef      *string
	FollowUpItemState *string
}

func (s *Store) ApplyExternalBindingReconcileUpdates(accountID int64, provider string, updates []ExternalBindingReconcileUpdate) error {
	if _, err := s.validateExternalBindingAccount(accountID, provider); err != nil {
		return err
	}
	cleanProvider := normalizeExternalAccountProvider(provider)
	if len(updates) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, update := range updates {
		objectType := normalizeExternalBindingObjectType(update.ObjectType)
		if objectType == "" {
			return errors.New("external binding reconcile update object_type is required")
		}
		oldRemoteID := normalizeExternalBindingRemoteID(update.OldRemoteID)
		newRemoteID := normalizeExternalBindingRemoteID(update.NewRemoteID)
		if oldRemoteID == "" && newRemoteID == "" {
			continue
		}
		lookupRemoteID := oldRemoteID
		if lookupRemoteID == "" {
			lookupRemoteID = newRemoteID
		}
		var (
			bindingID       int64
			itemID          sql.NullInt64
			artifactID      sql.NullInt64
			currentID       string
			container       sql.NullString
			ignoredRemoteAt sql.NullString
			ignoredSyncedAt string
		)
		err := tx.QueryRow(`SELECT id, item_id, artifact_id, remote_id, container_ref, remote_updated_at, last_synced_at
			 FROM external_bindings
			 WHERE account_id = ? AND provider = ? AND object_type = ? AND remote_id = ?`, accountID, cleanProvider, objectType, lookupRemoteID).Scan(&bindingID, &itemID, &artifactID, &currentID, &container, &ignoredRemoteAt, &ignoredSyncedAt)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		targetRemoteID := currentID
		if newRemoteID != "" {
			targetRemoteID = newRemoteID
		}
		targetContainer := normalizeOptionalString(update.ContainerRef)
		if targetContainer == nil {
			targetContainer = nullStringPointer(container)
		}
		if targetRemoteID != currentID {
			var existingID sql.NullInt64
			err = tx.QueryRow(`SELECT id
				 FROM external_bindings
				 WHERE account_id = ? AND provider = ? AND object_type = ? AND remote_id = ?`, accountID, cleanProvider, objectType, targetRemoteID).Scan(&existingID)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if existingID.Valid && existingID.Int64 != bindingID {
				if _, err := tx.Exec(`UPDATE external_bindings
					 SET item_id = COALESCE(item_id, ?),
					     artifact_id = COALESCE(artifact_id, ?),
					     container_ref = ?,
					     last_synced_at = ?
					 WHERE id = ?`, nullablePositiveID(itemID.Int64), nullablePositiveID(artifactID.Int64), targetContainer, now, existingID.Int64); err != nil {
					return err
				}
				if _, err := tx.Exec(`DELETE FROM external_bindings WHERE id = ?`, bindingID); err != nil {
					return err
				}
				bindingID = existingID.Int64
			} else {
				if _, err := tx.Exec(`UPDATE external_bindings
					 SET remote_id = ?, container_ref = ?, last_synced_at = ?
					 WHERE id = ?`, targetRemoteID, targetContainer, now, bindingID); err != nil {
					return err
				}
			}
		} else {
			if _, err := tx.Exec(`UPDATE external_bindings
				 SET container_ref = ?, last_synced_at = ?
				 WHERE id = ?`, targetContainer, now, bindingID); err != nil {
				return err
			}
		}
		if itemID.Valid && update.FollowUpItemState != nil {
			state := strings.TrimSpace(*update.FollowUpItemState)
			if state != "" {
				if _, err := tx.Exec(`UPDATE items SET state = ? WHERE id = ?`, state, itemID.Int64); err != nil {
					return err
				}
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	tx = nil
	return nil
}

func normalizeExternalContainerType(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "project":
		return "project"
	case "collection":
		return "collection"
	case "notebook":
		return "notebook"
	case "tag":
		return "tag"
	case "label":
		return "label"
	case "calendar":
		return "calendar"
	case "folder":
		return "folder"
	default:
		return ""
	}
}

func normalizeExternalContainerRef(raw string) string {
	return strings.TrimSpace(raw)
}

func scanExternalContainerMapping(row interface{ Scan(dest ...any) error }) (ExternalContainerMapping, error) {
	var (
		out         ExternalContainerMapping
		workspaceID sql.NullInt64
		sphere      sql.NullString
	)
	if err := row.Scan(&out.ID, &out.Provider, &out.ContainerType, &out.ContainerRef, &workspaceID, &sphere); err != nil {
		return ExternalContainerMapping{}, err
	}
	out.Provider = normalizeExternalAccountProvider(out.Provider)
	out.ContainerType = normalizeExternalContainerType(out.ContainerType)
	out.ContainerRef = normalizeExternalContainerRef(out.ContainerRef)
	out.WorkspaceID = nullInt64Pointer(workspaceID)
	if sphere.Valid {
		clean := normalizeExternalAccountSphere(sphere.String)
		if clean != "" {
			out.Sphere = &clean
		}
	}
	return out, nil
}

func (s *Store) GetContainerMapping(provider, containerType, containerRef string) (ExternalContainerMapping, error) {
	cleanProvider := normalizeExternalAccountProvider(provider)
	if cleanProvider == "" {
		return ExternalContainerMapping{}, errors.New("external container mapping provider is required")
	}
	cleanType := normalizeExternalContainerType(containerType)
	if cleanType == "" {
		return ExternalContainerMapping{}, errors.New("external container mapping container_type is required")
	}
	cleanRef := normalizeExternalContainerRef(containerRef)
	if cleanRef == "" {
		return ExternalContainerMapping{}, errors.New("external container mapping container_ref is required")
	}
	return scanExternalContainerMapping(s.db.QueryRow(`SELECT id, provider, container_type, container_ref, workspace_id, `+scopedContextSelect("context_external_container_mappings", "mapping_id", "external_container_mappings.id")+` AS sphere
		 FROM external_container_mappings
		 WHERE lower(provider) = lower(?) AND lower(container_type) = lower(?) AND lower(container_ref) = lower(?)`, cleanProvider, cleanType, cleanRef))
}

func (s *Store) SetContainerMapping(provider, containerType, containerRef string, workspaceID *int64, sphere *string) (ExternalContainerMapping, error) {
	cleanProvider := normalizeExternalAccountProvider(provider)
	if cleanProvider == "" {
		return ExternalContainerMapping{}, errors.New("external container mapping provider is required")
	}
	cleanType := normalizeExternalContainerType(containerType)
	if cleanType == "" {
		return ExternalContainerMapping{}, errors.New("external container mapping container_type is required")
	}
	cleanRef := normalizeExternalContainerRef(containerRef)
	if cleanRef == "" {
		return ExternalContainerMapping{}, errors.New("external container mapping container_ref is required")
	}
	var normalizedSphere *string
	if sphere != nil {
		cleanSphere := normalizeExternalAccountSphere(*sphere)
		if cleanSphere == "" {
			return ExternalContainerMapping{}, errors.New("external container mapping sphere must be work or private")
		}
		normalizedSphere = &cleanSphere
	}
	if workspaceID == nil && normalizedSphere == nil {
		return ExternalContainerMapping{}, errors.New("external container mapping requires workspace_id or sphere")
	}
	if workspaceID != nil {
		if *workspaceID <= 0 {
			return ExternalContainerMapping{}, errors.New("external container mapping workspace_id is required")
		}
		if _, err := s.GetWorkspace(*workspaceID); err != nil {
			return ExternalContainerMapping{}, err
		}
	}
	if _, err := s.db.Exec(`INSERT INTO external_container_mappings (
			provider, container_type, container_ref, workspace_id
		) VALUES (?, ?, ?, ?)
		ON CONFLICT DO UPDATE SET
			workspace_id = excluded.workspace_id`, cleanProvider, cleanType, cleanRef, nullablePositiveID(valueOrZero(workspaceID))); err != nil {
		return ExternalContainerMapping{}, err
	}
	if normalizedSphere != nil {
		mapping, err := s.GetContainerMapping(cleanProvider, cleanType, cleanRef)
		if err != nil {
			return ExternalContainerMapping{}, err
		}
		if err := s.syncScopedContextLink("context_external_container_mappings", "mapping_id", mapping.ID, *normalizedSphere); err != nil {
			return ExternalContainerMapping{}, err
		}
	}
	return s.GetContainerMapping(cleanProvider, cleanType, cleanRef)
}

func (s *Store) ListContainerMappings(provider string) ([]ExternalContainerMapping, error) {
	cleanProvider := strings.TrimSpace(provider)
	query := `SELECT id, provider, container_type, container_ref, workspace_id, ` + scopedContextSelect("context_external_container_mappings", "mapping_id", "external_container_mappings.id") + ` AS sphere
		FROM external_container_mappings`
	args := []any{}
	if cleanProvider != "" {
		normalizedProvider := normalizeExternalAccountProvider(cleanProvider)
		if normalizedProvider == "" {
			return nil, errors.New("external container mapping provider is required")
		}
		query += ` WHERE lower(provider) = lower(?)`
		args = append(args, normalizedProvider)
	}
	query += ` ORDER BY lower(provider), lower(container_type), lower(container_ref), id`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ExternalContainerMapping
	for rows.Next() {
		mapping, err := scanExternalContainerMapping(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, mapping)
	}
	return out, rows.Err()
}

func (s *Store) DeleteContainerMapping(id int64) error {
	res, err := s.db.Exec(`DELETE FROM external_container_mappings WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func stringsJoin(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	out := parts[0]
	for i := 1; i < len(parts); i++ {
		out += sep + parts[i]
	}
	return out
}

func (s *Store) AddHost(h HostConfig) (HostConfig, error) {
	if h.Name == "" || h.Hostname == "" || h.Username == "" {
		return HostConfig{}, errors.New("name, hostname, username required")
	}
	if h.Port <= 0 {
		h.Port = 22
	}
	res, err := s.db.Exec(`INSERT INTO hosts (name,hostname,port,username,key_path,project_dir) VALUES (?,?,?,?,?,?)`, h.Name, h.Hostname, h.Port, h.Username, h.KeyPath, h.ProjectDir)
	if err != nil {
		return HostConfig{}, err
	}
	id, _ := res.LastInsertId()
	return s.GetHost(int(id))
}

func (s *Store) GetHost(id int) (HostConfig, error) {
	var h HostConfig
	err := s.db.QueryRow(`SELECT id,name,hostname,port,username,key_path,project_dir FROM hosts WHERE id=?`, id).Scan(&h.ID, &h.Name, &h.Hostname, &h.Port, &h.Username, &h.KeyPath, &h.ProjectDir)
	if err != nil {
		return HostConfig{}, err
	}
	return h, nil
}

func (s *Store) ListHosts() ([]HostConfig, error) {
	rows, err := s.db.Query(`SELECT id,name,hostname,port,username,key_path,project_dir FROM hosts`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []HostConfig{}
	for rows.Next() {
		var h HostConfig
		if err := rows.Scan(&h.ID, &h.Name, &h.Hostname, &h.Port, &h.Username, &h.KeyPath, &h.ProjectDir); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func (s *Store) UpdateHost(id int, updates map[string]interface{}) (HostConfig, error) {
	if len(updates) == 0 {
		return s.GetHost(id)
	}
	parts := []string{}
	args := []interface{}{}
	for _, key := range []string{"name", "hostname", "port", "username", "key_path", "project_dir"} {
		if v, ok := updates[key]; ok {
			parts = append(parts, fmt.Sprintf("%s=?", key))
			args = append(args, v)
		}
	}
	if len(parts) == 0 {
		return s.GetHost(id)
	}
	args = append(args, id)
	_, err := s.db.Exec(`UPDATE hosts SET `+stringsJoin(parts, ",")+` WHERE id=?`, args...)
	if err != nil {
		return HostConfig{}, err
	}
	return s.GetHost(id)
}

func (s *Store) DeleteHost(id int) error {
	_, err := s.db.Exec(`DELETE FROM hosts WHERE id=?`, id)
	return err
}

func (s *Store) AddRemoteSession(sessionID string, hostID int) error {
	_, err := s.db.Exec(`INSERT OR REPLACE INTO remote_sessions (session_id,host_id,created_at) VALUES (?,?,?)`, sessionID, hostID, time.Now().Unix())
	return err
}

func (s *Store) DeleteRemoteSession(sessionID string) error {
	_, err := s.db.Exec(`DELETE FROM remote_sessions WHERE session_id=?`, sessionID)
	return err
}

func (s *Store) ListRemoteSessions() ([][2]interface{}, error) {
	rows, err := s.db.Query(`SELECT session_id,host_id FROM remote_sessions ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := [][2]interface{}{}
	for rows.Next() {
		var sid string
		var hid int
		if err := rows.Scan(&sid, &hid); err != nil {
			return nil, err
		}
		out = append(out, [2]interface{}{sid, hid})
	}
	return out, nil
}

const itemArtifactsTableSchema = `CREATE TABLE IF NOT EXISTS item_artifacts (
  item_id INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  artifact_id INTEGER NOT NULL REFERENCES artifacts(id) ON DELETE CASCADE,
  role TEXT NOT NULL DEFAULT 'related' CHECK (role IN ('source', 'related', 'output')),
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  PRIMARY KEY (item_id, artifact_id)
);
CREATE INDEX IF NOT EXISTS idx_item_artifacts_artifact_id
  ON item_artifacts(artifact_id);`

func normalizeItemArtifactRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "", "related":
		return "related"
	case "source":
		return "source"
	case "output":
		return "output"
	default:
		return ""
	}
}
