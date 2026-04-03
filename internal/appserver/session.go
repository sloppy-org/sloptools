package appserver

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
)

// Session maintains a persistent WebSocket connection to the codex app server,
// reusing a single thread across multiple turns.
type Session struct {
	client       *Client
	conn         *websocket.Conn
	threadID     string
	model        string
	threadParams map[string]interface{}
	cwd          string
	mu           sync.Mutex
	closed       bool
	nextID       int

	// Context tracking (updated from turn/completed usage data).
	ContextUsed int64
	ContextMax  int64
}

// OpenSession connects to the app server, performs the initialize handshake,
// and starts a new thread. The returned Session can be used for multiple turns.
func (c *Client) OpenSession(ctx context.Context, cwd, model string) (*Session, error) {
	return c.OpenSessionWithParams(ctx, cwd, model, nil)
}

// OpenSessionWithParams connects to the app server, performs the initialize handshake,
// and starts a new thread with additional thread-level params.
func (c *Client) OpenSessionWithParams(ctx context.Context, cwd, model string, threadParams map[string]interface{}) (*Session, error) {
	return c.openSession(ctx, cwd, model, threadParams, "")
}

// ResumeSessionWithParams connects to the app server and resumes an existing
// thread by ID. If the thread cannot be resumed, it falls back to starting a
// new thread.
func (c *Client) ResumeSessionWithParams(ctx context.Context, cwd, model string, threadParams map[string]interface{}, existingThreadID string) (*Session, bool, error) {
	s, err := c.openSession(ctx, cwd, model, threadParams, existingThreadID)
	if err != nil {
		return nil, false, err
	}
	resumed := s.threadID == existingThreadID
	return s, resumed, nil
}

func (c *Client) openSession(ctx context.Context, cwd, model string, threadParams map[string]interface{}, resumeThreadID string) (*Session, error) {
	if c == nil {
		return nil, errors.New("app-server client is nil")
	}
	if strings.TrimSpace(c.URL) == "" {
		return nil, errors.New("app-server URL is empty")
	}

	dialer := c.Dialer
	if dialer == nil {
		dialer = websocket.DefaultDialer
	}
	conn, _, err := dialer.DialContext(ctx, c.URL, nil)
	if err != nil {
		return nil, err
	}

	s := &Session{
		client:       c,
		conn:         conn,
		model:        strings.TrimSpace(model),
		threadParams: cloneStringInterfaceMap(threadParams),
		cwd:          strings.TrimSpace(cwd),
		nextID:       1,
	}

	if err := s.handshake(ctx); err != nil {
		conn.Close()
		return nil, err
	}

	threadID, err := s.startThread(ctx, strings.TrimSpace(resumeThreadID))
	if err != nil {
		conn.Close()
		return nil, err
	}
	s.threadID = threadID
	return s, nil
}

func (s *Session) handshake(ctx context.Context) error {
	initID := s.allocID()
	if err := s.writeJSON(ctx, map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      initID,
		"method":  "initialize",
		"params": map[string]interface{}{
			"clientInfo": map[string]interface{}{
				"name":    "sloppy",
				"title":   "Slopshell Web",
				"version": "0.2.1",
			},
			"capabilities": map[string]interface{}{
				"experimentalApi": true,
			},
		},
	}); err != nil {
		return fmt.Errorf("initialize send: %w", err)
	}
	if _, err := s.waitForResponse(ctx, initID); err != nil {
		return fmt.Errorf("initialize failed: %w", err)
	}
	if err := s.writeJSON(ctx, map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "initialized",
	}); err != nil {
		return fmt.Errorf("initialized send: %w", err)
	}
	return nil
}

func (s *Session) startThread(ctx context.Context, resumeThreadID string) (string, error) {
	reqID := s.allocID()
	params := applyDefaultApprovalPolicy(map[string]interface{}{
		"cwd":                    s.cwd,
		"sandbox":                "danger-full-access",
		"experimentalRawEvents":  false,
		"persistExtendedHistory": true,
		"ephemeral":              false,
	})
	params = mergeStringInterfaceParams(params, s.threadParams)
	params["approvalPolicy"] = normalizeApprovalPolicy(fmt.Sprint(params["approvalPolicy"]))
	if s.model != "" {
		params["model"] = s.model
	}
	if resumeThreadID != "" {
		params["threadId"] = resumeThreadID
	}
	if err := s.writeJSON(ctx, map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      reqID,
		"method":  "thread/start",
		"params":  params,
	}); err != nil {
		return "", fmt.Errorf("thread/start send: %w", err)
	}
	resp, err := s.waitForResponse(ctx, reqID)
	if err != nil {
		return "", fmt.Errorf("thread/start failed: %w", err)
	}
	id := parseThreadID(resp)
	if id == "" {
		return "", errors.New("thread/start missing thread id")
	}
	return id, nil
}

// ThreadID returns the app server thread ID for this session.
func (s *Session) ThreadID() string { return s.threadID }

func (s *Session) MatchesConfig(cwd, model string, threadParams map[string]interface{}) bool {
	if s == nil {
		return false
	}
	expected := cloneStringInterfaceMap(threadParams)
	expected = applyDefaultApprovalPolicy(expected)
	expected["approvalPolicy"] = normalizeApprovalPolicy(fmt.Sprint(expected["approvalPolicy"]))
	actual := cloneStringInterfaceMap(s.threadParams)
	actual = applyDefaultApprovalPolicy(actual)
	actual["approvalPolicy"] = normalizeApprovalPolicy(fmt.Sprint(actual["approvalPolicy"]))
	return strings.TrimSpace(s.cwd) == strings.TrimSpace(cwd) &&
		strings.TrimSpace(s.model) == strings.TrimSpace(model) &&
		reflect.DeepEqual(actual, expected)
}

// IsOpen returns true if the session connection is still usable.
func (s *Session) IsOpen() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.closed && s.conn != nil
}

// Close tears down the underlying connection.
func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	if s.conn != nil {
		return s.conn.Close()
	}
	return nil
}

// SendTurn sends a single turn on the persistent thread and reads events until
// turn/completed. The onEvent callback receives streaming events including
// context_usage for token tracking.
func (s *Session) SendTurn(ctx context.Context, prompt, turnModel string, onEvent func(StreamEvent)) (*PromptResponse, error) {
	return s.SendTurnWithParams(ctx, prompt, turnModel, nil, onEvent)
}

func (s *Session) SendTurnInputWithParams(ctx context.Context, turnInput []map[string]interface{}, turnModel string, turnParams map[string]interface{}, onEvent func(StreamEvent)) (*PromptResponse, error) {
	s.mu.Lock()
	if s.closed || s.conn == nil {
		s.mu.Unlock()
		return nil, errors.New("session is closed")
	}
	s.mu.Unlock()

	if len(turnInput) == 0 {
		return nil, errors.New("turn input is required")
	}

	turnRPCID := s.allocID()
	baseTurnParams := map[string]interface{}{
		"threadId": s.threadID,
		"input":    turnInput,
	}
	turnParams = mergeStringInterfaceParams(baseTurnParams, turnParams)
	if strings.TrimSpace(turnModel) != "" {
		turnParams["model"] = strings.TrimSpace(turnModel)
	}
	if err := s.writeJSON(ctx, map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      turnRPCID,
		"method":  "turn/start",
		"params":  turnParams,
	}); err != nil {
		s.markClosed()
		return nil, contextErr(ctx, err)
	}
	stopCloseOnCancel := context.AfterFunc(ctx, func() {
		_ = s.Close()
	})
	defer func() {
		_ = stopCloseOnCancel()
	}()

	if onEvent != nil {
		onEvent(StreamEvent{Type: "thread_started", ThreadID: s.threadID})
	}

	turnID, message, fileChanges, err := s.readTurnUntilComplete(ctx, turnRPCID, onEvent)
	if err != nil {
		s.markClosed()
		return nil, contextErr(ctx, err)
	}
	return &PromptResponse{
		ThreadID:    s.threadID,
		TurnID:      turnID,
		Message:     message,
		FileChanges: fileChanges,
	}, nil
}

// SendTurnWithParams sends a single turn on the persistent thread and reads events until
// turn/completed. The onEvent callback receives streaming events including
// context_usage for token tracking.
func (s *Session) SendTurnWithParams(ctx context.Context, prompt, turnModel string, turnParams map[string]interface{}, onEvent func(StreamEvent)) (*PromptResponse, error) {
	return s.SendTurnInputWithParams(ctx, DefaultTurnInput(prompt), turnModel, turnParams, onEvent)
}

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
			ev := StreamEvent{
				Type:     "approval_request",
				ThreadID: s.threadID,
				TurnID:   turnID,
				Approval: approvalReq,
			}
			ev.Respond = func(decision string) error {
				return s.writeJSON(ctx, map[string]interface{}{
					"jsonrpc": "2.0",
					"id":      approvalReq.ID,
					"result": map[string]interface{}{
						"decision": normalizeApprovalDecision(decision),
					},
				})
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
					onEvent(StreamEvent{
						Type:     "item_completed",
						ThreadID: s.threadID,
						TurnID:   turnID,
						Message:  typ,
						Detail:   detail,
					})
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
			onEvent(StreamEvent{
				Type:     "assistant_message",
				ThreadID: s.threadID,
				TurnID:   turnID,
				Message:  trimmed,
				Delta:    computeSuffixDelta(previousMessage, trimmed),
			})
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

// extractUsage looks for token usage data in turn/completed params and updates
// the session's context tracking. Emits a context_usage event if found.
func (s *Session) extractUsage(params map[string]interface{}, onEvent func(StreamEvent)) {
	if params == nil {
		return
	}
	// Try multiple paths where usage data may appear.
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
		onEvent(StreamEvent{
			Type:        "context_usage",
			ThreadID:    s.threadID,
			ContextUsed: s.ContextUsed,
			ContextMax:  s.ContextMax,
		})
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
