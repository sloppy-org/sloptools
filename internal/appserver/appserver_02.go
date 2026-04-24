package appserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gorilla/websocket"
	"reflect"
	"strings"
	"sync"
	"time"
)

func extractItemDetail(item map[string]interface{}) string {
	if item == nil {
		return ""
	}
	for _, key := range []string{"detail", "summary", "command", "cmd", "message", "text", "path", "file", "filename", "reasoning", "reason", "output"} {
		if text := stringifyItemValue(item[key]); text != "" {
			return normalizeItemDetailText(text)
		}
	}
	if toolName := stringifyItemValue(item["tool_name"]); toolName != "" {
		if args := stringifyItemValue(item["arguments"]); args != "" {
			return normalizeItemDetailText(toolName + " " + args)
		}
		return normalizeItemDetailText(toolName)
	}
	if tool, _ := item["tool"].(map[string]interface{}); tool != nil {
		toolName := stringifyItemValue(tool["name"])
		if toolName == "" {
			toolName = stringifyItemValue(tool["tool_name"])
		}
		if toolName != "" {
			if args := stringifyItemValue(tool["arguments"]); args != "" {
				return normalizeItemDetailText(toolName + " " + args)
			}
			return normalizeItemDetailText(toolName)
		}
	}
	snapshot := map[string]interface{}{}
	for k, v := range item {
		switch k {
		case "type", "id", "status", "timestamp", "started_at", "completed_at":
			continue
		default:
			snapshot[k] = v
		}
	}
	if len(snapshot) == 0 {
		return ""
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return ""
	}
	return normalizeItemDetailText(string(raw))
}

func stringifyItemValue(value interface{}) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	case float64, float32, int, int64, int32, int16, int8, uint, uint64, uint32, uint16, uint8, bool:
		return strings.TrimSpace(fmt.Sprint(v))
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return strings.TrimSpace(fmt.Sprint(v))
		}
		return strings.TrimSpace(string(raw))
	}
}

func normalizeItemDetailText(text string) string {
	clean := strings.TrimSpace(strings.Join(strings.Fields(strings.TrimSpace(text)), " "))
	if clean == "" || clean == "<nil>" {
		return ""
	}
	const maxLen = 480
	if len(clean) > maxLen {
		return clean[:maxLen] + "..."
	}
	return clean
}

func computeSuffixDelta(previous, current string) string {
	if previous == "" {
		return current
	}
	i := 0
	max := len(previous)
	if len(current) < max {
		max = len(current)
	}
	for i < max && previous[i] == current[i] {
		i++
	}
	return current[i:]
}

func parseThreadID(msg map[string]interface{}) string {
	result, _ := msg["result"].(map[string]interface{})
	if result == nil {
		return ""
	}
	thread, _ := result["thread"].(map[string]interface{})
	if thread == nil {
		return ""
	}
	id, _ := thread["id"].(string)
	return strings.TrimSpace(id)
}

func jsonRPCID(msg map[string]interface{}) (int, bool) {
	id, ok := msg["id"]
	if !ok {
		return 0, false
	}
	switch v := id.(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	case json.Number:
		i, err := v.Int64()
		if err != nil {
			return 0, false
		}
		return int(i), true
	case string:
		return 0, false
	default:
		return 0, false
	}
}

func readJSON(ctx context.Context, conn *websocket.Conn) (map[string]interface{}, error) {
	if err := setReadDeadline(ctx, conn); err != nil {
		return nil, err
	}
	_, data, err := conn.ReadMessage()
	if err != nil {
		return nil, err
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func setReadDeadline(ctx context.Context, conn *websocket.Conn) error {
	if deadline, ok := ctx.Deadline(); ok {
		return conn.SetReadDeadline(deadline)
	}
	return conn.SetReadDeadline(time.Time{})
}

func setWriteDeadline(ctx context.Context, conn *websocket.Conn) error {
	if deadline, ok := ctx.Deadline(); ok {
		return conn.SetWriteDeadline(deadline)
	}
	return conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
}

type TurnInputItem struct {
	Type         string
	Text         string
	ImageURL     string
	TextElements []interface{}
}

func DefaultTurnInput(prompt string) []map[string]interface{} {
	return BuildTurnInput([]TurnInputItem{{Type: "text", Text: prompt, TextElements: []interface{}{}}})
}

func BuildTurnInput(items []TurnInputItem) []map[string]interface{} {
	if len(items) == 0 {
		return nil
	}
	out := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		switch strings.ToLower(strings.TrimSpace(item.Type)) {
		case "", "text":
			text := strings.TrimSpace(item.Text)
			if text == "" {
				continue
			}
			textElements := item.TextElements
			if textElements == nil {
				textElements = []interface{}{}
			}
			out = append(out, map[string]interface{}{"type": "text", "text": text, "text_elements": textElements})
		case "image_url":
			imageURL := strings.TrimSpace(item.ImageURL)
			if imageURL == "" {
				continue
			}
			out = append(out, map[string]interface{}{"type": "image_url", "image_url": imageURL})
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

type Session struct {
	client       *Client
	conn         *websocket.Conn
	threadID     string
	model        string
	threadParams map[ // Session maintains a persistent WebSocket connection to the codex app server,
	// reusing a single thread across multiple turns.
	string]interface{}
	cwd         string
	mu          sync.Mutex
	closed      bool
	nextID      int
	ContextUsed int64
	ContextMax  int64
} // Context tracking (updated from turn/completed usage data).

func (c *Client) OpenSession(ctx context.Context, cwd, model string) (*Session, error) {
	return c.OpenSessionWithParams(ctx, cwd, model, nil)
} // OpenSession connects to the app server, performs the initialize handshake,
// and starts a new thread. The returned Session can be used for multiple turns.

func (c *Client) OpenSessionWithParams(ctx context.Context, cwd, model string, threadParams map[ // OpenSessionWithParams connects to the app server, performs the initialize handshake,
// and starts a new thread with additional thread-level params.
string]interface{}) (*Session, error) {
	return c.openSession(ctx, cwd, model, threadParams, "")
}

func (c *Client) ResumeSessionWithParams(ctx context.Context, cwd, model string, threadParams map[ // ResumeSessionWithParams connects to the app server and resumes an existing
// thread by ID. If the thread cannot be resumed, it falls back to starting a
// new thread.
string]interface{}, existingThreadID string) (*Session, bool, error) {
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
	s := &Session{client: c, conn: conn, model: strings.TrimSpace(model), threadParams: cloneStringInterfaceMap(threadParams), cwd: strings.TrimSpace(cwd), nextID: 1}
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
	if err := s.writeJSON(ctx, map[string]interface{}{"jsonrpc": "2.0", "id": initID, "method": "initialize", "params": map[string]interface{}{"clientInfo": map[string]interface{}{"name": "sloptools", "title": "Slopshell Web", "version": "0.2.1"}, "capabilities": map[string]interface{}{"experimentalApi": true}}}); err != nil {
		return fmt.Errorf("initialize send: %w", err)
	}
	if _, err := s.waitForResponse(ctx, initID); err != nil {
		return fmt.Errorf("initialize failed: %w", err)
	}
	if err := s.writeJSON(ctx, map[string]interface{}{"jsonrpc": "2.0", "method": "initialized"}); err != nil {
		return fmt.Errorf("initialized send: %w", err)
	}
	return nil
}

func (s *Session) startThread(ctx context.Context, resumeThreadID string) (string, error) {
	reqID := s.allocID()
	params := applyDefaultApprovalPolicy(map[string]interface{}{"cwd": s.cwd, "sandbox": "danger-full-access", "experimentalRawEvents": false, "persistExtendedHistory": true, "ephemeral": false})
	params = mergeStringInterfaceParams(params, s.threadParams)
	params["approvalPolicy"] = normalizeApprovalPolicy(fmt.Sprint(params["approvalPolicy"]))
	if s.model != "" {
		params["model"] = s.model
	}
	if resumeThreadID != "" {
		params["threadId"] = resumeThreadID
	}
	if err := s.writeJSON(ctx, map[string]interface{}{"jsonrpc": "2.0", "id": reqID, "method": "thread/start", "params": params}); err != nil {
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

func (s *Session) ThreadID() string {
	return s.threadID
} // ThreadID returns the app server thread ID for this session.

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
	return strings.TrimSpace(s.cwd) == strings.TrimSpace(cwd) && strings.TrimSpace(s.model) == strings.TrimSpace(model) && reflect.DeepEqual(actual, expected)
}

func (s *Session) IsOpen() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.closed && s.conn != nil
} // IsOpen returns true if the session connection is still usable.

func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	if s.conn != nil {
		return s.conn.Close()
	}
	return nil
} // Close tears down the underlying connection.

func (s *Session) SendTurn(ctx context.Context, prompt, turnModel string, onEvent func(StreamEvent)) (*PromptResponse, error) {
	return s.SendTurnWithParams(ctx, prompt, turnModel, nil, onEvent)
} // SendTurn sends a single turn on the persistent thread and reads events until
// turn/completed. The onEvent callback receives streaming events including
// context_usage for token tracking.

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
	baseTurnParams := map[string]interface{}{"threadId": s.threadID, "input": turnInput}
	turnParams = mergeStringInterfaceParams(baseTurnParams, turnParams)
	if strings.TrimSpace(turnModel) != "" {
		turnParams["model"] = strings.TrimSpace(turnModel)
	}
	if err := s.writeJSON(ctx, map[string]interface{}{"jsonrpc": "2.0", "id": turnRPCID, "method": "turn/start", "params": turnParams}); err != nil {
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
	return &PromptResponse{ThreadID: s.threadID, TurnID: turnID, Message: message, FileChanges: fileChanges}, nil
}

func (s *Session) SendTurnWithParams(ctx context.Context, prompt, turnModel string, turnParams map[ // SendTurnWithParams sends a single turn on the persistent thread and reads events until
// turn/completed. The onEvent callback receives streaming events including
// context_usage for token tracking.
string]interface{}, onEvent func(StreamEvent)) (*PromptResponse, error) {
	return s.SendTurnInputWithParams(ctx, DefaultTurnInput(prompt), turnModel, turnParams, onEvent)
}
