package email

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// providerCache holds warmed-up providers for reuse across requests.
var (
	providerCache   = make(map[string]EmailProvider)
	providerCacheMu sync.RWMutex
)

// ProviderConfig represents a single provider configuration.
type ProviderConfig struct {
	Type     string `json:"type"`               // "gmail" or "imap"
	Host     string `json:"host,omitempty"`     // IMAP server host
	Port     int    `json:"port,omitempty"`     // IMAP server port (default: 993)
	Username string `json:"username,omitempty"` // IMAP username
	TLS      bool   `json:"tls,omitempty"`      // Use implicit TLS (port 993)
	StartTLS bool   `json:"starttls,omitempty"` // Use STARTTLS (port 143)
}

// ProvidersConfig represents the multi-provider configuration file.
type ProvidersConfig struct {
	DefaultProvider string                    `json:"default_provider"`
	Providers       map[string]ProviderConfig `json:"providers"`
}

func providersConfigFile() string {
	return filepath.Join(configDir(), "email_providers.json")
}

// LoadProvidersConfig loads the providers configuration from disk.
func LoadProvidersConfig() (*ProvidersConfig, error) {
	data, err := os.ReadFile(providersConfigFile())
	if err != nil {
		if os.IsNotExist(err) {
			return &ProvidersConfig{
				DefaultProvider: "gmail",
				Providers:       map[string]ProviderConfig{"gmail": {Type: "gmail"}},
			}, nil
		}
		return nil, fmt.Errorf("failed to read providers config: %w", err)
	}

	var config ProvidersConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse providers config: %w", err)
	}

	return &config, nil
}

// SaveProvidersConfig saves the providers configuration to disk.
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

// GetProvider returns a provider by name. If name is empty, returns the default.
// The caller is responsible for closing the provider when done.
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

// GetCachedProvider returns a cached provider by name.
// Unlike GetProvider, the returned provider should NOT be closed by the caller.
// Use this for long-lived server mode where connections should be kept alive.
func GetCachedProvider(name string) (EmailProvider, error) {
	config, err := LoadProvidersConfig()
	if err != nil {
		return nil, err
	}

	if name == "" {
		name = config.DefaultProvider
	}

	// Check cache first
	providerCacheMu.RLock()
	if provider, ok := providerCache[name]; ok {
		providerCacheMu.RUnlock()
		return provider, nil
	}
	providerCacheMu.RUnlock()

	// Create new provider
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

	// Cache it
	providerCacheMu.Lock()
	providerCache[name] = provider
	providerCacheMu.Unlock()

	return provider, nil
}

// WarmUpProviders pre-creates and warms up all configured IMAP providers.
// Call this at server startup to avoid connection latency on first request.
func WarmUpProviders(ctx context.Context) error {
	config, err := LoadProvidersConfig()
	if err != nil {
		return err
	}

	var wg sync.WaitGroup
	errors := make(chan error, len(config.Providers))

	for name, provConfig := range config.Providers {
		if provConfig.Type != "imap" {
			continue // Gmail uses OAuth, don't warm up
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

	// Collect errors
	var errMsgs []string
	for err := range errors {
		errMsgs = append(errMsgs, err.Error())
	}

	if len(errMsgs) > 0 {
		return fmt.Errorf("warmup errors: %v", errMsgs)
	}

	return nil
}

// CloseAllProviders closes all cached providers. Call on shutdown.
func CloseAllProviders() {
	providerCacheMu.Lock()
	defer providerCacheMu.Unlock()

	for name, provider := range providerCache {
		provider.Close()
		delete(providerCache, name)
	}
}

// AddIMAPProvider adds or updates an IMAP provider in the configuration.
func AddIMAPProvider(name, host string, port int, username string, tls, startTLS bool) error {
	config, err := LoadProvidersConfig()
	if err != nil {
		return err
	}

	if config.Providers == nil {
		config.Providers = make(map[string]ProviderConfig)
	}

	config.Providers[name] = ProviderConfig{
		Type:     "imap",
		Host:     host,
		Port:     port,
		Username: username,
		TLS:      tls,
		StartTLS: startTLS,
	}

	return SaveProvidersConfig(config)
}

// SetDefaultProvider sets the default provider.
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

// ListProviders returns a list of configured provider names and types.
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
		}{
			Name:      name,
			Type:      prov.Type,
			IsDefault: name == config.DefaultProvider,
		})
	}

	return providers, nil
}

// RemoveProvider removes a provider from the configuration.
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
