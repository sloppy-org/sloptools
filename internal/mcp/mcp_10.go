package mcp

import (
	"context"
	"fmt"
	tabcalendar "github.com/sloppy-org/sloptools/internal/calendar"
	"github.com/sloppy-org/sloptools/internal/groupware"
	"strings"
	"time"
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
