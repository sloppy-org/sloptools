package backend

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// CodexBackend invokes `codex exec` in one-shot mode under a scratch
// sandbox. Per-stage system prompts are injected via -c
// base_instructions=@<file> until upstream issue openai/codex#11588
// ships explicit --system-prompt-file flags.
type CodexBackend struct{}

// Provider returns OpenAI.
func (CodexBackend) Provider() Provider { return ProviderOpenAI }

// Name returns the backend identifier used in the ledger.
func (CodexBackend) Name() string { return "codex" }

// Run shells out to codex exec.
func (CodexBackend) Run(ctx context.Context, req Request) (Response, error) {
	if err := req.validate(); err != nil {
		return Response{}, err
	}
	if err := writeCodexConfig(req); err != nil {
		return Response{}, err
	}
	tmp, err := os.CreateTemp(req.Sandbox.Root, "codex-output-*.md")
	if err != nil {
		return Response{}, fmt.Errorf("codex: temp file: %w", err)
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	defer os.Remove(tmpPath)

	sandboxMode := "read-only"
	if req.AllowEdits {
		sandboxMode = "workspace-write"
	}
	args := []string{
		"--ask-for-approval", "never",
		"exec",
		"--skip-git-repo-check",
		"--sandbox", sandboxMode,
		"--model", req.Model,
		"-C", req.Sandbox.WorkDir,
		"--output-last-message", tmpPath,
		"-c", "base_instructions=@" + req.SystemPromptPath,
		"-c", "model_reasoning_effort=\"" + string(req.Reasoning) + "\"",
		"-",
	}

	cmd := exec.CommandContext(ctx, "codex", args...)
	cmd.Env = req.Sandbox.Env()
	cmd.Dir = req.Sandbox.WorkDir
	cmd.Stdin = strings.NewReader(req.Packet)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr

	start := time.Now()
	if err := cmd.Run(); err != nil {
		return Response{}, fmt.Errorf("codex exec run: %w", err)
	}
	wall := time.Since(start)

	body, err := os.ReadFile(tmpPath)
	if err != nil {
		return Response{}, fmt.Errorf("codex output read: %w", err)
	}
	if strings.TrimSpace(string(body)) == "" {
		return Response{}, ErrEmptyOutput
	}
	return Response{
		Output: strings.TrimRight(string(body), "\n") + "\n",
		WallMS: wall.Milliseconds(),
	}, nil
}

// writeCodexConfig drops a minimal config.toml into CODEX_HOME so the
// sandbox call honors the canonical sloppy + helpy MCP entries plus the
// stage profile. We cannot rely on the user's home ~/.codex/config.toml
// because that may carry repo-specific AGENTS.md or model defaults we
// want to override.
func writeCodexConfig(req Request) error {
	target := filepath.Join(req.Sandbox.CodexHome, "config.toml")
	servers := req.Sandbox.MCPServersFromFile()
	var b strings.Builder
	b.WriteString("# generated per-stage codex config\n")
	b.WriteString("[history]\n")
	b.WriteString("persistence = \"none\"\n\n")
	for name, spec := range servers {
		b.WriteString(fmt.Sprintf("[mcp_servers.%s]\n", name))
		b.WriteString(fmt.Sprintf("command = %q\n", spec.Command))
		if len(spec.Args) > 0 {
			b.WriteString("args = [")
			for i, a := range spec.Args {
				if i > 0 {
					b.WriteString(", ")
				}
				b.WriteString(fmt.Sprintf("%q", a))
			}
			b.WriteString("]\n")
		}
		b.WriteString("\n")
	}
	// Symlink auth from the user's real ~/.codex/auth.json if present so
	// codex's plan-tier auth still works inside the sandbox.
	realCodex := filepath.Join(req.Sandbox.HomeDir, ".codex-real")
	if info, err := os.Lstat(realCodex); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			if linked, lerr := os.Readlink(realCodex); lerr == nil {
				for _, name := range []string{"auth.json", "session.json"} {
					src := filepath.Join(linked, name)
					if _, err := os.Stat(src); err == nil {
						_ = os.Symlink(src, filepath.Join(req.Sandbox.CodexHome, name))
					}
				}
			}
		}
	}
	return os.WriteFile(target, []byte(b.String()), 0o600)
}
