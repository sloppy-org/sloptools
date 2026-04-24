package email

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	providerCache   = make(map[string]EmailProvider)
	providerCacheMu sync.RWMutex
)

type ProviderConfig struct {
	Type     string `json:"type"`
	Host     string `json:"host,omitempty"`
	Port     int    `json:"port,omitempty"`
	Username string `json:"username,omitempty"`
	TLS      bool   `json:"tls,omitempty"`
	StartTLS bool   `json:"starttls,omitempty"`
} // ProviderConfig represents a single provider configuration.

type ProvidersConfig struct {
	DefaultProvider string                    `json:"default_provider"`
	Providers       map[string]ProviderConfig `json:"providers"`
}

func providersConfigFile() string {
	return filepath.Join(configDir(), "email_providers.json")
}

func LoadProvidersConfig() (*ProvidersConfig, error) {
	data, err := os.ReadFile(providersConfigFile())
	if err != nil {
		if os.IsNotExist(err) {
			return &ProvidersConfig{DefaultProvider: "gmail", Providers: map[string]ProviderConfig{"gmail": {Type: "gmail"}}}, nil
		}
		return nil, fmt.Errorf("failed to read providers config: %w", err)
	}
	var config ProvidersConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse providers config: %w", err)
	}
	return &config, nil
}

func SaveProvidersConfig(config *ProvidersConfig) error {
	if err := os.MkdirAll(configDir(), 0755); err != nil {
		return fmt.Errorf("failed to create config dir: %w", err)
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal providers config: %w", err)
	}
	if err := os.WriteFile(providersConfigFile(), data, 0600); err != nil {
		return fmt.Errorf("failed to write providers config: %w", err)
	}
	return nil
}

func GetProvider(name string) (EmailProvider, error) {
	config, err := LoadProvidersConfig()
	if err != nil {
		return nil, err
	}
	if name == "" {
		name = config.DefaultProvider
	}
	provConfig, ok := config.Providers[name]
	if !ok {
		return nil, fmt.Errorf("provider %q not found", name)
	}
	switch provConfig.Type {
	case "gmail":
		return NewGmail()
	case "imap":
		return NewIMAPFromConfig(name, provConfig)
	default:
		return nil, fmt.Errorf("unknown provider type: %s", provConfig.Type)
	}
}

func GetCachedProvider(name string) (EmailProvider, error) {
	config, err := LoadProvidersConfig()
	if err != nil {
		return nil, err
	}
	if name == "" {
		name = config.DefaultProvider
	}
	providerCacheMu.RLock()
	if provider, ok := providerCache[name]; ok {
		providerCacheMu.RUnlock()
		return provider, nil
	}
	providerCacheMu.RUnlock()
	provConfig, ok := config.Providers[name]
	if !ok {
		return nil, fmt.Errorf("provider %q not found", name)
	}
	var provider EmailProvider
	switch provConfig.Type {
	case "gmail":
		provider, err = NewGmail()
	case "imap":
		provider, err = NewIMAPFromConfig(name, provConfig)
	default:
		return nil, fmt.Errorf("unknown provider type: %s", provConfig.Type)
	}
	if err != nil {
		return nil, err
	}
	providerCacheMu.Lock()
	providerCache[name] = provider
	providerCacheMu.Unlock()
	return provider, nil
}

func WarmUpProviders(ctx context.Context) error {
	config, err := LoadProvidersConfig()
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	errors := make(chan error, len(config.Providers))
	for name, provConfig := range config.Providers {
		if provConfig.Type != "imap" {
			continue
		}
		wg.Add(1)
		go func(name string, provConfig ProviderConfig) {
			defer wg.Done()
			client, err := NewIMAPFromConfig(name, provConfig)
			if err != nil {
				errors <- fmt.Errorf("failed to create %s: %w", name, err)
				return
			}
			if err := client.WarmUp(ctx); err != nil {
				client.Close()
				errors <- fmt.Errorf("failed to warm up %s: %w", name, err)
				return
			}
			providerCacheMu.Lock()
			providerCache[name] = client
			providerCacheMu.Unlock()
		}(name, provConfig)
	}
	wg.Wait()
	close(errors)
	var errMsgs []string
	for err := range // Collect errors
	errors {
		errMsgs = append(errMsgs, err.Error())
	}
	if len(errMsgs) > 0 {
		return fmt.Errorf("warmup errors: %v", errMsgs)
	}
	return nil
}

func CloseAllProviders() {
	providerCacheMu.Lock()
	defer providerCacheMu.Unlock()
	for name, provider := range providerCache {
		provider.Close()
		delete(providerCache, name)
	}
}

func AddIMAPProvider(name, host string, port int, username string, tls, startTLS bool) error {
	config, err := LoadProvidersConfig()
	if err != nil {
		return err
	}
	if config.Providers == nil {
		config.Providers = make(map[string]ProviderConfig)
	}
	config.Providers[name] = ProviderConfig{Type: "imap", Host: host, Port: port, Username: username, TLS: tls, StartTLS: startTLS}
	return SaveProvidersConfig(config)
}

func SetDefaultProvider(name string) error {
	config, err := LoadProvidersConfig()
	if err != nil {
		return err
	}
	if _, ok := config.Providers[name]; !ok {
		return fmt.Errorf("provider %q not found", name)
	}
	config.DefaultProvider = name
	return SaveProvidersConfig(config)
}

func ListProviders() ([]struct {
	Name      string
	Type      string
	IsDefault bool
}, error) {
	config, err := LoadProvidersConfig()
	if err != nil {
		return nil, err
	}
	var providers []struct {
		Name      string
		Type      string
		IsDefault bool
	}
	for name, prov := range config.Providers {
		providers = append(providers, struct {
			Name      string
			Type      string
			IsDefault bool
		}{Name: name, Type: prov.Type, IsDefault: name == config.DefaultProvider})
	}
	return providers, nil
}

func RemoveProvider(name string) error {
	config, err := LoadProvidersConfig()
	if err != nil {
		return err
	}
	if _, ok := config.Providers[name]; !ok {
		return fmt.Errorf("provider %q not found", name)
	}
	if name == config.DefaultProvider {
		return fmt.Errorf("cannot remove default provider %q", name)
	}
	delete(config.Providers, name)
	return SaveProvidersConfig(config)
}

type DraftAttachment struct {
	Filename    string
	ContentType string
	Content     []byte
}

type DraftInput struct {
	From        string
	To          []string
	Cc          []string
	Bcc         []string
	Subject     string
	Body        string
	ThreadID    string
	InReplyTo   string
	References  []string
	ReplyToID   string
	ReplyToAddr string
	Attachments []DraftAttachment
}

type Draft struct {
	ID       string
	ThreadID string
}

type DraftProvider interface {
	CreateDraft(context.Context, DraftInput) (Draft, error)
	CreateReplyDraft(context.Context, string, DraftInput) (Draft, error)
	UpdateDraft(context.Context, string, DraftInput) (Draft, error)
	SendDraft(context.Context, string, DraftInput) error
}

type ExistingDraftSender interface {
	SendExistingDraft(ctx context.Context, draftID string) error
} // ExistingDraftSender sends a draft that already lives in the mailbox as-is,

type SMTPConfig struct {
	Host      string
	Port      int
	Username  string
	Password  string
	TLS       bool
	StartTLS  bool
	From      string
	FromName  string
	DraftsBox string
}

type SMTPSender func(context.Context, SMTPConfig, string, []string, []byte) error

func normalizeDraftAddress(raw string) (string, error) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return "", nil
	}
	addr, err := mail.ParseAddress(clean)
	if err != nil {
		return "", fmt.Errorf("invalid address %q", clean)
	}
	return strings.ToLower(strings.TrimSpace(addr.Address)), nil
}

func normalizeDraftAddresses(values []string) ([]string, error) {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, raw := range values {
		clean := strings.TrimSpace(raw)
		if clean == "" {
			continue
		}
		parsed, err := mail.ParseAddressList(clean)
		if err != nil {
			single, singleErr := normalizeDraftAddress(clean)
			if singleErr != nil {
				return nil, fmt.Errorf("invalid address %q", clean)
			}
			if single != "" {
				if _, ok := seen[single]; !ok {
					seen[single] = struct{}{}
					out = append(out, single)
				}
			}
			continue
		}
		for _, addr := range parsed {
			lower := strings.ToLower(strings.TrimSpace(addr.Address))
			if lower == "" {
				continue
			}
			if _, ok := seen[lower]; ok {
				continue
			}
			seen[lower] = struct{}{}
			out = append(out, lower)
		}
	}
	return out, nil
}

func normalizeDraftInput(input DraftInput, requireRecipients bool) (DraftInput, error) {
	to, err := normalizeDraftAddresses(input.To)
	if err != nil {
		return DraftInput{}, err
	}
	cc, err := normalizeDraftAddresses(input.Cc)
	if err != nil {
		return DraftInput{}, err
	}
	bcc, err := normalizeDraftAddresses(input.Bcc)
	if err != nil {
		return DraftInput{}, err
	}
	from, err := normalizeDraftAddress(input.From)
	if err != nil {
		return DraftInput{}, err
	}
	replyToAddr, err := normalizeDraftAddress(input.ReplyToAddr)
	if err != nil {
		return DraftInput{}, err
	}
	subject := strings.TrimSpace(input.Subject)
	body := strings.ReplaceAll(strings.TrimSpace(input.Body), "\r\n", "\n")
	if requireRecipients && len(to) == 0 && len(cc) == 0 && len(bcc) == 0 {
		return DraftInput{}, errors.New("at least one recipient is required")
	}
	attachments, err := normalizeDraftAttachments(input.Attachments)
	if err != nil {
		return DraftInput{}, err
	}
	return DraftInput{From: from, To: to, Cc: cc, Bcc: bcc, Subject: subject, Body: body, ThreadID: strings.TrimSpace(input.ThreadID), InReplyTo: strings.TrimSpace(input.InReplyTo), References: trimStringList(input.References), ReplyToID: strings.TrimSpace(input.ReplyToID), ReplyToAddr: replyToAddr, Attachments: attachments}, nil
}

func normalizeDraftAttachments(values []DraftAttachment) ([]DraftAttachment, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]DraftAttachment, 0, len(values))
	for i, raw := range values {
		filename := strings.TrimSpace(raw.Filename)
		if filename == "" {
			return nil, fmt.Errorf("attachment %d: filename is required", i)
		}
		filename = filepath.Base(filename)
		if len(raw.Content) == 0 {
			return nil, fmt.Errorf("attachment %q: content is empty", filename)
		}
		ct := strings.TrimSpace(raw.ContentType)
		if ct == "" {
			ct = "application/octet-stream"
		}
		out = append(out, DraftAttachment{Filename: filename, ContentType: ct, Content: append([]byte(nil), raw.Content...)})
	}
	return out, nil
}

func NormalizeDraftInput(input DraftInput) (DraftInput, error) {
	return normalizeDraftInput(input, false)
}

func ExportRFC822ForTest(input DraftInput) ([]byte, error) {
	return buildRFC822Message(input)
}

func NormalizeDraftSendInput(input DraftInput) (DraftInput, error) {
	return normalizeDraftInput(input, true)
}

func trimStringList(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		clean := strings.TrimSpace(value)
		if clean != "" {
			out = append(out, clean)
		}
	}
	return out
}

func ensureReplySubject(subject string) string {
	return EnsureReplySubject(subject)
}

func EnsureReplySubject(subject string) string {
	clean := strings.TrimSpace(subject)
	if clean == "" {
		return "Re:"
	}
	if strings.HasPrefix(strings.ToLower(clean), "re:") {
		return clean
	}
	return "Re: " + clean
}

func formatDraftHeaderAddresses(values []string) string {
	return strings.Join(trimStringList(values), ", ")
}
