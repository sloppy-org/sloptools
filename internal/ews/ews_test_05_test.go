package ews

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientGetContactItemParsesFullPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>
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
              <t:ItemId Id="contact-99" ChangeKey="ck-99" />
              <t:ParentFolderId Id="folder-contacts" />
              <t:Body BodyType="Text">Notes about Ada</t:Body>
              <t:DisplayName>Ada Lovelace</t:DisplayName>
              <t:GivenName>Ada</t:GivenName>
              <t:CompanyName>Analytical Engine</t:CompanyName>
              <t:EmailAddresses>
                <t:Entry Key="EmailAddress1">ada@example.com</t:Entry>
                <t:Entry Key="EmailAddress2"></t:Entry>
              </t:EmailAddresses>
              <t:PhysicalAddresses>
                <t:Entry Key="Home">
                  <t:Street>1 Programmer Lane</t:Street>
                  <t:City>London</t:City>
                  <t:PostalCode>W1</t:PostalCode>
                  <t:CountryOrRegion>UK</t:CountryOrRegion>
                </t:Entry>
              </t:PhysicalAddresses>
              <t:PhoneNumbers>
                <t:Entry Key="BusinessPhone">+11111</t:Entry>
                <t:Entry Key="MobilePhone">+22222</t:Entry>
              </t:PhoneNumbers>
              <t:Birthday>1815-12-10T00:00:00Z</t:Birthday>
              <t:JobTitle>Mathematician</t:JobTitle>
              <t:Surname>Lovelace</t:Surname>
            </t:Contact>
          </m:Items>
        </m:GetItemResponseMessage>
      </m:ResponseMessages>
    </m:GetItemResponse>
  </soap:Body>
</soap:Envelope>`)
	}))
	defer server.Close()
	client, err := NewClient(Config{Endpoint: server.URL, Username: "ews-contacts-6", Password: "secret"})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer client.Close()

	got, err := client.GetContactItem(t.Context(), "contact-99")
	if err != nil {
		t.Fatalf("GetContactItem() error: %v", err)
	}
	if got.ID != "contact-99" || got.ChangeKey != "ck-99" {
		t.Fatalf("ID/ChangeKey = %q/%q", got.ID, got.ChangeKey)
	}
	if got.DisplayName != "Ada Lovelace" || got.GivenName != "Ada" || got.Surname != "Lovelace" {
		t.Fatalf("name fields = %+v", got)
	}
	if got.CompanyName != "Analytical Engine" || got.JobTitle != "Mathematician" {
		t.Fatalf("org fields = %+v", got)
	}
	if got.Notes != "Notes about Ada" {
		t.Fatalf("notes = %q", got.Notes)
	}
	if len(got.EmailAddresses) != 1 || got.EmailAddresses[0].Value != "ada@example.com" {
		t.Fatalf("emails = %#v", got.EmailAddresses)
	}
	if len(got.PhoneNumbers) != 2 {
		t.Fatalf("phones = %#v", got.PhoneNumbers)
	}
	if len(got.PhysicalAddresses) != 1 || got.PhysicalAddresses[0].City != "London" {
		t.Fatalf("addresses = %#v", got.PhysicalAddresses)
	}
	if got.Birthday == nil || got.Birthday.Year() != 1815 {
		t.Fatalf("birthday = %v", got.Birthday)
	}
}

func TestClientResolveNamesParsesResolutionSet(t *testing.T) {
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		r.Body.Close()
		body = string(raw)
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"
               xmlns:m="http://schemas.microsoft.com/exchange/services/2006/messages"
               xmlns:t="http://schemas.microsoft.com/exchange/services/2006/types">
  <soap:Body>
    <m:ResolveNamesResponse>
      <m:ResponseMessages>
        <m:ResolveNamesResponseMessage ResponseClass="Success">
          <m:ResponseCode>NoError</m:ResponseCode>
          <m:ResolutionSet TotalItemsInView="2" IncludesLastItemInRange="true">
            <t:Resolution>
              <t:Mailbox>
                <t:Name>Ada Lovelace</t:Name>
                <t:EmailAddress>ada@example.com</t:EmailAddress>
                <t:RoutingType>SMTP</t:RoutingType>
                <t:MailboxType>Mailbox</t:MailboxType>
              </t:Mailbox>
              <t:Contact>
                <t:DisplayName>Ada Lovelace</t:DisplayName>
                <t:EmailAddresses>
                  <t:Entry Key="EmailAddress1">ada@example.com</t:Entry>
                </t:EmailAddresses>
                <t:CompanyName>Analytical Engine</t:CompanyName>
              </t:Contact>
            </t:Resolution>
            <t:Resolution>
              <t:Mailbox>
                <t:Name>Adam Smith</t:Name>
                <t:EmailAddress>adam@example.com</t:EmailAddress>
                <t:RoutingType>SMTP</t:RoutingType>
                <t:MailboxType>Mailbox</t:MailboxType>
              </t:Mailbox>
            </t:Resolution>
          </m:ResolutionSet>
        </m:ResolveNamesResponseMessage>
      </m:ResponseMessages>
    </m:ResolveNamesResponse>
  </soap:Body>
</soap:Envelope>`)
	}))
	defer server.Close()
	client, err := NewClient(Config{Endpoint: server.URL, Username: "ews-contacts-7", Password: "secret"})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer client.Close()

	resolved, err := client.ResolveNames(t.Context(), "ada", true)
	if err != nil {
		t.Fatalf("ResolveNames() error: %v", err)
	}
	if len(resolved) != 2 {
		t.Fatalf("resolved = %d, want 2", len(resolved))
	}
	if resolved[0].Mailbox.Email != "ada@example.com" {
		t.Fatalf("mailbox[0] email = %q", resolved[0].Mailbox.Email)
	}
	if resolved[0].Contact.DisplayName != "Ada Lovelace" {
		t.Fatalf("contact[0] name = %q", resolved[0].Contact.DisplayName)
	}
	if len(resolved[0].Contact.EmailAddresses) != 1 {
		t.Fatalf("contact[0] emails = %#v", resolved[0].Contact.EmailAddresses)
	}
	if resolved[1].Contact.DisplayName != "" {
		t.Fatalf("contact[1] should be empty without Contact element, got %q", resolved[1].Contact.DisplayName)
	}
	for _, snippet := range []string{
		`<m:ResolveNames ReturnFullContactData="true"`,
		`<m:UnresolvedEntry>ada</m:UnresolvedEntry>`,
	} {
		if !strings.Contains(body, snippet) {
			t.Fatalf("request body missing %q:\n%s", snippet, body)
		}
	}
}

func TestClientResolveNamesSkipsEmptyQuery(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
	}))
	defer server.Close()
	client, err := NewClient(Config{Endpoint: server.URL, Username: "ews-contacts-empty", Password: "secret"})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer client.Close()

	resolved, err := client.ResolveNames(t.Context(), "  ", false)
	if err != nil {
		t.Fatalf("ResolveNames(empty) error: %v", err)
	}
	if len(resolved) != 0 {
		t.Fatalf("resolved = %d, want 0", len(resolved))
	}
	if calls != 0 {
		t.Fatalf("server calls = %d, want 0", calls)
	}
}

func TestClientResolveNamesSurfacesErrorResponseCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"
               xmlns:m="http://schemas.microsoft.com/exchange/services/2006/messages">
  <soap:Body>
    <m:ResolveNamesResponse>
      <m:ResponseMessages>
        <m:ResolveNamesResponseMessage ResponseClass="Error">
          <m:MessageText>No matches found.</m:MessageText>
          <m:ResponseCode>ErrorNameResolutionNoResults</m:ResponseCode>
        </m:ResolveNamesResponseMessage>
      </m:ResponseMessages>
    </m:ResolveNamesResponse>
  </soap:Body>
</soap:Envelope>`)
	}))
	defer server.Close()
	client, err := NewClient(Config{Endpoint: server.URL, Username: "ews-contacts-err", Password: "secret"})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer client.Close()

	if _, err := client.ResolveNames(t.Context(), "nobody", true); err == nil {
		t.Fatal("ResolveNames() error = nil, want ErrorNameResolutionNoResults")
	} else if !strings.Contains(err.Error(), "ErrorNameResolutionNoResults") {
		t.Fatalf("error = %v, want ErrorNameResolutionNoResults", err)
	}
}

func TestClientListContactItemsPagesFromFindAndGet(t *testing.T) {
	var soapActions []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		action := strings.Trim(r.Header.Get("SOAPAction"), `"`)
		soapActions = append(soapActions, action)
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		switch {
		case strings.HasSuffix(action, "/FindItem"):
			_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"
               xmlns:m="http://schemas.microsoft.com/exchange/services/2006/messages"
               xmlns:t="http://schemas.microsoft.com/exchange/services/2006/types">
  <soap:Body>
    <m:FindItemResponse>
      <m:ResponseMessages>
        <m:FindItemResponseMessage ResponseClass="Success">
          <m:ResponseCode>NoError</m:ResponseCode>
          <m:RootFolder IndexedPagingOffset="0" TotalItemsInView="1" IncludesLastItemInRange="true">
            <t:Items>
              <t:Contact>
                <t:ItemId Id="contact-1" ChangeKey="ck-1" />
              </t:Contact>
            </t:Items>
          </m:RootFolder>
        </m:FindItemResponseMessage>
      </m:ResponseMessages>
    </m:FindItemResponse>
  </soap:Body>
</soap:Envelope>`)
		case strings.HasSuffix(action, "/GetItem"):
			_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>
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
              <t:ItemId Id="contact-1" ChangeKey="ck-1" />
              <t:DisplayName>Ada Lovelace</t:DisplayName>
              <t:EmailAddresses>
                <t:Entry Key="EmailAddress1">ada@example.com</t:Entry>
              </t:EmailAddresses>
            </t:Contact>
          </m:Items>
        </m:GetItemResponseMessage>
      </m:ResponseMessages>
    </m:GetItemResponse>
  </soap:Body>
</soap:Envelope>`)
		default:
			t.Fatalf("unexpected SOAPAction %q", action)
		}
	}))
	defer server.Close()
	client, err := NewClient(Config{Endpoint: server.URL, Username: "ews-contacts-8", Password: "secret"})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer client.Close()

	list, err := client.ListContactItems(t.Context(), "", 0, 25)
	if err != nil {
		t.Fatalf("ListContactItems() error: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("items = %d, want 1", len(list))
	}
	if list[0].DisplayName != "Ada Lovelace" || len(list[0].EmailAddresses) != 1 {
		t.Fatalf("contact = %+v", list[0])
	}
	if len(soapActions) != 2 {
		t.Fatalf("soap calls = %d (%v), want FindItem + GetItem", len(soapActions), soapActions)
	}
}
