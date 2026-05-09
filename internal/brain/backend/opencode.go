package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// OpencodeLockPath is the host-level single-slot lock for opencode
// invocations. The user's machine is shared and only one opencode call
// should run at a time.
const OpencodeLockPath = "/tmp/sloptools-opencode-lock"

// OpencodeAgentName is the per-stage opencode agent name we register in
// the scratch XDG_CONFIG_HOME/opencode/agent/<name>.md.
const OpencodeAgentName = "brain-stage"

// OpencodeBackend invokes `opencode run` in one-shot mode under a
// scratch sandbox. The agent system prompt is the role-specific stage
// prompt copied into the scratch agent dir.
type OpencodeBackend struct{}

// Provider returns Local.
func (OpencodeBackend) Provider() Provider { return ProviderLocal }

// Name returns the backend identifier used in the ledger.
func (OpencodeBackend) Name() string { return "opencode" }

// processLock is per-process; the host-level flock further serializes
// across separate sloptools processes.
var processLock sync.Mutex

// Run shells out to opencode run --pure --agent <agent> --model <model>.
func (OpencodeBackend) Run(ctx context.Context, req Request) (Response, error) {
	if err := req.validate(); err != nil {
		return Response{}, err
	}
	if err := writeOpencodeAgent(req); err != nil {
		return Response{}, err
	}
	if err := writeOpencodeConfig(req); err != nil {
		return Response{}, err
	}
	processLock.Lock()
	defer processLock.Unlock()

	cwd := req.Sandbox.WorkDir
	if req.WorkDir != "" {
		cwd = req.WorkDir
	}
	args := opencodeArgs(OpencodeAgentName, req.Model, string(req.Reasoning), cwd)

	full := append([]string{"flock", OpencodeLockPath, "opencode"}, args...)
	cmd := exec.CommandContext(ctx, full[0], full[1:]...)
	cmd.Env = req.Sandbox.Env()
	cmd.Dir = cwd
	// #128: pass the packet on stdin instead of as the trailing argv
	// element. A 167 KB sleep packet baked into argv crashed fork/exec
	// with "argument list too long" because the kernel ARG_MAX (~128 KB
	// on Linux) caps argv + envp combined. opencode reads its message
	// from stdin when no positional arg is given.
	cmd.Stdin = strings.NewReader(req.Packet)
	setReapOnParentDeath(cmd)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderr)

	start := time.Now()
	err := cmd.Run()
	wall := time.Since(start)
	if err != nil {
		return Response{}, fmt.Errorf("opencode exec: %w; stderr=%s", err, stderr.String())
	}
	out := stdout.Bytes()
	// Surface every failed MCP tool call to stderr as a structured
	// line. Issue #130: previously these failures appeared only as
	// opencode's own "(failed)" log with no diagnostic, so we could
	// not tell why a long-running scout saw clusters of failures.
	for _, f := range extractToolFailures(out) {
		fmt.Fprintln(os.Stderr, formatToolFailureLogLine(req.Stage, f))
	}
	body, tin, tout := parseOpencodeJSON(out)
	if strings.TrimSpace(body) == "" {
		body = strings.TrimSpace(string(out))
	}
	if strings.TrimSpace(body) == "" {
		return Response{}, ErrEmptyOutput
	}
	return Response{
		Output:    body,
		TokensIn:  tin,
		TokensOut: tout,
		WallMS:    wall.Milliseconds(),
	}, nil
}

// opencodeArgs builds the argv for `opencode run`, omitting the prompt.
// The prompt MUST travel on stdin (#128): baking it into argv risks a
// fork/exec "argument list too long" failure because the kernel
// ARG_MAX caps argv + envp. Caller wraps this in `flock <lock> opencode
// ...` and pipes the packet via cmd.Stdin.
func opencodeArgs(agent, model, variant, cwd string) []string {
	return []string{
		"run",
		"--pure",
		"--print-logs",
		"--log-level", "WARN",
		"--agent", agent,
		"--model", model,
		"--variant", variant,
		"--format", "json",
		"--dir", cwd,
		"--dangerously-skip-permissions",
	}
}

// opencodeAgentFrontmatter returns the YAML frontmatter prepended to
// every brain-stage agent file. opencode requires mode: primary so the
// agent is a top-level conversation owner, and an explicit permission
// allow so MCP tool calls are not auto-denied — without that, the
// global permission setting does not propagate to a custom agent and
// MCP calls silently drop, leaving the model to confabulate sources.
//
// We deny `edit` (covers write/edit/apply_patch in opencode) so the
// model cannot route its deliverable through the file-write side
// channel; the rewrite must arrive on the streaming text or the `write`
// tool call we explicitly parse in parseOpencodeJSON.
//
// `bash` uses opencode's last-match-wins glob allowlist (object-form
// permission, supported since 2025; see https://opencode.ai/docs/permissions/)
// so a small set of read-only commands with bounded output is allowed
// while everything else stays denied. `cat` is deliberately excluded
// because it is unbounded; `pdftotext`/`pdfinfo` are excluded because
// helpy `pdf_read` provides bounded in-process equivalents; `curl`/
// `wget` are excluded because helpy `web_fetch` is the canonical path.
// `grep` is excluded because plain grep is unbounded; only `rg --files`
// / `rg -l` / `rg --files-with-matches` are allowed.
func opencodeAgentFrontmatter() string {
	return strings.Join([]string{
		"---",
		"description: brain-night stage agent",
		"mode: primary",
		"permission:",
		"  edit: deny",
		"  bash:",
		"    \"*\": deny",
		"    \"ls\": allow",
		"    \"ls *\": allow",
		"    \"pwd\": allow",
		"    \"stat *\": allow",
		"    \"file *\": allow",
		"    \"head *\": allow",
		"    \"tail *\": allow",
		"    \"wc *\": allow",
		"    \"find *\": allow",
		"    \"rg --files*\": allow",
		"    \"rg -l *\": allow",
		"    \"rg --files-with-matches *\": allow",
		"  '*': allow",
		"---",
	}, "\n")
}

func writeOpencodeAgent(req Request) error {
	dir := filepath.Join(req.Sandbox.XDGConfigHome, "opencode", "agent")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("opencode: mkdir agent: %w", err)
	}
	body, err := os.ReadFile(req.SystemPromptPath)
	if err != nil {
		return fmt.Errorf("opencode: read role prompt: %w", err)
	}
	full := opencodeAgentFrontmatter() + "\n" + string(body)
	return os.WriteFile(filepath.Join(dir, OpencodeAgentName+".md"), []byte(full), 0o600)
}

// writeOpencodeConfig writes a per-call opencode config that inherits
// the user's real provider / model definitions (so llamacpp/qwen27b and
// any other configured backends still resolve) but replaces the agent
// list and MCP entries with brain-night-specific ones.
func writeOpencodeConfig(req Request) error {
	realCfg := loadRealOpencodeConfig()
	if realCfg == nil {
		realCfg = map[string]any{}
	}
	realCfg["$schema"] = "https://opencode.ai/config.json"
	realCfg["mcp"] = mcpForOpencode(req.Sandbox.MCPServersFromFile())
	delete(realCfg, "agent")
	body, err := json.MarshalIndent(realCfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(req.Sandbox.XDGConfigHome, "opencode", "opencode.json"), body, 0o600)
}

// loadRealOpencodeConfig reads the user's actual ~/.config/opencode/
// opencode.json, returning nil on any error. Provider definitions are
// the part we cannot safely override; the file contains no secrets we
// would expose to the model (the model never sees the config; only the
// CLI does).
func loadRealOpencodeConfig() map[string]any {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	for _, name := range []string{"opencode.json", "config.json"} {
		path := filepath.Join(home, ".config", "opencode", name)
		body, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var out map[string]any
		if err := json.Unmarshal(body, &out); err != nil {
			continue
		}
		return out
	}
	return nil
}

func mcpForOpencode(servers MCPConfig) map[string]any {
	out := make(map[string]any, len(servers))
	for name, spec := range servers {
		out[name] = map[string]any{
			"type":    "local",
			"command": append([]string{spec.Command}, spec.Args...),
		}
	}
	return out
}

// parseOpencodeJSON extracts the assistant's deliverable from opencode
// --format json output. The model can emit the deliverable two ways:
//
//  1. As streaming "text" parts (free-form assistant prose).
//  2. As a "write" tool call whose part.state.input.content carries the
//     full file body the model intended to save. local OpenCode Qwen routes
//     the rewrite through this tool when the prompt nudges it toward a
//     save action; only inter-tool narration ("Now I have all the
//     evidence...", "Here's a summary...") arrives as text events, and
//     concatenating that narration is not the report.
//
// When any write tool call carried content, we prefer the last such
// content (overwrites win on disk). Otherwise we fall back to the
// concatenated text parts. The wrapping ``` fence is stripped either
// way. Token counts are scraped from part.tokens or top-level usage.
func parseOpencodeJSON(raw []byte) (body string, tin, tout int64) {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	var pieces []string
	var lastWrite string
	haveWrite := false
	for dec.More() {
		var ev map[string]any
		if err := dec.Decode(&ev); err != nil {
			break
		}
		if part, ok := ev["part"].(map[string]any); ok {
			if t, ok := part["text"].(string); ok && t != "" {
				pieces = append(pieces, t)
			}
			if tok, ok := part["tokens"].(map[string]any); ok {
				if v, ok := tok["input"].(float64); ok {
					tin = int64(v)
				}
				if v, ok := tok["output"].(float64); ok {
					tout = int64(v)
				}
			}
		}
		if u, ok := ev["usage"].(map[string]any); ok {
			if v, ok := u["input_tokens"].(float64); ok {
				tin = int64(v)
			}
			if v, ok := u["output_tokens"].(float64); ok {
				tout = int64(v)
			}
		}
		if c, ok := opencodeWriteContent(ev); ok {
			lastWrite = c
			haveWrite = true
		}
	}
	if haveWrite {
		body = strings.TrimSpace(lastWrite)
	} else {
		body = strings.TrimSpace(strings.Join(pieces, ""))
	}
	body = stripFences(body)
	return body, tin, tout
}

// opencodeWriteContent returns the file body the model passed to a
// "write" tool call, or ("", false) if the event isn't a completed
// write with a string-typed content field. We deliberately ignore
// "edit"/apply_patch tool inputs because their input shape is a
// {oldString, newString} patch, not a full body, and applying patches
// to reconstruct the on-disk file would force this parser to simulate
// the filesystem.
func opencodeWriteContent(ev map[string]any) (string, bool) {
	if t, _ := ev["type"].(string); t != "tool_use" {
		return "", false
	}
	part, ok := ev["part"].(map[string]any)
	if !ok {
		return "", false
	}
	if tool, _ := part["tool"].(string); tool != "write" {
		return "", false
	}
	state, ok := part["state"].(map[string]any)
	if !ok {
		return "", false
	}
	input, ok := state["input"].(map[string]any)
	if !ok {
		return "", false
	}
	c, _ := input["content"].(string)
	if c == "" {
		return "", false
	}
	return c, true
}

// ToolFailure is an instrumentation record for a single failed MCP
// tool call extracted from opencode's JSON event stream. Surfacing
// these fields was the deliverable of sloptools issue #130: the
// previous "mcp: <tool> (failed)" log gave no diagnostic at all, which
// blocked us from telling whether a failure cluster was a connection
// reset, a rate limit, or an upstream-config drift. Once enough real
// last_error values are logged we can pick the right intervention
// (retry-with-backoff, token bucket, or a config fix); guessing first
// is exactly what #130 warned against.
type ToolFailure struct {
	// Tool is the opencode-side tool identifier. opencode flattens
	// MCP tools to "<server>_<tool>" (e.g. "helpy_web_search",
	// "sloppy_brain_search"); we keep the flattened form because the
	// caller has not been observed splitting it back out reliably.
	Tool string
	// Attempt counts repeated failures of the same Tool inside one
	// opencode run, restarting at 1 per Tool. Useful for spotting the
	// "10+ web_search failures in a single escalate stage" pattern
	// called out in #130.
	Attempt int
	// LastError is the innermost error message the parser could
	// recover, in priority order: state.error, state.output (only when
	// status indicates failure), then a generic "no error detail"
	// sentinel. Never empty for an emitted ToolFailure.
	LastError string
}

// extractToolFailures walks the opencode `--format json` event stream
// and returns one ToolFailure per failed tool_use part, in stream
// order. A tool_use part is treated as failed when its
// part.state.status equals "errored" or "error" (opencode has emitted
// both spellings across versions).
//
// We deliberately keep this best-effort: opencode's event schema is
// not part of any stable contract we own, so unknown event shapes are
// silently skipped rather than logged as parse errors. The flip side
// is that a future opencode release that renames status values would
// need a corresponding update here; the alternative — strict schema
// validation — would risk dropping legitimate failures on every minor
// upgrade.
func extractToolFailures(raw []byte) []ToolFailure {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	var out []ToolFailure
	attempts := map[string]int{}
	for dec.More() {
		var ev map[string]any
		if err := dec.Decode(&ev); err != nil {
			break
		}
		if t, _ := ev["type"].(string); t != "tool_use" {
			continue
		}
		part, ok := ev["part"].(map[string]any)
		if !ok {
			continue
		}
		state, ok := part["state"].(map[string]any)
		if !ok {
			continue
		}
		status, _ := state["status"].(string)
		if status != "errored" && status != "error" {
			continue
		}
		tool, _ := part["tool"].(string)
		if tool == "" {
			tool = "unknown"
		}
		attempts[tool]++
		out = append(out, ToolFailure{
			Tool:      tool,
			Attempt:   attempts[tool],
			LastError: extractToolErrorMessage(state),
		})
	}
	return out
}

// extractToolErrorMessage pulls the most informative message out of an
// errored opencode tool_use state. Priority order: state.error (the
// dedicated field), state.output (some errored calls only populate
// output with the failure text), then a sentinel so the caller never
// emits a blank last_error.
func extractToolErrorMessage(state map[string]any) string {
	if msg, ok := state["error"].(string); ok && msg != "" {
		return msg
	}
	if msg, ok := state["output"].(string); ok && msg != "" {
		return msg
	}
	return "no error detail in opencode event"
}

// formatToolFailureLogLine renders one ToolFailure as a single-line
// structured log entry. The format is pinned by tests because future
// log scrapers (brain-sleep ingestion of brain-night transcripts,
// follow-up issues to #130) parse it. Embedded newlines in LastError
// are escaped so the entry stays one line.
func formatToolFailureLogLine(stage string, f ToolFailure) string {
	msg := strings.ReplaceAll(f.LastError, "\\", "\\\\")
	msg = strings.ReplaceAll(msg, "\"", "\\\"")
	msg = strings.ReplaceAll(msg, "\n", "\\n")
	msg = strings.ReplaceAll(msg, "\r", "\\r")
	return fmt.Sprintf("mcp tool_failure: stage=%s tool=%s attempt=%d last_error=\"%s\"",
		stage, f.Tool, f.Attempt, msg)
}

// stripFences removes a leading ```{lang}\n and trailing \n``` if present.
func stripFences(s string) string {
	if !strings.HasPrefix(s, "```") {
		return s
	}
	lines := strings.SplitN(s, "\n", 2)
	if len(lines) != 2 {
		return s
	}
	rest := lines[1]
	if !strings.HasSuffix(rest, "```") {
		return s
	}
	return strings.TrimRight(rest[:len(rest)-3], "\n")
}
