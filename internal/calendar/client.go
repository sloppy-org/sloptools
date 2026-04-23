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

type Client struct {
	service *gcal.Service
	auth    *googleauth.Session
}

func New(ctx context.Context) (*Client, error) {
	return NewWithFiles(ctx, "", "")
}

func NewWithFiles(ctx context.Context, credentialsPath, tokenPath string) (*Client, error) {
	auth, err := googleauth.New(credentialsPath, tokenPath, googleauth.DefaultScopes)
	if err != nil {
		return nil, err
	}
	tokenSource, err := auth.TokenSource(ctx)
	if err != nil {
		return nil, err
	}
	service, err := gcal.NewService(ctx, option.WithTokenSource(tokenSource))
	if err != nil {
		return nil, fmt.Errorf("create Google Calendar service: %w", err)
	}
	return &Client{service: service, auth: auth}, nil
}

func (c *Client) GetAuthURL() string {
	if c == nil || c.auth == nil {
		return ""
	}
	return c.auth.GetAuthURL()
}

func (c *Client) ExchangeCode(ctx context.Context, code string) error {
	if c == nil || c.auth == nil {
		return fmt.Errorf("google calendar auth is not configured")
	}
	return c.auth.ExchangeCode(ctx, code)
}

func (c *Client) ListCalendars(ctx context.Context) ([]providerdata.Calendar, error) {
	result, err := c.service.CalendarList.List().Context(ctx).Do()
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

type GetEventsOptions struct {
	CalendarID string
	TimeMin    time.Time
	TimeMax    time.Time
	MaxResults int64
	Query      string
}

type CreateEventOptions struct {
	CalendarID  string
	Summary     string
	Description string
	Location    string
	Start       time.Time
	End         time.Time
	AllDay      bool
	Attendees   []string
}

func (c *Client) GetEvents(ctx context.Context, opts GetEventsOptions) ([]providerdata.Event, error) {
	if opts.CalendarID == "" {
		opts.CalendarID = "primary"
	}
	if opts.TimeMin.IsZero() {
		opts.TimeMin = time.Now()
	}
	if opts.TimeMax.IsZero() {
		opts.TimeMax = opts.TimeMin.Add(30 * 24 * time.Hour)
	}
	if opts.MaxResults == 0 {
		opts.MaxResults = 100
	}

	call := c.service.Events.List(opts.CalendarID).
		Context(ctx).
		TimeMin(opts.TimeMin.Format(time.RFC3339)).
		TimeMax(opts.TimeMax.Format(time.RFC3339)).
		MaxResults(opts.MaxResults).
		SingleEvents(true).
		OrderBy("startTime")
	if opts.Query != "" {
		call = call.Q(opts.Query)
	}

	result, err := call.Do()
	if err != nil {
		return nil, fmt.Errorf("get calendar events: %w", err)
	}

	events := make([]providerdata.Event, 0, len(result.Items))
	for _, item := range result.Items {
		events = append(events, eventFromGoogleCalendarItem(item, opts.CalendarID))
	}
	sort.Slice(events, func(i, j int) bool {
		return events[i].Start.Before(events[j].Start)
	})
	return events, nil
}

func (c *Client) CreateEvent(ctx context.Context, opts CreateEventOptions) (providerdata.Event, error) {
	if c == nil || c.service == nil {
		return providerdata.Event{}, fmt.Errorf("google calendar client is not configured")
	}
	if strings.TrimSpace(opts.CalendarID) == "" {
		opts.CalendarID = "primary"
	}
	opts.Summary = strings.TrimSpace(opts.Summary)
	if opts.Summary == "" {
		return providerdata.Event{}, fmt.Errorf("summary is required")
	}
	if opts.Start.IsZero() {
		return providerdata.Event{}, fmt.Errorf("start is required")
	}
	if opts.End.IsZero() {
		if opts.AllDay {
			opts.End = opts.Start.Add(24 * time.Hour)
		} else {
			opts.End = opts.Start.Add(time.Hour)
		}
	}
	if !opts.End.After(opts.Start) {
		return providerdata.Event{}, fmt.Errorf("end must be after start")
	}

	item := &gcal.Event{
		Summary:     opts.Summary,
		Description: strings.TrimSpace(opts.Description),
		Location:    strings.TrimSpace(opts.Location),
		Start:       googleCalendarEventDateTime(opts.Start, opts.AllDay),
		End:         googleCalendarEventDateTime(opts.End, opts.AllDay),
	}
	for _, attendee := range opts.Attendees {
		email := strings.TrimSpace(attendee)
		if email == "" {
			continue
		}
		item.Attendees = append(item.Attendees, &gcal.EventAttendee{Email: email})
	}

	created, err := c.service.Events.Insert(opts.CalendarID, item).Context(ctx).Do()
	if err != nil {
		return providerdata.Event{}, fmt.Errorf("create calendar event: %w", err)
	}
	return eventFromGoogleCalendarItem(created, opts.CalendarID), nil
}

type UpdateEventOptions struct {
	CalendarID  string
	EventID     string
	Summary     string
	Description string
	Location    string
	Start       time.Time
	End         time.Time
	AllDay      bool
	Attendees   []string
}

func (c *Client) UpdateEvent(ctx context.Context, opts UpdateEventOptions) (providerdata.Event, error) {
	if c == nil || c.service == nil {
		return providerdata.Event{}, fmt.Errorf("google calendar client is not configured")
	}
	calID := strings.TrimSpace(opts.CalendarID)
	if calID == "" {
		calID = "primary"
	}
	eventID := strings.TrimSpace(opts.EventID)
	if eventID == "" {
		return providerdata.Event{}, fmt.Errorf("event_id is required")
	}
	existing, err := c.service.Events.Get(calID, eventID).Context(ctx).Do()
	if err != nil {
		return providerdata.Event{}, fmt.Errorf("get event for update: %w", err)
	}
	if s := strings.TrimSpace(opts.Summary); s != "" {
		existing.Summary = s
	}
	if opts.Description != "" {
		existing.Description = strings.TrimSpace(opts.Description)
	}
	if s := strings.TrimSpace(opts.Location); s != "" {
		existing.Location = s
	}
	if !opts.Start.IsZero() {
		existing.Start = googleCalendarEventDateTime(opts.Start, opts.AllDay)
	}
	if !opts.End.IsZero() {
		existing.End = googleCalendarEventDateTime(opts.End, opts.AllDay)
	}
	if len(opts.Attendees) > 0 {
		existing.Attendees = nil
		for _, email := range opts.Attendees {
			e := strings.TrimSpace(email)
			if e != "" {
				existing.Attendees = append(existing.Attendees, &gcal.EventAttendee{Email: e})
			}
		}
	}
	updated, err := c.service.Events.Update(calID, eventID, existing).Context(ctx).Do()
	if err != nil {
		return providerdata.Event{}, fmt.Errorf("update calendar event: %w", err)
	}
	return eventFromGoogleCalendarItem(updated, calID), nil
}

func (c *Client) DeleteEvent(ctx context.Context, calendarID, eventID string) error {
	if c == nil || c.service == nil {
		return fmt.Errorf("google calendar client is not configured")
	}
	calID := strings.TrimSpace(calendarID)
	if calID == "" {
		calID = "primary"
	}
	id := strings.TrimSpace(eventID)
	if id == "" {
		return fmt.Errorf("event_id is required")
	}
	return c.service.Events.Delete(calID, id).Context(ctx).Do()
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
