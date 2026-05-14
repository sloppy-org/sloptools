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
		OpenAIWeeklyShareMax: 0.05,
		OpenAITokensPerWeek:  1000,
	}
	l, err := ledger.New(dir, caps)
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	r := New(l, ov)
	return r, l, dir
}

func TestPickBulkStage_LlamacppMoE(t *testing.T) {
	r, _, _ := newTestRouter(t, Overrides{})
	p, err := r.Pick(StageFolderNote)
	if err != nil {
		t.Fatal(err)
	}
	if p.BackendID != LlamacppMoEBackendID {
		t.Fatalf("bulk stage wanted %s, got %s", LlamacppMoEBackendID, p.BackendID)
	}
	if p.Provider != backend.ProviderLocal {
		t.Fatalf("bulk provider wanted local, got %s", p.Provider)
	}
	if p.Model != LlamacppMoEModel {
		t.Fatalf("bulk model wanted %s, got %s", LlamacppMoEModel, p.Model)
	}
	if p.Reasoning != backend.ReasoningHigh {
		t.Fatalf("bulk reasoning wanted high, got %s", p.Reasoning)
	}
}

func TestDefaultBulkUsesLlamacppMoE(t *testing.T) {
	r, _, _ := newTestRouter(t, Overrides{})
	p, err := r.Pick(StageScout)
	if err != nil {
		t.Fatal(err)
	}
	if p.BackendID != LlamacppMoEBackendID {
		t.Fatalf("scout bulk wanted backendID=%s, got %s", LlamacppMoEBackendID, p.BackendID)
	}
	if p.Model != LlamacppMoEModel {
		t.Fatalf("scout bulk wanted model=%s, got %s", LlamacppMoEModel, p.Model)
	}
}

func TestPickValueLocal_ScoutReturnsQwen122B(t *testing.T) {
	r, _, _ := newTestRouter(t, Overrides{})
	p, err := r.PickValueLocal(StageScout)
	if err != nil {
		t.Fatal(err)
	}
	if p.BackendID != LlamacppMoEBackendID {
		t.Fatalf("value-local backendID wanted %s, got %s", LlamacppMoEBackendID, p.BackendID)
	}
	if p.Model != LlamacppQwen122BModel {
		t.Fatalf("value-local model wanted %s, got %s", LlamacppQwen122BModel, p.Model)
	}
}

func TestPickMediumStage_Qwen122BLocal(t *testing.T) {
	r, _, _ := newTestRouter(t, Overrides{})
	for i := 0; i < 4; i++ {
		p, err := r.Pick(StageSleepJudge)
		if err != nil {
			t.Fatal(err)
		}
		if p.BackendID != LlamacppMoEBackendID {
			t.Fatalf("medium pick %d wanted %s, got %s", i, LlamacppMoEBackendID, p.BackendID)
		}
		if p.Provider != backend.ProviderLocal {
			t.Fatalf("medium pick %d wanted local, got %s", i, p.Provider)
		}
		if p.Model != LlamacppQwen122BModel {
			t.Fatalf("medium pick %d wanted %s, got %s", i, LlamacppQwen122BModel, p.Model)
		}
		if p.Reasoning == "" {
			t.Fatalf("missing reasoning: %+v", p)
		}
	}
}

func TestPickTriage_UsesCodexMiniNativeWebFallback(t *testing.T) {
	r, _, _ := newTestRouter(t, Overrides{})
	p, err := r.Pick(StageTriage)
	if err != nil {
		t.Fatal(err)
	}
	if p.BackendID != "codex" {
		t.Fatalf("triage wanted codex, got %s", p.BackendID)
	}
	if p.Provider != backend.ProviderOpenAI {
		t.Fatalf("triage wanted openai, got %s", p.Provider)
	}
	if p.Model != "gpt-5.4-mini" {
		t.Fatalf("triage wanted gpt-5.4-mini, got %s", p.Model)
	}
	if p.Label != "codex/gpt-5.4-mini@medium-native-web" {
		t.Fatalf("triage label = %q", p.Label)
	}
}

func TestPickHardStage_CodexOnly(t *testing.T) {
	r, _, _ := newTestRouter(t, Overrides{})
	for i := 0; i < 4; i++ {
		p, err := r.Pick(StageEntityWrite)
		if err != nil {
			t.Fatal(err)
		}
		if p.BackendID != "codex" {
			t.Fatalf("hard pick %d wanted codex, got %s", i, p.BackendID)
		}
		if p.Model != "gpt-5.5" {
			t.Fatalf("hard pick %d wanted gpt-5.5, got %s", i, p.Model)
		}
		if p.Reasoning != backend.ReasoningHigh {
			t.Fatalf("hard pick %d reasoning wanted high, got %s", i, p.Reasoning)
		}
	}
}

func TestLedgerGuard_NativeWebFallbackFallsBackWhenOpenAISaturated(t *testing.T) {
	r, l, _ := newTestRouter(t, Overrides{})
	now := time.Now().UTC()
	r.now = func() time.Time { return now }
	// Saturate OpenAI. Cap is 1000 tokens/week × 0.05 max × 7 = 350 → push past it.
	if err := l.Append(ledger.Entry{
		TS: now, Provider: backend.ProviderOpenAI, TokensIn: 200, TokensOut: 200,
	}); err != nil {
		t.Fatal(err)
	}
	p, err := r.Pick(StageTriage)
	if err != nil {
		t.Fatal(err)
	}
	if p.Provider != backend.ProviderLocal {
		t.Fatalf("saturated paid provider should fall back to local, picked %s", p.Provider)
	}
}

func TestLedgerGuard_FallsBackToBulkWhenPaidProviderSaturated(t *testing.T) {
	r, l, _ := newTestRouter(t, Overrides{})
	now := time.Now().UTC()
	r.now = func() time.Time { return now }
	if err := l.Append(ledger.Entry{
		TS: now, Provider: backend.ProviderOpenAI, TokensIn: 200, TokensOut: 200,
	}); err != nil {
		t.Fatal(err)
	}
	pick, err := r.Pick(StageEntityWrite)
	if err != nil {
		t.Fatal(err)
	}
	if pick.Provider != backend.ProviderLocal {
		t.Fatalf("expected fallback to local, got %s", pick.Provider)
	}
	if pick.BackendID != "llamacpp" {
		t.Fatalf("expected llamacpp fallback, got %s", pick.BackendID)
	}
	if pick.Model != LlamacppMoEModel {
		t.Fatalf("expected fallback model %s, got %s", LlamacppMoEModel, pick.Model)
	}
}

func TestOverride_OpenAITier(t *testing.T) {
	r, _, _ := newTestRouter(t, Overrides{OpenAITier: "full"})
	p, err := r.Pick(StageSleepJudge)
	if err != nil {
		t.Fatal(err)
	}
	if p.Model != "gpt-5.5" || p.Provider != backend.ProviderOpenAI {
		t.Fatalf("override failed: %+v", p)
	}
}

func TestOverride_OpenAITierMiniNativeWeb(t *testing.T) {
	r, _, _ := newTestRouter(t, Overrides{OpenAITier: "mini-native-web"})
	p, err := r.Pick(StageSleepJudge)
	if err != nil {
		t.Fatal(err)
	}
	if p.Model != "gpt-5.4-mini" || p.Label != "codex/gpt-5.4-mini@medium-native-web" {
		t.Fatalf("override failed: %+v", p)
	}
}

func TestOverride_ForceLocal(t *testing.T) {
	r, _, _ := newTestRouter(t, Overrides{ForceLocal: true})
	p, err := r.Pick(StageEntityWrite)
	if err != nil {
		t.Fatal(err)
	}
	if p.Provider != backend.ProviderLocal {
		t.Fatalf("ForceLocal: expected local provider, got %+v", p)
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

func TestApplyStages_RejectsUnknownProvider(t *testing.T) {
	cfg := &FileConfig{Stages: map[string]StageFile{
		"sleep-judge": {
			Tier: "medium",
			Medium: []ChoiceFile{
				{Backend: "external", Provider: "unknown", Model: "external-model", Reasoning: "medium"},
			},
		},
	}}
	if _, err := cfg.ApplyStages(DefaultStageConfigs()); err == nil {
		t.Fatal("expected unknown provider override to be rejected")
	}
}
