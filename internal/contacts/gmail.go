package contacts

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/googleauth"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"golang.org/x/oauth2"
	"google.golang.org/api/option"
	people "google.golang.org/api/people/v1"
)

const googleProviderName = "google_contacts"

// GmailProvider adapts the Google People API to the contacts Provider plus
// Searcher, Mutator, Grouper, and PhotoFetcher capability interfaces. It
// reuses a shared googleauth.Session so mail, calendar, contacts, and tasks
// all route through a single OAuth pipeline per Google account.
type GmailProvider struct {
	session *googleauth.Session
	svcFn   func(ctx context.Context) (*people.Service, error)
	httpFn  func(ctx context.Context) (*http.Client, error)
}

var (
	_ Provider     = (*GmailProvider)(nil)
	_ Searcher     = (*GmailProvider)(nil)
	_ Mutator      = (*GmailProvider)(nil)
	_ Grouper      = (*GmailProvider)(nil)
	_ PhotoFetcher = (*GmailProvider)(nil)
)

// NewGmailProvider wraps an existing authenticated session. The ScopeContacts
// scope must already be part of the session's granted scope set;
// googleauth.DefaultScopes includes it.
func NewGmailProvider(session *googleauth.Session) *GmailProvider {
	return &GmailProvider{session: session}
}

// Session returns the underlying OAuth session so callers can verify sharing
// across feature providers.
func (g *GmailProvider) Session() *googleauth.Session { return g.session }

// ProviderName identifies the backend in logs and MCP payloads.
func (g *GmailProvider) ProviderName() string { return googleProviderName }

// Close is a no-op: the registry owns the session so the provider must not
// tear it down.
func (g *GmailProvider) Close() error { return nil }

func (g *GmailProvider) service(ctx context.Context) (*people.Service, error) {
	if g == nil {
		return nil, fmt.Errorf("contacts: google provider is nil")
	}
	if g.svcFn != nil {
		return g.svcFn(ctx)
	}
	if g.session == nil {
		return nil, fmt.Errorf("contacts: google auth session is not configured")
	}
	tokenSource, err := g.session.TokenSource(ctx)
	if err != nil {
		return nil, err
	}
	svc, err := people.NewService(ctx, option.WithTokenSource(tokenSource))
	if err != nil {
		return nil, fmt.Errorf("create google people service: %w", err)
	}
	return svc, nil
}

func (g *GmailProvider) httpClient(ctx context.Context) (*http.Client, error) {
	if g.httpFn != nil {
		return g.httpFn(ctx)
	}
	if g.session == nil {
		return nil, fmt.Errorf("contacts: google auth session is not configured")
	}
	tokenSource, err := g.session.TokenSource(ctx)
	if err != nil {
		return nil, err
	}
	return oauth2.NewClient(ctx, tokenSource), nil
}

const personFields = "names,emailAddresses,organizations,phoneNumbers,addresses,birthdays,biographies,photos,memberships"

// ListContacts returns every contact in the authenticated account's "My
// Contacts" collection. The People API enforces pagination above 1000.
func (g *GmailProvider) ListContacts(ctx context.Context) ([]providerdata.Contact, error) {
	svc, err := g.service(ctx)
	if err != nil {
		return nil, err
	}
	var out []providerdata.Contact
	pageToken := ""
	for {
		call := svc.People.Connections.List("people/me").
			Context(ctx).
			PageSize(1000).
			PersonFields(personFields)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		result, err := call.Do()
		if err != nil {
			return nil, fmt.Errorf("list google contacts: %w", err)
		}
		for _, person := range result.Connections {
			if c := contactFromGooglePerson(person); c != nil {
				out = append(out, *c)
			}
		}
		if strings.TrimSpace(result.NextPageToken) == "" {
			break
		}
		pageToken = result.NextPageToken
	}
	return out, nil
}

// GetContact resolves a single contact by its resourceName (e.g. people/c1234).
func (g *GmailProvider) GetContact(ctx context.Context, id string) (providerdata.Contact, error) {
	svc, err := g.service(ctx)
	if err != nil {
		return providerdata.Contact{}, err
	}
	name := strings.TrimSpace(id)
	if name == "" {
		return providerdata.Contact{}, fmt.Errorf("id is required")
	}
	person, err := svc.People.Get(name).Context(ctx).PersonFields(personFields).Do()
	if err != nil {
		return providerdata.Contact{}, fmt.Errorf("get google contact: %w", err)
	}
	contact := contactFromGooglePerson(person)
	if contact == nil {
		return providerdata.Contact{}, fmt.Errorf("empty contact %q", id)
	}
	return *contact, nil
}

// SearchContacts runs the People API searchContacts endpoint and returns the
// matched contacts in their full-field form.
func (g *GmailProvider) SearchContacts(ctx context.Context, query string) ([]providerdata.Contact, error) {
	svc, err := g.service(ctx)
	if err != nil {
		return nil, err
	}
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil
	}
	result, err := svc.People.SearchContacts().Context(ctx).Query(q).ReadMask(personFields).PageSize(30).Do()
	if err != nil {
		return nil, fmt.Errorf("search google contacts: %w", err)
	}
	out := make([]providerdata.Contact, 0, len(result.Results))
	for _, r := range result.Results {
		if c := contactFromGooglePerson(r.Person); c != nil {
			out = append(out, *c)
		}
	}
	return out, nil
}

// CreateContact writes a new person to "My Contacts".
func (g *GmailProvider) CreateContact(ctx context.Context, c providerdata.Contact) (providerdata.Contact, error) {
	svc, err := g.service(ctx)
	if err != nil {
		return providerdata.Contact{}, err
	}
	person := googlePersonFromContact(c)
	created, err := svc.People.CreateContact(person).Context(ctx).Do()
	if err != nil {
		return providerdata.Contact{}, fmt.Errorf("create google contact: %w", err)
	}
	out := contactFromGooglePerson(created)
	if out == nil {
		return providerdata.Contact{}, fmt.Errorf("create returned empty contact")
	}
	return *out, nil
}

// UpdateContact overwrites the editable fields on an existing person. The
// caller must supply `ProviderRef` (resourceName); People requires an etag,
// which we fetch via an initial Get.
func (g *GmailProvider) UpdateContact(ctx context.Context, c providerdata.Contact) (providerdata.Contact, error) {
	svc, err := g.service(ctx)
	if err != nil {
		return providerdata.Contact{}, err
	}
	name := strings.TrimSpace(c.ProviderRef)
	if name == "" {
		return providerdata.Contact{}, fmt.Errorf("provider_ref is required")
	}
	existing, err := svc.People.Get(name).Context(ctx).PersonFields(personFields).Do()
	if err != nil {
		return providerdata.Contact{}, fmt.Errorf("get contact for update: %w", err)
	}
	patch := googlePersonFromContact(c)
	patch.Etag = existing.Etag
	updated, err := svc.People.UpdateContact(name, patch).UpdatePersonFields(personFields).Context(ctx).Do()
	if err != nil {
		return providerdata.Contact{}, fmt.Errorf("update google contact: %w", err)
	}
	out := contactFromGooglePerson(updated)
	if out == nil {
		return providerdata.Contact{}, fmt.Errorf("update returned empty contact")
	}
	return *out, nil
}

// DeleteContact removes the contact. id is the People resourceName.
func (g *GmailProvider) DeleteContact(ctx context.Context, id string) error {
	svc, err := g.service(ctx)
	if err != nil {
		return err
	}
	name := strings.TrimSpace(id)
	if name == "" {
		return fmt.Errorf("id is required")
	}
	if _, err := svc.People.DeleteContact(name).Context(ctx).Do(); err != nil {
		return fmt.Errorf("delete google contact: %w", err)
	}
	return nil
}

// ListContactGroups returns the user's contact groups. The "system" groups
// (myContacts, starred, etc.) come back alongside user-defined ones.
func (g *GmailProvider) ListContactGroups(ctx context.Context) ([]Group, error) {
	svc, err := g.service(ctx)
	if err != nil {
		return nil, err
	}
	result, err := svc.ContactGroups.List().Context(ctx).PageSize(1000).Do()
	if err != nil {
		return nil, fmt.Errorf("list contact groups: %w", err)
	}
	out := make([]Group, 0, len(result.ContactGroups))
	for _, gr := range result.ContactGroups {
		if gr == nil {
			continue
		}
		out = append(out, Group{
			ID:          gr.ResourceName,
			Name:        strings.TrimSpace(gr.FormattedName),
			MemberCount: int(gr.MemberCount),
		})
	}
	return out, nil
}

// AddToGroup appends memberships by patching contactGroups/members:modify.
func (g *GmailProvider) AddToGroup(ctx context.Context, groupID string, contactIDs []string) error {
	svc, err := g.service(ctx)
	if err != nil {
		return err
	}
	g.ensureResourcePrefix(contactIDs)
	_, err = svc.ContactGroups.Members.Modify(strings.TrimSpace(groupID), &people.ModifyContactGroupMembersRequest{
		ResourceNamesToAdd: contactIDs,
	}).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("add members to group: %w", err)
	}
	return nil
}

// RemoveFromGroup drops memberships from a group.
func (g *GmailProvider) RemoveFromGroup(ctx context.Context, groupID string, contactIDs []string) error {
	svc, err := g.service(ctx)
	if err != nil {
		return err
	}
	g.ensureResourcePrefix(contactIDs)
	_, err = svc.ContactGroups.Members.Modify(strings.TrimSpace(groupID), &people.ModifyContactGroupMembersRequest{
		ResourceNamesToRemove: contactIDs,
	}).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("remove members from group: %w", err)
	}
	return nil
}

func (g *GmailProvider) ensureResourcePrefix(ids []string) {
	for i, id := range ids {
		trimmed := strings.TrimSpace(id)
		if trimmed != "" && !strings.HasPrefix(trimmed, "people/") {
			ids[i] = "people/" + trimmed
			continue
		}
		ids[i] = trimmed
	}
}

// GetContactPhoto downloads the primary photo. People puts the photo URL on
// the Person; we fetch the bytes through the authenticated HTTP client so
// private photos come through without an extra auth round-trip.
func (g *GmailProvider) GetContactPhoto(ctx context.Context, id string) ([]byte, string, error) {
	svc, err := g.service(ctx)
	if err != nil {
		return nil, "", err
	}
	name := strings.TrimSpace(id)
	if name == "" {
		return nil, "", fmt.Errorf("id is required")
	}
	person, err := svc.People.Get(name).Context(ctx).PersonFields("photos").Do()
	if err != nil {
		return nil, "", fmt.Errorf("get contact photo metadata: %w", err)
	}
	url := firstGooglePhotoURL(person.Photos)
	if url == "" {
		return nil, "", fmt.Errorf("contact %q has no photo: %w", id, ErrUnsupported)
	}
	client, err := g.httpClient(ctx)
	if err != nil {
		return nil, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("fetch contact photo: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("photo fetch returned %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	return body, resp.Header.Get("Content-Type"), nil
}

func contactFromGooglePerson(person *people.Person) *providerdata.Contact {
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
	contact := &providerdata.Contact{
		ProviderRef:  strings.TrimSpace(person.ResourceName),
		Name:         name,
		Email:        emailAddress,
		Organization: firstGoogleOrganization(person.Organizations),
		Phones:       allGooglePhones(person.PhoneNumbers),
		Addresses:    allGoogleAddresses(person.Addresses),
		Notes:        firstGoogleBiography(person.Biographies),
	}
	if bd := firstGoogleBirthday(person.Birthdays); bd != nil {
		contact.Birthday = bd
	}
	if photos := allGooglePhotoRefs(person.Photos); len(photos) > 0 {
		contact.Photos = photos
	}
	return contact
}

func googlePersonFromContact(c providerdata.Contact) *people.Person {
	person := &people.Person{}
	if name := strings.TrimSpace(c.Name); name != "" {
		person.Names = []*people.Name{{DisplayName: name, GivenName: name}}
	}
	if email := strings.TrimSpace(c.Email); email != "" {
		person.EmailAddresses = []*people.EmailAddress{{Value: email}}
	}
	if org := strings.TrimSpace(c.Organization); org != "" {
		person.Organizations = []*people.Organization{{Name: org}}
	}
	for _, phone := range c.Phones {
		if p := strings.TrimSpace(phone); p != "" {
			person.PhoneNumbers = append(person.PhoneNumbers, &people.PhoneNumber{Value: p})
		}
	}
	for _, addr := range c.Addresses {
		pa := &people.Address{
			Type:          addr.Type,
			StreetAddress: addr.Street,
			City:          addr.City,
			Region:        addr.Region,
			PostalCode:    addr.Postal,
			Country:       addr.Country,
		}
		if pa.Type == "" {
			pa.Type = "other"
		}
		person.Addresses = append(person.Addresses, pa)
	}
	if notes := strings.TrimSpace(c.Notes); notes != "" {
		person.Biographies = []*people.Biography{{Value: notes, ContentType: "TEXT_PLAIN"}}
	}
	return person
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

func allGoogleAddresses(values []*people.Address) []providerdata.PostalAddress {
	if len(values) == 0 {
		return nil
	}
	out := make([]providerdata.PostalAddress, 0, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		addr := providerdata.PostalAddress{
			Type:    strings.TrimSpace(value.Type),
			Street:  strings.TrimSpace(value.StreetAddress),
			City:    strings.TrimSpace(value.City),
			Region:  strings.TrimSpace(value.Region),
			Postal:  strings.TrimSpace(value.PostalCode),
			Country: strings.TrimSpace(value.Country),
		}
		if addr.Street == "" && addr.City == "" && addr.Postal == "" && addr.Country == "" {
			continue
		}
		out = append(out, addr)
	}
	return out
}

func firstGoogleBiography(values []*people.Biography) string {
	for _, value := range values {
		if value == nil {
			continue
		}
		if note := strings.TrimSpace(value.Value); note != "" {
			return note
		}
	}
	return ""
}

// firstGoogleBirthday returns the first birthday with a full Y-M-D date.
// Partial birthdays (missing year) are skipped; callers get nil and the
// contact-level field stays unset.
func firstGoogleBirthday(values []*people.Birthday) *time.Time {
	for _, value := range values {
		if value == nil || value.Date == nil {
			continue
		}
		if value.Date.Year == 0 || value.Date.Month == 0 || value.Date.Day == 0 {
			continue
		}
		t := time.Date(int(value.Date.Year), time.Month(value.Date.Month), int(value.Date.Day), 0, 0, 0, 0, time.UTC)
		return &t
	}
	return nil
}

func allGooglePhotoRefs(values []*people.Photo) []providerdata.PhotoRef {
	out := make([]providerdata.PhotoRef, 0, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		if url := strings.TrimSpace(value.Url); url != "" {
			out = append(out, providerdata.PhotoRef{URL: url})
		}
	}
	return out
}

func firstGooglePhotoURL(values []*people.Photo) string {
	for _, value := range values {
		if value == nil {
			continue
		}
		if url := strings.TrimSpace(value.Url); url != "" {
			return url
		}
	}
	return ""
}
