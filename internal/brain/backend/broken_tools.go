package backend

import "sync"

// BrokenTools is a concurrent-safe registry of MCP tools that have failed
// enough times in one brain session that subsequent agent loops should not
// be offered them at all. The orchestrator owns one instance per session
// and threads it through every backend.Request.
//
// The registry exists because the per-agent-loop circuit breaker
// (toolFailureTracker) resets each time a new scout starts. Without a
// session-wide layer, a tool that is genuinely broken (catalog ↔ dispatcher
// drift, network down, credentials expired) costs each of N sequential
// scouts 3 retries — that is what produced 1051 helpy_tugonline failures in
// one night.
type BrokenTools struct {
	mu        sync.RWMutex
	broken    map[string]string // tool name -> error class that broke it
	threshold int               // strikes to register; 0 means register on first strike
	strikes   map[string]int
}

// NewBrokenTools returns a registry that marks a tool broken after
// `threshold` distinct agent-loop reports. threshold<=0 means one strike.
func NewBrokenTools(threshold int) *BrokenTools {
	if threshold < 1 {
		threshold = 1
	}
	return &BrokenTools{
		broken:    make(map[string]string),
		strikes:   make(map[string]int),
		threshold: threshold,
	}
}

// Report records that an agent loop hit a broken tool. Returns true when
// the call promoted the tool to the broken set.
func (b *BrokenTools) Report(name, class string) bool {
	if b == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, already := b.broken[name]; already {
		return false
	}
	b.strikes[name]++
	if b.strikes[name] >= b.threshold {
		b.broken[name] = class
		return true
	}
	return false
}

// IsBroken reports whether the tool has been marked broken.
func (b *BrokenTools) IsBroken(name string) bool {
	if b == nil {
		return false
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	_, ok := b.broken[name]
	return ok
}

// Snapshot returns a copy of the current broken map (name -> error class).
func (b *BrokenTools) Snapshot() map[string]string {
	if b == nil {
		return nil
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make(map[string]string, len(b.broken))
	for k, v := range b.broken {
		out[k] = v
	}
	return out
}

// FilterAllowList returns a copy of allow with broken tools removed. The
// input slice is never mutated, so the caller's stage-config allowlist
// stays intact for the next scout.
func (b *BrokenTools) FilterAllowList(allow []string) []string {
	if b == nil || len(allow) == 0 {
		return allow
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	if len(b.broken) == 0 {
		return allow
	}
	kept := make([]string, 0, len(allow))
	for _, name := range allow {
		if _, bad := b.broken[name]; bad {
			continue
		}
		kept = append(kept, name)
	}
	return kept
}
