package contacts

import (
	"strings"
	"testing"

	"github.com/sloppy-org/sloptools/internal/ews"
	"github.com/sloppy-org/sloptools/internal/providerdata"
)

func TestEWSProviderCreateContactSendsFullItem(t *testing.T) {
	server := newEWSFakeServer(t)
	server.handle2("CreateItem", func(body []byte) string {
		for _, snippet := range []string{
			`<t:DistinguishedFolderId Id="contacts" />`,
			`<t:DisplayName>Ada Lovelace</t:DisplayName>`,
			`<t:CompanyName>Analytical Engine</t:CompanyName>`,
			`<t:Entry Key="EmailAddress1">ada@example.com</t:Entry>`,
			`<t:Entry Key="BusinessPhone">+11111</t:Entry>`,
			`<t:Entry Key="Home">`,
		} {
			if !strings.Contains(string(body), snippet) {
				t.Fatalf("CreateItem body missing %q:\n%s", snippet, string(body))
			}
		}
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
              <t:ItemId Id="contact-new" ChangeKey="ck-new" />
            </t:Contact>
          </m:Items>
        </m:CreateItemResponseMessage>
      </m:ResponseMessages>
    </m:CreateItemResponse>
  </soap:Body>
</soap:Envelope>`
	})
	provider := newEWSContactsProvider(t, server)

	input := providerdata.Contact{
		Name:         "Ada Lovelace",
		Email:        "ada@example.com",
		Organization: "Analytical Engine",
		Phones:       []string{"+11111"},
		Addresses: []providerdata.PostalAddress{
			{Type: "home", Street: "1 Programmer Lane", City: "London", Postal: "W1", Country: "UK"},
		},
	}
	created, err := provider.CreateContact(t.Context(), input)
	if err != nil {
		t.Fatalf("CreateContact() error: %v", err)
	}
	if created.ProviderRef != "contact-new" {
		t.Fatalf("ProviderRef = %q, want contact-new", created.ProviderRef)
	}
}

func TestEWSProviderUpdateContactFetchesChangeKeyAndPatches(t *testing.T) {
	server := newEWSFakeServer(t)
	getCalls := 0
	server.handle2("GetItem", func(body []byte) string {
		getCalls++
		switch getCalls {
		case 1:
			return getItemContactResponse(ews.ContactItem{
				ID: "contact-1", ChangeKey: "ck-original",
				DisplayName:    "Ada Lovelace",
				EmailAddresses: []ews.ContactEmail{{Key: "EmailAddress1", Value: "ada@example.com"}},
			})
		case 2:
			return getItemContactResponse(ews.ContactItem{
				ID: "contact-1", ChangeKey: "ck-updated",
				DisplayName:    "Ada Lovelace (updated)",
				CompanyName:    "Analytical Engine",
				EmailAddresses: []ews.ContactEmail{{Key: "EmailAddress1", Value: "ada@new.example"}},
			})
		default:
			t.Fatalf("unexpected GetItem call %d", getCalls)
			return ""
		}
	})
	var updateBody string
	server.handle2("UpdateItem", func(body []byte) string {
		updateBody = string(body)
		return `<?xml version="1.0" encoding="utf-8"?>
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
              <t:ItemId Id="contact-1" ChangeKey="ck-updated" />
            </t:Contact>
          </m:Items>
        </m:UpdateItemResponseMessage>
      </m:ResponseMessages>
    </m:UpdateItemResponse>
  </soap:Body>
</soap:Envelope>`
	})
	provider := newEWSContactsProvider(t, server)

	input := providerdata.Contact{
		ProviderRef:  "contact-1",
		Name:         "Ada Lovelace (updated)",
		Organization: "Analytical Engine",
		Email:        "ada@new.example",
	}
	updated, err := provider.UpdateContact(t.Context(), input)
	if err != nil {
		t.Fatalf("UpdateContact() error: %v", err)
	}
	if updated.Email != "ada@new.example" {
		t.Fatalf("email = %q", updated.Email)
	}
	if !strings.Contains(updateBody, `ChangeKey="ck-original"`) {
		t.Fatalf("UpdateItem missing original ChangeKey: %s", updateBody)
	}
	if server.callCount("UpdateItem") != 1 {
		t.Fatalf("UpdateItem calls = %d", server.callCount("UpdateItem"))
	}
}

func TestEWSProviderUpdateContactRequiresProviderRef(t *testing.T) {
	server := newEWSFakeServer(t)
	provider := newEWSContactsProvider(t, server)
	if _, err := provider.UpdateContact(t.Context(), providerdata.Contact{Name: "No Ref"}); err == nil {
		t.Fatal("UpdateContact() error = nil, want provider_ref error")
	}
}

func TestEWSProviderDeleteContactSendsHardDelete(t *testing.T) {
	server := newEWSFakeServer(t)
	server.handle2("DeleteItem", func(body []byte) string {
		if !strings.Contains(string(body), `<t:ItemId Id="contact-1" />`) {
			t.Fatalf("DeleteItem body = %s", string(body))
		}
		if !strings.Contains(string(body), `DeleteType="HardDelete"`) {
			t.Fatalf("DeleteItem not HardDelete: %s", string(body))
		}
		return `<?xml version="1.0" encoding="utf-8"?>
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
</soap:Envelope>`
	})
	provider := newEWSContactsProvider(t, server)

	if err := provider.DeleteContact(t.Context(), "contact-1"); err != nil {
		t.Fatalf("DeleteContact() error: %v", err)
	}
}

func TestEWSProviderRoundTripsListCreateUpdateDelete(t *testing.T) {
	server := newEWSFakeServer(t)

	storage := map[string]ews.ContactItem{}
	versions := map[string]int{}
	var nextID int

	server.handle2("FindItem", func(body []byte) string {
		ids := make([]string, 0, len(storage))
		for id := range storage {
			ids = append(ids, id)
		}
		return findItemContactsResponse(ids...)
	})
	server.handle2("GetItem", func(body []byte) string {
		var id string
		for key := range storage {
			if strings.Contains(string(body), `Id="`+key+`"`) {
				id = key
				break
			}
		}
		if id == "" && len(storage) > 0 {
			return getItemContactResponse(ews.ContactItem{})
		}
		item := storage[id]
		return getItemContactResponse(item)
	})
	server.handle2("CreateItem", func(body []byte) string {
		nextID++
		id := "c-" + itoa(nextID)
		versions[id]++
		ck := "ck-" + id + "-" + itoa(versions[id])
		storage[id] = ews.ContactItem{
			ID:             id,
			ChangeKey:      ck,
			DisplayName:    "Ada Lovelace",
			CompanyName:    "Analytical Engine",
			EmailAddresses: []ews.ContactEmail{{Key: "EmailAddress1", Value: "ada@example.com"}},
		}
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
              <t:ItemId Id="` + id + `" ChangeKey="` + ck + `" />
            </t:Contact>
          </m:Items>
        </m:CreateItemResponseMessage>
      </m:ResponseMessages>
    </m:CreateItemResponse>
  </soap:Body>
</soap:Envelope>`
	})
	server.handle2("UpdateItem", func(body []byte) string {
		for id := range storage {
			if !strings.Contains(string(body), `Id="`+id+`"`) {
				continue
			}
			versions[id]++
			newKey := "ck-" + id + "-" + itoa(versions[id])
			item := storage[id]
			item.ChangeKey = newKey
			item.DisplayName = "Ada Lovelace (updated)"
			item.CompanyName = "Analytical Engine"
			item.EmailAddresses = []ews.ContactEmail{{Key: "EmailAddress1", Value: "ada@new.example"}}
			storage[id] = item
			return `<?xml version="1.0" encoding="utf-8"?>
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
              <t:ItemId Id="` + id + `" ChangeKey="` + newKey + `" />
            </t:Contact>
          </m:Items>
        </m:UpdateItemResponseMessage>
      </m:ResponseMessages>
    </m:UpdateItemResponse>
  </soap:Body>
</soap:Envelope>`
		}
		t.Fatalf("UpdateItem for unknown contact: %s", string(body))
		return ""
	})
	server.handle2("DeleteItem", func(body []byte) string {
		for id := range storage {
			if strings.Contains(string(body), `Id="`+id+`"`) {
				delete(storage, id)
				break
			}
		}
		return `<?xml version="1.0" encoding="utf-8"?>
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
</soap:Envelope>`
	})

	provider := newEWSContactsProvider(t, server)

	created, err := provider.CreateContact(t.Context(), providerdata.Contact{
		Name:         "Ada Lovelace",
		Email:        "ada@example.com",
		Organization: "Analytical Engine",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if listed, err := provider.ListContacts(t.Context()); err != nil {
		t.Fatalf("list-1: %v", err)
	} else if len(listed) != 1 {
		t.Fatalf("after create: %d contacts, want 1", len(listed))
	}
	updated, err := provider.UpdateContact(t.Context(), providerdata.Contact{
		ProviderRef:  created.ProviderRef,
		Name:         "Ada Lovelace (updated)",
		Organization: "Analytical Engine",
		Email:        "ada@new.example",
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Email != "ada@new.example" {
		t.Fatalf("updated email = %q", updated.Email)
	}
	if err := provider.DeleteContact(t.Context(), created.ProviderRef); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if listed, err := provider.ListContacts(t.Context()); err != nil {
		t.Fatalf("list-2: %v", err)
	} else if len(listed) != 0 {
		t.Fatalf("after delete: %d contacts, want 0", len(listed))
	}
}
