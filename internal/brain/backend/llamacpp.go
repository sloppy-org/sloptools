package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	// LlamacppBaseURL is the slopgate HTTP endpoint.
	LlamacppBaseURL = "http://10.77.0.20:8080"
	// llamacppMaxTokens is the per-request output token budget.
	llamacppMaxTokens = 4096
	// llamacppAgentBudget is the wall-clock ceiling for the full agent loop.
	// Matches opencode behaviour: no round limit, just a time budget. The
	// model runs until it naturally stops making tool calls and produces text.
	llamacppAgentBudget = 30 * time.Minute
	// perToolBreakThreshold opens the circuit for a single tool after this many
	// consecutive identical failures, preventing the model from retrying a broken
	// tool indefinitely within one run.
	perToolBreakThreshold = 3
	// globalBreakThreshold aborts the agent loop after this many total consecutive
	// tool errors (across all tools), capping runaway failure cascades.
	globalBreakThreshold = 6
)

// toolFailureTracker counts consecutive identical failures per tool within one
// agent-loop run. It is not shared across runs.
type toolFailureTracker struct {
	consecutive map[string]int
	lastClass   map[string]string
	globalTotal int
}

func newToolFailureTracker() *toolFailureTracker {
	return &toolFailureTracker{
		consecutive: make(map[string]int),
		lastClass:   make(map[string]string),
	}
}

// recordSuccess resets the per-tool and global counters for name.
func (t *toolFailureTracker) recordSuccess(name string) {
	t.consecutive[name] = 0
	t.lastClass[name] = ""
	t.globalTotal = 0
}

// recordFailure increments the consecutive counter for name+class and returns
// true when the per-tool circuit-break threshold is reached.
func (t *toolFailureTracker) recordFailure(name, class string) bool {
	if t.lastClass[name] == class {
		t.consecutive[name]++
	} else {
		t.consecutive[name] = 1
		t.lastClass[name] = class
	}
	t.globalTotal++
	return t.consecutive[name] >= perToolBreakThreshold
}

// globalTripped returns true when total consecutive failures across all tools
// exceed the global threshold.
func (t *toolFailureTracker) globalTripped() bool {
	return t.globalTotal >= globalBreakThreshold
}

// errorClassShort extracts the short error prefix used as a circuit-breaker key
// (head before first ':' or '\n', capped at 80 bytes).
func errorClassShort(msg string) string {
	if i := strings.IndexAny(msg, ":\n"); i > 0 && i <= 80 {
		return strings.TrimSpace(msg[:i])
	}
	if len(msg) > 80 {
		return msg[:80]
	}
	return msg
}

// LlamacppBackend calls the slopgate HTTP API directly (OpenAI-compatible
// /v1/chat/completions). No subprocess overhead; no opencode cold-start cost.
// When req.MCPAllowList is non-nil and non-empty, it runs an agent loop with
// no round limit (mirrors opencode behaviour) bounded only by llamacppAgentBudget.
//
// Qwen3 MoE at slopgate may return tool calls in two formats:
//   - Standard OpenAI: tool_calls JSON field on the message
//   - Qwen native XML: <tool_call>...</tool_call> blocks in the content field
//
// Both are detected and routed through MCPClient.
type LlamacppBackend struct{}

// Provider returns Local.
func (LlamacppBackend) Provider() Provider { return ProviderLocal }

// Name returns the backend identifier used in the ledger.
func (LlamacppBackend) Name() string { return "llamacpp" }

// Run executes the request. Single-shot when MCPAllowList is empty; agent
// loop bounded by llamacppAgentBudget (wall clock) otherwise.
func (LlamacppBackend) Run(ctx context.Context, req Request) (Response, error) {
	if err := req.validate(); err != nil {
		return Response{}, err
	}
	systemPrompt, err := os.ReadFile(req.SystemPromptPath)
	if err != nil {
		return Response{}, fmt.Errorf("llamacpp: read system prompt: %w", err)
	}
	messages := []map[string]interface{}{
		{"role": "system", "content": string(systemPrompt)},
		{"role": "user", "content": req.Packet},
	}
	modelHeader := llamacppModelHeader(req.Model)
	start := time.Now()

	if len(req.MCPAllowList) == 0 {
		return runLlamacppSingleShot(ctx, req.Model, modelHeader, req.Affinity, messages, start)
	}
	clients, toolMap, toolDefs, err := startMCPClients(req)
	if err != nil {
		return Response{}, fmt.Errorf("llamacpp: start MCP clients: %w", err)
	}
	defer func() {
		for _, c := range clients {
			c.Close()
		}
	}()
	return runLlamacppAgentLoop(ctx, req.Model, modelHeader, req.Affinity, messages, toolMap, toolDefs, start)
}

func runLlamacppSingleShot(ctx context.Context, model, modelHeader, affinity string,
	messages []map[string]interface{}, start time.Time) (Response, error) {
	body, err := llamacppPost(ctx, model, modelHeader, affinity, messages, nil)
	if err != nil {
		return Response{}, err
	}
	content := chatContent(body)
	if strings.TrimSpace(content) == "" {
		return Response{}, ErrEmptyOutput
	}
	ti, to := extractUsage(body)
	return Response{Output: content, TokensIn: ti, TokensOut: to, WallMS: time.Since(start).Milliseconds()}, nil
}

func runLlamacppAgentLoop(ctx context.Context, model, modelHeader, affinity string,
	messages []map[string]interface{}, toolMap map[string]*MCPClient, toolDefs []interface{},
	start time.Time) (Response, error) {
	var (
		lastContent    string
		totalTokensIn  int64
		totalTokensOut int64
	)
	deadline := start.Add(llamacppAgentBudget)
	tracker := newToolFailureTracker()
	for {
		if time.Now().After(deadline) {
			break
		}
		body, err := llamacppPost(ctx, model, modelHeader, affinity, messages, toolDefs)
		if err != nil {
			return Response{}, err
		}
		ti, to := extractUsage(body)
		totalTokensIn += ti
		totalTokensOut += to

		choice := firstChoice(body)
		if choice == nil {
			break
		}
		msg, _ := choice["message"].(map[string]interface{})
		if msg == nil {
			break
		}
		if c, ok := msg["content"].(string); ok {
			lastContent = c
		}

		toolCalls, _ := msg["tool_calls"].([]interface{})
		xmlCalls := parseQwenXMLCalls(lastContent)

		if len(toolCalls) == 0 && len(xmlCalls) == 0 {
			// No tool calls: model produced its final answer.
			clean := stripXMLToolCalls(lastContent)
			if strings.TrimSpace(clean) == "" {
				return Response{}, ErrEmptyOutput
			}
			return Response{
				Output:    clean,
				TokensIn:  totalTokensIn,
				TokensOut: totalTokensOut,
				WallMS:    time.Since(start).Milliseconds(),
			}, nil
		}

		messages = append(messages, msg)

		if len(toolCalls) > 0 {
			executeOpenAIToolCalls(toolCalls, toolMap, &messages, tracker)
		} else {
			executeQwenXMLToolCalls(xmlCalls, toolMap, &messages, tracker)
		}
		if tracker.globalTripped() {
			break
		}
	}

	// Budget exhausted: return whatever the model last produced.
	clean := stripXMLToolCalls(lastContent)
	if strings.TrimSpace(clean) == "" {
		return Response{}, ErrEmptyOutput
	}
	return Response{
		Output:    clean,
		TokensIn:  totalTokensIn,
		TokensOut: totalTokensOut,
		WallMS:    time.Since(start).Milliseconds(),
	}, nil
}

// executeOpenAIToolCalls runs standard OpenAI tool_calls entries through MCPClient
// and appends role=tool messages to messages.
func executeOpenAIToolCalls(toolCalls []interface{}, toolMap map[string]*MCPClient, messages *[]map[string]interface{}, tracker *toolFailureTracker) {
	for _, rawTC := range toolCalls {
		tc, _ := rawTC.(map[string]interface{})
		if tc == nil {
			continue
		}
		tcID, _ := tc["id"].(string)
		fn, _ := tc["function"].(map[string]interface{})
		if fn == nil {
			continue
		}
		toolName, _ := fn["name"].(string)
		argsStr, _ := fn["arguments"].(string)
		var args map[string]interface{}
		if argsStr != "" {
			_ = json.Unmarshal([]byte(argsStr), &args)
		}
		result := callToolSafe(toolMap, toolName, args, tracker)
		*messages = append(*messages, map[string]interface{}{
			"role":         "tool",
			"tool_call_id": tcID,
			"content":      result,
		})
	}
}

// executeQwenXMLToolCalls runs parsed qwen XML tool calls and appends a single
// role=user message containing all <tool_response> blocks.
func executeQwenXMLToolCalls(xmlCalls []qwenXMLCall, toolMap map[string]*MCPClient, messages *[]map[string]interface{}, tracker *toolFailureTracker) {
	var sb strings.Builder
	for _, tc := range xmlCalls {
		result := callToolSafe(toolMap, tc.Name, tc.Args, tracker)
		fmt.Fprintf(&sb, "<tool_response>\n<function_name>%s</function_name>\n<output>%s</output>\n</tool_response>\n\n", tc.Name, result)
	}
	*messages = append(*messages, map[string]interface{}{
		"role":    "user",
		"content": strings.TrimSpace(sb.String()),
	})
}

// toolResultCap is the maximum byte length of a single tool result appended
// to the messages history. Brain actions (gtd_list, projects_render, search)
// return hundreds of KB of vault content; without a cap the accumulated
// messages array overflows the model context window across rounds.
const toolResultCap = 8 * 1024

// callToolSafe invokes the named tool; returns an error string on failure so
// the model sees what went wrong without aborting the agent loop. Results
// are capped at toolResultCap bytes so the messages array stays bounded.
// tracker accumulates consecutive identical failures; when the per-tool
// threshold is reached the circuit opens and the tool is refused without
// calling the MCP client again.
func callToolSafe(toolMap map[string]*MCPClient, name string, args map[string]interface{}, tracker *toolFailureTracker) string {
	if tracker != nil && tracker.consecutive[name] >= perToolBreakThreshold {
		return fmt.Sprintf("tool error (circuit open): %q has failed with %q %d times in a row; do not call it again this run, try a different approach or finish", name, tracker.lastClass[name], tracker.consecutive[name])
	}
	client, ok := toolMap[name]
	if !ok {
		class := "unknown tool"
		if tracker != nil && tracker.recordFailure(name, class) {
			return fmt.Sprintf("tool error (circuit open): %q has returned %q %d times in a row; do not call it again this run, try a different approach or finish", name, class, tracker.consecutive[name])
		}
		return fmt.Sprintf("error: unknown tool %q", name)
	}
	result, err := client.Call(name, args)
	if err != nil {
		class := errorClassShort(err.Error())
		if tracker != nil && tracker.recordFailure(name, class) {
			return fmt.Sprintf("tool error (circuit open): %q has returned %q %d times in a row; do not call it again this run, try a different approach or finish", name, class, tracker.consecutive[name])
		}
		return fmt.Sprintf("tool error: %v", err)
	}
	if tracker != nil {
		tracker.recordSuccess(name)
	}
	if len(result) > toolResultCap {
		result = result[:toolResultCap] + fmt.Sprintf("\n[truncated: %d bytes omitted]", len(result)-toolResultCap)
	}
	return result
}

// startMCPClients launches the sloppy + helpy MCP servers and filters tools
// to the MCPAllowList. Returns the clients (caller must Close), a map of
// toolName → *MCPClient, and the OpenAI tools array.
func startMCPClients(req Request) ([]*MCPClient, map[string]*MCPClient, []interface{}, error) {
	servers := req.Sandbox.MCPServersFromFile()
	allowSet := make(map[string]bool, len(req.MCPAllowList))
	for _, n := range req.MCPAllowList {
		allowSet[n] = true
	}
	var clients []*MCPClient
	toolMap := make(map[string]*MCPClient)
	var toolDefs []interface{}
	for serverName, spec := range servers {
		_ = serverName
		c, err := NewMCPClient(spec, req.Sandbox.Env())
		if err != nil {
			for _, existing := range clients {
				existing.Close()
			}
			return nil, nil, nil, fmt.Errorf("start server %s: %w", spec.Command, err)
		}
		clients = append(clients, c)
		tools, err := c.ListTools()
		if err != nil {
			for _, existing := range clients {
				existing.Close()
			}
			return nil, nil, nil, fmt.Errorf("list tools from %s: %w", spec.Command, err)
		}
		for _, td := range tools {
			if !allowSet[td.Name] {
				continue
			}
			toolMap[td.Name] = c
			schema := td.InputSchema
			if schema == nil {
				schema = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
			}
			toolDefs = append(toolDefs, map[string]interface{}{
				"type": "function",
				"function": map[string]interface{}{
					"name":        td.Name,
					"description": td.Description,
					"parameters":  schema,
				},
			})
		}
	}
	return clients, toolMap, toolDefs, nil
}

// llamacppPost sends one /v1/chat/completions request and returns the decoded
// response body. tools may be nil for single-shot or forced-text calls.
func llamacppPost(ctx context.Context, model, modelHeader, affinity string,
	messages []map[string]interface{}, tools []interface{}) (map[string]interface{}, error) {
	payload := map[string]interface{}{
		"model":      model,
		"messages":   messages,
		"max_tokens": llamacppMaxTokens,
	}
	if len(tools) > 0 {
		payload["tools"] = tools
		payload["tool_choice"] = "auto"
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("llamacpp: marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		LlamacppBaseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("llamacpp: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if modelHeader != "" {
		httpReq.Header.Set("x-model", modelHeader)
	}
	if affinity != "" {
		httpReq.Header.Set("x-session-affinity", affinity)
	}
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("llamacpp: HTTP: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("llamacpp: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		snippet := string(respBody)
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		return nil, fmt.Errorf("llamacpp: HTTP %d: %s", resp.StatusCode, snippet)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("llamacpp: decode response: %w", err)
	}
	return out, nil
}

// llamacppModelHeader derives the x-model header value from req.Model.
// "llamacpp-moe/qwen" → "qwen"; "llamacpp/qwen27b" → "qwen27b".
func llamacppModelHeader(model string) string {
	if idx := strings.LastIndex(model, "/"); idx >= 0 {
		return model[idx+1:]
	}
	return model
}

// firstChoice extracts choices[0] from a completions response.
func firstChoice(body map[string]interface{}) map[string]interface{} {
	choices, _ := body["choices"].([]interface{})
	if len(choices) == 0 {
		return nil
	}
	c, _ := choices[0].(map[string]interface{})
	return c
}

// chatContent extracts the assistant text from choices[0].message.content.
func chatContent(body map[string]interface{}) string {
	choice := firstChoice(body)
	if choice == nil {
		return ""
	}
	msg, _ := choice["message"].(map[string]interface{})
	if msg == nil {
		return ""
	}
	c, _ := msg["content"].(string)
	return c
}

// extractUsage returns prompt and completion token counts from the response
// usage field. Returns zeros when the field is absent.
func extractUsage(body map[string]interface{}) (tokensIn, tokensOut int64) {
	usage, _ := body["usage"].(map[string]interface{})
	if usage == nil {
		return 0, 0
	}
	if v, ok := usage["prompt_tokens"].(float64); ok {
		tokensIn = int64(v)
	}
	if v, ok := usage["completion_tokens"].(float64); ok {
		tokensOut = int64(v)
	}
	return tokensIn, tokensOut
}
