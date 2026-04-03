package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

func normalizeChatMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "plan":
		return "plan"
	case "review":
		return "review"
	default:
		return "chat"
	}
}

func normalizeChatRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "assistant":
		return "assistant"
	case "system":
		return "system"
	default:
		return "user"
	}
}

func normalizeRenderFormat(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "text":
		return "text"
	case "canvas":
		return "text"
	default:
		return "markdown"
	}
}

var chatSessionSelect = `
SELECT cs.id,
       cs.workspace_id,
       w.dir_path AS workspace_path,
       cs.app_thread_id,
       cs.mode,
       cs.created_at,
       cs.updated_at
  FROM chat_sessions cs
  JOIN workspaces w ON w.id = cs.workspace_id
`

func (s *Store) resolveChatSessionWorkspace(ref string) (Workspace, error) {
	cleanRef := strings.TrimSpace(ref)
	if cleanRef != "" {
		if workspace, err := s.GetWorkspaceByPath(cleanRef); err == nil {
			return workspace, nil
		} else if !errors.Is(err, sql.ErrNoRows) {
			return Workspace{}, err
		}
		if workspaceID, err := s.FindWorkspaceContainingPath(cleanRef); err != nil {
			return Workspace{}, err
		} else if workspaceID != nil {
			return s.GetWorkspace(*workspaceID)
		}
		if workspace, err := s.GetWorkspaceByStoredPath(cleanRef); err == nil {
			return workspace, nil
		} else if !errors.Is(err, sql.ErrNoRows) {
			return Workspace{}, err
		}
	}
	if workspace, err := s.ActiveWorkspace(); err == nil {
		return workspace, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Workspace{}, err
	}
	return Workspace{}, sql.ErrNoRows
}

func scanChatSession(scanner interface{ Scan(...any) error }) (ChatSession, error) {
	var out ChatSession
	err := scanner.Scan(
		&out.ID,
		&out.WorkspaceID,
		&out.WorkspacePath,
		&out.AppThreadID,
		&out.Mode,
		&out.CreatedAt,
		&out.UpdatedAt,
	)
	if err != nil {
		return ChatSession{}, err
	}
	out.WorkspacePath = strings.TrimSpace(out.WorkspacePath)
	out.Mode = normalizeChatMode(out.Mode)
	return out, nil
}

func (s *Store) hydrateChatSessionWorkspacePath(session ChatSession) ChatSession {
	session.WorkspacePath = s.compatibilityWorkspacePath(session.WorkspaceID, session.WorkspacePath)
	return session
}

func (s *Store) GetOrCreateChatSession(ref string) (ChatSession, error) {
	workspace, err := s.resolveChatSessionWorkspace(ref)
	if err != nil {
		return ChatSession{}, err
	}
	return s.GetOrCreateChatSessionForWorkspace(workspace.ID)
}

func (s *Store) GetOrCreateChatSessionForWorkspace(workspaceID int64) (ChatSession, error) {
	if workspaceID <= 0 {
		return ChatSession{}, errors.New("workspace id is required")
	}
	if existing, err := s.GetChatSessionByWorkspaceID(workspaceID); err == nil {
		return existing, nil
	}
	now := time.Now().Unix()
	id := fmt.Sprintf("chat-%s", randomHex(8))
	_, err := s.db.Exec(
		`INSERT INTO chat_sessions (id, workspace_id, app_thread_id, mode, created_at, updated_at) VALUES (?,?,?,?,?,?)`,
		id, workspaceID, "", "chat", now, now,
	)
	if err != nil {
		return ChatSession{}, err
	}
	return s.GetChatSession(id)
}

func (s *Store) GetChatSessionByWorkspacePath(ref string) (ChatSession, error) {
	workspace, err := s.resolveChatSessionWorkspace(ref)
	if err != nil {
		return ChatSession{}, err
	}
	return s.GetChatSessionByWorkspaceID(workspace.ID)
}

func (s *Store) GetChatSessionByWorkspaceID(workspaceID int64) (ChatSession, error) {
	if workspaceID <= 0 {
		return ChatSession{}, errors.New("workspace id is required")
	}
	session, err := scanChatSession(s.db.QueryRow(
		chatSessionSelect+` WHERE cs.workspace_id = ?`,
		workspaceID,
	))
	if err != nil {
		return ChatSession{}, err
	}
	return s.hydrateChatSessionWorkspacePath(session), nil
}

func (s *Store) GetChatSession(id string) (ChatSession, error) {
	session, err := scanChatSession(s.db.QueryRow(
		chatSessionSelect+` WHERE cs.id = ?`,
		strings.TrimSpace(id),
	))
	if err != nil {
		return ChatSession{}, err
	}
	return s.hydrateChatSessionWorkspacePath(session), nil
}

func (s *Store) ListChatSessions() ([]ChatSession, error) {
	rows, err := s.db.Query(
		chatSessionSelect + ` ORDER BY cs.created_at ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ChatSession, 0, 32)
	for rows.Next() {
		item, err := scanChatSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		out[i] = s.hydrateChatSessionWorkspacePath(out[i])
	}
	return out, nil
}

func (s *Store) UpdateChatSessionMode(id, mode string) (ChatSession, error) {
	normalizedMode := normalizeChatMode(mode)
	now := time.Now().Unix()
	_, err := s.db.Exec(
		`UPDATE chat_sessions SET mode = ?, updated_at = ? WHERE id = ?`,
		normalizedMode, now, strings.TrimSpace(id),
	)
	if err != nil {
		return ChatSession{}, err
	}
	return s.GetChatSession(id)
}

func (s *Store) UpdateChatSessionThread(id, appThreadID string) error {
	_, err := s.db.Exec(
		`UPDATE chat_sessions SET app_thread_id = ?, updated_at = ? WHERE id = ?`,
		strings.TrimSpace(appThreadID), time.Now().Unix(), strings.TrimSpace(id),
	)
	return err
}

func (s *Store) migrateChatSessionWorkspaceKey() error {
	tableColumns, err := s.tableColumnSet("chat_sessions")
	if err != nil {
		return err
	}
	columns := tableColumns["chat_sessions"]
	if columns["workspace_id"] && !columns["workspace_path"] {
		return nil
	}

	type legacySession struct {
		ID            string
		WorkspaceID   int64
		WorkspacePath string
		AppThreadID   string
		Mode          string
		CreatedAt     int64
		UpdatedAt     int64
	}

	legacy := make([]legacySession, 0, 16)
	switch {
	case columns["workspace_id"] && columns["workspace_path"]:
		rows, err := s.db.Query(`SELECT id, workspace_id, workspace_path, app_thread_id, mode, created_at, updated_at FROM chat_sessions ORDER BY created_at ASC, id ASC`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item legacySession
			if err := rows.Scan(&item.ID, &item.WorkspaceID, &item.WorkspacePath, &item.AppThreadID, &item.Mode, &item.CreatedAt, &item.UpdatedAt); err != nil {
				return err
			}
			item.WorkspacePath = strings.TrimSpace(item.WorkspacePath)
			legacy = append(legacy, item)
		}
		if err := rows.Err(); err != nil {
			return err
		}
	default:
		rows, err := s.db.Query(`SELECT id, workspace_path, app_thread_id, mode, created_at, updated_at FROM chat_sessions ORDER BY created_at ASC, id ASC`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item legacySession
			if err := rows.Scan(&item.ID, &item.WorkspacePath, &item.AppThreadID, &item.Mode, &item.CreatedAt, &item.UpdatedAt); err != nil {
				return err
			}
			item.WorkspacePath = strings.TrimSpace(item.WorkspacePath)
			legacy = append(legacy, item)
		}
		if err := rows.Err(); err != nil {
			return err
		}
	}

	for i := range legacy {
		if legacy[i].WorkspaceID > 0 {
			continue
		}
		workspace, err := s.resolveChatSessionWorkspace(legacy[i].WorkspacePath)
		if err != nil {
			return fmt.Errorf("resolve chat session workspace for %q: %w", legacy[i].ID, err)
		}
		legacy[i].WorkspaceID = workspace.ID
	}

	seenWorkspace := map[int64]struct{}{}
	deduped := make([]legacySession, 0, len(legacy))
	for _, item := range legacy {
		if item.WorkspaceID <= 0 {
			continue
		}
		if _, exists := seenWorkspace[item.WorkspaceID]; exists {
			continue
		}
		seenWorkspace[item.WorkspaceID] = struct{}{}
		deduped = append(deduped, item)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
CREATE TABLE chat_sessions_new (
  id TEXT PRIMARY KEY,
  workspace_id INTEGER NOT NULL UNIQUE REFERENCES workspaces(id) ON DELETE CASCADE,
  app_thread_id TEXT NOT NULL DEFAULT '',
  mode TEXT NOT NULL DEFAULT 'chat',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
)`); err != nil {
		return err
	}
	for _, item := range deduped {
		if _, err := tx.Exec(
			`INSERT INTO chat_sessions_new (id, workspace_id, app_thread_id, mode, created_at, updated_at) VALUES (?,?,?,?,?,?)`,
			item.ID,
			item.WorkspaceID,
			strings.TrimSpace(item.AppThreadID),
			normalizeChatMode(item.Mode),
			item.CreatedAt,
			item.UpdatedAt,
		); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`DROP TABLE chat_sessions`); err != nil {
		return err
	}
	if _, err := tx.Exec(`ALTER TABLE chat_sessions_new RENAME TO chat_sessions`); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) AddChatMessage(sessionID, role, contentMarkdown, contentPlain, renderFormat string, opts ...ChatMessageOption) (ChatMessage, error) {
	role = normalizeChatRole(role)
	renderFormat = normalizeRenderFormat(renderFormat)
	var o chatMessageOpts
	for _, fn := range opts {
		fn(&o)
	}
	threadKey := strings.TrimSpace(o.threadKey)
	provider := normalizeChatMessageProvider(o.provider)
	providerModel := strings.TrimSpace(o.providerModel)
	providerLatency := normalizeChatMessageProviderLatency(o.providerLatency)
	now := time.Now().Unix()
	res, err := s.db.Exec(
		`INSERT INTO chat_messages (session_id, role, content_markdown, content_plain, render_format, thread_key, provider, provider_model, provider_latency_ms, created_at) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		strings.TrimSpace(sessionID),
		role,
		contentMarkdown,
		contentPlain,
		renderFormat,
		threadKey,
		provider,
		providerModel,
		providerLatency,
		now,
	)
	if err != nil {
		return ChatMessage{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return ChatMessage{}, err
	}
	return ChatMessage{
		ID:              id,
		SessionID:       strings.TrimSpace(sessionID),
		Role:            role,
		ContentMarkdown: contentMarkdown,
		ContentPlain:    contentPlain,
		RenderFormat:    renderFormat,
		ThreadKey:       threadKey,
		Provider:        provider,
		ProviderModel:   providerModel,
		ProviderLatency: providerLatency,
		CreatedAt:       now,
	}, nil
}

type chatMessageOpts struct {
	threadKey       string
	provider        string
	providerModel   string
	providerLatency int
}

type ChatMessageOption func(*chatMessageOpts)

func WithThreadKey(key string) ChatMessageOption {
	return func(o *chatMessageOpts) {
		o.threadKey = key
	}
}

func WithProviderMetadata(provider, model string, latencyMS int) ChatMessageOption {
	return func(o *chatMessageOpts) {
		o.provider = provider
		o.providerModel = model
		o.providerLatency = latencyMS
	}
}

func normalizeChatMessageProvider(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func normalizeChatMessageProviderLatency(raw int) int {
	if raw < 0 {
		return 0
	}
	return raw
}

func (s *Store) UpdateChatMessageContent(id int64, contentMarkdown, contentPlain, renderFormat string, opts ...ChatMessageOption) error {
	if id <= 0 {
		return errors.New("message id is required")
	}
	renderFormat = normalizeRenderFormat(renderFormat)
	var o chatMessageOpts
	for _, fn := range opts {
		fn(&o)
	}
	_, err := s.db.Exec(
		`UPDATE chat_messages
		 SET content_markdown = ?, content_plain = ?, render_format = ?,
		     provider = ?, provider_model = ?, provider_latency_ms = ?
		 WHERE id = ?`,
		contentMarkdown,
		contentPlain,
		renderFormat,
		normalizeChatMessageProvider(o.provider),
		strings.TrimSpace(o.providerModel),
		normalizeChatMessageProviderLatency(o.providerLatency),
		id,
	)
	return err
}

func (s *Store) ListChatMessages(sessionID string, limit int, opts ...ChatMessageOption) ([]ChatMessage, error) {
	if limit <= 0 {
		limit = 200
	}
	var o chatMessageOpts
	for _, fn := range opts {
		fn(&o)
	}
	threadKey := strings.TrimSpace(o.threadKey)
	rows, err := s.db.Query(
		`SELECT id, session_id, role, content_markdown, content_plain, render_format, thread_key, provider, provider_model, provider_latency_ms, created_at
		 FROM chat_messages WHERE session_id = ? AND thread_key = ? ORDER BY id ASC LIMIT ?`,
		strings.TrimSpace(sessionID), threadKey, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ChatMessage, 0, limit)
	for rows.Next() {
		var item ChatMessage
		if err := rows.Scan(
			&item.ID,
			&item.SessionID,
			&item.Role,
			&item.ContentMarkdown,
			&item.ContentPlain,
			&item.RenderFormat,
			&item.ThreadKey,
			&item.Provider,
			&item.ProviderModel,
			&item.ProviderLatency,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		item.Role = normalizeChatRole(item.Role)
		item.RenderFormat = normalizeRenderFormat(item.RenderFormat)
		item.Provider = normalizeChatMessageProvider(item.Provider)
		item.ProviderModel = strings.TrimSpace(item.ProviderModel)
		item.ProviderLatency = normalizeChatMessageProviderLatency(item.ProviderLatency)
		out = append(out, item)
	}
	return out, nil
}

func (s *Store) ClearChatMessages(sessionID string) error {
	_, err := s.db.Exec("DELETE FROM chat_messages WHERE session_id = ?", sessionID)
	return err
}

func (s *Store) ClearAllChatMessages() error {
	_, err := s.db.Exec("DELETE FROM chat_messages")
	return err
}

func (s *Store) ClearAllChatEvents() error {
	_, err := s.db.Exec("DELETE FROM chat_events")
	return err
}

func (s *Store) ResetChatSessionThread(sessionID string) error {
	_, err := s.db.Exec("UPDATE chat_sessions SET app_thread_id = '' WHERE id = ?", sessionID)
	return err
}

func (s *Store) ResetAllChatSessionThreads() error {
	_, err := s.db.Exec("UPDATE chat_sessions SET app_thread_id = '', updated_at = ?", time.Now().Unix())
	return err
}

func (s *Store) AddChatEvent(sessionID, turnID, eventType, payloadJSON string) error {
	_, err := s.db.Exec(
		`INSERT INTO chat_events (session_id, turn_id, event_type, payload_json, created_at) VALUES (?,?,?,?,?)`,
		strings.TrimSpace(sessionID),
		strings.TrimSpace(turnID),
		strings.TrimSpace(eventType),
		payloadJSON,
		time.Now().Unix(),
	)
	return err
}
