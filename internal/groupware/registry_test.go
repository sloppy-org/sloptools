package groupware

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/sloppy-org/sloptools/internal/email"
	"github.com/sloppy-org/sloptools/internal/store"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.New(filepath.Join(t.TempDir(), "groupware.db"))
	if err != nil {
		t.Fatalf("store.New() error: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// writeGmailFixtures drops a credentials.json and per-account token file into
// configDir so googleauth.New succeeds without network access.
func writeGmailFixtures(t *testing.T, configDir, accountName string) {
	t.Helper()
	credentialsPath := filepath.Join(configDir, "gmail_credentials.json")
	credentials := `{
  "installed": {
    "client_id": "client-id.apps.googleusercontent.com",
    "project_id": "groupware-test",
    "auth_uri": "https://accounts.google.com/o/oauth2/auth",
    "token_uri": "https://oauth2.googleapis.com/token",
    "client_secret": "secret",
    "redirect_uris": ["http://localhost"]
  }
}`
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(configDir) error: %v", err)
	}
	if err := os.WriteFile(credentialsPath, []byte(credentials), 0o600); err != nil {
		t.Fatalf("WriteFile(credentials) error: %v", err)
	}

	tokenPath := store.ExternalAccountTokenPath(configDir, store.ExternalProviderGmail, accountName)
	if err := os.MkdirAll(filepath.Dir(tokenPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(tokens) error: %v", err)
	}
	token := `{"access_token":"access-token","refresh_token":"refresh-token","token_type":"Bearer","expiry":"2030-01-01T00:00:00Z"}`
	if err := os.WriteFile(tokenPath, []byte(token), 0o600); err != nil {
		t.Fatalf("WriteFile(token) error: %v", err)
	}
}

func TestRegistryMailForGmailSharesSession(t *testing.T) {
	st := newTestStore(t)
	configDir := t.TempDir()
	accountName := "Personal Gmail"
	writeGmailFixtures(t, configDir, accountName)

	account, err := st.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderGmail, accountName, map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}

	reg := NewRegistry(st, configDir)
	ctx := context.Background()

	first, err := reg.MailFor(ctx, account.ID)
	if err != nil {
		t.Fatalf("MailFor() first call error: %v", err)
	}
	second, err := reg.MailFor(ctx, account.ID)
	if err != nil {
		t.Fatalf("MailFor() second call error: %v", err)
	}

	gmailFirst, ok := first.(*email.GmailClient)
	if !ok {
		t.Fatalf("first provider = %T, want *email.GmailClient", first)
	}
	gmailSecond, ok := second.(*email.GmailClient)
	if !ok {
		t.Fatalf("second provider = %T, want *email.GmailClient", second)
	}
	if gmailFirst.Session() == nil {
		t.Fatalf("gmail session is nil")
	}
	if gmailFirst.Session() != gmailSecond.Session() {
		t.Fatalf("gmail sessions differ between MailFor calls")
	}
	if cached := reg.GoogleSession(account.ID); cached != gmailFirst.Session() {
		t.Fatalf("registry cached session = %p, want %p", cached, gmailFirst.Session())
	}
}

func TestRegistryMailForEWSSharesClient(t *testing.T) {
	st := newTestStore(t)
	accountName := "TU Graz"
	config := map[string]any{
		"endpoint":       "https://example.invalid/EWS/Exchange.asmx",
		"username":       "alice@example.invalid",
		"server_version": "Exchange2016",
	}
	account, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderExchangeEWS, accountName, config)
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}

	envVar := store.ExternalAccountPasswordEnvVar(store.ExternalProviderExchangeEWS, accountName)
	t.Setenv(envVar, "swordfish")

	reg := NewRegistry(st, t.TempDir())
	ctx := context.Background()

	first, err := reg.MailFor(ctx, account.ID)
	if err != nil {
		t.Fatalf("MailFor() first call error: %v", err)
	}
	second, err := reg.MailFor(ctx, account.ID)
	if err != nil {
		t.Fatalf("MailFor() second call error: %v", err)
	}
	ewsFirst, ok := first.(*email.ExchangeEWSMailProvider)
	if !ok {
		t.Fatalf("first provider = %T, want *email.ExchangeEWSMailProvider", first)
	}
	ewsSecond, ok := second.(*email.ExchangeEWSMailProvider)
	if !ok {
		t.Fatalf("second provider = %T, want *email.ExchangeEWSMailProvider", second)
	}
	if ewsFirst.Client() == nil {
		t.Fatalf("ews client is nil")
	}
	if ewsFirst.Client() != ewsSecond.Client() {
		t.Fatalf("ews clients differ between MailFor calls")
	}
	if cached := reg.EWSClient(account.ID); cached != ewsFirst.Client() {
		t.Fatalf("registry cached client = %p, want %p", cached, ewsFirst.Client())
	}
	// Provider instance itself must be cached so the EWS pipeline doesn't
	// rebuild on each call.
	if first != second {
		t.Fatalf("cached provider identity mismatch: first=%p second=%p", first, second)
	}
}

func TestRegistryMailForConcurrentCalls(t *testing.T) {
	st := newTestStore(t)
	configDir := t.TempDir()
	accountName := "Concurrent Gmail"
	writeGmailFixtures(t, configDir, accountName)

	account, err := st.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderGmail, accountName, map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}

	reg := NewRegistry(st, configDir)
	ctx := context.Background()

	const workers = 16
	providers := make([]email.EmailProvider, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(i int) {
			defer wg.Done()
			providers[i], errs[i] = reg.MailFor(ctx, account.ID)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("MailFor() worker %d error: %v", i, err)
		}
	}
	first := providers[0].(*email.GmailClient).Session()
	if first == nil {
		t.Fatalf("first session is nil")
	}
	for i := 1; i < workers; i++ {
		gmail := providers[i].(*email.GmailClient)
		if gmail.Session() != first {
			t.Fatalf("worker %d session differs from worker 0", i)
		}
	}
}

func TestRegistryPlaceholderCapabilitiesReturnErrUnsupported(t *testing.T) {
	reg := NewRegistry(nil, "")
	ctx := context.Background()
	cases := []struct {
		name string
		call func() error
	}{
		{"TasksFor", func() error { _, err := reg.TasksFor(ctx, 1); return err }},
		{"MailboxSettingsFor", func() error { _, err := reg.MailboxSettingsFor(ctx, 1); return err }},
	}
	for _, tc := range cases {
		if err := tc.call(); !errors.Is(err, ErrUnsupported) {
			t.Fatalf("%s returned %v, want ErrUnsupported", tc.name, err)
		}
	}
}

func TestRegistryMailForIMAPNotCached(t *testing.T) {
	st := newTestStore(t)
	config := map[string]any{"host": "imap.example.com", "port": 993, "username": "alice"}
	account, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderIMAP, "IMAP Work", config)
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}
	envVar := store.ExternalAccountPasswordEnvVar(store.ExternalProviderIMAP, "IMAP Work")
	t.Setenv(envVar, "secret")

	reg := NewRegistry(st, t.TempDir())
	first, err := reg.MailFor(context.Background(), account.ID)
	if err != nil {
		t.Fatalf("MailFor(IMAP) error: %v", err)
	}
	if _, ok := first.(*email.IMAPClient); !ok {
		t.Fatalf("provider = %T, want *email.IMAPClient", first)
	}
	// IMAP clients hold real connection pools that Close() drains; returning a
	// cached instance would tear down sockets underneath concurrent callers.
	second, err := reg.MailFor(context.Background(), account.ID)
	if err != nil {
		t.Fatalf("MailFor(IMAP) second error: %v", err)
	}
	if first == second {
		t.Fatalf("IMAP provider was cached; expected fresh instance per call")
	}
}
