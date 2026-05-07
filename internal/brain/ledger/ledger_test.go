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
	// No session start -> only weekly ceiling applies; 40% > 35% so reject.
	if err := l.Guard(backend.ProviderAnthropic, time.Time{}, time.Now()); !errors.Is(err, ErrCapExceeded) {
		t.Fatalf("expected ErrCapExceeded, got %v", err)
	}
	if err := l.Guard(backend.ProviderOpenAI, time.Time{}, time.Now()); err != nil {
		t.Fatalf("openai guard should pass, got %v", err)
	}
}

// Per-night gate must refuse when this nightly run alone crossed 5%
// even though the rolling 7-day weekly cap (35%) is still well within.
func TestLedgerGuardRejectsOverNightlyCap(t *testing.T) {
	dir := t.TempDir()
	caps := PlanCaps{
		AnthropicWeeklyShareMax: 0.05,
		OpenAIWeeklyShareMax:    0.05,
		AnthropicTokensPerWeek:  10_000,
		OpenAITokensPerWeek:     10_000,
	}
	l, err := New(dir, caps)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	sessionStart := time.Now().UTC().Add(-30 * time.Minute)
	// 600 tokens this session -> 6% share; per-night cap = 5%. Reject.
	_ = l.Append(Entry{
		TS:        sessionStart.Add(time.Minute),
		Provider:  backend.ProviderAnthropic,
		TokensIn:  300,
		TokensOut: 300,
	})
	if err := l.Guard(backend.ProviderAnthropic, sessionStart, time.Now()); !errors.Is(err, ErrCapExceeded) {
		t.Fatalf("expected ErrCapExceeded for per-night, got %v", err)
	}
	// A different session start that excludes the spend -> pass.
	if err := l.Guard(backend.ProviderAnthropic, time.Now().UTC(), time.Now()); err != nil {
		t.Fatalf("fresh session should pass, got %v", err)
	}
}

// Per-night gate ignores spend that happened before sessionStart even
// when that older spend pushes the weekly window above 5%.
func TestLedgerGuardIgnoresPreSessionSpend(t *testing.T) {
	dir := t.TempDir()
	caps := PlanCaps{
		AnthropicWeeklyShareMax: 0.05,
		OpenAIWeeklyShareMax:    0.05,
		AnthropicTokensPerWeek:  10_000,
		OpenAITokensPerWeek:     10_000,
	}
	l, err := New(dir, caps)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	now := time.Now().UTC()
	// Earlier spend (yesterday) used 6% in one shot; weekly is at 6%, well
	// under 35%. A new session must not be blocked by yesterday's spend.
	_ = l.Append(Entry{
		TS:        now.Add(-24 * time.Hour),
		Provider:  backend.ProviderAnthropic,
		TokensIn:  300,
		TokensOut: 300,
	})
	if err := l.Guard(backend.ProviderAnthropic, now, now); err != nil {
		t.Fatalf("fresh session must not see yesterday's spend, got %v", err)
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
