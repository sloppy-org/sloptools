package appserver

import (
	"context"
	"errors"
	"fmt"
	"github.com/gorilla/websocket"
	"net"
	"net/url"
	"strings"
	"time"
)

const (
	ApprovalPolicyNever          = "never"
	ApprovalPolicyOnRequest      = "on-request"
	ApprovalPolicyUnlessTrusted  = "unlessTrusted"
	ApprovalMethodCommandRequest = "item/commandExecution/requestApproval"
	ApprovalMethodFileRequest    = "item/fileChange/requestApproval"
)

type ApprovalRequest struct {
	ID        interface{}
	Method    string
	Kind      string
	ItemID    string
	Reason    string
	GrantRoot string
	Risk      map[string]interface{}
}

func applyDefaultApprovalPolicy(params map[string]interface{}) map[string]interface{} {
	if params == nil {
		params = map[string]interface{}{}
	}
	value, ok := params["approvalPolicy"]
	if !ok || strings.TrimSpace(fmt.Sprint(value)) == "" || strings.EqualFold(strings.TrimSpace(fmt.Sprint(value)), "<nil>") {
		params["approvalPolicy"] = ApprovalPolicyOnRequest
	}
	return params
}

func normalizeApprovalPolicy(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "default", "<nil>":
		return ApprovalPolicyOnRequest
	case ApprovalPolicyNever:
		return ApprovalPolicyNever
	case ApprovalPolicyOnRequest:
		return ApprovalPolicyOnRequest
	case "untrusted", "unless-trusted", "unlesstrusted", "unless_trusted", ApprovalPolicyUnlessTrusted:
		return ApprovalPolicyUnlessTrusted
	default:
		return strings.TrimSpace(raw)
	}
}

func NormalizeApprovalPolicy(raw string) string {
	return normalizeApprovalPolicy(raw)
}

func parseApprovalRequest(msg map[string]interface{}) (*ApprovalRequest, bool) {
	method := strings.TrimSpace(stringifyItemValue(msg["method"]))
	switch method {
	case ApprovalMethodCommandRequest, "execCommandApproval":
		params, _ := msg["params"].(map[string]interface{})
		req := &ApprovalRequest{ID: msg["id"], Method: method, Kind: "command_execution", ItemID: strings.TrimSpace(stringifyItemValue(params["item_id"])), Reason: strings.TrimSpace(stringifyItemValue(params["reason"]))}
		if risk, _ := params["risk"].(map[string]interface{}); risk != nil {
			req.Risk = risk
		}
		return req, true
	case ApprovalMethodFileRequest, "fileChangeApproval":
		params, _ := msg["params"].(map[string]interface{})
		return &ApprovalRequest{ID: msg["id"], Method: method, Kind: "file_change", ItemID: strings.TrimSpace(stringifyItemValue(params["item_id"])), Reason: strings.TrimSpace(stringifyItemValue(params["reason"])), GrantRoot: strings.TrimSpace(stringifyItemValue(params["grant_root"]))}, true
	default:
		return nil, false
	}
}

func normalizeApprovalDecision(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "cancel", "canceled", "cancelled":
		return "cancel"
	case "approve", "approved", "accept", "acceptforsession":
		return "accept"
	case "reject", "rejected", "decline", "denied", "deny":
		return "decline"
	default:
		return "cancel"
	}
}

type Client struct {
	URL    string
	Dialer *websocket.Dialer
}

type PromptRequest struct {
	CWD          string
	Prompt       string
	TurnInput    []map[string]interface{}
	Model        string
	TurnModel    string
	ThreadParams map[ // explicit turn input items; defaults to Prompt as a text input
	// per-turn model override (sent in turn/start if set)
	string]interface{}
	TurnParams map[ // additional params for thread/start
	string]interface{}
	Timeout time.Duration
} // additional params for turn/start

type PromptResponse struct {
	ThreadID    string
	TurnID      string
	Message     string
	FileChanges []string
}

type StreamEvent struct {
	Type        string
	ThreadID    string
	TurnID      string
	Message     string
	Delta       string
	Detail      string
	Error       string
	ContextUsed int64
	ContextMax  int64
	Approval    *ApprovalRequest
	Respond     func(string) error
}

func NewClient(rawURL string) (*Client, error) {
	normalized, err := NormalizeURL(rawURL)
	if err != nil {
		return nil, err
	}
	return &Client{URL: normalized, Dialer: websocket.DefaultDialer}, nil
}

func NormalizeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("app_server_url is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid app_server_url")
	}
	if u.Scheme != "ws" && u.Scheme != "wss" {
		return "", fmt.Errorf("app_server_url must use ws or wss")
	}
	host := strings.TrimSpace(u.Hostname())
	if host == "" {
		return "", fmt.Errorf("app_server_url must include host")
	}
	if !isLoopbackHost(host) {
		return "", fmt.Errorf("app_server_url host must be loopback")
	}
	path := strings.TrimSpace(u.Path)
	if path != "" && path != "/" {
		return "", fmt.Errorf("app_server_url path must be empty or /")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("app_server_url must not include query or fragment")
	}
	if path == "/" {
		u.Path = ""
	}
	return u.String(), nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (c *Client) SendPrompt(ctx context.Context, req PromptRequest) (*PromptResponse, error) {
	return c.SendPromptStream(ctx, req, nil)
}

func (c *Client) SendPromptStream(ctx context.Context, req PromptRequest, onEvent func(StreamEvent)) (*PromptResponse, error) {
	if c == nil {
		return nil, errors.New("app-server client is nil")
	}
	if strings.TrimSpace(c.URL) == "" {
		return nil, errors.New("app-server URL is empty")
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" && len(req.TurnInput) == 0 {
		return nil, errors.New("prompt or turn input is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if req.Timeout > 0 {
		if _, hasDeadline := ctx.Deadline(); !hasDeadline {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, req.Timeout)
			defer cancel()
		}
	}
	dialer := c.Dialer
	if dialer == nil {
		dialer = websocket.DefaultDialer
	}
	conn, _, err := dialer.DialContext(ctx, c.URL, nil)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	ctxWatcherDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-ctxWatcherDone:
		}
	}()
	defer close(ctxWatcherDone)
	if err := c.writeJSON(ctx, conn, map[string]interface{}{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]interface{}{"clientInfo": map[string]interface{}{"name": "sloptools", "title": "Slopshell Web", "version": "0.2.1"}, "capabilities": map[string]interface{}{"experimentalApi": true}}}); err != nil {
		return nil, contextErr(ctx, err)
	}
	if _, err := c.waitForResponse(ctx, conn, 1); err != nil {
		return nil, contextErr(ctx, fmt.Errorf("initialize failed: %w", err))
	}
	if err := c.writeJSON(ctx, conn, map[string]interface{}{"jsonrpc": "2.0", "method": "initialized"}); err != nil {
		return nil, contextErr(ctx, err)
	}
	threadParams := applyDefaultApprovalPolicy(map[string]interface{}{"cwd": strings.TrimSpace(req.CWD), "sandbox": "danger-full-access", "experimentalRawEvents": false, "persistExtendedHistory": true, "ephemeral": false})
	if strings.TrimSpace(req.Model) != "" {
		threadParams["model"] = strings.TrimSpace(req.Model)
	}
	if len(req.ThreadParams) > 0 {
		for key, value := range req.ThreadParams {
			if strings.TrimSpace(key) != "" {
				if strings.EqualFold(strings.TrimSpace(key), "approvalPolicy") {
					threadParams[key] = normalizeApprovalPolicy(fmt.Sprint(value))
					continue
				}
				threadParams[key] = value
			}
		}
	}
	threadParams["approvalPolicy"] = normalizeApprovalPolicy(fmt.Sprint(threadParams["approvalPolicy"]))
	if err := c.writeJSON(ctx, conn, map[string]interface{}{"jsonrpc": "2.0", "id": 2, "method": "thread/start", "params": threadParams}); err != nil {
		return nil, contextErr(ctx, err)
	}
	threadResp, err := c.waitForResponse(ctx, conn, 2)
	if err != nil {
		return nil, contextErr(ctx, fmt.Errorf("thread/start failed: %w", err))
	}
	threadID := parseThreadID(threadResp)
	if threadID == "" {
		return nil, errors.New("thread/start missing thread id")
	}
	if onEvent != nil {
		onEvent(StreamEvent{Type: "thread_started", ThreadID: threadID})
	}
	turnInput := req.TurnInput
	if len(turnInput) == 0 {
		turnInput = DefaultTurnInput(prompt)
	}
	turnParams := map[string]interface{}{"threadId": threadID, "input": turnInput}
	if strings.TrimSpace(req.TurnModel) != "" {
		turnParams["model"] = strings.TrimSpace(req.TurnModel)
	}
	if len(req.TurnParams) > 0 {
		for key, value := range req.TurnParams {
			if strings.TrimSpace(key) != "" {
				turnParams[key] = value
			}
		}
	}
	if err := c.writeJSON(ctx, conn, map[string]interface{}{"jsonrpc": "2.0", "id": 3, "method": "turn/start", "params": turnParams}); err != nil {
		return nil, contextErr(ctx, err)
	}
	turnID, message, fileChanges, err := c.readTurnUntilComplete(ctx, conn, threadID, onEvent)
	if err != nil {
		return nil, contextErr(ctx, err)
	}
	return &PromptResponse{ThreadID: threadID, TurnID: turnID, Message: message, FileChanges: fileChanges}, nil
}

func contextErr(ctx context.Context, err error) error {
	if err == nil || ctx == nil {
		return err
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) <= 200*time.Millisecond {
			return context.DeadlineExceeded
		}
	}
	return err
}

func (c *Client) writeJSON(ctx context.Context, conn *websocket.Conn, payload interface{}) error {
	if err := setWriteDeadline(ctx, conn); err != nil {
		return err
	}
	return conn.WriteJSON(payload)
}

func (c *Client) waitForResponse(ctx context.Context, conn *websocket.Conn, id int) (map[string]interface{}, error) {
	for {
		msg, err := readJSON(ctx, conn)
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

func (c *Client) readTurnUntilComplete(ctx context.Context, conn *websocket.Conn, threadID string, onEvent func(StreamEvent)) (string, string, []string, error) {
	turnResponseSeen := false
	turnID := ""
	message := ""
	previousMessage := ""
	turnCompleted := false
	var fileChanges []string
	for {
		msg, err := readJSON(ctx, conn)
		if err != nil {
			return "", "", nil, err
		}
		if approvalReq, ok := parseApprovalRequest(msg); ok {
			ev := StreamEvent{Type: "approval_request", ThreadID: threadID, TurnID: turnID, Approval: approvalReq}
			ev.Respond = func(decision string) error {
				return c.writeJSON(ctx, conn, map[string]interface{}{"jsonrpc": "2.0", "id": approvalReq.ID, "result": map[string]interface{}{"decision": normalizeApprovalDecision(decision)}})
			}
			if onEvent != nil {
				onEvent(ev)
			} else {
				_ = ev.Respond("cancel")
			}
			continue
		}
		if msgID, hasID := jsonRPCID(msg); hasID && msgID == 3 {
			if errObj, ok := msg["error"].(map[string]interface{}); ok && errObj != nil {
				errText := strings.TrimSpace(fmt.Sprint(errObj["message"]))
				if onEvent != nil {
					onEvent(StreamEvent{Type: "error", ThreadID: threadID, TurnID: turnID, Error: errText})
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
				onEvent(StreamEvent{Type: "turn_started", ThreadID: threadID, TurnID: turnID})
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
					onEvent(StreamEvent{Type: "item_completed", ThreadID: threadID, TurnID: turnID, Message: typ, Detail: detail})
				}
			}
		case "turn/completed":
			turnCompleted = true
			if turn, _ := params["turn"].(map[string]interface{}); turn != nil {
				if id, _ := turn["id"].(string); strings.TrimSpace(id) != "" {
					turnID = id
				}
			}
			if onEvent != nil {
				onEvent(StreamEvent{Type: "turn_completed", ThreadID: threadID, TurnID: turnID, Message: strings.TrimSpace(message)})
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
		case "error":
			if params != nil {
				errText := strings.TrimSpace(fmt.Sprint(params["message"]))
				if errText != "" && errText != "<nil>" {
					if onEvent != nil {
						onEvent(StreamEvent{Type: "error", ThreadID: threadID, TurnID: turnID, Error: errText})
					}
					return "", "", nil, errors.New(errText)
				}
			}
		}
		trimmed := strings.TrimSpace(message)
		if onEvent != nil && trimmed != "" && trimmed != previousMessage {
			onEvent(StreamEvent{Type: "assistant_message", ThreadID: threadID, TurnID: turnID, Message: trimmed, Delta: computeSuffixDelta(previousMessage, trimmed)})
			previousMessage = trimmed
		}
		if turnResponseSeen && turnCompleted {
			final := strings.TrimSpace(message)
			if final == "" {
				if onEvent != nil {
					onEvent(StreamEvent{Type: "error", ThreadID: threadID, TurnID: turnID, Error: "app-server returned an empty assistant message"})
				}
				return turnID, "", nil, errors.New("app-server returned an empty assistant message")
			}
			return turnID, final, fileChanges, nil
		}
	}
}

func extractFileChangePath(item map[string]interface{}) string {
	for _, key := range []string{"file", "path", "filename"} {
		if v, _ := item[key].(string); strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
