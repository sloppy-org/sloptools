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
	LlamacppBaseURL   = "http://10.77.0.20:8080"
	llamacppMaxRounds = 5
	llamacppMaxTokens = 4096
)

// LlamacppBackend calls the slopgate HTTP API directly (OpenAI-compatible
// /v1/chat/completions). No subprocess overhead; no opencode cold-start cost.
// When req.MCPAllowList is non-nil and non-empty, it runs an agent loop with
// up to llamacppMaxRounds tool-call rounds over MCP stdio clients.
type LlamacppBackend struct{}

// Provider returns Local.
func (LlamacppBackend) Provider() Provider { return ProviderLocal }

// Name returns the backend identifier used in the ledger.
func (LlamacppBackend) Name() string { return "llamacpp" }

// Run executes the request. Single-shot when MCPAllowList is empty; agent
// loop (≤llamacppMaxRounds) otherwise.
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
		body, err := llamacppPost(ctx, req.Model, modelHeader, messages, nil)
		if err != nil {
			return Response{}, err
		}
		content := chatContent(body)
		if strings.TrimSpace(content) == "" {
			return Response{}, ErrEmptyOutput
		}
		return Response{
			Output: content,
			WallMS: time.Since(start).Milliseconds(),
		}, nil
	}

	// Agent loop with MCP tool calls.
	clients, toolMap, toolDefs, err := startMCPClients(req)
	if err != nil {
		return Response{}, fmt.Errorf("llamacpp: start MCP clients: %w", err)
	}
	defer func() {
		for _, c := range clients {
			c.Close()
		}
	}()

	var lastContent string
	for round := 0; round < llamacppMaxRounds; round++ {
		body, err := llamacppPost(ctx, req.Model, modelHeader, messages, toolDefs)
		if err != nil {
			return Response{}, err
		}

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
		if len(toolCalls) == 0 {
			// No tool calls: model is done.
			if strings.TrimSpace(lastContent) == "" {
				return Response{}, ErrEmptyOutput
			}
			return Response{
				Output: lastContent,
				WallMS: time.Since(start).Milliseconds(),
			}, nil
		}

		// Append assistant message with tool calls.
		messages = append(messages, msg)

		// Execute each tool call.
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

			client, ok := toolMap[toolName]
			var result string
			if !ok {
				result = fmt.Sprintf("error: unknown tool %q", toolName)
			} else {
				result, err = client.Call(toolName, args)
				if err != nil {
					result = fmt.Sprintf("tool error: %v", err)
				}
			}

			messages = append(messages, map[string]interface{}{
				"role":         "tool",
				"tool_call_id": tcID,
				"content":      result,
			})
		}
	}

	// Round limit reached: return whatever we have.
	if strings.TrimSpace(lastContent) == "" {
		return Response{}, ErrEmptyOutput
	}
	return Response{
		Output: lastContent,
		WallMS: time.Since(start).Milliseconds(),
	}, nil
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
			// Close already-started clients before returning.
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

// llamacppPost sends one /v1/chat/completions request and returns the
// decoded response body. tools may be nil for single-shot calls.
func llamacppPost(ctx context.Context, model, modelHeader string, messages []map[string]interface{}, tools []interface{}) (map[string]interface{}, error) {
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
