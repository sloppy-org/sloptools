package ews

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ContactItem is the EWS-side representation of a contact used by
// create/read/update paths. It mirrors the fields of the SOAP `Contact`
// element that the adapter in `internal/contacts` maps to
// providerdata.Contact.
type ContactItem struct {
	ID                string
	ChangeKey         string
	ParentFolderID    string
	DisplayName       string
	GivenName         string
	Surname           string
	CompanyName       string
	JobTitle          string
	Department        string
	Notes             string
	EmailAddresses    []ContactEmail
	PhoneNumbers      []ContactPhone
	PhysicalAddresses []ContactPhysicalAddress
	Birthday          *time.Time
}

// ContactEmail is one entry in the EWS `EmailAddresses` collection. Key must
// be one of EmailAddress1, EmailAddress2, or EmailAddress3.
type ContactEmail struct {
	Key   string
	Value string
}

// ContactPhone is one entry in the EWS `PhoneNumbers` collection. Key follows
// the EWS `PhoneNumberKeyType` vocabulary (BusinessPhone, HomePhone,
// MobilePhone, OtherTelephone, ...).
type ContactPhone struct {
	Key   string
	Value string
}

// ContactPhysicalAddress is one entry in the EWS `PhysicalAddresses`
// collection. Key is Home, Business, or Other.
type ContactPhysicalAddress struct {
	Key             string
	Street          string
	City            string
	State           string
	PostalCode      string
	CountryOrRegion string
}

// ContactUpdate describes a partial write against an existing contact item.
// Non-nil scalar pointers replace the matching EWS property; an empty string
// clears it via DeleteItemField. Nil scalars are left untouched. Collection
// pointers follow the same rule: nil skips, non-nil replaces the whole
// collection (an empty slice clears it).
type ContactUpdate struct {
	DisplayName       *string
	GivenName         *string
	Surname           *string
	CompanyName       *string
	JobTitle          *string
	Department        *string
	Notes             *string
	Birthday          *time.Time
	ClearBirthday     bool
	EmailAddresses    *[]ContactEmail
	PhoneNumbers      *[]ContactPhone
	PhysicalAddresses *[]ContactPhysicalAddress
}

// ResolvedContact pairs a mailbox with the optional Contact element that
// ResolveNames returns when IncludeFullContactData is set.
type ResolvedContact struct {
	Mailbox Mailbox
	Contact ContactItem
}

// CreateContactItem creates a Contact item in the given folder and returns
// the new EWS item id and change key. A blank parentFolderID defaults to the
// `contacts` distinguished folder.
func (c *Client) CreateContactItem(ctx context.Context, parentFolderID string, contact ContactItem) (string, string, error) {
	body := `<m:CreateItem><m:SavedItemFolderId>` + folderIDXML(folderIDOrDistinguished(parentFolderID, "contacts")) + `</m:SavedItemFolderId><m:Items>` + contactItemCreateXML(contact) + `</m:Items></m:CreateItem>`
	var resp createItemEnvelope
	if err := c.call(ctx, "CreateItem", body, &resp); err != nil {
		return "", "", err
	}
	items := resp.Body.CreateItemResponse.ResponseMessages.Message.Items.Values
	if len(items) == 0 {
		return "", "", fmt.Errorf("ews CreateContactItem returned no items")
	}
	return strings.TrimSpace(items[0].ItemID.ID), strings.TrimSpace(items[0].ItemID.ChangeKey), nil
}

// UpdateContactItem issues an UpdateItem SOAP call against an existing
// contact. It returns the new change key. Non-nil fields in updates are
// translated into SetItemField or DeleteItemField changes per the EWS
// schema.
func (c *Client) UpdateContactItem(ctx context.Context, itemID, changeKey string, updates ContactUpdate) (string, error) {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return "", fmt.Errorf("ews UpdateContactItem: item id is required")
	}
	changes := contactUpdateXML(updates)
	if strings.TrimSpace(changes) == "" {
		return strings.TrimSpace(changeKey), nil
	}
	var b strings.Builder
	b.WriteString(`<m:UpdateItem MessageDisposition="SaveOnly" ConflictResolution="AlwaysOverwrite"><m:ItemChanges><t:ItemChange><t:ItemId Id="`)
	b.WriteString(xmlEscapeAttr(itemID))
	b.WriteString(`"`)
	if ck := strings.TrimSpace(changeKey); ck != "" {
		b.WriteString(` ChangeKey="`)
		b.WriteString(xmlEscapeAttr(ck))
		b.WriteString(`"`)
	}
	b.WriteString(` /><t:Updates>`)
	b.WriteString(changes)
	b.WriteString(`</t:Updates></t:ItemChange></m:ItemChanges></m:UpdateItem>`)
	var resp updateContactItemEnvelope
	if err := c.call(ctx, "UpdateItem", b.String(), &resp); err != nil {
		return "", err
	}
	items := resp.Body.UpdateItemResponse.ResponseMessages.Message.Items.Values
	if len(items) == 0 {
		return strings.TrimSpace(changeKey), nil
	}
	newKey := strings.TrimSpace(items[0].ItemID.ChangeKey)
	if newKey == "" {
		newKey = strings.TrimSpace(changeKey)
	}
	return newKey, nil
}

// DeleteContactItem permanently removes a contact via DeleteItem with
// DeleteType=HardDelete.
func (c *Client) DeleteContactItem(ctx context.Context, itemID string) error {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return fmt.Errorf("ews DeleteContactItem: item id is required")
	}
	body := `<m:DeleteItem DeleteType="HardDelete" AffectedTaskOccurrences="AllOccurrences"><m:ItemIds><t:ItemId Id="` + xmlEscapeAttr(itemID) + `" /></m:ItemIds></m:DeleteItem>`
	var resp simpleResponseEnvelope
	return c.call(ctx, "DeleteItem", body, &resp)
}

// GetContactItem fetches a single contact by id and returns its parsed
// fields. Used by the contacts EWS adapter to implement GetContact against
// the shared EWS client.
func (c *Client) GetContactItem(ctx context.Context, itemID string) (ContactItem, error) {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return ContactItem{}, fmt.Errorf("ews GetContactItem: item id is required")
	}
	var resp getContactItemEnvelope
	if err := c.call(ctx, "GetItem", getItemBody([]string{itemID}, true), &resp); err != nil {
		return ContactItem{}, err
	}
	contacts := resp.Body.GetItemResponse.ResponseMessages.Message.Items.Contacts
	if len(contacts) == 0 {
		return ContactItem{}, fmt.Errorf("ews GetContactItem: contact %q not found", itemID)
	}
	return contacts[0].toContactItem(), nil
}

// ListContactItems enumerates contacts in the given folder with all
// properties populated. Empty folderID defaults to the distinguished
// `contacts` folder. The returned slice preserves server order. offset/max
// map to FindItem paging; pass max<=0 for the client's configured batch
// size.
func (c *Client) ListContactItems(ctx context.Context, folderID string, offset, max int) ([]ContactItem, error) {
	found, err := c.FindMessages(ctx, folderIDOrDistinguished(folderID, "contacts"), offset, max)
	if err != nil {
		return nil, err
	}
	if len(found.ItemIDs) == 0 {
		return nil, nil
	}
	var resp getContactItemEnvelope
	if err := c.call(ctx, "GetItem", getItemBody(found.ItemIDs, true), &resp); err != nil {
		return nil, err
	}
	contacts := resp.Body.GetItemResponse.ResponseMessages.Message.Items.Contacts
	out := make([]ContactItem, 0, len(contacts))
	for _, raw := range contacts {
		out = append(out, raw.toContactItem())
	}
	return out, nil
}

// ResolveNames invokes the EWS `ResolveNames` operation. It matches against
// the user's contacts and, when permitted, the Global Address List, and
// returns every resolution the server found. When includeFullContactData is
// true each resolution may carry a populated Contact element.
func (c *Client) ResolveNames(ctx context.Context, unresolved string, includeFullContactData bool) ([]ResolvedContact, error) {
	unresolved = strings.TrimSpace(unresolved)
	if unresolved == "" {
		return nil, nil
	}
	full := "false"
	if includeFullContactData {
		full = "true"
	}
	body := fmt.Sprintf(`<m:ResolveNames ReturnFullContactData="%s" SearchScope="ActiveDirectoryContacts"><m:UnresolvedEntry>%s</m:UnresolvedEntry></m:ResolveNames>`, full, xmlEscapeText(unresolved))
	var resp resolveNamesEnvelope
	if err := c.call(ctx, "ResolveNames", body, &resp); err != nil {
		return nil, err
	}
	return resp.toResolvedContacts(), nil
}
