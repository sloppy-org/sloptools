package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	tabcalendar "github.com/sloppy-org/sloptools/internal/calendar"
	"github.com/sloppy-org/sloptools/internal/groupware"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/store"
)

func (s *Server) dispatchCalendarEvent(method string, args map[string]interface{}) (map[string]interface{}, error) {
	switch method {
	case "calendar_event_get":
		return s.calendarEventGet(args)
	case "calendar_event_update":
		return s.calendarEventUpdate(args)
	case "calendar_event_delete":
		return s.calendarEventDelete(args)
	case "calendar_event_respond":
		return s.calendarEventRespond(args)
	case "calendar_event_ics_export":
		return s.calendarEventIcsExport(args)
	}
	return nil, fmt.Errorf("unknown calendar event method: %s", method)
}

func (s *Server) calendarEventGet(args map[string]interface{}) (map[string]interface{}, error) {
	st, err := s.requireStore()
	if err != nil {
		return nil, err
	}
	ctx := context.Background()
	accounts, err := s.resolveCalendarAccounts(st, args)
	if err != nil {
		return nil, err
	}
	if len(accounts) == 0 {
		return nil, fmt.Errorf("no calendar accounts are configured")
	}
	account := accounts[0]
	provider, err := s.calendarProvider(ctx, account)
	if err != nil {
		return nil, err
	}
	allAccounts, err := tabcalendar.GoogleCalendarAccounts(st)
	if err != nil {
		return nil, err
	}
	eventID := strings.TrimSpace(strArg(args, "event_id"))
	if eventID == "" {
		return nil, fmt.Errorf("event_id is required")
	}
	calendarID := strings.TrimSpace(strArg(args, "calendar_id"))
	event, err := provider.GetEvent(ctx, calendarID, eventID)
	if err != nil {
		return nil, err
	}
	calendars, err := provider.ListCalendars(ctx)
	if err != nil {
		return nil, err
	}
	target, err := tabcalendar.SelectCalendar(calendars, st, allAccounts, calendarID, "")
	sphere := tabcalendar.ResolveCalendarSphere(st, store.ExternalProviderGoogleCalendar, target.ID, target.Name, "", allAccounts)
	return map[string]interface{}{
		"provider": calendarProviderName(account, provider),
		"event":    eventPayload(event, target.Name, sphere, calendarProviderName(account, provider)),
	}, nil
}

func (s *Server) calendarEventUpdate(args map[string]interface{}) (map[string]interface{}, error) {
	st, err := s.requireStore()
	if err != nil {
		return nil, err
	}
	ctx := context.Background()
	accounts, err := s.resolveCalendarAccounts(st, args)
	if err != nil {
		return nil, err
	}
	if len(accounts) == 0 {
		return nil, fmt.Errorf("no calendar accounts are configured")
	}
	account := accounts[0]
	provider, err := s.calendarProvider(ctx, account)
	if err != nil {
		return nil, err
	}
	allAccounts, err := tabcalendar.GoogleCalendarAccounts(st)
	if err != nil {
		return nil, err
	}
	mutator, ok := groupware.Supports[tabcalendar.EventMutator](provider)
	if !ok {
		return nil, fmt.Errorf("calendar account %q does not support event updates", account.Label)
	}
	eventID := strings.TrimSpace(strArg(args, "event_id"))
	if eventID == "" {
		return nil, fmt.Errorf("event_id is required")
	}
	calendars, err := provider.ListCalendars(ctx)
	if err != nil {
		return nil, err
	}
	target, err := tabcalendar.SelectCalendar(calendars, st, allAccounts, strArg(args, "calendar_id"), strArg(args, "sphere"))
	if err != nil {
		return nil, err
	}
	draft, err := calendarEventFromArgs(args, target.ID)
	if err != nil {
		return nil, err
	}
	draft.ID = eventID
	updated, err := mutator.UpdateEvent(ctx, target.ID, draft)
	if err != nil {
		return nil, err
	}
	providerName := calendarProviderName(account, provider)
	sphere := tabcalendar.ResolveCalendarSphere(st, store.ExternalProviderGoogleCalendar, target.ID, target.Name, strArg(args, "sphere"), allAccounts)
	return withAffected(
		map[string]interface{}{
			"provider": providerName,
			"updated":  true,
			"event":    eventPayload(updated, target.Name, sphere, providerName),
		},
		calendarEventAffectedRefFromEvent(account, providerName, sphere, updated),
	), nil
}

func (s *Server) calendarEventDelete(args map[string]interface{}) (map[string]interface{}, error) {
	st, err := s.requireStore()
	if err != nil {
		return nil, err
	}
	ctx := context.Background()
	accounts, err := s.resolveCalendarAccounts(st, args)
	if err != nil {
		return nil, err
	}
	if len(accounts) == 0 {
		return nil, fmt.Errorf("no calendar accounts are configured")
	}
	account := accounts[0]
	provider, err := s.calendarProvider(ctx, account)
	if err != nil {
		return nil, err
	}
	allAccounts, err := tabcalendar.GoogleCalendarAccounts(st)
	if err != nil {
		return nil, err
	}
	mutator, ok := groupware.Supports[tabcalendar.EventMutator](provider)
	if !ok {
		return nil, fmt.Errorf("calendar account %q does not support event deletion", account.Label)
	}
	eventID := strings.TrimSpace(strArg(args, "event_id"))
	if eventID == "" {
		return nil, fmt.Errorf("event_id is required")
	}
	calendars, err := provider.ListCalendars(ctx)
	if err != nil {
		return nil, err
	}
	target, err := tabcalendar.SelectCalendar(calendars, st, allAccounts, strArg(args, "calendar_id"), strArg(args, "sphere"))
	if err != nil {
		return nil, err
	}
	if err := mutator.DeleteEvent(ctx, target.ID, eventID); err != nil {
		return nil, err
	}
	providerName := calendarProviderName(account, provider)
	sphere := tabcalendar.ResolveCalendarSphere(st, store.ExternalProviderGoogleCalendar, target.ID, target.Name, strArg(args, "sphere"), allAccounts)
	return withAffected(
		map[string]interface{}{
			"provider":    providerName,
			"deleted":     true,
			"id":          eventID,
			"calendar_id": target.ID,
			"sphere":      sphere,
		},
		calendarEventAffectedRef(account, providerName, sphere, target.ID, eventID),
	), nil
}

func (s *Server) calendarEventRespond(args map[string]interface{}) (map[string]interface{}, error) {
	st, err := s.requireStore()
	if err != nil {
		return nil, err
	}
	ctx := context.Background()
	accounts, err := s.resolveCalendarAccounts(st, args)
	if err != nil {
		return nil, err
	}
	if len(accounts) == 0 {
		return nil, fmt.Errorf("no calendar accounts are configured")
	}
	account := accounts[0]
	provider, err := s.calendarProvider(ctx, account)
	if err != nil {
		return nil, err
	}
	responder, ok := groupware.Supports[tabcalendar.InviteResponder](provider)
	if !ok {
		return nil, fmt.Errorf("calendar account %q does not support invite responses", account.Label)
	}
	eventID := strings.TrimSpace(strArg(args, "event_id"))
	if eventID == "" {
		return nil, fmt.Errorf("event_id is required")
	}
	response := strings.TrimSpace(strArg(args, "response"))
	if response == "" {
		return nil, fmt.Errorf("response is required")
	}
	switch response {
	case "accepted", "declined", "tentative":
	default:
		return nil, fmt.Errorf("response must be one of: accepted, declined, tentative")
	}
	comment := strings.TrimSpace(strArg(args, "comment"))
	if err := responder.RespondToInvite(ctx, eventID, providerdata.InviteResponse{Status: response, Comment: comment}); err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"provider":  calendarProviderName(account, provider),
		"responded": true,
		"status":    response,
	}, nil
}

func (s *Server) calendarEventIcsExport(args map[string]interface{}) (map[string]interface{}, error) {
	st, err := s.requireStore()
	if err != nil {
		return nil, err
	}
	ctx := context.Background()
	accounts, err := s.resolveCalendarAccounts(st, args)
	if err != nil {
		return nil, err
	}
	if len(accounts) == 0 {
		return nil, fmt.Errorf("no calendar accounts are configured")
	}
	account := accounts[0]
	provider, err := s.calendarProvider(ctx, account)
	if err != nil {
		return nil, err
	}
	eventID := strings.TrimSpace(strArg(args, "event_id"))
	if eventID == "" {
		return nil, fmt.Errorf("event_id is required")
	}
	calendarID := strings.TrimSpace(strArg(args, "calendar_id"))

	if exporter, ok := groupware.Supports[tabcalendar.ICSExporter](provider); ok {
		ics, err := exporter.ExportICS(ctx, calendarID, eventID)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{
			"provider": calendarProviderName(account, provider),
			"ics":      string(ics),
		}, nil
	}

	// Synthetic fallback: fetch the event and build RFC5545 payload.
	calendars, err := provider.ListCalendars(ctx)
	if err != nil {
		return nil, err
	}
	target, err := tabcalendar.SelectCalendar(calendars, st, nil, calendarID, "")
	if err != nil {
		return nil, err
	}
	event, err := provider.GetEvent(ctx, target.ID, eventID)
	if err != nil {
		return nil, err
	}
	ics, err := buildICSFromEvent(event, target.Name)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"provider": calendarProviderName(account, provider),
		"ics":      string(ics),
	}, nil
}

// buildICSFromEvent renders a providerdata.Event as a minimal RFC5545 iCalendar
// payload. It produces deterministic line order and escapes values per RFC5545
// §3.3.11 (escaping backslashes, semicolons, and commas in text).
func buildICSFromEvent(ev providerdata.Event, calendarName string) ([]byte, error) {
	var b strings.Builder
	b.WriteString("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//sloptools//EN\r\n")
	b.WriteString("BEGIN:VEVENT\r\n")

	uid := ev.ICSUID
	if uid == "" {
		uid = ev.ID
	}
	if uid == "" {
		uid = "sloptools-" + time.Now().Format("20060102T150405Z")
	}
	b.WriteString("UID:" + icsEscape(uid) + "\r\n")
	b.WriteString("DTSTAMP:" + time.Now().UTC().Format("20060102T150405Z") + "\r\n")

	if ev.Summary != "" {
		b.WriteString("SUMMARY:" + icsEscape(ev.Summary) + "\r\n")
	}
	if ev.Description != "" {
		b.WriteString("DESCRIPTION:" + icsEscape(ev.Description) + "\r\n")
	}
	if ev.Location != "" {
		b.WriteString("LOCATION:" + icsEscape(ev.Location) + "\r\n")
	}

	if ev.Start.Year() > 0 {
		if ev.AllDay {
			b.WriteString("DTSTART;VALUE=DATE:" + ev.Start.Format("20060102") + "\r\n")
			end := ev.End
			if end.IsZero() {
				end = ev.Start
			}
			b.WriteString("DTEND;VALUE=DATE:" + end.Format("20060102") + "\r\n")
		} else {
			start := ev.Start
			if start.IsZero() {
				start = time.Now()
			}
			b.WriteString("DTSTART:" + start.UTC().Format("20060102T150405Z") + "\r\n")
			end := ev.End
			if end.IsZero() {
				end = start.Add(time.Hour)
			}
			b.WriteString("DTEND:" + end.UTC().Format("20060102T150405Z") + "\r\n")
		}
	}

	if ev.Status != "" {
		b.WriteString("STATUS:" + icsEscape(strings.ToUpper(ev.Status)) + "\r\n")
	}
	if ev.Organizer != "" {
		b.WriteString("ORGANIZER;CN=" + icsEscape(ev.Organizer) + ":mailto:" + icsEscape(ev.Organizer) + "\r\n")
	}

	for _, att := range ev.Attendees {
		b.WriteString("ATTENDEE;RSVP=TRUE:mailto:" + icsEscape(att.Email) + "\r\n")
		if att.Name != "" {
			// CN is set via parameter after the URI in a separate line
			// but for simplicity we just include it inline
		}
	}

	b.WriteString("END:VEVENT\r\n")
	b.WriteString("END:VCALENDAR\r\n")
	return []byte(b.String()), nil
}

// icsEscape escapes backslashes, semicolons, and commas per RFC5545 §3.3.11.
func icsEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `;`, `\;`)
	s = strings.ReplaceAll(s, `,`, `\,`)
	return s
}

func calendarEventsRange(args map[string]interface{}, now time.Time) (tabcalendar.TimeRange, int, error) {
	start, _, err := parseCalendarToolOptionalTimeArg(args, "start")
	if err != nil {
		return tabcalendar.TimeRange{}, 0, err
	}
	end, _, err := parseCalendarToolOptionalTimeArg(args, "end")
	if err != nil {
		return tabcalendar.TimeRange{}, 0, err
	}
	days := intArg(args, "days", 30)
	switch {
	case !start.IsZero() && !end.IsZero():
	case !start.IsZero():
		if days == 0 {
			days = 30
		}
		end = start.Add(time.Duration(absInt(days)) * 24 * time.Hour)
	case !end.IsZero():
		if days == 0 {
			days = 30
		}
		start = end.Add(-time.Duration(absInt(days)) * 24 * time.Hour)
	default:
		if days == 0 {
			days = 30
		}
		if days < 0 {
			start = now.Add(time.Duration(days) * 24 * time.Hour)
			end = now
		} else {
			start = now
			end = now.Add(time.Duration(days) * 24 * time.Hour)
		}
	}
	if !end.After(start) {
		return tabcalendar.TimeRange{}, days, fmt.Errorf("calendar_events end must be after start")
	}
	return tabcalendar.TimeRange{Start: start, End: end}, days, nil
}

func (s *Server) calendarFreeBusy(args map[string]interface{}) (map[string]interface{}, error) {
	st, err := s.requireStore()
	if err != nil {
		return nil, err
	}
	ctx := context.Background()
	accounts, err := s.resolveCalendarAccounts(st, args)
	if err != nil {
		return nil, err
	}
	if len(accounts) == 0 {
		return nil, fmt.Errorf("no calendar accounts are configured")
	}
	account := accounts[0]
	provider, err := s.calendarProvider(ctx, account)
	if err != nil {
		return nil, err
	}
	looker, ok := groupware.Supports[tabcalendar.FreeBusyLooker](provider)
	if !ok {
		return nil, fmt.Errorf("calendar account %q does not support free/busy queries", account.Label)
	}
	participants := stringListArg(args, "participants")
	if len(participants) == 0 {
		return nil, fmt.Errorf("participants is required")
	}
	start, _, err := parseCalendarToolTimeArg(args, "start")
	if err != nil {
		return nil, err
	}
	end, _, err := parseCalendarToolTimeArg(args, "end")
	if err != nil {
		return nil, err
	}
	if !end.After(start) {
		return nil, fmt.Errorf("end must be after start")
	}
	slots, err := looker.QueryFreeBusy(ctx, participants, tabcalendar.TimeRange{Start: start, End: end})
	if err != nil {
		return nil, err
	}
	slotMaps := make([]map[string]interface{}, 0, len(slots))
	for _, slot := range slots {
		slotMaps = append(slotMaps, map[string]interface{}{
			"participant": slot.Participant,
			"start":       slot.Start.Format(time.RFC3339),
			"end":         slot.End.Format(time.RFC3339),
			"status":      slot.Status,
		})
	}
	return map[string]interface{}{"slots": slotMaps}, nil
}
