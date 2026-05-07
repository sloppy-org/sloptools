package ledger

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sloppy-org/sloptools/internal/brain/backend"
)

func TestLedgerAppendAndShare(t *testing.T) {
	dir := t.TempDir()
	caps := PlanCaps{
		AnthropicWeeklyShareMax: 0.05,
		OpenAIWeeklyShareMax:    0.05,
		AnthropicTokensPerWeek:  1000,
		OpenAITokensPerWeek:     1000,
	}
	l, err := New(dir, caps)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		if err := l.Append(Entry{
			TS:        now.Add(-time.Hour * time.Duration(i)),
			Sphere:    "work",
			Stage:     "sleep-judge",
			Provider:  backend.ProviderAnthropic,
			Backend:   "claude",
			Model:     "claude-haiku-4-5",
			TokensIn:  50,
			TokensOut: 50,
		}); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	share, err := l.WeeklyShare(backend.ProviderAnthropic, now)
	if err != nil {
		t.Fatalf("WeeklyShare: %v", err)
	}
	want := 300.0 / 1000.0
	if abs(share-want) > 0.001 {
		t.Fatalf("share got %.4f want %.4f", share, want)
	}
	openai, err := l.WeeklyShare(backend.ProviderOpenAI, now)
	if err != nil {
		t.Fatalf("WeeklyShare openai: %v", err)
	}
	if openai != 0 {
		t.Fatalf("openai share should be zero, got %.3f", openai)
	}
	if _, err := os.Stat(filepath.Join(dir, "data", "llm-ledger.jsonl")); err != nil {
		t.Fatalf("ledger file missing: %v", err)
	}
}

func TestLedgerGuardRejectsOverCap(t *testing.T) {
	dir := t.TempDir()
	caps := PlanCaps{
		AnthropicWeeklyShareMax: 0.05, // 5% per night => 35% weekly headroom
		OpenAIWeeklyShareMax:    0.05,
		AnthropicTokensPerWeek:  1000,
		OpenAITokensPerWeek:     1000,
	}
	l, err := New(dir, caps)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// 400 tokens consumed -> 40% share; threshold = 35%. Should reject.
	_ = l.Append(Entry{
		TS:        time.Now().UTC().Add(-time.Hour),
		Provider:  backend.ProviderAnthropic,
		TokensIn:  200,
		TokensOut: 200,
	})
	if err := l.Guard(backend.ProviderAnthropic, time.Now()); !errors.Is(err, ErrCapExceeded) {
		t.Fatalf("expected ErrCapExceeded, got %v", err)
	}
	if err := l.Guard(backend.ProviderOpenAI, time.Now()); err != nil {
		t.Fatalf("openai guard should pass, got %v", err)
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
