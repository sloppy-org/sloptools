package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
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
	res, err := s.db.Exec(`INSERT INTO batch_runs (workspace_id, config_json, status)
		 VALUES (?, ?, ?)`, workspaceID, cleanConfig, cleanStatus)
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
	return scanBatchRun(s.db.QueryRow(`SELECT id, workspace_id, started_at, finished_at, config_json, status
		 FROM batch_runs
		 WHERE id = ?`, id))
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
	res, err := s.db.Exec(`UPDATE batch_runs
		 SET status = ?, finished_at = ?
		 WHERE id = ?`, cleanStatus, finishedValue, id)
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
	_, err := s.db.Exec(`INSERT INTO batch_run_items (
		   batch_id, item_id, status, pr_number, pr_url, error_msg, started_at, finished_at
		 )
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(batch_id, item_id) DO UPDATE SET
		   status = excluded.status,
		   pr_number = excluded.pr_number,
		   pr_url = excluded.pr_url,
		   error_msg = excluded.error_msg,
		   started_at = COALESCE(batch_run_items.started_at, excluded.started_at),
		   finished_at = excluded.finished_at`, batchID, itemID, cleanStatus, normalizeOptionalInt64(update.PRNumber), normalizeOptionalString(update.PRURL), normalizeOptionalString(update.ErrorMsg), startedAt, finishedAt)
	if err != nil {
		return BatchRunItem{}, err
	}
	return s.GetBatchRunItem(batchID, itemID)
}

func (s *Store) GetBatchRunItem(batchID, itemID int64) (BatchRunItem, error) {
	return scanBatchRunItem(s.db.QueryRow(`SELECT bri.batch_id,
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
		 WHERE bri.batch_id = ? AND bri.item_id = ?`, batchID, itemID))
}

func (s *Store) ListBatchRunItems(batchID int64) ([]BatchRunItem, error) {
	if _, err := s.GetBatchRun(batchID); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT bri.batch_id,
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
		          bri.item_id ASC`, batchID)
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
	err := scanner.Scan(&out.ID, &out.WorkspaceID, &out.WorkspacePath, &out.AppThreadID, &out.Mode, &out.CreatedAt, &out.UpdatedAt)
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
	_, err := s.db.Exec(`INSERT INTO chat_sessions (id, workspace_id, app_thread_id, mode, created_at, updated_at) VALUES (?,?,?,?,?,?)`, id, workspaceID, "", "chat", now, now)
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
	session, err := scanChatSession(s.db.QueryRow(chatSessionSelect+` WHERE cs.workspace_id = ?`, workspaceID))
	if err != nil {
		return ChatSession{}, err
	}
	return s.hydrateChatSessionWorkspacePath(session), nil
}

func (s *Store) GetChatSession(id string) (ChatSession, error) {
	session, err := scanChatSession(s.db.QueryRow(chatSessionSelect+` WHERE cs.id = ?`, strings.TrimSpace(id)))
	if err != nil {
		return ChatSession{}, err
	}
	return s.hydrateChatSessionWorkspacePath(session), nil
}

func (s *Store) ListChatSessions() ([]ChatSession, error) {
	rows, err := s.db.Query(chatSessionSelect + ` ORDER BY cs.created_at ASC`)
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
	_, err := s.db.Exec(`UPDATE chat_sessions SET mode = ?, updated_at = ? WHERE id = ?`, normalizedMode, now, strings.TrimSpace(id))
	if err != nil {
		return ChatSession{}, err
	}
	return s.GetChatSession(id)
}

func (s *Store) UpdateChatSessionThread(id, appThreadID string) error {
	_, err := s.db.Exec(`UPDATE chat_sessions SET app_thread_id = ?, updated_at = ? WHERE id = ?`, strings.TrimSpace(appThreadID), time.Now().Unix(), strings.TrimSpace(id))
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
		if _, err := tx.Exec(`INSERT INTO chat_sessions_new (id, workspace_id, app_thread_id, mode, created_at, updated_at) VALUES (?,?,?,?,?,?)`, item.ID, item.WorkspaceID, strings.TrimSpace(item.AppThreadID), normalizeChatMode(item.Mode), item.CreatedAt, item.UpdatedAt); err != nil {
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
	res, err := s.db.Exec(`INSERT INTO chat_messages (session_id, role, content_markdown, content_plain, render_format, thread_key, provider, provider_model, provider_latency_ms, created_at) VALUES (?,?,?,?,?,?,?,?,?,?)`, strings.TrimSpace(sessionID), role, contentMarkdown, contentPlain, renderFormat, threadKey, provider, providerModel, providerLatency, now)
	if err != nil {
		return ChatMessage{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return ChatMessage{}, err
	}
	return ChatMessage{ID: id, SessionID: strings.TrimSpace(sessionID), Role: role, ContentMarkdown: contentMarkdown, ContentPlain: contentPlain, RenderFormat: renderFormat, ThreadKey: threadKey, Provider: provider, ProviderModel: providerModel, ProviderLatency: providerLatency, CreatedAt: now}, nil
}
