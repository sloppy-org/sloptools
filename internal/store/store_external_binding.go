package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

func normalizeExternalBindingObjectType(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func normalizeExternalBindingRemoteID(raw string) string {
	return strings.TrimSpace(raw)
}

func scanExternalBinding(
	row interface {
		Scan(dest ...any) error
	},
) (ExternalBinding, error) {
	var (
		out                           ExternalBinding
		itemID, artifactID            sql.NullInt64
		containerRef, remoteUpdatedAt sql.NullString
	)
	if err := row.Scan(
		&out.ID,
		&out.AccountID,
		&out.Provider,
		&out.ObjectType,
		&out.RemoteID,
		&itemID,
		&artifactID,
		&containerRef,
		&remoteUpdatedAt,
		&out.LastSyncedAt,
	); err != nil {
		return ExternalBinding{}, err
	}
	out.Provider = normalizeExternalAccountProvider(out.Provider)
	out.ObjectType = normalizeExternalBindingObjectType(out.ObjectType)
	out.RemoteID = normalizeExternalBindingRemoteID(out.RemoteID)
	out.ItemID = nullInt64Pointer(itemID)
	out.ArtifactID = nullInt64Pointer(artifactID)
	out.ContainerRef = nullStringPointer(containerRef)
	out.RemoteUpdatedAt = nullStringPointer(remoteUpdatedAt)
	out.LastSyncedAt = strings.TrimSpace(out.LastSyncedAt)
	return out, nil
}

func (s *Store) validateExternalBindingAccount(accountID int64, provider string) (ExternalAccount, error) {
	if accountID <= 0 {
		return ExternalAccount{}, errors.New("external binding account_id is required")
	}
	cleanProvider := normalizeExternalAccountProvider(provider)
	if cleanProvider == "" {
		return ExternalAccount{}, errors.New("external binding provider is required")
	}
	account, err := s.GetExternalAccount(accountID)
	if err != nil {
		return ExternalAccount{}, err
	}
	if account.Provider != cleanProvider {
		return ExternalAccount{}, errors.New("external binding provider must match account provider")
	}
	return account, nil
}

func (s *Store) UpsertExternalBinding(binding ExternalBinding) (ExternalBinding, error) {
	account, err := s.validateExternalBindingAccount(binding.AccountID, binding.Provider)
	if err != nil {
		return ExternalBinding{}, err
	}
	objectType := normalizeExternalBindingObjectType(binding.ObjectType)
	if objectType == "" {
		return ExternalBinding{}, errors.New("external binding object_type is required")
	}
	remoteID := normalizeExternalBindingRemoteID(binding.RemoteID)
	if remoteID == "" {
		return ExternalBinding{}, errors.New("external binding remote_id is required")
	}
	remoteUpdatedAt, err := normalizeOptionalRFC3339String(binding.RemoteUpdatedAt)
	if err != nil {
		return ExternalBinding{}, errors.New("external binding remote_updated_at must be a valid RFC3339 timestamp")
	}
	lastSyncedAt := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := s.db.Exec(
		`INSERT INTO external_bindings (
			account_id, provider, object_type, remote_id, item_id, artifact_id, container_ref, remote_updated_at, last_synced_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(account_id, provider, object_type, remote_id) DO UPDATE SET
			item_id = excluded.item_id,
			artifact_id = excluded.artifact_id,
			container_ref = excluded.container_ref,
			remote_updated_at = excluded.remote_updated_at,
			last_synced_at = excluded.last_synced_at`,
		account.ID,
		account.Provider,
		objectType,
		remoteID,
		nullablePositiveID(valueOrZero(binding.ItemID)),
		nullablePositiveID(valueOrZero(binding.ArtifactID)),
		normalizeOptionalString(binding.ContainerRef),
		remoteUpdatedAt,
		lastSyncedAt,
	); err != nil {
		return ExternalBinding{}, err
	}
	return s.GetBindingByRemote(account.ID, account.Provider, objectType, remoteID)
}

func (s *Store) GetBindingByRemote(accountID int64, provider, objectType, remoteID string) (ExternalBinding, error) {
	if _, err := s.validateExternalBindingAccount(accountID, provider); err != nil {
		return ExternalBinding{}, err
	}
	cleanObjectType := normalizeExternalBindingObjectType(objectType)
	if cleanObjectType == "" {
		return ExternalBinding{}, errors.New("external binding object_type is required")
	}
	cleanRemoteID := normalizeExternalBindingRemoteID(remoteID)
	if cleanRemoteID == "" {
		return ExternalBinding{}, errors.New("external binding remote_id is required")
	}
	return scanExternalBinding(s.db.QueryRow(
		`SELECT id, account_id, provider, object_type, remote_id, item_id, artifact_id, container_ref, remote_updated_at, last_synced_at
		 FROM external_bindings
		 WHERE account_id = ? AND provider = ? AND object_type = ? AND remote_id = ?`,
		accountID,
		normalizeExternalAccountProvider(provider),
		cleanObjectType,
		cleanRemoteID,
	))
}

func (s *Store) GetBindingsByItem(itemID int64) ([]ExternalBinding, error) {
	rows, err := s.db.Query(
		`SELECT id, account_id, provider, object_type, remote_id, item_id, artifact_id, container_ref, remote_updated_at, last_synced_at
		 FROM external_bindings
		 WHERE item_id = ?
		 ORDER BY lower(provider), lower(object_type), remote_id, id`,
		itemID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanExternalBindingRows(rows)
}

func (s *Store) GetBindingsByArtifact(artifactID int64) ([]ExternalBinding, error) {
	rows, err := s.db.Query(
		`SELECT id, account_id, provider, object_type, remote_id, item_id, artifact_id, container_ref, remote_updated_at, last_synced_at
		 FROM external_bindings
		 WHERE artifact_id = ?
		 ORDER BY lower(provider), lower(object_type), remote_id, id`,
		artifactID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanExternalBindingRows(rows)
}

func (s *Store) ListBindingsByAccount(accountID int64, provider, objectType string) ([]ExternalBinding, error) {
	if _, err := s.validateExternalBindingAccount(accountID, provider); err != nil {
		return nil, err
	}
	cleanObjectType := normalizeExternalBindingObjectType(objectType)
	if cleanObjectType == "" {
		return nil, errors.New("external binding object_type is required")
	}
	rows, err := s.db.Query(
		`SELECT id, account_id, provider, object_type, remote_id, item_id, artifact_id, container_ref, remote_updated_at, last_synced_at
		 FROM external_bindings
		 WHERE account_id = ? AND provider = ? AND object_type = ?
		 ORDER BY remote_id, id`,
		accountID,
		normalizeExternalAccountProvider(provider),
		cleanObjectType,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanExternalBindingRows(rows)
}

func (s *Store) LatestBindingRemoteUpdatedAt(accountID int64, provider, objectType string) (*string, error) {
	if _, err := s.validateExternalBindingAccount(accountID, provider); err != nil {
		return nil, err
	}
	cleanObjectType := normalizeExternalBindingObjectType(objectType)
	if cleanObjectType == "" {
		return nil, errors.New("external binding object_type is required")
	}
	var value sql.NullString
	err := s.db.QueryRow(
		`SELECT remote_updated_at
		 FROM external_bindings
		 WHERE account_id = ? AND provider = ? AND object_type = ? AND remote_updated_at IS NOT NULL
		 ORDER BY datetime(remote_updated_at) DESC, id DESC
		 LIMIT 1`,
		accountID,
		normalizeExternalAccountProvider(provider),
		cleanObjectType,
	).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !value.Valid {
		return nil, nil
	}
	clean := strings.TrimSpace(value.String)
	if clean == "" {
		return nil, nil
	}
	return &clean, nil
}

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
	args := []any{
		accountID,
		normalizeExternalAccountProvider(provider),
		cleanObjectType,
	}
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
	rows, err := s.db.Query(
		`SELECT id, account_id, provider, object_type, remote_id, item_id, artifact_id, container_ref, remote_updated_at, last_synced_at
		 FROM external_bindings
		 WHERE provider = ? AND datetime(last_synced_at) < datetime(?)
		 ORDER BY datetime(last_synced_at) ASC, id ASC`,
		cleanProvider,
		olderThan.UTC().Format(time.RFC3339Nano),
	)
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
