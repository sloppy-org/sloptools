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
	args := []string{
		"run",
		"--pure",
		"--print-logs",
		"--log-level", "WARN",
		"--agent", OpencodeAgentName,
		"--model", req.Model,
		"--variant", string(req.Reasoning),
		"--format", "json",
		"--dir", cwd,
		"--dangerously-skip-permissions",
		req.Packet,
	}

	full := append([]string{"flock", OpencodeLockPath, "opencode"}, args...)
	cmd := exec.CommandContext(ctx, full[0], full[1:]...)
	cmd.Env = req.Sandbox.Env()
	cmd.Dir = cwd

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

func writeOpencodeAgent(req Request) error {
	dir := filepath.Join(req.Sandbox.XDGConfigHome, "opencode", "agent")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("opencode: mkdir agent: %w", err)
	}
	body, err := os.ReadFile(req.SystemPromptPath)
	if err != nil {
		return fmt.Errorf("opencode: read role prompt: %w", err)
	}
	// opencode requires mode: primary so the agent is a top-level
	// conversation owner, and an explicit permission allow so MCP tool
	// calls are not auto-denied. Without these the global `permission:
	// allow` does not propagate to a custom agent and tool calls silently
	// drop, leaving the model to confabulate sources. Match the user's
	// known-working brain-evidence-scout agent shape.
	full := strings.Join([]string{
		"---",
		"description: brain-night stage agent",
		"mode: primary",
		"permission:",
		"  '*': allow",
		"---",
		"",
		string(body),
	}, "\n")
	return os.WriteFile(filepath.Join(dir, OpencodeAgentName+".md"), []byte(full), 0o600)
}

// writeOpencodeConfig writes a per-call opencode config that inherits
// the user's real provider / model definitions (so llamacpp/qwen and
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

// parseOpencodeJSON extracts the assistant message and best-effort
// token counts from opencode --format json output. The opencode
// streaming events look like:
//
//	{"type":"step_start", ...}
//	{"type":"text", "part":{"type":"text", "text":"..."} ...}
//	{"type":"step_finish", "part":{...,"tokens":{...}} ...}
//
// We collect every "text" part and trim a wrapping ``` fence the model
// sometimes emits despite the prompt asking for plain Markdown.
func parseOpencodeJSON(raw []byte) (body string, tin, tout int64) {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	var pieces []string
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
	}
	body = strings.Join(pieces, "")
	body = stripFences(strings.TrimSpace(body))
	return body, tin, tout
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
