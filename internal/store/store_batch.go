package store

import (
	"database/sql"
	"errors"
	"time"
)

func (s *Store) CreateBatchRun(workspaceID int64, configJSON, status string) (BatchRun, error) {
	if workspaceID <= 0 {
		return BatchRun{}, errors.New("workspace_id must be positive")
	}
	if _, err := s.GetWorkspace(workspaceID); err != nil {
		return BatchRun{}, err
	}
	cleanConfig, err := normalizeBatchConfigJSON(configJSON)
	if err != nil {
		return BatchRun{}, err
	}
	cleanStatus := normalizeBatchStatus(status)
	if cleanStatus == "" {
		cleanStatus = "running"
	}
	res, err := s.db.Exec(
		`INSERT INTO batch_runs (workspace_id, config_json, status)
		 VALUES (?, ?, ?)`,
		workspaceID,
		cleanConfig,
		cleanStatus,
	)
	if err != nil {
		return BatchRun{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return BatchRun{}, err
	}
	return s.GetBatchRun(id)
}

func (s *Store) GetBatchRun(id int64) (BatchRun, error) {
	if id <= 0 {
		return BatchRun{}, errors.New("batch id is required")
	}
	return scanBatchRun(s.db.QueryRow(
		`SELECT id, workspace_id, started_at, finished_at, config_json, status
		 FROM batch_runs
		 WHERE id = ?`,
		id,
	))
}

func (s *Store) ListBatchRuns(workspaceID *int64) ([]BatchRun, error) {
	query := `SELECT id, workspace_id, started_at, finished_at, config_json, status
		FROM batch_runs`
	args := []any{}
	if workspaceID != nil {
		if *workspaceID <= 0 {
			return nil, errors.New("workspace_id must be positive")
		}
		query += ` WHERE workspace_id = ?`
		args = append(args, *workspaceID)
	}
	query += ` ORDER BY CASE WHEN finished_at IS NULL THEN 0 ELSE 1 END,
	                  datetime(started_at) DESC,
	                  id DESC`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BatchRun{}
	for rows.Next() {
		run, err := scanBatchRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

func (s *Store) SetBatchRunStatus(id int64, status string, finishedAt *string) (BatchRun, error) {
	cleanStatus := normalizeBatchStatus(status)
	if cleanStatus == "" {
		return BatchRun{}, errors.New("batch status is required")
	}
	finishedValue := normalizeOptionalString(finishedAt)
	res, err := s.db.Exec(
		`UPDATE batch_runs
		 SET status = ?, finished_at = ?
		 WHERE id = ?`,
		cleanStatus,
		finishedValue,
		id,
	)
	if err != nil {
		return BatchRun{}, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return BatchRun{}, err
	}
	if affected == 0 {
		return BatchRun{}, sql.ErrNoRows
	}
	return s.GetBatchRun(id)
}

func (s *Store) UpsertBatchRunItem(batchID, itemID int64, update BatchRunItemUpdate) (BatchRunItem, error) {
	cleanStatus := normalizeBatchStatus(update.Status)
	if cleanStatus == "" {
		return BatchRunItem{}, errors.New("batch item status is required")
	}
	if _, err := s.GetBatchRun(batchID); err != nil {
		return BatchRunItem{}, err
	}
	if _, err := s.GetItem(itemID); err != nil {
		return BatchRunItem{}, err
	}
	startedAt := normalizeOptionalString(update.StartedAt)
	if startedAt == nil && (cleanStatus == "running" || cleanStatus == "done" || cleanStatus == "failed" || cleanStatus == "completed") {
		now := time.Now().UTC().Format(time.RFC3339)
		startedAt = now
	}
	finishedAt := normalizeOptionalString(update.FinishedAt)
	if finishedAt == nil && (cleanStatus == "done" || cleanStatus == "failed" || cleanStatus == "completed" || cleanStatus == "canceled") {
		now := time.Now().UTC().Format(time.RFC3339)
		finishedAt = now
	}
	_, err := s.db.Exec(
		`INSERT INTO batch_run_items (
		   batch_id, item_id, status, pr_number, pr_url, error_msg, started_at, finished_at
		 )
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(batch_id, item_id) DO UPDATE SET
		   status = excluded.status,
		   pr_number = excluded.pr_number,
		   pr_url = excluded.pr_url,
		   error_msg = excluded.error_msg,
		   started_at = COALESCE(batch_run_items.started_at, excluded.started_at),
		   finished_at = excluded.finished_at`,
		batchID,
		itemID,
		cleanStatus,
		normalizeOptionalInt64(update.PRNumber),
		normalizeOptionalString(update.PRURL),
		normalizeOptionalString(update.ErrorMsg),
		startedAt,
		finishedAt,
	)
	if err != nil {
		return BatchRunItem{}, err
	}
	return s.GetBatchRunItem(batchID, itemID)
}

func (s *Store) GetBatchRunItem(batchID, itemID int64) (BatchRunItem, error) {
	return scanBatchRunItem(s.db.QueryRow(
		`SELECT bri.batch_id,
		        bri.item_id,
		        i.title,
		        bri.status,
		        bri.pr_number,
		        bri.pr_url,
		        bri.error_msg,
		        bri.started_at,
		        bri.finished_at
		 FROM batch_run_items bri
		 INNER JOIN items i ON i.id = bri.item_id
		 WHERE bri.batch_id = ? AND bri.item_id = ?`,
		batchID,
		itemID,
	))
}

func (s *Store) ListBatchRunItems(batchID int64) ([]BatchRunItem, error) {
	if _, err := s.GetBatchRun(batchID); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(
		`SELECT bri.batch_id,
		        bri.item_id,
		        i.title,
		        bri.status,
		        bri.pr_number,
		        bri.pr_url,
		        bri.error_msg,
		        bri.started_at,
		        bri.finished_at
		 FROM batch_run_items bri
		 INNER JOIN items i ON i.id = bri.item_id
		 WHERE bri.batch_id = ?
		 ORDER BY CASE bri.status WHEN 'running' THEN 0 WHEN 'pending' THEN 1 WHEN 'done' THEN 2 WHEN 'completed' THEN 3 WHEN 'failed' THEN 4 ELSE 5 END,
		          bri.item_id ASC`,
		batchID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BatchRunItem{}
	for rows.Next() {
		entry, err := scanBatchRunItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, entry)
	}
	return out, rows.Err()
}

func normalizeOptionalInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}
