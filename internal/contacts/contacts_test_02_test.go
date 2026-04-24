package contacts

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/sloppy-org/sloptools/internal/ews"
	"github.com/sloppy-org/sloptools/internal/providerdata"
)

type ewsFakeServer struct {
	t         *testing.T
	server    *httptest.Server
	mu        sync.Mutex
	handlers  map[string]func(body []byte) string
	seen      map[string]int
	rawBodies map[string][]string
}

func newEWSFakeServer(t *testing.T) *ewsFakeServer {
	t.Helper()
	fake := &ewsFakeServer{
		t:         t,
		handlers:  map[string]func(body []byte) string{},
		seen:      map[string]int{},
		rawBodies: map[string][]string{},
	}
	fake.server = httptest.NewServer(http.HandlerFunc(fake.handle))
	t.Cleanup(fake.server.Close)
	return fake
}

func (f *ewsFakeServer) handle(w http.ResponseWriter, r *http.Request) {
	action := strings.Trim(r.Header.Get("SOAPAction"), `"`)
	parts := strings.Split(action, "/")
	if len(parts) > 0 {
		action = parts[len(parts)-1]
	}
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()
	f.mu.Lock()
	f.seen[action]++
	f.rawBodies[action] = append(f.rawBodies[action], string(body))
	handler, ok := f.handlers[action]
	f.mu.Unlock()
	if !ok {
		f.t.Fatalf("unexpected SOAPAction %q; body=%s", action, string(body))
	}
	response := handler(body)
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	_, _ = io.WriteString(w, response)
}

func (f *ewsFakeServer) handle2(action string, responder func(body []byte) string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handlers[action] = responder
}

func (f *ewsFakeServer) callCount(action string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.seen[action]
}

func newEWSContactsProvider(t *testing.T, server *ewsFakeServer) *EWSProvider {
	t.Helper()
	client, err := ews.NewClient(ews.Config{Endpoint: server.server.URL, Username: "contacts-ews-" + t.Name(), Password: "secret"})
	if err != nil {
		t.Fatalf("ews.NewClient() error: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return NewEWSProvider(client, "")
}

func findItemContactsResponse(ids ...string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"
               xmlns:m="http://schemas.microsoft.com/exchange/services/2006/messages"
               xmlns:t="http://schemas.microsoft.com/exchange/services/2006/types">
  <soap:Body>
    <m:FindItemResponse>
      <m:ResponseMessages>
        <m:FindItemResponseMessage ResponseClass="Success">
          <m:ResponseCode>NoError</m:ResponseCode>
          <m:RootFolder IndexedPagingOffset="0" TotalItemsInView="`)
	b.WriteString(itoa(len(ids)))
	b.WriteString(`" IncludesLastItemInRange="true">
            <t:Items>`)
	for _, id := range ids {
		b.WriteString(`<t:Contact><t:ItemId Id="`)
		b.WriteString(id)
		b.WriteString(`" ChangeKey="ck-`)
		b.WriteString(id)
		b.WriteString(`" /></t:Contact>`)
	}
	b.WriteString(`</t:Items>
          </m:RootFolder>
        </m:FindItemResponseMessage>
      </m:ResponseMessages>
    </m:FindItemResponse>
  </soap:Body>
</soap:Envelope>`)
	return b.String()
}

func getItemContactResponse(item ews.ContactItem) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"
               xmlns:m="http://schemas.microsoft.com/exchange/services/2006/messages"
               xmlns:t="http://schemas.microsoft.com/exchange/services/2006/types">
  <soap:Body>
    <m:GetItemResponse>
      <m:ResponseMessages>
        <m:GetItemResponseMessage ResponseClass="Success">
          <m:ResponseCode>NoError</m:ResponseCode>
          <m:Items>`)
	b.WriteString(contactXML(item))
	b.WriteString(`</m:Items>
        </m:GetItemResponseMessage>
      </m:ResponseMessages>
    </m:GetItemResponse>
  </soap:Body>
</soap:Envelope>`)
	return b.String()
}

func contactXML(item ews.ContactItem) string {
	var b strings.Builder
	b.WriteString(`<t:Contact>`)
	if item.ID != "" {
		b.WriteString(`<t:ItemId Id="` + item.ID + `" ChangeKey="` + item.ChangeKey + `" />`)
	}
	if item.ParentFolderID != "" {
		b.WriteString(`<t:ParentFolderId Id="` + item.ParentFolderID + `" />`)
	}
	if item.DisplayName != "" {
		b.WriteString(`<t:DisplayName>` + item.DisplayName + `</t:DisplayName>`)
	}
	if item.GivenName != "" {
		b.WriteString(`<t:GivenName>` + item.GivenName + `</t:GivenName>`)
	}
	if item.Surname != "" {
		b.WriteString(`<t:Surname>` + item.Surname + `</t:Surname>`)
	}
	if item.CompanyName != "" {
		b.WriteString(`<t:CompanyName>` + item.CompanyName + `</t:CompanyName>`)
	}
	if len(item.EmailAddresses) > 0 {
		b.WriteString(`<t:EmailAddresses>`)
		for _, entry := range item.EmailAddresses {
			b.WriteString(`<t:Entry Key="` + entry.Key + `">` + entry.Value + `</t:Entry>`)
		}
		b.WriteString(`</t:EmailAddresses>`)
	}
	if len(item.PhoneNumbers) > 0 {
		b.WriteString(`<t:PhoneNumbers>`)
		for _, entry := range item.PhoneNumbers {
			b.WriteString(`<t:Entry Key="` + entry.Key + `">` + entry.Value + `</t:Entry>`)
		}
		b.WriteString(`</t:PhoneNumbers>`)
	}
	if len(item.PhysicalAddresses) > 0 {
		b.WriteString(`<t:PhysicalAddresses>`)
		for _, entry := range item.PhysicalAddresses {
			b.WriteString(`<t:Entry Key="` + entry.Key + `">`)
			if entry.Street != "" {
				b.WriteString(`<t:Street>` + entry.Street + `</t:Street>`)
			}
			if entry.City != "" {
				b.WriteString(`<t:City>` + entry.City + `</t:City>`)
			}
			if entry.State != "" {
				b.WriteString(`<t:State>` + entry.State + `</t:State>`)
			}
			if entry.PostalCode != "" {
				b.WriteString(`<t:PostalCode>` + entry.PostalCode + `</t:PostalCode>`)
			}
			if entry.CountryOrRegion != "" {
				b.WriteString(`<t:CountryOrRegion>` + entry.CountryOrRegion + `</t:CountryOrRegion>`)
			}
			b.WriteString(`</t:Entry>`)
		}
		b.WriteString(`</t:PhysicalAddresses>`)
	}
	b.WriteString(`</t:Contact>`)
	return b.String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func TestEWSProviderListContactsRoundTripsFindAndGet(t *testing.T) {
	server := newEWSFakeServer(t)
	server.handle2("FindItem", func(body []byte) string {
		if !strings.Contains(string(body), `<t:DistinguishedFolderId Id="contacts" />`) {
			t.Fatalf("FindItem missing contacts folder: %s", string(body))
		}
		return findItemContactsResponse("c-1", "c-2")
	})
	server.handle2("GetItem", func(body []byte) string {
		return `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"
               xmlns:m="http://schemas.microsoft.com/exchange/services/2006/messages"
               xmlns:t="http://schemas.microsoft.com/exchange/services/2006/types">
  <soap:Body>
    <m:GetItemResponse>
      <m:ResponseMessages>
        <m:GetItemResponseMessage ResponseClass="Success">
          <m:ResponseCode>NoError</m:ResponseCode>
          <m:Items>
            <t:Contact>
              <t:ItemId Id="c-1" ChangeKey="ck-1" />
              <t:DisplayName>Ada Lovelace</t:DisplayName>
              <t:CompanyName>Analytical Engine</t:CompanyName>
              <t:EmailAddresses>
                <t:Entry Key="EmailAddress1">Ada@Example.com</t:Entry>
              </t:EmailAddresses>
              <t:PhoneNumbers>
                <t:Entry Key="BusinessPhone">+11111</t:Entry>
              </t:PhoneNumbers>
              <t:PhysicalAddresses>
                <t:Entry Key="Home">
                  <t:Street>1 Programmer Lane</t:Street>
                  <t:City>London</t:City>
                  <t:PostalCode>W1</t:PostalCode>
                  <t:CountryOrRegion>UK</t:CountryOrRegion>
                </t:Entry>
              </t:PhysicalAddresses>
              <t:Birthday>1815-12-10T00:00:00Z</t:Birthday>
            </t:Contact>
            <t:Contact>
              <t:ItemId Id="c-2" ChangeKey="ck-2" />
              <t:GivenName>Alan</t:GivenName>
              <t:Surname>Turing</t:Surname>
              <t:EmailAddresses>
                <t:Entry Key="EmailAddress1">alan@example.com</t:Entry>
              </t:EmailAddresses>
            </t:Contact>
          </m:Items>
        </m:GetItemResponseMessage>
      </m:ResponseMessages>
    </m:GetItemResponse>
  </soap:Body>
</soap:Envelope>`
	})
	provider := newEWSContactsProvider(t, server)

	out, err := provider.ListContacts(t.Context())
	if err != nil {
		t.Fatalf("ListContacts() error: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("len = %d, want 2", len(out))
	}
	if out[0].Name != "Ada Lovelace" || out[0].Email != "ada@example.com" {
		t.Fatalf("contact[0] = %+v", out[0])
	}
	if out[0].Organization != "Analytical Engine" {
		t.Fatalf("organization = %q", out[0].Organization)
	}
	if len(out[0].Addresses) != 1 || out[0].Addresses[0].City != "London" || out[0].Addresses[0].Type != "home" {
		t.Fatalf("addresses = %+v", out[0].Addresses)
	}
	if out[0].Birthday == nil || out[0].Birthday.Year() != 1815 {
		t.Fatalf("birthday = %v", out[0].Birthday)
	}
	if out[1].Name != "Alan Turing" {
		t.Fatalf("contact[1].Name = %q, want Alan Turing (combined from given+surname)", out[1].Name)
	}
}

func TestEWSProviderGetContactFetchesSingleItem(t *testing.T) {
	server := newEWSFakeServer(t)
	server.handle2("GetItem", func(body []byte) string {
		if !strings.Contains(string(body), `<t:ItemId Id="contact-1" />`) {
			t.Fatalf("GetItem body = %s", string(body))
		}
		return getItemContactResponse(ews.ContactItem{
			ID:             "contact-1",
			ChangeKey:      "ck-1",
			DisplayName:    "Ada Lovelace",
			EmailAddresses: []ews.ContactEmail{{Key: "EmailAddress1", Value: "ada@example.com"}},
		})
	})
	provider := newEWSContactsProvider(t, server)

	got, err := provider.GetContact(t.Context(), "contact-1")
	if err != nil {
		t.Fatalf("GetContact() error: %v", err)
	}
	if got.ProviderRef != "contact-1" || got.Email != "ada@example.com" {
		t.Fatalf("contact = %+v", got)
	}
}

func TestEWSProviderSearchContactsUsesResolveNames(t *testing.T) {
	server := newEWSFakeServer(t)
	server.handle2("ResolveNames", func(body []byte) string {
		if !strings.Contains(string(body), `<m:UnresolvedEntry>ada</m:UnresolvedEntry>`) {
			t.Fatalf("ResolveNames body = %s", string(body))
		}
		if !strings.Contains(string(body), `ReturnFullContactData="true"`) {
			t.Fatalf("ResolveNames body missing ReturnFullContactData=true: %s", string(body))
		}
		return `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"
               xmlns:m="http://schemas.microsoft.com/exchange/services/2006/messages"
               xmlns:t="http://schemas.microsoft.com/exchange/services/2006/types">
  <soap:Body>
    <m:ResolveNamesResponse>
      <m:ResponseMessages>
        <m:ResolveNamesResponseMessage ResponseClass="Success">
          <m:ResponseCode>NoError</m:ResponseCode>
          <m:ResolutionSet TotalItemsInView="1" IncludesLastItemInRange="true">
            <t:Resolution>
              <t:Mailbox>
                <t:Name>Ada Lovelace</t:Name>
                <t:EmailAddress>ada@example.com</t:EmailAddress>
              </t:Mailbox>
              <t:Contact>
                <t:DisplayName>Ada Lovelace</t:DisplayName>
                <t:CompanyName>Analytical Engine</t:CompanyName>
              </t:Contact>
            </t:Resolution>
          </m:ResolutionSet>
        </m:ResolveNamesResponseMessage>
      </m:ResponseMessages>
    </m:ResolveNamesResponse>
  </soap:Body>
</soap:Envelope>`
	})
	provider := newEWSContactsProvider(t, server)

	out, err := provider.SearchContacts(t.Context(), "ada")
	if err != nil {
		t.Fatalf("SearchContacts() error: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("len = %d, want 1", len(out))
	}
	if out[0].Email != "ada@example.com" || out[0].Organization != "Analytical Engine" {
		t.Fatalf("contact = %+v", out[0])
	}
}

func TestEWSProviderGetContactPropagatesEmptyID(t *testing.T) {
	server := newEWSFakeServer(t)
	provider := newEWSContactsProvider(t, server)
	if _, err := provider.GetContact(t.Context(), "   "); err == nil {
		t.Fatal("GetContact() error = nil, want missing-id error")
	}
}

func TestEWSProviderNilClientErrors(t *testing.T) {
	provider := &EWSProvider{}
	if _, err := provider.ListContacts(t.Context()); err == nil {
		t.Fatal("ListContacts with nil client: error = nil")
	}
	if _, err := provider.GetContact(t.Context(), "x"); err == nil {
		t.Fatal("GetContact with nil client: error = nil")
	}
	if _, err := provider.SearchContacts(t.Context(), "x"); err == nil {
		t.Fatal("SearchContacts with nil client: error = nil")
	}
	if _, err := provider.CreateContact(t.Context(), providerdata.Contact{Name: "x"}); err == nil {
		t.Fatal("CreateContact with nil client: error = nil")
	}
	if _, err := provider.UpdateContact(t.Context(), providerdata.Contact{ProviderRef: "x"}); err == nil {
		t.Fatal("UpdateContact with nil client: error = nil")
	}
	if err := provider.DeleteContact(t.Context(), "x"); err == nil {
		t.Fatal("DeleteContact with nil client: error = nil")
	}
}
