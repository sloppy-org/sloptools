package calendar

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/googleauth"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	gcal "google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
)

// ProviderNameGoogle is the stable string reported by GoogleProvider so MCP
// payloads and logs can identify the backend without reflection.
const ProviderNameGoogle = "google_calendar"

// GoogleProvider adapts the Google Calendar v3 API to the Provider plus
// EventMutator plus InviteResponder contract. It reuses a shared
// googleauth.Session so mail, calendar, contacts, and tasks can all sit on a
// single OAuth pipeline per account.
type GoogleProvider struct {
	session *googleauth.Session
	service *gcal.Service
}

var (
	_ Provider        = (*GoogleProvider)(nil)
	_ EventMutator    = (*GoogleProvider)(nil)
	_ InviteResponder = (*GoogleProvider)(nil)
)

// NewGoogleProvider wraps an existing OAuth session. The Google Calendar
// service is built lazily on the first call that needs it so constructing a
// provider never touches the network.
func NewGoogleProvider(session *googleauth.Session) *GoogleProvider {
	return &GoogleProvider{session: session}
}

// NewGoogleProviderWithFiles builds an isolated session from credential and
// token files on disk. Intended for CLI paths and tests that do not route
// through the groupware registry.
func NewGoogleProviderWithFiles(credentialsPath, tokenPath string) (*GoogleProvider, error) {
	session, err := googleauth.New(credentialsPath, tokenPath, googleauth.DefaultScopes)
	if err != nil {
		return nil, err
	}
	return NewGoogleProvider(session), nil
}

// Session exposes the underlying OAuth session so tests and higher tiers can
// verify session sharing.
func (g *GoogleProvider) Session() *googleauth.Session {
	if g == nil {
		return nil
	}
	return g.session
}

func (g *GoogleProvider) getService(ctx context.Context) (*gcal.Service, error) {
	if g == nil || g.session == nil {
		return nil, fmt.Errorf("google calendar provider is not configured")
	}
	if g.service != nil {
		return g.service, nil
	}
	tokenSource, err := g.session.TokenSource(ctx)
	if err != nil {
		return nil, err
	}
	service, err := gcal.NewService(ctx, option.WithTokenSource(tokenSource))
	if err != nil {
		return nil, fmt.Errorf("create Google Calendar service: %w", err)
	}
	g.service = service
	return service, nil
}

// ProviderName returns the stable backend identifier.
func (g *GoogleProvider) ProviderName() string { return ProviderNameGoogle }

// Close is a no-op today; the session is owned by the groupware registry.
func (g *GoogleProvider) Close() error { return nil }

// GetAuthURL forwards to the underlying session for the CLI auth bootstrap.
func (g *GoogleProvider) GetAuthURL() string {
	if g == nil || g.session == nil {
		return ""
	}
	return g.session.GetAuthURL()
}

// ExchangeCode forwards the CLI auth exchange to the underlying session.
func (g *GoogleProvider) ExchangeCode(ctx context.Context, code string) error {
	if g == nil || g.session == nil {
		return fmt.Errorf("google calendar auth is not configured")
	}
	return g.session.ExchangeCode(ctx, code)
}

// ListCalendars returns every calendar visible to the authenticated account.
func (g *GoogleProvider) ListCalendars(ctx context.Context) ([]providerdata.Calendar, error) {
	service, err := g.getService(ctx)
	if err != nil {
		return nil, err
	}
	result, err := service.CalendarList.List().Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("list calendars: %w", err)
	}
	calendars := make([]providerdata.Calendar, 0, len(result.Items))
	for _, cal := range result.Items {
		calendars = append(calendars, providerdata.Calendar{
			ID:          cal.Id,
			Name:        cal.Summary,
			Description: cal.Description,
			TimeZone:    cal.TimeZone,
			Primary:     cal.Primary,
		})
	}
	return calendars, nil
}

// ListEvents returns events in rng sorted by start time.
func (g *GoogleProvider) ListEvents(ctx context.Context, calendarID string, rng TimeRange) ([]providerdata.Event, error) {
	return g.searchEvents(ctx, calendarID, rng, "", 0)
}

// SearchEvents extends ListEvents with Google's server-side free-text search
// and an explicit page-size cap. Used by the MCP handler so query and limit
// stay in the Google API path and the calendar_events output matches the
// pre-refactor baseline byte-for-byte.
func (g *GoogleProvider) SearchEvents(ctx context.Context, calendarID string, rng TimeRange, query string, maxResults int64) ([]providerdata.Event, error) {
	return g.searchEvents(ctx, calendarID, rng, query, maxResults)
}

func (g *GoogleProvider) searchEvents(ctx context.Context, calendarID string, rng TimeRange, query string, maxResults int64) ([]providerdata.Event, error) {
	service, err := g.getService(ctx)
	if err != nil {
		return nil, err
	}
	calID := strings.TrimSpace(calendarID)
	if calID == "" {
		calID = "primary"
	}
	if rng.Start.IsZero() {
		rng.Start = time.Now()
	}
	if rng.End.IsZero() {
		rng.End = rng.Start.Add(30 * 24 * time.Hour)
	}
	if maxResults <= 0 {
		maxResults = 100
	}
	call := service.Events.List(calID).
		Context(ctx).
		TimeMin(rng.Start.Format(time.RFC3339)).
		TimeMax(rng.End.Format(time.RFC3339)).
		MaxResults(maxResults).
		SingleEvents(true).
		OrderBy("startTime")
	if q := strings.TrimSpace(query); q != "" {
		call = call.Q(q)
	}
	result, err := call.Do()
	if err != nil {
		return nil, fmt.Errorf("get calendar events: %w", err)
	}
	events := make([]providerdata.Event, 0, len(result.Items))
	for _, item := range result.Items {
		events = append(events, eventFromGoogleCalendarItem(item, calID))
	}
	sort.Slice(events, func(i, j int) bool {
		return events[i].Start.Before(events[j].Start)
	})
	return events, nil
}

// GetEvent fetches a single event by id.
func (g *GoogleProvider) GetEvent(ctx context.Context, calendarID, eventID string) (providerdata.Event, error) {
	service, err := g.getService(ctx)
	if err != nil {
		return providerdata.Event{}, err
	}
	calID := strings.TrimSpace(calendarID)
	if calID == "" {
		calID = "primary"
	}
	id := strings.TrimSpace(eventID)
	if id == "" {
		return providerdata.Event{}, fmt.Errorf("event_id is required")
	}
	item, err := service.Events.Get(calID, id).Context(ctx).Do()
	if err != nil {
		return providerdata.Event{}, fmt.Errorf("get calendar event: %w", err)
	}
	return eventFromGoogleCalendarItem(item, calID), nil
}

// CreateEvent inserts a new event on calendarID. An empty CalendarID on the
// Event falls back to the argument; callers typically pass the calendar id
// through both for clarity.
func (g *GoogleProvider) CreateEvent(ctx context.Context, calendarID string, ev providerdata.Event) (providerdata.Event, error) {
	service, err := g.getService(ctx)
	if err != nil {
		return providerdata.Event{}, err
	}
	calID := strings.TrimSpace(calendarID)
	if calID == "" {
		calID = strings.TrimSpace(ev.CalendarID)
	}
	if calID == "" {
		calID = "primary"
	}
	summary := strings.TrimSpace(ev.Summary)
	if summary == "" {
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
	item := &gcal.Event{
		Summary:     summary,
		Description: strings.TrimSpace(ev.Description),
		Location:    strings.TrimSpace(ev.Location),
		Start:       googleCalendarEventDateTime(ev.Start, ev.AllDay),
		End:         googleCalendarEventDateTime(end, ev.AllDay),
	}
	for _, attendee := range ev.Attendees {
		email := strings.TrimSpace(attendee.Email)
		if email == "" {
			continue
		}
		item.Attendees = append(item.Attendees, &gcal.EventAttendee{
			Email:          email,
			DisplayName:    strings.TrimSpace(attendee.Name),
			ResponseStatus: strings.TrimSpace(attendee.Response),
		})
	}
	created, err := service.Events.Insert(calID, item).Context(ctx).Do()
	if err != nil {
		return providerdata.Event{}, fmt.Errorf("create calendar event: %w", err)
	}
	return eventFromGoogleCalendarItem(created, calID), nil
}

// UpdateEvent overwrites the event identified by ev.ID. Empty fields are
// treated as unchanged for Summary, Description, Location, and Attendees;
// zero time values leave Start/End untouched.
func (g *GoogleProvider) UpdateEvent(ctx context.Context, calendarID string, ev providerdata.Event) (providerdata.Event, error) {
	service, err := g.getService(ctx)
	if err != nil {
		return providerdata.Event{}, err
	}
	calID := strings.TrimSpace(calendarID)
	if calID == "" {
		calID = strings.TrimSpace(ev.CalendarID)
	}
	if calID == "" {
		calID = "primary"
	}
	eventID := strings.TrimSpace(ev.ID)
	if eventID == "" {
		return providerdata.Event{}, fmt.Errorf("event_id is required")
	}
	existing, err := service.Events.Get(calID, eventID).Context(ctx).Do()
	if err != nil {
		return providerdata.Event{}, fmt.Errorf("get event for update: %w", err)
	}
	if s := strings.TrimSpace(ev.Summary); s != "" {
		existing.Summary = s
	}
	if ev.Description != "" {
		existing.Description = strings.TrimSpace(ev.Description)
	}
	if s := strings.TrimSpace(ev.Location); s != "" {
		existing.Location = s
	}
	if !ev.Start.IsZero() {
		existing.Start = googleCalendarEventDateTime(ev.Start, ev.AllDay)
	}
	if !ev.End.IsZero() {
		existing.End = googleCalendarEventDateTime(ev.End, ev.AllDay)
	}
	if len(ev.Attendees) > 0 {
		existing.Attendees = nil
		for _, attendee := range ev.Attendees {
			email := strings.TrimSpace(attendee.Email)
			if email == "" {
				continue
			}
			existing.Attendees = append(existing.Attendees, &gcal.EventAttendee{
				Email:          email,
				DisplayName:    strings.TrimSpace(attendee.Name),
				ResponseStatus: strings.TrimSpace(attendee.Response),
			})
		}
	}
	updated, err := service.Events.Update(calID, eventID, existing).Context(ctx).Do()
	if err != nil {
		return providerdata.Event{}, fmt.Errorf("update calendar event: %w", err)
	}
	return eventFromGoogleCalendarItem(updated, calID), nil
}

// DeleteEvent removes the given event from the calendar.
func (g *GoogleProvider) DeleteEvent(ctx context.Context, calendarID, eventID string) error {
	service, err := g.getService(ctx)
	if err != nil {
		return err
	}
	calID := strings.TrimSpace(calendarID)
	if calID == "" {
		calID = "primary"
	}
	id := strings.TrimSpace(eventID)
	if id == "" {
		return fmt.Errorf("event_id is required")
	}
	return service.Events.Delete(calID, id).Context(ctx).Do()
}

// RespondToInvite patches the authenticated user's attendee record on the
// event with the requested response status.
func (g *GoogleProvider) RespondToInvite(ctx context.Context, eventID string, resp providerdata.InviteResponse) error {
	service, err := g.getService(ctx)
	if err != nil {
		return err
	}
	id := strings.TrimSpace(eventID)
	if id == "" {
		return fmt.Errorf("event_id is required")
	}
	status := normalizeInviteStatus(resp.Status)
	if status == "" {
		return fmt.Errorf("invite status must be one of accepted, declined, tentative")
	}
	existing, err := service.Events.Get("primary", id).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("get invite event: %w", err)
	}
	self := findSelfAttendee(existing)
	if self == nil {
		return fmt.Errorf("authenticated user is not an attendee on event %s", id)
	}
	self.ResponseStatus = status
	if comment := strings.TrimSpace(resp.Comment); comment != "" {
		self.Comment = comment
	}
	patch := &gcal.Event{Attendees: existing.Attendees}
	if _, err := service.Events.Patch("primary", id, patch).Context(ctx).Do(); err != nil {
		return fmt.Errorf("patch invite response: %w", err)
	}
	return nil
}

func normalizeInviteStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "accepted", "accept", "yes":
		return "accepted"
	case "declined", "decline", "no":
		return "declined"
	case "tentative", "maybe":
		return "tentative"
	default:
		return ""
	}
}

func findSelfAttendee(event *gcal.Event) *gcal.EventAttendee {
	if event == nil {
		return nil
	}
	for _, attendee := range event.Attendees {
		if attendee != nil && attendee.Self {
			return attendee
		}
	}
	return nil
}

func parseEventTime(eventTime *gcal.EventDateTime) (time.Time, bool) {
	if eventTime == nil {
		return time.Time{}, false
	}
	if eventTime.DateTime != "" {
		t, err := time.Parse(time.RFC3339, eventTime.DateTime)
		if err == nil {
			return t, false
		}
	}
	if eventTime.Date != "" {
		t, err := time.Parse("2006-01-02", eventTime.Date)
		if err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func googleCalendarEventDateTime(value time.Time, allDay bool) *gcal.EventDateTime {
	if allDay {
		return &gcal.EventDateTime{Date: value.In(time.Local).Format("2006-01-02")}
	}
	return &gcal.EventDateTime{DateTime: value.Format(time.RFC3339)}
}

func eventFromGoogleCalendarItem(item *gcal.Event, calendarID string) providerdata.Event {
	event := providerdata.Event{
		ID:          item.Id,
		CalendarID:  calendarID,
		Summary:     item.Summary,
		Description: item.Description,
		Location:    item.Location,
		Status:      item.Status,
		Recurring:   item.RecurringEventId != "",
	}
	if event.Summary == "" {
		event.Summary = "(No title)"
	}
	if item.Organizer != nil {
		event.Organizer = item.Organizer.Email
	}
	for _, att := range item.Attendees {
		if att == nil {
			continue
		}
		event.Attendees = append(event.Attendees, providerdata.Attendee{
			Email:    att.Email,
			Name:     att.DisplayName,
			Response: att.ResponseStatus,
		})
	}
	if item.ICalUID != "" {
		event.ICSUID = item.ICalUID
	}
	if item.Reminders != nil && !item.Reminders.UseDefault {
		// First explicit override wins; the Google API orders overrides by priority.
		for _, override := range item.Reminders.Overrides {
			if override == nil {
				continue
			}
			minutes := int(override.Minutes)
			event.ReminderMinutes = &minutes
			break
		}
	}
	event.Start, event.AllDay = parseEventTime(item.Start)
	event.End, _ = parseEventTime(item.End)
	return event
}
