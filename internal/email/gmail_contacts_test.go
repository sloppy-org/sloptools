package email

import (
	"testing"

	people "google.golang.org/api/people/v1"
)

func TestParseGoogleContact(t *testing.T) {
	contact := parseGoogleContact(&people.Person{
		ResourceName: "people/c123",
		Names: []*people.Name{{
			DisplayName: "Ada Lovelace",
		}},
		EmailAddresses: []*people.EmailAddress{{
			Value: "Ada@example.com",
		}},
		Organizations: []*people.Organization{{
			Name: "Analytical Engines",
		}},
		PhoneNumbers: []*people.PhoneNumber{{
			Value: "+1 555 0100",
		}},
	})
	if contact == nil {
		t.Fatal("parseGoogleContact() = nil, want contact")
	}
	if contact.Email != "ada@example.com" {
		t.Fatalf("contact.Email = %q, want ada@example.com", contact.Email)
	}
	if contact.Organization != "Analytical Engines" {
		t.Fatalf("contact.Organization = %q", contact.Organization)
	}
	if len(contact.Phones) != 1 || contact.Phones[0] != "+1 555 0100" {
		t.Fatalf("contact.Phones = %#v", contact.Phones)
	}
}
