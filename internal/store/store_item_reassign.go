package store

import (
	"database/sql"
)

func (s *Store) SetItemWorkspace(id int64, workspaceID *int64) error {
	args := []any{nullablePositiveID(valueOrZeroInt64(workspaceID)), id}
	query := `UPDATE items
		 SET workspace_id = ?, updated_at = datetime('now')
		 WHERE id = ?`
	res, err := s.db.Exec(query, args...)
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
	if err := s.syncItemDateContext(id, workspaceID); err != nil {
		return err
	}
	return nil
}

func valueOrZeroInt64(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}
