package routing

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/sloppy-org/sloptools/internal/brain/backend"
	"github.com/sloppy-org/sloptools/internal/brain/ledger"
)

func newTestRouter(t *testing.T, ov Overrides) (*Router, *ledger.Ledger, string) {
	t.Helper()
	dir := t.TempDir()
	caps := ledger.PlanCaps{
		AnthropicWeeklyShareMax: 0.05,
		OpenAIWeeklyShareMax:    0.05,
		AnthropicTokensPerWeek:  1000,
		OpenAITokensPerWeek:     1000,
	}
	l, err := ledger.New(dir, caps)
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	r := New(l, ov)
	return r, l, dir
}

func TestPickBulkStage_OpencodeAlways(t *testing.T) {
	r, _, _ := newTestRouter(t, Overrides{})
	p, err := r.Pick(StageFolderNote)
	if err != nil {
		t.Fatal(err)
	}
	if p.BackendID != "opencode" {
		t.Fatalf("bulk stage wanted opencode, got %s", p.BackendID)
	}
	if p.Provider != backend.ProviderLocal {
		t.Fatalf("bulk provider wanted local, got %s", p.Provider)
	}
	if p.Reasoning != backend.ReasoningHigh {
		t.Fatalf("bulk reasoning wanted high, got %s", p.Reasoning)
	}
}

func TestPickMediumStage_AlternatesProviders(t *testing.T) {
	r, _, _ := newTestRouter(t, Overrides{})
	picks := []Pick{}
	for i := 0; i < 4; i++ {
		p, err := r.Pick(StageSleepJudge)
		if err != nil {
			t.Fatal(err)
		}
		picks = append(picks, p)
	}
	if picks[0].Provider == picks[1].Provider {
		t.Fatalf("expected alternation, picks[0]=%s picks[1]=%s", picks[0].Provider, picks[1].Provider)
	}
	if picks[2].Provider != picks[0].Provider {
		t.Fatalf("expected wrap, picks[2]=%s picks[0]=%s", picks[2].Provider, picks[0].Provider)
	}
	for _, p := range picks {
		if p.Provider == backend.ProviderLocal {
			t.Fatalf("medium tier should not pick local: %+v", p)
		}
		if p.Reasoning == "" {
			t.Fatalf("missing reasoning: %+v", p)
		}
	}
}

func TestPickHardStage_RoundRobinIndependentFromMedium(t *testing.T) {
	r, _, _ := newTestRouter(t, Overrides{})
	hard1, _ := r.Pick(StageEntityWrite)
	med1, _ := r.Pick(StageSleepJudge)
	hard2, _ := r.Pick(StageEntityWrite)
	if hard1.Provider == hard2.Provider {
		t.Fatalf("hard pool should alternate, got %s %s", hard1.Provider, hard2.Provider)
	}
	if med1.Provider == backend.ProviderLocal {
		t.Fatalf("medium tier should not be local")
	}
	if hard1.Reasoning != backend.ReasoningHigh && hard1.Reasoning != backend.ReasoningMedium {
		t.Fatalf("hard tier reasoning unexpected: %s", hard1.Reasoning)
	}
}

func TestLedgerGuard_SkipsSaturatedProvider(t *testing.T) {
	r, l, _ := newTestRouter(t, Overrides{})
	now := time.Now().UTC()
	r.now = func() time.Time { return now }
	// Saturate Anthropic. Cap is 1000 tokens/week × 0.05 max × 7 = 350 → push past it.
	if err := l.Append(ledger.Entry{
		TS: now, Provider: backend.ProviderAnthropic, TokensIn: 200, TokensOut: 200,
	}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		p, err := r.Pick(StageSleepJudge)
		if err != nil {
			t.Fatal(err)
		}
		if p.Provider == backend.ProviderAnthropic {
			t.Fatalf("saturated provider should be skipped, picked %s", p.Provider)
		}
	}
}

func TestLedgerGuard_FallsBackToBulkWhenBothSaturated(t *testing.T) {
	r, l, _ := newTestRouter(t, Overrides{})
	now := time.Now().UTC()
	r.now = func() time.Time { return now }
	for _, p := range []backend.Provider{backend.ProviderAnthropic, backend.ProviderOpenAI} {
		if err := l.Append(ledger.Entry{
			TS: now, Provider: p, TokensIn: 200, TokensOut: 200,
		}); err != nil {
			t.Fatal(err)
		}
	}
	pick, err := r.Pick(StageSleepJudge)
	if err != nil {
		t.Fatal(err)
	}
	if pick.Provider != backend.ProviderLocal {
		t.Fatalf("expected fallback to local, got %s", pick.Provider)
	}
	if pick.BackendID != "opencode" {
		t.Fatalf("expected opencode fallback, got %s", pick.BackendID)
	}
}

func TestOverride_ClaudeTier(t *testing.T) {
	r, _, _ := newTestRouter(t, Overrides{ClaudeTier: "opus"})
	p, err := r.Pick(StageSleepJudge)
	if err != nil {
		t.Fatal(err)
	}
	if p.Model != "claude-opus-4-7" || p.Provider != backend.ProviderAnthropic {
		t.Fatalf("override failed: %+v", p)
	}
}

func TestOverride_ForceLocal(t *testing.T) {
	r, _, _ := newTestRouter(t, Overrides{ForceLocal: true})
	p, err := r.Pick(StageEntityWrite)
	if err != nil {
		t.Fatal(err)
	}
	if p.BackendID != "opencode" {
		t.Fatalf("ForceLocal failed: %+v", p)
	}
}

func TestLoadFile_MissingReturnsNilNoError(t *testing.T) {
	dir := t.TempDir()
	cfg, err := LoadFile(filepath.Join(dir, "absent.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg != nil {
		t.Fatalf("absent file should return nil, got %+v", cfg)
	}
}

func TestApplyStages_OverrideMediumPool(t *testing.T) {
	cfg := &FileConfig{Stages: map[string]StageFile{
		"sleep-judge": {
			Tier: "medium",
			Medium: []ChoiceFile{
				{Backend: "claude", Provider: "anthropic", Model: "claude-sonnet-4-6", Reasoning: "medium"},
			},
		},
	}}
	out, err := cfg.ApplyStages(DefaultStageConfigs())
	if err != nil {
		t.Fatal(err)
	}
	pool := out[StageSleepJudge].Medium
	if len(pool) != 1 || pool[0].Model != "claude-sonnet-4-6" {
		t.Fatalf("override not applied: %+v", pool)
	}
}
