package ews

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientCreateContactItemBuildsSOAPAndParsesItemID(t *testing.T) {
	var (
		body       string
		soapAction string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		soapAction = r.Header.Get("SOAPAction")
		raw, _ := io.ReadAll(r.Body)
		r.Body.Close()
		body = string(raw)
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"
               xmlns:m="http://schemas.microsoft.com/exchange/services/2006/messages"
               xmlns:t="http://schemas.microsoft.com/exchange/services/2006/types">
  <soap:Body>
    <m:CreateItemResponse>
      <m:ResponseMessages>
        <m:CreateItemResponseMessage ResponseClass="Success">
          <m:ResponseCode>NoError</m:ResponseCode>
          <m:Items>
            <t:Contact>
              <t:ItemId Id="contact-1" ChangeKey="ck-1" />
              <t:DisplayName>Ada Lovelace</t:DisplayName>
            </t:Contact>
          </m:Items>
        </m:CreateItemResponseMessage>
      </m:ResponseMessages>
    </m:CreateItemResponse>
  </soap:Body>
</soap:Envelope>`)
	}))
	defer server.Close()
	client, err := NewClient(Config{Endpoint: server.URL, Username: "ews-contacts-1", Password: "secret"})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer client.Close()

	birthday := time.Date(1815, 12, 10, 0, 0, 0, 0, time.UTC)
	itemID, changeKey, err := client.CreateContactItem(t.Context(), "", ContactItem{
		DisplayName: "Ada Lovelace",
		GivenName:   "Ada",
		Surname:     "Lovelace",
		CompanyName: "Analytical Engine",
		JobTitle:    "Mathematician",
		Department:  "R&D",
		Notes:       "Portrait on the £50 note",
		EmailAddresses: []ContactEmail{
			{Key: "EmailAddress1", Value: "ada@example.com"},
		},
		PhoneNumbers: []ContactPhone{
			{Key: "BusinessPhone", Value: "+11111"},
			{Key: "MobilePhone", Value: "+22222"},
		},
		PhysicalAddresses: []ContactPhysicalAddress{
			{Key: "Home", Street: "1 Programmer Lane", City: "London", PostalCode: "W1", CountryOrRegion: "UK"},
		},
		Birthday: &birthday,
	})
	if err != nil {
		t.Fatalf("CreateContactItem() error: %v", err)
	}
	if itemID != "contact-1" || changeKey != "ck-1" {
		t.Fatalf("itemID=%q changeKey=%q, want contact-1/ck-1", itemID, changeKey)
	}
	if !strings.Contains(soapAction, "CreateItem") {
		t.Fatalf("SOAPAction = %q, want CreateItem", soapAction)
	}
	for _, snippet := range []string{
		`<m:CreateItem>`,
		`<t:DistinguishedFolderId Id="contacts" />`,
		`<t:Contact>`,
		`<t:DisplayName>Ada Lovelace</t:DisplayName>`,
		`<t:GivenName>Ada</t:GivenName>`,
		`<t:CompanyName>Analytical Engine</t:CompanyName>`,
		`<t:EmailAddresses>`,
		`<t:Entry Key="EmailAddress1">ada@example.com</t:Entry>`,
		`<t:PhoneNumbers>`,
		`<t:Entry Key="BusinessPhone">+11111</t:Entry>`,
		`<t:Entry Key="MobilePhone">+22222</t:Entry>`,
		`<t:PhysicalAddresses>`,
		`<t:Entry Key="Home">`,
		`<t:Street>1 Programmer Lane</t:Street>`,
		`<t:City>London</t:City>`,
		`<t:PostalCode>W1</t:PostalCode>`,
		`<t:CountryOrRegion>UK</t:CountryOrRegion>`,
		`<t:Birthday>1815-12-10T00:00:00Z</t:Birthday>`,
		`<t:Department>R&amp;D</t:Department>`,
		`<t:JobTitle>Mathematician</t:JobTitle>`,
		`<t:Surname>Lovelace</t:Surname>`,
		`<t:Body BodyType="Text">Portrait on the £50 note</t:Body>`,
	} {
		if !strings.Contains(body, snippet) {
			t.Fatalf("request body missing %q:\n%s", snippet, body)
		}
	}
}

func TestClientCreateContactItemHonoursExplicitFolder(t *testing.T) {
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		r.Body.Close()
		body = string(raw)
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = io.WriteString(w, successCreateContactResponse("contact-42", "ck-42"))
	}))
	defer server.Close()
	client, err := NewClient(Config{Endpoint: server.URL, Username: "ews-contacts-2", Password: "secret"})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer client.Close()

	if _, _, err := client.CreateContactItem(t.Context(), "folder-AA", ContactItem{DisplayName: "Generic"}); err != nil {
		t.Fatalf("CreateContactItem() error: %v", err)
	}
	if !strings.Contains(body, `<t:FolderId Id="folder-AA" />`) {
		t.Fatalf("request body missing folder id:\n%s", body)
	}
}

func TestClientUpdateContactItemBuildsSetAndDeleteFields(t *testing.T) {
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
    <m:UpdateItemResponse>
      <m:ResponseMessages>
        <m:UpdateItemResponseMessage ResponseClass="Success">
          <m:ResponseCode>NoError</m:ResponseCode>
          <m:Items>
            <t:Contact>
              <t:ItemId Id="contact-1" ChangeKey="ck-2" />
            </t:Contact>
          </m:Items>
        </m:UpdateItemResponseMessage>
      </m:ResponseMessages>
    </m:UpdateItemResponse>
  </soap:Body>
</soap:Envelope>`)
	}))
	defer server.Close()
	client, err := NewClient(Config{Endpoint: server.URL, Username: "ews-contacts-3", Password: "secret"})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer client.Close()

	updatedEmails := []ContactEmail{{Key: "EmailAddress1", Value: "ada@new.example"}}
	emptyPhones := []ContactPhone{}
	name := "Ada Lovelace (updated)"
	notes := "  " // whitespace-only notes should clear the Body field
	updates := ContactUpdate{
		DisplayName:    &name,
		Notes:          &notes,
		EmailAddresses: &updatedEmails,
		PhoneNumbers:   &emptyPhones,
		ClearBirthday:  true,
	}
	newChangeKey, err := client.UpdateContactItem(t.Context(), "contact-1", "ck-1", updates)
	if err != nil {
		t.Fatalf("UpdateContactItem() error: %v", err)
	}
	if newChangeKey != "ck-2" {
		t.Fatalf("new ChangeKey = %q, want ck-2", newChangeKey)
	}
	for _, snippet := range []string{
		`<m:UpdateItem MessageDisposition="SaveOnly" ConflictResolution="AlwaysOverwrite">`,
		`<t:ItemId Id="contact-1" ChangeKey="ck-1" />`,
		`<t:SetItemField><t:FieldURI FieldURI="contacts:DisplayName" /><t:Contact><t:DisplayName>Ada Lovelace (updated)</t:DisplayName></t:Contact></t:SetItemField>`,
		`<t:DeleteItemField><t:FieldURI FieldURI="item:Body" /></t:DeleteItemField>`,
		`<t:DeleteItemField><t:FieldURI FieldURI="contacts:Birthday" /></t:DeleteItemField>`,
		`<t:SetItemField><t:FieldURI FieldURI="contacts:EmailAddresses" /><t:Contact><t:EmailAddresses><t:Entry Key="EmailAddress1">ada@new.example</t:Entry></t:EmailAddresses></t:Contact></t:SetItemField>`,
		`<t:DeleteItemField><t:FieldURI FieldURI="contacts:PhoneNumbers" /></t:DeleteItemField>`,
	} {
		if !strings.Contains(body, snippet) {
			t.Fatalf("request body missing %q:\n%s", snippet, body)
		}
	}
}

func TestClientUpdateContactItemNoChangesReturnsOriginalKey(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
	}))
	defer server.Close()
	client, err := NewClient(Config{Endpoint: server.URL, Username: "ews-contacts-noop", Password: "secret"})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer client.Close()

	got, err := client.UpdateContactItem(t.Context(), "contact-1", "ck-orig", ContactUpdate{})
	if err != nil {
		t.Fatalf("UpdateContactItem() error: %v", err)
	}
	if got != "ck-orig" {
		t.Fatalf("change key = %q, want ck-orig", got)
	}
	if calls != 0 {
		t.Fatalf("server calls = %d, want 0 (no SOAP request on empty update)", calls)
	}
}

func TestClientDeleteContactItemIssuesHardDelete(t *testing.T) {
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		r.Body.Close()
		body = string(raw)
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"
               xmlns:m="http://schemas.microsoft.com/exchange/services/2006/messages">
  <soap:Body>
    <m:DeleteItemResponse>
      <m:ResponseMessages>
        <m:DeleteItemResponseMessage ResponseClass="Success">
          <m:ResponseCode>NoError</m:ResponseCode>
        </m:DeleteItemResponseMessage>
      </m:ResponseMessages>
    </m:DeleteItemResponse>
  </soap:Body>
</soap:Envelope>`)
	}))
	defer server.Close()
	client, err := NewClient(Config{Endpoint: server.URL, Username: "ews-contacts-4", Password: "secret"})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer client.Close()

	if err := client.DeleteContactItem(t.Context(), "contact-1"); err != nil {
		t.Fatalf("DeleteContactItem() error: %v", err)
	}
	for _, snippet := range []string{
		`<m:DeleteItem DeleteType="HardDelete"`,
		`<t:ItemId Id="contact-1" />`,
	} {
		if !strings.Contains(body, snippet) {
			t.Fatalf("request body missing %q:\n%s", snippet, body)
		}
	}
}

func TestClientDeleteContactItemRejectsEmptyID(t *testing.T) {
	client, err := NewClient(Config{Endpoint: "https://example.invalid", Username: "ews-contacts-5", Password: "secret"})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer client.Close()
	if err := client.DeleteContactItem(t.Context(), "  "); err == nil {
		t.Fatal("DeleteContactItem() error = nil, want missing-id error")
	}
}

func successCreateContactResponse(itemID, changeKey string) string {
	return `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"
               xmlns:m="http://schemas.microsoft.com/exchange/services/2006/messages"
               xmlns:t="http://schemas.microsoft.com/exchange/services/2006/types">
  <soap:Body>
    <m:CreateItemResponse>
      <m:ResponseMessages>
        <m:CreateItemResponseMessage ResponseClass="Success">
          <m:ResponseCode>NoError</m:ResponseCode>
          <m:Items>
            <t:Contact>
              <t:ItemId Id="` + itemID + `" ChangeKey="` + changeKey + `" />
            </t:Contact>
          </m:Items>
        </m:CreateItemResponseMessage>
      </m:ResponseMessages>
    </m:CreateItemResponse>
  </soap:Body>
</soap:Envelope>`
}
