package ics

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/providerdata"
)

// ICSEvent represents an event from an ICS feed.
type ICSEvent struct {
	Summary     string
	Start       time.Time
	End         time.Time
	Location    string
	Description string
	Calendar    string
	AllDay      bool
}

// Config holds the ICS calendar configuration.
type Config struct {
	ICSCalendars map[string]string `json:"ics_calendars"`
}

func configPath() string {
	return filepath.Join(defaultConfigDir(), "calendars.json")
}

// LoadCalendars loads ICS calendar URLs from config.
func LoadCalendars() (map[string]string, error) {
	path := configPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]string), nil
		}
		return nil, err
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	if config.ICSCalendars == nil {
		return make(map[string]string), nil
	}
	return config.ICSCalendars, nil
}

// SaveCalendars saves ICS calendar URLs to config.
func SaveCalendars(calendars map[string]string) error {
	path := configPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	config := Config{ICSCalendars: calendars}

	// Load existing config to preserve other settings
	if data, err := os.ReadFile(path); err == nil {
		var existing map[string]interface{}
		if json.Unmarshal(data, &existing) == nil {
			existing["ics_calendars"] = calendars
			data, _ := json.MarshalIndent(existing, "", "  ")
			return os.WriteFile(path, data, 0644)
		}
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// AddCalendar adds an ICS calendar to config.
func AddCalendar(name, url string) error {
	calendars, err := LoadCalendars()
	if err != nil {
		return err
	}
	calendars[name] = url
	return SaveCalendars(calendars)
}

// RemoveCalendar removes an ICS calendar from config.
func RemoveCalendar(name string) error {
	calendars, err := LoadCalendars()
	if err != nil {
		return err
	}
	if _, ok := calendars[name]; !ok {
		return fmt.Errorf("calendar %q not found", name)
	}
	delete(calendars, name)
	return SaveCalendars(calendars)
}

// Client provides access to ICS calendar feeds.
type Client struct {
	calendars  map[string]string
	httpClient *http.Client
}

// New creates a new ICS calendar client.
func New() (*Client, error) {
	calendars, err := LoadCalendars()
	if err != nil {
		return nil, err
	}
	return &Client{
		calendars:  calendars,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// ListCalendars returns configured ICS calendars.
func (c *Client) ListCalendars() []providerdata.Calendar {
	var result []providerdata.Calendar
	for name := range c.calendars {
		result = append(result, providerdata.Calendar{
			ID:   name,
			Name: name,
		})
	}
	return result
}

// GetEvents retrieves events from a specific ICS calendar.
func (c *Client) GetEvents(calendarName string, timeMin, timeMax time.Time) ([]ICSEvent, error) {
	url, ok := c.calendars[calendarName]
	if !ok {
		return nil, fmt.Errorf("calendar %q not found in config", calendarName)
	}

	content, err := c.fetchICS(url)
	if err != nil {
		return nil, err
	}

	return parseICSEvents(content, calendarName, timeMin, timeMax)
}

// GetAllEvents retrieves events from all configured ICS calendars.
func (c *Client) GetAllEvents(timeMin, timeMax time.Time) ([]ICSEvent, error) {
	var allEvents []ICSEvent

	for name := range c.calendars {
		events, err := c.GetEvents(name, timeMin, timeMax)
		if err != nil {
			continue // Skip calendars that fail
		}
		allEvents = append(allEvents, events...)
	}

	sort.Slice(allEvents, func(i, j int) bool {
		return allEvents[i].Start.Before(allEvents[j].Start)
	})

	return allEvents, nil
}

func (c *Client) fetchICS(url string) (string, error) {
	resp, err := c.httpClient.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to fetch ICS: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d fetching ICS", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read ICS content: %w", err)
	}

	return string(data), nil
}

func parseICSEvents(content, calendarName string, timeMin, timeMax time.Time) ([]ICSEvent, error) {
	if timeMin.IsZero() {
		timeMin = time.Now()
	}
	if timeMax.IsZero() {
		timeMax = timeMin.Add(30 * 24 * time.Hour)
	}

	var events []ICSEvent
	var current *ICSEvent

	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		switch {
		case line == "BEGIN:VEVENT":
			current = &ICSEvent{Calendar: calendarName}

		case line == "END:VEVENT" && current != nil:
			if !current.Start.IsZero() &&
				(current.Start.After(timeMin) || current.Start.Equal(timeMin)) &&
				current.Start.Before(timeMax) {
				if current.Summary == "" {
					current.Summary = "(No title)"
				}
				events = append(events, *current)
			}
			current = nil

		case current != nil:
			if strings.HasPrefix(line, "SUMMARY:") {
				current.Summary = line[8:]
			} else if strings.HasPrefix(line, "DTSTART") {
				current.Start, current.AllDay = parseICSDateTime(extractValue(line))
			} else if strings.HasPrefix(line, "DTEND") {
				current.End, _ = parseICSDateTime(extractValue(line))
			} else if strings.HasPrefix(line, "LOCATION:") {
				current.Location = line[9:]
			} else if strings.HasPrefix(line, "DESCRIPTION:") {
				current.Description = line[12:]
			}
		}
	}

	sort.Slice(events, func(i, j int) bool {
		return events[i].Start.Before(events[j].Start)
	})

	return events, scanner.Err()
}

func extractValue(line string) string {
	if idx := strings.Index(line, ":"); idx != -1 {
		return line[idx+1:]
	}
	return ""
}

func parseICSDateTime(dtStr string) (time.Time, bool) {
	dtStr = strings.TrimSuffix(dtStr, "Z")

	// Try datetime format first
	if t, err := time.Parse("20060102T150405", dtStr); err == nil {
		return t, false
	}

	// Try date-only format (all-day event)
	if t, err := time.Parse("20060102", dtStr); err == nil {
		return t, true
	}

	return time.Time{}, false
}

func defaultConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ".sloppy"
	}
	return filepath.Join(home, ".config", "sloppy")
}
