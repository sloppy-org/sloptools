package backend

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Sandbox is a per-call scratch tree that isolates the child CLI from
// the user's home configuration. Every backend exports HOME, CODEX_HOME,
// and XDG_CONFIG_HOME at this tree, places the role-specific system
// prompt under system.md, and registers the canonical sloppy + helpy MCP
// servers in mcp.json (and CODEX_HOME/config.toml).
//
// The user's auth credentials (claude OAuth, codex auth, opencode
// session) are preserved by symlinking the relevant credential files
// from the original $HOME into the scratch HOME. The role-specific
// CLAUDE.md / AGENTS.md / opencode agent markdown overrides any global
// instruction file the CLI would otherwise auto-discover.
type Sandbox struct {
	Root           string
	HomeDir        string
	CodexHome      string
	XDGConfigHome  string
	WorkDir        string
	SystemPromptIn string // copied per-call from Request.SystemPromptPath
	MCPConfigPath  string
}

// NewSandbox builds the scratch tree under /tmp/sloptools-brain-<runID>/<stage>/.
// It does not invoke any CLI; backends own the exec step.
//
// stagePromptPath is copied into <root>/system.md so the call has a stable,
// short, role-only instruction file. mcpServers is the canonical sloppy +
// helpy registration; backends translate it into the format their CLI wants.
func NewSandbox(runID, stage, stagePromptPath string, mcpServers MCPConfig) (*Sandbox, error) {
	if runID == "" {
		return nil, fmt.Errorf("sandbox: runID required")
	}
	if stage == "" {
		return nil, fmt.Errorf("sandbox: stage required")
	}
	root := filepath.Join(os.TempDir(), fmt.Sprintf("sloptools-brain-%s", runID), stage)
	homeDir := filepath.Join(root, "HOME")
	codexHome := filepath.Join(root, "CODEX_HOME")
	xdgConfig := filepath.Join(homeDir, ".config")
	workDir := filepath.Join(root, "workdir")
	for _, d := range []string{
		root,
		homeDir,
		filepath.Join(homeDir, ".claude"),
		filepath.Join(xdgConfig, "opencode", "agent"),
		codexHome,
		workDir,
	} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return nil, fmt.Errorf("sandbox mkdir %s: %w", d, err)
		}
	}
	sb := &Sandbox{
		Root:          root,
		HomeDir:       homeDir,
		CodexHome:     codexHome,
		XDGConfigHome: xdgConfig,
		WorkDir:       workDir,
	}
	if stagePromptPath != "" {
		dst := filepath.Join(root, "system.md")
		body, err := os.ReadFile(stagePromptPath)
		if err != nil {
			return nil, fmt.Errorf("sandbox: read stage prompt: %w", err)
		}
		if err := os.WriteFile(dst, body, 0o600); err != nil {
			return nil, fmt.Errorf("sandbox: write stage prompt: %w", err)
		}
		sb.SystemPromptIn = dst
		// Stage prompt also serves as the auto-discovered CLAUDE.md /
		// AGENTS.md so the home-dir version cannot leak through.
		for _, p := range []string{
			filepath.Join(homeDir, ".claude", "CLAUDE.md"),
			filepath.Join(workDir, "AGENTS.md"),
			filepath.Join(workDir, "CLAUDE.md"),
		} {
			if err := os.WriteFile(p, body, 0o600); err != nil {
				return nil, fmt.Errorf("sandbox: stage prompt copy %s: %w", p, err)
			}
		}
	}
	if err := sb.preserveCredentials(); err != nil {
		return nil, err
	}
	if err := sb.writeMCPConfig(mcpServers); err != nil {
		return nil, err
	}
	return sb, nil
}

// Cleanup removes the scratch tree.
func (sb *Sandbox) Cleanup() error {
	if sb == nil || sb.Root == "" {
		return nil
	}
	return os.RemoveAll(sb.Root)
}

// Env returns the environment overrides every backend sets when exec'ing
// its CLI. The slice extends os.Environ().
func (sb *Sandbox) Env() []string {
	overrides := map[string]string{
		"HOME":            sb.HomeDir,
		"CODEX_HOME":      sb.CodexHome,
		"XDG_CONFIG_HOME": sb.XDGConfigHome,
	}
	keep := os.Environ()
	out := make([]string, 0, len(keep)+len(overrides))
	for _, kv := range keep {
		drop := false
		for k := range overrides {
			if len(kv) > len(k) && kv[:len(k)+1] == k+"=" {
				drop = true
				break
			}
		}
		if !drop {
			out = append(out, kv)
		}
	}
	for k, v := range overrides {
		out = append(out, k+"="+v)
	}
	return out
}

// preserveCredentials symlinks the user's auth files into the scratch
// HOME so OAuth and CLI sessions still work. Symlinks (not copies) keep
// secrets outside the scratch tree.
func (sb *Sandbox) preserveCredentials() error {
	realHome, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("sandbox: real home: %w", err)
	}
	pairs := []struct {
		src, dst string
	}{
		{filepath.Join(realHome, ".claude", ".credentials.json"), filepath.Join(sb.HomeDir, ".claude", ".credentials.json")},
		{filepath.Join(realHome, ".codex"), filepath.Join(sb.HomeDir, ".codex-real")},
		{filepath.Join(realHome, ".local", "share", "opencode"), filepath.Join(sb.HomeDir, ".local", "share", "opencode")},
		{filepath.Join(realHome, ".config", "opencode", "auth.json"), filepath.Join(sb.XDGConfigHome, "opencode", "auth.json")},
	}
	for _, p := range pairs {
		if _, err := os.Stat(p.src); err != nil {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p.dst), 0o700); err != nil {
			return fmt.Errorf("sandbox: mkdir %s: %w", filepath.Dir(p.dst), err)
		}
		_ = os.Remove(p.dst)
		if err := os.Symlink(p.src, p.dst); err != nil {
			return fmt.Errorf("sandbox: symlink %s -> %s: %w", p.src, p.dst, err)
		}
	}
	return nil
}

// MCPServerSpec describes one MCP server entry as the canonical sloppy +
// helpy registration. Backends translate it into their CLI's preferred
// format.
type MCPServerSpec struct {
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
}

// MCPConfig is the canonical map { "sloppy": ..., "helpy": ... }.
type MCPConfig map[string]MCPServerSpec

// DefaultMCPConfig returns the project-standard sloppy + helpy stdio
// registration. slopshell is intentionally absent.
func DefaultMCPConfig() MCPConfig {
	realHome, _ := os.UserHomeDir()
	dataDir := filepath.Join(realHome, ".local", "share", "sloppy")
	return MCPConfig{
		"sloppy": {
			Command: "sloptools",
			Args:    []string{"mcp-server", "--project-dir", realHome, "--data-dir", dataDir},
		},
		"helpy": {
			Command: "helpy",
			Args:    []string{"mcp-stdio"},
		},
	}
}

// claudeMCPConfigFile is the on-disk shape claude --mcp-config expects.
type claudeMCPConfigFile struct {
	MCPServers MCPConfig `json:"mcpServers"`
}

func (sb *Sandbox) writeMCPConfig(servers MCPConfig) error {
	if servers == nil {
		servers = DefaultMCPConfig()
	}
	path := filepath.Join(sb.Root, "mcp.json")
	body, err := json.MarshalIndent(claudeMCPConfigFile{MCPServers: servers}, "", "  ")
	if err != nil {
		return fmt.Errorf("sandbox: marshal mcp.json: %w", err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return fmt.Errorf("sandbox: write mcp.json: %w", err)
	}
	sb.MCPConfigPath = path
	return nil
}
