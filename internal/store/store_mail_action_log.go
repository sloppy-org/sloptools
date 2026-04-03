package store

import (
	"encoding/json"
	"errors"
	"strings"
)

const (
	MailActionLogPending         = "pending"
	MailActionLogApplied         = "applied"
	MailActionLogFailed          = "failed"
	MailActionLogReconcileFailed = "reconcile_failed"
)

type MailActionLog struct {
	ID                int64  `json:"id"`
	AccountID         int64  `json:"account_id"`
	Provider          string `json:"provider"`
	MessageID         string `json:"message_id"`
	ResolvedMessageID string `json:"resolved_message_id"`
	Action            string `json:"action"`
	FolderFrom        string `json:"folder_from"`
	FolderTo          string `json:"folder_to"`
	Subject           string `json:"subject"`
	Sender            string `json:"sender"`
	RequestJSON       string `json:"request_json"`
	Status            string `json:"status"`
	ErrorText         string `json:"error_text"`
	CreatedAt         string `json:"created_at"`
	UpdatedAt         string `json:"updated_at"`
}

type MailActionLogInput struct {
	AccountID         int64
	Provider          string
	MessageID         string
	ResolvedMessageID string
	Action            string
	FolderFrom        string
	FolderTo          string
	Subject           string
	Sender            string
	Request           map[string]any
	Status            string
	ErrorText         string
}

func normalizeMailActionLogStatus(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case MailActionLogPending:
		return MailActionLogPending
	case MailActionLogApplied:
		return MailActionLogApplied
	case MailActionLogFailed:
		return MailActionLogFailed
	case MailActionLogReconcileFailed:
		return MailActionLogReconcileFailed
	default:
		return ""
	}
}

func (s *Store) CreateMailActionLog(input MailActionLogInput) (MailActionLog, error) {
	if input.AccountID <= 0 {
		return MailActionLog{}, errors.New("account_id is required")
	}
	provider := normalizeExternalAccountProvider(input.Provider)
	if provider == "" {
		return MailActionLog{}, errors.New("provider is required")
	}
	messageID := strings.TrimSpace(input.MessageID)
	if messageID == "" {
		return MailActionLog{}, errors.New("message_id is required")
	}
	action := strings.TrimSpace(strings.ToLower(input.Action))
	if action == "" {
		return MailActionLog{}, errors.New("action is required")
	}
	status := normalizeMailActionLogStatus(input.Status)
	if status == "" {
		status = MailActionLogPending
	}
	requestJSON := "{}"
	if input.Request != nil {
		raw, err := json.Marshal(input.Request)
		if err != nil {
			return MailActionLog{}, err
		}
		requestJSON = string(raw)
	}
	result, err := s.db.Exec(`INSERT INTO mail_action_logs (
	  account_id, provider, message_id, resolved_message_id, action, folder_from, folder_to, subject, sender, request_json, status, error_text
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		input.AccountID,
		provider,
		messageID,
		strings.TrimSpace(input.ResolvedMessageID),
		action,
		strings.TrimSpace(input.FolderFrom),
		strings.TrimSpace(input.FolderTo),
		strings.TrimSpace(input.Subject),
		strings.TrimSpace(input.Sender),
		requestJSON,
		status,
		strings.TrimSpace(input.ErrorText),
	)
	if err != nil {
		return MailActionLog{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return MailActionLog{}, err
	}
	return s.GetMailActionLog(id)
}

func (s *Store) GetMailActionLog(id int64) (MailActionLog, error) {
	if id <= 0 {
		return MailActionLog{}, errors.New("id is required")
	}
	var log MailActionLog
	err := s.db.QueryRow(`SELECT id, account_id, provider, message_id, resolved_message_id, action, folder_from, folder_to, subject, sender, request_json, status, error_text, created_at, updated_at
FROM mail_action_logs
WHERE id = ?`, id).Scan(
		&log.ID,
		&log.AccountID,
		&log.Provider,
		&log.MessageID,
		&log.ResolvedMessageID,
		&log.Action,
		&log.FolderFrom,
		&log.FolderTo,
		&log.Subject,
		&log.Sender,
		&log.RequestJSON,
		&log.Status,
		&log.ErrorText,
		&log.CreatedAt,
		&log.UpdatedAt,
	)
	if err != nil {
		return MailActionLog{}, err
	}
	return log, nil
}

func (s *Store) UpdateMailActionLogResult(id int64, status, resolvedMessageID, errorText string) error {
	if id <= 0 {
		return errors.New("id is required")
	}
	cleanStatus := normalizeMailActionLogStatus(status)
	if cleanStatus == "" {
		return errors.New("status is required")
	}
	_, err := s.db.Exec(`UPDATE mail_action_logs
SET resolved_message_id = ?, status = ?, error_text = ?, updated_at = datetime('now')
WHERE id = ?`,
		strings.TrimSpace(resolvedMessageID),
		cleanStatus,
		strings.TrimSpace(errorText),
		id,
	)
	return err
}

func (s *Store) ListMailActionLogs(accountID int64, limit int) ([]MailActionLog, error) {
	if accountID <= 0 {
		return nil, errors.New("account_id is required")
	}
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Query(`SELECT id, account_id, provider, message_id, resolved_message_id, action, folder_from, folder_to, subject, sender, request_json, status, error_text, created_at, updated_at
FROM mail_action_logs
WHERE account_id = ?
ORDER BY datetime(created_at) DESC, id DESC
LIMIT ?`, accountID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var logs []MailActionLog
	for rows.Next() {
		var log MailActionLog
		if err := rows.Scan(
			&log.ID,
			&log.AccountID,
			&log.Provider,
			&log.MessageID,
			&log.ResolvedMessageID,
			&log.Action,
			&log.FolderFrom,
			&log.FolderTo,
			&log.Subject,
			&log.Sender,
			&log.RequestJSON,
			&log.Status,
			&log.ErrorText,
			&log.CreatedAt,
			&log.UpdatedAt,
		); err != nil {
			return nil, err
		}
		logs = append(logs, log)
	}
	return logs, rows.Err()
}
