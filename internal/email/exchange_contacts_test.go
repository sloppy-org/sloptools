package email

import "testing"

func TestParseExchangeContact(t *testing.T) {
	contact := parseExchangeContact(exchangeContact{
		ID:          "contact-1",
		DisplayName: "Carol Example",
		CompanyName: "Example Corp",
		EmailAddresses: []exchangeAddress{{
			Address: "Carol@example.com",
		}},
		BusinessPhones: []string{"+1 555 0110"},
	})
	if contact == nil {
		t.Fatal("parseExchangeContact() = nil, want contact")
	}
	if contact.Email != "carol@example.com" {
		t.Fatalf("contact.Email = %q, want carol@example.com", contact.Email)
	}
	if contact.ProviderRef != "contact-1" {
		t.Fatalf("contact.ProviderRef = %q, want contact-1", contact.ProviderRef)
	}
}
