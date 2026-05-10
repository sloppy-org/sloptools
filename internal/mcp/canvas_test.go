package mcp

import (
	"context"
	"fmt"
	"testing"

	"github.com/sloppy-org/sloptools/internal/contacts"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/store"
)

type fakeContactsProvider struct {
	name           string
	listed         []providerdata.Contact
	searched       []providerdata.Contact
	groups         []contacts.Group
	photoMime      string
	photoBytes     []byte
	listCalls      int
	searchCalls    int
	createCalls    int
	updateCalls    int
	deleteCalls    int
	groupCalls     int
	photoCalls     int
	closeCalls     int
	failListWith   error
	failSearchWith error
	failCreateWith error
	failUpdateWith error
	failDeleteWith error
	failGroupWith  error
	failPhotoWith  error
}

func (f *fakeContactsProvider) ListContacts(_ context.Context) ([]providerdata.Contact, error) {
	f.listCalls++
	if f.failListWith != nil {
		return nil, f.failListWith
	}
	out := make([]providerdata.Contact, len(f.listed))
	copy(out, f.listed)
	return out, nil
}

func (f *fakeContactsProvider) GetContact(_ context.Context, id string) (providerdata.Contact, error) {
	for _, c := range f.listed {
		if c.ProviderRef == id {
			return c, nil
		}
	}
	return providerdata.Contact{}, fmt.Errorf("contact %q not found", id)
}

func (f *fakeContactsProvider) SearchContacts(_ context.Context, query string) ([]providerdata.Contact, error) {
	f.searchCalls++
	if f.failSearchWith != nil {
		return nil, f.failSearchWith
	}
	if f.searched != nil {
		return f.searched, nil
	}
	q := query
	out := make([]providerdata.Contact, 0)
	for _, c := range f.listed {
		if c.Name == q || c.Email == q {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeContactsProvider) CreateContact(_ context.Context, c providerdata.Contact) (providerdata.Contact, error) {
	f.createCalls++
	if f.failCreateWith != nil {
		return providerdata.Contact{}, f.failCreateWith
	}
	c.ProviderRef = fmt.Sprintf("people/c%d", len(f.listed)+1)
	f.listed = append(f.listed, c)
	return c, nil
}

func (f *fakeContactsProvider) UpdateContact(_ context.Context, c providerdata.Contact) (providerdata.Contact, error) {
	f.updateCalls++
	if f.failUpdateWith != nil {
		return providerdata.Contact{}, f.failUpdateWith
	}
	for i := range f.listed {
		if f.listed[i].ProviderRef == c.ProviderRef {
			f.listed[i] = c
			return c, nil
		}
	}
	return providerdata.Contact{}, fmt.Errorf("contact %q not found", c.ProviderRef)
}

func (f *fakeContactsProvider) DeleteContact(_ context.Context, id string) error {
	f.deleteCalls++
	if f.failDeleteWith != nil {
		return f.failDeleteWith
	}
	for i := range f.listed {
		if f.listed[i].ProviderRef == id {
			f.listed = append(f.listed[:i], f.listed[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("contact %q not found", id)
}

func (f *fakeContactsProvider) ListContactGroups(_ context.Context) ([]contacts.Group, error) {
	f.groupCalls++
	if f.failGroupWith != nil {
		return nil, f.failGroupWith
	}
	out := make([]contacts.Group, len(f.groups))
	copy(out, f.groups)
	return out, nil
}

func (f *fakeContactsProvider) AddToGroup(_ context.Context, _ string, _ []string) error {
	return nil
}

func (f *fakeContactsProvider) RemoveFromGroup(_ context.Context, _ string, _ []string) error {
	return nil
}

func (f *fakeContactsProvider) GetContactPhoto(_ context.Context, _ string) ([]byte, string, error) {
	f.photoCalls++
	if f.failPhotoWith != nil {
		return nil, "", f.failPhotoWith
	}
	return append([]byte(nil), f.photoBytes...), f.photoMime, nil
}

func (f *fakeContactsProvider) ProviderName() string {
	if f.name == "" {
		return "fake_contacts"
	}
	return f.name
}

func (f *fakeContactsProvider) Close() error {
	f.closeCalls++
	return nil
}

type readOnlyContactsProvider struct {
	name string
	list []providerdata.Contact
}

func (p *readOnlyContactsProvider) ListContacts(_ context.Context) ([]providerdata.Contact, error) {
	return append([]providerdata.Contact(nil), p.list...), nil
}

func (p *readOnlyContactsProvider) GetContact(_ context.Context, id string) (providerdata.Contact, error) {
	for _, c := range p.list {
		if c.ProviderRef == id {
			return c, nil
		}
	}
	return providerdata.Contact{}, fmt.Errorf("contact %q not found", id)
}

func (p *readOnlyContactsProvider) ProviderName() string { return p.name }
func (p *readOnlyContactsProvider) Close() error         { return nil }

func TestContactListRoutesByAccountID(t *testing.T) {
	s, st, _ := newDomainServerForTest(t)
	work, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderExchangeEWS, "Work EWS", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount(work): %v", err)
	}
	private, err := st.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderGmail, "Personal Gmail", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount(private): %v", err)
	}
	workProvider := &fakeContactsProvider{
		name: "exchange_ews_contacts",
		listed: []providerdata.Contact{
			{ProviderRef: "ews:1", Name: "Bob", Email: "bob@tugraz.at"},
			{ProviderRef: "ews:2", Name: "Alice", Email: "alice@tugraz.at"},
		},
	}
	privateProvider := &fakeContactsProvider{name: "google_contacts", listed: []providerdata.Contact{{ProviderRef: "people/c1", Name: "Friend"}}}
	s.newContactsProvider = func(_ context.Context, account store.ExternalAccount) (contacts.Provider, error) {
		switch account.ID {
		case work.ID:
			return workProvider, nil
		case private.ID:
			return privateProvider, nil
		}
		return nil, fmt.Errorf("unexpected account: %d", account.ID)
	}

	got, err := s.callTool("sloppy_contacts", map[string]interface{}{"action": "list", "account_id": work.ID})
	if err != nil {
		t.Fatalf("contact_list: %v", err)
	}
	if got["account_id"] != work.ID {
		t.Fatalf("account_id = %v, want %d", got["account_id"], work.ID)
	}
	if got["provider"] != "exchange_ews_contacts" {
		t.Fatalf("provider = %v, want exchange_ews_contacts", got["provider"])
	}
	if got["count"].(int) != 2 {
		t.Fatalf("count = %v, want 2", got["count"])
	}
	listPayload, _ := got["contacts"].([]map[string]interface{})
	if len(listPayload) != 2 {
		t.Fatalf("len(contacts) = %d, want 2", len(listPayload))
	}
	if listPayload[0]["name"] != "Alice" {
		t.Fatalf("first contact = %v, want Alice (sorted by name)", listPayload[0]["name"])
	}
	if workProvider.listCalls != 1 || privateProvider.listCalls != 0 {
		t.Fatalf("call routing failed: work=%d private=%d", workProvider.listCalls, privateProvider.listCalls)
	}
	if workProvider.closeCalls != 1 {
		t.Fatalf("workProvider.closeCalls = %d, want 1", workProvider.closeCalls)
	}
}

func TestContactListPicksFirstEnabledAccountForSphere(t *testing.T) {
	s, st, _ := newDomainServerForTest(t)
	disabled, err := st.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderGmail, "Disabled Gmail", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount(disabled): %v", err)
	}
	if err := st.UpdateExternalAccount(disabled.ID, store.ExternalAccountUpdate{Enabled: ptrBool(false)}); err != nil {
		t.Fatalf("disable account: %v", err)
	}
	enabled, err := st.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderGmail, "Enabled Gmail", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount(enabled): %v", err)
	}
	if _, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderExchangeEWS, "Work EWS", map[string]any{}); err != nil {
		t.Fatalf("CreateExternalAccount(work): %v", err)
	}
	enabledProvider := &fakeContactsProvider{name: "google_contacts", listed: []providerdata.Contact{{ProviderRef: "people/c1", Name: "Pat"}}}
	s.newContactsProvider = func(_ context.Context, account store.ExternalAccount) (contacts.Provider, error) {
		if account.ID != enabled.ID {
			return nil, fmt.Errorf("unexpected account selected: %d", account.ID)
		}
		return enabledProvider, nil
	}

	got, err := s.callTool("sloppy_contacts", map[string]interface{}{"action": "list", "sphere": "private"})
	if err != nil {
		t.Fatalf("contact_list(sphere=private): %v", err)
	}
	if got["account_id"] != enabled.ID {
		t.Fatalf("account_id = %v, want %d", got["account_id"], enabled.ID)
	}
	if enabledProvider.listCalls != 1 {
		t.Fatalf("enabledProvider.listCalls = %d, want 1", enabledProvider.listCalls)
	}
}

func TestContactListWithoutAnyContactsAccountErrors(t *testing.T) {
	s, _, _ := newDomainServerForTest(t)
	if _, err := s.callTool("sloppy_contacts", map[string]interface{}{"action": "list"}); err == nil {
		t.Fatal("contact_list without any contacts-capable account should error")
	}
}

func TestContactListRejectsNonContactsAccount(t *testing.T) {
	s, st, _ := newDomainServerForTest(t)
	imap, err := st.CreateExternalAccount(store.SpherePrivate, "imap", "imap.example.com", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount(imap): %v", err)
	}
	if _, err := s.callTool("sloppy_contacts", map[string]interface{}{"action": "list", "account_id": imap.ID}); err == nil {
		t.Fatal("contact_list with imap account should error: imap is not contacts-capable")
	}
}

func TestContactCreateUpdateDeleteRoundTrip(t *testing.T) {
	s, st, _ := newDomainServerForTest(t)
	account, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderExchangeEWS, "TU Graz", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	provider := &fakeContactsProvider{name: "exchange_ews_contacts"}
	s.newContactsProvider = func(_ context.Context, _ store.ExternalAccount) (contacts.Provider, error) {
		return provider, nil
	}

	created, err := s.callTool("sloppy_contacts", map[string]interface{}{"action": "create",
		"account_id": account.ID,
		"contact": map[string]interface{}{
			"name":         "Marie Curie",
			"email":        "marie@example.com",
			"organization": "Sorbonne",
			"phones":       []interface{}{"+33 1 23 45 67 89"},
			"addresses":    []interface{}{map[string]interface{}{"type": "work", "street": "11 rue Pierre", "city": "Paris", "country": "FR"}},
			"birthday":     "1867-11-07",
			"notes":        "physicist",
		},
	})
	if err != nil {
		t.Fatalf("contact_create: %v", err)
	}
	if created["created"] != true {
		t.Fatalf("created = %v, want true", created["created"])
	}
	if provider.createCalls != 1 {
		t.Fatalf("createCalls = %d, want 1", provider.createCalls)
	}
	contact := mapValue(t, created["contact"])
	providerRef := stringValue(t, contact["provider_ref"])
	if providerRef == "" {
		t.Fatal("created contact missing provider_ref")
	}
	if contact["birthday"] != "1867-11-07" {
		t.Fatalf("birthday = %v, want 1867-11-07", contact["birthday"])
	}

	updated, err := s.callTool("sloppy_contacts", map[string]interface{}{"action": "update",
		"account_id": account.ID,
		"contact": map[string]interface{}{
			"provider_ref": providerRef,
			"name":         "Marie Sklodowska-Curie",
			"email":        "marie@example.com",
		},
	})
	if err != nil {
		t.Fatalf("contact_update: %v", err)
	}
	if updated["updated"] != true {
		t.Fatalf("updated = %v, want true", updated["updated"])
	}
	if provider.updateCalls != 1 {
		t.Fatalf("updateCalls = %d, want 1", provider.updateCalls)
	}
	updatedContact := mapValue(t, updated["contact"])
	if updatedContact["name"] != "Marie Sklodowska-Curie" {
		t.Fatalf("updated name = %v", updatedContact["name"])
	}

	got, err := s.callTool("sloppy_contacts", map[string]interface{}{"action": "get", "account_id": account.ID, "id": providerRef})
	if err != nil {
		t.Fatalf("contact_get: %v", err)
	}
	if mapValue(t, got["contact"])["name"] != "Marie Sklodowska-Curie" {
		t.Fatalf("get returned stale contact")
	}

	deleted, err := s.callTool("sloppy_contacts", map[string]interface{}{"action": "delete", "account_id": account.ID, "id": providerRef})
	if err != nil {
		t.Fatalf("contact_delete: %v", err)
	}
	if deleted["deleted"] != true {
		t.Fatalf("deleted = %v, want true", deleted["deleted"])
	}
	if provider.deleteCalls != 1 {
		t.Fatalf("deleteCalls = %d, want 1", provider.deleteCalls)
	}
	if len(provider.listed) != 0 {
		t.Fatalf("provider.listed len = %d after delete, want 0", len(provider.listed))
	}
}

func TestContactCreateRequiresContactArg(t *testing.T) {
	s, st, _ := newDomainServerForTest(t)
	account, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderExchangeEWS, "TU Graz", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	provider := &fakeContactsProvider{name: "exchange_ews_contacts"}
	s.newContactsProvider = func(_ context.Context, _ store.ExternalAccount) (contacts.Provider, error) {
		return provider, nil
	}
	if _, err := s.callTool("sloppy_contacts", map[string]interface{}{"action": "create", "account_id": account.ID}); err == nil {
		t.Fatal("contact_create without contact arg should error")
	}
	if _, err := s.callTool("sloppy_contacts", map[string]interface{}{"action": "create", "account_id": account.ID, "contact": map[string]interface{}{}}); err == nil {
		t.Fatal("contact_create with empty contact should error")
	}
	if provider.createCalls != 0 {
		t.Fatalf("createCalls = %d, want 0 (validation should reject before calling provider)", provider.createCalls)
	}
}

func TestContactUpdateRequiresProviderRef(t *testing.T) {
	s, st, _ := newDomainServerForTest(t)
	account, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderExchangeEWS, "TU Graz", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	s.newContactsProvider = func(_ context.Context, _ store.ExternalAccount) (contacts.Provider, error) {
		return &fakeContactsProvider{name: "exchange_ews_contacts"}, nil
	}
	if _, err := s.callTool("sloppy_contacts", map[string]interface{}{"action": "update", "account_id": account.ID, "contact": map[string]interface{}{"name": "Anon"}}); err == nil {
		t.Fatal("contact_update without provider_ref should error")
	}
}

func ptrBool(v bool) *bool { return &v }
