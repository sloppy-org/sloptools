package store

import (
	"database/sql"
	"errors"
	"strings"
)

func (s *Store) CreateLabel(name string, parentID *int64) (Label, error) {
	cleanName := strings.TrimSpace(name)
	if cleanName == "" {
		return Label{}, errors.New("label name is required")
	}
	if parentID != nil && *parentID <= 0 {
		return Label{}, errors.New("parent_id must be a positive integer")
	}
	var existingID int64
	err := s.db.QueryRow(
		`SELECT id
		 FROM contexts
		 WHERE lower(name) = lower(?)
		   AND (
		     (parent_id IS NULL AND ? IS NULL)
		     OR parent_id = ?
		   )`,
		cleanName,
		parentID,
		parentID,
	).Scan(&existingID)
	switch {
	case err == nil:
		return s.GetLabel(existingID)
	case !errors.Is(err, sql.ErrNoRows):
		return Label{}, err
	}
	res, err := s.db.Exec(
		`INSERT INTO contexts (name, parent_id) VALUES (?, ?)`,
		cleanName,
		parentID,
	)
	if err != nil {
		return Label{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Label{}, err
	}
	return s.GetLabel(id)
}

func (s *Store) contextIDByName(name string) (int64, error) {
	var contextID int64
	err := s.db.QueryRow(
		`SELECT id
		 FROM contexts
		 WHERE lower(name) = lower(?)`,
		strings.TrimSpace(name),
	).Scan(&contextID)
	return contextID, err
}

func (s *Store) GetLabel(id int64) (Label, error) {
	row := s.db.QueryRow(
		`SELECT id, name, color, parent_id, created_at
		 FROM contexts
		 WHERE id = ?`,
		id,
	)
	var (
		label    Label
		parentID sql.NullInt64
	)
	if err := row.Scan(&label.ID, &label.Name, &label.Color, &parentID, &label.CreatedAt); err != nil {
		return Label{}, err
	}
	label.ParentID = nullInt64Pointer(parentID)
	return label, nil
}

func (s *Store) LinkLabelToWorkspace(labelID, workspaceID int64) error {
	if labelID <= 0 || workspaceID <= 0 {
		return errors.New("label_id and workspace_id must be positive integers")
	}
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO context_workspaces (context_id, workspace_id) VALUES (?, ?)`,
		labelID,
		workspaceID,
	)
	return err
}

func (s *Store) LinkLabelToItem(labelID, itemID int64) error {
	if labelID <= 0 || itemID <= 0 {
		return errors.New("label_id and item_id must be positive integers")
	}
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO context_items (context_id, item_id) VALUES (?, ?)`,
		labelID,
		itemID,
	)
	return err
}

func (s *Store) LinkLabelToArtifact(labelID, artifactID int64) error {
	if labelID <= 0 || artifactID <= 0 {
		return errors.New("label_id and artifact_id must be positive integers")
	}
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO context_artifacts (context_id, artifact_id) VALUES (?, ?)`,
		labelID,
		artifactID,
	)
	return err
}

func (s *Store) ListLabels() ([]Label, error) {
	rows, err := s.db.Query(
		`SELECT id, name, color, parent_id, created_at
		 FROM contexts
		 ORDER BY CASE WHEN parent_id IS NULL THEN 0 ELSE 1 END, lower(name), id`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Label
	for rows.Next() {
		var (
			label    Label
			parentID sql.NullInt64
		)
		if err := rows.Scan(&label.ID, &label.Name, &label.Color, &parentID, &label.CreatedAt); err != nil {
			return nil, err
		}
		label.ParentID = nullInt64Pointer(parentID)
		out = append(out, label)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
