package store

import (
	"database/sql"
	"errors"
	"strconv"
	"strings"
)

const appStateFocusedWorkspaceIDKey = "focused_workspace_id"

func (s *Store) SetFocusedWorkspaceID(id int64) error {
	if id < 0 {
		return errors.New("focused workspace id must be zero or a positive integer")
	}
	if id == 0 {
		return s.SetAppState(appStateFocusedWorkspaceIDKey, "")
	}
	if _, err := s.GetWorkspace(id); err != nil {
		return err
	}
	return s.SetAppState(appStateFocusedWorkspaceIDKey, strconv.FormatInt(id, 10))
}

func (s *Store) FocusedWorkspaceID() (int64, error) {
	raw, err := s.AppState(appStateFocusedWorkspaceIDKey)
	if err != nil {
		return 0, err
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, errors.New("focused workspace id is invalid")
	}
	if id <= 0 {
		return 0, nil
	}
	if _, err := s.GetWorkspace(id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, err
	}
	return id, nil
}
