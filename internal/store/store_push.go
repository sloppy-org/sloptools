package store

import (
	"database/sql"
	"errors"
	"strings"
)

type PushRegistration struct {
	ID          int64  `json:"id"`
	SessionID   string `json:"session_id"`
	WorkspaceID int64  `json:"workspace_id"`
	Platform    string `json:"platform"`
	DeviceToken string `json:"device_token"`
	DeviceLabel string `json:"device_label"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

func normalizePushPlatform(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "apns":
		return "apns"
	case "fcm":
		return "fcm"
	default:
		return ""
	}
}

func scanPushRegistration(scanner interface{ Scan(...any) error }) (PushRegistration, error) {
	var item PushRegistration
	if err := scanner.Scan(
		&item.ID,
		&item.SessionID,
		&item.WorkspaceID,
		&item.Platform,
		&item.DeviceToken,
		&item.DeviceLabel,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return PushRegistration{}, err
	}
	item.SessionID = strings.TrimSpace(item.SessionID)
	item.Platform = normalizePushPlatform(item.Platform)
	item.DeviceToken = strings.TrimSpace(item.DeviceToken)
	item.DeviceLabel = strings.TrimSpace(item.DeviceLabel)
	if item.WorkspaceID < 0 {
		item.WorkspaceID = 0
	}
	return item, nil
}

func (s *Store) GetPushRegistration(id int64) (PushRegistration, error) {
	if id <= 0 {
		return PushRegistration{}, errors.New("push registration id is required")
	}
	return scanPushRegistration(s.db.QueryRow(
		`SELECT id, session_id, workspace_id, platform, device_token, device_label, created_at, updated_at
		   FROM push_registrations
		  WHERE id = ?`,
		id,
	))
}

func (s *Store) UpsertPushRegistration(reg PushRegistration) (PushRegistration, error) {
	platform := normalizePushPlatform(reg.Platform)
	if platform == "" {
		return PushRegistration{}, errors.New("platform must be apns or fcm")
	}
	deviceToken := strings.TrimSpace(reg.DeviceToken)
	if deviceToken == "" {
		return PushRegistration{}, errors.New("device_token is required")
	}
	sessionID := strings.TrimSpace(reg.SessionID)
	workspaceID := reg.WorkspaceID
	if workspaceID < 0 {
		return PushRegistration{}, errors.New("workspace_id must not be negative")
	}
	deviceLabel := strings.TrimSpace(reg.DeviceLabel)

	tx, err := s.db.Begin()
	if err != nil {
		return PushRegistration{}, err
	}
	defer tx.Rollback()

	var existingID int64
	err = tx.QueryRow(
		`SELECT id
		   FROM push_registrations
		  WHERE platform = ? AND device_token = ? AND session_id = ? AND workspace_id = ?`,
		platform,
		deviceToken,
		sessionID,
		workspaceID,
	).Scan(&existingID)
	switch {
	case err == nil:
		if _, err := tx.Exec(
			`UPDATE push_registrations
			    SET device_label = ?, updated_at = datetime('now')
			  WHERE id = ?`,
			deviceLabel,
			existingID,
		); err != nil {
			return PushRegistration{}, err
		}
	case errors.Is(err, sql.ErrNoRows):
		res, execErr := tx.Exec(
			`INSERT INTO push_registrations (session_id, workspace_id, platform, device_token, device_label)
			 VALUES (?,?,?,?,?)`,
			sessionID,
			workspaceID,
			platform,
			deviceToken,
			deviceLabel,
		)
		if execErr != nil {
			return PushRegistration{}, execErr
		}
		existingID, err = res.LastInsertId()
		if err != nil {
			return PushRegistration{}, err
		}
	default:
		return PushRegistration{}, err
	}

	if err := tx.Commit(); err != nil {
		return PushRegistration{}, err
	}
	return s.GetPushRegistration(existingID)
}

func (s *Store) ListPushRegistrations() ([]PushRegistration, error) {
	rows, err := s.db.Query(
		`SELECT id, session_id, workspace_id, platform, device_token, device_label, created_at, updated_at
		   FROM push_registrations
		  ORDER BY id ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]PushRegistration, 0, 16)
	for rows.Next() {
		item, err := scanPushRegistration(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) ListPushRegistrationsForChatSession(sessionID string, workspaceID int64) ([]PushRegistration, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" && workspaceID <= 0 {
		return nil, errors.New("session_id or workspace_id is required")
	}
	rows, err := s.db.Query(
		`SELECT id, session_id, workspace_id, platform, device_token, device_label, created_at, updated_at
		   FROM push_registrations
		  WHERE (session_id != '' AND session_id = ?)
		     OR (workspace_id > 0 AND workspace_id = ?)
		  ORDER BY id ASC`,
		sessionID,
		workspaceID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]PushRegistration, 0, 8)
	seen := map[int64]struct{}{}
	for rows.Next() {
		item, err := scanPushRegistration(rows)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[item.ID]; exists {
			continue
		}
		seen[item.ID] = struct{}{}
		out = append(out, item)
	}
	return out, rows.Err()
}
