package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/sloppy-org/sloptools/internal/mailboxsettings"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/store"
)

type fakeDelegationProvider struct {
	fakeOOFProvider
	delegates       []providerdata.Delegate
	shared          []providerdata.SharedMailbox
	delegateErr     error
	sharedErr       error
	delegateCalls   int
	sharedCalls     int
	closeCallsExtra int
}

func (p *fakeDelegationProvider) ListDelegates(_ context.Context) ([]providerdata.Delegate, error) {
	p.delegateCalls++
	if p.delegateErr != nil {
		return nil, p.delegateErr
	}
	return p.delegates, nil
}

func (p *fakeDelegationProvider) ListSharedMailboxes(_ context.Context) ([]providerdata.SharedMailbox, error) {
	p.sharedCalls++
	if p.sharedErr != nil {
		return nil, p.sharedErr
	}
	return p.shared, nil
}

func TestMailDelegateListReturnsDelegatesAndSharedMailboxes(t *testing.T) {
	s, st, _ := newDomainServerForTest(t)
	account, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderGmail, "Gmail", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	provider := &fakeDelegationProvider{
		fakeOOFProvider: fakeOOFProvider{name: "gmail_mailbox_settings"},
		delegates: []providerdata.Delegate{
			{Email: "alice@example.com", Name: "Alice", Permissions: []string{"verification:accepted"}},
			{Email: "bob@example.com", Permissions: []string{"verification:pending"}},
		},
		shared: []providerdata.SharedMailbox{
			{Email: "archive@example.com", AccessLevel: "forwarding:accepted"},
		},
	}
	s.newMailboxSettingsProvider = func(_ context.Context, _ store.ExternalAccount) (mailboxsettings.OOFProvider, error) {
		return provider, nil
	}

	got, err := s.callTool("mail_delegate_list", map[string]interface{}{"account_id": account.ID})
	if err != nil {
		t.Fatalf("mail_delegate_list: %v", err)
	}
	if got["provider"] != "gmail_mailbox_settings" {
		t.Fatalf("provider = %v", got["provider"])
	}
	delegates, ok := got["delegates"].([]map[string]interface{})
	if !ok {
		t.Fatalf("delegates = %T, want []map[string]interface{}", got["delegates"])
	}
	if len(delegates) != 2 {
		t.Fatalf("len(delegates) = %d, want 2", len(delegates))
	}
	if stringValue(t, delegates[0]["email"]) != "alice@example.com" {
		t.Fatalf("delegates[0].email = %v", delegates[0]["email"])
	}
	if stringValue(t, delegates[0]["name"]) != "Alice" {
		t.Fatalf("delegates[0].name = %v", delegates[0]["name"])
	}
	perms, ok := delegates[0]["permissions"].([]string)
	if !ok || len(perms) != 1 || perms[0] != "verification:accepted" {
		t.Fatalf("delegates[0].permissions = %v", delegates[0]["permissions"])
	}
	shared, ok := got["shared_mailboxes"].([]map[string]interface{})
	if !ok {
		t.Fatalf("shared_mailboxes = %T", got["shared_mailboxes"])
	}
	if len(shared) != 1 {
		t.Fatalf("len(shared_mailboxes) = %d, want 1", len(shared))
	}
	if stringValue(t, shared[0]["access_level"]) != "forwarding:accepted" {
		t.Fatalf("shared_mailboxes[0].access_level = %v", shared[0]["access_level"])
	}
	if provider.delegateCalls != 1 || provider.sharedCalls != 1 {
		t.Fatalf("delegateCalls=%d sharedCalls=%d, both want 1", provider.delegateCalls, provider.sharedCalls)
	}
	if provider.closeCalls != 1 {
		t.Fatalf("provider.closeCalls = %d, want 1", provider.closeCalls)
	}
}

func TestMailDelegateListReturnsCapabilityUnsupportedForNonDelegationProvider(t *testing.T) {
	s, st, _ := newDomainServerForTest(t)
	account, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderGmail, "Gmail", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	s.newMailboxSettingsProvider = func(_ context.Context, _ store.ExternalAccount) (mailboxsettings.OOFProvider, error) {
		return &fakeOOFProvider{name: "fake_no_delegation"}, nil
	}

	got, err := s.callTool("mail_delegate_list", map[string]interface{}{"account_id": account.ID})
	if err != nil {
		t.Fatalf("mail_delegate_list: %v", err)
	}
	if got["error_code"] != "capability_unsupported" {
		t.Fatalf("error_code = %v, want capability_unsupported", got["error_code"])
	}
	if got["capability"] != "mailboxsettings.DelegationProvider" {
		t.Fatalf("capability = %v", got["capability"])
	}
}

func TestMailDelegateListSurfacesUnsupportedErrorFromDelegatesListAsCapabilityCode(t *testing.T) {
	s, st, _ := newDomainServerForTest(t)
	account, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderExchangeEWS, "TU Graz", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	provider := &fakeDelegationProvider{
		fakeOOFProvider: fakeOOFProvider{name: "exchange_ews_mailbox_settings"},
		delegateErr:     errors.New("wrapped: " + mailboxsettings.ErrUnsupported.Error()),
	}
	provider.delegateErr = &wrappedErr{inner: mailboxsettings.ErrUnsupported, msg: "ews: wrapped"}
	s.newMailboxSettingsProvider = func(_ context.Context, _ store.ExternalAccount) (mailboxsettings.OOFProvider, error) {
		return provider, nil
	}

	got, err := s.callTool("mail_delegate_list", map[string]interface{}{"account_id": account.ID})
	if err != nil {
		t.Fatalf("mail_delegate_list: %v", err)
	}
	if got["error_code"] != "capability_unsupported" {
		t.Fatalf("error_code = %v, want capability_unsupported", got["error_code"])
	}
}

type wrappedErr struct {
	inner error
	msg   string
}

func (e *wrappedErr) Error() string { return e.msg }
func (e *wrappedErr) Unwrap() error { return e.inner }
