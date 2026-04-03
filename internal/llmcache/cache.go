package llmcache

import (
	"database/sql"
	"encoding/json"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS llm_cache (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    cache_key TEXT NOT NULL UNIQUE,
    content TEXT NOT NULL DEFAULT '',
    tool_calls_json TEXT NOT NULL DEFAULT '[]',
    finish_reason TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    hit_count INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    last_hit_at INTEGER NOT NULL DEFAULT 0,
    invalidated INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_llm_cache_key ON llm_cache(cache_key);
`

const maxLoadEntries = 50000

// ToolCall mirrors the OpenAI tool call structure for serialization.
type ToolCall struct {
	ID       string       `json:"id,omitempty"`
	Type     string       `json:"type,omitempty"`
	Function FunctionCall `json:"function"`
}

// FunctionCall holds the function name and JSON-encoded arguments.
type FunctionCall struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// Entry is a cached LLM response.
type Entry struct {
	ID            int64
	CacheKey      string
	Content       string
	ToolCallsJSON string
	FinishReason  string
	Model         string
	HitCount      int64
	CreatedAt     int64
	LastHitAt     int64
}

// CacheStats reports aggregate cache metrics.
type CacheStats struct {
	Total       int64
	Invalidated int64
	TotalHits   int64
}

// Cache provides an in-memory + SQLite LLM response cache.
type Cache struct {
	db      *sql.DB
	entries sync.Map
	hits    chan string
	done    chan struct{}
}

// New opens the SQLite database at dbPath, creates the schema, and loads
// existing entries into memory.
func New(dbPath string) (*Cache, error) {
	db, err := sql.Open("sqlite", dbPath+"?_journal=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	c := &Cache{
		db:   db,
		hits: make(chan string, 256),
		done: make(chan struct{}),
	}
	c.cleanup()
	if err := c.loadAll(); err != nil {
		db.Close()
		return nil, err
	}
	go c.drainHits()
	return c, nil
}

// Close shuts down the background goroutine and closes the database.
func (c *Cache) Close() error {
	if c == nil {
		return nil
	}
	close(c.hits)
	<-c.done
	return c.db.Close()
}

// Lookup returns a cached entry for the given key. The hit count is
// updated asynchronously so the call is non-blocking.
func (c *Cache) Lookup(key string) (*Entry, bool) {
	if c == nil {
		return nil, false
	}
	v, ok := c.entries.Load(key)
	if !ok {
		return nil, false
	}
	entry := v.(*Entry)
	select {
	case c.hits <- key:
	default:
	}
	return entry, true
}

// Store persists a new cache entry to SQLite and the in-memory map.
// Preserves hit_count if the key already exists.
func (c *Cache) Store(key, content string, toolCalls []ToolCall, finishReason, model string) error {
	if c == nil {
		return nil
	}
	tcJSON, _ := json.Marshal(toolCalls)
	now := time.Now().Unix()
	_, err := c.db.Exec(
		`INSERT INTO llm_cache (cache_key, content, tool_calls_json, finish_reason, model, hit_count, created_at, last_hit_at, invalidated)
		 VALUES (?, ?, ?, ?, ?, 0, ?, 0, 0)
		 ON CONFLICT(cache_key) DO UPDATE SET
		   content=excluded.content, tool_calls_json=excluded.tool_calls_json,
		   finish_reason=excluded.finish_reason, model=excluded.model,
		   hit_count=llm_cache.hit_count, invalidated=0`,
		key, content, string(tcJSON), finishReason, model, now,
	)
	if err != nil {
		return err
	}
	c.entries.Store(key, &Entry{
		CacheKey:      key,
		Content:       content,
		ToolCallsJSON: string(tcJSON),
		FinishReason:  finishReason,
		Model:         model,
		CreatedAt:     now,
	})
	return nil
}

// ParseToolCalls deserializes the cached tool calls JSON.
func (e *Entry) ParseToolCalls() []ToolCall {
	if e == nil || strings.TrimSpace(e.ToolCallsJSON) == "" || e.ToolCallsJSON == "[]" {
		return nil
	}
	var calls []ToolCall
	if json.Unmarshal([]byte(e.ToolCallsJSON), &calls) != nil {
		return nil
	}
	return calls
}

// Invalidate marks a single entry as invalidated.
func (c *Cache) Invalidate(key string) error {
	if c == nil {
		return nil
	}
	c.entries.Delete(key)
	_, err := c.db.Exec(`UPDATE llm_cache SET invalidated=1 WHERE cache_key=?`, key)
	return err
}

// InvalidateRecent marks the N most recently created entries as invalidated
// in a single atomic UPDATE.
func (c *Cache) InvalidateRecent(n int) (int, error) {
	if c == nil || n <= 0 {
		return 0, nil
	}
	res, err := c.db.Exec(
		`UPDATE llm_cache SET invalidated=1 WHERE id IN (SELECT id FROM llm_cache WHERE invalidated=0 ORDER BY created_at DESC LIMIT ?)`, n,
	)
	if err != nil {
		return 0, err
	}
	affected, _ := res.RowsAffected()
	// Reload memory from DB to stay consistent.
	if affected > 0 {
		c.entries.Range(func(key, _ any) bool {
			c.entries.Delete(key)
			return true
		})
		if err := c.loadAll(); err != nil {
			return int(affected), err
		}
	}
	return int(affected), nil
}

// InvalidateAll marks all entries as invalidated and clears the in-memory map.
func (c *Cache) InvalidateAll() error {
	if c == nil {
		return nil
	}
	c.entries.Range(func(key, _ any) bool {
		c.entries.Delete(key)
		return true
	})
	_, err := c.db.Exec(`UPDATE llm_cache SET invalidated=1 WHERE invalidated=0`)
	return err
}

// Stats returns aggregate cache metrics.
func (c *Cache) Stats() CacheStats {
	if c == nil {
		return CacheStats{}
	}
	var stats CacheStats
	c.db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(CASE WHEN invalidated=1 THEN 1 ELSE 0 END),0), COALESCE(SUM(hit_count),0) FROM llm_cache`).
		Scan(&stats.Total, &stats.Invalidated, &stats.TotalHits)
	return stats
}

func (c *Cache) loadAll() error {
	rows, err := c.db.Query(
		`SELECT id, cache_key, content, tool_calls_json, finish_reason, model, hit_count, created_at, last_hit_at
		 FROM llm_cache WHERE invalidated=0 ORDER BY created_at DESC LIMIT ?`, maxLoadEntries,
	)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.CacheKey, &e.Content, &e.ToolCallsJSON, &e.FinishReason, &e.Model, &e.HitCount, &e.CreatedAt, &e.LastHitAt); err != nil {
			continue
		}
		c.entries.Store(e.CacheKey, &e)
	}
	return nil
}

const cleanupMaxAgeDays = 90

func (c *Cache) cleanup() {
	cutoff := time.Now().AddDate(0, 0, -cleanupMaxAgeDays).Unix()
	c.db.Exec(`DELETE FROM llm_cache WHERE created_at < ?`, cutoff)
}

func (c *Cache) drainHits() {
	defer close(c.done)
	for key := range c.hits {
		now := time.Now().Unix()
		c.db.Exec(`UPDATE llm_cache SET hit_count=hit_count+1, last_hit_at=? WHERE cache_key=?`, now, key)
	}
}
