package serve

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestHandleMCPPostParseError(t *testing.T) {
	app := NewApp(t.TempDir(), "")
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString("{bad-json"))
	rr := httptest.NewRecorder()

	app.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	errObj, _ := payload["error"].(map[string]interface{})
	if int(errObj["code"].(float64)) != -32700 {
		t.Fatalf("error code = %v, want -32700", errObj["code"])
	}
}

func TestHandleMCPPostInitializeSetsSessionHeader(t *testing.T) {
	app := NewApp(t.TempDir(), "")
	body := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]interface{}{
			"protocolVersion": "2025-03-26",
		},
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(raw))
	rr := httptest.NewRecorder()

	app.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if rr.Header().Get("Mcp-Session-Id") == "" {
		t.Fatalf("expected Mcp-Session-Id header to be set")
	}
}

func TestHandleMCPPostNotificationReturnsAccepted(t *testing.T) {
	app := NewApp(t.TempDir(), "")
	body := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "ping",
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(raw))
	rr := httptest.NewRecorder()

	app.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rr.Code)
	}
}

func TestHandleHealthIncludesStatusAndProjectDir(t *testing.T) {
	projectDir := t.TempDir()
	app := NewApp(projectDir, "")
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()

	app.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode health payload: %v", err)
	}
	if payload["status"] != "ok" {
		t.Fatalf("status field = %v, want ok", payload["status"])
	}
	if payload["project_dir"] != projectDir {
		t.Fatalf("project_dir = %v, want %q", payload["project_dir"], projectDir)
	}
}

func TestListenURLsWithScheme(t *testing.T) {
	urls := ListenURLsWithScheme("127.0.0.1", 9420, "HTTPS")
	if len(urls) != 1 {
		t.Fatalf("len(urls) = %d, want 1", len(urls))
	}
	if urls[0] != "https://127.0.0.1:9420" {
		t.Fatalf("ListenURLsWithScheme() = %q, want https://127.0.0.1:9420", urls[0])
	}
}

func TestRandomSessionIDIsNumeric(t *testing.T) {
	id := randomSessionID()
	if strings.TrimSpace(id) == "" {
		t.Fatalf("randomSessionID() returned empty id")
	}
	if _, err := strconv.ParseInt(id, 10, 64); err != nil {
		t.Fatalf("randomSessionID() should be numeric, got %q: %v", id, err)
	}
}

func TestStopNoServerIsNoop(t *testing.T) {
	app := NewApp(t.TempDir(), "")
	if err := app.Stop(context.Background()); err != nil {
		t.Fatalf("Stop(nil server) error: %v", err)
	}
}

func TestStartUnixTightensSocketPermissions(t *testing.T) {
	projectDir := t.TempDir()
	socketDir := mustShortTempDir(t, "sloptools-mcp-")
	if err := os.MkdirAll(socketDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(socketDir): %v", err)
	}
	socketPath := filepath.Join(socketDir, "mcp.sock")
	app := NewApp(projectDir, "")
	errCh := make(chan error, 1)
	go func() {
		errCh <- app.StartUnix(socketPath)
	}()
	waitForUnixHealth(t, socketPath)
	socketInfo, err := os.Lstat(socketPath)
	if err != nil {
		t.Fatalf("Lstat(socketPath): %v", err)
	}
	if socketInfo.Mode()&os.ModeSocket == 0 {
		t.Fatalf("socket mode = %v, want unix socket bit set", socketInfo.Mode())
	}
	if perms := socketInfo.Mode().Perm(); perms != 0o600 {
		t.Fatalf("socket perms = %o, want 600", perms)
	}
	dirInfo, err := os.Stat(socketDir)
	if err != nil {
		t.Fatalf("Stat(socketDir): %v", err)
	}
	if perms := dirInfo.Mode().Perm(); perms != 0o700 {
		t.Fatalf("socket dir perms = %o, want 700", perms)
	}
	if err := app.Stop(context.Background()); err != nil {
		t.Fatalf("Stop(): %v", err)
	}
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("StartUnix returned error after Stop: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("StartUnix did not return after Stop")
	}
}

func TestHandleMCPGetReturnsEventStreamHeaders(t *testing.T) {
	app := NewApp(t.TempDir(), "")
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/mcp", nil).WithContext(ctx)
	rr := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		app.Router().ServeHTTP(rr, req)
		close(done)
	}()

	time.Sleep(10 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("handleMCPGet did not return after request context cancel")
	}

	if ct := rr.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}
	if cc := rr.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Fatalf("Cache-Control = %q, want no-cache", cc)
	}
}

func mustShortTempDir(t *testing.T, pattern string) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", pattern)
	if err != nil {
		t.Fatalf("MkdirTemp(/tmp): %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func waitForUnixHealth(t *testing.T, socketPath string) {
	t.Helper()
	client := &http.Client{
		Timeout: 250 * time.Millisecond,
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", socketPath)
			},
		},
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get("http://unix/health")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("unix socket %q did not become healthy", socketPath)
}
