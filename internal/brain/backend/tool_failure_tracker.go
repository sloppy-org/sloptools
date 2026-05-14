package backend

import "strings"

const (
	// perToolBreakThreshold opens the circuit for a single tool after this many
	// consecutive identical failures, preventing the model from retrying a broken
	// tool indefinitely within one run.
	perToolBreakThreshold = 3
	// globalBreakThreshold aborts the agent loop after this many total consecutive
	// tool errors (across all tools), capping runaway failure cascades.
	globalBreakThreshold = 6
)

// toolFailureTracker counts consecutive identical failures per tool within one
// agent-loop run. It is not shared across runs.
type toolFailureTracker struct {
	consecutive map[string]int
	lastClass   map[string]string
	globalTotal int
}

func newToolFailureTracker() *toolFailureTracker {
	return &toolFailureTracker{
		consecutive: make(map[string]int),
		lastClass:   make(map[string]string),
	}
}

// recordSuccess resets the per-tool and global counters for name.
func (t *toolFailureTracker) recordSuccess(name string) {
	t.consecutive[name] = 0
	t.lastClass[name] = ""
	t.globalTotal = 0
}

// recordFailure increments the consecutive counter for name+class and returns
// true when the per-tool circuit-break threshold is reached.
func (t *toolFailureTracker) recordFailure(name, class string) bool {
	if t.lastClass[name] == class {
		t.consecutive[name]++
	} else {
		t.consecutive[name] = 1
		t.lastClass[name] = class
	}
	t.globalTotal++
	return t.consecutive[name] >= perToolBreakThreshold
}

// recordTerminalFailure marks an unrecoverable tool-loop failure. Quota
// exhaustion is not fixed by retrying, so the agent loop should stop after
// returning the explanatory tool response once.
func (t *toolFailureTracker) recordTerminalFailure(name, class string) {
	t.recordFailure(name, class)
	t.globalTotal = globalBreakThreshold
}

// globalTripped returns true when total consecutive failures across all tools
// exceed the global threshold.
func (t *toolFailureTracker) globalTripped() bool {
	return t.globalTotal >= globalBreakThreshold
}

// errorClassShort extracts the short error prefix used as a circuit-breaker key
// (head before first ':' or '\n', capped at 80 bytes).
func errorClassShort(msg string) string {
	if i := strings.IndexAny(msg, ":\n"); i > 0 && i <= 80 {
		return strings.TrimSpace(msg[:i])
	}
	if len(msg) > 80 {
		return msg[:80]
	}
	return msg
}
