package llmcache

import (
	"path/filepath"
	"testing"
	"time"
)

func testCache(t *testing.T) *Cache {
	t.Helper()
	c, err := New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestStoreAndLookup(t *testing.T) {
	c := testCache(t)
	calls := []ToolCall{{ID: "c1", Type: "function", Function: FunctionCall{Name: "calendar_events", Arguments: `{"days":1}`}}}
	if err := c.Store("key1", "", calls, "tool_calls", "qwen"); err != nil {
		t.Fatalf("Store: %v", err)
	}
	entry, ok := c.Lookup("key1")
	if !ok {
		t.Fatal("Lookup miss for stored key")
	}
	if entry.Content != "" {
		t.Fatalf("Content = %q, want empty", entry.Content)
	}
	parsed := entry.ParseToolCalls()
	if len(parsed) != 1 || parsed[0].Function.Name != "calendar_events" {
		t.Fatalf("ParseToolCalls = %+v", parsed)
	}
}

func TestLookupMiss(t *testing.T) {
	c := testCache(t)
	if _, ok := c.Lookup("nonexistent"); ok {
		t.Fatal("expected miss")
	}
}

func TestPersistence(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "persist.db")
	c1, err := New(dbPath)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c1.Store("pkey", "hello", nil, "stop", "qwen")
	c1.Close()

	c2, err := New(dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer c2.Close()
	entry, ok := c2.Lookup("pkey")
	if !ok {
		t.Fatal("entry not persisted")
	}
	if entry.Content != "hello" {
		t.Fatalf("Content = %q, want hello", entry.Content)
	}
}

func TestInvalidate(t *testing.T) {
	c := testCache(t)
	c.Store("inv1", "data", nil, "stop", "qwen")
	if _, ok := c.Lookup("inv1"); !ok {
		t.Fatal("expected hit before invalidate")
	}
	c.Invalidate("inv1")
	if _, ok := c.Lookup("inv1"); ok {
		t.Fatal("expected miss after invalidate")
	}
}

func TestInvalidateRecent(t *testing.T) {
	c := testCache(t)
	// Insert with increasing timestamps by sleeping 1s between entries.
	// Use direct SQL to set distinct created_at values.
	now := time.Now().Unix()
	c.db.Exec(`INSERT INTO llm_cache (cache_key, content, finish_reason, model, created_at) VALUES (?, 'a', 'stop', 'qwen', ?)`, "r1", now-2)
	c.entries.Store("r1", &Entry{CacheKey: "r1", Content: "a", CreatedAt: now - 2})
	c.db.Exec(`INSERT INTO llm_cache (cache_key, content, finish_reason, model, created_at) VALUES (?, 'b', 'stop', 'qwen', ?)`, "r2", now-1)
	c.entries.Store("r2", &Entry{CacheKey: "r2", Content: "b", CreatedAt: now - 1})
	c.db.Exec(`INSERT INTO llm_cache (cache_key, content, finish_reason, model, created_at) VALUES (?, 'c', 'stop', 'qwen', ?)`, "r3", now)
	c.entries.Store("r3", &Entry{CacheKey: "r3", Content: "c", CreatedAt: now})

	n, err := c.InvalidateRecent(1)
	if err != nil {
		t.Fatalf("InvalidateRecent: %v", err)
	}
	if n != 1 {
		t.Fatalf("invalidated %d, want 1", n)
	}
	if _, ok := c.Lookup("r3"); ok {
		t.Fatal("r3 should be invalidated")
	}
	if _, ok := c.Lookup("r1"); !ok {
		t.Fatal("r1 should still be valid")
	}
}

func TestInvalidateAll(t *testing.T) {
	c := testCache(t)
	c.Store("a1", "x", nil, "stop", "qwen")
	c.Store("a2", "y", nil, "stop", "qwen")
	c.InvalidateAll()
	if _, ok := c.Lookup("a1"); ok {
		t.Fatal("a1 should be invalidated")
	}
	if _, ok := c.Lookup("a2"); ok {
		t.Fatal("a2 should be invalidated")
	}
}

func TestBuildKeyDeterministic(t *testing.T) {
	msgs := []map[string]any{{"role": "user", "content": "hello"}}
	k1 := BuildKey(msgs, nil, "qwen", false)
	k2 := BuildKey(msgs, nil, "qwen", false)
	if k1 != k2 {
		t.Fatalf("non-deterministic keys: %q != %q", k1, k2)
	}
}

func TestBuildKeyDifferentModel(t *testing.T) {
	msgs := []map[string]any{{"role": "user", "content": "hello"}}
	k1 := BuildKey(msgs, nil, "qwen", false)
	k2 := BuildKey(msgs, nil, "llama", false)
	if k1 == k2 {
		t.Fatal("different models should produce different keys")
	}
}

func TestBuildKeyTimeNormalization(t *testing.T) {
	msgs1 := []map[string]any{{"role": "user", "content": "Current UTC time: 2026-03-26T14:30:00Z\nwelche Termine morgen?"}}
	msgs2 := []map[string]any{{"role": "user", "content": "Current UTC time: 2026-03-26T19:45:33Z\nwelche Termine morgen?"}}
	k1 := BuildKey(msgs1, nil, "qwen", false)
	k2 := BuildKey(msgs2, nil, "qwen", false)
	if k1 != k2 {
		t.Fatal("same-day different-time should produce same key")
	}
}

func TestBuildKeyDifferentDay(t *testing.T) {
	msgs1 := []map[string]any{{"role": "user", "content": "Current UTC time: 2026-03-26T14:30:00Z\nwelche Termine morgen?"}}
	msgs2 := []map[string]any{{"role": "user", "content": "Current UTC time: 2026-03-27T14:30:00Z\nwelche Termine morgen?"}}
	k1 := BuildKey(msgs1, nil, "qwen", false)
	k2 := BuildKey(msgs2, nil, "qwen", false)
	if k1 == k2 {
		t.Fatal("different days should produce different keys")
	}
}

func TestContainsToolResults(t *testing.T) {
	noTools := []map[string]any{
		{"role": "system", "content": "you are helpful"},
		{"role": "user", "content": "hello"},
	}
	if ContainsToolResults(noTools) {
		t.Fatal("expected false for no tool results")
	}
	withTools := []map[string]any{
		{"role": "system", "content": "you are helpful"},
		{"role": "user", "content": "hello"},
		{"role": "assistant", "content": ""},
		{"role": "tool", "tool_call_id": "c1", "content": "result"},
	}
	if !ContainsToolResults(withTools) {
		t.Fatal("expected true for tool results")
	}
}

func TestRunningTasksStripped(t *testing.T) {
	msgs1 := []map[string]any{{"role": "user", "content": "Running tasks: 0 active, 0 queued\nshow calendar"}}
	msgs2 := []map[string]any{{"role": "user", "content": "Running tasks: 3 active, 1 queued\nshow calendar"}}
	k1 := BuildKey(msgs1, nil, "qwen", false)
	k2 := BuildKey(msgs2, nil, "qwen", false)
	if k1 != k2 {
		t.Fatal("different running tasks should produce same key")
	}
}

func TestBuildIntentKeyStripsHistory(t *testing.T) {
	msgs1 := []map[string]any{
		{"role": "system", "content": "you are helpful"},
		{"role": "user", "content": "Current UTC time: 2026-03-26\nRecent conversation:\n- USER: hello\n- ASSISTANT: hi\n\nUser request:\nwelche termine morgen?"},
	}
	msgs2 := []map[string]any{
		{"role": "system", "content": "you are helpful"},
		{"role": "user", "content": "Current UTC time: 2026-03-26\nRecent conversation:\n- USER: show my mail\n- ASSISTANT: here are your emails\n\nUser request:\nwelche termine morgen?"},
	}
	k1 := BuildIntentKey(msgs1, "qwen")
	k2 := BuildIntentKey(msgs2, "qwen")
	if k1 != k2 {
		t.Fatal("same intent with different history should produce same canonical key")
	}
	// But full keys should differ
	fk1 := BuildKey(msgs1, nil, "qwen", false)
	fk2 := BuildKey(msgs2, nil, "qwen", false)
	if fk1 == fk2 {
		t.Fatal("full keys should differ with different history")
	}
}

func TestIsSelfContainedQuery(t *testing.T) {
	self := []string{
		"welche termine hab ich morgen?",
		"show my calendar",
		"zeig meine mails",
		"what meetings do I have?",
	}
	for _, q := range self {
		if !IsSelfContainedQuery(q) {
			t.Fatalf("expected self-contained: %q", q)
		}
	}
	followUp := []string{
		"and tomorrow?",
		"und morgen?",
		"also next week",
		"what about Friday?",
		"it please",
		"das auch",
	}
	for _, q := range followUp {
		if IsSelfContainedQuery(q) {
			t.Fatalf("expected follow-up: %q", q)
		}
	}
}

func TestBuildIntentKeyRecentMessagesBlock(t *testing.T) {
	msgs1 := []map[string]any{
		{"role": "system", "content": "prompt"},
		{"role": "user", "content": "Recent messages:\nUSER: old question\nASSISTANT: old answer\nshow calendar"},
	}
	msgs2 := []map[string]any{
		{"role": "system", "content": "prompt"},
		{"role": "user", "content": "Recent messages:\nUSER: different thing\nASSISTANT: different reply\nshow calendar"},
	}
	k1 := BuildIntentKey(msgs1, "qwen")
	k2 := BuildIntentKey(msgs2, "qwen")
	if k1 != k2 {
		t.Fatal("same request with different Recent messages block should produce same canonical key")
	}
}

func TestStats(t *testing.T) {
	c := testCache(t)
	c.Store("s1", "x", nil, "stop", "qwen")
	c.Store("s2", "y", nil, "stop", "qwen")
	c.Invalidate("s2")
	stats := c.Stats()
	if stats.Total != 2 {
		t.Fatalf("Total = %d, want 2", stats.Total)
	}
	if stats.Invalidated != 1 {
		t.Fatalf("Invalidated = %d, want 1", stats.Invalidated)
	}
}
