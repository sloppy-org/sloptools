package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const (
	externalAccountCredentialSourceEnv       = "env"
	externalAccountCredentialSourceBitwarden = "bitwarden"
)

var errExternalAccountPasswordUnavailable = errors.New("external account password is not configured")

type externalAccountCommandRunner func(context.Context, string, ...string) (string, error)

type cachedExternalAccountCredential struct {
	value  string
	source string
}

func runExternalAccountCommand(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			if stderr := strings.TrimSpace(string(exitErr.Stderr)); stderr != "" {
				return "", errors.New(stderr)
			}
		}
		return "", err
	}
	return string(output), nil
}

func decodeExternalAccountConfigJSON(raw string) (map[string]any, error) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return map[string]any{}, nil
	}
	var config map[string]any
	if err := json.Unmarshal([]byte(clean), &config); err != nil {
		return nil, fmt.Errorf("decode external account config: %w", err)
	}
	if config == nil {
		config = map[string]any{}
	}
	return config, nil
}

func externalAccountCredentialRef(config map[string]any) string {
	raw, _ := config["credential_ref"].(string)
	return strings.TrimSpace(raw)
}

func externalAccountLegacyEnvVar(config map[string]any) string {
	raw, _ := config["legacy_helpy_env_var"].(string)
	return strings.TrimSpace(raw)
}

func bitwardenItemNameFromCredentialRef(ref string) (string, error) {
	clean := strings.TrimSpace(ref)
	if clean == "" {
		return "", errors.New("credential_ref is required")
	}
	if !strings.HasPrefix(strings.ToLower(clean), "bw://") {
		return "", fmt.Errorf("unsupported credential_ref %q", clean)
	}
	itemName := strings.TrimLeft(clean[len("bw://"):], "/")
	itemName = strings.TrimSpace(itemName)
	if itemName == "" {
		return "", errors.New("bitwarden credential_ref must include an item name")
	}
	return itemName, nil
}

func trimSecretOutput(raw string) string {
	return strings.TrimRight(raw, "\r\n")
}

func externalAccountCredentialCacheKey(account ExternalAccount, credentialRef string) string {
	return strings.Join([]string{
		strings.TrimSpace(account.Provider),
		strings.TrimSpace(account.AccountName),
		strings.TrimSpace(credentialRef),
	}, "\x00")
}

func (s *Store) cachedExternalAccountPassword(key string) (cachedExternalAccountCredential, bool) {
	s.externalAccountCredentialMu.Lock()
	defer s.externalAccountCredentialMu.Unlock()
	entry, ok := s.externalAccountCredentialCache[key]
	return entry, ok
}

func (s *Store) cacheExternalAccountPassword(key, source, value string) {
	if key == "" || value == "" {
		return
	}
	s.externalAccountCredentialMu.Lock()
	defer s.externalAccountCredentialMu.Unlock()
	if s.externalAccountCredentialCache == nil {
		s.externalAccountCredentialCache = map[string]cachedExternalAccountCredential{}
	}
	s.externalAccountCredentialCache[key] = cachedExternalAccountCredential{
		value:  value,
		source: source,
	}
}

func (s *Store) lookupExternalAccountEnv(key string) (string, bool) {
	if s.externalAccountLookupEnv != nil {
		return s.externalAccountLookupEnv(key)
	}
	return os.LookupEnv(key)
}

func (s *Store) runExternalAccountCredentialCommand(ctx context.Context, name string, args ...string) (string, error) {
	if s.externalAccountCommandRunner != nil {
		return s.externalAccountCommandRunner(ctx, name, args...)
	}
	return runExternalAccountCommand(ctx, name, args...)
}

func (s *Store) resolveBitwardenPassword(ctx context.Context, credentialRef string) (string, error) {
	itemName, err := bitwardenItemNameFromCredentialRef(credentialRef)
	if err != nil {
		return "", err
	}
	output, err := s.runExternalAccountCredentialCommand(ctx, "bw", "get", "password", itemName)
	if err != nil {
		return "", fmt.Errorf("resolve bitwarden credential %q: %w", itemName, err)
	}
	password := trimSecretOutput(output)
	if password == "" {
		return "", fmt.Errorf("resolve bitwarden credential %q: empty password", itemName)
	}
	return password, nil
}

func (s *Store) ResolveExternalAccountPassword(ctx context.Context, accountID int64) (string, string, error) {
	account, err := s.GetExternalAccount(accountID)
	if err != nil {
		return "", "", err
	}
	return s.ResolveExternalAccountPasswordForAccount(ctx, account)
}

func (s *Store) ResolveExternalAccountPasswordForAccount(ctx context.Context, account ExternalAccount) (string, string, error) {
	envVar := ExternalAccountPasswordEnvVar(account.Provider, account.AccountName)
	config, err := decodeExternalAccountConfigJSON(account.ConfigJSON)
	if err != nil {
		return "", "", err
	}
	credentialRef := externalAccountCredentialRef(config)
	cacheKey := externalAccountCredentialCacheKey(account, credentialRef)

	if value, ok := s.lookupExternalAccountEnv(envVar); ok && value != "" {
		s.cacheExternalAccountPassword(cacheKey, externalAccountCredentialSourceEnv, value)
		return value, externalAccountCredentialSourceEnv, nil
	}
	if legacyEnvVar := externalAccountLegacyEnvVar(config); legacyEnvVar != "" {
		if value, ok := s.lookupExternalAccountEnv(legacyEnvVar); ok && value != "" {
			s.cacheExternalAccountPassword(cacheKey, externalAccountCredentialSourceEnv, value)
			return value, externalAccountCredentialSourceEnv, nil
		}
	}
	if cached, ok := s.cachedExternalAccountPassword(cacheKey); ok {
		return cached.value, cached.source, nil
	}
	if credentialRef == "" {
		return "", "", errExternalAccountPasswordUnavailable
	}
	password, err := s.resolveBitwardenPassword(ctx, credentialRef)
	if err != nil {
		return "", "", err
	}
	s.cacheExternalAccountPassword(cacheKey, externalAccountCredentialSourceBitwarden, password)
	return password, externalAccountCredentialSourceBitwarden, nil
}
