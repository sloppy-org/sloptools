package activity

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func callSloppy(tool string, args map[string]interface{}) (string, error) {
	realHome, _ := os.UserHomeDir()
	dataDir := filepath.Join(realHome, ".local", "share", "sloppy")

	cmd := exec.Command("sloptools", "mcp-server",
		"--project-dir", realHome,
		"--data-dir", dataDir)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return "", fmt.Errorf("stdin pipe: %w", err)
	}
	var stdout strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start sloptools: %w", err)
	}

	enc := json.NewEncoder(stdin)
	_ = enc.Encode(map[string]interface{}{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]interface{}{},
			"clientInfo":      map[string]interface{}{"name": "sloptools-activity", "version": "0.1"},
		},
	})
	_ = enc.Encode(map[string]interface{}{
		"jsonrpc": "2.0", "id": 2, "method": "tools/call",
		"params": map[string]interface{}{"name": tool, "arguments": args},
	})
	stdin.Close()

	_ = cmd.Wait()

	for _, line := range strings.Split(stdout.String(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var msg map[string]interface{}
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}
		id, _ := msg["id"].(float64)
		if int(id) != 2 {
			continue
		}
		result, _ := msg["result"].(map[string]interface{})
		if result == nil {
			break
		}
		content, _ := result["content"].([]interface{})
		if len(content) == 0 {
			break
		}
		first, _ := content[0].(map[string]interface{})
		text, _ := first["text"].(string)
		return text, nil
	}
	return "", fmt.Errorf("no response for tool %q", tool)
}
