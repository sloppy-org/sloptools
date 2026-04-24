package store

import (
	"context"

	_ "modernc.org/sqlite"
)

const (
	ExternalAccountCredentialSourceEnv       = externalAccountCredentialSourceEnv
	ExternalAccountCredentialSourceBitwarden = externalAccountCredentialSourceBitwarden
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
