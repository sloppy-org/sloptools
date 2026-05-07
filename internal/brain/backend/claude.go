package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// ClaudeBackend invokes `claude -p` in one-shot mode under a scratch
// sandbox. The role-specific system prompt replaces the default; the
// home CLAUDE.md is never auto-discovered because HOME is overridden.
type ClaudeBackend struct{}

// Provider returns Anthropic.
func (ClaudeBackend) Provider() Provider { return ProviderAnthropic }

// Name returns the backend identifier used in the ledger.
func (ClaudeBackend) Name() string { return "claude" }

// Run shells out to claude -p with --system-prompt-file and --mcp-config.
func (ClaudeBackend) Run(ctx context.Context, req Request) (Response, error) {
	if err := req.validate(); err != nil {
		return Response{}, err
	}
	args := []string{
		"-p",
		"--system-prompt-file", req.SystemPromptPath,
		"--model", req.Model,
		"--effort", string(req.Reasoning),
		"--output-format", "json",
		"--no-session-persistence",
	}
	if req.MaxBudgetUSD > 0 {
		args = append(args, "--max-budget-usd", fmt.Sprintf("%.4f", req.MaxBudgetUSD))
	}
	if req.Sandbox.MCPConfigPath != "" {
		args = append(args, "--mcp-config", req.Sandbox.MCPConfigPath)
	}
	if req.AllowEdits {
		args = append(args, "--permission-mode", "acceptEdits")
	} else {
		args = append(args, "--permission-mode", "default")
	}
	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Env = req.Sandbox.Env()
	cmd.Dir = req.Sandbox.WorkDir
	cmd.Stdin = strings.NewReader(req.Packet)
	start := time.Now()
	out, err := cmd.Output()
	wall := time.Since(start)
	if err != nil {
		stderr := ""
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = string(ee.Stderr)
		}
		return Response{}, fmt.Errorf("claude exec: %w; stderr=%s", err, stderr)
	}
	body, tin, tout, costHint, parseErr := parseClaudeJSON(out)
	if parseErr != nil {
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
		CostHint:  costHint,
	}, nil
}

// parseClaudeJSON extracts the assistant text and token counts from
// `claude -p --output-format json` output. The exact schema varies
// between releases; we tolerate both the `{type:"result", result, usage}`
// and the `{result, total_cost_usd, usage}` shapes.
func parseClaudeJSON(raw []byte) (body string, tin, tout int64, cost float64, err error) {
	var top map[string]any
	if jerr := json.Unmarshal(raw, &top); jerr != nil {
		return "", 0, 0, 0, jerr
	}
	if v, ok := top["result"].(string); ok {
		body = v
	} else if v, ok := top["text"].(string); ok {
		body = v
	}
	if v, ok := top["total_cost_usd"].(float64); ok {
		cost = v
	}
	if u, ok := top["usage"].(map[string]any); ok {
		if v, ok := u["input_tokens"].(float64); ok {
			tin = int64(v)
		}
		if v, ok := u["output_tokens"].(float64); ok {
			tout = int64(v)
		}
	}
	return body, tin, tout, cost, nil
}
