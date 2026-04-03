package mcp

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tabcalendar "github.com/sloppy-org/sloptools/internal/calendar"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/store"
)

func (s *Server) calendarList(args map[string]interface{}) (map[string]interface{}, error) {
	st, err := s.requireStore()
	if err != nil {
		return nil, err
	}
	accounts, err := tabcalendar.GoogleCalendarAccounts(st)
	if err != nil {
		return nil, err
	}
	if len(accounts) == 0 {
		return map[string]interface{}{
			"provider":  store.ExternalProviderGoogleCalendar,
			"calendars": []map[string]interface{}{},
			"count":     0,
		}, nil
	}
	if s.newGoogleCalendarReader == nil {
		return nil, fmt.Errorf("google calendar reader is unavailable")
	}
	reader, err := s.newGoogleCalendarReader(context.Background())
	if err != nil {
		return nil, err
	}
	calendars, err := reader.ListCalendars(context.Background())
	if err != nil {
		return nil, err
	}
	items := make([]map[string]interface{}, 0, len(calendars))
	for _, cal := range calendars {
		items = append(items, map[string]interface{}{
			"id":          cal.ID,
			"name":        cal.Name,
			"description": cal.Description,
			"time_zone":   cal.TimeZone,
			"primary":     cal.Primary,
			"provider":    store.ExternalProviderGoogleCalendar,
			"sphere":      tabcalendar.ResolveCalendarSphere(st, store.ExternalProviderGoogleCalendar, cal.ID, cal.Name, "", accounts),
		})
	}
	sort.Slice(items, func(i, j int) bool {
		return strings.ToLower(strFromAny(items[i]["name"])) < strings.ToLower(strFromAny(items[j]["name"]))
	})
	return map[string]interface{}{
		"provider":  store.ExternalProviderGoogleCalendar,
		"calendars": items,
		"count":     len(items),
	}, nil
}

func (s *Server) calendarEvents(args map[string]interface{}) (map[string]interface{}, error) {
	st, err := s.requireStore()
	if err != nil {
		return nil, err
	}
	accounts, err := tabcalendar.GoogleCalendarAccounts(st)
	if err != nil {
		return nil, err
	}
	if len(accounts) == 0 {
		return map[string]interface{}{
			"provider": store.ExternalProviderGoogleCalendar,
			"events":   []map[string]interface{}{},
			"count":    0,
		}, nil
	}
	if s.newGoogleCalendarReader == nil {
		return nil, fmt.Errorf("google calendar reader is unavailable")
	}
	reader, err := s.newGoogleCalendarReader(context.Background())
	if err != nil {
		return nil, err
	}
	calendars, err := reader.ListCalendars(context.Background())
	if err != nil {
		return nil, err
	}
	calendarID := strings.TrimSpace(strArg(args, "calendar_id"))
	query := strings.TrimSpace(strArg(args, "query"))
	days := intArg(args, "days", 30)
	if days <= 0 {
		days = 30
	}
	limit := intArg(args, "limit", 100)
	if limit <= 0 {
		limit = 100
	}
	now := time.Now()
	timeMin := now
	timeMax := now.Add(time.Duration(days) * 24 * time.Hour)
	calendarNames := make(map[string]string, len(calendars))
	events := make([]map[string]interface{}, 0, limit)
	for _, cal := range calendars {
		if calendarID != "" && !strings.EqualFold(strings.TrimSpace(cal.ID), calendarID) {
			continue
		}
		calendarNames[cal.ID] = cal.Name
		providerSphere := tabcalendar.ResolveCalendarSphere(st, store.ExternalProviderGoogleCalendar, cal.ID, cal.Name, "", accounts)
		items, err := reader.GetEvents(context.Background(), tabcalendar.GetEventsOptions{
			CalendarID: cal.ID,
			TimeMin:    timeMin,
			TimeMax:    timeMax,
			MaxResults: int64(limit),
			Query:      query,
		})
		if err != nil {
			return nil, fmt.Errorf("list events for %q: %w", cal.Name, err)
		}
		for _, event := range items {
			events = append(events, eventPayload(event, cal.Name, providerSphere))
		}
	}
	sort.Slice(events, func(i, j int) bool {
		iStart, _ := time.Parse(time.RFC3339, strFromAny(events[i]["start"]))
		jStart, _ := time.Parse(time.RFC3339, strFromAny(events[j]["start"]))
		if iStart.Equal(jStart) {
			return strings.ToLower(strFromAny(events[i]["summary"])) < strings.ToLower(strFromAny(events[j]["summary"]))
		}
		return iStart.Before(jStart)
	})
	if len(events) > limit {
		events = events[:limit]
	}
	return map[string]interface{}{
		"provider":      store.ExternalProviderGoogleCalendar,
		"calendar_id":   calendarID,
		"calendar_name": strings.TrimSpace(calendarNames[calendarID]),
		"days":          days,
		"query":         query,
		"events":        events,
		"count":         len(events),
	}, nil
}

func (s *Server) calendarEventCreate(args map[string]interface{}) (map[string]interface{}, error) {
	st, err := s.requireStore()
	if err != nil {
		return nil, err
	}
	accounts, err := tabcalendar.GoogleCalendarAccounts(st)
	if err != nil {
		return nil, err
	}
	if len(accounts) == 0 {
		return nil, fmt.Errorf("no Google Calendar accounts are configured")
	}
	if s.newGoogleCalendarReader == nil {
		return nil, fmt.Errorf("google calendar writer is unavailable")
	}
	reader, err := s.newGoogleCalendarReader(context.Background())
	if err != nil {
		return nil, err
	}
	calendars, err := reader.ListCalendars(context.Background())
	if err != nil {
		return nil, err
	}
	target, err := tabcalendar.SelectCalendar(
		calendars,
		st,
		accounts,
		strArg(args, "calendar_id"),
		strArg(args, "sphere"),
	)
	if err != nil {
		return nil, err
	}
	createOpts, err := calendarCreateOptionsFromArgs(args, target.ID)
	if err != nil {
		return nil, err
	}
	event, err := reader.CreateEvent(context.Background(), createOpts)
	if err != nil {
		return nil, err
	}
	sphere := tabcalendar.ResolveCalendarSphere(st, store.ExternalProviderGoogleCalendar, target.ID, target.Name, strArg(args, "sphere"), accounts)
	return map[string]interface{}{
		"provider": store.ExternalProviderGoogleCalendar,
		"created":  true,
		"event":    eventPayload(event, target.Name, sphere),
	}, nil
}

func eventPayload(event providerdata.Event, calendarName, sphere string) map[string]interface{} {
	return map[string]interface{}{
		"id":            event.ID,
		"calendar_id":   event.CalendarID,
		"calendar_name": calendarName,
		"provider":      store.ExternalProviderGoogleCalendar,
		"sphere":        sphere,
		"summary":       event.Summary,
		"description":   event.Description,
		"location":      event.Location,
		"start":         event.Start.Format(time.RFC3339),
		"end":           event.End.Format(time.RFC3339),
		"all_day":       event.AllDay,
		"status":        event.Status,
		"organizer":     event.Organizer,
		"attendees":     append([]string(nil), event.Attendees...),
		"recurring":     event.Recurring,
	}
}

func strFromAny(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func calendarCreateOptionsFromArgs(args map[string]interface{}, calendarID string) (tabcalendar.CreateEventOptions, error) {
	summary := strings.TrimSpace(strArg(args, "summary"))
	if summary == "" {
		summary = strings.TrimSpace(strArg(args, "title"))
	}
	if summary == "" {
		return tabcalendar.CreateEventOptions{}, fmt.Errorf("summary is required")
	}
	allDay := boolArg(args, "all_day")
	start, startRaw, err := parseCalendarToolTimeArg(args, "start")
	if err != nil {
		return tabcalendar.CreateEventOptions{}, err
	}
	if startRaw != "" && calendarTimeLooksDateOnly(startRaw) {
		allDay = true
	}
	end, endRaw, err := parseCalendarToolOptionalTimeArg(args, "end")
	if err != nil {
		return tabcalendar.CreateEventOptions{}, err
	}
	durationMinutes := intArg(args, "duration_minutes", 0)
	if end.IsZero() {
		if durationMinutes <= 0 {
			if allDay {
				durationMinutes = 24 * 60
			} else {
				durationMinutes = 60
			}
		}
		end = start.Add(time.Duration(durationMinutes) * time.Minute)
	} else if endRaw != "" && calendarTimeLooksDateOnly(endRaw) {
		allDay = true
	}
	if !end.After(start) {
		return tabcalendar.CreateEventOptions{}, fmt.Errorf("end must be after start")
	}
	return tabcalendar.CreateEventOptions{
		CalendarID:  calendarID,
		Summary:     summary,
		Description: strings.TrimSpace(strArg(args, "description")),
		Location:    strings.TrimSpace(strArg(args, "location")),
		Start:       start,
		End:         end,
		AllDay:      allDay,
		Attendees:   stringListArg(args, "attendees"),
	}, nil
}

func parseCalendarToolTimeArg(args map[string]interface{}, key string) (time.Time, string, error) {
	raw := strings.TrimSpace(strArg(args, key))
	if raw == "" {
		return time.Time{}, "", fmt.Errorf("%s is required", key)
	}
	parsed, err := parseCalendarToolTime(raw)
	if err != nil {
		return time.Time{}, raw, fmt.Errorf("%s must be RFC3339, YYYY-MM-DDTHH:MM, YYYY-MM-DD HH:MM, or YYYY-MM-DD", key)
	}
	return parsed, raw, nil
}

func parseCalendarToolOptionalTimeArg(args map[string]interface{}, key string) (time.Time, string, error) {
	raw := strings.TrimSpace(strArg(args, key))
	if raw == "" {
		return time.Time{}, "", nil
	}
	parsed, err := parseCalendarToolTime(raw)
	if err != nil {
		return time.Time{}, raw, fmt.Errorf("%s must be RFC3339, YYYY-MM-DDTHH:MM, YYYY-MM-DD HH:MM, or YYYY-MM-DD", key)
	}
	return parsed, raw, nil
}

func parseCalendarToolTime(raw string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04", "2006-01-02 15:04", "2006-01-02"} {
		if layout == "2006-01-02" || layout == "2006-01-02T15:04" || layout == "2006-01-02 15:04" {
			if parsed, err := time.ParseInLocation(layout, raw, time.Local); err == nil {
				return parsed, nil
			}
			continue
		}
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid time %q", raw)
}

func calendarTimeLooksDateOnly(raw string) bool {
	return len(strings.TrimSpace(raw)) == len("2006-01-02") && strings.Count(raw, "-") == 2
}

func stringListArg(args map[string]interface{}, key string) []string {
	value, ok := args[key]
	if !ok {
		return nil
	}
	switch typed := value.(type) {
	case []string:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if clean := strings.TrimSpace(item); clean != "" {
				out = append(out, clean)
			}
		}
		return out
	case []interface{}:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if clean := strings.TrimSpace(fmt.Sprint(item)); clean != "" && clean != "<nil>" {
				out = append(out, clean)
			}
		}
		return out
	case string:
		parts := strings.Split(typed, ",")
		out := make([]string, 0, len(parts))
		for _, part := range parts {
			if clean := strings.TrimSpace(part); clean != "" {
				out = append(out, clean)
			}
		}
		return out
	default:
		return nil
	}
}
