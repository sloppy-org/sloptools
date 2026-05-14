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
	setReapOnParentDeath(cmd)

	var captured bytes.Buffer
	cmd.Stdout = io.MultiWriter(os.Stderr, &captured)
	cmd.Stderr = io.MultiWriter(os.Stderr, &captured)

	start := time.Now()
	fmt.Fprintf(os.Stderr, "brain night: codex start stage=%s model=%s reasoning=%s cwd=%s allow_edits=%t\n",
		req.Stage, req.Model, req.Reasoning, cwd, req.AllowEdits)
	stopHeartbeat := make(chan struct{})
	go codexHeartbeat(stopHeartbeat, req.Stage, start)
	err = cmd.Run()
	close(stopHeartbeat)
	if err != nil {
		fmt.Fprintf(os.Stderr, "brain night: codex error stage=%s elapsed=%s error=%s\n",
			req.Stage, time.Since(start).Round(time.Second), err)
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
	if in == 0 && out > 0 {
		// Total-only form: codex 0.128 prints `tokens used\n<TOTAL>`
		// with no prompt/completion split. Split by byte ratio so the
		// ledger reflects approximate input vs output instead of
		// suggesting 0 input tokens. Token/byte ratio is ~0.25 for
		// both English and code, so the byte ratio is a reasonable
		// proxy for the token ratio.
		total := out
		inBytes := int64(len(req.Packet))
		outBytes := int64(len(body))
		if denom := inBytes + outBytes; denom > 0 {
			in = total * inBytes / denom
			out = total - in
		}
	}
	fmt.Fprintf(os.Stderr, "brain night: codex done stage=%s wall_ms=%d tokens_in=%d tokens_out=%d output_bytes=%d preview=%s\n",
		req.Stage, wall.Milliseconds(), in, out, len(body), traceText(string(body), 700))
	return Response{
		Output:    strings.TrimRight(string(body), "\n") + "\n",
		WallMS:    wall.Milliseconds(),
		TokensIn:  in,
		TokensOut: out,
	}, nil
}

func codexHeartbeat(stop <-chan struct{}, stage string, start time.Time) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			fmt.Fprintf(os.Stderr, "brain night: codex running stage=%s elapsed=%s\n",
				stage, time.Since(start).Round(time.Second))
		}
	}
}

// codex 0.128 prints final usage near the tail of stderr in formats like:
//
//	tokens used
//	78,965
//	tokens used: 12345
//	[2026-01-15T12:34:56] tokens used: 12345
//	prompt_tokens=1234 completion_tokens=567 total_tokens=1801
//	"input_tokens": 1234, "output_tokens": 567
//
// We try several patterns; the last hit wins (final usage line). The
// "tokens used" / "Total tokens" forms accept either an inline ": N"
// separator or a number on the next line, with optional thousands
// commas (codex 0.128 prints "78,965").
var (
	reCodexInOut       = regexp.MustCompile(`(?i)(?:prompt|input)[_ -]?tokens?["']?\s*[:=]\s*(\d+).{0,80}?(?:completion|output)[_ -]?tokens?["']?\s*[:=]\s*(\d+)`)
	reCodexTotalInline = regexp.MustCompile(`(?i)(?:total[_ -]?tokens?|tokens used)\s*[:=]\s*([\d,]+)`)
	reCodexTotalNL     = regexp.MustCompile(`(?im)^\s*(?:total[_ -]?tokens?|tokens used)\s*\n\s*([\d,]+)`)
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
	// Total-only forms: ledger sums tokens_in + tokens_out, so record
	// the total as `out` to avoid double-counting. Try newline form
	// first (codex 0.128 default), then inline.
	if matches := reCodexTotalNL.FindAllStringSubmatch(s, -1); len(matches) > 0 {
		out = parseIntOr(matches[len(matches)-1][1])
		return in, out
	}
	if matches := reCodexTotalInline.FindAllStringSubmatch(s, -1); len(matches) > 0 {
		out = parseIntOr(matches[len(matches)-1][1])
	}
	return in, out
}

// parseIntOr accepts comma-separated thousands ("78,965") and trims
// whitespace; returns zero on any parse failure.
func parseIntOr(s string) int64 {
	clean := strings.ReplaceAll(strings.TrimSpace(s), ",", "")
	v, err := strconv.ParseInt(clean, 10, 64)
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
