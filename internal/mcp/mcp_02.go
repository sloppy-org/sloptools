package mcp

import (
	"context"
	"errors"
	"fmt"
	tabcalendar "github.com/sloppy-org/sloptools/internal/calendar"
	"github.com/sloppy-org/sloptools/internal/groupware"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/store"
	"strings"
	"time"
)

func (s *Server) calendarEventCreate(args map[string]interface{}) (map[string]interface{}, error) {
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
	allAccounts, err := tabcalendar.GoogleCalendarAccounts(st)
	if err != nil {
		return nil, err
	}
	account := accounts[0]
	provider, err := s.calendarProvider(ctx, account)
	if err != nil {
		return nil, err
	}
	mutator, ok := groupware.Supports[tabcalendar.EventMutator](provider)
	if !ok {
		return nil, fmt.Errorf("calendar account %q does not support event creation", account.Label)
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
	created, err := mutator.CreateEvent(ctx, target.ID, draft)
	if err != nil {
		return nil, err
	}
	sphere := tabcalendar.ResolveCalendarSphere(st, store.ExternalProviderGoogleCalendar, target.ID, target.Name, strArg(args, "sphere"), allAccounts)
	return map[string]interface{}{"provider": calendarProviderName(account, provider), "created": true, "event": eventPayload(created, target.Name, sphere, calendarProviderName(account, provider))}, nil
}

func (s *Server) resolveCalendarAccounts(st *store.Store, args map[ // resolveCalendarAccounts applies account_id / sphere routing. Absent
// account_id defaults to the existing GoogleCalendarAccounts() behaviour
// (Google accounts with the Gmail fallback), preserving the pre-refactor
// calendar_list response when no routing arguments are supplied.
string]interface{}) ([]store.ExternalAccount, error) {
	accountIDPtr, _, err := optionalInt64Arg(args, "account_id")
	if err != nil {
		return nil, err
	}
	if accountIDPtr != nil {
		account, err := st.GetExternalAccount(*accountIDPtr)
		if err != nil {
			return nil, err
		}
		if !account.Enabled {
			return nil, fmt.Errorf("account %d is disabled", *accountIDPtr)
		}
		return []store.ExternalAccount{account}, nil
	}
	accounts, err := tabcalendar.GoogleCalendarAccounts(st)
	if err != nil {
		return nil, err
	}
	sphere := strings.TrimSpace(strArg(args, "sphere"))
	if sphere == "" {
		return accounts, nil
	}
	filtered := make([]store.ExternalAccount, 0, len(accounts))
	for _, account := range accounts {
		if strings.EqualFold(account.Sphere, sphere) {
			filtered = append(filtered, account)
		}
	}
	return filtered, nil
}

func (s *Server) calendarProvider(ctx context.Context, account store.ExternalAccount) (tabcalendar.Provider, error) {
	if s.newCalendarProvider != nil {
		return s.newCalendarProvider(ctx, account)
	}
	if s.groupware == nil {
		return nil, fmt.Errorf("groupware registry is not configured")
	}
	return s.groupware.CalendarFor(ctx, account.ID)
}

func calendarProviderName(account store.ExternalAccount, provider tabcalendar.Provider) string {
	if provider != nil {
		if name := strings.TrimSpace(provider.ProviderName()); name != "" {
			return name
		}
	}
	if account.Provider != "" {
		return account.Provider
	}
	return store.ExternalProviderGoogleCalendar
}

func listCalendarEvents(ctx context.Context, provider tabcalendar.Provider, calendarID string, rng tabcalendar.TimeRange, query string, limit int64) ([]providerdata.Event, error) {
	if searcher, ok := groupware.Supports[tabcalendar.EventSearcher](provider); ok {
		return searcher.SearchEvents(ctx, calendarID, rng, query, limit)
	}
	items, err := provider.ListEvents(ctx, calendarID, rng)
	if err != nil {
		return nil, err
	}
	if query == "" {
		return items, nil
	}
	q := strings.ToLower(query)
	filtered := items[:0]
	for _, ev := range items {
		if strings.Contains(strings.ToLower(ev.Summary+" "+ev.Description+" "+ev.Location), q) {
			filtered = append(filtered, ev)
		}
	}
	return filtered, nil
}

func eventPayload(event providerdata.Event, calendarName, sphere, providerName string) map[string]interface{} {
	attendees := make([]map[string]interface{}, 0, len(event.Attendees))
	for _, att := range event.Attendees {
		attendees = append(attendees, map[string]interface{}{"email": att.Email, "name": att.Name, "response": att.Response})
	}
	if strings.TrimSpace(providerName) == "" {
		providerName = store.ExternalProviderGoogleCalendar
	}
	payload := map[string]interface{}{"id": event.ID, "calendar_id": event.CalendarID, "calendar_name": calendarName, "provider": providerName, "sphere": sphere, "summary": event.Summary, "description": event.Description, "location": event.Location, "start": event.Start.Format(time.RFC3339), "end": event.End.Format(time.RFC3339), "all_day": event.AllDay, "status": event.Status, "organizer": event.Organizer, "attendees": attendees, "recurring": event.Recurring, "ics_uid": event.ICSUID}
	if event.ReminderMinutes != nil {
		payload["reminder_minutes"] = *event.ReminderMinutes
	}
	return payload
}

func strFromAny(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func calendarEventFromArgs(args map[string]interface{}, calendarID string) (providerdata.Event, error) {
	summary := strings.TrimSpace(strArg(args, "summary"))
	if summary == "" {
		summary = strings.TrimSpace(strArg(args, "title"))
	}
	if summary == "" {
		return providerdata.Event{}, fmt.Errorf("summary is required")
	}
	allDay := boolArg(args, "all_day")
	start, startRaw, err := parseCalendarToolTimeArg(args, "start")
	if err != nil {
		return providerdata.Event{}, err
	}
	if startRaw != "" && calendarTimeLooksDateOnly(startRaw) {
		allDay = true
	}
	end, endRaw, err := parseCalendarToolOptionalTimeArg(args, "end")
	if err != nil {
		return providerdata.Event{}, err
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
		return providerdata.Event{}, fmt.Errorf("end must be after start")
	}
	attendees := make([]providerdata.Attendee, 0)
	for _, email := range stringListArg(args, "attendees") {
		attendees = append(attendees, providerdata.Attendee{Email: email, Response: "needsAction"})
	}
	return providerdata.Event{CalendarID: calendarID, Summary: summary, Description: strings.TrimSpace(strArg(args, "description")), Location: strings.TrimSpace(strArg(args, "location")), Start: start, End: end, AllDay: allDay, Attendees: attendees}, nil
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

const maxArtifactContentBytes = 64 * 1024

func (s *Server) requireStore() (*store.Store, error) {
	if s.store == nil {
		return nil, errors.New("domain store unavailable for this MCP server")
	}
	return s.store, nil
}

func int64Arg(args map[string]interface{}, key string) (int64, error) {
	v, ok := args[key]
	if !ok {
		return 0, fmt.Errorf("%s is required", key)
	}
	switch typed := v.(type) {
	case float64:
		return int64(typed), nil
	case int:
		return int64(typed), nil
	case int64:
		return typed, nil
	default:
		return 0, fmt.Errorf("%s must be an integer", key)
	}
}

func optionalInt64Arg(args map[string]interface{}, key string) (*int64, bool, error) {
	v, ok := args[key]
	if !ok {
		return nil, false, nil
	}
	switch typed := v.(type) {
	case float64:
		value := int64(typed)
		return &value, true, nil
	case int:
		value := int64(typed)
		return &value, true, nil
	case int64:
		value := typed
		return &value, true, nil
	default:
		return nil, false, fmt.Errorf("%s must be an integer", key)
	}
}

func optionalStringArg(args map[string]interface{}, key string) (*string, bool) {
	v, ok := args[key]
	if !ok {
		return nil, false
	}
	value, ok := v.(string)
	if !ok {
		return nil, false
	}
	clean := strings.TrimSpace(value)
	return &clean, true
}

func boolArg(args map[string]interface{}, key string) bool {
	v, _ := args[key].(bool)
	return v
}

func domainItemFilter(args map[string]interface{}) (store.ItemListFilter, error) {
	filter := store.ItemListFilter{Sphere: strings.TrimSpace(strArg(args, "sphere")), Source: strings.TrimSpace(strArg(args, "source"))}
	if workspaceID, ok, err := optionalInt64Arg(args, "workspace_id"); err != nil {
		return store.ItemListFilter{}, err
	} else if ok && workspaceID != nil {
		if *workspaceID <= 0 {
			filter.WorkspaceUnassigned = true
		} else {
			filter.WorkspaceID = workspaceID
		}
	}
	return filter, nil
}

func (s *Server) workspaceList(args map[string]interface{}) (map[string]interface{}, error) {
	st, err := s.requireStore()
	if err != nil {
		return nil, err
	}
	workspaces, err := st.ListWorkspacesForSphere(strings.TrimSpace(strArg(args, "sphere")))
	if err != nil {
		return nil, err
	}
	var activeID int64
	for _, workspace := range workspaces {
		if workspace.IsActive {
			activeID = workspace.ID
			break
		}
	}
	return map[string]interface{}{"workspaces": workspaces, "active_workspace_id": activeID}, nil
}

func (s *Server) workspaceActivate(args map[string]interface{}) (map[string]interface{}, error) {
	st, err := s.requireStore()
	if err != nil {
		return nil, err
	}
	workspaceID, err := int64Arg(args, "workspace_id")
	if err != nil {
		return nil, err
	}
	if workspaceID <= 0 {
		return nil, errors.New("workspace_id must be positive")
	}
	if err := st.SetActiveWorkspace(workspaceID); err != nil {
		return nil, err
	}
	workspace, err := st.GetWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"workspace": workspace}, nil
}

func (s *Server) workspaceGet(args map[string]interface{}) (map[string]interface{}, error) {
	st, err := s.requireStore()
	if err != nil {
		return nil, err
	}
	workspaceID, err := int64Arg(args, "workspace_id")
	if err != nil {
		return nil, err
	}
	if workspaceID <= 0 {
		return nil, errors.New("workspace_id must be positive")
	}
	workspace, err := st.GetWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	filter := store.ItemListFilter{WorkspaceID: &workspaceID}
	counts, err := st.CountItemsByStateFiltered(time.Now(), filter)
	if err != nil {
		return nil, err
	}
	openCount := counts[store.ItemStateInbox] + counts[store.ItemStateWaiting] + counts[store.ItemStateSomeday]
	return map[string]interface{}{"workspace": workspace, "item_counts": counts, "open_count": openCount}, nil
}

func (s *Server) workspaceWatchStart(args map[string]interface{}) (map[string]interface{}, error) {
	st, err := s.requireStore()
	if err != nil {
		return nil, err
	}
	workspaceID, err := int64Arg(args, "workspace_id")
	if err != nil {
		return nil, err
	}
	configJSON := strings.TrimSpace(strArg(args, "config_json"))
	pollInterval := intArg(args, "poll_interval_seconds", 0)
	watch, err := st.UpsertWorkspaceWatch(workspaceID, configJSON, pollInterval, true, nil)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"watch": watch}, nil
}

func (s *Server) workspaceWatchStop(args map[string]interface{}) (map[string]interface{}, error) {
	st, err := s.requireStore()
	if err != nil {
		return nil, err
	}
	workspaceID, err := int64Arg(args, "workspace_id")
	if err != nil {
		return nil, err
	}
	watch, err := st.SetWorkspaceWatchEnabled(workspaceID, false)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"watch": watch}, nil
}

func (s *Server) workspaceWatchStatus(args map[string]interface{}) (map[string]interface{}, error) {
	st, err := s.requireStore()
	if err != nil {
		return nil, err
	}
	workspaceID, err := int64Arg(args, "workspace_id")
	if err != nil {
		return nil, err
	}
	watch, err := st.GetWorkspaceWatch(workspaceID)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"watch": watch}, nil
}
