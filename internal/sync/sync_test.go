package sync

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/sloppy-org/sloptools/internal/store"
)

type fakeAccountSource struct {
	accounts []store.ExternalAccount
}

func (f fakeAccountSource) ListExternalAccounts(string) ([]store.ExternalAccount, error) {
	out := make([]store.ExternalAccount, len(f.accounts))
	copy(out, f.accounts)
	return out, nil
}

type fakeCleaner struct {
	staleByProvider map[string][]store.ExternalBinding
	deleted         []int64
}

func (f *fakeCleaner) ListStaleBindings(provider string, _ time.Time) ([]store.ExternalBinding, error) {
	out := make([]store.ExternalBinding, len(f.staleByProvider[provider]))
	copy(out, f.staleByProvider[provider])
	return out, nil
}

func (f *fakeCleaner) DeleteBinding(id int64) error {
	f.deleted = append(f.deleted, id)
	return nil
}

type fakeSink struct{}

func (fakeSink) UpsertItem(context.Context, store.Item, store.ExternalBinding) (store.Item, error) {
	return store.Item{}, nil
}

func (fakeSink) UpsertArtifact(context.Context, store.Artifact, store.ExternalBinding) (store.Artifact, error) {
	return store.Artifact{}, nil
}

type fakeProvider struct {
	name  string
	calls []int64
	err   error
}

func (f *fakeProvider) Name() string {
	return f.name
}

func (f *fakeProvider) Sync(_ context.Context, account store.ExternalAccount, _ Sink) error {
	f.calls = append(f.calls, account.ID)
	return f.err
}

type fakePolicyProvider struct {
	fakeProvider
	policy SyncPolicy
}

func (f *fakePolicyProvider) SyncPolicy(context.Context, store.ExternalAccount) (SyncPolicy, error) {
	return f.policy, nil
}

func TestEngineRunOnceAppliesIntervalsRateLimitsCleanupAndErrorIsolation(t *testing.T) {
	todoConfig, err := json.Marshal(map[string]any{"sync_interval_seconds": 600})
	if err != nil {
		t.Fatalf("Marshal(todoConfig) error: %v", err)
	}
	source := fakeAccountSource{
		accounts: []store.ExternalAccount{
			{ID: 3, Provider: store.ExternalProviderBear, Label: "bear", Enabled: true},
			{ID: 1, Provider: store.ExternalProviderTodoist, Label: "todo-a", Enabled: true, ConfigJSON: string(todoConfig)},
			{ID: 2, Provider: store.ExternalProviderTodoist, Label: "todo-b", Enabled: true},
			{ID: 4, Provider: store.ExternalProviderICS, Label: "ics-off", Enabled: false},
		},
	}
	cleaner := &fakeCleaner{
		staleByProvider: map[string][]store.ExternalBinding{
			store.ExternalProviderTodoist: {
				{ID: 11, AccountID: 1, Provider: store.ExternalProviderTodoist},
				{ID: 12, AccountID: 999, Provider: store.ExternalProviderTodoist},
			},
		},
	}
	todoProvider := &fakeProvider{name: store.ExternalProviderTodoist}
	bearProvider := &fakeProvider{name: store.ExternalProviderBear, err: errors.New("bear failed")}

	engine := NewEngine(source, cleaner, fakeSink{}, Options{
		DefaultInterval: 5 * time.Minute,
		ProviderLimits: map[string]time.Duration{
			store.ExternalProviderTodoist: 2 * time.Second,
		},
		StaleAfter: time.Hour,
	})

	now := time.Date(2026, time.March, 9, 12, 0, 0, 0, time.UTC)
	var sleeps []time.Duration
	engine.now = func() time.Time { return now }
	engine.sleep = func(_ context.Context, d time.Duration) error {
		sleeps = append(sleeps, d)
		now = now.Add(d)
		return nil
	}
	engine.Register(todoProvider)
	engine.Register(bearProvider)

	result, err := engine.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce() error: %v", err)
	}
	if !reflect.DeepEqual(todoProvider.calls, []int64{1, 2}) {
		t.Fatalf("todoProvider.calls = %#v, want [1 2]", todoProvider.calls)
	}
	if !reflect.DeepEqual(bearProvider.calls, []int64{3}) {
		t.Fatalf("bearProvider.calls = %#v, want [3]", bearProvider.calls)
	}
	if !reflect.DeepEqual(sleeps, []time.Duration{0, 0, 2 * time.Second}) {
		t.Fatalf("sleep calls = %#v, want [0 0 2s]", sleeps)
	}
	if !reflect.DeepEqual(cleaner.deleted, []int64{11}) {
		t.Fatalf("cleaner.deleted = %#v, want [11]", cleaner.deleted)
	}
	if len(result.Accounts) != 3 {
		t.Fatalf("len(result.Accounts) = %d, want 3", len(result.Accounts))
	}
	if result.Accounts[0].Provider != store.ExternalProviderBear || result.Accounts[0].Err == nil {
		t.Fatalf("bear result = %#v, want provider error", result.Accounts[0])
	}
	if result.NextDelay != 5*time.Minute {
		t.Fatalf("result.NextDelay = %s, want %s", result.NextDelay, 5*time.Minute)
	}

	result, err = engine.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("second RunOnce() error: %v", err)
	}
	if len(todoProvider.calls) != 2 {
		t.Fatalf("todoProvider second call count = %d, want 2", len(todoProvider.calls))
	}
	if len(bearProvider.calls) != 2 {
		t.Fatalf("bearProvider second call count = %d, want 2", len(bearProvider.calls))
	}
	if result.Accounts[1].Reason != "interval" || !result.Accounts[1].Skipped {
		t.Fatalf("todo account result = %#v, want interval skip", result.Accounts[1])
	}
}

func TestIntervalFromAccountConfig(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    time.Duration
		wantErr bool
	}{
		{name: "empty", raw: "", want: 0},
		{name: "seconds", raw: `{"sync_interval_seconds":300}`, want: 5 * time.Minute},
		{name: "duration", raw: `{"sync_interval":"90s"}`, want: 90 * time.Second},
		{name: "invalid", raw: `{"sync_interval_seconds":"fast"}`, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := intervalFromAccountConfig(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("intervalFromAccountConfig(%q) error = nil, want error", tc.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("intervalFromAccountConfig(%q) error: %v", tc.raw, err)
			}
			if got != tc.want {
				t.Fatalf("intervalFromAccountConfig(%q) = %s, want %s", tc.raw, got, tc.want)
			}
		})
	}
}

func TestEngineRunNowIgnoresIntervals(t *testing.T) {
	source := fakeAccountSource{
		accounts: []store.ExternalAccount{
			{ID: 1, Provider: store.ExternalProviderTodoist, Label: "todo-a", Enabled: true},
		},
	}
	provider := &fakeProvider{name: store.ExternalProviderTodoist}
	engine := NewEngine(source, &fakeCleaner{}, fakeSink{}, Options{
		DefaultInterval: 10 * time.Minute,
	})
	now := time.Date(2026, time.March, 9, 12, 0, 0, 0, time.UTC)
	engine.now = func() time.Time { return now }
	engine.sleep = func(_ context.Context, d time.Duration) error {
		now = now.Add(d)
		return nil
	}
	engine.Register(provider)

	if _, err := engine.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error: %v", err)
	}
	if len(provider.calls) != 1 {
		t.Fatalf("provider call count after RunOnce = %d, want 1", len(provider.calls))
	}
	if _, err := engine.RunNow(context.Background()); err != nil {
		t.Fatalf("RunNow() error: %v", err)
	}
	if len(provider.calls) != 2 {
		t.Fatalf("provider call count after RunNow = %d, want 2", len(provider.calls))
	}
}

func TestEngineSkipsPollingWhenPushPolicyDisablesPoll(t *testing.T) {
	source := fakeAccountSource{
		accounts: []store.ExternalAccount{
			{ID: 1, Provider: store.ExternalProviderExchangeEWS, Label: "tugraz", Enabled: true},
		},
	}
	provider := &fakePolicyProvider{
		fakeProvider: fakeProvider{name: store.ExternalProviderExchangeEWS},
		policy: SyncPolicy{
			DisablePoll:      true,
			FallbackInterval: 30 * time.Minute,
		},
	}
	engine := NewEngine(source, &fakeCleaner{}, fakeSink{}, Options{
		DefaultInterval: 5 * time.Minute,
	})
	engine.Register(provider)

	result, err := engine.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce() error: %v", err)
	}
	if len(provider.calls) != 0 {
		t.Fatalf("provider calls = %d, want 0", len(provider.calls))
	}
	if len(result.Accounts) != 1 || !result.Accounts[0].Skipped || result.Accounts[0].Reason != "push" {
		t.Fatalf("result.Accounts = %#v, want push skip", result.Accounts)
	}
	if result.NextDelay != 30*time.Minute {
		t.Fatalf("result.NextDelay = %s, want 30m", result.NextDelay)
	}
}
