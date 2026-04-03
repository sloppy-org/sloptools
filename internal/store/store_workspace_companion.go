package store

import (
	"errors"
	"strings"
)

func normalizeWorkspaceCompanionConfigJSON(configJSON string) string {
	clean := strings.TrimSpace(configJSON)
	if clean == "" {
		return "{}"
	}
	return clean
}

func (s *Store) UpdateWorkspaceCompanionConfig(id int64, configJSON string) error {
	if id <= 0 {
		return errors.New("workspace id is required")
	}
	_, err := s.db.Exec(
		`UPDATE workspaces SET companion_config_json = ?, updated_at = datetime('now') WHERE id = ?`,
		normalizeWorkspaceCompanionConfigJSON(configJSON),
		id,
	)
	return err
}
