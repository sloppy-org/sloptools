package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

type ParticipantSession struct {
	ID            string `json:"id"`
	WorkspaceID   int64  `json:"workspace_id"`
	WorkspacePath string `json:"workspace_path"`
	StartedAt     int64  `json:"started_at"`
	EndedAt       int64  `json:"ended_at"`
	ConfigJSON    string `json:"config_json"`
}

type ParticipantSegment struct {
	ID          int64  `json:"id"`
	SessionID   string `json:"session_id"`
	StartTS     int64  `json:"start_ts"`
	EndTS       int64  `json:"end_ts"`
	Speaker     string `json:"speaker"`
	Text        string `json:"text"`
	Model       string `json:"model"`
	LatencyMS   int64  `json:"latency_ms"`
	CommittedAt int64  `json:"committed_at"`
	Status      string `json:"status"`
}

type ParticipantEvent struct {
	ID          int64  `json:"id"`
	SessionID   string `json:"session_id"`
	SegmentID   int64  `json:"segment_id"`
	EventType   string `json:"event_type"`
	PayloadJSON string `json:"payload_json"`
	CreatedAt   int64  `json:"created_at"`
}

type ParticipantRoomState struct {
	ID                int64  `json:"id"`
	SessionID         string `json:"session_id"`
	SummaryText       string `json:"summary_text"`
	EntitiesJSON      string `json:"entities_json"`
	TopicTimelineJSON string `json:"topic_timeline_json"`
	UpdatedAt         int64  `json:"updated_at"`
}

var ErrParticipantSessionEnded = errors.New("participant session is ended")

var participantSessionSelect = `
SELECT ps.id,
       ps.workspace_id,
       w.dir_path AS workspace_path,
       ps.started_at,
       ps.ended_at,
       ps.config_json
  FROM participant_sessions ps
  JOIN workspaces w ON w.id = ps.workspace_id
`

func scanParticipantSession(scanner interface{ Scan(...any) error }) (ParticipantSession, error) {
	var out ParticipantSession
	if err := scanner.Scan(
		&out.ID,
		&out.WorkspaceID,
		&out.WorkspacePath,
		&out.StartedAt,
		&out.EndedAt,
		&out.ConfigJSON,
	); err != nil {
		return ParticipantSession{}, err
	}
	out.WorkspacePath = strings.TrimSpace(out.WorkspacePath)
	out.ConfigJSON = strings.TrimSpace(out.ConfigJSON)
	return out, nil
}

func (s *Store) hydrateParticipantSessionWorkspacePath(session ParticipantSession) ParticipantSession {
	session.WorkspacePath = s.compatibilityWorkspacePath(session.WorkspaceID, session.WorkspacePath)
	return session
}

func (s *Store) resolveParticipantSessionWorkspace(ref string) (Workspace, error) {
	cleanRef := strings.TrimSpace(ref)
	if cleanRef == "" {
		return Workspace{}, sql.ErrNoRows
	}
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
	return Workspace{}, sql.ErrNoRows
}

func (s *Store) AddParticipantSession(ref, configJSON string) (ParticipantSession, error) {
	cleanRef := strings.TrimSpace(ref)
	if cleanRef == "" {
		return ParticipantSession{}, errors.New("workspace reference is required")
	}
	workspace, err := s.resolveParticipantSessionWorkspace(cleanRef)
	if err != nil {
		return ParticipantSession{}, err
	}
	return s.AddParticipantSessionForWorkspace(workspace.ID, configJSON)
}

func (s *Store) AddParticipantSessionForWorkspace(workspaceID int64, configJSON string) (ParticipantSession, error) {
	if workspaceID <= 0 {
		return ParticipantSession{}, errors.New("workspace id is required")
	}
	if strings.TrimSpace(configJSON) == "" {
		configJSON = "{}"
	}
	now := time.Now().Unix()
	id := fmt.Sprintf("psess-%s", randomHex(8))
	_, err := s.db.Exec(
		`INSERT INTO participant_sessions (id, workspace_id, started_at, ended_at, config_json) VALUES (?,?,?,0,?)`,
		id, workspaceID, now, configJSON,
	)
	if err != nil {
		return ParticipantSession{}, err
	}
	return s.GetParticipantSession(id)
}

func (s *Store) GetParticipantSession(id string) (ParticipantSession, error) {
	session, err := scanParticipantSession(s.db.QueryRow(
		participantSessionSelect+` WHERE ps.id = ?`,
		strings.TrimSpace(id),
	))
	if err != nil {
		return ParticipantSession{}, err
	}
	return s.hydrateParticipantSessionWorkspacePath(session), nil
}

func (s *Store) ListParticipantSessions(ref string) ([]ParticipantSession, error) {
	cleanRef := strings.TrimSpace(ref)
	if cleanRef == "" {
		return s.listParticipantSessionsQuery(
			participantSessionSelect + ` ORDER BY ps.started_at DESC, ps.id DESC`,
		)
	}
	workspace, err := s.resolveParticipantSessionWorkspace(cleanRef)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return []ParticipantSession{}, nil
		}
		return nil, err
	}
	return s.ListParticipantSessionsForWorkspace(workspace.ID)
}

func (s *Store) ListParticipantSessionsForWorkspace(workspaceID int64) ([]ParticipantSession, error) {
	if workspaceID <= 0 {
		return nil, errors.New("workspace id is required")
	}
	return s.listParticipantSessionsQuery(
		participantSessionSelect+` WHERE ps.workspace_id = ? ORDER BY ps.started_at DESC, ps.id DESC`,
		workspaceID,
	)
}

func (s *Store) listParticipantSessionsQuery(query string, args ...any) ([]ParticipantSession, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ParticipantSession{}
	for rows.Next() {
		item, err := scanParticipantSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		out[i] = s.hydrateParticipantSessionWorkspacePath(out[i])
	}
	return out, nil
}

func (s *Store) EndParticipantSession(id string) error {
	cleanID := strings.TrimSpace(id)
	if cleanID == "" {
		return errors.New("session id is required")
	}
	now := time.Now().Unix()
	_, err := s.db.Exec(`UPDATE participant_sessions SET ended_at = ? WHERE id = ?`, now, cleanID)
	return err
}

func (s *Store) migrateParticipantSessionWorkspaceKey() error {
	tableColumns, err := s.tableColumnSet("participant_sessions")
	if err != nil {
		return err
	}
	columns := tableColumns["participant_sessions"]
	if columns["workspace_id"] && !columns["workspace_path"] {
		return nil
	}

	type legacySession struct {
		ID          string
		WorkspaceID int64
		Ref         string
		StartedAt   int64
		EndedAt     int64
		ConfigJSON  string
	}

	legacy := make([]legacySession, 0, 16)
	if columns["workspace_id"] {
		rows, err := s.db.Query(`SELECT id, workspace_id, started_at, ended_at, config_json FROM participant_sessions ORDER BY started_at ASC, id ASC`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item legacySession
			if err := rows.Scan(&item.ID, &item.WorkspaceID, &item.StartedAt, &item.EndedAt, &item.ConfigJSON); err != nil {
				return err
			}
			legacy = append(legacy, item)
		}
		if err := rows.Err(); err != nil {
			return err
		}
	} else {
		rows, err := s.db.Query(`SELECT id, workspace_path, started_at, ended_at, config_json FROM participant_sessions ORDER BY started_at ASC, id ASC`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item legacySession
			if err := rows.Scan(&item.ID, &item.Ref, &item.StartedAt, &item.EndedAt, &item.ConfigJSON); err != nil {
				return err
			}
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
		workspace, err := s.resolveParticipantSessionWorkspace(legacy[i].Ref)
		if err != nil {
			return fmt.Errorf("resolve participant session workspace for %q: %w", legacy[i].ID, err)
		}
		legacy[i].WorkspaceID = workspace.ID
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
CREATE TABLE participant_sessions_new (
  id TEXT PRIMARY KEY,
  workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  started_at INTEGER NOT NULL,
  ended_at INTEGER NOT NULL DEFAULT 0,
  config_json TEXT NOT NULL DEFAULT '{}'
)`); err != nil {
		return err
	}
	for _, item := range legacy {
		if _, err := tx.Exec(
			`INSERT INTO participant_sessions_new (id, workspace_id, started_at, ended_at, config_json) VALUES (?,?,?,?,?)`,
			item.ID,
			item.WorkspaceID,
			item.StartedAt,
			item.EndedAt,
			strings.TrimSpace(item.ConfigJSON),
		); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`DROP TABLE participant_sessions`); err != nil {
		return err
	}
	if _, err := tx.Exec(`ALTER TABLE participant_sessions_new RENAME TO participant_sessions`); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) AddParticipantSegment(seg ParticipantSegment) (ParticipantSegment, error) {
	sessionID := strings.TrimSpace(seg.SessionID)
	if sessionID == "" {
		return ParticipantSegment{}, errors.New("session id is required")
	}
	var endedAt int64
	if err := s.db.QueryRow(
		`SELECT ended_at FROM participant_sessions WHERE id = ?`,
		sessionID,
	).Scan(&endedAt); err != nil {
		return ParticipantSegment{}, err
	}
	if endedAt != 0 {
		return ParticipantSegment{}, ErrParticipantSessionEnded
	}
	now := time.Now().Unix()
	if seg.CommittedAt == 0 {
		seg.CommittedAt = now
	}
	if seg.Status == "" {
		seg.Status = "final"
	}
	res, err := s.db.Exec(
		`INSERT INTO participant_segments (session_id, start_ts, end_ts, speaker, text, model, latency_ms, committed_at, status) VALUES (?,?,?,?,?,?,?,?,?)`,
		sessionID, seg.StartTS, seg.EndTS, seg.Speaker, seg.Text, seg.Model, seg.LatencyMS, seg.CommittedAt, seg.Status,
	)
	if err != nil {
		return ParticipantSegment{}, err
	}
	id, _ := res.LastInsertId()
	seg.ID = id
	seg.SessionID = sessionID
	return seg, nil
}

func (s *Store) ListParticipantSegments(sessionID string, fromTS, toTS int64) ([]ParticipantSegment, error) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return nil, errors.New("session id is required")
	}
	query := `SELECT id, session_id, start_ts, end_ts, speaker, text, model, latency_ms, committed_at, status FROM participant_segments WHERE session_id = ?`
	args := []interface{}{sid}
	if fromTS > 0 {
		query += ` AND start_ts >= ?`
		args = append(args, fromTS)
	}
	if toTS > 0 {
		query += ` AND start_ts <= ?`
		args = append(args, toTS)
	}
	query += ` ORDER BY start_ts ASC, id ASC`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ParticipantSegment{}
	for rows.Next() {
		var item ParticipantSegment
		if err := rows.Scan(&item.ID, &item.SessionID, &item.StartTS, &item.EndTS, &item.Speaker, &item.Text, &item.Model, &item.LatencyMS, &item.CommittedAt, &item.Status); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) SearchParticipantSegments(sessionID, query string) ([]ParticipantSegment, error) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return nil, errors.New("session id is required")
	}
	q := strings.TrimSpace(query)
	if q == "" {
		return s.ListParticipantSegments(sid, 0, 0)
	}
	rows, err := s.db.Query(
		`SELECT id, session_id, start_ts, end_ts, speaker, text, model, latency_ms, committed_at, status
		 FROM participant_segments WHERE session_id = ? AND text LIKE ? ORDER BY start_ts ASC, id ASC`,
		sid, "%"+q+"%",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ParticipantSegment{}
	for rows.Next() {
		var item ParticipantSegment
		if err := rows.Scan(&item.ID, &item.SessionID, &item.StartTS, &item.EndTS, &item.Speaker, &item.Text, &item.Model, &item.LatencyMS, &item.CommittedAt, &item.Status); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) AddParticipantEvent(sessionID string, segmentID int64, eventType, payloadJSON string) error {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return errors.New("session id is required")
	}
	_, err := s.db.Exec(
		`INSERT INTO participant_events (session_id, segment_id, event_type, payload_json, created_at) VALUES (?,?,?,?,?)`,
		sid, segmentID, strings.TrimSpace(eventType), payloadJSON, time.Now().Unix(),
	)
	return err
}

func (s *Store) ListParticipantEvents(sessionID string) ([]ParticipantEvent, error) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return nil, errors.New("session id is required")
	}
	rows, err := s.db.Query(
		`SELECT id, session_id, segment_id, event_type, payload_json, created_at FROM participant_events WHERE session_id = ? ORDER BY created_at ASC, id ASC`,
		sid,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ParticipantEvent{}
	for rows.Next() {
		var item ParticipantEvent
		if err := rows.Scan(&item.ID, &item.SessionID, &item.SegmentID, &item.EventType, &item.PayloadJSON, &item.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) UpsertParticipantRoomState(sessionID, summaryText, entitiesJSON, topicTimelineJSON string) error {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return errors.New("session id is required")
	}
	if strings.TrimSpace(entitiesJSON) == "" {
		entitiesJSON = "[]"
	}
	if strings.TrimSpace(topicTimelineJSON) == "" {
		topicTimelineJSON = "[]"
	}
	now := time.Now().Unix()
	_, err := s.db.Exec(
		`INSERT INTO participant_room_state (session_id, summary_text, entities_json, topic_timeline_json, updated_at)
		 VALUES (?,?,?,?,?)
		 ON CONFLICT(session_id) DO UPDATE SET summary_text=excluded.summary_text, entities_json=excluded.entities_json, topic_timeline_json=excluded.topic_timeline_json, updated_at=excluded.updated_at`,
		sid, summaryText, entitiesJSON, topicTimelineJSON, now,
	)
	return err
}

func (s *Store) GetParticipantRoomState(sessionID string) (ParticipantRoomState, error) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return ParticipantRoomState{}, errors.New("session id is required")
	}
	var out ParticipantRoomState
	err := s.db.QueryRow(
		`SELECT id, session_id, summary_text, entities_json, topic_timeline_json, updated_at FROM participant_room_state WHERE session_id = ?`,
		sid,
	).Scan(&out.ID, &out.SessionID, &out.SummaryText, &out.EntitiesJSON, &out.TopicTimelineJSON, &out.UpdatedAt)
	if err != nil {
		return ParticipantRoomState{}, err
	}
	return out, nil
}
