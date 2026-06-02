package store

import (
	"context"
	"strings"

	"github.com/sloppy-org/sloptools/internal/store/providerkind"
	_ "modernc.org/sqlite"
)

const (
	ExternalAccountCredentialSourceEnv       = externalAccountCredentialSourceEnv
	ExternalAccountCredentialSourceBitwarden = externalAccountCredentialSourceBitwarden
	ExternalAccountCredentialSourceFile      = externalAccountCredentialSourceFile
)

var ErrExternalAccountPasswordUnavailable = errExternalAccountPasswordUnavailable

func ScopedContextSelect(linkTable, entityColumn, entityExpr string) string {
	return scopedContextSelect(linkTable, entityColumn, entityExpr)
}

func (s *Store) SetExternalAccountLookupEnv(fn func(string) (string, bool)) {
	s.externalAccountLookupEnv = fn
}

func (s *Store) SetExternalAccountCommandRunner(fn func(context.Context, string, ...string) (string, error)) {
	s.externalAccountCommandRunner = fn
}

func StringsJoin(parts []string, sep string) string {
	return stringsJoin(parts, sep)
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
	if cached, ok := s.cachedExternalAccountPassword(cacheKey); ok {
		return cached.value, cached.source, nil
	}
	if credentialRef == "" {
		return "", "", errExternalAccountPasswordUnavailable
	}
	if strings.HasPrefix(strings.ToLower(credentialRef), "file://") {
		password, err := s.resolveFilePassword(credentialRef)
		if err != nil {
			return "", "", err
		}
		s.cacheExternalAccountPassword(cacheKey, externalAccountCredentialSourceFile, password)
		return password, externalAccountCredentialSourceFile, nil
	}
	password, err := s.resolveBitwardenPassword(ctx, credentialRef)
	if err != nil {
		return "", "", err
	}
	s.cacheExternalAccountPassword(cacheKey, externalAccountCredentialSourceBitwarden, password)
	return password, externalAccountCredentialSourceBitwarden, nil
}

func IsEmailProvider(provider string) bool {
	return providerkind.IsEmail(provider)
}

func IsManagedEmailProvider(provider string) bool {
	return providerkind.IsManagedEmail(provider)
}
