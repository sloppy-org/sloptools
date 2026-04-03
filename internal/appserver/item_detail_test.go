package appserver

import (
	"strings"
	"testing"
)

func TestExtractItemDetailPrefersReadableFields(t *testing.T) {
	item := map[string]interface{}{
		"type":    "exec_command",
		"summary": "  running   go test   ./internal/web  ",
		"detail":  "ignored because summary wins",
	}

	got := extractItemDetail(item)
	if got != "ignored because summary wins" {
		t.Fatalf("expected detail field to win, got %q", got)
	}
}

func TestExtractItemDetailFormatsToolArguments(t *testing.T) {
	item := map[string]interface{}{
		"type":      "tool_call",
		"tool_name": "exec_command",
		"arguments": map[string]interface{}{
			"cmd": "go test ./internal/web -run TestStop",
		},
	}

	got := extractItemDetail(item)
	if !strings.Contains(got, "exec_command") {
		t.Fatalf("expected tool name in detail, got %q", got)
	}
	if !strings.Contains(got, "TestStop") {
		t.Fatalf("expected arguments in detail, got %q", got)
	}
}

func TestExtractItemDetailFallsBackToSnapshot(t *testing.T) {
	item := map[string]interface{}{
		"type": "reasoning",
		"foo":  "bar",
	}

	got := extractItemDetail(item)
	if !strings.Contains(got, "foo") || !strings.Contains(got, "bar") {
		t.Fatalf("expected fallback snapshot detail, got %q", got)
	}
}
