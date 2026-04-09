package mcp

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/sloppy-org/sloptools/internal/canvas"
)

func TestCanvasImportHandoffFileText(t *testing.T) {
	content := []byte("hello from handoff")
	sum := sha256.Sum256(content)

	producer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		params, _ := req["params"].(map[string]interface{})
		name, _ := params["name"].(string)
		var structured map[string]interface{}
		switch name {
		case "handoff.peek":
			structured = map[string]interface{}{"handoff_id": "h1", "kind": "file"}
		case "handoff.consume":
			structured = map[string]interface{}{
				"spec_version": "handoff.v1",
				"handoff_id":   "h1",
				"kind":         "file",
				"meta": map[string]interface{}{
					"filename":   "note.txt",
					"mime_type":  "text/plain",
					"size_bytes": len(content),
					"sha256":     stringHex(sum[:]),
				},
				"payload": map[string]interface{}{
					"content_base64": base64.StdEncoding.EncodeToString(content),
				},
			}
		default:
			t.Fatalf("unexpected tool call: %s", name)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]interface{}{
				"isError":           false,
				"structuredContent": structured,
			},
		})
	}))
	defer producer.Close()

	projectDir := t.TempDir()
	s := NewServer(projectDir)
	s.SetAdapter(canvas.NewAdapter(projectDir, nil))
	got, err := s.callTool("canvas_import_handoff", map[string]interface{}{
		"session_id":       "s1",
		"handoff_id":       "h1",
		"producer_mcp_url": producer.URL,
		"title":            "Imported File",
	})
	if err != nil {
		t.Fatalf("canvas_import_handoff failed: %v", err)
	}
	if got["kind"] != "file" {
		t.Fatalf("expected kind=file, got %#v", got["kind"])
	}
	if got["artifact_id"] == nil {
		t.Fatalf("missing artifact_id: %#v", got)
	}

	matches, err := filepath.Glob(filepath.Join(projectDir, ".sloptools", "artifacts", "imports", "h1-*.txt"))
	if err != nil {
		t.Fatalf("glob failed: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected one imported file, found %d", len(matches))
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("read imported file: %v", err)
	}
	if string(data) != string(content) {
		t.Fatalf("imported content mismatch")
	}
}

func stringHex(b []byte) string {
	const hextable = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = hextable[v>>4]
		out[i*2+1] = hextable[v&0x0f]
	}
	return string(out)
}
