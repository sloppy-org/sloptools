package backend

import (
	"encoding/json"
	"os"
)

// MCPServersFromFile returns the canonical MCP server map written by
// NewSandbox. Backends use this when translating into per-CLI formats.
func (sb *Sandbox) MCPServersFromFile() MCPConfig {
	if sb == nil || sb.MCPConfigPath == "" {
		return DefaultMCPConfig()
	}
	body, err := os.ReadFile(sb.MCPConfigPath)
	if err != nil {
		return DefaultMCPConfig()
	}
	var f mcpConfigFile
	if err := json.Unmarshal(body, &f); err != nil {
		return DefaultMCPConfig()
	}
	if f.MCPServers == nil {
		return DefaultMCPConfig()
	}
	return f.MCPServers
}
