package appserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

func TestNormalizeApprovalPolicy(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "default", raw: "", want: ApprovalPolicyOnRequest},
		{name: "never", raw: "never", want: ApprovalPolicyNever},
		{name: "on request", raw: "on-request", want: ApprovalPolicyOnRequest},
		{name: "untrusted alias", raw: "untrusted", want: ApprovalPolicyUnlessTrusted},
		{name: "unless trusted alias", raw: "unless-trusted", want: ApprovalPolicyUnlessTrusted},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeApprovalPolicy(tc.raw); got != tc.want {
				t.Fatalf("normalizeApprovalPolicy(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestSendPromptStreamDefaultsToOnRequestApprovalPolicy(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
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
				_ = conn.WriteJSON(map[string]interface{}{
					"id":     msg["id"],
					"result": map[string]interface{}{"userAgent": "test"},
				})
			case "thread/start":
				params, _ := msg["params"].(map[string]interface{})
				gotPolicy = strings.TrimSpace(stringifyItemValue(params["approvalPolicy"]))
				_ = conn.WriteJSON(map[string]interface{}{
					"id": msg["id"],
					"result": map[string]interface{}{
						"thread": map[string]interface{}{"id": "thread-policy"},
					},
				})
			case "turn/start":
				_ = conn.WriteJSON(map[string]interface{}{
					"id": msg["id"],
					"result": map[string]interface{}{
						"turn": map[string]interface{}{"id": "turn-policy"},
					},
				})
				_ = conn.WriteJSON(map[string]interface{}{
					"method": "item/completed",
					"params": map[string]interface{}{
						"item": map[string]interface{}{
							"type": "agentMessage",
							"text": "done",
						},
					},
				})
				_ = conn.WriteJSON(map[string]interface{}{
					"method": "turn/completed",
					"params": map[string]interface{}{
						"turn": map[string]interface{}{"id": "turn-policy", "status": "completed"},
					},
				})
				return
			}
		}
	}))
	defer srv.Close()

	client, err := NewClient("ws" + strings.TrimPrefix(srv.URL, "http"))
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	_, err = client.SendPromptStream(context.Background(), PromptRequest{
		CWD:    "/tmp",
		Prompt: "plan this",
	}, nil)
	if err != nil {
		t.Fatalf("SendPromptStream: %v", err)
	}
	if gotPolicy != ApprovalPolicyOnRequest {
		t.Fatalf("approvalPolicy = %q, want %q", gotPolicy, ApprovalPolicyOnRequest)
	}
}

func TestSessionSendTurnHandlesApprovalRequests(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
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
				_ = conn.WriteJSON(map[string]interface{}{
					"id":     msg["id"],
					"result": map[string]interface{}{"userAgent": "test"},
				})
			case "thread/start":
				params, _ := msg["params"].(map[string]interface{})
				gotPolicy = strings.TrimSpace(stringifyItemValue(params["approvalPolicy"]))
				_ = conn.WriteJSON(map[string]interface{}{
					"id": msg["id"],
					"result": map[string]interface{}{
						"thread": map[string]interface{}{"id": "thread-approval"},
					},
				})
			case "turn/start":
				_ = conn.WriteJSON(map[string]interface{}{
					"id": msg["id"],
					"result": map[string]interface{}{
						"turn": map[string]interface{}{"id": "turn-approval"},
					},
				})
				_ = conn.WriteJSON(map[string]interface{}{
					"jsonrpc": "2.0",
					"id":      "approval-1",
					"method":  ApprovalMethodCommandRequest,
					"params": map[string]interface{}{
						"item_id": "item-7",
						"reason":  "run git status",
					},
				})
			default:
				if msg["id"] == "approval-1" {
					result, _ := msg["result"].(map[string]interface{})
					gotDecision = strings.TrimSpace(stringifyItemValue(result["decision"]))
					_ = conn.WriteJSON(map[string]interface{}{
						"method": "item/completed",
						"params": map[string]interface{}{
							"item": map[string]interface{}{
								"type": "agentMessage",
								"text": "approved and done",
							},
						},
					})
					_ = conn.WriteJSON(map[string]interface{}{
						"method": "turn/completed",
						"params": map[string]interface{}{
							"turn": map[string]interface{}{"id": "turn-approval", "status": "completed"},
						},
					})
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
	sess, err := client.OpenSessionWithParams(context.Background(), "/tmp", "", map[string]interface{}{
		"approvalPolicy": "untrusted",
	})
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
