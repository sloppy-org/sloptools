package store

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestExternalAccountStoreCRUD(t *testing.T) {
	s := newTestStore(t)

	workConfig := map[string]any{
		"host":     "imap.example.com",
		"port":     993,
		"username": "alice@example.com",
	}
	work, err := s.CreateExternalAccount(SphereWork, ExternalProviderIMAP, " Work Mail ", workConfig)
	if err != nil {
		t.Fatalf("CreateExternalAccount(work) error: %v", err)
	}
	if work.Label != "Work Mail" {
		t.Fatalf("work label = %q, want %q", work.Label, "Work Mail")
	}
	if !work.Enabled {
		t.Fatal("expected created external account to be enabled")
	}

	personal, err := s.CreateExternalAccount(SpherePrivate, ExternalProviderGmail, "Personal Gmail", map[string]any{
		"username":   "bob@gmail.com",
		"token_file": "gmail-personal.json",
	})
	if err != nil {
		t.Fatalf("CreateExternalAccount(personal) error: %v", err)
	}

	gotWork, err := s.GetExternalAccount(work.ID)
	if err != nil {
		t.Fatalf("GetExternalAccount(work) error: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(gotWork.ConfigJSON), &decoded); err != nil {
		t.Fatalf("unmarshal config_json: %v", err)
	}
	if decoded["host"] != "imap.example.com" {
		t.Fatalf("config host = %v, want imap.example.com", decoded["host"])
	}

	workAccounts, err := s.ListExternalAccounts(SphereWork)
	if err != nil {
		t.Fatalf("ListExternalAccounts(work) error: %v", err)
	}
	if len(workAccounts) != 1 || workAccounts[0].ID != work.ID {
		t.Fatalf("ListExternalAccounts(work) = %+v, want only work account", workAccounts)
	}

	gmailAccounts, err := s.ListExternalAccountsByProvider(ExternalProviderGmail)
	if err != nil {
		t.Fatalf("ListExternalAccountsByProvider(gmail) error: %v", err)
	}
	if len(gmailAccounts) != 1 || gmailAccounts[0].ID != personal.ID {
		t.Fatalf("ListExternalAccountsByProvider(gmail) = %+v, want personal account", gmailAccounts)
	}

	updatedLabel := "Personal Gmail Primary"
	disabled := false
	if err := s.UpdateExternalAccount(personal.ID, ExternalAccountUpdate{
		AccountName: &updatedLabel,
		Config:      map[string]any{"username": "bob@gmail.com", "token_path": "/tmp/tokens/personal.json"},
		Enabled:     &disabled,
	}); err != nil {
		t.Fatalf("UpdateExternalAccount() error: %v", err)
	}
	gotPersonal, err := s.GetExternalAccount(personal.ID)
	if err != nil {
		t.Fatalf("GetExternalAccount(personal) error: %v", err)
	}
	if gotPersonal.Label != updatedLabel {
		t.Fatalf("updated label = %q, want %q", gotPersonal.Label, updatedLabel)
	}
	if gotPersonal.Enabled {
		t.Fatal("expected updated external account to be disabled")
	}

	if err := s.DeleteExternalAccount(work.ID); err != nil {
		t.Fatalf("DeleteExternalAccount(work) error: %v", err)
	}
	accounts, err := s.ListExternalAccounts("")
	if err != nil {
		t.Fatalf("ListExternalAccounts(all) error: %v", err)
	}
	if len(accounts) != 1 || accounts[0].ID != personal.ID {
		t.Fatalf("ListExternalAccounts(all) = %+v, want only personal account", accounts)
	}
}

func TestExternalAccountStoreRejectsInvalidConfigAndIdentity(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.CreateExternalAccount("", ExternalProviderGmail, "Mail", nil); err == nil {
		t.Fatal("expected missing sphere error")
	}
	if _, err := s.CreateExternalAccount(SphereWork, "smtp", "Mail", nil); err == nil {
		t.Fatal("expected unsupported provider error")
	}
	if _, err := s.CreateExternalAccount(SphereWork, ExternalProviderGmail, "", nil); err == nil {
		t.Fatal("expected missing label error")
	}
	if _, err := s.CreateExternalAccount(SphereWork, ExternalProviderGmail, "Mail", map[string]any{"password": "secret"}); err == nil {
		t.Fatal("expected password config rejection")
	}
	if _, err := s.CreateExternalAccount(SphereWork, ExternalProviderGmail, "Mail", map[string]any{"oauth_token": "raw-token"}); err == nil {
		t.Fatal("expected token config rejection")
	}
	first, err := s.CreateExternalAccount(SphereWork, ExternalProviderGmail, "Mail", map[string]any{"username": "mail@example.com"})
	if err != nil {
		t.Fatalf("CreateExternalAccount(first) error: %v", err)
	}
	if _, err := s.CreateExternalAccount(SphereWork, ExternalProviderGmail, "Mail", map[string]any{"username": "dupe@example.com"}); err == nil {
		t.Fatal("expected duplicate account identity rejection")
	}
	badSphere := "office"
	if err := s.UpdateExternalAccount(first.ID, ExternalAccountUpdate{Sphere: &badSphere}); err == nil {
		t.Fatal("expected invalid update sphere error")
	}
}

func TestExternalAccountCredentialHelpers(t *testing.T) {
	envVar := ExternalAccountPasswordEnvVar(ExternalProviderGoogleCalendar, "Work Calendar")
	if envVar != "SLOPSHELL_GOOGLE_CALENDAR_PASSWORD_WORK_CALENDAR" {
		t.Fatalf("ExternalAccountPasswordEnvVar() = %q", envVar)
	}

	tokenPath := ExternalAccountTokenPath("/home/test/.config/slopshell", ExternalProviderGmail, "Work Gmail")
	wantPath := filepath.Join("/home/test/.config/slopshell", "tokens", "gmail_work_gmail.json")
	if tokenPath != wantPath {
		t.Fatalf("ExternalAccountTokenPath() = %q, want %q", tokenPath, wantPath)
	}
}

func TestResolveExternalAccountPasswordUsesEnvFirstAndCaches(t *testing.T) {
	s := newTestStore(t)

	account, err := s.CreateExternalAccount(SphereWork, ExternalProviderIMAP, "Work Mail", map[string]any{
		"host":           "imap.example.com",
		"username":       "alice@example.com",
		"credential_ref": "bw://work-email-imap",
	})
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}

	envVar := ExternalAccountPasswordEnvVar(account.Provider, account.Label)
	s.externalAccountLookupEnv = func(key string) (string, bool) {
		if key != envVar {
			t.Fatalf("env lookup key = %q, want %q", key, envVar)
		}
		return "env-secret", true
	}
	commandCalls := 0
	s.externalAccountCommandRunner = func(_ context.Context, name string, args ...string) (string, error) {
		commandCalls++
		t.Fatalf("unexpected credential command: %s %v", name, args)
		return "", nil
	}

	password, source, err := s.ResolveExternalAccountPassword(context.Background(), account.ID)
	if err != nil {
		t.Fatalf("ResolveExternalAccountPassword() error: %v", err)
	}
	if password != "env-secret" {
		t.Fatalf("ResolveExternalAccountPassword() password = %q, want env-secret", password)
	}
	if source != externalAccountCredentialSourceEnv {
		t.Fatalf("ResolveExternalAccountPassword() source = %q, want %q", source, externalAccountCredentialSourceEnv)
	}
	if commandCalls != 0 {
		t.Fatalf("credential command calls = %d, want 0", commandCalls)
	}

	s.externalAccountLookupEnv = func(string) (string, bool) {
		return "", false
	}
	password, source, err = s.ResolveExternalAccountPassword(context.Background(), account.ID)
	if err != nil {
		t.Fatalf("ResolveExternalAccountPassword() cached error: %v", err)
	}
	if password != "env-secret" {
		t.Fatalf("cached password = %q, want env-secret", password)
	}
	if source != externalAccountCredentialSourceEnv {
		t.Fatalf("cached source = %q, want %q", source, externalAccountCredentialSourceEnv)
	}
	if commandCalls != 0 {
		t.Fatalf("credential command calls after cache = %d, want 0", commandCalls)
	}
}

func TestResolveExternalAccountPasswordFallsBackToBitwardenAndCaches(t *testing.T) {
	s := newTestStore(t)

	account, err := s.CreateExternalAccount(SphereWork, ExternalProviderIMAP, "Work Mail", map[string]any{
		"host":           "imap.example.com",
		"username":       "alice@example.com",
		"credential_ref": "bw://work-email-imap",
	})
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}

	s.externalAccountLookupEnv = func(string) (string, bool) {
		return "", false
	}
	commandCalls := 0
	s.externalAccountCommandRunner = func(_ context.Context, name string, args ...string) (string, error) {
		commandCalls++
		if name != "bw" {
			t.Fatalf("command name = %q, want bw", name)
		}
		if len(args) != 3 || args[0] != "get" || args[1] != "password" || args[2] != "work-email-imap" {
			t.Fatalf("command args = %#v, want bw get password work-email-imap", args)
		}
		return "bitwarden-secret\n", nil
	}

	password, source, err := s.ResolveExternalAccountPasswordForAccount(context.Background(), account)
	if err != nil {
		t.Fatalf("ResolveExternalAccountPasswordForAccount() error: %v", err)
	}
	if password != "bitwarden-secret" {
		t.Fatalf("password = %q, want bitwarden-secret", password)
	}
	if source != externalAccountCredentialSourceBitwarden {
		t.Fatalf("source = %q, want %q", source, externalAccountCredentialSourceBitwarden)
	}
	if commandCalls != 1 {
		t.Fatalf("credential command calls = %d, want 1", commandCalls)
	}

	password, source, err = s.ResolveExternalAccountPasswordForAccount(context.Background(), account)
	if err != nil {
		t.Fatalf("ResolveExternalAccountPasswordForAccount() cached error: %v", err)
	}
	if password != "bitwarden-secret" {
		t.Fatalf("cached password = %q, want bitwarden-secret", password)
	}
	if source != externalAccountCredentialSourceBitwarden {
		t.Fatalf("cached source = %q, want %q", source, externalAccountCredentialSourceBitwarden)
	}
	if commandCalls != 1 {
		t.Fatalf("credential command calls after cache = %d, want 1", commandCalls)
	}
}

func TestResolveExternalAccountPasswordRejectsMissingOrUnsupportedCredentialConfig(t *testing.T) {
	s := newTestStore(t)
	s.externalAccountLookupEnv = func(string) (string, bool) {
		return "", false
	}

	missingAccount, err := s.CreateExternalAccount(SphereWork, ExternalProviderIMAP, "Work Mail", map[string]any{
		"host":     "imap.example.com",
		"username": "alice@example.com",
	})
	if err != nil {
		t.Fatalf("CreateExternalAccount(missing) error: %v", err)
	}
	if _, _, err := s.ResolveExternalAccountPassword(context.Background(), missingAccount.ID); !errors.Is(err, errExternalAccountPasswordUnavailable) {
		t.Fatalf("ResolveExternalAccountPassword(missing) error = %v, want %v", err, errExternalAccountPasswordUnavailable)
	}

	unsupportedAccount, err := s.CreateExternalAccount(SphereWork, ExternalProviderIMAP, "Other Mail", map[string]any{
		"host":           "imap.example.com",
		"username":       "bob@example.com",
		"credential_ref": "vault://other-mail",
	})
	if err != nil {
		t.Fatalf("CreateExternalAccount(unsupported) error: %v", err)
	}
	if _, _, err := s.ResolveExternalAccountPassword(context.Background(), unsupportedAccount.ID); err == nil || !strings.Contains(err.Error(), `unsupported credential_ref "vault://other-mail"`) {
		t.Fatalf("ResolveExternalAccountPassword(unsupported) error = %v, want unsupported credential_ref", err)
	}
}

func TestResolveExternalAccountPasswordFallsBackToLegacyHelpyEnvVar(t *testing.T) {
	s := newTestStore(t)

	account, err := s.CreateExternalAccount(SphereWork, ExternalProviderExchangeEWS, "TU Graz Exchange", map[string]any{
		"endpoint":             "https://exchange.example.test/EWS/Exchange.asmx",
		"username":             "ert",
		"legacy_helpy_env_var": "HELPY_IMAP_PASSWORD_TUGRAZ",
	})
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}

	s.externalAccountLookupEnv = func(key string) (string, bool) {
		if key != "HELPY_IMAP_PASSWORD_TUGRAZ" {
			return "", false
		}
		return "legacy-secret", true
	}

	password, source, err := s.ResolveExternalAccountPasswordForAccount(context.Background(), account)
	if err != nil {
		t.Fatalf("ResolveExternalAccountPasswordForAccount() error: %v", err)
	}
	if password != "legacy-secret" {
		t.Fatalf("password = %q, want legacy-secret", password)
	}
	if source != externalAccountCredentialSourceEnv {
		t.Fatalf("source = %q, want %q", source, externalAccountCredentialSourceEnv)
	}
}
