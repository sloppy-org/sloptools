package calendar

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/ews"
	"github.com/sloppy-org/sloptools/internal/providerdata"
)

const ProviderNameEWS = "exchange_ews"

type EWSProvider struct {
	client           *ews.Client
	calendarFolderID string
}

var (
	_ Provider           = (*EWSProvider)(nil)
	_ EventMutator       = (*EWSProvider)(nil)
	_ InviteResponder    = (*EWSProvider)(nil)
	_ RecurrenceExpander = (*EWSProvider)(nil)
)

func NewEWSProvider(client *ews.Client) *EWSProvider {
	return &EWSProvider{client: client}
}

// Client exposes the underlying EWS client for tests that need to verify
// client sharing.
func (p *EWSProvider) Client() *ews.Client {
	if p == nil {
		return nil
	}
	return p.client
}

func (p *EWSProvider) ProviderName() string { return ProviderNameEWS }
func (p *EWSProvider) Close() error         { return nil }

func (p *EWSProvider) ListCalendars(ctx context.Context) ([]providerdata.Calendar, error) {
	folderID := string(ews.FolderKindCalendar)
	if strings.TrimSpace(p.calendarFolderID) != "" {
		folderID = p.calendarFolderID
	}
	return []providerdata.Calendar{{ID: folderID, Name: "Calendar", Primary: true}}, nil
}

func (p *EWSProvider) ListEvents(ctx context.Context, calendarID string, rng TimeRange) ([]providerdata.Event, error) {
	folderID := strings.TrimSpace(calendarID)
	if folderID == "" {
		folderID = string(ews.FolderKindCalendar)
	}
	items, err := p.client.ListCalendarItems(ctx, folderID, ews.TimeRange{Start: rng.Start, End: rng.End})
	if err != nil {
		return nil, fmt.Errorf("ews list calendar items: %w", err)
	}
	events := make([]providerdata.Event, 0, len(items))
	for _, item := range items {
		events = append(events, eventFromEWS(item, folderID))
	}
	sortEventsByStart(events)
	return events, nil
}

func (p *EWSProvider) GetEvent(ctx context.Context, calendarID, eventID string) (providerdata.Event, error) {
	item, err := p.client.GetCalendarItem(ctx, eventID)
	if err != nil {
		return providerdata.Event{}, fmt.Errorf("ews get calendar item: %w", err)
	}
	folderID := strings.TrimSpace(calendarID)
	if folderID == "" {
		folderID = string(ews.FolderKindCalendar)
	}
	return eventFromEWS(item, folderID), nil
}

func (p *EWSProvider) CreateEvent(ctx context.Context, calendarID string, ev providerdata.Event) (providerdata.Event, error) {
	calID := strings.TrimSpace(calendarID)
	if calID == "" {
		calID = strings.TrimSpace(ev.CalendarID)
	}
	if calID == "" {
		calID = string(ews.FolderKindCalendar)
	}
	input := calendarItemToEWS(ev)
	input.SendInvitations = sendInvitationsFlag(ev.Attendees)
	itemID, _, err := p.client.CreateCalendarItem(ctx, calID, input, input.SendInvitations)
	if err != nil {
		return providerdata.Event{}, fmt.Errorf("ews create calendar item: %w", err)
	}
	item, err := p.client.GetCalendarItem(ctx, itemID)
	if err != nil {
		return providerdata.Event{}, fmt.Errorf("ews get created item: %w", err)
	}
	return eventFromEWS(item, calID), nil
}

func (p *EWSProvider) UpdateEvent(ctx context.Context, calendarID string, ev providerdata.Event) (providerdata.Event, error) {
	existing, err := p.GetEvent(ctx, calendarID, ev.ID)
	if err != nil {
		return providerdata.Event{}, fmt.Errorf("ews get event for update: %w", err)
	}
	updates := calendarItemUpdateFromEvent(existing, ev)
	calID := strings.TrimSpace(calendarID)
	if calID == "" {
		calID = string(ews.FolderKindCalendar)
	}
	_, err = p.client.UpdateCalendarItem(ctx, ev.ID, existing.ID, updates, sendInvitationsFlag(ev.Attendees))
	if err != nil {
		return providerdata.Event{}, fmt.Errorf("ews update calendar item: %w", err)
	}
	return p.GetEvent(ctx, calID, ev.ID)
}

func (p *EWSProvider) DeleteEvent(ctx context.Context, calendarID, eventID string) error {
	return p.client.DeleteCalendarItem(ctx, eventID, "SendToNone")
}

func (p *EWSProvider) RespondToInvite(ctx context.Context, eventID string, resp providerdata.InviteResponse) error {
	status := normalizeInviteStatus(resp.Status)
	if status == "" {
		return fmt.Errorf("invite status must be one of accepted, declined, tentative")
	}
	var kind ews.MeetingResponseKind
	switch status {
	case "accepted":
		kind = ews.MeetingAccept
	case "declined":
		kind = ews.MeetingDecline
	case "tentative":
		kind = ews.MeetingTentativelyAccept
	}
	return p.client.CreateMeetingResponse(ctx, eventID, "", kind, resp.Comment)
}

func (p *EWSProvider) ExpandOccurrences(ctx context.Context, calendarID, eventID string, rng TimeRange) ([]providerdata.Event, error) {
	folderID := strings.TrimSpace(calendarID)
	if folderID == "" {
		folderID = string(ews.FolderKindCalendar)
	}
	items, err := p.client.ListCalendarItems(ctx, folderID, ews.TimeRange{Start: rng.Start, End: rng.End})
	if err != nil {
		return nil, fmt.Errorf("ews expand occurrences: %w", err)
	}
	out := make([]providerdata.Event, 0, len(items))
	for _, item := range items {
		if item.UID != "" && strings.Contains(item.UID, eventID) || item.ID == eventID {
			out = append(out, eventFromEWS(item, folderID))
		}
	}
	sortEventsByStart(out)
	return out, nil
}

func calendarItemToEWS(ev providerdata.Event) ews.CalendarItemInput {
	input := ews.CalendarItemInput{
		Subject:    ev.Summary,
		Body:       ev.Description,
		BodyType:   "HTML",
		Location:   ev.Location,
		Start:      ev.Start,
		End:        ev.End,
		IsAllDay:   ev.AllDay,
		UID:        ev.ICSUID,
		Recurrence: "",
	}
	if input.Body == "" {
		input.Body = ev.Summary
	}
	if input.BodyType == "" {
		input.BodyType = "Text"
	}
	if input.End.IsZero() {
		if ev.AllDay {
			input.End = ev.Start.Add(24 * time.Hour)
		} else {
			input.End = ev.Start.Add(time.Hour)
		}
	}
	if ev.Organizer != "" {
		input.OrganizerEmail = ev.Organizer
		input.OrganizerName = ev.Organizer
	}
	for _, att := range ev.Attendees {
		email := strings.TrimSpace(att.Email)
		if email == "" {
			continue
		}
		input.RequiredAttendees = append(input.RequiredAttendees, ews.EventAttendee{Email: email, Name: att.Name})
	}
	return input
}

func calendarItemUpdateFromEvent(existing, newEv providerdata.Event) ews.CalendarItemUpdate {
	var updates ews.CalendarItemUpdate
	if newEv.Summary != "" && newEv.Summary != existing.Summary {
		updates.Subject = &newEv.Summary
	}
	if newEv.Description != "" && newEv.Description != existing.Description {
		updates.Body = &newEv.Description
	}
	if newEv.Location != "" && newEv.Location != existing.Location {
		updates.Location = &newEv.Location
	}
	if !newEv.Start.IsZero() && !newEv.Start.Equal(existing.Start) {
		updates.Start = &newEv.Start
	}
	if !newEv.End.IsZero() && !newEv.End.Equal(existing.End) {
		updates.End = &newEv.End
	}
	if newEv.AllDay != existing.AllDay {
		updates.IsAllDay = &newEv.AllDay
	}
	if len(newEv.Attendees) > 0 {
		ewsAttendees := make([]ews.EventAttendee, 0, len(newEv.Attendees))
		for _, a := range newEv.Attendees {
			ewsAttendees = append(ewsAttendees, ews.EventAttendee{Email: a.Email, Name: a.Name, Response: a.Response})
		}
		updates.RequiredAttendees = &ewsAttendees
	}
	return updates
}

func eventFromEWS(item ews.CalendarItem, calendarID string) providerdata.Event {
	ev := providerdata.Event{
		ID:          item.ID,
		CalendarID:  calendarID,
		Summary:     item.Subject,
		Description: item.Body,
		Location:    item.Location,
		Start:       item.Start,
		End:         item.End,
		AllDay:      item.IsAllDay,
		Status:      item.Status,
		ICSUID:      item.UID,
		Recurring:   item.Recurrence != "",
	}
	if ev.Summary == "" {
		ev.Summary = "(No title)"
	}
	if item.Organizer != "" {
		ev.Organizer = item.Organizer
	}
	for _, att := range item.RequiredAttendees {
		ev.Attendees = append(ev.Attendees, providerdata.Attendee{Email: att.Email, Name: att.Name, Response: att.Response})
	}
	for _, att := range item.OptionalAttendees {
		ev.Attendees = append(ev.Attendees, providerdata.Attendee{Email: att.Email, Name: att.Name, Response: att.Response})
	}
	return ev
}

func sendInvitationsFlag(attendees []providerdata.Attendee) string {
	for _, a := range attendees {
		if strings.TrimSpace(a.Email) != "" {
			return "SendOnlyToAll"
		}
	}
	return "SendToNone"
}

func sortEventsByStart(events []providerdata.Event) {
	for i := 0; i < len(events); i++ {
		for j := i + 1; j < len(events); j++ {
			if events[j].Start.Before(events[i].Start) {
				events[i], events[j] = events[j], events[i]
			}
		}
	}
}
