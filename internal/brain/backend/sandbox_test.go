package backend

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSandboxCopiesStagePromptAndIsolatesGlobalInstructions(t *testing.T) {
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
		filepath.Join(sb.WorkDir, "AGENTS.md"),
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

func TestSandboxEnvPointsHelpyZoteroAtRealHome(t *testing.T) {
	realHome := t.TempDir()
	t.Setenv("HOME", realHome)
	zoteroDir := filepath.Join(realHome, "Zotero")
	if err := os.MkdirAll(filepath.Join(zoteroDir, "storage"), 0o700); err != nil {
		t.Fatalf("mkdir Zotero storage: %v", err)
	}
	dbPath := filepath.Join(zoteroDir, "zotero.sqlite")
	if err := os.WriteFile(dbPath, []byte("fixture"), 0o600); err != nil {
		t.Fatalf("write zotero db: %v", err)
	}

	sb, err := NewSandbox("test-zotero", "stage", "", DefaultMCPConfig())
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
	if have["HELPY_ZOTERO_DB"] != dbPath {
		t.Fatalf("HELPY_ZOTERO_DB = %q, want %q", have["HELPY_ZOTERO_DB"], dbPath)
	}
	if have["HELPY_ZOTERO_STORAGE"] != filepath.Join(zoteroDir, "storage") {
		t.Fatalf("HELPY_ZOTERO_STORAGE = %q", have["HELPY_ZOTERO_STORAGE"])
	}
}

func TestSandboxEnvFindsLinuxZoteroProfile(t *testing.T) {
	realHome := t.TempDir()
	t.Setenv("HOME", realHome)
	profileDir := filepath.Join(realHome, ".zotero", "zotero", "abc.default")
	if err := os.MkdirAll(filepath.Join(profileDir, "storage"), 0o700); err != nil {
		t.Fatalf("mkdir Zotero profile storage: %v", err)
	}
	dbPath := filepath.Join(profileDir, "zotero.sqlite")
	if err := os.WriteFile(dbPath, []byte("fixture"), 0o600); err != nil {
		t.Fatalf("write zotero db: %v", err)
	}

	sb, err := NewSandbox("test-zotero-linux", "stage", "", DefaultMCPConfig())
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
	if have["HELPY_ZOTERO_DB"] != dbPath {
		t.Fatalf("HELPY_ZOTERO_DB = %q, want %q", have["HELPY_ZOTERO_DB"], dbPath)
	}
	if have["HELPY_ZOTERO_STORAGE"] != filepath.Join(profileDir, "storage") {
		t.Fatalf("HELPY_ZOTERO_STORAGE = %q", have["HELPY_ZOTERO_STORAGE"])
	}
}

func TestSandboxRootIsUniqueForSameRunAndStage(t *testing.T) {
	first, err := NewSandbox("same-run", "sleep/judge", "", DefaultMCPConfig())
	if err != nil {
		t.Fatalf("first NewSandbox: %v", err)
	}
	t.Cleanup(func() { _ = first.Cleanup() })

	second, err := NewSandbox("same-run", "sleep/judge", "", DefaultMCPConfig())
	if err != nil {
		t.Fatalf("second NewSandbox: %v", err)
	}
	t.Cleanup(func() { _ = second.Cleanup() })

	if first.Root == second.Root {
		t.Fatalf("sandbox roots collided: %s", first.Root)
	}
	parent := filepath.Join(os.TempDir(), "sloptools-brain-same-run")
	for _, root := range []string{first.Root, second.Root} {
		if filepath.Dir(root) != parent {
			t.Fatalf("root %q not under %q", root, parent)
		}
		if strings.Contains(filepath.Base(root), "/") {
			t.Fatalf("root basename contains path separator: %q", root)
		}
	}
}
