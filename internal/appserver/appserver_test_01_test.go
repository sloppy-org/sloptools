package appserver

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/gorilla/websocket"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNormalizeApprovalPolicy(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{{name: "default", raw: "", want: ApprovalPolicyOnRequest}, {name: "never", raw: "never", want: ApprovalPolicyNever}, {name: "on request", raw: "on-request", want: ApprovalPolicyOnRequest}, {name: "untrusted alias", raw: "untrusted", want: ApprovalPolicyUnlessTrusted}, {name: "unless trusted alias", raw: "unless-trusted", want: ApprovalPolicyUnlessTrusted}}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeApprovalPolicy(tc.raw); got != tc.want {
				t.Fatalf("normalizeApprovalPolicy(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestSendPromptStreamDefaultsToOnRequestApprovalPolicy(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool {
		return true
	}}
	gotPolicy := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer conn.Close()
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg map[string]interface{}
			if err := json.Unmarshal(data, &msg); err != nil {
				t.Fatalf("decode: %v", err)
			}
			switch strings.TrimSpace(stringifyItemValue(msg["method"])) {
			case "initialize":
				_ = conn.WriteJSON(map[string]interface{}{"id": msg["id"], "result": map[string]interface{}{"userAgent": "test"}})
			case "thread/start":
				params, _ := msg["params"].(map[string]interface{})
				gotPolicy = strings.TrimSpace(stringifyItemValue(params["approvalPolicy"]))
				_ = conn.WriteJSON(map[string]interface{}{"id": msg["id"], "result": map[string]interface{}{"thread": map[string]interface{}{"id": "thread-policy"}}})
			case "turn/start":
				_ = conn.WriteJSON(map[string]interface{}{"id": msg["id"], "result": map[string]interface{}{"turn": map[string]interface{}{"id": "turn-policy"}}})
				_ = conn.WriteJSON(map[string]interface{}{"method": "item/completed", "params": map[string]interface{}{"item": map[string]interface{}{"type": "agentMessage", "text": "done"}}})
				_ = conn.WriteJSON(map[string]interface{}{"method": "turn/completed", "params": map[string]interface{}{"turn": map[string]interface{}{"id": "turn-policy", "status": "completed"}}})
				return
			}
		}
	}))
	defer srv.Close()
	client, err := NewClient("ws" + strings.TrimPrefix(srv.URL, "http"))
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	_, err = client.SendPromptStream(context.Background(), PromptRequest{CWD: "/tmp", Prompt: "plan this"}, nil)
	if err != nil {
		t.Fatalf("SendPromptStream: %v", err)
	}
	if gotPolicy != ApprovalPolicyOnRequest {
		t.Fatalf("approvalPolicy = %q, want %q", gotPolicy, ApprovalPolicyOnRequest)
	}
}

func TestSessionSendTurnHandlesApprovalRequests(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool {
		return true
	}}
	gotPolicy := ""
	gotDecision := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer conn.Close()
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg map[string]interface{}
			if err := json.Unmarshal(data, &msg); err != nil {
				t.Fatalf("decode: %v", err)
			}
			switch strings.TrimSpace(stringifyItemValue(msg["method"])) {
			case "initialize":
				_ = conn.WriteJSON(map[string]interface{}{"id": msg["id"], "result": map[string]interface{}{"userAgent": "test"}})
			case "thread/start":
				params, _ := msg["params"].(map[string]interface{})
				gotPolicy = strings.TrimSpace(stringifyItemValue(params["approvalPolicy"]))
				_ = conn.WriteJSON(map[string]interface{}{"id": msg["id"], "result": map[string]interface{}{"thread": map[string]interface{}{"id": "thread-approval"}}})
			case "turn/start":
				_ = conn.WriteJSON(map[string]interface{}{"id": msg["id"], "result": map[string]interface{}{"turn": map[string]interface{}{"id": "turn-approval"}}})
				_ = conn.WriteJSON(map[string]interface{}{"jsonrpc": "2.0", "id": "approval-1", "method": ApprovalMethodCommandRequest, "params": map[string]interface{}{"item_id": "item-7", "reason": "run git status"}})
			default:
				if msg["id"] == "approval-1" {
					result, _ := msg["result"].(map[string]interface{})
					gotDecision = strings.TrimSpace(stringifyItemValue(result["decision"]))
					_ = conn.WriteJSON(map[string]interface{}{"method": "item/completed", "params": map[string]interface{}{"item": map[string]interface{}{"type": "agentMessage", "text": "approved and done"}}})
					_ = conn.WriteJSON(map[string]interface{}{"method": "turn/completed", "params": map[string]interface{}{"turn": map[string]interface{}{"id": "turn-approval", "status": "completed"}}})
					return
				}
			}
		}
	}))
	defer srv.Close()
	client, err := NewClient("ws" + strings.TrimPrefix(srv.URL, "http"))
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	sess, err := client.OpenSessionWithParams(context.Background(), "/tmp", "", map[string]interface{}{"approvalPolicy": "untrusted"})
	if err != nil {
		t.Fatalf("open session: %v", err)
	}
	defer sess.Close()
	var approvalEvent *StreamEvent
	resp, err := sess.SendTurn(context.Background(), "inspect repo", "", func(ev StreamEvent) {
		if ev.Type != "approval_request" {
			return
		}
		approvalEvent = &ev
		if ev.Approval == nil {
			t.Fatal("approval event missing request")
		}
		if err := ev.Respond("approve"); err != nil {
			t.Fatalf("Respond: %v", err)
		}
	})
	if err != nil {
		t.Fatalf("SendTurn: %v", err)
	}
	if gotPolicy != ApprovalPolicyUnlessTrusted {
		t.Fatalf("approvalPolicy = %q, want %q", gotPolicy, ApprovalPolicyUnlessTrusted)
	}
	if approvalEvent == nil {
		t.Fatal("expected approval_request event")
	}
	if approvalEvent.Approval.Kind != "command_execution" {
		t.Fatalf("approval kind = %q, want command_execution", approvalEvent.Approval.Kind)
	}
	if approvalEvent.Approval.Reason != "run git status" {
		t.Fatalf("approval reason = %q, want %q", approvalEvent.Approval.Reason, "run git status")
	}
	if gotDecision != "accept" {
		t.Fatalf("decision = %q, want %q", gotDecision, "accept")
	}
	if resp.Message != "approved and done" {
		t.Fatalf("unexpected response message %q", resp.Message)
	}
}

func TestSendPrompt(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool {
		return true
	}}
	promptSeen := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer conn.Close()
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg map[string]interface{}
			if err := json.Unmarshal(data, &msg); err != nil {
				t.Fatalf("decode message: %v", err)
			}
			method, _ := msg["method"].(string)
			switch method {
			case "initialize":
				_ = conn.WriteJSON(map[string]interface{}{"id": msg["id"], "result": map[string]interface{}{"userAgent": "test-client"}})
			case "thread/start":
				_ = conn.WriteJSON(map[string]interface{}{"id": msg["id"], "result": map[string]interface{}{"thread": map[string]interface{}{"id": "thread-test-1"}}})
			case "turn/start":
				params, _ := msg["params"].(map[string]interface{})
				input, _ := params["input"].([]interface{})
				if len(input) > 0 {
					first, _ := input[0].(map[string]interface{})
					promptSeen, _ = first["text"].(string)
				}
				_ = conn.WriteJSON(map[string]interface{}{"id": msg["id"], "result": map[string]interface{}{"turn": map[string]interface{}{"id": "turn-test-1"}}})
				_ = conn.WriteJSON(map[string]interface{}{"method": "item/completed", "params": map[string]interface{}{"item": map[string]interface{}{"type": "agentMessage", "text": "# Improved\n\nUpdated by test."}}})
				_ = conn.WriteJSON(map[string]interface{}{"method": "turn/completed", "params": map[string]interface{}{"turn": map[string]interface{}{"id": "turn-test-1", "status": "completed"}}})
				return
			}
		}
	}))
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	client, err := NewClient(wsURL)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := client.SendPrompt(ctx, PromptRequest{CWD: "/tmp", Prompt: "rewrite this markdown"})
	if err != nil {
		t.Fatalf("send prompt: %v", err)
	}
	if promptSeen != "rewrite this markdown" {
		t.Fatalf("expected prompt to be forwarded, got %q", promptSeen)
	}
	if resp.ThreadID != "thread-test-1" {
		t.Fatalf("expected thread-test-1, got %q", resp.ThreadID)
	}
	if resp.TurnID != "turn-test-1" {
		t.Fatalf("expected turn-test-1, got %q", resp.TurnID)
	}
	if strings.TrimSpace(resp.Message) != "# Improved\n\nUpdated by test." {
		t.Fatalf("unexpected assistant message: %q", resp.Message)
	}
}

func TestNormalizeURLRejectsNonLoopback(t *testing.T) {
	if _, err := NormalizeURL("ws://example.com:8787"); err == nil {
		t.Fatalf("expected non-loopback URL to be rejected")
	}
}

func TestSendPromptStreamHonorsContextCancel(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool {
		return true
	}}
	turnStarted := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer conn.Close()
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg map[string]interface{}
			if err := json.Unmarshal(data, &msg); err != nil {
				t.Fatalf("decode message: %v", err)
			}
			method, _ := msg["method"].(string)
			switch method {
			case "initialize":
				_ = conn.WriteJSON(map[string]interface{}{"id": msg["id"], "result": map[string]interface{}{"userAgent": "test-client"}})
			case "thread/start":
				_ = conn.WriteJSON(map[string]interface{}{"id": msg["id"], "result": map[string]interface{}{"thread": map[string]interface{}{"id": "thread-cancel"}}})
			case "turn/start":
				_ = conn.WriteJSON(map[string]interface{}{"id": msg["id"], "result": map[string]interface{}{"turn": map[string]interface{}{"id": "turn-cancel"}}})
				select {
				case <-turnStarted:
				default:
					close(turnStarted)
				}
			}
		}
	}))
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	client, err := NewClient(wsURL)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-turnStarted
		cancel()
	}()
	startedAt := time.Now()
	_, err = client.SendPromptStream(ctx, PromptRequest{CWD: "/tmp", Prompt: "cancel test", Timeout: 20 * time.Second}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation error, got %v", err)
	}
	if elapsed := time.Since(startedAt); elapsed > 2*time.Second {
		t.Fatalf("expected prompt cancellation to return promptly, took %s", elapsed)
	}
}

func TestSendPromptCollectsFileChanges(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool {
		return true
	}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer conn.Close()
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg map[string]interface{}
			if err := json.Unmarshal(data, &msg); err != nil {
				t.Fatalf("decode message: %v", err)
			}
			method, _ := msg["method"].(string)
			switch method {
			case "initialize":
				_ = conn.WriteJSON(map[string]interface{}{"id": msg["id"], "result": map[string]interface{}{"userAgent": "test-client"}})
			case "thread/start":
				_ = conn.WriteJSON(map[string]interface{}{"id": msg["id"], "result": map[string]interface{}{"thread": map[string]interface{}{"id": "thread-fc"}}})
			case "turn/start":
				_ = conn.WriteJSON(map[string]interface{}{"id": msg["id"], "result": map[string]interface{}{"turn": map[string]interface{}{"id": "turn-fc"}}})
				_ = conn.WriteJSON(map[string]interface{}{"method": "item/completed", "params": map[string]interface{}{"item": map[string]interface{}{"type": "fileChange", "file": "server.go"}}})
				_ = conn.WriteJSON(map[string]interface{}{"method": "item/completed", "params": map[string]interface{}{"item": map[string]interface{}{"type": "agentMessage", "text": "edited server.go"}}})
				_ = conn.WriteJSON(map[string]interface{}{"method": "turn/completed", "params": map[string]interface{}{"turn": map[string]interface{}{"id": "turn-fc", "status": "completed"}}})
				return
			}
		}
	}))
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	client, err := NewClient(wsURL)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := client.SendPrompt(ctx, PromptRequest{CWD: "/tmp", Prompt: "edit server.go"})
	if err != nil {
		t.Fatalf("send prompt: %v", err)
	}
	if len(resp.FileChanges) != 1 {
		t.Fatalf("expected 1 file change, got %d", len(resp.FileChanges))
	}
	if resp.FileChanges[0] != "server.go" {
		t.Fatalf("expected server.go, got %q", resp.FileChanges[0])
	}
}

func TestSendPromptStreamTimeoutMapsToDeadlineExceeded(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool {
		return true
	}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer conn.Close()
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg map[string]interface{}
			if err := json.Unmarshal(data, &msg); err != nil {
				t.Fatalf("decode message: %v", err)
			}
			method, _ := msg["method"].(string)
			switch method {
			case "initialize":
				_ = conn.WriteJSON(map[string]interface{}{"id": msg["id"], "result": map[string]interface{}{"userAgent": "test-client"}})
			case "thread/start":
				_ = conn.WriteJSON(map[string]interface{}{"id": msg["id"], "result": map[string]interface{}{"thread": map[string]interface{}{"id": "thread-timeout"}}})
			case "turn/start":
				_ = conn.WriteJSON(map[string]interface{}{"id": msg["id"], "result": map[string]interface{}{"turn": map[string]interface{}{"id": "turn-timeout"}}})
			}
		}
	}))
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	client, err := NewClient(wsURL)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	startedAt := time.Now()
	_, err = client.SendPrompt(context.Background(), PromptRequest{CWD: "/tmp", Prompt: "timeout test", Timeout: 120 * time.Millisecond})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline exceeded, got %v", err)
	}
	if elapsed := time.Since(startedAt); elapsed > 2*time.Second {
		t.Fatalf("timeout should return promptly, took %s", elapsed)
	}
}

func TestExtractItemDetailPrefersReadableFields(t *testing.T) {
	item := map[string]interface{}{"type": "exec_command", "summary": "  running   go test   ./internal/web  ", "detail": "ignored because summary wins"}
	got := extractItemDetail(item)
	if got != "ignored because summary wins" {
		t.Fatalf("expected detail field to win, got %q", got)
	}
}

func TestExtractItemDetailFormatsToolArguments(t *testing.T) {
	item := map[string]interface{}{"type": "tool_call", "tool_name": "exec_command", "arguments": map[string]interface{}{"cmd": "go test ./internal/web -run TestStop"}}
	got := extractItemDetail(item)
	if !strings.Contains(got, "exec_command") {
		t.Fatalf("expected tool name in detail, got %q", got)
	}
	if !strings.Contains(got, "TestStop") {
		t.Fatalf("expected arguments in detail, got %q", got)
	}
}

func TestExtractItemDetailFallsBackToSnapshot(t *testing.T) {
	item := map[string]interface{}{"type": "reasoning", "foo": "bar"}
	got := extractItemDetail(item)
	if !strings.Contains(got, "foo") || !strings.Contains(got, "bar") {
		t.Fatalf("expected fallback snapshot detail, got %q", got)
	}
}

func newTestServer(t *testing.T, turnHandler func(conn *websocket.Conn, msg map[ // newTestServer creates a mock codex app server that handles initialize,
// thread/start, and multiple turn/start requests on a single connection.
string]interface{}, turnCount int)) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool {
		return true
	}}
	turnCount := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer conn.Close()
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg map[string]interface{}
			if err := json.Unmarshal(data, &msg); err != nil {
				t.Fatalf("decode: %v", err)
			}
			method, _ := msg["method"].(string)
			switch method {
			case "initialize":
				_ = conn.WriteJSON(map[string]interface{}{"id": msg["id"], "result": map[string]interface{}{"userAgent": "test"}})
			case "initialized":
			case "thread/start":
				_ = conn.WriteJSON(map[string]interface{}{"id": msg["id"], "result": map[string]interface{}{"thread": map[string]interface{}{"id": "thread-persist"}}})
			case "turn/start":
				turnCount++
				turnHandler(conn, msg, turnCount)
			}
		}
	}))
}

func simpleTurnHandler(conn *websocket.Conn, msg map[string]interface{}, turnCount int) {
	id := msg["id"]
	params, _ := msg["params"].(map[string]interface{})
	input, _ := params["input"].([]interface{})
	text := ""
	if len(input) > 0 {
		first, _ := input[0].(map[string]interface{})
		text, _ = first["text"].(string)
	}
	_ = conn.WriteJSON(map[string]interface{}{"id": id, "result": map[string]interface{}{"turn": map[string]interface{}{"id": "turn-" + strings.TrimSpace(text)}}})
	_ = conn.WriteJSON(map[string]interface{}{"method": "item/completed", "params": map[string]interface{}{"item": map[string]interface{}{"type": "agentMessage", "text": "reply to: " + text}}})
	_ = conn.WriteJSON(map[string]interface{}{"method": "turn/completed", "params": map[string]interface{}{"turn": map[string]interface{}{"id": "turn-" + strings.TrimSpace(text), "status": "completed"}, "usage": map[string]interface{}{"input_tokens": float64(turnCount * 1000), "context_window": float64(128000)}}})
}
