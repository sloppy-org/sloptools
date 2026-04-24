package calendar

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/ews"
	"github.com/sloppy-org/sloptools/internal/providerdata"
)

// ProviderNameEWS is the stable string reported by EWSProvider so MCP
// payloads and logs can identify the backend without reflection.
const ProviderNameEWS = "exchange_ews"

// EWSProvider adapts the Exchange EWS API to the Provider plus EventMutator
// plus InviteResponder plus RecurrenceExpander contract.
type EWSProvider struct {
	client           *ews.Client
	calendarFolderID string // defaults to string(ews.FolderKindCalendar)
}

var (
	_ Provider           = (*EWSProvider)(nil)
	_ EventMutator       = (*EWSProvider)(nil)
	_ InviteResponder    = (*EWSProvider)(nil)
	_ RecurrenceExpander = (*EWSProvider)(nil)
)

// NewEWSProvider wraps an EWS client for calendar operations. calendarFolderID
// may be blank to use the distinguished "calendar" folder.
func NewEWSProvider(client *ews.Client, calendarFolderID string) *EWSProvider {
	return &EWSProvider{
		client:           client,
		calendarFolderID: strings.TrimSpace(calendarFolderID),
	}
}

// Client exposes the underlying EWS client for tests and higher tiers.
func (p *EWSProvider) Client() *ews.Client {
	if p == nil {
		return nil
	}
	return p.client
}

// ProviderName returns the stable backend identifier.
func (p *EWSProvider) ProviderName() string { return ProviderNameEWS }

// Close is a no-op; the client is owned by the groupware registry.
func (p *EWSProvider) Close() error { return nil }

// ListCalendars returns a single synthetic calendar for the primary EWS
// calendar folder. Exchange exposes one primary calendar per mailbox;
// additional mailboxes are a later feature.
func (p *EWSProvider) ListCalendars(ctx context.Context) ([]providerdata.Calendar, error) {
	folderID := p.calendarFolderID
	if folderID == "" {
		folderID = string(ews.FolderKindCalendar)
	}
	return []providerdata.Calendar{
		{ID: folderID, Name: "Calendar", Primary: true},
	}, nil
}

// ListEvents returns events in rng sorted by start time.
func (p *EWSProvider) ListEvents(ctx context.Context, calendarID string, rng TimeRange) ([]providerdata.Event, error) {
	folderID := calendarID
	if folderID == "" {
		folderID = p.calendarFolderID
	}
	if folderID == "" {
		folderID = string(ews.FolderKindCalendar)
	}
	if rng.Start.IsZero() {
		rng.Start = time.Now().AddDate(0, 0, -7)
	}
	if rng.End.IsZero() {
		rng.End = rng.Start.Add(30 * 24 * time.Hour)
	}
	rawItems, err := p.client.ListCalendarItems(ctx, folderID, ews.TimeRange{Start: rng.Start, End: rng.End})
	if err != nil {
		return nil, fmt.Errorf("list calendar items: %w", err)
	}
	events := make([]providerdata.Event, 0, len(rawItems))
	for _, item := range rawItems {
		events = append(events, calendarItemToEvent(item, folderID))
	}
	return events, nil
}

// GetEvent fetches a single event by id.
func (p *EWSProvider) GetEvent(ctx context.Context, calendarID, eventID string) (providerdata.Event, error) {
	itemID := strings.TrimSpace(eventID)
	if itemID == "" {
		return providerdata.Event{}, fmt.Errorf("event_id is required")
	}
	item, err := p.client.GetCalendarItem(ctx, itemID)
	if err != nil {
		return providerdata.Event{}, fmt.Errorf("get calendar item: %w", err)
	}
	folderID := calendarID
	if folderID == "" {
		folderID = p.calendarFolderID
	}
	if folderID == "" {
		folderID = string(ews.FolderKindCalendar)
	}
	return calendarItemToEvent(item, folderID), nil
}

// CreateEvent inserts a new event on calendarID. An empty CalendarID on the
// Event falls back to the argument; callers typically pass the calendar id
// through both for clarity.
func (p *EWSProvider) CreateEvent(ctx context.Context, calendarID string, ev providerdata.Event) (providerdata.Event, error) {
	folderID := calendarID
	if folderID == "" {
		folderID = p.calendarFolderID
	}
	if folderID == "" {
		folderID = string(ews.FolderKindCalendar)
	}
	if ev.Summary == "" {
		return providerdata.Event{}, fmt.Errorf("summary is required")
	}
	if ev.Start.IsZero() {
		return providerdata.Event{}, fmt.Errorf("start is required")
	}
	end := ev.End
	if end.IsZero() {
		if ev.AllDay {
			end = ev.Start.Add(24 * time.Hour)
		} else {
			end = ev.Start.Add(time.Hour)
		}
	}
	if !end.After(ev.Start) {
		return providerdata.Event{}, fmt.Errorf("end must be after start")
	}
	input := eventToCalendarItemInput(ev, folderID)
	sendInvitations := ews.SendToNone
	if len(ev.Attendees) > 0 {
		sendInvitations = ews.SendOnlyToAll
	}
	itemID, _, err := p.client.CreateCalendarItem(ctx, folderID, input, sendInvitations)
	if err != nil {
		return providerdata.Event{}, fmt.Errorf("create calendar event: %w", err)
	}
	item, err := p.client.GetCalendarItem(ctx, itemID)
	if err != nil {
		return providerdata.Event{}, fmt.Errorf("fetch created event: %w", err)
	}
	return calendarItemToEvent(item, folderID), nil
}

// UpdateEvent overwrites the event identified by ev.ID. Empty fields are
// treated as unchanged for Summary, Description, Location; zero time values
// leave Start/End untouched.
func (p *EWSProvider) UpdateEvent(ctx context.Context, calendarID string, ev providerdata.Event) (providerdata.Event, error) {
	itemID := strings.TrimSpace(ev.ID)
	if itemID == "" {
		return providerdata.Event{}, fmt.Errorf("event_id is required")
	}
	existing, err := p.client.GetCalendarItem(ctx, itemID)
	if err != nil {
		return providerdata.Event{}, fmt.Errorf("get event for update: %w", err)
	}
	folderID := calendarID
	if folderID == "" {
		folderID = p.calendarFolderID
	}
	if folderID == "" {
		folderID = string(ews.FolderKindCalendar)
	}
	var updates ews.CalendarItemUpdate
	if s := strings.TrimSpace(ev.Summary); s != "" {
		updates.Subject = &s
	}
	if ev.Description != "" {
		updates.Body = &ev.Description
	}
	if s := strings.TrimSpace(ev.Location); s != "" {
		updates.Location = &s
	}
	if !ev.Start.IsZero() {
		updates.Start = &ev.Start
	}
	if !ev.End.IsZero() {
		updates.End = &ev.End
	}
	if ev.AllDay {
		allDay := true
		updates.IsAllDay = &allDay
	}
	if len(ev.Attendees) > 0 {
		attendees := eventAttendeesToMailboxes(ev.Attendees)
		updates.RequiredAttendees = &attendees
	}
	if ev.ReminderMinutes != nil {
		updates.ReminderMinutes = ev.ReminderMinutes
	}
	_, err = p.client.UpdateCalendarItem(ctx, itemID, existing.ChangeKey, updates, ews.SendOnlyToChanged)
	if err != nil {
		return providerdata.Event{}, fmt.Errorf("update calendar event: %w", err)
	}
	updated, err := p.client.GetCalendarItem(ctx, itemID)
	if err != nil {
		return providerdata.Event{}, fmt.Errorf("fetch updated event: %w", err)
	}
	return calendarItemToEvent(updated, folderID), nil
}

// DeleteEvent removes the given event from the calendar.
func (p *EWSProvider) DeleteEvent(ctx context.Context, calendarID, eventID string) error {
	itemID := strings.TrimSpace(eventID)
	if itemID == "" {
		return fmt.Errorf("event_id is required")
	}
	// Fetch the item to determine if cancellations should be sent.
	item, err := p.client.GetCalendarItem(ctx, itemID)
	if err != nil {
		return fmt.Errorf("get event for delete: %w", err)
	}
	sendCancellations := ews.SendToNoneCancel
	if len(item.RequiredAttendees) > 0 {
		sendCancellations = ews.SendToAllAndSaveCopyCancel
	}
	return p.client.DeleteCalendarItem(ctx, itemID, sendCancellations)
}

// RespondToInvite sends an accept/decline/tentative response to a meeting
// invitation. It fetches the item first to get the change key, then sends the
// meeting response via CreateMeetingResponse.
func (p *EWSProvider) RespondToInvite(ctx context.Context, eventID string, resp providerdata.InviteResponse) error {
	itemID := strings.TrimSpace(eventID)
	if itemID == "" {
		return fmt.Errorf("event_id is required")
	}
	item, err := p.client.GetCalendarItem(ctx, itemID)
	if err != nil {
		return fmt.Errorf("get invite event: %w", err)
	}
	kind := ews.MeetingAccept
	switch strings.ToLower(strings.TrimSpace(resp.Status)) {
	case "accepted", "accept", "yes":
		kind = ews.MeetingAccept
	case "declined", "decline", "no":
		kind = ews.MeetingDecline
	case "tentative", "maybe":
		kind = ews.MeetingTentativelyAccept
	default:
		return fmt.Errorf("invite status must be one of accepted, declined, tentative")
	}
	return p.client.CreateMeetingResponse(ctx, item.ID, item.ChangeKey, kind, resp.Comment)
}

// ExpandOccurrences materialises individual occurrences of a recurring event
// for the given window. It uses FindItem with a CalendarView filtered to the
// master event's folder to get all occurrences.
func (p *EWSProvider) ExpandOccurrences(ctx context.Context, calendarID, eventID string, rng TimeRange) ([]providerdata.Event, error) {
	if rng.Start.IsZero() {
		rng.Start = time.Now()
	}
	if rng.End.IsZero() {
		rng.End = rng.Start.Add(30 * 24 * time.Hour)
	}
	// Use the event's parent folder (the calendar folder) with a CalendarView.
	folderID := calendarID
	if folderID == "" {
		folderID = p.calendarFolderID
	}
	if folderID == "" {
		folderID = string(ews.FolderKindCalendar)
	}
	items, err := p.client.ListCalendarItems(ctx, folderID, ews.TimeRange{Start: rng.Start, End: rng.End})
	if err != nil {
		return nil, fmt.Errorf("expand occurrences: %w", err)
	}
	// Filter to occurrences whose UID matches the master event's UID.
	masterUID := ""
	if master, err := p.client.GetCalendarItem(ctx, eventID); err == nil {
		masterUID = master.ICSUID
	}
	out := make([]providerdata.Event, 0, len(items))
	for _, item := range items {
		if masterUID != "" && item.ICSUID != masterUID {
			continue
		}
		out = append(out, calendarItemToEvent(item, folderID))
	}
	return out, nil
}
