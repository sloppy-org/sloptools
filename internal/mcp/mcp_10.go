package mcp

import (
	"fmt"
	"time"

	tabcalendar "github.com/sloppy-org/sloptools/internal/calendar"
)

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

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
