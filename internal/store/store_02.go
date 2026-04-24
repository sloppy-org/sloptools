package store

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/sloppy-org/sloptools/internal/modelprofile"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type HostConfig struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	Hostname   string `json:"hostname"`
	Port       int    `json:"port"`
	Username   string `json:"username"`
	KeyPath    string `json:"key_path"`
	ProjectDir string `json:"project_dir"`
}

type Store struct {
	db                             *sql.DB
	externalAccountLookupEnv       func(string) (string, bool)
	externalAccountCommandRunner   externalAccountCommandRunner
	externalAccountCredentialMu    sync.Mutex
	externalAccountCredentialCache map[string]cachedExternalAccountCredential
}

type ChatSession struct {
	ID            string `json:"id"`
	WorkspaceID   int64  `json:"workspace_id"`
	WorkspacePath string `json:"workspace_path"`
	AppThreadID   string `json:"app_thread_id"`
	Mode          string `json:"mode"`
	CreatedAt     int64  `json:"created_at"`
	UpdatedAt     int64  `json:"updated_at"`
}

type ChatMessage struct {
	ID              int64  `json:"id"`
	SessionID       string `json:"session_id"`
	Role            string `json:"role"`
	ContentMarkdown string `json:"content_markdown"`
	ContentPlain    string `json:"content_plain"`
	RenderFormat    string `json:"render_format"`
	ThreadKey       string `json:"thread_key,omitempty"`
	Provider        string `json:"provider,omitempty"`
	ProviderModel   string `json:"provider_model,omitempty"`
	ProviderLatency int    `json:"provider_latency_ms,omitempty"`
	CreatedAt       int64  `json:"created_at"`
}

func New(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		_ = db.Close()
		return nil, err
	}
	s := &Store{db: db, externalAccountLookupEnv: os.LookupEnv, externalAccountCommandRunner: runExternalAccountCommand, externalAccountCredentialCache: map[string]cachedExternalAccountCredential{}}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) tableColumnNames(table string) ([]string, // tableColumnNames returns the lowercased column names for a single table.
	error) {
	rows, err := s.db.Query(fmt.Sprintf(`PRAGMA table_info("%s")`, table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var cid int
		var name, ctype string
		var notNull int
		var dflt sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &ctype, &notNull, &dflt, &pk); err != nil {
			return nil, err
		}
		cols = append(cols, strings.ToLower(strings.TrimSpace(name)))
	}
	return cols, rows.Err()
}

func (s *Store) TableColumns() (map[ // TableColumns returns a map from table name to the list of column names
// for every user table in the database.
string][]string, error) {
	rows, err := s.db.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result := make(map[string][]string, len(tables))
	for _, table := range tables {
		cols, err := s.tableColumnNames(table)
		if err != nil {
			return nil, err
		}
		result[table] = cols
	}
	return result, nil
}

func (s *Store) migrate() error {
	schema := `
CREATE TABLE IF NOT EXISTS hosts (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE,
  hostname TEXT NOT NULL,
  port INTEGER NOT NULL DEFAULT 22,
  username TEXT NOT NULL,
  key_path TEXT NOT NULL DEFAULT '',
  project_dir TEXT NOT NULL DEFAULT '~'
);
CREATE TABLE IF NOT EXISTS admin (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  pw_hash TEXT NOT NULL,
  pw_salt TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS auth_sessions (
  token TEXT PRIMARY KEY,
  created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS remote_sessions (
  session_id TEXT PRIMARY KEY,
  host_id INTEGER NOT NULL,
  created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS chat_sessions (
  id TEXT PRIMARY KEY,
  workspace_id INTEGER NOT NULL UNIQUE REFERENCES workspaces(id) ON DELETE CASCADE,
  app_thread_id TEXT NOT NULL DEFAULT '',
  mode TEXT NOT NULL DEFAULT 'chat',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS chat_messages (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT NOT NULL,
  role TEXT NOT NULL,
  content_markdown TEXT NOT NULL DEFAULT '',
  content_plain TEXT NOT NULL DEFAULT '',
  render_format TEXT NOT NULL DEFAULT 'markdown',
  thread_key TEXT NOT NULL DEFAULT '',
  provider TEXT NOT NULL DEFAULT '',
  provider_model TEXT NOT NULL DEFAULT '',
  provider_latency_ms INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_chat_messages_session_created
  ON chat_messages(session_id, created_at, id);
CREATE TABLE IF NOT EXISTS chat_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT NOT NULL,
  turn_id TEXT NOT NULL DEFAULT '',
  event_type TEXT NOT NULL,
  payload_json TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_chat_events_session_created
  ON chat_events(session_id, created_at, id);
CREATE TABLE IF NOT EXISTS app_state (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS participant_sessions (
  id TEXT PRIMARY KEY,
  workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  started_at INTEGER NOT NULL,
  ended_at INTEGER NOT NULL DEFAULT 0,
  config_json TEXT NOT NULL DEFAULT '{}'
);
CREATE TABLE IF NOT EXISTS participant_segments (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT NOT NULL,
  start_ts INTEGER NOT NULL,
  end_ts INTEGER NOT NULL DEFAULT 0,
  speaker TEXT NOT NULL DEFAULT '',
  text TEXT NOT NULL DEFAULT '',
  model TEXT NOT NULL DEFAULT '',
  latency_ms INTEGER NOT NULL DEFAULT 0,
  committed_at INTEGER NOT NULL,
  status TEXT NOT NULL DEFAULT 'final'
);
CREATE INDEX IF NOT EXISTS idx_participant_segments_session
  ON participant_segments(session_id, start_ts);
CREATE TABLE IF NOT EXISTS participant_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT NOT NULL,
  segment_id INTEGER NOT NULL DEFAULT 0,
  event_type TEXT NOT NULL,
  payload_json TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_participant_events_session
  ON participant_events(session_id, created_at);
CREATE TABLE IF NOT EXISTS participant_room_state (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT NOT NULL UNIQUE,
  summary_text TEXT NOT NULL DEFAULT '',
  entities_json TEXT NOT NULL DEFAULT '[]',
  topic_timeline_json TEXT NOT NULL DEFAULT '[]',
  updated_at INTEGER NOT NULL
);
`
	if _, err := s.db.Exec(schema); err != nil {
		return err
	}
	if err := s.migrateChatMessageColumns(); err != nil {
		return err
	}
	if err := s.migrateParticipantColumns(); err != nil {
		return err
	}
	if err := s.migrateDomainTables(); err != nil {
		return err
	}
	if err := s.migrateParticipantSessionWorkspaceKey(); err != nil {
		return err
	}
	return s.migrateChatSessionWorkspaceKey()
}

func (s *Store) migrateChatMessageColumns() error {
	type colDef struct {
		Table string
		Name  string
		SQL   string
	}
	columns := []colDef{{Table: "chat_messages", Name: "thread_key", SQL: `ALTER TABLE chat_messages ADD COLUMN thread_key TEXT NOT NULL DEFAULT ''`}, {Table: "chat_messages", Name: "provider", SQL: `ALTER TABLE chat_messages ADD COLUMN provider TEXT NOT NULL DEFAULT ''`}, {Table: "chat_messages", Name: "provider_model", SQL: `ALTER TABLE chat_messages ADD COLUMN provider_model TEXT NOT NULL DEFAULT ''`}, {Table: "chat_messages", Name: "provider_latency_ms", SQL: `ALTER TABLE chat_messages ADD COLUMN provider_latency_ms INTEGER NOT NULL DEFAULT 0`}}
	tableColumns := map[string]map[string]bool{}
	for _, table := range []string{"chat_messages"} {
		cols, err := s.tableColumnNames(table)
		if err != nil {
			return err
		}
		existing := make(map[string]bool, len(cols))
		for _, c := range cols {
			existing[c] = true
		}
		tableColumns[table] = existing
	}
	for _, col := range columns {
		if tableColumns[col.Table][col.Name] {
			continue
		}
		if _, err := s.db.Exec(col.SQL); err != nil {
			return err
		}
	}
	_, _ = s.db.Exec(`UPDATE chat_messages SET render_format = 'text' WHERE lower(trim(render_format)) = 'canvas'`)
	return nil
}

func (s *Store) migrateParticipantColumns() error {
	type colDef struct {
		Table string
		Name  string
		SQL   string
	}
	columns := []colDef{{Table: "participant_sessions", Name: "config_json", SQL: `ALTER TABLE participant_sessions ADD COLUMN config_json TEXT NOT NULL DEFAULT '{}'`}, {Table: "participant_segments", Name: "end_ts", SQL: `ALTER TABLE participant_segments ADD COLUMN end_ts INTEGER NOT NULL DEFAULT 0`}, {Table: "participant_segments", Name: "speaker", SQL: `ALTER TABLE participant_segments ADD COLUMN speaker TEXT NOT NULL DEFAULT ''`}, {Table: "participant_segments", Name: "text", SQL: `ALTER TABLE participant_segments ADD COLUMN text TEXT NOT NULL DEFAULT ''`}, {Table: "participant_segments", Name: "model", SQL: `ALTER TABLE participant_segments ADD COLUMN model TEXT NOT NULL DEFAULT ''`}, {Table: "participant_segments", Name: "latency_ms", SQL: `ALTER TABLE participant_segments ADD COLUMN latency_ms INTEGER NOT NULL DEFAULT 0`}, {Table: "participant_segments", Name: "committed_at", SQL: `ALTER TABLE participant_segments ADD COLUMN committed_at INTEGER NOT NULL DEFAULT 0`}, {Table: "participant_segments", Name: "status", SQL: `ALTER TABLE participant_segments ADD COLUMN status TEXT NOT NULL DEFAULT 'final'`}, {Table: "participant_events", Name: "segment_id", SQL: `ALTER TABLE participant_events ADD COLUMN segment_id INTEGER NOT NULL DEFAULT 0`}, {Table: "participant_events", Name: "payload_json", SQL: `ALTER TABLE participant_events ADD COLUMN payload_json TEXT NOT NULL DEFAULT ''`}, {Table: "participant_room_state", Name: "summary_text", SQL: `ALTER TABLE participant_room_state ADD COLUMN summary_text TEXT NOT NULL DEFAULT ''`}, {Table: "participant_room_state", Name: "entities_json", SQL: `ALTER TABLE participant_room_state ADD COLUMN entities_json TEXT NOT NULL DEFAULT '[]'`}, {Table: "participant_room_state", Name: "topic_timeline_json", SQL: `ALTER TABLE participant_room_state ADD COLUMN topic_timeline_json TEXT NOT NULL DEFAULT '[]'`}}
	tableColumns, err := s.tableColumnSet("participant_sessions", "participant_segments", "participant_events", "participant_room_state")
	if err != nil {
		return err
	}
	for _, col := range columns {
		if tableColumns[col.Table][col.Name] {
			continue
		}
		if _, err := s.db.Exec(col.SQL); err != nil {
			return err
		}
	}
	_, _ = s.db.Exec(`UPDATE participant_sessions SET config_json = '{}' WHERE trim(config_json) = ''`)
	_, _ = s.db.Exec(`UPDATE participant_segments SET committed_at = start_ts WHERE committed_at = 0`)
	_, _ = s.db.Exec(`UPDATE participant_segments SET status = 'final' WHERE trim(status) = ''`)
	_, _ = s.db.Exec(`UPDATE participant_events SET payload_json = '{}' WHERE trim(payload_json) = ''`)
	_, _ = s.db.Exec(`UPDATE participant_room_state SET entities_json = '[]' WHERE trim(entities_json) = ''`)
	_, _ = s.db.Exec(`UPDATE participant_room_state SET topic_timeline_json = '[]' WHERE trim(topic_timeline_json) = ''`)
	return nil
}

func (s *Store) tableColumnSet(tables ...string) (map[string]map[string]bool, error) {
	tableColumns := make(map[string]map[string]bool, len(tables))
	for _, table := range tables {
		cols, err := s.tableColumnNames(table)
		if err != nil {
			return nil, err
		}
		existing := make(map[string]bool, len(cols))
		for _, c := range cols {
			existing[c] = true
		}
		tableColumns[table] = existing
	}
	return tableColumns, nil
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = time.Now().UTC().MarshalBinary()
	seed := sha256.Sum256([]byte(strconv.FormatInt(time.Now().UnixNano(), 10)))
	copy(b, seed[:])
	return hex.EncodeToString(b)
}

func (s *Store) SetAppState(key, value string) error {
	cleanKey := strings.TrimSpace(key)
	if cleanKey == "" {
		return errors.New("app state key is required")
	}
	_, err := s.db.Exec(`INSERT INTO app_state (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, cleanKey, strings.TrimSpace(value))
	return err
}

func (s *Store) AppState(key string) (string, error) {
	cleanKey := strings.TrimSpace(key)
	if cleanKey == "" {
		return "", errors.New("app state key is required")
	}
	var value string
	if err := s.db.QueryRow(`SELECT value FROM app_state WHERE key = ?`, cleanKey).Scan(&value); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(value), nil
}

func normalizeWorkspaceChatModel(raw string) string {
	alias := modelprofile.ResolveAlias(raw, modelprofile.AliasLocal)
	if alias == "" {
		return modelprofile.AliasLocal
	}
	return alias
}

func normalizeWorkspaceChatModelReasoningEffort(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", modelprofile.ReasoningNone:
		return modelprofile.ReasoningNone
	case modelprofile.ReasoningLow:
		return modelprofile.ReasoningLow
	case modelprofile.ReasoningMedium:
		return modelprofile.ReasoningMedium
	case modelprofile.ReasoningHigh:
		return modelprofile.ReasoningHigh
	case modelprofile.ReasoningExtraHigh, "extra_high":
		return modelprofile.ReasoningExtraHigh
	default:
		return ""
	}
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

const pbkdfIter = 600000

func hashPassword(password, salt string) string {
	data := []byte(password + ":" + salt)
	sum := sha256.Sum256(data)
	for i := 0; i < pbkdfIter/10000; i++ {
		next := sha256.Sum256(sum[:])
		sum = next
	}
	return hex.EncodeToString(sum[:])
}

func (s *Store) HasAdminPassword() bool {
	var c int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM admin`).Scan(&c)
	return c > 0
}

func (s *Store) SetAdminPassword(password string) error {
	if len(password) < 8 {
		return errors.New("password must be at least 8 characters")
	}
	salt := randomHex(16)
	h := hashPassword(password, salt)
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM admin`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM auth_sessions`); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO admin (id,pw_hash,pw_salt) VALUES (1,?,?)`, h, salt); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) VerifyAdminPassword(password string) bool {
	var h, salt string
	if err := s.db.QueryRow(`SELECT pw_hash,pw_salt FROM admin WHERE id=1`).Scan(&h, &salt); err != nil {
		return false
	}
	cand := hashPassword(password, salt)
	return hmac.Equal([]byte(cand), []byte(h))
}

func (s *Store) AddAuthSession(token string) error {
	if token == "" {
		return errors.New("empty token")
	}
	_, err := s.db.Exec(`INSERT OR REPLACE INTO auth_sessions (token,created_at) VALUES (?,?)`, token, time.Now().Unix())
	return err
}

func (s *Store) HasAuthSession(token string) bool {
	if token == "" {
		return false
	}
	var one int
	if err := s.db.QueryRow(`SELECT 1 FROM auth_sessions WHERE token=?`, token).Scan(&one); err != nil {
		return false
	}
	return true
}

func (s *Store) DeleteAuthSession(token string) error {
	if token == "" {
		return nil
	}
	_, err := s.db.Exec(`DELETE FROM auth_sessions WHERE token=?`, token)
	return err
}
