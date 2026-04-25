package calendar

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/providerdata"
	gcal "google.golang.org/api/calendar/v3"
)

// QueryFreeBusy reports busy windows for the given participants across the
// requested time range. Google Calendar reports only busy blocks; the
// returned slots all carry status "busy".
func (g *GoogleProvider) QueryFreeBusy(ctx context.Context, participants []string, rng TimeRange) ([]providerdata.FreeBusySlot, error) {
	service, err := g.getService(ctx)
	if err != nil {
		return nil, err
	}
	if len(participants) == 0 {
		return nil, fmt.Errorf("participants is required")
	}
	if rng.Start.IsZero() || rng.End.IsZero() {
		return nil, fmt.Errorf("start and end are required")
	}
	items := make([]*gcal.FreeBusyRequestItem, 0, len(participants))
	for _, p := range participants {
		if email := strings.TrimSpace(p); email != "" {
			items = append(items, &gcal.FreeBusyRequestItem{Id: email})
		}
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("no valid participant emails")
	}
	resp, err := service.Freebusy.Query(&gcal.FreeBusyRequest{
		TimeMin:  rng.Start.Format(time.RFC3339),
		TimeMax:  rng.End.Format(time.RFC3339),
		TimeZone: "UTC",
		Items:    items,
	}).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("freebusy query: %w", err)
	}
	slots := make([]providerdata.FreeBusySlot, 0)
	for _, item := range items {
		cal, ok := resp.Calendars[item.Id]
		if !ok {
			continue
		}
		for _, busy := range cal.Busy {
			var start, end time.Time
			if busy.Start != "" {
				if t, err := time.Parse(time.RFC3339, busy.Start); err == nil {
					start = t
				}
			}
			if busy.End != "" {
				if t, err := time.Parse(time.RFC3339, busy.End); err == nil {
					end = t
				}
			}
			if !start.IsZero() && !end.IsZero() {
				slots = append(slots, providerdata.FreeBusySlot{
					Participant: item.Id,
					Start:       start,
					End:         end,
					Status:      "busy",
				})
			}
		}
	}
	return slots, nil
}
