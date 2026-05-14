package backend

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sync"
)

// MCPClient is a minimal JSON-RPC 2.0 MCP client over subprocess stdio.
// Requests are serialized: mu is held for the full send+receive cycle.
type MCPClient struct {
	cmd    *exec.Cmd
	enc    *json.Encoder
	dec    *json.Decoder
	mu     sync.Mutex
	nextID int
}

// MCPToolDef is a single tool definition returned by tools/list.
type MCPToolDef struct {
	Name        string
	Description string
	InputSchema map[string]interface{}
}

// NewMCPClient launches the MCP server described by spec, performs the
// JSON-RPC initialize handshake, and returns a ready client. env extends
// the subprocess environment; pass nil to inherit the current process env.
func NewMCPClient(spec MCPServerSpec, env []string) (*MCPClient, error) {
	args := spec.Args
	cmd := exec.Command(spec.Command, args...)
	if env != nil {
		cmd.Env = env
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("mcpclient: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("mcpclient: stdout pipe: %w", err)
	}
	// Route stderr to the parent process so MCP server errors appear in
	// brain night output instead of being silently swallowed.
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("mcpclient: start %s: %w", spec.Command, err)
	}

	c := &MCPClient{
		cmd:    cmd,
		enc:    json.NewEncoder(stdin),
		dec:    json.NewDecoder(bufio.NewReader(stdout)),
		nextID: 2, // 1 is used for initialize
	}

	// Send initialize.
	init := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]interface{}{},
			"clientInfo": map[string]interface{}{
				"name":    "sloptools",
				"version": "0.1",
			},
		},
	}
	if err := c.enc.Encode(init); err != nil {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("mcpclient: write initialize: %w", err)
	}

	// Read responses until we see id=1.
	if err := c.waitForID(1); err != nil {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("mcpclient: initialize response: %w", err)
	}

	// Send initialized notification (no id).
	notif := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
		"params":  map[string]interface{}{},
	}
	if err := c.enc.Encode(notif); err != nil {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("mcpclient: write initialized notification: %w", err)
	}

	return c, nil
}

// waitForID reads messages from the decoder until it receives one with
// the given numeric id. Objects without an id (notifications) are discarded.
func (c *MCPClient) waitForID(id int) error {
	for {
		var msg map[string]interface{}
		if err := c.dec.Decode(&msg); err != nil {
			return fmt.Errorf("decode: %w", err)
		}
		rawID, hasID := msg["id"]
		if !hasID {
			continue // notification
		}
		// JSON numbers decode as float64.
		switch v := rawID.(type) {
		case float64:
			if int(v) == id {
				if errObj, ok := msg["error"].(map[string]interface{}); ok {
					if m, ok := errObj["message"].(string); ok {
						return fmt.Errorf("server error: %s", m)
					}
					return fmt.Errorf("server error: %v", errObj)
				}
				return nil
			}
		}
	}
}

// ListTools sends tools/list and returns the server's tool definitions.
func (c *MCPClient) ListTools() ([]MCPToolDef, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	id := c.nextID
	c.nextID++

	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "tools/list",
		"params":  map[string]interface{}{},
	}
	if err := c.enc.Encode(req); err != nil {
		return nil, fmt.Errorf("mcpclient ListTools: encode: %w", err)
	}

	resp, err := c.readResponse(id)
	if err != nil {
		return nil, fmt.Errorf("mcpclient ListTools: %w", err)
	}

	result, ok := resp["result"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("mcpclient ListTools: unexpected result shape")
	}
	rawTools, _ := result["tools"].([]interface{})
	out := make([]MCPToolDef, 0, len(rawTools))
	for _, rt := range rawTools {
		t, ok := rt.(map[string]interface{})
		if !ok {
			continue
		}
		def := MCPToolDef{
			Name:        stringField(t, "name"),
			Description: stringField(t, "description"),
		}
		if s, ok := t["inputSchema"].(map[string]interface{}); ok {
			def.InputSchema = s
		}
		out = append(out, def)
	}
	return out, nil
}

// Call sends tools/call for the named tool and returns the text content.
// Tool-level isError results are returned as errors so the agent loop's
// breaker can stop repeated failing calls.
func (c *MCPClient) Call(name string, args map[string]interface{}) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	id := c.nextID
	c.nextID++

	if args == nil {
		args = map[string]interface{}{}
	}
	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      name,
			"arguments": args,
		},
	}
	if err := c.enc.Encode(req); err != nil {
		return "", fmt.Errorf("mcpclient Call: encode: %w", err)
	}

	resp, err := c.readResponse(id)
	if err != nil {
		return "", fmt.Errorf("mcpclient Call: %w", err)
	}

	// MCP error object.
	if errObj, ok := resp["error"].(map[string]interface{}); ok {
		if m, ok := errObj["message"].(string); ok {
			return "", fmt.Errorf("%s", m)
		}
		return "", fmt.Errorf("%v", errObj)
	}

	result, ok := resp["result"].(map[string]interface{})
	if !ok {
		return fmt.Sprintf("%v", resp["result"]), nil
	}
	text := toolResultText(result)
	if isErr, _ := result["isError"].(bool); isErr {
		if text == "" {
			text = "tool returned isError=true"
		}
		return "", fmt.Errorf("%s", text)
	}
	return text, nil
}

func toolResultText(result map[string]interface{}) string {
	content, _ := result["content"].([]interface{})
	if len(content) == 0 {
		return ""
	}
	first, _ := content[0].(map[string]interface{})
	text, _ := first["text"].(string)
	return text
}

// Close kills the subprocess. Errors are ignored.
func (c *MCPClient) Close() {
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
		_ = c.cmd.Wait()
	}
}

// readResponse reads messages until it sees one with the given id.
// Notifications (no id) are discarded.
func (c *MCPClient) readResponse(id int) (map[string]interface{}, error) {
	for {
		var msg map[string]interface{}
		if err := c.dec.Decode(&msg); err != nil {
			return nil, fmt.Errorf("decode: %w", err)
		}
		rawID, hasID := msg["id"]
		if !hasID {
			continue
		}
		if v, ok := rawID.(float64); ok && int(v) == id {
			return msg, nil
		}
	}
}

func stringField(m map[string]interface{}, key string) string {
	v, _ := m[key].(string)
	return v
}
