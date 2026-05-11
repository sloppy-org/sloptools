package backend

import (
	"strings"
	"testing"
	"fmt"
)

// --- toolFailureTracker unit tests ---

func TestTrackerPerToolCircuitOpensAtThreshold(t *testing.T) {
	tr := newToolFailureTracker()
	const tool = "helpy_tugonline"
	const class = "unknown tool"

	for i := 1; i < perToolBreakThreshold; i++ {
		if tr.recordFailure(tool, class) {
			t.Fatalf("circuit should not open at failure %d (threshold=%d)", i, perToolBreakThreshold)
		}
	}
	if !tr.recordFailure(tool, class) {
		t.Fatalf("circuit should open at failure %d (threshold=%d)", perToolBreakThreshold, perToolBreakThreshold)
	}
}

func TestTrackerResetsConsecutiveOnSuccess(t *testing.T) {
	tr := newToolFailureTracker()
	const tool = "helpy_tu4u"

	tr.recordFailure(tool, "tu4u search failed")
	tr.recordFailure(tool, "tu4u search failed")
	tr.recordSuccess(tool)

	if tr.consecutive[tool] != 0 {
		t.Fatalf("consecutive should be 0 after success, got %d", tr.consecutive[tool])
	}
	if tr.globalTotal != 0 {
		t.Fatalf("globalTotal should be 0 after success, got %d", tr.globalTotal)
	}
	// Two more failures should not trip the per-tool circuit.
	for i := 1; i < perToolBreakThreshold; i++ {
		if tr.recordFailure(tool, "tu4u search failed") {
			t.Fatalf("circuit should not open at failure %d after reset", i)
		}
	}
}

func TestTrackerClassChangeResetsPerToolCounter(t *testing.T) {
	tr := newToolFailureTracker()
	const tool = "helpy_tu4u"

	for i := 0; i < perToolBreakThreshold-1; i++ {
		tr.recordFailure(tool, "class-a")
	}
	// Different error class resets the counter — should not open.
	if tr.recordFailure(tool, "class-b") {
		t.Fatalf("class change must reset consecutive counter: should not open circuit on first occurrence of new class")
	}
	if tr.consecutive[tool] != 1 {
		t.Fatalf("expected consecutive=1 after class change, got %d", tr.consecutive[tool])
	}
}

func TestTrackerGlobalThresholdTrips(t *testing.T) {
	tr := newToolFailureTracker()
	tripped := false
	for i := 0; i < globalBreakThreshold+1; i++ {
		tr.recordFailure(fmt.Sprintf("tool_%d", i), "some error")
		if tr.globalTripped() {
			tripped = true
			break
		}
	}
	if !tripped {
		t.Fatalf("global circuit should trip after %d consecutive failures", globalBreakThreshold)
	}
}

func TestTrackerGlobalNotTrippedBeforeThreshold(t *testing.T) {
	tr := newToolFailureTracker()
	for i := 0; i < globalBreakThreshold-1; i++ {
		tr.recordFailure(fmt.Sprintf("tool_%d", i), "error")
	}
	if tr.globalTripped() {
		t.Fatalf("global circuit must not trip before reaching threshold %d", globalBreakThreshold)
	}
}

// --- callToolSafe circuit-breaker integration ---

func TestCallToolSafeOpensCircuitAfterThreshold(t *testing.T) {
	tr := newToolFailureTracker()
	toolMap := map[string]*MCPClient{} // empty: every call is unknown-tool error

	const tool = "helpy_tugonline"
	var lastResult string
	for i := 0; i <= perToolBreakThreshold+1; i++ {
		lastResult = callToolSafe(toolMap, tool, nil, tr)
	}
	if !strings.Contains(lastResult, "circuit open") {
		t.Fatalf("expected circuit-open message after %d failures, got: %s", perToolBreakThreshold, lastResult)
	}
}

func TestCallToolSafeCircuitOpenSkipsClient(t *testing.T) {
	tr := newToolFailureTracker()
	toolMap := map[string]*MCPClient{}
	const tool = "helpy_tu4u"

	// Exhaust the threshold.
	for i := 0; i < perToolBreakThreshold; i++ {
		callToolSafe(toolMap, tool, nil, tr)
	}
	// Confirm circuit is open.
	result := callToolSafe(toolMap, tool, nil, tr)
	if !strings.Contains(result, "circuit open") {
		t.Fatalf("expected circuit-open on next call, got: %s", result)
	}
}

func TestCallToolSafeNilTrackerDoesNotPanic(t *testing.T) {
	toolMap := map[string]*MCPClient{}
	// nil tracker: callToolSafe must not panic and must return the normal error string.
	result := callToolSafe(toolMap, "missing_tool", nil, nil)
	if !strings.Contains(result, "unknown tool") {
		t.Fatalf("expected unknown-tool error, got: %s", result)
	}
}
