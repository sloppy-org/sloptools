package contacts

import (
	"context"
	"fmt"
	"strings"

	"github.com/sloppy-org/sloptools/internal/ews"
	"github.com/sloppy-org/sloptools/internal/providerdata"
)

const ewsProviderName = "exchange_ews_contacts"

// EWSProvider adapts the EWS Contact item SOAP operations to the contacts
// Provider, Searcher, and Mutator capability interfaces. The underlying
// ews.Client is shared across mail/calendar/contacts/tasks providers via the
// groupware registry. Photo fetching uses the separate EWS `GetUserPhoto`
// endpoint and is intentionally out of scope here; callers that need photos
// fall back to contacts.ErrUnsupported via groupware.Supports.
type EWSProvider struct {
	client       *ews.Client
	contactsRoot string
}

var (
	_ Provider = (*EWSProvider)(nil)
	_ Searcher = (*EWSProvider)(nil)
	_ Mutator  = (*EWSProvider)(nil)
)

// NewEWSProvider wraps a cached EWS client. An empty contactsFolderID
// defaults to the distinguished `contacts` folder, matching the
// `FolderKindContacts` constant in internal/ews.
func NewEWSProvider(client *ews.Client, contactsFolderID string) *EWSProvider {
	return &EWSProvider{client: client, contactsRoot: strings.TrimSpace(contactsFolderID)}
}

// Client exposes the cached ews.Client so callers can verify that mail,
// calendar, contacts, and tasks share one pipeline per Exchange account.
func (p *EWSProvider) Client() *ews.Client { return p.client }

// ProviderName identifies the backend in logs and MCP payloads.
func (p *EWSProvider) ProviderName() string { return ewsProviderName }

// Close is a no-op: the registry owns the underlying ews.Client.
func (p *EWSProvider) Close() error { return nil }

// ListContacts enumerates every contact in the configured folder, paging
// through EWS until the final batch is reached.
func (p *EWSProvider) ListContacts(ctx context.Context) ([]providerdata.Contact, error) {
	if err := p.ensureClient("ListContacts"); err != nil {
		return nil, err
	}
	const batch = 100
	var out []providerdata.Contact
	offset := 0
	for {
		items, err := p.client.ListContactItems(ctx, p.contactsRoot, offset, batch)
		if err != nil {
			return nil, fmt.Errorf("ews contacts list: %w", err)
		}
		if len(items) == 0 {
			break
		}
		for _, item := range items {
			if c := ewsContactToProviderContact(item); c != nil {
				out = append(out, *c)
			}
		}
		if len(items) < batch {
			break
		}
		offset += len(items)
	}
	return out, nil
}

// GetContact fetches a single contact by its EWS item id.
func (p *EWSProvider) GetContact(ctx context.Context, id string) (providerdata.Contact, error) {
	if err := p.ensureClient("GetContact"); err != nil {
		return providerdata.Contact{}, err
	}
	clean := strings.TrimSpace(id)
	if clean == "" {
		return providerdata.Contact{}, fmt.Errorf("ews contacts GetContact: id is required")
	}
	item, err := p.client.GetContactItem(ctx, clean)
	if err != nil {
		return providerdata.Contact{}, fmt.Errorf("ews contacts get: %w", err)
	}
	contact := ewsContactToProviderContact(item)
	if contact == nil {
		return providerdata.Contact{}, fmt.Errorf("ews contact %q has no displayable fields", clean)
	}
	return *contact, nil
}

// SearchContacts runs EWS `ResolveNames` with full contact data and converts
// each resolution into providerdata.Contact. The results merge Global Address
// List hits with personal contacts.
func (p *EWSProvider) SearchContacts(ctx context.Context, query string) ([]providerdata.Contact, error) {
	if err := p.ensureClient("SearchContacts"); err != nil {
		return nil, err
	}
	clean := strings.TrimSpace(query)
	if clean == "" {
		return nil, nil
	}
	resolved, err := p.client.ResolveNames(ctx, clean, true)
	if err != nil {
		return nil, fmt.Errorf("ews contacts search: %w", err)
	}
	out := make([]providerdata.Contact, 0, len(resolved))
	for _, entry := range resolved {
		if c := ewsResolvedContactToProviderContact(entry); c != nil {
			out = append(out, *c)
		}
	}
	return out, nil
}

// CreateContact writes a new Contact item into the configured folder.
func (p *EWSProvider) CreateContact(ctx context.Context, c providerdata.Contact) (providerdata.Contact, error) {
	if err := p.ensureClient("CreateContact"); err != nil {
		return providerdata.Contact{}, err
	}
	itemID, _, err := p.client.CreateContactItem(ctx, p.contactsRoot, providerContactToEWSItem(c, ""))
	if err != nil {
		return providerdata.Contact{}, fmt.Errorf("ews contacts create: %w", err)
	}
	if strings.TrimSpace(itemID) == "" {
		return providerdata.Contact{}, fmt.Errorf("ews contacts create: server returned empty item id")
	}
	c.ProviderRef = itemID
	return c, nil
}

// UpdateContact rewrites the editable fields on an existing Contact. The
// caller must populate ProviderRef with the EWS item id. The adapter fetches
// the current item to obtain a fresh ChangeKey and then issues one UpdateItem
// call covering every provider-level field.
func (p *EWSProvider) UpdateContact(ctx context.Context, c providerdata.Contact) (providerdata.Contact, error) {
	if err := p.ensureClient("UpdateContact"); err != nil {
		return providerdata.Contact{}, err
	}
	itemID := strings.TrimSpace(c.ProviderRef)
	if itemID == "" {
		return providerdata.Contact{}, fmt.Errorf("ews contacts update: provider_ref is required")
	}
	existing, err := p.client.GetContactItem(ctx, itemID)
	if err != nil {
		return providerdata.Contact{}, fmt.Errorf("ews contacts update: fetch current item: %w", err)
	}
	updates := providerContactToEWSUpdate(c)
	if _, err := p.client.UpdateContactItem(ctx, itemID, existing.ChangeKey, updates); err != nil {
		return providerdata.Contact{}, fmt.Errorf("ews contacts update: %w", err)
	}
	refreshed, err := p.client.GetContactItem(ctx, itemID)
	if err != nil {
		return providerdata.Contact{}, fmt.Errorf("ews contacts update: reload item: %w", err)
	}
	updated := ewsContactToProviderContact(refreshed)
	if updated == nil {
		return providerdata.Contact{}, fmt.Errorf("ews contacts update: refreshed item is empty")
	}
	return *updated, nil
}

// DeleteContact permanently removes the contact identified by its EWS item
// id.
func (p *EWSProvider) DeleteContact(ctx context.Context, id string) error {
	if err := p.ensureClient("DeleteContact"); err != nil {
		return err
	}
	clean := strings.TrimSpace(id)
	if clean == "" {
		return fmt.Errorf("ews contacts delete: id is required")
	}
	if err := p.client.DeleteContactItem(ctx, clean); err != nil {
		return fmt.Errorf("ews contacts delete: %w", err)
	}
	return nil
}

func (p *EWSProvider) ensureClient(op string) error {
	if p == nil || p.client == nil {
		return fmt.Errorf("ews contacts %s: client is not configured", op)
	}
	return nil
}

func ewsContactToProviderContact(item ews.ContactItem) *providerdata.Contact {
	name := bestContactDisplayName(item)
	emailAddress := firstEWSEmail(item.EmailAddresses)
	if name == "" {
		name = emailAddress
	}
	if name == "" && emailAddress == "" && len(item.PhoneNumbers) == 0 {
		return nil
	}
	out := &providerdata.Contact{
		ProviderRef:  strings.TrimSpace(item.ID),
		Name:         name,
		Email:        strings.ToLower(emailAddress),
		Organization: strings.TrimSpace(item.CompanyName),
		Phones:       allEWSPhones(item.PhoneNumbers),
		Addresses:    allEWSAddresses(item.PhysicalAddresses),
		Notes:        strings.TrimSpace(item.Notes),
	}
	if item.Birthday != nil && !item.Birthday.IsZero() {
		bd := *item.Birthday
		out.Birthday = &bd
	}
	return out
}

func ewsResolvedContactToProviderContact(entry ews.ResolvedContact) *providerdata.Contact {
	if strings.TrimSpace(entry.Mailbox.Email) == "" && strings.TrimSpace(entry.Contact.DisplayName) == "" {
		return nil
	}
	base := ewsContactToProviderContact(entry.Contact)
	if base == nil {
		base = &providerdata.Contact{}
	}
	if base.Name == "" {
		base.Name = strings.TrimSpace(entry.Mailbox.Name)
	}
	if base.Email == "" {
		base.Email = strings.ToLower(strings.TrimSpace(entry.Mailbox.Email))
	}
	if base.Name == "" && base.Email == "" {
		return nil
	}
	return base
}

func bestContactDisplayName(item ews.ContactItem) string {
	if name := strings.TrimSpace(item.DisplayName); name != "" {
		return name
	}
	given := strings.TrimSpace(item.GivenName)
	surname := strings.TrimSpace(item.Surname)
	switch {
	case given != "" && surname != "":
		return given + " " + surname
	case given != "":
		return given
	case surname != "":
		return surname
	}
	return ""
}

func firstEWSEmail(entries []ews.ContactEmail) string {
	for _, entry := range entries {
		if clean := strings.TrimSpace(entry.Value); clean != "" {
			return clean
		}
	}
	return ""
}

func allEWSPhones(entries []ews.ContactPhone) []string {
	if len(entries) == 0 {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		if clean := strings.TrimSpace(entry.Value); clean != "" {
			out = append(out, clean)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func allEWSAddresses(entries []ews.ContactPhysicalAddress) []providerdata.PostalAddress {
	if len(entries) == 0 {
		return nil
	}
	out := make([]providerdata.PostalAddress, 0, len(entries))
	for _, entry := range entries {
		addr := providerdata.PostalAddress{
			Type:    strings.ToLower(strings.TrimSpace(entry.Key)),
			Street:  strings.TrimSpace(entry.Street),
			City:    strings.TrimSpace(entry.City),
			Region:  strings.TrimSpace(entry.State),
			Postal:  strings.TrimSpace(entry.PostalCode),
			Country: strings.TrimSpace(entry.CountryOrRegion),
		}
		if addr.Street == "" && addr.City == "" && addr.Region == "" && addr.Postal == "" && addr.Country == "" {
			continue
		}
		out = append(out, addr)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func providerContactToEWSItem(c providerdata.Contact, itemID string) ews.ContactItem {
	item := ews.ContactItem{
		ID:                strings.TrimSpace(itemID),
		DisplayName:       strings.TrimSpace(c.Name),
		CompanyName:       strings.TrimSpace(c.Organization),
		Notes:             strings.TrimSpace(c.Notes),
		EmailAddresses:    contactEmailsFromProvider(c.Email),
		PhoneNumbers:      contactPhonesFromProvider(c.Phones),
		PhysicalAddresses: contactAddressesFromProvider(c.Addresses),
	}
	if c.Birthday != nil && !c.Birthday.IsZero() {
		bd := *c.Birthday
		item.Birthday = &bd
	}
	return item
}

func providerContactToEWSUpdate(c providerdata.Contact) ews.ContactUpdate {
	name := strings.TrimSpace(c.Name)
	organization := strings.TrimSpace(c.Organization)
	notes := c.Notes
	emails := contactEmailsFromProvider(c.Email)
	phones := contactPhonesFromProvider(c.Phones)
	addresses := contactAddressesFromProvider(c.Addresses)
	update := ews.ContactUpdate{
		DisplayName:       &name,
		CompanyName:       &organization,
		Notes:             &notes,
		EmailAddresses:    &emails,
		PhoneNumbers:      &phones,
		PhysicalAddresses: &addresses,
	}
	if c.Birthday != nil && !c.Birthday.IsZero() {
		bd := *c.Birthday
		update.Birthday = &bd
	} else {
		update.ClearBirthday = true
	}
	return update
}

func contactEmailsFromProvider(email string) []ews.ContactEmail {
	clean := strings.TrimSpace(email)
	if clean == "" {
		return nil
	}
	return []ews.ContactEmail{{Key: "EmailAddress1", Value: clean}}
}

func contactPhonesFromProvider(phones []string) []ews.ContactPhone {
	if len(phones) == 0 {
		return nil
	}
	keys := []string{"BusinessPhone", "HomePhone", "MobilePhone", "OtherTelephone"}
	out := make([]ews.ContactPhone, 0, len(phones))
	for i, phone := range phones {
		clean := strings.TrimSpace(phone)
		if clean == "" {
			continue
		}
		key := "OtherTelephone"
		if i < len(keys) {
			key = keys[i]
		}
		out = append(out, ews.ContactPhone{Key: key, Value: clean})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func contactAddressesFromProvider(entries []providerdata.PostalAddress) []ews.ContactPhysicalAddress {
	if len(entries) == 0 {
		return nil
	}
	out := make([]ews.ContactPhysicalAddress, 0, len(entries))
	for _, entry := range entries {
		key := ewsAddressKey(entry.Type)
		if strings.TrimSpace(entry.Street) == "" && strings.TrimSpace(entry.City) == "" && strings.TrimSpace(entry.Region) == "" && strings.TrimSpace(entry.Postal) == "" && strings.TrimSpace(entry.Country) == "" {
			continue
		}
		out = append(out, ews.ContactPhysicalAddress{
			Key:             key,
			Street:          strings.TrimSpace(entry.Street),
			City:            strings.TrimSpace(entry.City),
			State:           strings.TrimSpace(entry.Region),
			PostalCode:      strings.TrimSpace(entry.Postal),
			CountryOrRegion: strings.TrimSpace(entry.Country),
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func ewsAddressKey(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "home":
		return "Home"
	case "work", "business", "office":
		return "Business"
	default:
		return "Other"
	}
}
