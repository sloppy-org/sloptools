package groupware

import (
	"context"
	"testing"

	"github.com/sloppy-org/sloptools/internal/calendar"
	"github.com/sloppy-org/sloptools/internal/email"
	"github.com/sloppy-org/sloptools/internal/store"
)

func TestRegistryCalendarForEWSSharesClient(t *testing.T) {
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

	mailProvider, err := reg.MailFor(ctx, account.ID)
	if err != nil {
		t.Fatalf("MailFor() error: %v", err)
	}
	mailEWS, ok := mailProvider.(*email.ExchangeEWSMailProvider)
	if !ok {
		t.Fatalf("mail provider = %T, want *email.ExchangeEWSMailProvider", mailProvider)
	}

	first, err := reg.CalendarFor(ctx, account.ID)
	if err != nil {
		t.Fatalf("CalendarFor() first call error: %v", err)
	}
	second, err := reg.CalendarFor(ctx, account.ID)
	if err != nil {
		t.Fatalf("CalendarFor() second call error: %v", err)
	}
	ewsCal, ok := first.(*calendar.EWSProvider)
	if !ok {
		t.Fatalf("calendar provider = %T, want *calendar.EWSProvider", first)
	}
	if first != second {
		t.Fatalf("calendar provider was rebuilt between calls: first=%p second=%p", first, second)
	}
	if ewsCal == nil {
		t.Fatalf("calendar ews provider is nil")
	}
	if ewsCal.Client() != mailEWS.Client() {
		t.Fatalf("calendar ews client %p differs from mail ews client %p", ewsCal.Client(), mailEWS.Client())
	}
}
