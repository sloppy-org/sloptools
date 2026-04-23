// Package groupware owns per-account provider construction and auth-session
// sharing across mail, calendar, contacts, and task features. Callers go
// through a single Registry instead of each feature package recreating auth
// pipelines on its own.
package groupware

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/sloppy-org/sloptools/internal/contacts"
	"github.com/sloppy-org/sloptools/internal/email"
	"github.com/sloppy-org/sloptools/internal/ews"
	"github.com/sloppy-org/sloptools/internal/googleauth"
	"github.com/sloppy-org/sloptools/internal/store"
)

// ErrUnsupported is returned when a provider cannot back a requested
// capability yet. Later tiers will replace these returns with real
// implementations.
var ErrUnsupported = errors.New("groupware: provider does not support this capability yet")

// Registry resolves provider instances for external accounts and caches the
// auth primitives (OAuth session, EWS client) so repeated lookups reuse one
// authenticated pipeline per account.
type Registry struct {
	store       *store.Store
	configDir   string
	googleCreds string

	mu                sync.Mutex
	googleSessions    map[int64]*googleauth.Session
	ewsClients        map[int64]*ews.Client
	mailProviders     map[int64]email.EmailProvider
	contactsProviders map[int64]contacts.Provider
}

// NewRegistry constructs a Registry backed by the given store. googleCreds
// points at either a Google OAuth credentials file or a directory that
// contains one; an empty value defers to googleauth.DefaultCredentialsPath.
func NewRegistry(st *store.Store, googleCreds string) *Registry {
	cleaned := strings.TrimSpace(googleCreds)
	configDir := googleauth.DefaultConfigDir()
	if info, err := os.Stat(cleaned); err == nil && info.IsDir() {
		configDir = cleaned
	}
	return &Registry{
		store:             st,
		configDir:         configDir,
		googleCreds:       cleaned,
		googleSessions:    make(map[int64]*googleauth.Session),
		ewsClients:        make(map[int64]*ews.Client),
		mailProviders:     make(map[int64]email.EmailProvider),
		contactsProviders: make(map[int64]contacts.Provider),
	}
}

// MailFor returns an email.EmailProvider for the given account, reusing a
// cached auth session on repeat calls so Gmail/Calendar/Contacts/Tasks share
// one OAuth pipeline per Google account and one EWS client per Exchange
// account.
func (r *Registry) MailFor(ctx context.Context, accountID int64) (email.EmailProvider, error) {
	if r == nil {
		return nil, errors.New("groupware: registry is nil")
	}
	if r.store == nil {
		return nil, errors.New("groupware: store is not configured")
	}
	account, err := r.store.GetExternalAccount(accountID)
	if err != nil {
		return nil, err
	}
	return r.mailFor(ctx, account)
}

func (r *Registry) mailFor(ctx context.Context, account store.ExternalAccount) (email.EmailProvider, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if cached, ok := r.mailProviders[account.ID]; ok {
		return cached, nil
	}
	cfg, err := decodeMailSyncAccountConfig(account)
	if err != nil {
		return nil, err
	}
	switch account.Provider {
	case store.ExternalProviderGmail:
		session, err := r.googleSessionLocked(account, cfg)
		if err != nil {
			return nil, err
		}
		provider := email.NewGmailFromSession(session)
		r.mailProviders[account.ID] = provider
		return provider, nil
	case store.ExternalProviderIMAP:
		if cfg.Host == "" {
			return nil, fmt.Errorf("imap host is required")
		}
		if cfg.Username == "" {
			return nil, fmt.Errorf("imap username is required")
		}
		password, _, err := r.store.ResolveExternalAccountPasswordForAccount(ctx, account)
		if err != nil {
			return nil, err
		}
		useTLS := cfg.TLS || cfg.Port == 993
		return email.NewIMAPClient(account.AccountName, cfg.Host, cfg.Port, cfg.Username, password, useTLS, cfg.StartTLS), nil
	case store.ExternalProviderExchange:
		exchangeCfg, err := decodeExchangeAccountConfig(account)
		if err != nil {
			return nil, err
		}
		return email.NewExchangeMailProvider(exchangeCfg)
	case store.ExternalProviderExchangeEWS:
		ewsCfg, err := decodeExchangeEWSAccountConfig(account)
		if err != nil {
			return nil, err
		}
		password, _, err := r.store.ResolveExternalAccountPasswordForAccount(ctx, account)
		if err != nil {
			return nil, err
		}
		ewsCfg.Password = password
		client, err := r.ewsClientLocked(account, ewsCfg)
		if err != nil {
			return nil, err
		}
		provider := email.NewExchangeEWSMailProviderFromClient(ewsCfg, client)
		r.mailProviders[account.ID] = provider
		return provider, nil
	default:
		return nil, fmt.Errorf("email provider %s is not supported", account.Provider)
	}
}

func (r *Registry) googleSessionLocked(account store.ExternalAccount, cfg mailSyncAccountConfig) (*googleauth.Session, error) {
	if existing, ok := r.googleSessions[account.ID]; ok {
		return existing, nil
	}
	session, err := googleauth.New(r.gmailCredentialsPath(cfg), r.gmailTokenPath(account, cfg), nil)
	if err != nil {
		return nil, err
	}
	r.googleSessions[account.ID] = session
	return session, nil
}

func (r *Registry) ewsClientLocked(account store.ExternalAccount, cfg email.ExchangeEWSConfig) (*ews.Client, error) {
	if existing, ok := r.ewsClients[account.ID]; ok {
		return existing, nil
	}
	client, err := ews.NewClient(ews.Config{
		Endpoint:      cfg.Endpoint,
		Username:      cfg.Username,
		Password:      cfg.Password,
		ServerVersion: cfg.ServerVersion,
		BatchSize:     cfg.BatchSize,
		InsecureTLS:   cfg.InsecureTLS,
	})
	if err != nil {
		return nil, err
	}
	r.ewsClients[account.ID] = client
	return client, nil
}

// GoogleSession exposes the cached OAuth session for an account. Returns nil
// when no MailFor call has populated the cache yet. Used by tests and by
// later-tier Google features that need to attach to the same token pipeline.
func (r *Registry) GoogleSession(accountID int64) *googleauth.Session {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.googleSessions[accountID]
}

// EWSClient exposes the cached EWS client for an account. Returns nil when
// no MailFor call has populated the cache yet.
func (r *Registry) EWSClient(accountID int64) *ews.Client {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ewsClients[accountID]
}

// ContactsFor returns a contacts.Provider for the given account, reusing the
// cached OAuth session so mail, calendar, contacts, and tasks share one
// token pipeline per Google account. EWS contacts lands in a follow-up and
// returns contacts.ErrUnsupported.
func (r *Registry) ContactsFor(ctx context.Context, accountID int64) (contacts.Provider, error) {
	if r == nil {
		return nil, errors.New("groupware: registry is nil")
	}
	if r.store == nil {
		return nil, errors.New("groupware: store is not configured")
	}
	account, err := r.store.GetExternalAccount(accountID)
	if err != nil {
		return nil, err
	}
	return r.contactsFor(ctx, account)
}

func (r *Registry) contactsFor(_ context.Context, account store.ExternalAccount) (contacts.Provider, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if cached, ok := r.contactsProviders[account.ID]; ok {
		return cached, nil
	}
	switch account.Provider {
	case store.ExternalProviderGmail, store.ExternalProviderGoogleCalendar:
		cfg, err := decodeMailSyncAccountConfig(account)
		if err != nil {
			return nil, err
		}
		session, err := r.googleSessionLocked(account, cfg)
		if err != nil {
			return nil, err
		}
		provider := contacts.NewGmailProvider(session)
		r.contactsProviders[account.ID] = provider
		return provider, nil
	case store.ExternalProviderExchangeEWS:
		return nil, fmt.Errorf("%s contacts: %w", account.Provider, contacts.ErrUnsupported)
	default:
		return nil, fmt.Errorf("contacts provider %s is not supported", account.Provider)
	}
}

// CalendarFor is a placeholder for later tiers.
func (r *Registry) CalendarFor(context.Context, int64) (any, error) { return nil, ErrUnsupported }

// TasksFor is a placeholder for later tiers.
func (r *Registry) TasksFor(context.Context, int64) (any, error) { return nil, ErrUnsupported }

// MailboxSettingsFor is a placeholder for later tiers.
func (r *Registry) MailboxSettingsFor(context.Context, int64) (any, error) {
	return nil, ErrUnsupported
}

type mailSyncAccountConfig struct {
	Host            string `json:"host"`
	Port            int    `json:"port"`
	Username        string `json:"username"`
	TLS             bool   `json:"tls"`
	StartTLS        bool   `json:"starttls"`
	FromAddress     string `json:"from_address"`
	TokenPath       string `json:"token_path"`
	TokenFile       string `json:"token_file"`
	CredentialsPath string `json:"credentials_path"`
	CredentialsFile string `json:"credentials_file"`
}

func decodeMailSyncAccountConfig(account store.ExternalAccount) (mailSyncAccountConfig, error) {
	var cfg mailSyncAccountConfig
	raw := strings.TrimSpace(account.ConfigJSON)
	if raw == "" || raw == "{}" {
		return cfg, nil
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return mailSyncAccountConfig{}, fmt.Errorf("decode %s mail config: %w", account.Provider, err)
	}
	cfg.Host = strings.TrimSpace(cfg.Host)
	cfg.Username = strings.TrimSpace(cfg.Username)
	cfg.FromAddress = strings.TrimSpace(cfg.FromAddress)
	cfg.TokenPath = strings.TrimSpace(cfg.TokenPath)
	cfg.TokenFile = strings.TrimSpace(cfg.TokenFile)
	cfg.CredentialsPath = strings.TrimSpace(cfg.CredentialsPath)
	cfg.CredentialsFile = strings.TrimSpace(cfg.CredentialsFile)
	return cfg, nil
}

func decodeExchangeAccountConfig(account store.ExternalAccount) (email.ExchangeConfig, error) {
	config := map[string]interface{}{}
	raw := strings.TrimSpace(account.ConfigJSON)
	if raw != "" && raw != "{}" {
		if err := json.Unmarshal([]byte(raw), &config); err != nil {
			return email.ExchangeConfig{}, fmt.Errorf("decode exchange account config: %w", err)
		}
	}
	return email.ExchangeConfigFromMap(account.AccountName, config)
}

func decodeExchangeEWSAccountConfig(account store.ExternalAccount) (email.ExchangeEWSConfig, error) {
	config := map[string]interface{}{}
	raw := strings.TrimSpace(account.ConfigJSON)
	if raw != "" && raw != "{}" {
		if err := json.Unmarshal([]byte(raw), &config); err != nil {
			return email.ExchangeEWSConfig{}, fmt.Errorf("decode exchange ews account config: %w", err)
		}
	}
	return email.ExchangeEWSConfigFromMap(account.AccountName, config)
}

func (r *Registry) gmailTokenPath(account store.ExternalAccount, cfg mailSyncAccountConfig) string {
	if path := emailConfigPath(r.configDir, cfg.TokenPath, ""); path != "" {
		return path
	}
	if strings.TrimSpace(cfg.TokenFile) != "" {
		return filepath.Join(r.configDir, "tokens", strings.TrimSpace(cfg.TokenFile))
	}
	return store.ExternalAccountTokenPath(r.configDir, account.Provider, account.AccountName)
}

func (r *Registry) gmailCredentialsPath(cfg mailSyncAccountConfig) string {
	if path := emailConfigPath(r.configDir, cfg.CredentialsPath, cfg.CredentialsFile); path != "" {
		return path
	}
	if r.googleCreds != "" {
		if info, err := os.Stat(r.googleCreds); err == nil && !info.IsDir() {
			return r.googleCreds
		}
	}
	return filepath.Join(r.configDir, "gmail_credentials.json")
}

func emailConfigPath(configDir, explicitPath, fileName string) string {
	switch {
	case strings.TrimSpace(explicitPath) != "":
		clean := filepath.Clean(strings.TrimSpace(explicitPath))
		if filepath.IsAbs(clean) {
			return clean
		}
		return filepath.Join(configDir, clean)
	case strings.TrimSpace(fileName) != "":
		return filepath.Join(configDir, strings.TrimSpace(fileName))
	default:
		return ""
	}
}
