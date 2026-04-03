package appserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// newTestServer creates a mock codex app server that handles initialize,
// thread/start, and multiple turn/start requests on a single connection.
func newTestServer(t *testing.T, turnHandler func(conn *websocket.Conn, msg map[string]interface{}, turnCount int)) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
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
				_ = conn.WriteJSON(map[string]interface{}{
					"id":     msg["id"],
					"result": map[string]interface{}{"userAgent": "test"},
				})
			case "initialized":
				// no response needed
			case "thread/start":
				_ = conn.WriteJSON(map[string]interface{}{
					"id": msg["id"],
					"result": map[string]interface{}{
						"thread": map[string]interface{}{"id": "thread-persist"},
					},
				})
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
	_ = conn.WriteJSON(map[string]interface{}{
		"id": id,
		"result": map[string]interface{}{
			"turn": map[string]interface{}{"id": "turn-" + strings.TrimSpace(text)},
		},
	})
	_ = conn.WriteJSON(map[string]interface{}{
		"method": "item/completed",
		"params": map[string]interface{}{
			"item": map[string]interface{}{
				"type": "agentMessage",
				"text": "reply to: " + text,
			},
		},
	})
	_ = conn.WriteJSON(map[string]interface{}{
		"method": "turn/completed",
		"params": map[string]interface{}{
			"turn": map[string]interface{}{
				"id":     "turn-" + strings.TrimSpace(text),
				"status": "completed",
			},
			"usage": map[string]interface{}{
				"input_tokens":   float64(turnCount * 1000),
				"context_window": float64(128000),
			},
		},
	})
}

func TestSessionMultiTurnReuseThread(t *testing.T) {
	srv := newTestServer(t, simpleTurnHandler)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	client, err := NewClient(wsURL)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	ctx := context.Background()
	sess, err := client.OpenSession(ctx, "/tmp", "")
	if err != nil {
		t.Fatalf("open session: %v", err)
	}
	defer sess.Close()

	if sess.ThreadID() != "thread-persist" {
		t.Fatalf("expected thread-persist, got %q", sess.ThreadID())
	}

	// Turn 1
	resp1, err := sess.SendTurn(ctx, "hello", "", nil)
	if err != nil {
		t.Fatalf("turn 1: %v", err)
	}
	if resp1.ThreadID != "thread-persist" {
		t.Fatalf("turn 1 thread mismatch: %q", resp1.ThreadID)
	}
	if resp1.Message != "reply to: hello" {
		t.Fatalf("turn 1 message: %q", resp1.Message)
	}

	// Turn 2 on same session
	resp2, err := sess.SendTurn(ctx, "world", "", nil)
	if err != nil {
		t.Fatalf("turn 2: %v", err)
	}
	if resp2.ThreadID != "thread-persist" {
		t.Fatalf("turn 2 thread mismatch: %q", resp2.ThreadID)
	}
	if resp2.Message != "reply to: world" {
		t.Fatalf("turn 2 message: %q", resp2.Message)
	}
}

func TestSessionContextUsageTracking(t *testing.T) {
	srv := newTestServer(t, simpleTurnHandler)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	client, err := NewClient(wsURL)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	ctx := context.Background()
	sess, err := client.OpenSession(ctx, "/tmp", "")
	if err != nil {
		t.Fatalf("open session: %v", err)
	}
	defer sess.Close()

	var usageEvents []StreamEvent
	onEvent := func(ev StreamEvent) {
		if ev.Type == "context_usage" {
			usageEvents = append(usageEvents, ev)
		}
	}

	_, err = sess.SendTurn(ctx, "first", "", onEvent)
	if err != nil {
		t.Fatalf("turn: %v", err)
	}

	if sess.ContextUsed != 1000 {
		t.Fatalf("expected 1000 tokens used, got %d", sess.ContextUsed)
	}
	if sess.ContextMax != 128000 {
		t.Fatalf("expected 128000 max, got %d", sess.ContextMax)
	}
	if len(usageEvents) != 1 {
		t.Fatalf("expected 1 usage event, got %d", len(usageEvents))
	}
	if usageEvents[0].ContextUsed != 1000 {
		t.Fatalf("usage event ContextUsed: %d", usageEvents[0].ContextUsed)
	}

	// Turn 2: usage should update
	_, err = sess.SendTurn(ctx, "second", "", onEvent)
	if err != nil {
		t.Fatalf("turn 2: %v", err)
	}
	if sess.ContextUsed != 2000 {
		t.Fatalf("expected 2000 tokens used after turn 2, got %d", sess.ContextUsed)
	}
}

func TestSessionSendTurnInputWithParams_UsesExplicitImageInput(t *testing.T) {
	sawImage := false
	srv := newTestServer(t, func(conn *websocket.Conn, msg map[string]interface{}, turnCount int) {
		params, _ := msg["params"].(map[string]interface{})
		input, _ := params["input"].([]interface{})
		if len(input) != 2 {
			t.Fatalf("input len = %d, want 2", len(input))
		}
		second, _ := input[1].(map[string]interface{})
		if strings.TrimSpace(second["type"].(string)) != "image_url" {
			t.Fatalf("input[1].type = %v, want image_url", second["type"])
		}
		if strings.TrimSpace(second["image_url"].(string)) == "" {
			t.Fatal("input[1].image_url should not be empty")
		}
		sawImage = true
		simpleTurnHandler(conn, msg, turnCount)
	})
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	client, err := NewClient(wsURL)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	ctx := context.Background()
	sess, err := client.OpenSession(ctx, "/tmp", "")
	if err != nil {
		t.Fatalf("open session: %v", err)
	}
	defer sess.Close()

	_, err = sess.SendTurnInputWithParams(ctx, BuildTurnInput([]TurnInputItem{
		{Type: "text", Text: "inspect this"},
		{Type: "image_url", ImageURL: "data:image/png;base64,Zm9v"},
	}), "", nil, nil)
	if err != nil {
		t.Fatalf("SendTurnInputWithParams: %v", err)
	}
	if !sawImage {
		t.Fatal("expected explicit image input to be forwarded")
	}
}

func TestSessionSendTurnHonorsContextCancel(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
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
				t.Fatalf("decode: %v", err)
			}
			method, _ := msg["method"].(string)
			switch method {
			case "initialize":
				_ = conn.WriteJSON(map[string]interface{}{
					"id":     msg["id"],
					"result": map[string]interface{}{"userAgent": "test"},
				})
			case "initialized":
			case "thread/start":
				_ = conn.WriteJSON(map[string]interface{}{
					"id": msg["id"],
					"result": map[string]interface{}{
						"thread": map[string]interface{}{"id": "thread-persist"},
					},
				})
			case "turn/start":
				_ = conn.WriteJSON(map[string]interface{}{
					"id": msg["id"],
					"result": map[string]interface{}{
						"turn": map[string]interface{}{"id": "turn-cancel"},
					},
				})
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

	sess, err := client.OpenSession(context.Background(), "/tmp", "")
	if err != nil {
		t.Fatalf("open session: %v", err)
	}
	defer sess.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-turnStarted
		cancel()
	}()

	startedAt := time.Now()
	_, err = sess.SendTurn(ctx, "cancel test", "", nil)
	if err == nil || !strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Fatalf("expected context cancellation error, got %v", err)
	}
	if elapsed := time.Since(startedAt); elapsed > 2*time.Second {
		t.Fatalf("expected turn cancellation to return promptly, took %s", elapsed)
	}
	if sess.IsOpen() {
		t.Fatal("expected canceled session turn to close the persistent session")
	}
}

func TestSessionClosedAfterError(t *testing.T) {
	srv := newTestServer(t, func(conn *websocket.Conn, msg map[string]interface{}, _ int) {
		_ = conn.WriteJSON(map[string]interface{}{
			"id":    msg["id"],
			"error": map[string]interface{}{"message": "model overloaded"},
		})
	})
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	client, err := NewClient(wsURL)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sess, err := client.OpenSession(ctx, "/tmp", "")
	if err != nil {
		t.Fatalf("open session: %v", err)
	}

	_, err = sess.SendTurn(ctx, "fail", "", nil)
	if err == nil {
		t.Fatal("expected error from failed turn")
	}
	if !strings.Contains(err.Error(), "model overloaded") {
		t.Fatalf("expected model overloaded, got: %v", err)
	}
}

func TestSessionItemCompletedForwarding(t *testing.T) {
	srv := newTestServer(t, func(conn *websocket.Conn, msg map[string]interface{}, _ int) {
		id := msg["id"]
		_ = conn.WriteJSON(map[string]interface{}{
			"id": id,
			"result": map[string]interface{}{
				"turn": map[string]interface{}{"id": "turn-edit"},
			},
		})
		// Emit fileChange items with paths before the agentMessage.
		_ = conn.WriteJSON(map[string]interface{}{
			"method": "item/completed",
			"params": map[string]interface{}{
				"item": map[string]interface{}{
					"type": "fileChange",
					"file": "src/main.go",
				},
			},
		})
		_ = conn.WriteJSON(map[string]interface{}{
			"method": "item/completed",
			"params": map[string]interface{}{
				"item": map[string]interface{}{
					"type": "fileChange",
					"path": "src/util.go",
				},
			},
		})
		_ = conn.WriteJSON(map[string]interface{}{
			"method": "item/completed",
			"params": map[string]interface{}{
				"item": map[string]interface{}{
					"type": "agentMessage",
					"text": "done editing",
				},
			},
		})
		_ = conn.WriteJSON(map[string]interface{}{
			"method": "turn/completed",
			"params": map[string]interface{}{
				"turn": map[string]interface{}{"id": "turn-edit", "status": "completed"},
			},
		})
	})
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	client, err := NewClient(wsURL)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	ctx := context.Background()
	sess, err := client.OpenSession(ctx, "/tmp", "")
	if err != nil {
		t.Fatalf("open session: %v", err)
	}
	defer sess.Close()

	var itemEvents []StreamEvent
	resp, err := sess.SendTurn(ctx, "edit file", "", func(ev StreamEvent) {
		if ev.Type == "item_completed" {
			itemEvents = append(itemEvents, ev)
		}
	})
	if err != nil {
		t.Fatalf("turn: %v", err)
	}
	if resp.Message != "done editing" {
		t.Fatalf("unexpected message: %q", resp.Message)
	}
	if len(itemEvents) != 2 {
		t.Fatalf("expected 2 item_completed events, got %d", len(itemEvents))
	}
	if itemEvents[0].Message != "fileChange" {
		t.Fatalf("expected fileChange type, got %q", itemEvents[0].Message)
	}
	if len(resp.FileChanges) != 2 {
		t.Fatalf("expected 2 file changes, got %d", len(resp.FileChanges))
	}
	if resp.FileChanges[0] != "src/main.go" {
		t.Fatalf("expected src/main.go, got %q", resp.FileChanges[0])
	}
	if resp.FileChanges[1] != "src/util.go" {
		t.Fatalf("expected src/util.go, got %q", resp.FileChanges[1])
	}
}

func TestSessionCompactEvent(t *testing.T) {
	srv := newTestServer(t, func(conn *websocket.Conn, msg map[string]interface{}, _ int) {
		id := msg["id"]
		_ = conn.WriteJSON(map[string]interface{}{
			"id": id,
			"result": map[string]interface{}{
				"turn": map[string]interface{}{"id": "turn-compact"},
			},
		})
		_ = conn.WriteJSON(map[string]interface{}{
			"method": "codex/event/context_compact",
			"params": map[string]interface{}{},
		})
		_ = conn.WriteJSON(map[string]interface{}{
			"method": "item/completed",
			"params": map[string]interface{}{
				"item": map[string]interface{}{
					"type": "agentMessage",
					"text": "compacted reply",
				},
			},
		})
		_ = conn.WriteJSON(map[string]interface{}{
			"method": "turn/completed",
			"params": map[string]interface{}{
				"turn": map[string]interface{}{"id": "turn-compact", "status": "completed"},
			},
		})
	})
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	client, err := NewClient(wsURL)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	ctx := context.Background()
	sess, err := client.OpenSession(ctx, "/tmp", "")
	if err != nil {
		t.Fatalf("open session: %v", err)
	}
	defer sess.Close()

	var compactSeen bool
	resp, err := sess.SendTurn(ctx, "big prompt", "", func(ev StreamEvent) {
		if ev.Type == "context_compact" {
			compactSeen = true
		}
	})
	if err != nil {
		t.Fatalf("turn: %v", err)
	}
	if !compactSeen {
		t.Fatal("expected context_compact event")
	}
	if resp.Message != "compacted reply" {
		t.Fatalf("unexpected message: %q", resp.Message)
	}
}
