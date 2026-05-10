package routing

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/sloppy-org/sloptools/internal/brain/backend"
	"github.com/sloppy-org/sloptools/internal/brain/ledger"
)

// FileConfig is the on-disk shape of ~/.config/sloptools/brain.toml. Any
// field left zero falls back to the in-code default.
type FileConfig struct {
	Plan     PlanFile             `toml:"plan"`
	Llamacpp LlamacppFile         `toml:"llamacpp"`
	Stages   map[string]StageFile `toml:"stage"`
}

// LlamacppFile carries optional llamacpp-specific config.
type LlamacppFile struct {
	// AffinityUUID overrides the x-session-affinity base UUID read from
	// ~/.config/opencode/opencode.json. Useful for routing to a specific
	// slopgate slot independent of the opencode config.
	AffinityUUID string `toml:"affinity_uuid"`
}

// PlanFile carries weekly cap overrides per provider.
type PlanFile struct {
	AnthropicShareMax         float64 `toml:"anthropic_weekly_share_max"`
	OpenAIShareMax            float64 `toml:"openai_weekly_share_max"`
	AnthropicTokensPerWeek    int64   `toml:"anthropic_tokens_per_week"`
	OpenAITokensPerWeek       int64   `toml:"openai_tokens_per_week"`
	AnthropicMaxCallsPerNight int     `toml:"anthropic_max_calls_per_night"`
	OpenAIMaxCallsPerNight    int     `toml:"openai_max_calls_per_night"`
}

// StageFile overrides one stage's tier and pool.
type StageFile struct {
	Tier       string       `toml:"tier"`
	Bulk       ChoiceFile   `toml:"bulk"`
	ValueLocal ChoiceFile   `toml:"value_local"`
	Medium     []ChoiceFile `toml:"medium"`
	Hard       []ChoiceFile `toml:"hard"`
	MCPTools   []string     `toml:"mcp_tools"`
}

// ChoiceFile is one choice in a tier pool.
type ChoiceFile struct {
	Backend   string `toml:"backend"`
	Provider  string `toml:"provider"`
	Model     string `toml:"model"`
	Reasoning string `toml:"reasoning"`
	Label     string `toml:"label"`
}

// LoadFile reads brain.toml from path; an empty path resolves to
// ~/.config/sloptools/brain.toml. Returns (nil, nil) when the file is
// absent — the router falls back to DefaultStageConfigs.
func LoadFile(path string) (*FileConfig, error) {
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("routing: %w", err)
		}
		path = filepath.Join(home, ".config", "sloptools", "brain.toml")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("routing: read %s: %w", path, err)
	}
	var cfg FileConfig
	if err := toml.Unmarshal(body, &cfg); err != nil {
		return nil, fmt.Errorf("routing: parse %s: %w", path, err)
	}
	return &cfg, nil
}

// PlanCaps converts the file plan section to ledger.PlanCaps. Missing
// fields fall back to ledger.DefaultPlanCaps().
func (c *FileConfig) PlanCaps() ledger.PlanCaps {
	d := ledger.DefaultPlanCaps()
	if c == nil {
		return d
	}
	if c.Plan.AnthropicShareMax > 0 {
		d.AnthropicWeeklyShareMax = c.Plan.AnthropicShareMax
	}
	if c.Plan.OpenAIShareMax > 0 {
		d.OpenAIWeeklyShareMax = c.Plan.OpenAIShareMax
	}
	if c.Plan.AnthropicTokensPerWeek > 0 {
		d.AnthropicTokensPerWeek = c.Plan.AnthropicTokensPerWeek
	}
	if c.Plan.OpenAITokensPerWeek > 0 {
		d.OpenAITokensPerWeek = c.Plan.OpenAITokensPerWeek
	}
	if c.Plan.AnthropicMaxCallsPerNight > 0 {
		d.AnthropicMaxCallsPerNight = c.Plan.AnthropicMaxCallsPerNight
	}
	if c.Plan.OpenAIMaxCallsPerNight > 0 {
		d.OpenAIMaxCallsPerNight = c.Plan.OpenAIMaxCallsPerNight
	}
	return d
}

// ApplyStages merges per-stage overrides into the in-code defaults. The
// returned map is a fresh copy.
func (c *FileConfig) ApplyStages(defaults map[Stage]StageConfig) (map[Stage]StageConfig, error) {
	out := make(map[Stage]StageConfig, len(defaults))
	for k, v := range defaults {
		out[k] = v
	}
	if c == nil {
		return out, nil
	}
	for name, sf := range c.Stages {
		s := Stage(name)
		base, ok := out[s]
		if !ok {
			base = StageConfig{Stage: s}
		}
		if sf.Tier != "" {
			base.Tier = Tier(strings.ToLower(sf.Tier))
		}
		if !isZero(sf.Bulk) {
			c, err := sf.Bulk.toChoice()
			if err != nil {
				return nil, fmt.Errorf("stage %s.bulk: %w", name, err)
			}
			base.Bulk = c
			base.Fallback = c
		}
		if !isZero(sf.ValueLocal) {
			c, err := sf.ValueLocal.toChoice()
			if err != nil {
				return nil, fmt.Errorf("stage %s.value_local: %w", name, err)
			}
			base.ValueLocal = c
		}
		if len(sf.Medium) > 0 {
			pool, err := toPool(sf.Medium)
			if err != nil {
				return nil, fmt.Errorf("stage %s.medium: %w", name, err)
			}
			base.Medium = pool
		}
		if len(sf.Hard) > 0 {
			pool, err := toPool(sf.Hard)
			if err != nil {
				return nil, fmt.Errorf("stage %s.hard: %w", name, err)
			}
			base.Hard = pool
		}
		if len(sf.MCPTools) > 0 {
			base.MCPTools = sf.MCPTools
		}
		out[s] = base
	}
	return out, nil
}

func toPool(in []ChoiceFile) ([]Choice, error) {
	out := make([]Choice, 0, len(in))
	for _, c := range in {
		ch, err := c.toChoice()
		if err != nil {
			return nil, err
		}
		out = append(out, ch)
	}
	return out, nil
}

func (c ChoiceFile) toChoice() (Choice, error) {
	prov, err := parseProvider(c.Provider)
	if err != nil {
		return Choice{}, err
	}
	r, err := parseReasoning(c.Reasoning)
	if err != nil {
		return Choice{}, err
	}
	if c.Backend == "" {
		return Choice{}, fmt.Errorf("backend required")
	}
	if c.Model == "" {
		return Choice{}, fmt.Errorf("model required")
	}
	label := c.Label
	if label == "" {
		label = fmt.Sprintf("%s/%s@%s", c.Backend, c.Model, r)
	}
	return Choice{
		BackendID: c.Backend,
		Provider:  prov,
		Model:     c.Model,
		Reasoning: r,
		Label:     label,
	}, nil
}

func parseProvider(p string) (backend.Provider, error) {
	switch strings.ToLower(p) {
	case "openai":
		return backend.ProviderOpenAI, nil
	case "anthropic":
		return backend.ProviderAnthropic, nil
	case "local", "":
		return backend.ProviderLocal, nil
	}
	return "", fmt.Errorf("unknown provider %q", p)
}

func parseReasoning(r string) (backend.Reasoning, error) {
	switch strings.ToLower(r) {
	case "minimal":
		return backend.ReasoningMinimal, nil
	case "low":
		return backend.ReasoningLow, nil
	case "medium", "":
		return backend.ReasoningMedium, nil
	case "high":
		return backend.ReasoningHigh, nil
	case "xhigh":
		return backend.ReasoningXHigh, nil
	case "max":
		return backend.ReasoningMax, nil
	}
	return "", fmt.Errorf("unknown reasoning %q", r)
}

func isZero(c ChoiceFile) bool {
	return c.Backend == "" && c.Model == "" && c.Reasoning == "" && c.Provider == ""
}
