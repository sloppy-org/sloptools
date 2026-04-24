package ews

import (
	"encoding/xml"
	"strings"
	"time"
)

func contactItemCreateXML(contact ContactItem) string {
	var b strings.Builder
	b.WriteString(`<t:Contact>`)
	writeOptionalElement(&b, "FileAs", contact.DisplayName)
	writeOptionalElement(&b, "DisplayName", contact.DisplayName)
	writeOptionalElement(&b, "GivenName", contact.GivenName)
	writeOptionalElement(&b, "CompanyName", contact.CompanyName)
	writeContactEmailAddresses(&b, contact.EmailAddresses)
	writeContactPhysicalAddresses(&b, contact.PhysicalAddresses)
	writeContactPhoneNumbers(&b, contact.PhoneNumbers)
	if contact.Birthday != nil && !contact.Birthday.IsZero() {
		b.WriteString(`<t:Birthday>`)
		b.WriteString(contact.Birthday.UTC().Format(time.RFC3339))
		b.WriteString(`</t:Birthday>`)
	}
	writeOptionalElement(&b, "Department", contact.Department)
	writeOptionalElement(&b, "JobTitle", contact.JobTitle)
	writeOptionalElement(&b, "Surname", contact.Surname)
	if notes := strings.TrimSpace(contact.Notes); notes != "" {
		b.WriteString(`<t:Body BodyType="Text">`)
		b.WriteString(xmlEscapeText(contact.Notes))
		b.WriteString(`</t:Body>`)
	}
	b.WriteString(`</t:Contact>`)
	return b.String()
}

func writeOptionalElement(b *strings.Builder, name, value string) {
	clean := strings.TrimSpace(value)
	if clean == "" {
		return
	}
	b.WriteString(`<t:`)
	b.WriteString(name)
	b.WriteString(`>`)
	b.WriteString(xmlEscapeText(clean))
	b.WriteString(`</t:`)
	b.WriteString(name)
	b.WriteString(`>`)
}

func writeContactEmailAddresses(b *strings.Builder, entries []ContactEmail) {
	clean := filterContactEmails(entries)
	if len(clean) == 0 {
		return
	}
	b.WriteString(`<t:EmailAddresses>`)
	for _, entry := range clean {
		b.WriteString(`<t:Entry Key="`)
		b.WriteString(xmlEscapeAttr(entry.Key))
		b.WriteString(`">`)
		b.WriteString(xmlEscapeText(entry.Value))
		b.WriteString(`</t:Entry>`)
	}
	b.WriteString(`</t:EmailAddresses>`)
}

func writeContactPhoneNumbers(b *strings.Builder, entries []ContactPhone) {
	clean := filterContactPhones(entries)
	if len(clean) == 0 {
		return
	}
	b.WriteString(`<t:PhoneNumbers>`)
	for _, entry := range clean {
		b.WriteString(`<t:Entry Key="`)
		b.WriteString(xmlEscapeAttr(entry.Key))
		b.WriteString(`">`)
		b.WriteString(xmlEscapeText(entry.Value))
		b.WriteString(`</t:Entry>`)
	}
	b.WriteString(`</t:PhoneNumbers>`)
}

func writeContactPhysicalAddresses(b *strings.Builder, entries []ContactPhysicalAddress) {
	clean := filterContactAddresses(entries)
	if len(clean) == 0 {
		return
	}
	b.WriteString(`<t:PhysicalAddresses>`)
	for _, entry := range clean {
		b.WriteString(`<t:Entry Key="`)
		b.WriteString(xmlEscapeAttr(entry.Key))
		b.WriteString(`">`)
		writeOptionalElement(b, "Street", entry.Street)
		writeOptionalElement(b, "City", entry.City)
		writeOptionalElement(b, "State", entry.State)
		writeOptionalElement(b, "PostalCode", entry.PostalCode)
		writeOptionalElement(b, "CountryOrRegion", entry.CountryOrRegion)
		b.WriteString(`</t:Entry>`)
	}
	b.WriteString(`</t:PhysicalAddresses>`)
}

func filterContactEmails(entries []ContactEmail) []ContactEmail {
	out := make([]ContactEmail, 0, len(entries))
	for _, entry := range entries {
		value := strings.TrimSpace(entry.Value)
		key := strings.TrimSpace(entry.Key)
		if value == "" || key == "" {
			continue
		}
		out = append(out, ContactEmail{Key: key, Value: value})
	}
	return out
}

func filterContactPhones(entries []ContactPhone) []ContactPhone {
	out := make([]ContactPhone, 0, len(entries))
	for _, entry := range entries {
		value := strings.TrimSpace(entry.Value)
		key := strings.TrimSpace(entry.Key)
		if value == "" || key == "" {
			continue
		}
		out = append(out, ContactPhone{Key: key, Value: value})
	}
	return out
}

func filterContactAddresses(entries []ContactPhysicalAddress) []ContactPhysicalAddress {
	out := make([]ContactPhysicalAddress, 0, len(entries))
	for _, entry := range entries {
		key := strings.TrimSpace(entry.Key)
		if key == "" {
			continue
		}
		street := strings.TrimSpace(entry.Street)
		city := strings.TrimSpace(entry.City)
		state := strings.TrimSpace(entry.State)
		postal := strings.TrimSpace(entry.PostalCode)
		country := strings.TrimSpace(entry.CountryOrRegion)
		if street == "" && city == "" && state == "" && postal == "" && country == "" {
			continue
		}
		out = append(out, ContactPhysicalAddress{Key: key, Street: street, City: city, State: state, PostalCode: postal, CountryOrRegion: country})
	}
	return out
}

func contactUpdateXML(updates ContactUpdate) string {
	var b strings.Builder
	appendContactScalarUpdate(&b, "DisplayName", updates.DisplayName)
	appendContactScalarUpdate(&b, "GivenName", updates.GivenName)
	appendContactScalarUpdate(&b, "Surname", updates.Surname)
	appendContactScalarUpdate(&b, "CompanyName", updates.CompanyName)
	appendContactScalarUpdate(&b, "JobTitle", updates.JobTitle)
	appendContactScalarUpdate(&b, "Department", updates.Department)
	if updates.Notes != nil {
		if clean := strings.TrimSpace(*updates.Notes); clean == "" {
			b.WriteString(`<t:DeleteItemField><t:FieldURI FieldURI="item:Body" /></t:DeleteItemField>`)
		} else {
			b.WriteString(`<t:SetItemField><t:FieldURI FieldURI="item:Body" /><t:Contact><t:Body BodyType="Text">`)
			b.WriteString(xmlEscapeText(*updates.Notes))
			b.WriteString(`</t:Body></t:Contact></t:SetItemField>`)
		}
	}
	if updates.ClearBirthday {
		b.WriteString(`<t:DeleteItemField><t:FieldURI FieldURI="contacts:Birthday" /></t:DeleteItemField>`)
	} else if updates.Birthday != nil && !updates.Birthday.IsZero() {
		b.WriteString(`<t:SetItemField><t:FieldURI FieldURI="contacts:Birthday" /><t:Contact><t:Birthday>`)
		b.WriteString(updates.Birthday.UTC().Format(time.RFC3339))
		b.WriteString(`</t:Birthday></t:Contact></t:SetItemField>`)
	}
	if updates.EmailAddresses != nil {
		cleaned := filterContactEmails(*updates.EmailAddresses)
		appendContactCollectionUpdate(&b, "contacts:EmailAddresses", func(inner *strings.Builder) {
			inner.WriteString(`<t:EmailAddresses>`)
			for _, entry := range cleaned {
				inner.WriteString(`<t:Entry Key="`)
				inner.WriteString(xmlEscapeAttr(entry.Key))
				inner.WriteString(`">`)
				inner.WriteString(xmlEscapeText(entry.Value))
				inner.WriteString(`</t:Entry>`)
			}
			inner.WriteString(`</t:EmailAddresses>`)
		}, len(cleaned) == 0)
	}
	if updates.PhoneNumbers != nil {
		cleaned := filterContactPhones(*updates.PhoneNumbers)
		appendContactCollectionUpdate(&b, "contacts:PhoneNumbers", func(inner *strings.Builder) {
			inner.WriteString(`<t:PhoneNumbers>`)
			for _, entry := range cleaned {
				inner.WriteString(`<t:Entry Key="`)
				inner.WriteString(xmlEscapeAttr(entry.Key))
				inner.WriteString(`">`)
				inner.WriteString(xmlEscapeText(entry.Value))
				inner.WriteString(`</t:Entry>`)
			}
			inner.WriteString(`</t:PhoneNumbers>`)
		}, len(cleaned) == 0)
	}
	if updates.PhysicalAddresses != nil {
		cleaned := filterContactAddresses(*updates.PhysicalAddresses)
		appendContactCollectionUpdate(&b, "contacts:PhysicalAddresses", func(inner *strings.Builder) {
			inner.WriteString(`<t:PhysicalAddresses>`)
			for _, entry := range cleaned {
				inner.WriteString(`<t:Entry Key="`)
				inner.WriteString(xmlEscapeAttr(entry.Key))
				inner.WriteString(`">`)
				writeOptionalElement(inner, "Street", entry.Street)
				writeOptionalElement(inner, "City", entry.City)
				writeOptionalElement(inner, "State", entry.State)
				writeOptionalElement(inner, "PostalCode", entry.PostalCode)
				writeOptionalElement(inner, "CountryOrRegion", entry.CountryOrRegion)
				inner.WriteString(`</t:Entry>`)
			}
			inner.WriteString(`</t:PhysicalAddresses>`)
		}, len(cleaned) == 0)
	}
	return b.String()
}

func appendContactScalarUpdate(b *strings.Builder, name string, value *string) {
	if value == nil {
		return
	}
	clean := strings.TrimSpace(*value)
	if clean == "" {
		b.WriteString(`<t:DeleteItemField><t:FieldURI FieldURI="contacts:`)
		b.WriteString(name)
		b.WriteString(`" /></t:DeleteItemField>`)
		return
	}
	b.WriteString(`<t:SetItemField><t:FieldURI FieldURI="contacts:`)
	b.WriteString(name)
	b.WriteString(`" /><t:Contact><t:`)
	b.WriteString(name)
	b.WriteString(`>`)
	b.WriteString(xmlEscapeText(clean))
	b.WriteString(`</t:`)
	b.WriteString(name)
	b.WriteString(`></t:Contact></t:SetItemField>`)
}

func appendContactCollectionUpdate(b *strings.Builder, fieldURI string, writeCollection func(*strings.Builder), isEmpty bool) {
	if isEmpty {
		b.WriteString(`<t:DeleteItemField><t:FieldURI FieldURI="`)
		b.WriteString(fieldURI)
		b.WriteString(`" /></t:DeleteItemField>`)
		return
	}
	b.WriteString(`<t:SetItemField><t:FieldURI FieldURI="`)
	b.WriteString(fieldURI)
	b.WriteString(`" /><t:Contact>`)
	writeCollection(b)
	b.WriteString(`</t:Contact></t:SetItemField>`)
}

type updateContactItemEnvelope struct {
	Body struct {
		UpdateItemResponse struct {
			ResponseMessages struct {
				Message struct {
					ResponseCode string `xml:"ResponseCode"`
					Items        struct {
						Values []contactItemXML `xml:",any"`
					} `xml:"Items"`
				} `xml:"UpdateItemResponseMessage"`
			} `xml:"ResponseMessages"`
		} `xml:"UpdateItemResponse"`
	} `xml:"Body"`
}

func (e *updateContactItemEnvelope) responseCode() string {
	return strings.TrimSpace(e.Body.UpdateItemResponse.ResponseMessages.Message.ResponseCode)
}

// contactEnvelopeResponseCode resolves the ResponseCode field on every
// contact-specific SOAP envelope. ews_04.go dispatches to this helper as a
// fallback so the core responseCode switch stays compact and new envelope
// types land next to their XML definitions.
func contactEnvelopeResponseCode(target any) string {
	switch typed := target.(type) {
	case *updateContactItemEnvelope:
		return typed.responseCode()
	case *getContactItemEnvelope:
		return typed.responseCode()
	case *resolveNamesEnvelope:
		return typed.responseCode()
	default:
		return ""
	}
}

type getContactItemEnvelope struct {
	Body struct {
		GetItemResponse struct {
			ResponseMessages struct {
				Message struct {
					ResponseCode string `xml:"ResponseCode"`
					Items        struct {
						Contacts []contactItemXML `xml:"Contact"`
					} `xml:"Items"`
				} `xml:"GetItemResponseMessage"`
			} `xml:"ResponseMessages"`
		} `xml:"GetItemResponse"`
	} `xml:"Body"`
}

func (e *getContactItemEnvelope) responseCode() string {
	return strings.TrimSpace(e.Body.GetItemResponse.ResponseMessages.Message.ResponseCode)
}

type resolveNamesEnvelope struct {
	Body struct {
		ResolveNamesResponse struct {
			ResponseMessages struct {
				Message struct {
					ResponseCode  string `xml:"ResponseCode"`
					ResolutionSet struct {
						TotalItemsInView        int  `xml:"TotalItemsInView,attr"`
						IncludesLastItemInRange bool `xml:"IncludesLastItemInRange,attr"`
						Resolutions             []struct {
							Mailbox mailboxXML      `xml:"Mailbox"`
							Contact *contactItemXML `xml:"Contact"`
						} `xml:"Resolution"`
					} `xml:"ResolutionSet"`
				} `xml:"ResolveNamesResponseMessage"`
			} `xml:"ResponseMessages"`
		} `xml:"ResolveNamesResponse"`
	} `xml:"Body"`
}

func (e *resolveNamesEnvelope) responseCode() string {
	return strings.TrimSpace(e.Body.ResolveNamesResponse.ResponseMessages.Message.ResponseCode)
}

func (e *resolveNamesEnvelope) toResolvedContacts() []ResolvedContact {
	raw := e.Body.ResolveNamesResponse.ResponseMessages.Message.ResolutionSet.Resolutions
	out := make([]ResolvedContact, 0, len(raw))
	for _, entry := range raw {
		resolved := ResolvedContact{Mailbox: entry.Mailbox.toMailbox()}
		if entry.Contact != nil {
			resolved.Contact = entry.Contact.toContactItem()
		}
		out = append(out, resolved)
	}
	return out
}

type contactItemXML struct {
	XMLName        xml.Name        `xml:"Contact"`
	ItemID         folderIDXMLNode `xml:"ItemId"`
	ParentFolderID folderIDXMLNode `xml:"ParentFolderId"`
	DisplayName    string          `xml:"DisplayName"`
	GivenName      string          `xml:"GivenName"`
	Surname        string          `xml:"Surname"`
	CompanyName    string          `xml:"CompanyName"`
	JobTitle       string          `xml:"JobTitle"`
	Department     string          `xml:"Department"`
	Birthday       string          `xml:"Birthday"`
	Body           struct {
		Type  string `xml:"BodyType,attr"`
		Value string `xml:",chardata"`
	} `xml:"Body"`
	EmailAddresses struct {
		Entries []contactEmailEntryXML `xml:"Entry"`
	} `xml:"EmailAddresses"`
	PhoneNumbers struct {
		Entries []labeledStringXML `xml:"Entry"`
	} `xml:"PhoneNumbers"`
	PhysicalAddresses struct {
		Entries []physicalAddressEntryXML `xml:"Entry"`
	} `xml:"PhysicalAddresses"`
}

type contactEmailEntryXML struct {
	Key   string `xml:"Key,attr"`
	Name  string `xml:"Name"`
	Value string `xml:",chardata"`
}

type physicalAddressEntryXML struct {
	Key             string `xml:"Key,attr"`
	Street          string `xml:"Street"`
	City            string `xml:"City"`
	State           string `xml:"State"`
	PostalCode      string `xml:"PostalCode"`
	CountryOrRegion string `xml:"CountryOrRegion"`
}

func (c contactItemXML) toContactItem() ContactItem {
	out := ContactItem{
		ID:             strings.TrimSpace(c.ItemID.ID),
		ChangeKey:      strings.TrimSpace(c.ItemID.ChangeKey),
		ParentFolderID: strings.TrimSpace(c.ParentFolderID.ID),
		DisplayName:    strings.TrimSpace(c.DisplayName),
		GivenName:      strings.TrimSpace(c.GivenName),
		Surname:        strings.TrimSpace(c.Surname),
		CompanyName:    strings.TrimSpace(c.CompanyName),
		JobTitle:       strings.TrimSpace(c.JobTitle),
		Department:     strings.TrimSpace(c.Department),
		Notes:          strings.TrimSpace(c.Body.Value),
	}
	if birthday := parseTime(c.Birthday); !birthday.IsZero() {
		bd := birthday
		out.Birthday = &bd
	}
	for _, entry := range c.EmailAddresses.Entries {
		value := strings.TrimSpace(entry.Value)
		key := strings.TrimSpace(entry.Key)
		if value == "" {
			continue
		}
		out.EmailAddresses = append(out.EmailAddresses, ContactEmail{Key: key, Value: value})
	}
	for _, entry := range c.PhoneNumbers.Entries {
		value := strings.TrimSpace(entry.Value)
		key := strings.TrimSpace(entry.Key)
		if value == "" {
			continue
		}
		out.PhoneNumbers = append(out.PhoneNumbers, ContactPhone{Key: key, Value: value})
	}
	for _, entry := range c.PhysicalAddresses.Entries {
		address := ContactPhysicalAddress{
			Key:             strings.TrimSpace(entry.Key),
			Street:          strings.TrimSpace(entry.Street),
			City:            strings.TrimSpace(entry.City),
			State:           strings.TrimSpace(entry.State),
			PostalCode:      strings.TrimSpace(entry.PostalCode),
			CountryOrRegion: strings.TrimSpace(entry.CountryOrRegion),
		}
		if address.Key == "" {
			continue
		}
		if address.Street == "" && address.City == "" && address.State == "" && address.PostalCode == "" && address.CountryOrRegion == "" {
			continue
		}
		out.PhysicalAddresses = append(out.PhysicalAddresses, address)
	}
	return out
}
