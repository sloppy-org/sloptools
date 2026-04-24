package calendar

import (
	"strings"

	"github.com/sloppy-org/sloptools/internal/ews"
	"github.com/sloppy-org/sloptools/internal/providerdata"
)

// calendarItemToEvent converts an EWS-side CalendarItem into the canonical
// providerdata.Event used by the MCP surface and higher tiers.
func calendarItemToEvent(item ews.CalendarItem, calendarID string) providerdata.Event {
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
		ICSUID:      item.ICSUID,
	}
	if ev.Summary == "" {
		ev.Summary = "(No title)"
	}
	if item.Organizer.Email != "" {
		ev.Organizer = item.Organizer.Email
	}
	ev.Attendees = calendarItemAttendeesToProviderdata(item.RequiredAttendees, item.OptionalAttendees)
	ev.Recurring = item.Recurrence != ""
	return ev
}

// eventToCalendarItemInput converts a canonical providerdata.Event into an
// EWS-side CalendarItemInput suitable for CreateCalendarItem.
func eventToCalendarItemInput(ev providerdata.Event, folderID string) ews.CalendarItemInput {
	input := ews.CalendarItemInput{
		Subject:  ev.Summary,
		Body:     ev.Description,
		Location: ev.Location,
		Start:    ev.Start,
		End:      ev.End,
		IsAllDay: ev.AllDay,
		ICSUID:   ev.ICSUID,
		ReminderMinutes: func() int {
			if ev.ReminderMinutes != nil {
				return *ev.ReminderMinutes
			}
			return 0
		}(),
	}
	if ev.Organizer != "" {
		input.Organizer = ews.Mailbox{Email: ev.Organizer}
	}
	input.RequiredAttendees = eventAttendeesToMailboxes(ev.Attendees)
	return input
}

// eventAttendeesToMailboxes converts providerdata.Attendee slices into EWS
// mailbox entries. Only attendees with non-empty email are included.
func eventAttendeesToMailboxes(att []providerdata.Attendee) []ews.Mailbox {
	out := make([]ews.Mailbox, 0, len(att))
	for _, a := range att {
		email := strings.TrimSpace(a.Email)
		if email == "" {
			continue
		}
		out = append(out, ews.Mailbox{
			Email: email,
			Name:  strings.TrimSpace(a.Name),
		})
	}
	return out
}

// calendarItemAttendeesToProviderdata merges required and optional EWS
// attendees into the canonical Attendee slice.
func calendarItemAttendeesToProviderdata(required, optional []ews.Mailbox) []providerdata.Attendee {
	out := make([]providerdata.Attendee, 0, len(required)+len(optional))
	for _, m := range required {
		out = append(out, providerdata.Attendee{
			Email:    m.Email,
			Name:     m.Name,
			Response: "needsAction",
		})
	}
	for _, m := range optional {
		out = append(out, providerdata.Attendee{
			Email:    m.Email,
			Name:     m.Name,
			Response: "needsAction",
		})
	}
	return out
}
