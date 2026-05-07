package backend

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSandboxCopiesStagePromptAndIsolatesGlobalCLAUDE(t *testing.T) {
	tmpPrompt := filepath.Join(t.TempDir(), "stage.md")
	stagePromptBody := "You are a librarian. Output strict folder notes only.\n"
	if err := os.WriteFile(tmpPrompt, []byte(stagePromptBody), 0o600); err != nil {
		t.Fatalf("write stage prompt: %v", err)
	}
	sb, err := NewSandbox("test-run", "stage", tmpPrompt, DefaultMCPConfig())
	if err != nil {
		t.Fatalf("NewSandbox: %v", err)
	}
	t.Cleanup(func() { _ = sb.Cleanup() })

	systemBody, err := os.ReadFile(sb.SystemPromptIn)
	if err != nil {
		t.Fatalf("read system.md: %v", err)
	}
	if string(systemBody) != stagePromptBody {
		t.Fatalf("system.md mismatch: %q", string(systemBody))
	}
	for _, expected := range []string{
		filepath.Join(sb.HomeDir, ".claude", "CLAUDE.md"),
		filepath.Join(sb.WorkDir, "AGENTS.md"),
		filepath.Join(sb.WorkDir, "CLAUDE.md"),
	} {
		body, err := os.ReadFile(expected)
		if err != nil {
			t.Fatalf("expected %s populated: %v", expected, err)
		}
		if string(body) != stagePromptBody {
			t.Fatalf("%s did not match stage prompt", expected)
		}
	}
}

func TestSandboxMCPConfigContainsSloppyAndHelpyButNotSlopshell(t *testing.T) {
	sb, err := NewSandbox("test-mcp", "stage", "", DefaultMCPConfig())
	if err != nil {
		t.Fatalf("NewSandbox: %v", err)
	}
	t.Cleanup(func() { _ = sb.Cleanup() })
	body, err := os.ReadFile(sb.MCPConfigPath)
	if err != nil {
		t.Fatalf("read mcp.json: %v", err)
	}
	got := string(body)
	for _, must := range []string{`"sloppy"`, `"helpy"`, `"mcp-stdio"`, `"mcp-server"`} {
		if !strings.Contains(got, must) {
			t.Fatalf("mcp.json missing %s: %s", must, got)
		}
	}
	if strings.Contains(got, "slopshell") {
		t.Fatalf("mcp.json must not register slopshell: %s", got)
	}
}

func TestSandboxEnvOverridesHOMEAndCODEXHOME(t *testing.T) {
	sb, err := NewSandbox("test-env", "stage", "", DefaultMCPConfig())
	if err != nil {
		t.Fatalf("NewSandbox: %v", err)
	}
	t.Cleanup(func() { _ = sb.Cleanup() })
	env := sb.Env()
	have := map[string]string{}
	for _, kv := range env {
		i := strings.Index(kv, "=")
		if i < 0 {
			continue
		}
		have[kv[:i]] = kv[i+1:]
	}
	if have["HOME"] != sb.HomeDir {
		t.Fatalf("HOME not overridden, got %q want %q", have["HOME"], sb.HomeDir)
	}
	if have["CODEX_HOME"] != sb.CodexHome {
		t.Fatalf("CODEX_HOME not overridden, got %q want %q", have["CODEX_HOME"], sb.CodexHome)
	}
	if have["XDG_CONFIG_HOME"] != sb.XDGConfigHome {
		t.Fatalf("XDG_CONFIG_HOME not overridden, got %q want %q", have["XDG_CONFIG_HOME"], sb.XDGConfigHome)
	}
}
