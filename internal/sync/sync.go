package sync

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	stdsync "sync"
	"time"

	"github.com/krystophny/sloppy/internal/store"
)

type Provider interface {
	Name() string
	Sync(ctx context.Context, account store.ExternalAccount, sink Sink) error
}

type SyncPolicy struct {
	DisablePoll      bool
	FallbackInterval time.Duration
}

type SyncPolicyProvider interface {
	SyncPolicy(ctx context.Context, account store.ExternalAccount) (SyncPolicy, error)
}

type Sink interface {
	UpsertItem(ctx context.Context, item store.Item, binding store.ExternalBinding) (store.Item, error)
	UpsertArtifact(ctx context.Context, artifact store.Artifact, binding store.ExternalBinding) (store.Artifact, error)
}

type AccountSource interface {
	ListExternalAccounts(sphere string) ([]store.ExternalAccount, error)
}

type BindingCleaner interface {
	ListStaleBindings(provider string, olderThan time.Time) ([]store.ExternalBinding, error)
	DeleteBinding(id int64) error
}

type Options struct {
	DefaultInterval time.Duration
	ProviderLimits  map[string]time.Duration
	StaleAfter      time.Duration
}

type AccountResult struct {
	AccountID   int64
	Provider    string
	AccountName string
	Skipped     bool
	Reason      string
	Err         error
}

type RunResult struct {
	Accounts  []AccountResult
	NextDelay time.Duration
}

type Engine struct {
	accounts AccountSource
	cleaner  BindingCleaner
	sink     Sink

	defaultInterval time.Duration
	staleAfter      time.Duration

	mu              stdsync.Mutex
	lastAccountRun  map[int64]time.Time
	lastProviderRun map[string]time.Time
	providers       map[string]Provider
	providerLimits  map[string]time.Duration

	now   func() time.Time
	sleep func(context.Context, time.Duration) error
}

func NewEngine(accounts AccountSource, cleaner BindingCleaner, sink Sink, opts Options) *Engine {
	defaultInterval := opts.DefaultInterval
	if defaultInterval <= 0 {
		defaultInterval = 5 * time.Minute
	}
	engine := &Engine{
		accounts:        accounts,
		cleaner:         cleaner,
		sink:            sink,
		defaultInterval: defaultInterval,
		staleAfter:      opts.StaleAfter,
		lastAccountRun:  make(map[int64]time.Time),
		lastProviderRun: make(map[string]time.Time),
		providers:       make(map[string]Provider),
		providerLimits:  make(map[string]time.Duration),
		now:             time.Now,
		sleep:           sleepContext,
	}
	for provider, limit := range opts.ProviderLimits {
		if limit > 0 {
			engine.providerLimits[provider] = limit
		}
	}
	return engine
}

func (e *Engine) Register(provider Provider) {
	if provider == nil {
		return
	}
	name := provider.Name()
	if name == "" {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.providers[name] = provider
}

func (e *Engine) RunOnce(ctx context.Context) (RunResult, error) {
	return e.run(ctx, false)
}

func (e *Engine) RunNow(ctx context.Context) (RunResult, error) {
	return e.run(ctx, true)
}

func (e *Engine) run(ctx context.Context, force bool) (RunResult, error) {
	accounts, err := e.accounts.ListExternalAccounts("")
	if err != nil {
		return RunResult{}, err
	}
	sort.Slice(accounts, func(i, j int) bool {
		if accounts[i].Provider == accounts[j].Provider {
			if accounts[i].AccountName == accounts[j].AccountName {
				return accounts[i].ID < accounts[j].ID
			}
			return accounts[i].AccountName < accounts[j].AccountName
		}
		return accounts[i].Provider < accounts[j].Provider
	})

	result := RunResult{
		Accounts: make([]AccountResult, 0, len(accounts)),
	}
	now := e.now()
	nextDelay := time.Duration(-1)

	for _, account := range accounts {
		if !account.Enabled {
			continue
		}
		interval := e.accountInterval(account)
		provider := e.providerFor(account.Provider)
		if provider == nil {
			result.Accounts = append(result.Accounts, AccountResult{
				AccountID:   account.ID,
				Provider:    account.Provider,
				AccountName: account.AccountName,
				Skipped:     true,
				Reason:      "provider_unregistered",
			})
			nextDelay = minPositiveDuration(nextDelay, interval)
			continue
		}
		if policyProvider, ok := provider.(SyncPolicyProvider); ok {
			policy, policyErr := policyProvider.SyncPolicy(ctx, account)
			if policyErr != nil {
				result.Accounts = append(result.Accounts, AccountResult{
					AccountID:   account.ID,
					Provider:    account.Provider,
					AccountName: account.AccountName,
					Err:         policyErr,
				})
				nextDelay = minPositiveDuration(nextDelay, interval)
				continue
			}
			if policy.FallbackInterval > 0 {
				interval = policy.FallbackInterval
			}
			if policy.DisablePoll && !force {
				result.Accounts = append(result.Accounts, AccountResult{
					AccountID:   account.ID,
					Provider:    account.Provider,
					AccountName: account.AccountName,
					Skipped:     true,
					Reason:      "push",
				})
				if interval > 0 {
					nextDelay = minPositiveDuration(nextDelay, interval)
				}
				continue
			}
		}
		lastRun, due := e.accountDue(account.ID, interval, now)
		if !force && !due {
			remaining := interval - now.Sub(lastRun)
			if remaining < 0 {
				remaining = 0
			}
			nextDelay = minPositiveDuration(nextDelay, remaining)
			result.Accounts = append(result.Accounts, AccountResult{
				AccountID:   account.ID,
				Provider:    account.Provider,
				AccountName: account.AccountName,
				Skipped:     true,
				Reason:      "interval",
			})
			continue
		}

		if err := e.waitForProvider(ctx, account.Provider); err != nil {
			return result, err
		}
		runStarted := e.now()
		runErr := provider.Sync(ctx, account, e.sink)
		accountResult := AccountResult{
			AccountID:   account.ID,
			Provider:    account.Provider,
			AccountName: account.AccountName,
			Err:         runErr,
		}
		result.Accounts = append(result.Accounts, accountResult)
		if runErr != nil {
			nextDelay = minPositiveDuration(nextDelay, interval)
			continue
		}
		e.markAccountRun(account.ID, runStarted)
		nextDelay = minPositiveDuration(nextDelay, interval)
		if err := e.cleanupStaleBindings(account); err != nil {
			result.Accounts[len(result.Accounts)-1].Err = err
		}
	}

	if nextDelay < 0 {
		nextDelay = e.defaultInterval
	}
	result.NextDelay = nextDelay
	return result, nil
}

func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (e *Engine) providerFor(name string) Provider {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.providers[name]
}

func (e *Engine) accountDue(accountID int64, interval time.Duration, now time.Time) (time.Time, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	lastRun, ok := e.lastAccountRun[accountID]
	if !ok {
		return time.Time{}, true
	}
	return lastRun, now.Sub(lastRun) >= interval
}

func (e *Engine) markAccountRun(accountID int64, at time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.lastAccountRun[accountID] = at
}

func (e *Engine) waitForProvider(ctx context.Context, provider string) error {
	e.mu.Lock()
	limit := e.providerLimits[provider]
	lastRun := e.lastProviderRun[provider]
	now := e.now()
	wait := time.Duration(0)
	if limit > 0 && !lastRun.IsZero() {
		wait = limit - now.Sub(lastRun)
		if wait < 0 {
			wait = 0
		}
	}
	e.mu.Unlock()

	if err := e.sleep(ctx, wait); err != nil {
		return err
	}

	e.mu.Lock()
	e.lastProviderRun[provider] = e.now()
	e.mu.Unlock()
	return nil
}

func (e *Engine) cleanupStaleBindings(account store.ExternalAccount) error {
	if e.cleaner == nil || e.staleAfter <= 0 {
		return nil
	}
	olderThan := e.now().Add(-e.staleAfter)
	bindings, err := e.cleaner.ListStaleBindings(account.Provider, olderThan)
	if err != nil {
		return err
	}
	for _, binding := range bindings {
		if binding.AccountID != account.ID {
			continue
		}
		if err := e.cleaner.DeleteBinding(binding.ID); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) accountInterval(account store.ExternalAccount) time.Duration {
	interval, err := intervalFromAccountConfig(account.ConfigJSON)
	if err == nil && interval > 0 {
		return interval
	}
	return e.defaultInterval
}

func intervalFromAccountConfig(raw string) (time.Duration, error) {
	if raw == "" {
		return 0, nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return 0, err
	}
	if value, ok := payload["sync_interval_seconds"]; ok {
		seconds, ok := numberSeconds(value)
		if !ok || seconds <= 0 {
			return 0, errors.New("sync_interval_seconds must be positive")
		}
		return time.Duration(seconds) * time.Second, nil
	}
	if value, ok := payload["sync_interval"]; ok {
		text, ok := value.(string)
		if !ok {
			return 0, errors.New("sync_interval must be a duration string")
		}
		return time.ParseDuration(text)
	}
	return 0, nil
}

func numberSeconds(value any) (int64, bool) {
	switch typed := value.(type) {
	case float64:
		return int64(typed), typed > 0 && float64(int64(typed)) == typed
	case int:
		return int64(typed), typed > 0
	case int64:
		return typed, typed > 0
	case json.Number:
		v, err := typed.Int64()
		return v, err == nil && v > 0
	default:
		return 0, false
	}
}

func minPositiveDuration(current, candidate time.Duration) time.Duration {
	if candidate < 0 {
		return current
	}
	if current < 0 || candidate < current {
		return candidate
	}
	return current
}
