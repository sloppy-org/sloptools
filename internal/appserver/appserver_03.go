package appserver

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

func (s *Session) readTurnUntilComplete(ctx context.Context, turnRPCID int, onEvent func(StreamEvent)) (string, string, []string, error) {
	turnResponseSeen := false
	turnID := ""
	message := ""
	previousMessage := ""
	turnCompleted := false
	var fileChanges []string
	for {
		msg, err := readJSON(ctx, s.conn)
		if err != nil {
			return "", "", nil, err
		}
		if approvalReq, ok := parseApprovalRequest(msg); ok {
			ev := StreamEvent{Type: "approval_request", ThreadID: s.threadID, TurnID: turnID, Approval: approvalReq}
			ev.Respond = func(decision string) error {
				return s.writeJSON(ctx, map[string]interface{}{"jsonrpc": "2.0", "id": approvalReq.ID, "result": map[string]interface{}{"decision": normalizeApprovalDecision(decision)}})
			}
			if onEvent != nil {
				onEvent(ev)
			} else {
				_ = ev.Respond("cancel")
			}
			continue
		}
		if msgID, hasID := jsonRPCID(msg); hasID && msgID == turnRPCID {
			if errObj, ok := msg["error"].(map[string]interface{}); ok && errObj != nil {
				errText := strings.TrimSpace(fmt.Sprint(errObj["message"]))
				if onEvent != nil {
					onEvent(StreamEvent{Type: "error", ThreadID: s.threadID, TurnID: turnID, Error: errText})
				}
				return "", "", nil, fmt.Errorf("turn/start rpc error: %s", errText)
			}
			turnResponseSeen = true
			if result, _ := msg["result"].(map[string]interface{}); result != nil {
				if turn, _ := result["turn"].(map[string]interface{}); turn != nil {
					if id, _ := turn["id"].(string); strings.TrimSpace(id) != "" {
						turnID = id
					}
				}
			}
			if onEvent != nil {
				onEvent(StreamEvent{Type: "turn_started", ThreadID: s.threadID, TurnID: turnID})
			}
			continue
		}
		method, _ := msg["method"].(string)
		params, _ := msg["params"].(map[string]interface{})
		switch method {
		case "item/completed":
			if item, _ := params["item"].(map[string]interface{}); item != nil {
				typ, _ := item["type"].(string)
				detail := extractItemDetail(item)
				if typ == "agentMessage" {
					if text, _ := item["text"].(string); strings.TrimSpace(text) != "" {
						message = text
					}
					continue
				}
				if typ == "fileChange" {
					p := extractFileChangePath(item)
					if p != "" {
						fileChanges = append(fileChanges, p)
					}
					if detail == "" {
						detail = p
					}
				}
				if typ != "" && onEvent != nil {
					onEvent(StreamEvent{Type: "item_completed", ThreadID: s.threadID, TurnID: turnID, Message: typ, Detail: detail})
				}
			}
		case "turn/completed":
			turnCompleted = true
			if turn, _ := params["turn"].(map[string]interface{}); turn != nil {
				if id, _ := turn["id"].(string); strings.TrimSpace(id) != "" {
					turnID = id
				}
			}
			s.extractUsage(params, onEvent)
			if onEvent != nil {
				onEvent(StreamEvent{Type: "turn_completed", ThreadID: s.threadID, TurnID: turnID, Message: strings.TrimSpace(message)})
			}
		case "codex/event/agent_message":
			if msgObj, _ := params["msg"].(map[string]interface{}); msgObj != nil {
				if text, _ := msgObj["message"].(string); strings.TrimSpace(text) != "" {
					message = text
				}
			}
		case "codex/event/task_complete":
			if msgObj, _ := params["msg"].(map[string]interface{}); msgObj != nil {
				if text, _ := msgObj["last_agent_message"].(string); strings.TrimSpace(text) != "" {
					message = text
				}
			}
		case "codex/event/context_compact", "context/compact":
			if onEvent != nil {
				onEvent(StreamEvent{Type: "context_compact", ThreadID: s.threadID, TurnID: turnID})
			}
		case "error":
			if params != nil {
				errText := strings.TrimSpace(fmt.Sprint(params["message"]))
				if errText != "" && errText != "<nil>" {
					if onEvent != nil {
						onEvent(StreamEvent{Type: "error", ThreadID: s.threadID, TurnID: turnID, Error: errText})
					}
					return "", "", nil, errors.New(errText)
				}
			}
		}
		trimmed := strings.TrimSpace(message)
		if onEvent != nil && trimmed != "" && trimmed != previousMessage {
			onEvent(StreamEvent{Type: "assistant_message", ThreadID: s.threadID, TurnID: turnID, Message: trimmed, Delta: computeSuffixDelta(previousMessage, trimmed)})
			previousMessage = trimmed
		}
		if turnResponseSeen && turnCompleted {
			final := strings.TrimSpace(message)
			if final == "" {
				if onEvent != nil {
					onEvent(StreamEvent{Type: "error", ThreadID: s.threadID, TurnID: turnID, Error: "app-server returned an empty assistant message"})
				}
				return turnID, "", nil, errors.New("app-server returned an empty assistant message")
			}
			return turnID, final, fileChanges, nil
		}
	}
}

func (s *Session) extractUsage(params map[ // extractUsage looks for token usage data in turn/completed params and updates
// the session's context tracking. Emits a context_usage event if found.
string]interface{}, onEvent func(StreamEvent)) {
	if params == nil {
		return
	}
	usage := findUsageMap(params)
	if usage == nil {
		return
	}
	used := jsonInt64(usage, "input_tokens")
	if used == 0 {
		used = jsonInt64(usage, "prompt_tokens")
	}
	if used == 0 {
		used = jsonInt64(usage, "total_tokens")
	}
	max := jsonInt64(usage, "max_tokens")
	if max == 0 {
		max = jsonInt64(usage, "context_window")
	}
	if used > 0 {
		s.ContextUsed = used
	}
	if max > 0 {
		s.ContextMax = max
	}
	if onEvent != nil && s.ContextUsed > 0 {
		onEvent(StreamEvent{Type: "context_usage", ThreadID: s.threadID, ContextUsed: s.ContextUsed, ContextMax: s.ContextMax})
	}
}

func findUsageMap(m map[string]interface{}) map[string]interface{} {
	if u, ok := m["usage"].(map[string]interface{}); ok {
		return u
	}
	if turn, ok := m["turn"].(map[string]interface{}); ok {
		if u, ok := turn["usage"].(map[string]interface{}); ok {
			return u
		}
	}
	if result, ok := m["result"].(map[string]interface{}); ok {
		if u, ok := result["usage"].(map[string]interface{}); ok {
			return u
		}
	}
	return nil
}

func jsonInt64(m map[string]interface{}, key string) int64 {
	v, ok := m[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	default:
		return 0
	}
}

func (s *Session) allocID() int {
	s.nextID++
	return s.nextID - 1
}

func (s *Session) writeJSON(ctx context.Context, payload interface{}) error {
	if err := setWriteDeadline(ctx, s.conn); err != nil {
		return err
	}
	return s.conn.WriteJSON(payload)
}

func (s *Session) waitForResponse(ctx context.Context, id int) (map[string]interface{}, error) {
	for {
		msg, err := readJSON(ctx, s.conn)
		if err != nil {
			return nil, err
		}
		msgID, hasID := jsonRPCID(msg)
		if !hasID || msgID != id {
			continue
		}
		if errObj, ok := msg["error"].(map[string]interface{}); ok && errObj != nil {
			return nil, fmt.Errorf("rpc error: %s", strings.TrimSpace(fmt.Sprint(errObj["message"])))
		}
		return msg, nil
	}
}

func (s *Session) markClosed() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
}

func cloneStringInterfaceMap(src map[string]interface{}) map[string]interface{} {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]interface{}, len(src))
	for key, value := range src {
		if strings.TrimSpace(key) == "" {
			continue
		}
		out[key] = value
	}
	return out
}

func mergeStringInterfaceParams(dst map[string]interface{}, extra map[string]interface{}) map[string]interface{} {
	if len(extra) == 0 {
		return dst
	}
	if dst == nil {
		dst = make(map[string]interface{}, len(extra))
	}
	for key, value := range extra {
		if strings.TrimSpace(key) == "" {
			continue
		}
		dst[key] = value
	}
	return dst
}
