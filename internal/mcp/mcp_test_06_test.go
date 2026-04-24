package mcp

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/sloppy-org/sloptools/internal/mailboxsettings"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/store"
)

type fakeOOFProvider struct {
	name        string
	state       providerdata.OOFSettings
	getCalls    int
	setCalls    int
	closeCalls  int
	failGetWith error
	failSetWith error
}

func (p *fakeOOFProvider) GetOOF(_ context.Context) (providerdata.OOFSettings, error) {
	p.getCalls++
	if p.failGetWith != nil {
		return providerdata.OOFSettings{}, p.failGetWith
	}
	return p.state, nil
}

func (p *fakeOOFProvider) SetOOF(_ context.Context, settings providerdata.OOFSettings) error {
	p.setCalls++
	if p.failSetWith != nil {
		return p.failSetWith
	}
	p.state = settings
	return nil
}

func (p *fakeOOFProvider) ProviderName() string {
	if p.name == "" {
		return "fake_oof"
	}
	return p.name
}

func (p *fakeOOFProvider) Close() error {
	p.closeCalls++
	return nil
}

func TestMailOOFRoundTripWithEWSFakeProvider(t *testing.T) {
	s, st, _ := newDomainServerForTest(t)
	account, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderExchangeEWS, "TU Graz", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	provider := &fakeOOFProvider{name: "exchange_ews_mailbox_settings"}
	s.newMailboxSettingsProvider = func(_ context.Context, _ store.ExternalAccount) (mailboxsettings.OOFProvider, error) {
		return provider, nil
	}

	got, err := s.callTool("mail_oof_get", map[string]interface{}{"account_id": account.ID})
	if err != nil {
		t.Fatalf("mail_oof_get(initial): %v", err)
	}
	if got["provider"] != "exchange_ews_mailbox_settings" {
		t.Fatalf("provider = %v, want exchange_ews_mailbox_settings", got["provider"])
	}
	settings := mapValue(t, got["settings"])
	if boolValue(t, settings["enabled"]) {
		t.Fatalf("initial enabled = true, want false")
	}

	setOut, err := s.callTool("mail_oof_set", map[string]interface{}{
		"account_id": account.ID,
		"settings": map[string]interface{}{
			"enabled":        true,
			"scope":          "all",
			"internal_reply": "Out today.",
			"external_reply": "External: out today.",
			"start_at":       "2026-04-24T08:00:00Z",
			"end_at":         "2026-05-01T17:00:00Z",
		},
	})
	if err != nil {
		t.Fatalf("mail_oof_set: %v", err)
	}
	if setOut["set"] != true {
		t.Fatalf("set = %v, want true", setOut["set"])
	}
	if provider.setCalls != 1 {
		t.Fatalf("provider.setCalls = %d, want 1", provider.setCalls)
	}
	if !provider.state.Enabled {
		t.Fatal("provider stored Enabled = false, want true")
	}
	if provider.state.Scope != "all" {
		t.Fatalf("provider stored Scope = %q, want all", provider.state.Scope)
	}
	if provider.state.InternalReply != "Out today." || provider.state.ExternalReply != "External: out today." {
		t.Fatalf("provider stored replies = %q/%q", provider.state.InternalReply, provider.state.ExternalReply)
	}
	wantStart := time.Date(2026, time.April, 24, 8, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2026, time.May, 1, 17, 0, 0, 0, time.UTC)
	if provider.state.StartAt == nil || !provider.state.StartAt.Equal(wantStart) {
		t.Fatalf("provider stored StartAt = %v, want %v", provider.state.StartAt, wantStart)
	}
	if provider.state.EndAt == nil || !provider.state.EndAt.Equal(wantEnd) {
		t.Fatalf("provider stored EndAt = %v, want %v", provider.state.EndAt, wantEnd)
	}

	got2, err := s.callTool("mail_oof_get", map[string]interface{}{"account_id": account.ID})
	if err != nil {
		t.Fatalf("mail_oof_get(after-set): %v", err)
	}
	settings2 := mapValue(t, got2["settings"])
	if !boolValue(t, settings2["enabled"]) {
		t.Fatal("after-set enabled = false, want true")
	}
	if stringValue(t, settings2["scope"]) != "all" {
		t.Fatalf("after-set scope = %v, want all", settings2["scope"])
	}
	if stringValue(t, settings2["start_at"]) != "2026-04-24T08:00:00Z" {
		t.Fatalf("after-set start_at = %v", settings2["start_at"])
	}
	if stringValue(t, settings2["end_at"]) != "2026-05-01T17:00:00Z" {
		t.Fatalf("after-set end_at = %v", settings2["end_at"])
	}
}

func TestMailOOFGetSurfacesUnsupportedAsCapabilityCode(t *testing.T) {
	s, st, _ := newDomainServerForTest(t)
	account, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderExchangeEWS, "TU Graz", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	s.newMailboxSettingsProvider = func(_ context.Context, _ store.ExternalAccount) (mailboxsettings.OOFProvider, error) {
		return &fakeOOFProvider{
			name:        "fake_oof",
			failGetWith: fmt.Errorf("ews OOF GetOOF: %w", mailboxsettings.ErrUnsupported),
		}, nil
	}
	got, err := s.callTool("mail_oof_get", map[string]interface{}{"account_id": account.ID})
	if err != nil {
		t.Fatalf("mail_oof_get returned error: %v", err)
	}
	if got["error_code"] != "capability_unsupported" {
		t.Fatalf("error_code = %v, want capability_unsupported", got["error_code"])
	}
}
