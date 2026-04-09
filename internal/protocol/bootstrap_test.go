package protocol

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBootstrapProjectCreatesExpectedFilesWithoutAgentsMutation(t *testing.T) {
	projectDir := t.TempDir()

	result, err := BootstrapProject(projectDir)
	if err != nil {
		t.Fatalf("BootstrapProject() error = %v", err)
	}
	if result.GitInitialized {
		t.Fatalf("GitInitialized = true, want false")
	}
	if result.Paths.ProjectDir == "" {
		t.Fatalf("ProjectDir should not be empty")
	}
	if _, err := os.Stat(filepath.Join(projectDir, "AGENTS.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("AGENTS.md should not be created, stat err = %v", err)
	}

	mcpBody, err := os.ReadFile(result.Paths.MCPConfigPath)
	if err != nil {
		t.Fatalf("read mcp config: %v", err)
	}
	if !strings.Contains(string(mcpBody), "mcp-server") {
		t.Fatalf("mcp config missing mcp-server invocation")
	}
	if !strings.Contains(string(mcpBody), "[mcp_servers.sloptools]") {
		t.Fatalf("mcp config missing sloptools server key")
	}

	gitignoreBody, err := os.ReadFile(filepath.Join(projectDir, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	if !strings.Contains(string(gitignoreBody), ".sloptools/artifacts/") {
		t.Fatalf(".gitignore missing .sloptools/artifacts/ entry")
	}
}

func TestBootstrapProjectPreservesExistingAgentsAndDetectsGit(t *testing.T) {
	projectDir := t.TempDir()
	agentsPath := filepath.Join(projectDir, "AGENTS.md")
	initial := "custom agents content\n"
	if err := os.WriteFile(agentsPath, []byte(initial), 0o644); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}
	if err := os.Mkdir(filepath.Join(projectDir, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}

	result, err := BootstrapProject(projectDir)
	if err != nil {
		t.Fatalf("BootstrapProject() error = %v", err)
	}
	if !result.GitInitialized {
		t.Fatalf("GitInitialized = false, want true")
	}

	agentsBody, err := os.ReadFile(agentsPath)
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}
	if string(agentsBody) != initial {
		t.Fatalf("AGENTS.md was unexpectedly modified")
	}
}

func TestEnsureGitignoreAppendsEntryOnlyOnce(t *testing.T) {
	projectDir := t.TempDir()
	gitignorePath := filepath.Join(projectDir, ".gitignore")
	if err := os.WriteFile(gitignorePath, []byte("node_modules/\n"), 0o644); err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}

	if err := ensureGitignore(projectDir); err != nil {
		t.Fatalf("ensureGitignore() first call: %v", err)
	}
	if err := ensureGitignore(projectDir); err != nil {
		t.Fatalf("ensureGitignore() second call: %v", err)
	}

	body, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	content := string(body)
	if strings.Count(content, ".sloptools/artifacts/") != 1 {
		t.Fatalf("expected .sloptools/artifacts/ exactly once, got content:\n%s", content)
	}
}
