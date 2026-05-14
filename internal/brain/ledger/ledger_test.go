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
		OpenAIWeeklyShareMax: 0.05,
		OpenAITokensPerWeek:  1000,
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
			Provider:  backend.ProviderOpenAI,
			Backend:   "codex",
			Model:     "gpt-5.5",
			TokensIn:  50,
			TokensOut: 50,
		}); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	share, err := l.WeeklyShare(backend.ProviderOpenAI, now)
	if err != nil {
		t.Fatalf("WeeklyShare: %v", err)
	}
	want := 300.0 / 1000.0
	if abs(share-want) > 0.001 {
		t.Fatalf("share got %.4f want %.4f", share, want)
	}
	local, err := l.WeeklyShare(backend.ProviderLocal, now)
	if err != nil {
		t.Fatalf("WeeklyShare local: %v", err)
	}
	if local != 0 {
		t.Fatalf("local share should be zero, got %.3f", local)
	}
	if _, err := os.Stat(filepath.Join(dir, "data", "llm-ledger.jsonl")); err != nil {
		t.Fatalf("ledger file missing: %v", err)
	}
}

func TestLedgerGuardRejectsOverCap(t *testing.T) {
	dir := t.TempDir()
	caps := PlanCaps{ // 5% per night => 35% weekly headroom
		OpenAIWeeklyShareMax: 0.05,
		OpenAITokensPerWeek:  1000,
	}
	l, err := New(dir, caps)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// 400 tokens consumed -> 40% share; threshold = 35%. Should reject.
	_ = l.Append(Entry{
		TS:        time.Now().UTC().Add(-time.Hour),
		Provider:  backend.ProviderOpenAI,
		TokensIn:  200,
		TokensOut: 200,
	})
	// No session start -> only weekly ceiling applies; 40% > 35% so reject.
	if err := l.Guard(backend.ProviderOpenAI, time.Time{}, time.Now()); !errors.Is(err, ErrCapExceeded) {
		t.Fatalf("expected ErrCapExceeded, got %v", err)
	}
	if err := l.Guard(backend.ProviderLocal, time.Time{}, time.Now()); err != nil {
		t.Fatalf("local guard should pass, got %v", err)
	}
}

// Per-night gate must refuse when this nightly run alone crossed 5%
// even though the rolling 7-day weekly cap (35%) is still well within.
func TestLedgerGuardRejectsOverNightlyCap(t *testing.T) {
	dir := t.TempDir()
	caps := PlanCaps{
		OpenAIWeeklyShareMax: 0.05,
		OpenAITokensPerWeek:  10_000,
	}
	l, err := New(dir, caps)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	sessionStart := time.Now().UTC().Add(-30 * time.Minute)
	// 600 tokens this session -> 6% share; per-night cap = 5%. Reject.
	_ = l.Append(Entry{
		TS:        sessionStart.Add(time.Minute),
		Provider:  backend.ProviderOpenAI,
		TokensIn:  300,
		TokensOut: 300,
	})
	if err := l.Guard(backend.ProviderOpenAI, sessionStart, time.Now()); !errors.Is(err, ErrCapExceeded) {
		t.Fatalf("expected ErrCapExceeded for per-night, got %v", err)
	}
	// A different session start that excludes the spend -> pass.
	if err := l.Guard(backend.ProviderOpenAI, time.Now().UTC(), time.Now()); err != nil {
		t.Fatalf("fresh session should pass, got %v", err)
	}
}

// Per-night gate ignores spend that happened before sessionStart even
// when that older spend pushes the weekly window above 5%.
func TestLedgerGuardIgnoresPreSessionSpend(t *testing.T) {
	dir := t.TempDir()
	caps := PlanCaps{
		OpenAIWeeklyShareMax: 0.05,
		OpenAITokensPerWeek:  10_000,
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
		Provider:  backend.ProviderOpenAI,
		TokensIn:  300,
		TokensOut: 300,
	})
	if err := l.Guard(backend.ProviderOpenAI, now, now); err != nil {
		t.Fatalf("fresh session must not see yesterday's spend, got %v", err)
	}
}

// Per-night call-count gate refuses the (N+1)th call to a provider in
// the current session, regardless of token totals.
func TestLedgerGuardRejectsOverCallCount(t *testing.T) {
	dir := t.TempDir()
	caps := PlanCaps{
		OpenAIWeeklyShareMax:   0.05, // big enough that token gate never fires
		OpenAITokensPerWeek:    1_000_000_000,
		OpenAIMaxCallsPerNight: 3,
	}
	l, err := New(dir, caps)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	sessionStart := time.Now().UTC().Add(-30 * time.Minute)
	for i := 0; i < 3; i++ {
		_ = l.Append(Entry{
			TS:        sessionStart.Add(time.Duration(i) * time.Minute),
			Provider:  backend.ProviderOpenAI,
			TokensIn:  10,
			TokensOut: 10,
		})
	}
	if err := l.Guard(backend.ProviderOpenAI, sessionStart, time.Now()); !errors.Is(err, ErrCapExceeded) {
		t.Fatalf("expected ErrCapExceeded after 3rd call, got %v", err)
	}
	if err := l.Guard(backend.ProviderLocal, sessionStart, time.Now()); err != nil {
		t.Fatalf("local cap fresh; should pass, got %v", err)
	}
}

// Pre-session calls don't count against the per-night call cap.
func TestLedgerGuardCallCountIgnoresPreSession(t *testing.T) {
	dir := t.TempDir()
	caps := PlanCaps{
		OpenAIWeeklyShareMax:   0.05,
		OpenAITokensPerWeek:    1_000_000_000,
		OpenAIMaxCallsPerNight: 3,
	}
	l, err := New(dir, caps)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	now := time.Now().UTC()
	for i := 0; i < 5; i++ {
		_ = l.Append(Entry{
			TS:       now.Add(-24 * time.Hour),
			Provider: backend.ProviderOpenAI,
		})
	}
	if err := l.Guard(backend.ProviderOpenAI, now, now); err != nil {
		t.Fatalf("yesterday's calls must not count toward today; got %v", err)
	}
}

// Zero MaxCallsPerNight disables the call gate (token gate still active).
func TestLedgerGuardCallCountZeroDisables(t *testing.T) {
	dir := t.TempDir()
	caps := PlanCaps{
		OpenAIWeeklyShareMax:   0.05,
		OpenAITokensPerWeek:    1_000_000_000,
		OpenAIMaxCallsPerNight: 0,
	}
	l, err := New(dir, caps)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	sessionStart := time.Now().UTC().Add(-time.Hour)
	for i := 0; i < 100; i++ {
		_ = l.Append(Entry{
			TS:        sessionStart.Add(time.Duration(i) * time.Second),
			Provider:  backend.ProviderOpenAI,
			TokensIn:  1,
			TokensOut: 1,
		})
	}
	if err := l.Guard(backend.ProviderOpenAI, sessionStart, time.Now()); err != nil {
		t.Fatalf("call gate should be disabled when cap=0; got %v", err)
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
