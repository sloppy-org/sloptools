package backend

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestToolResultTextReadsFirstTextContent(t *testing.T) {
	got := toolResultText(map[string]interface{}{
		"content": []interface{}{
			map[string]interface{}{"type": "text", "text": "tu4u fetch failed: fetch failed with HTTP 404"},
		},
		"isError": true,
	})
	if got != "tu4u fetch failed: fetch failed with HTTP 404" {
		t.Fatalf("toolResultText = %q", got)
	}
}

func TestMCPClientCallReturnsIsErrorAsError(t *testing.T) {
	c := &MCPClient{
		enc:    json.NewEncoder(io.Discard),
		dec:    json.NewDecoder(strings.NewReader(`{"jsonrpc":"2.0","id":2,"result":{"isError":true,"content":[{"type":"text","text":"tu4u fetch failed: fetch failed with HTTP 404"}]}}`)),
		nextID: 2,
	}
	text, err := c.Call("helpy_tu4u", map[string]interface{}{"action": "fetch"})
	if err == nil {
		t.Fatalf("Call error = nil, text=%q", text)
	}
	if !strings.Contains(err.Error(), "HTTP 404") {
		t.Fatalf("Call error = %q", err.Error())
	}
}
