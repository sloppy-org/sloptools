package email

import (
	"context"
	"fmt"
	"strings"

	"github.com/sloppy-org/sloptools/internal/providerdata"
	"google.golang.org/api/option"
	people "google.golang.org/api/people/v1"
)

func (c *GmailClient) ListContacts(ctx context.Context) ([]providerdata.Contact, error) {
	tokenSource, err := c.getTokenSource(ctx)
	if err != nil {
		return nil, err
	}
	service, err := people.NewService(ctx, option.WithTokenSource(tokenSource))
	if err != nil {
		return nil, fmt.Errorf("failed to initialize Google contacts service: %w", err)
	}
	var out []providerdata.Contact
	pageToken := ""
	for {
		call := service.People.Connections.List("people/me").
			Context(ctx).
			PageSize(1000).
			PersonFields("names,emailAddresses,organizations,phoneNumbers")
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		result, err := call.Do()
		if err != nil {
			return nil, fmt.Errorf("failed to list Google contacts: %w", err)
		}
		for _, person := range result.Connections {
			contact := parseGoogleContact(person)
			if contact == nil {
				continue
			}
			out = append(out, *contact)
		}
		if strings.TrimSpace(result.NextPageToken) == "" {
			break
		}
		pageToken = result.NextPageToken
	}
	return out, nil
}

func parseGoogleContact(person *people.Person) *providerdata.Contact {
	if person == nil {
		return nil
	}
	emailAddress := firstGoogleEmail(person.EmailAddresses)
	name := firstGoogleName(person.Names)
	if name == "" {
		name = emailAddress
	}
	if name == "" && emailAddress == "" {
		return nil
	}
	return &providerdata.Contact{
		ProviderRef:  strings.TrimSpace(person.ResourceName),
		Name:         name,
		Email:        emailAddress,
		Organization: firstGoogleOrganization(person.Organizations),
		Phones:       allGooglePhones(person.PhoneNumbers),
	}
}

func firstGoogleEmail(values []*people.EmailAddress) string {
	for _, value := range values {
		if value == nil {
			continue
		}
		if emailAddress := strings.ToLower(strings.TrimSpace(value.Value)); emailAddress != "" {
			return emailAddress
		}
	}
	return ""
}

func firstGoogleName(values []*people.Name) string {
	for _, value := range values {
		if value == nil {
			continue
		}
		if name := strings.TrimSpace(value.DisplayName); name != "" {
			return name
		}
	}
	return ""
}

func firstGoogleOrganization(values []*people.Organization) string {
	for _, value := range values {
		if value == nil {
			continue
		}
		if name := strings.TrimSpace(value.Name); name != "" {
			return name
		}
	}
	return ""
}

func allGooglePhones(values []*people.PhoneNumber) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		if phone := strings.TrimSpace(value.Value); phone != "" {
			out = append(out, phone)
		}
	}
	return out
}
