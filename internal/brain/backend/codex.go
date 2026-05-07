package backend

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
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
	cwd := req.Sandbox.WorkDir
	if req.WorkDir != "" {
		cwd = req.WorkDir
	}
	args := []string{
		"--ask-for-approval", "never",
		"exec",
		"--skip-git-repo-check",
		"--sandbox", sandboxMode,
		"--model", req.Model,
		"-C", cwd,
		"--output-last-message", tmpPath,
		"-c", "base_instructions=@" + req.SystemPromptPath,
		"-c", "model_reasoning_effort=\"" + string(req.Reasoning) + "\"",
		"-",
	}

	cmd := exec.CommandContext(ctx, "codex", args...)
	cmd.Env = req.Sandbox.Env()
	cmd.Dir = cwd
	cmd.Stdin = strings.NewReader(req.Packet)

	var captured bytes.Buffer
	cmd.Stdout = io.MultiWriter(os.Stderr, &captured)
	cmd.Stderr = io.MultiWriter(os.Stderr, &captured)

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
	in, out := scrapeCodexTokens(captured.String())
	return Response{
		Output:    strings.TrimRight(string(body), "\n") + "\n",
		WallMS:    wall.Milliseconds(),
		TokensIn:  in,
		TokensOut: out,
	}, nil
}

// codex 0.128 prints final usage near the tail of stderr in formats like:
//
//	tokens used: 12345
//	[2026-01-15T12:34:56] tokens used: 12345
//	prompt_tokens=1234 completion_tokens=567 total_tokens=1801
//	"input_tokens": 1234, "output_tokens": 567
//
// We try several patterns; the last hit wins (final usage line).
var (
	reCodexInOut = regexp.MustCompile(`(?i)(?:prompt|input)[_ -]?tokens?["']?\s*[:=]\s*(\d+).{0,80}?(?:completion|output)[_ -]?tokens?["']?\s*[:=]\s*(\d+)`)
	reCodexTotal = regexp.MustCompile(`(?i)(?:total[_ -]?tokens?|tokens used)\s*[:=]\s*(\d+)`)
)

// scrapeCodexTokens parses input/output token totals from codex CLI
// output. Returns zeros when no recognised line is present; callers
// treat zero as "unknown" rather than "free".
func scrapeCodexTokens(s string) (int64, int64) {
	var in, out int64
	if matches := reCodexInOut.FindAllStringSubmatch(s, -1); len(matches) > 0 {
		last := matches[len(matches)-1]
		in = parseIntOr(last[1])
		out = parseIntOr(last[2])
		return in, out
	}
	if matches := reCodexTotal.FindAllStringSubmatch(s, -1); len(matches) > 0 {
		last := matches[len(matches)-1]
		// Ledger sums tokens_in + tokens_out; record the total as out so
		// nothing is double-counted.
		out = parseIntOr(last[1])
	}
	return in, out
}

func parseIntOr(s string) int64 {
	v, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0
	}
	return v
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
