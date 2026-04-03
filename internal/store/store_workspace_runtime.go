package store

import (
	"database/sql"
	"errors"
)

func (s *Store) UpdateWorkspaceChatModel(id int64, chatModel string) error {
	if id <= 0 {
		return errors.New("workspace id is required")
	}
	res, err := s.db.Exec(
		`UPDATE workspaces SET chat_model = ?, updated_at = datetime('now') WHERE id = ?`,
		normalizeWorkspaceChatModel(chatModel),
		id,
	)
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

func (s *Store) UpdateWorkspaceChatModelReasoningEffort(id int64, effort string) error {
	if id <= 0 {
		return errors.New("workspace id is required")
	}
	res, err := s.db.Exec(
		`UPDATE workspaces SET chat_model_reasoning_effort = ?, updated_at = datetime('now') WHERE id = ?`,
		normalizeWorkspaceChatModelReasoningEffort(effort),
		id,
	)
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
