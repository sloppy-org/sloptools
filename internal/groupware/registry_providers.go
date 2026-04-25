package groupware

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sloppy-org/sloptools/internal/calendar"
	"github.com/sloppy-org/sloptools/internal/contacts"
	"github.com/sloppy-org/sloptools/internal/email"
	"github.com/sloppy-org/sloptools/internal/mailboxsettings"
	"github.com/sloppy-org/sloptools/internal/store"
	"github.com/sloppy-org/sloptools/internal/tasks"
)

// ContactsFor returns a contacts.Provider for the given account, reusing the
// cached OAuth session for Google accounts and the cached ews.Client for
// Exchange/EWS accounts so mail, calendar, contacts, and tasks share a
// single auth pipeline per external account.
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

func (r *Registry) contactsFor(ctx context.Context, account store.ExternalAccount) (contacts.Provider, error) {
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
		provider := contacts.NewEWSProvider(client, "")
		r.contactsProviders[account.ID] = provider
		return provider, nil
	default:
		return nil, fmt.Errorf("contacts provider %s is not supported", account.Provider)
	}
}

// CalendarFor returns a calendar.Provider for the given account, reusing the
// cached OAuth session so mail and calendar share one token pipeline per
// Google account. EWS accounts get an *calendar.EWSProvider backed by the
// shared ews.Client.
func (r *Registry) CalendarFor(ctx context.Context, accountID int64) (calendar.Provider, error) {
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
	return r.calendarFor(ctx, account)
}

func (r *Registry) calendarFor(ctx context.Context, account store.ExternalAccount) (calendar.Provider, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if cached, ok := r.calendarProviders[account.ID]; ok {
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
		provider := calendar.NewGoogleProvider(session)
		r.calendarProviders[account.ID] = provider
		return provider, nil
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
		provider := calendar.NewEWSProvider(client)
		r.calendarProviders[account.ID] = provider
		return provider, nil
	default:
		return nil, fmt.Errorf("calendar provider %s is not supported", account.Provider)
	}
}

// TasksFor returns a tasks.Provider for the given account, reusing the cached
// OAuth session so mail, calendar, and tasks share one token pipeline per
// Google account. EWS accounts get a *tasks.EWSProvider backed by the shared
// ews.Client.
func (r *Registry) TasksFor(ctx context.Context, accountID int64) (tasks.Provider, error) {
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
	return r.tasksFor(ctx, account)
}

func (r *Registry) tasksFor(ctx context.Context, account store.ExternalAccount) (tasks.Provider, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if cached, ok := r.tasksProviders[account.ID]; ok {
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
		provider := tasks.NewGoogleProvider(session)
		r.tasksProviders[account.ID] = provider
		return provider, nil
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
		provider := tasks.NewEWSProvider(client, "")
		r.tasksProviders[account.ID] = provider
		return provider, nil
	default:
		return nil, fmt.Errorf("tasks provider %s is not supported", account.Provider)
	}
}

// MailboxSettingsFor returns a mailboxsettings.OOFProvider for the given
// account, reusing cached auth primitives so Gmail and EWS share one
// pipeline across the feature providers. EWS is wired through but its SOAP
// methods return ErrUnsupported until the OOF wrappers land.
func (r *Registry) MailboxSettingsFor(ctx context.Context, accountID int64) (mailboxsettings.OOFProvider, error) {
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
	return r.mailboxSettingsFor(ctx, account)
}

func (r *Registry) mailboxSettingsFor(ctx context.Context, account store.ExternalAccount) (mailboxsettings.OOFProvider, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if cached, ok := r.mailboxSettingsProviders[account.ID]; ok {
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
		provider := mailboxsettings.NewGmailProvider(session)
		r.mailboxSettingsProviders[account.ID] = provider
		return provider, nil
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
		provider := mailboxsettings.NewEWSProvider(client, ewsCfg.Username)
		r.mailboxSettingsProviders[account.ID] = provider
		return provider, nil
	default:
		return nil, fmt.Errorf("mailbox settings provider %s is not supported", account.Provider)
	}
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
