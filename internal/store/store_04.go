package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

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
	_, err := s.db.Exec(`UPDATE chat_messages
		 SET content_markdown = ?, content_plain = ?, render_format = ?,
		     provider = ?, provider_model = ?, provider_latency_ms = ?
		 WHERE id = ?`, contentMarkdown, contentPlain, renderFormat, normalizeChatMessageProvider(o.provider), strings.TrimSpace(o.providerModel), normalizeChatMessageProviderLatency(o.providerLatency), id)
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
	rows, err := s.db.Query(`SELECT id, session_id, role, content_markdown, content_plain, render_format, thread_key, provider, provider_model, provider_latency_ms, created_at
		 FROM chat_messages WHERE session_id = ? AND thread_key = ? ORDER BY id ASC LIMIT ?`, strings.TrimSpace(sessionID), threadKey, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ChatMessage, 0, limit)
	for rows.Next() {
		var item ChatMessage
		if err := rows.Scan(&item.ID, &item.SessionID, &item.Role, &item.ContentMarkdown, &item.ContentPlain, &item.RenderFormat, &item.ThreadKey, &item.Provider, &item.ProviderModel, &item.ProviderLatency, &item.CreatedAt); err != nil {
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
	_, err := s.db.Exec(`INSERT INTO chat_events (session_id, turn_id, event_type, payload_json, created_at) VALUES (?,?,?,?,?)`, strings.TrimSpace(sessionID), strings.TrimSpace(turnID), strings.TrimSpace(eventType), payloadJSON, time.Now().Unix())
	return err
}

func (s *Store) CreateLabel(name string, parentID *int64) (Label, error) {
	cleanName := strings.TrimSpace(name)
	if cleanName == "" {
		return Label{}, errors.New("label name is required")
	}
	if parentID != nil && *parentID <= 0 {
		return Label{}, errors.New("parent_id must be a positive integer")
	}
	var existingID int64
	err := s.db.QueryRow(`SELECT id
		 FROM contexts
		 WHERE lower(name) = lower(?)
		   AND (
		     (parent_id IS NULL AND ? IS NULL)
		     OR parent_id = ?
		   )`, cleanName, parentID, parentID).Scan(&existingID)
	switch {
	case err == nil:
		return s.GetLabel(existingID)
	case !errors.Is(err, sql.ErrNoRows):
		return Label{}, err
	}
	res, err := s.db.Exec(`INSERT INTO contexts (name, parent_id) VALUES (?, ?)`, cleanName, parentID)
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
	err := s.db.QueryRow(`SELECT id
		 FROM contexts
		 WHERE lower(name) = lower(?)`, strings.TrimSpace(name)).Scan(&contextID)
	return contextID, err
}

func (s *Store) GetLabel(id int64) (Label, error) {
	row := s.db.QueryRow(`SELECT id, name, color, parent_id, created_at
		 FROM contexts
		 WHERE id = ?`, id)
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
	_, err := s.db.Exec(`INSERT OR IGNORE INTO context_workspaces (context_id, workspace_id) VALUES (?, ?)`, labelID, workspaceID)
	return err
}

func (s *Store) LinkLabelToItem(labelID, itemID int64) error {
	if labelID <= 0 || itemID <= 0 {
		return errors.New("label_id and item_id must be positive integers")
	}
	_, err := s.db.Exec(`INSERT OR IGNORE INTO context_items (context_id, item_id) VALUES (?, ?)`, labelID, itemID)
	return err
}

func (s *Store) LinkLabelToArtifact(labelID, artifactID int64) error {
	if labelID <= 0 || artifactID <= 0 {
		return errors.New("label_id and artifact_id must be positive integers")
	}
	_, err := s.db.Exec(`INSERT OR IGNORE INTO context_artifacts (context_id, artifact_id) VALUES (?, ?)`, labelID, artifactID)
	return err
}

func (s *Store) ListLabels() ([]Label, error) {
	rows, err := s.db.Query(`SELECT id, name, color, parent_id, created_at
		 FROM contexts
		 ORDER BY CASE WHEN parent_id IS NULL THEN 0 ELSE 1 END, lower(name), id`)
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

func (s *Store) EnsureDateContextHierarchy(date time.Time) (int64, error) {
	names := dateContextNames(date)
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var (
		parentID *int64
		lastID   int64
	)
	for _, name := range // EnsureDateContextHierarchy creates or repairs the YYYY -> YYYY/MM -> YYYY/MM/DD
	// context chain for a given UTC calendar date and returns the day-level context ID.
	names {
		contextID, err := ensureContextWithParentTx(tx, name, parentID)
		if err != nil {
			return 0, err
		}
		lastID = contextID
		parentID = &lastID
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return lastID, nil
}

func dateContextNames(date time.Time) []string {
	utc := date.UTC()
	return []string{utc.Format("2006"), utc.Format("2006/01"), utc.Format("2006/01/02")}
}

func ensureContextWithParentTx(tx *sql.Tx, name string, parentID *int64) (int64, error) {
	cleanName := strings.TrimSpace(name)
	if cleanName == "" {
		return 0, errors.New("context name is required")
	}
	var (
		contextID        int64
		existingParentID sql.NullInt64
	)
	err := tx.QueryRow(`SELECT id, parent_id
		 FROM contexts
		 WHERE lower(name) = lower(?)`, cleanName).Scan(&contextID, &existingParentID)
	switch {
	case err == nil:
		if !sameContextParent(existingParentID, parentID) {
			if _, err := tx.Exec(`UPDATE contexts SET parent_id = ? WHERE id = ?`, parentID, contextID); err != nil {
				return 0, err
			}
		}
		return contextID, nil
	case err != nil && !errors.Is(err, sql.ErrNoRows):
		return 0, err
	}
	res, err := tx.Exec(`INSERT INTO contexts (name, parent_id) VALUES (?, ?)`, cleanName, parentID)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func sameContextParent(existing sql.NullInt64, parentID *int64) bool {
	switch {
	case !existing.Valid && parentID == nil:
		return true
	case !existing.Valid || parentID == nil:
		return false
	default:
		return existing.Int64 == *parentID
	}
}

func parseDailyWorkspaceDate(value string) (time.Time, error) {
	clean := normalizeDailyWorkspaceDate(value)
	if clean == "" {
		return time.Time{}, errors.New("daily workspace date must be YYYY-MM-DD")
	}
	return time.Parse("2006-01-02", clean)
}

func (s *Store) syncWorkspaceDateContext(workspaceID int64, dailyDate *string) error {
	if workspaceID <= 0 || dailyDate == nil || strings.TrimSpace(*dailyDate) == "" {
		return nil
	}
	date, err := parseDailyWorkspaceDate(*dailyDate)
	if err != nil {
		return err
	}
	contextID, err := s.EnsureDateContextHierarchy(date)
	if err != nil {
		return err
	}
	return s.LinkLabelToWorkspace(contextID, workspaceID)
}

func (s *Store) syncItemDateContext(itemID int64, workspaceID *int64) error {
	if itemID <= 0 {
		return nil
	}
	if _, err := s.db.Exec(`DELETE FROM context_items
		 WHERE item_id = ?
		   AND context_id IN (
		     SELECT id
		     FROM contexts
		     WHERE name GLOB '[0-9][0-9][0-9][0-9]/[0-9][0-9]/[0-9][0-9]'
		   )`, itemID); err != nil {
		return err
	}
	if workspaceID == nil || *workspaceID <= 0 {
		return nil
	}
	workspace, err := s.GetWorkspace(*workspaceID)
	if err != nil {
		return err
	}
	if workspace.DailyDate == nil || strings.TrimSpace(*workspace.DailyDate) == "" {
		return nil
	}
	date, err := parseDailyWorkspaceDate(*workspace.DailyDate)
	if err != nil {
		return err
	}
	contextID, err := s.EnsureDateContextHierarchy(date)
	if err != nil {
		return err
	}
	return s.LinkLabelToItem(contextID, itemID)
}

func normalizeOptionalContextQuery(value string) string {
	return strings.TrimSpace(value)
}

func splitContextQueryTerms(query string) []string {
	cleanQuery := normalizeOptionalContextQuery(query)
	if cleanQuery == "" {
		return nil
	}
	rawTerms := strings.FieldsFunc(cleanQuery, func(r rune) bool {
		return r == '+' || r == ','
	})
	terms := make([]string, 0, len(rawTerms))
	for _, term := range rawTerms {
		clean := normalizeOptionalContextQuery(term)
		if clean == "" {
			continue
		}
		terms = append(terms, clean)
	}
	if len(terms) > 0 {
		return terms
	}
	return []string{cleanQuery}
}

func placeholders(count int) string {
	if count <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", count), ",")
}

func contextLinkExistsClause(linkTable, entityColumn, entityExpr string, contextIDs []int64) (string, []any) {
	if len(contextIDs) == 0 {
		return "0=1", nil
	}
	args := make([]any, 0, len(contextIDs))
	for _, contextID := range contextIDs {
		args = append(args, contextID)
	}
	return `EXISTS (
SELECT 1
FROM ` + linkTable + ` link
WHERE link.` + entityColumn + ` = ` + entityExpr + `
  AND link.context_id IN (` + placeholders(len(contextIDs)) + `)
)`, args
}

func (s *Store) resolveContextQueryIDs(query string) ([]int64, error) {
	cleanQuery := strings.ToLower(strings.TrimSpace(query))
	if cleanQuery == "" {
		return nil, nil
	}
	rows, err := s.db.Query(`WITH RECURSIVE context_paths(id, parent_id, name, path) AS (
		   SELECT id, parent_id, lower(name), lower(name)
		   FROM contexts
		   WHERE parent_id IS NULL
		   UNION ALL
		   SELECT c.id, c.parent_id, lower(c.name), cp.path || '/' || lower(c.name)
		   FROM contexts c
		   JOIN context_paths cp ON cp.id = c.parent_id
		 ),
		 matched_exact(id) AS (
		   SELECT id
		   FROM context_paths
		   WHERE name = ? OR path = ?
		 ),
		 matched_prefix(id) AS (
		   SELECT id
		   FROM context_paths
		   WHERE name = ?
		      OR name LIKE ? || '/%'
		      OR path = ?
		      OR path LIKE ? || '/%'
		 ),
		 descendants(id) AS (
		   SELECT id FROM matched_exact
		   UNION
		   SELECT c.id
		   FROM contexts c
		   JOIN descendants d ON c.parent_id = d.id
		 )
		 SELECT DISTINCT id
		 FROM (
		   SELECT id FROM matched_prefix
		   UNION
		   SELECT id FROM descendants
		 )
		 ORDER BY id ASC`, cleanQuery, cleanQuery, cleanQuery, cleanQuery, cleanQuery, cleanQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var contextID int64
		if err := rows.Scan(&contextID); err != nil {
			return nil, err
		}
		out = append(out, contextID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) workspaceHasAnyContext(workspaceID int64, contextIDs []int64) (bool, error) {
	if workspaceID <= 0 {
		return false, nil
	}
	clause, args := contextLinkExistsClause("context_workspaces", "workspace_id", "?", contextIDs)
	var matched int
	rowArgs := append([]any{workspaceID}, args...)
	if err := s.db.QueryRow(`SELECT CASE WHEN `+clause+` THEN 1 ELSE 0 END`, rowArgs...).Scan(&matched); err != nil {
		return false, err
	}
	return matched != 0, nil
}

func (s *Store) entityHasAnyContext(linkTable, entityColumn string, entityID int64, contextIDs []int64) (bool, error) {
	if entityID <= 0 {
		return false, nil
	}
	clause, args := contextLinkExistsClause(linkTable, entityColumn, "?", contextIDs)
	var matched int
	rowArgs := append([]any{entityID}, args...)
	if err := s.db.QueryRow(`SELECT CASE WHEN `+clause+` THEN 1 ELSE 0 END`, rowArgs...).Scan(&matched); err != nil {
		return false, err
	}
	return matched != 0, nil
}
