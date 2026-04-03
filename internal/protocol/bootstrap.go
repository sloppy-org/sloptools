package protocol

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Paths struct {
	ProjectDir    string
	MCPConfigPath string
}

type Result struct {
	Paths          Paths
	GitInitialized bool
}

func BootstrapProject(projectDir string) (Result, error) {
	abs, err := filepath.Abs(projectDir)
	if err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return Result{}, err
	}
	sloppyDir := filepath.Join(abs, ".sloppy")
	if err := os.MkdirAll(sloppyDir, 0o755); err != nil {
		return Result{}, err
	}
	paths := Paths{
		ProjectDir:    abs,
		MCPConfigPath: filepath.Join(sloppyDir, "codex-mcp.toml"),
	}
	_ = os.WriteFile(paths.MCPConfigPath, []byte(fmt.Sprintf("[mcp_servers.sloppy]\ncommand = \"sloppy\"\nargs = [\"mcp-server\", \"--project-dir\", \"%s\"]\n", strings.ReplaceAll(abs, "\\", "\\\\"))), 0o644)
	_ = ensureGitignore(abs)
	gitInit := false
	if _, err := os.Stat(filepath.Join(abs, ".git")); err == nil {
		gitInit = true
	}
	return Result{Paths: paths, GitInitialized: gitInit}, nil
}

func ensureGitignore(projectDir string) error {
	gitignore := filepath.Join(projectDir, ".gitignore")
	data := ""
	if b, err := os.ReadFile(gitignore); err == nil {
		data = string(b)
	}
	want := ".sloppy/artifacts/\n"
	if strings.Contains(data, ".sloppy/artifacts/") {
		return nil
	}
	if data != "" && !strings.HasSuffix(data, "\n") {
		data += "\n"
	}
	data += want
	return os.WriteFile(gitignore, []byte(data), 0o644)
}
