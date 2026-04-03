package store

import (
	"database/sql"
	"errors"
)

func (s *Store) GetWorkspaceWatch(workspaceID int64) (WorkspaceWatch, error) {
	if workspaceID <= 0 {
		return WorkspaceWatch{}, errors.New("workspace_id must be positive")
	}
	return scanWorkspaceWatch(s.db.QueryRow(
		`SELECT workspace_id, config_json, poll_interval_seconds, enabled, current_batch_id, created_at, updated_at
		 FROM workspace_watches
		 WHERE workspace_id = ?`,
		workspaceID,
	))
}

func (s *Store) ListWorkspaceWatches(enabledOnly bool) ([]WorkspaceWatch, error) {
	query := `SELECT workspace_id, config_json, poll_interval_seconds, enabled, current_batch_id, created_at, updated_at
		FROM workspace_watches`
	args := []any{}
	if enabledOnly {
		query += ` WHERE enabled <> 0`
	}
	query += ` ORDER BY workspace_id ASC`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WorkspaceWatch{}
	for rows.Next() {
		watch, err := scanWorkspaceWatch(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, watch)
	}
	return out, rows.Err()
}

func (s *Store) UpsertWorkspaceWatch(workspaceID int64, configJSON string, pollIntervalSeconds int, enabled bool, currentBatchID *int64) (WorkspaceWatch, error) {
	if workspaceID <= 0 {
		return WorkspaceWatch{}, errors.New("workspace_id must be positive")
	}
	if _, err := s.GetWorkspace(workspaceID); err != nil {
		return WorkspaceWatch{}, err
	}
	cleanConfig, err := normalizeBatchConfigJSON(configJSON)
	if err != nil {
		return WorkspaceWatch{}, err
	}
	cleanInterval := normalizeWorkspaceWatchPollIntervalSeconds(pollIntervalSeconds)
	if currentBatchID != nil && *currentBatchID > 0 {
		if _, err := s.GetBatchRun(*currentBatchID); err != nil {
			return WorkspaceWatch{}, err
		}
	}
	_, err = s.db.Exec(
		`INSERT INTO workspace_watches (workspace_id, config_json, poll_interval_seconds, enabled, current_batch_id)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(workspace_id) DO UPDATE SET
		   config_json = excluded.config_json,
		   poll_interval_seconds = excluded.poll_interval_seconds,
		   enabled = excluded.enabled,
		   current_batch_id = excluded.current_batch_id,
		   updated_at = datetime('now')`,
		workspaceID,
		cleanConfig,
		cleanInterval,
		boolToSQLiteInt(enabled),
		normalizeOptionalInt64(currentBatchID),
	)
	if err != nil {
		return WorkspaceWatch{}, err
	}
	return s.GetWorkspaceWatch(workspaceID)
}

func (s *Store) SetWorkspaceWatchEnabled(workspaceID int64, enabled bool) (WorkspaceWatch, error) {
	res, err := s.db.Exec(
		`UPDATE workspace_watches
		 SET enabled = ?, updated_at = datetime('now')
		 WHERE workspace_id = ?`,
		boolToSQLiteInt(enabled),
		workspaceID,
	)
	if err != nil {
		return WorkspaceWatch{}, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return WorkspaceWatch{}, err
	}
	if affected == 0 {
		return WorkspaceWatch{}, sql.ErrNoRows
	}
	return s.GetWorkspaceWatch(workspaceID)
}

func (s *Store) SetWorkspaceWatchBatch(workspaceID int64, batchID *int64) (WorkspaceWatch, error) {
	if batchID != nil && *batchID > 0 {
		if _, err := s.GetBatchRun(*batchID); err != nil {
			return WorkspaceWatch{}, err
		}
	}
	res, err := s.db.Exec(
		`UPDATE workspace_watches
		 SET current_batch_id = ?, updated_at = datetime('now')
		 WHERE workspace_id = ?`,
		normalizeOptionalInt64(batchID),
		workspaceID,
	)
	if err != nil {
		return WorkspaceWatch{}, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return WorkspaceWatch{}, err
	}
	if affected == 0 {
		return WorkspaceWatch{}, sql.ErrNoRows
	}
	return s.GetWorkspaceWatch(workspaceID)
}

func boolToSQLiteInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
