package ews

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type CalendarItemInput struct {
	Subject           string
	Body              string
	BodyType          string
	Location          string
	Start             time.Time
	End               time.Time
	IsAllDay          bool
	UID               string
	OrganizerEmail    string
	OrganizerName     string
	RequiredAttendees []EventAttendee
	OptionalAttendees []EventAttendee
	Recurrence        string
	SendInvitations   string
}

type CalendarItemUpdate struct {
	Subject           *string
	Body              *string
	BodyType          *string
	Location          *string
	Start             *time.Time
	End               *time.Time
	IsAllDay          *bool
	OrganizerEmail    *string
	RequiredAttendees *[]EventAttendee
	OptionalAttendees *[]EventAttendee
	Recurrence        *string
	SendCancellations string
}

type MeetingResponseKind string

const (
	MeetingAccept            MeetingResponseKind = "accept"
	MeetingTentativelyAccept MeetingResponseKind = "tentative"
	MeetingDecline           MeetingResponseKind = "decline"
)

func (c *Client) CreateCalendarItem(ctx context.Context, parentFolderID string, item CalendarItemInput, sendInvitations string) (string, string, error) {
	body := `<m:CreateItem MessageDisposition="SaveOnly" ` +
		`SendMeetingInvitations="` + xmlEscapeAttr(sendInvitations) + `">` +
		`<m:SavedItemFolderId>` + folderIDXML(folderIDOrDistinguished(parentFolderID, "calendar")) +
		`</m:SavedItemFolderId><m:Items>` + calendarItemCreateXML(item) + `</m:Items></m:CreateItem>`
	var resp createItemEnvelope
	if err := c.call(ctx, "CreateItem", body, &resp); err != nil {
		return "", "", err
	}
	items := resp.Body.CreateItemResponse.ResponseMessages.Message.Items.Values
	if len(items) == 0 {
		return "", "", fmt.Errorf("ews CreateCalendarItem returned no items")
	}
	return strings.TrimSpace(items[0].ItemID.ID), strings.TrimSpace(items[0].ItemID.ChangeKey), nil
}

func (c *Client) UpdateCalendarItem(ctx context.Context, itemID, changeKey string, updates CalendarItemUpdate, sendInvitationsOrCancellations string) (string, error) {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return "", fmt.Errorf("ews UpdateCalendarItem: item id is required")
	}
	changes := calendarItemUpdateXML(updates)
	if strings.TrimSpace(changes) == "" {
		return strings.TrimSpace(changeKey), nil
	}
	var b strings.Builder
	b.WriteString(`<m:UpdateItem MessageDisposition="SaveOnly" ConflictResolution="AlwaysOverwrite" `)
	b.WriteString(`SendMeetingInvitationsOrCancellations="`)
	b.WriteString(xmlEscapeAttr(sendInvitationsOrCancellations))
	b.WriteString(`"><m:ItemChanges><t:ItemChange><t:ItemId Id="`)
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
	var resp updateCalendarItemEnvelope
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

func (c *Client) DeleteCalendarItem(ctx context.Context, itemID string, sendCancellations string) error {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return fmt.Errorf("ews DeleteCalendarItem: item id is required")
	}
	body := `<m:DeleteItem DeleteType="HardDelete" ` +
		`SendMeetingCancellations="` + xmlEscapeAttr(sendCancellations) + `" ` +
		`AffectedTaskOccurrences="AllOccurrences">` +
		`<m:ItemIds><t:ItemId Id="` + xmlEscapeAttr(itemID) + `" /></m:ItemIds></m:DeleteItem>`
	var resp simpleResponseEnvelope
	return c.call(ctx, "DeleteItem", body, &resp)
}

func (c *Client) GetCalendarItem(ctx context.Context, itemID string) (CalendarItem, error) {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return CalendarItem{}, fmt.Errorf("ews GetCalendarItem: item id is required")
	}
	var resp getCalendarItemEnvelope
	if err := c.call(ctx, "GetItem", getItemBody([]string{itemID}, true), &resp); err != nil {
		return CalendarItem{}, err
	}
	items := resp.Body.GetItemResponse.ResponseMessages.Message.Items.CalendarItems
	if len(items) == 0 {
		return CalendarItem{}, fmt.Errorf("ews GetCalendarItem: calendar item %q not found", itemID)
	}
	return items[0].toCalendarItem(), nil
}

type TimeRange struct {
	Start time.Time
	End   time.Time
}

func (c *Client) ListCalendarItems(ctx context.Context, folderID string, rng TimeRange) ([]CalendarItem, error) {
	folderID = folderIDOrDistinguished(folderID, "calendar")
	if rng.Start.IsZero() {
		rng.Start = time.Now().AddDate(0, 0, -7)
	}
	if rng.End.IsZero() {
		rng.End = rng.Start.Add(30 * 24 * time.Hour)
	}
	body := fmt.Sprintf(`<m:FindItem Traversal="Shallow">
      <m:ItemShape><t:BaseShape>FullInformation</t:BaseShape></m:ItemShape>
      <m:IndexedPageItemView MaxEntriesReturned="%d" Offset="0" BasePoint="Beginning" />
      <m:CalendarView StartDate="%s" EndDate="%s" />
      <m:ParentFolderIds>%s</m:ParentFolderIds>
    </m:FindItem>`, c.cfg.BatchSize, rng.Start.UTC().Format(time.RFC3339), rng.End.UTC().Format(time.RFC3339), folderIDXML(folderID))
	var resp findCalendarItemEnvelope
	if err := c.call(ctx, "FindItem", body, &resp); err != nil {
		return nil, err
	}
	items := resp.Body.FindItemResponse.ResponseMessages.Message.Root.CalendarItems.Items
	out := make([]CalendarItem, 0, len(items))
	for _, raw := range items {
		out = append(out, raw.toCalendarItem())
	}
	return out, nil
}

func (c *Client) CreateMeetingResponse(ctx context.Context, referenceItemID, changeKey string, kind MeetingResponseKind, body string) error {
	referenceItemID = strings.TrimSpace(referenceItemID)
	if referenceItemID == "" {
		return fmt.Errorf("ews CreateMeetingResponse: reference item id is required")
	}
	var element string
	switch kind {
	case MeetingAccept:
		element = "AcceptItem"
	case MeetingTentativelyAccept:
		element = "TentativelyAcceptItem"
	case MeetingDecline:
		element = "DeclineItem"
	default:
		return fmt.Errorf("ews CreateMeetingResponse: unknown kind %q", kind)
	}
	var b strings.Builder
	b.WriteString(`<m:CreateItem MessageDisposition="SaveOnly" SendMeetingCancellations="SendToNone">`)
	b.WriteString(`<m:SavedItemFolderId><t:DistinguishedFolderId Id="calendar" /></m:SavedItemFolderId>`)
	b.WriteString(`<m:Items>`)
	b.WriteString(`<t:`)
	b.WriteString(element)
	b.WriteString(`>`)
	b.WriteString(`<ReferenceItemId Id="`)
	b.WriteString(xmlEscapeAttr(referenceItemID))
	if ck := strings.TrimSpace(changeKey); ck != "" {
		b.WriteString(`" ChangeKey="`)
		b.WriteString(xmlEscapeAttr(ck))
	}
	b.WriteString(`" />`)
	if clean := strings.TrimSpace(body); clean != "" {
		b.WriteString(`<t:Body BodyType="HTML">`)
		b.WriteString(xmlEscapeText(clean))
		b.WriteString(`</t:Body>`)
	}
	b.WriteString(`</t:`)
	b.WriteString(element)
	b.WriteString(`></m:Items></m:CreateItem>`)
	var resp simpleResponseEnvelope
	return c.call(ctx, "CreateItem", b.String(), &resp)
}

type CalendarItem struct {
	ID                string
	ChangeKey         string
	ParentFolderID    string
	Subject           string
	Body              string
	BodyType          string
	Location          string
	Start             time.Time
	End               time.Time
	IsAllDay          bool
	UID               string
	Organizer         string
	RequiredAttendees []EventAttendee
	OptionalAttendees []EventAttendee
	Recurrence        string
	Status            string
}

func calendarItemCreateXML(item CalendarItemInput) string {
	var b strings.Builder
	b.WriteString(`<t:CalendarItem>`)
	writeOptionalElement(&b, "Subject", item.Subject)
	if clean := strings.TrimSpace(item.Body); clean != "" {
		bodyType := "Text"
		if item.BodyType != "" {
			bodyType = item.BodyType
		}
		b.WriteString(`<t:Body BodyType="`)
		b.WriteString(bodyType)
		b.WriteString(`">`)
		b.WriteString(xmlEscapeText(clean))
		b.WriteString(`</t:Body>`)
	}
	writeOptionalElement(&b, "Location", item.Location)
	if !item.IsAllDay {
		b.WriteString(`<t:Start>`)
		b.WriteString(item.Start.UTC().Format(time.RFC3339))
		b.WriteString(`</t:Start>`)
		b.WriteString(`<t:End>`)
		b.WriteString(item.End.UTC().Format(time.RFC3339))
		b.WriteString(`</t:End>`)
	} else {
		b.WriteString(`<t:Start>`)
		b.WriteString(item.Start.UTC().Format("2006-01-02"))
		b.WriteString(`</t:Start>`)
		b.WriteString(`<t:End>`)
		b.WriteString(item.End.UTC().Format("2006-01-02"))
		b.WriteString(`</t:End>`)
		b.WriteString(`<t:IsAllDayEvent>true</t:IsAllDayEvent>`)
	}
	if clean := strings.TrimSpace(item.UID); clean != "" {
		b.WriteString(`<t:UID>`)
		b.WriteString(xmlEscapeText(clean))
		b.WriteString(`</t:UID>`)
	}
	if clean := strings.TrimSpace(item.OrganizerEmail); clean != "" {
		b.WriteString(`<t:Organizer><t:Mailbox><t:Name>`)
		b.WriteString(xmlEscapeText(item.OrganizerName))
		b.WriteString(`</t:Name><t:EmailAddress>`)
		b.WriteString(xmlEscapeText(clean))
		b.WriteString(`</t:EmailAddress></t:Mailbox></t:Organizer>`)
	}
	writeEventAttendees(&b, "RequiredAttendees", item.RequiredAttendees)
	writeEventAttendees(&b, "OptionalAttendees", item.OptionalAttendees)
	if clean := strings.TrimSpace(item.Recurrence); clean != "" {
		b.WriteString(`<t:Recurrence>`)
		b.WriteString(xmlEscapeText(clean))
		b.WriteString(`</t:Recurrence>`)
	}
	b.WriteString(`</t:CalendarItem>`)
	return b.String()
}

func writeEventAttendees(b *strings.Builder, name string, attendees []EventAttendee) {
	clean := filterEventAttendees(attendees)
	if len(clean) == 0 {
		return
	}
	b.WriteString(`<t:`)
	b.WriteString(name)
	b.WriteString(`>`)
	for _, a := range clean {
		b.WriteString(`<t:Mailbox><t:Name>`)
		b.WriteString(xmlEscapeText(strings.TrimSpace(a.Name)))
		b.WriteString(`</t:Name><t:EmailAddress>`)
		b.WriteString(xmlEscapeText(strings.TrimSpace(a.Email)))
		b.WriteString(`</t:EmailAddress></t:Mailbox>`)
	}
	b.WriteString(`</t:`)
	b.WriteString(name)
	b.WriteString(`>`)
}

func calendarItemUpdateXML(updates CalendarItemUpdate) string {
	var b strings.Builder
	if updates.Subject != nil {
		writeSetItemField(&b, "Subject", "item:Subject", *updates.Subject)
	}
	if updates.Body != nil {
		bodyType := "Text"
		if updates.BodyType != nil && *updates.BodyType != "" {
			bodyType = *updates.BodyType
		}
		b.WriteString(`<t:SetItemField><t:FieldURI FieldURI="item:Body"><t:Body BodyType="`)
		b.WriteString(bodyType)
		b.WriteString(`">`)
		b.WriteString(xmlEscapeText(*updates.Body))
		b.WriteString(`</t:Body></t:CalendarItem></t:FieldURI></t:SetItemField>`)
	}
	if updates.Location != nil {
		writeSetItemField(&b, "Location", "item:Location", *updates.Location)
	}
	if updates.Start != nil && !updates.Start.IsZero() {
		b.WriteString(`<t:SetItemField><t:FieldURI FieldURI="calendar:Start"><t:CalendarItem><t:Start>`)
		b.WriteString(updates.Start.UTC().Format(time.RFC3339))
		b.WriteString(`</t:Start></t:CalendarItem></t:FieldURI></t:SetItemField>`)
	}
	if updates.End != nil && !updates.End.IsZero() {
		b.WriteString(`<t:SetItemField><t:FieldURI FieldURI="calendar:End"><t:CalendarItem><t:End>`)
		b.WriteString(updates.End.UTC().Format(time.RFC3339))
		b.WriteString(`</t:End></t:CalendarItem></t:FieldURI></t:SetItemField>`)
	}
	if updates.IsAllDay != nil {
		val := "false"
		if *updates.IsAllDay {
			val = "true"
		}
		b.WriteString(`<t:SetItemField><t:FieldURI FieldURI="calendar:IsAllDayEvent"><t:CalendarItem><t:IsAllDayEvent>`)
		b.WriteString(val)
		b.WriteString(`</t:IsAllDayEvent></t:CalendarItem></t:FieldURI></t:SetItemField>`)
	}
	if updates.RequiredAttendees != nil {
		writeAttendeesUpdate(&b, "RequiredAttendees", *updates.RequiredAttendees)
	}
	if updates.OptionalAttendees != nil {
		writeAttendeesUpdate(&b, "OptionalAttendees", *updates.OptionalAttendees)
	}
	if updates.Recurrence != nil {
		writeSetItemField(&b, "Recurrence", "calendar:Recurrence", *updates.Recurrence)
	}
	return b.String()
}

func writeAttendeesUpdate(b *strings.Builder, name string, attendees []EventAttendee) {
	clean := filterEventAttendees(attendees)
	if len(clean) == 0 {
		return
	}
	b.WriteString(`<t:SetItemField><t:FieldURI FieldURI="calendar:`)
	b.WriteString(name)
	b.WriteString(`"><t:CalendarItem>`)
	for _, a := range clean {
		b.WriteString(`<t:`)
		b.WriteString(name)
		b.WriteString(`><t:Mailbox><t:Name>`)
		b.WriteString(xmlEscapeText(strings.TrimSpace(a.Name)))
		b.WriteString(`</t:Name><t:EmailAddress>`)
		b.WriteString(xmlEscapeText(strings.TrimSpace(a.Email)))
		b.WriteString(`</t:EmailAddress></t:Mailbox></t:`)
		b.WriteString(name)
		b.WriteString(`>`)
	}
	b.WriteString(`</t:CalendarItem></t:FieldURI></t:SetItemField>`)
}

func (i itemXML) toCalendarItem() CalendarItem {
	ci := CalendarItem{
		ID:             strings.TrimSpace(i.ItemID.ID),
		ChangeKey:      strings.TrimSpace(i.ItemID.ChangeKey),
		ParentFolderID: strings.TrimSpace(i.ParentFolderID.ID),
		Subject:        strings.TrimSpace(i.Subject),
		Body:           strings.TrimSpace(i.Body.Value),
		BodyType:       strings.TrimSpace(i.Body.Type),
		Location:       strings.TrimSpace(i.Location),
		Start:          parseTime(i.Start),
		End:            parseTime(i.End),
		IsAllDay:       parseBool(i.IsAllDayEvent),
		UID:            strings.TrimSpace(i.UID),
		Status:         strings.TrimSpace(i.Status),
		Recurrence:     strings.TrimSpace(i.Recurrence),
	}
	if clean := strings.TrimSpace(i.Organizer.Mailbox.Email); clean != "" {
		ci.Organizer = clean
	}
	for _, m := range i.RequiredAttendees.Mailboxes {
		ci.RequiredAttendees = append(ci.RequiredAttendees, EventAttendee{Email: m.Email, Name: m.Name, Required: true})
	}
	for _, m := range i.OptionalAttendees.Mailboxes {
		ci.OptionalAttendees = append(ci.OptionalAttendees, EventAttendee{Email: m.Email, Name: m.Name, Required: false})
	}
	return ci
}

func filterEventAttendees(attendees []EventAttendee) []EventAttendee {
	out := make([]EventAttendee, 0, len(attendees))
	for _, a := range attendees {
		if strings.TrimSpace(a.Email) != "" {
			out = append(out, a)
		}
	}
	return out
}

type getCalendarItemEnvelope struct {
	Body struct {
		GetItemResponse struct {
			ResponseMessages struct {
				Message struct {
					ResponseCode string `xml:"ResponseCode"`
					Items        struct {
						CalendarItems []itemXML `xml:",any"`
					} `xml:"Items"`
				} `xml:"GetItemResponseMessage"`
			} `xml:"ResponseMessages"`
		} `xml:"GetItemResponse"`
	} `xml:"Body"`
}

type findCalendarItemEnvelope struct {
	Body struct {
		FindItemResponse struct {
			ResponseMessages struct {
				Message struct {
					ResponseCode string         `xml:"ResponseCode"`
					Root         findRootXMLCal `xml:"RootFolder"`
				} `xml:"FindItemResponseMessage"`
			} `xml:"ResponseMessages"`
		} `xml:"FindItemResponse"`
	} `xml:"Body"`
}

type findRootXMLCal struct {
	IndexedPagingOffset     int                  `xml:"IndexedPagingOffset,attr"`
	TotalItemsInView        int                  `xml:"TotalItemsInView,attr"`
	IncludesLastItemInRange bool                 `xml:"IncludesLastItemInRange,attr"`
	CalendarItems           calendarItemsWrapper `xml:"CalendarItems"`
}

type calendarItemsWrapper struct {
	Items []itemXML `xml:",any"`
}

func (c calendarItemsWrapper) toCalendarItems() []itemXML {
	return c.Items
}

type updateCalendarItemEnvelope struct {
	Body struct {
		UpdateItemResponse struct {
			ResponseMessages struct {
				Message struct {
					ResponseCode string         `xml:"ResponseCode"`
					Items        updateItemsCal `xml:"Items"`
				} `xml:"UpdateItemResponseMessage"`
			} `xml:"ResponseMessages"`
		} `xml:"UpdateItemResponse"`
	} `xml:"Body"`
}

type updateItemsCal struct {
	Values []itemXML `xml:",any"`
}

func writeSetItemField(b *strings.Builder, name, fieldURI, value string) {
	clean := strings.TrimSpace(value)
	if clean == "" {
		b.WriteString(`<t:DeleteItemField><t:FieldURI FieldURI="`)
		b.WriteString(fieldURI)
		b.WriteString(`" /></t:DeleteItemField>`)
		return
	}
	b.WriteString(`<t:SetItemField><t:FieldURI FieldURI="`)
	b.WriteString(fieldURI)
	b.WriteString(`"><t:CalendarItem><t:`)
	b.WriteString(name)
	b.WriteString(`>`)
	b.WriteString(xmlEscapeText(clean))
	b.WriteString(`</t:`)
	b.WriteString(name)
	b.WriteString(`></t:CalendarItem></t:FieldURI></t:SetItemField>`)
}
