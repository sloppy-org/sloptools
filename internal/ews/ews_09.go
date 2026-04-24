package ews

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// MeetingResponseKind maps to the EWS AcceptItem/TentativelyAcceptItem/DeclineItem elements.
type MeetingResponseKind string

const (
	MeetingAccept            MeetingResponseKind = "accept"
	MeetingTentativelyAccept MeetingResponseKind = "tentatively_accept"
	MeetingDecline           MeetingResponseKind = "decline"
)

// MeetingInvitations controls whether meeting invitations are sent when
// creating a calendar item.
type MeetingInvitations string

const (
	SendToNone           MeetingInvitations = "SendToNone"
	SendOnlyToAll        MeetingInvitations = "SendOnlyToAll"
	SendToAllAndSaveCopy MeetingInvitations = "SendToAllAndSaveCopy"
)

// MeetingInvitationsOrCancellations controls whether meeting responses or
// cancellations are sent when updating a calendar item.
type MeetingInvitationsOrCancellations string

const (
	SendToNoneIOC           MeetingInvitationsOrCancellations = "SendToNone"
	SendOnlyToChanged       MeetingInvitationsOrCancellations = "SendToAllChanged"
	SendToAllAndSaveCopyIOC MeetingInvitationsOrCancellations = "SendToAllAndSaveCopy"
)

// MeetingCancellations controls whether meeting cancellations are sent when
// deleting a calendar item.
type MeetingCancellations string

const (
	SendToNoneCancel           MeetingCancellations = "SendToNone"
	SendOnlyToAllCancel        MeetingCancellations = "SendToAll"
	SendToAllAndSaveCopyCancel MeetingCancellations = "SendToAllAndSaveCopy"
)

// CalendarItemInput carries the fields needed to create a calendar item.
type CalendarItemInput struct {
	Subject           string
	Body              string
	BodyType          string
	Location          string
	Start             time.Time
	End               time.Time
	IsAllDay          bool
	Organizer         Mailbox
	RequiredAttendees []Mailbox
	OptionalAttendees []Mailbox
	Recurrence        string
	ICSUID            string
	ReminderMinutes   int
}

// CalendarItem is the fully populated calendar item returned by EWS.
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
	Organizer         Mailbox
	RequiredAttendees []Mailbox
	OptionalAttendees []Mailbox
	Recurrence        string
	ICSUID            string
	Status            string
	ReminderMinutes   int
}

// CalendarItemUpdate carries partial updates for an existing calendar item.
type CalendarItemUpdate struct {
	Subject           *string
	Body              *string
	Location          *string
	Start             *time.Time
	End               *time.Time
	IsAllDay          *bool
	RequiredAttendees *[]Mailbox
	OptionalAttendees *[]Mailbox
	ReminderMinutes   *int
}

// TimeRange is a half-open [Start, End) window for calendar queries.
type TimeRange struct {
	Start time.Time
	End   time.Time
}

// CreateCalendarItem creates a calendar item on the server and returns the
// new item ID and change key. sendInvitations controls whether meeting
// invitations are dispatched to attendees.
func (c *Client) CreateCalendarItem(ctx context.Context, parentFolderID string, item CalendarItemInput, sendInvitations MeetingInvitations) (itemID, changeKey string, err error) {
	body := `<m:CreateItem MessageDisposition="SendOnly">`
	body += `<m:SavedItemFolderId>` + folderIDXML(folderIDOrDistinguished(parentFolderID, "calendar")) + `</m:SavedItemFolderId>`
	body += `<m:Items>` + calendarItemCreateXML(item) + `</m:Items>`
	body += fmt.Sprintf(`<m:SendMeetingInvitations>%s</m:SendMeetingInvitations>`, sendInvitations)
	body += `</m:CreateItem>`
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

// UpdateCalendarItem updates an existing calendar item and returns the new
// change key. sendInvitationsOrCancellations controls meeting response
// dispatch behaviour.
func (c *Client) UpdateCalendarItem(ctx context.Context, itemID, changeKey string, updates CalendarItemUpdate, sendInvitationsOrCancellations MeetingInvitationsOrCancellations) (newChangeKey string, err error) {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return "", fmt.Errorf("ews UpdateCalendarItem: item id is required")
	}
	changes := calendarItemUpdateXML(updates)
	if strings.TrimSpace(changes) == "" {
		return strings.TrimSpace(changeKey), nil
	}
	var b strings.Builder
	b.WriteString(`<m:UpdateItem MessageDisposition="SaveOnly" ConflictResolution="AlwaysOverwrite">`)
	b.WriteString(`<m:ItemChanges><t:ItemChange><t:ItemId Id="`)
	b.WriteString(xmlEscapeAttr(itemID))
	if ck := strings.TrimSpace(changeKey); ck != "" {
		b.WriteString(`" ChangeKey="`)
		b.WriteString(xmlEscapeAttr(ck))
	}
	b.WriteString(`" /><t:Updates>`)
	b.WriteString(changes)
	b.WriteString(`</t:Updates></t:ItemChange></m:ItemChanges>`)
	b.WriteString(fmt.Sprintf(`<m:SendMeetingInvitationsOrCancellations>%s</m:SendMeetingInvitationsOrCancellations>`, sendInvitationsOrCancellations))
	b.WriteString(`</m:UpdateItem>`)
	var resp updateItemEnvelope
	if err := c.call(ctx, "UpdateItem", b.String(), &resp); err != nil {
		return "", err
	}
	_ = resp
	return strings.TrimSpace(changeKey), nil
}

// DeleteCalendarItem removes a calendar item. sendCancellations controls
// whether meeting cancellations are dispatched.
func (c *Client) DeleteCalendarItem(ctx context.Context, itemID string, sendCancellations MeetingCancellations) error {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return fmt.Errorf("ews DeleteCalendarItem: item id is required")
	}
	body := fmt.Sprintf(`<m:DeleteItem DeleteType="HardDelete" SendMeetingCancellations="%s" AffectedTaskOccurrences="AllOccurrences"><m:ItemIds><t:ItemId Id="%s" /></m:ItemIds></m:DeleteItem>`,
		sendCancellations, xmlEscapeAttr(itemID))
	var resp simpleResponseEnvelope
	return c.call(ctx, "DeleteItem", body, &resp)
}

// GetCalendarItem fetches a single calendar item by id.
func (c *Client) GetCalendarItem(ctx context.Context, itemID string) (CalendarItem, error) {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return CalendarItem{}, fmt.Errorf("ews GetCalendarItem: item id is required")
	}
	var resp getItemEnvelope
	if err := c.call(ctx, "GetItem", getItemBody([]string{itemID}, true), &resp); err != nil {
		return CalendarItem{}, err
	}
	items := resp.Body.GetItemResponse.ResponseMessages.Message.Items.Values
	if len(items) == 0 {
		return CalendarItem{}, fmt.Errorf("ews GetCalendarItem: item %q not found", itemID)
	}
	return items[0].toCalendarItem(), nil
}

// ListCalendarItems enumerates calendar items in the given folder whose start
// falls inside rng. It uses a CalendarView so the server expands recurring
// occurrences within the window.
func (c *Client) ListCalendarItems(ctx context.Context, folderID string, rng TimeRange) ([]CalendarItem, error) {
	folderID = folderIDOrDistinguished(folderID, "calendar")
	body := fmt.Sprintf(`<m:FindItem Traversal="Shallow">
		<m:ItemShape><t:BaseShape>AllProperties</t:BaseShape><t:BodyType>Text</t:BodyType></m:ItemShape>
		<m:CalendarView StartDate="%s" EndDate="%s" />
		<m:ParentFolderIds>%s</m:ParentFolderIds>
	</m:FindItem>`,
		rng.Start.UTC().Format(time.RFC3339),
		rng.End.UTC().Format(time.RFC3339),
		folderIDXML(folderID))
	var resp findItemEnvelope
	if err := c.call(ctx, "FindItem", body, &resp); err != nil {
		return nil, err
	}
	root := resp.Body.FindItemResponse.ResponseMessages.Message.Root
	items := root.Items.Items
	out := make([]CalendarItem, 0, len(items))
	for _, raw := range items {
		if !strings.EqualFold(raw.XMLName.Local, "CalendarItem") {
			continue
		}
		// Re-fetch with full properties via GetItem since FindItem with
		// CalendarView may return id-only results depending on server config.
		cal, err := c.GetCalendarItem(ctx, raw.ItemID.ID)
		if err != nil {
			continue
		}
		out = append(out, cal)
	}
	return out, nil
}

// CreateMeetingResponse sends an accept/decline/tentative response to a
// meeting invitation identified by referenceItemID.
func (c *Client) CreateMeetingResponse(ctx context.Context, referenceItemID, changeKey string, kind MeetingResponseKind, body string) error {
	refID := strings.TrimSpace(referenceItemID)
	if refID == "" {
		return fmt.Errorf("ews CreateMeetingResponse: reference item id is required")
	}
	var elementName string
	switch kind {
	case MeetingAccept:
		elementName = "AcceptItem"
	case MeetingTentativelyAccept:
		elementName = "TentativelyAcceptItem"
	case MeetingDecline:
		elementName = "DeclineItem"
	default:
		return fmt.Errorf("ews CreateMeetingResponse: unknown kind %q", kind)
	}
	var b strings.Builder
	b.WriteString(`<m:CreateItem MessageDisposition="SendOnly">`)
	b.WriteString(`<m:Items>`)
	b.WriteString(fmt.Sprintf(`<t:%s>`, elementName))
	b.WriteString(`<t:ReferenceItemId>`)
	b.WriteString(`<t:ItemId Id="`)
	b.WriteString(xmlEscapeAttr(refID))
	if ck := strings.TrimSpace(changeKey); ck != "" {
		b.WriteString(`" ChangeKey="`)
		b.WriteString(xmlEscapeAttr(ck))
	}
	b.WriteString(`" /></t:ItemId></t:ReferenceItemId>`)
	if strings.TrimSpace(body) != "" {
		b.WriteString(`<t:Body BodyType="Text">`)
		b.WriteString(xmlEscapeText(body))
		b.WriteString(`</t:Body>`)
	}
	b.WriteString(fmt.Sprintf(`</t:%s>`, elementName))
	b.WriteString(`</m:Items></m:CreateItem>`)
	var resp createItemEnvelope
	return c.call(ctx, "CreateItem", b.String(), &resp)
}

func calendarItemCreateXML(item CalendarItemInput) string {
	var b strings.Builder
	b.WriteString(`<t:CalendarItem>`)
	writeOptionalElement(&b, "Subject", item.Subject)
	writeOptionalElement(&b, "Body", item.Body)
	if item.BodyType != "" {
		b.WriteString(`<t:Body BodyType="`)
		b.WriteString(item.BodyType)
		b.WriteString(`">`)
		b.WriteString(xmlEscapeText(item.Body))
		b.WriteString(`</t:Body>`)
	}
	writeOptionalElement(&b, "Location", item.Location)
	if !item.Start.IsZero() {
		b.WriteString(`<t:Start>`)
		b.WriteString(item.Start.UTC().Format(time.RFC3339))
		b.WriteString(`</t:Start>`)
	}
	if !item.End.IsZero() {
		b.WriteString(`<t:End>`)
		b.WriteString(item.End.UTC().Format(time.RFC3339))
		b.WriteString(`</t:End>`)
	}
	if item.IsAllDay {
		b.WriteString(`<t:IsAllDay>true</t:IsAllDay>`)
	}
	if item.Organizer.Email != "" {
		b.WriteString(`<t:Organizer><t:Mailbox><t:EmailAddress>`)
		b.WriteString(xmlEscapeText(item.Organizer.Email))
		if item.Organizer.Name != "" {
			b.WriteString(`</t:EmailAddress><t:Name>`)
			b.WriteString(xmlEscapeText(item.Organizer.Name))
		}
		b.WriteString(`</t:Name></t:Mailbox></t:Organizer>`)
	}
	for _, att := range item.RequiredAttendees {
		b.WriteString(`<t:RequiredAttendees><t:Mailbox><t:EmailAddress>`)
		b.WriteString(xmlEscapeText(att.Email))
		if att.Name != "" {
			b.WriteString(`</t:EmailAddress><t:Name>`)
			b.WriteString(xmlEscapeText(att.Name))
		}
		b.WriteString(`</t:Name></t:Mailbox></t:RequiredAttendees>`)
	}
	for _, att := range item.OptionalAttendees {
		b.WriteString(`<t:OptionalAttendees><t:Mailbox><t:EmailAddress>`)
		b.WriteString(xmlEscapeText(att.Email))
		if att.Name != "" {
			b.WriteString(`</t:EmailAddress><t:Name>`)
			b.WriteString(xmlEscapeText(att.Name))
		}
		b.WriteString(`</t:Name></t:Mailbox></t:OptionalAttendees>`)
	}
	if item.Recurrence != "" {
		b.WriteString(`<t:Recurrence>`)
		b.WriteString(item.Recurrence)
		b.WriteString(`</t:Recurrence>`)
	}
	if item.ICSUID != "" {
		b.WriteString(`<t:Uid>`)
		b.WriteString(xmlEscapeText(item.ICSUID))
		b.WriteString(`</t:Uid>`)
	}
	if item.ReminderMinutes > 0 {
		b.WriteString(`<t:ReminderIsSet>true</t:ReminderIsSet>`)
		b.WriteString(`<t:ReminderMinutesBeforeStart>`)
		b.WriteString(fmt.Sprintf("%d", item.ReminderMinutes))
		b.WriteString(`</t:ReminderMinutesBeforeStart>`)
	}
	b.WriteString(`</t:CalendarItem>`)
	return b.String()
}

func calendarItemUpdateXML(updates CalendarItemUpdate) string {
	var b strings.Builder
	if updates.Subject != nil {
		if clean := strings.TrimSpace(*updates.Subject); clean == "" {
			b.WriteString(`<t:DeleteItemField><t:FieldURI FieldURI="calendar:Subject" /></t:DeleteItemField>`)
		} else {
			b.WriteString(`<t:SetItemField><t:FieldURI FieldURI="calendar:Subject" /><t:CalendarItem><t:Subject>`)
			b.WriteString(xmlEscapeText(clean))
			b.WriteString(`</t:Subject></t:CalendarItem></t:SetItemField>`)
		}
	}
	if updates.Body != nil {
		if clean := strings.TrimSpace(*updates.Body); clean == "" {
			b.WriteString(`<t:DeleteItemField><t:FieldURI FieldURI="calendar:Body" /></t:DeleteItemField>`)
		} else {
			b.WriteString(`<t:SetItemField><t:FieldURI FieldURI="calendar:Body" /><t:CalendarItem><t:Body BodyType="Text">`)
			b.WriteString(xmlEscapeText(clean))
			b.WriteString(`</t:Body></t:CalendarItem></t:SetItemField>`)
		}
	}
	if updates.Location != nil {
		if clean := strings.TrimSpace(*updates.Location); clean == "" {
			b.WriteString(`<t:DeleteItemField><t:FieldURI FieldURI="calendar:Location" /></t:DeleteItemField>`)
		} else {
			b.WriteString(`<t:SetItemField><t:FieldURI FieldURI="calendar:Location" /><t:CalendarItem><t:Location>`)
			b.WriteString(xmlEscapeText(clean))
			b.WriteString(`</t:Location></t:CalendarItem></t:SetItemField>`)
		}
	}
	if updates.Start != nil && !updates.Start.IsZero() {
		b.WriteString(`<t:SetItemField><t:FieldURI FieldURI="calendar:Start" /><t:CalendarItem><t:Start>`)
		b.WriteString(updates.Start.UTC().Format(time.RFC3339))
		b.WriteString(`</t:Start></t:CalendarItem></t:SetItemField>`)
	}
	if updates.End != nil && !updates.End.IsZero() {
		b.WriteString(`<t:SetItemField><t:FieldURI FieldURI="calendar:End" /><t:CalendarItem><t:End>`)
		b.WriteString(updates.End.UTC().Format(time.RFC3339))
		b.WriteString(`</t:End></t:CalendarItem></t:SetItemField>`)
	}
	if updates.IsAllDay != nil {
		val := "false"
		if *updates.IsAllDay {
			val = "true"
		}
		b.WriteString(`<t:SetItemField><t:FieldURI FieldURI="calendar:IsAllDayEvent" /><t:CalendarItem><t:IsAllDayEvent>`)
		b.WriteString(val)
		b.WriteString(`</t:IsAllDayEvent></t:CalendarItem></t:SetItemField>`)
	}
	if updates.RequiredAttendees != nil {
		cleaned := filterMailboxes(*updates.RequiredAttendees)
		if len(cleaned) == 0 {
			b.WriteString(`<t:DeleteItemField><t:FieldURI FieldURI="calendar:RequiredAttendees" /></t:DeleteItemField>`)
		} else {
			b.WriteString(`<t:SetItemField><t:FieldURI FieldURI="calendar:RequiredAttendees" /><t:CalendarItem><t:RequiredAttendees>`)
			for _, m := range cleaned {
				b.WriteString(`<t:Mailbox><t:EmailAddress>`)
				b.WriteString(xmlEscapeText(m.Email))
				if m.Name != "" {
					b.WriteString(`</t:EmailAddress><t:Name>`)
					b.WriteString(xmlEscapeText(m.Name))
				}
				b.WriteString(`</t:Name></t:Mailbox>`)
			}
			b.WriteString(`</t:RequiredAttendees></t:CalendarItem></t:SetItemField>`)
		}
	}
	if updates.OptionalAttendees != nil {
		cleaned := filterMailboxes(*updates.OptionalAttendees)
		if len(cleaned) == 0 {
			b.WriteString(`<t:DeleteItemField><t:FieldURI FieldURI="calendar:OptionalAttendees" /></t:DeleteItemField>`)
		} else {
			b.WriteString(`<t:SetItemField><t:FieldURI FieldURI="calendar:OptionalAttendees" /><t:CalendarItem><t:OptionalAttendees>`)
			for _, m := range cleaned {
				b.WriteString(`<t:Mailbox><t:EmailAddress>`)
				b.WriteString(xmlEscapeText(m.Email))
				if m.Name != "" {
					b.WriteString(`</t:EmailAddress><t:Name>`)
					b.WriteString(xmlEscapeText(m.Name))
				}
				b.WriteString(`</t:Name></t:Mailbox>`)
			}
			b.WriteString(`</t:OptionalAttendees></t:CalendarItem></t:SetItemField>`)
		}
	}
	if updates.ReminderMinutes != nil {
		if *updates.ReminderMinutes <= 0 {
			b.WriteString(`<t:SetItemField><t:FieldURI FieldURI="calendar:ReminderIsSet" /><t:CalendarItem><t:ReminderIsSet>false</t:ReminderIsSet></t:CalendarItem></t:SetItemField>`)
		} else {
			b.WriteString(`<t:SetItemField><t:FieldURI FieldURI="calendar:ReminderIsSet" /><t:CalendarItem><t:ReminderIsSet>true</t:ReminderIsSet></t:CalendarItem></t:SetItemField>`)
			b.WriteString(`<t:SetItemField><t:FieldURI FieldURI="calendar:ReminderMinutesBeforeStart" /><t:CalendarItem><t:ReminderMinutesBeforeStart>`)
			b.WriteString(fmt.Sprintf("%d", *updates.ReminderMinutes))
			b.WriteString(`</t:ReminderMinutesBeforeStart></t:CalendarItem></t:SetItemField>`)
		}
	}
	return b.String()
}

func filterMailboxes(mboxes []Mailbox) []Mailbox {
	out := make([]Mailbox, 0, len(mboxes))
	for _, m := range mboxes {
		if strings.TrimSpace(m.Email) != "" {
			out = append(out, m)
		}
	}
	return out
}

func (i itemXML) toCalendarItem() CalendarItem {
	return CalendarItem{
		ID:                strings.TrimSpace(i.ItemID.ID),
		ChangeKey:         strings.TrimSpace(i.ItemID.ChangeKey),
		ParentFolderID:    strings.TrimSpace(i.ParentFolderID.ID),
		Subject:           strings.TrimSpace(i.Subject),
		Body:              strings.TrimSpace(i.Body.Value),
		BodyType:          strings.TrimSpace(i.Body.Type),
		Location:          strings.TrimSpace(i.Location),
		Start:             parseTime(i.Start),
		End:               parseTime(i.End),
		IsAllDay:          parseBool(i.IsAllDayEvent),
		Organizer:         i.From.Mailbox.toMailbox(),
		RequiredAttendees: mailboxSlice(i.ToRecipients.Mailboxes),
		OptionalAttendees: mailboxSlice(i.CcRecipients.Mailboxes),
		ICSUID:            strings.TrimSpace(i.ICalUID),
		Status:            strings.TrimSpace(i.Status),
	}
}

func (i itemXML) getICalUID() string {
	return strings.TrimSpace(i.ICalUID)
}

type getCalendarItemEnvelope struct {
	Body struct {
		GetItemResponse struct {
			ResponseMessages struct {
				Message struct {
					ResponseCode string `xml:"ResponseCode"`
					Items        struct {
						Values []itemXML `xml:",any"`
					} `xml:"Items"`
				} `xml:"GetItemResponseMessage"`
			} `xml:"ResponseMessages"`
		} `xml:"GetItemResponse"`
	} `xml:"Body"`
}

func (e *getCalendarItemEnvelope) responseCode() string {
	return strings.TrimSpace(e.Body.GetItemResponse.ResponseMessages.Message.ResponseCode)
}
