package backend

import (
	"fmt"
	"strings"
	"testing"
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
		lastResult = callToolSafe(toolMap, tool, nil, tr, nil, nil)
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
		callToolSafe(toolMap, tool, nil, tr, nil, nil)
	}
	// Confirm circuit is open.
	result := callToolSafe(toolMap, tool, nil, tr, nil, nil)
	if !strings.Contains(result, "circuit open") {
		t.Fatalf("expected circuit-open on next call, got: %s", result)
	}
}

func TestCallToolSafeNilTrackerDoesNotPanic(t *testing.T) {
	toolMap := map[string]*MCPClient{}
	// nil tracker: callToolSafe must not panic and must return the normal error string.
	result := callToolSafe(toolMap, "missing_tool", nil, nil, nil, nil)
	if !strings.Contains(result, "unknown tool") {
		t.Fatalf("expected unknown-tool error, got: %s", result)
	}
}

// --- per-tool quota integration ---

func TestCallToolSafeQuotaBlocksAfterCap(t *testing.T) {
	tr := newToolFailureTracker()
	toolMap := map[string]*MCPClient{} // every call is unknown-tool error
	usage := newToolUsage(map[string]int{"web_search": 2})

	for i := 0; i < 2; i++ {
		result := callToolSafe(toolMap, "web_search", nil, tr, usage, nil)
		if strings.Contains(result, "quota exceeded") {
			t.Fatalf("quota must not block within cap (i=%d): %s", i, result)
		}
	}
	result := callToolSafe(toolMap, "web_search", nil, tr, usage, nil)
	if !strings.Contains(result, "quota exceeded") {
		t.Fatalf("expected quota-exceeded message past cap, got: %s", result)
	}
}

func TestCallToolSafeQuotaDoesNotTripGlobalBreaker(t *testing.T) {
	tr := newToolFailureTracker()
	usage := newToolUsage(map[string]int{"helpy_tu4u": 1})
	toolMap := map[string]*MCPClient{}

	callToolSafe(toolMap, "helpy_tu4u", nil, tr, usage, nil)
	result := callToolSafe(toolMap, "helpy_tu4u", nil, tr, usage, nil)

	if !strings.Contains(result, "quota exceeded") {
		t.Fatalf("expected quota-exceeded message, got: %s", result)
	}
	if tr.globalTripped() {
		t.Fatalf("quota exhaustion should let the model write a final answer")
	}
}

func TestCallToolSafePromotesToSessionBroken(t *testing.T) {
	tr := newToolFailureTracker()
	toolMap := map[string]*MCPClient{}
	broken := NewBrokenTools(1)

	for i := 0; i < perToolBreakThreshold; i++ {
		callToolSafe(toolMap, "helpy_tugonline", nil, tr, nil, broken)
	}
	if !broken.IsBroken("helpy_tugonline") {
		t.Fatalf("session registry must mark tool broken after per-loop circuit opens")
	}
}

func TestBrokenToolsFilterAllowList(t *testing.T) {
	broken := NewBrokenTools(1)
	broken.Report("helpy_tugonline", "unknown tool")
	original := []string{"web_search", "helpy_tugonline", "helpy_zotero"}
	filtered := broken.FilterAllowList(original)
	if len(filtered) != 2 {
		t.Fatalf("expected 2 surviving tools, got %v", filtered)
	}
	for _, name := range filtered {
		if name == "helpy_tugonline" {
			t.Fatalf("broken tool leaked through filter: %v", filtered)
		}
	}
	if len(original) != 3 || original[1] != "helpy_tugonline" {
		t.Fatalf("filter mutated caller slice: %v", original)
	}
}
