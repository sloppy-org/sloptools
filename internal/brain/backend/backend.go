// Package backend defines a uniform Backend interface over the three model
// CLIs we use: claude, codex, opencode. Every brain-night call goes through
// one of these. No SDK or HTTP API path exists.
package backend

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Provider identifies the upstream plan that owns a backend's quota.
type Provider string

const (
	ProviderOpenAI    Provider = "openai"
	ProviderAnthropic Provider = "anthropic"
	ProviderLocal     Provider = "local"
)

// Reasoning is a CLI-level reasoning effort hint. Values map to:
//
//   - codex `-c model_reasoning_effort="..."` (minimal | low | medium | high | xhigh)
//   - claude `--effort ...` (low | medium | high | xhigh | max)
//   - opencode `--variant ...` (provider-specific; high | max | minimal)
//
// Brain-night defaults are deliberately NOT xhigh: the tasks are
// medium-complexity bounded packets, not the hardest asynchronous
// agentic eval workloads xhigh is meant for. Medium is the balanced
// starting point GPT-5.5 documents as recommended; high only for the
// hard tier (canonical Markdown writes, contradiction adjudication).
type Reasoning string

const (
	ReasoningMinimal Reasoning = "minimal"
	ReasoningLow     Reasoning = "low"
	ReasoningMedium  Reasoning = "medium"
	ReasoningHigh    Reasoning = "high"
	ReasoningXHigh   Reasoning = "xhigh"
	ReasoningMax     Reasoning = "max"
)

// Backend runs one model call via its CLI in one-shot mode.
type Backend interface {
	Provider() Provider
	Name() string
	Run(ctx context.Context, req Request) (Response, error)
}

// Request is the per-call input. SystemPromptPath, Sandbox, Model, and
// Packet are mandatory. The remaining fields adjust CLI behavior.
//
// WorkDir, when non-empty, overrides the per-stage scratch workdir as
// the child's cwd and the codex `-C` argument. Set this for stages that
// must edit files outside the sandbox, e.g. the sleep judge editing
// canonical Markdown in the brain vault. When empty, the child runs in
// Sandbox.WorkDir (default for read-only stages).
type Request struct {
	Stage            string
	Packet           string
	SystemPromptPath string
	Model            string
	Reasoning        Reasoning
	AllowEdits       bool
	MCPAllowList     []string
	MaxBudgetUSD     float64
	Sandbox          *Sandbox
	WorkDir          string
}

// Response is the per-call output. Tokens / wall / cost are best-effort:
// not every CLI exposes them, in which case the field is zero.
type Response struct {
	Output    string
	TokensIn  int64
	TokensOut int64
	WallMS    int64
	CostHint  float64
}

// ErrEmptyOutput is returned when a backend ran without error but its
// output stream was empty after trimming whitespace.
var ErrEmptyOutput = errors.New("backend produced empty output")

func (req Request) validate() error {
	if strings.TrimSpace(req.Stage) == "" {
		return fmt.Errorf("backend: Stage is required")
	}
	if strings.TrimSpace(req.Model) == "" {
		return fmt.Errorf("backend: Model is required")
	}
	if strings.TrimSpace(string(req.Reasoning)) == "" {
		return fmt.Errorf("backend: Reasoning is required (medium for routine stages, high for hard tier; never xhigh by default)")
	}
	if strings.TrimSpace(req.SystemPromptPath) == "" {
		return fmt.Errorf("backend: SystemPromptPath is required")
	}
	if req.Sandbox == nil {
		return fmt.Errorf("backend: Sandbox is required")
	}
	return nil
}
