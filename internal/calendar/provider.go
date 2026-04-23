// Package calendar exposes a core Provider interface plus capability
// interfaces mirroring internal/email/provider.go. Concrete backends (Google
// Calendar today, Exchange EWS next) live alongside this file and satisfy
// whichever capabilities they support.
package calendar

import (
	"context"
	"errors"
	"time"

	"github.com/sloppy-org/sloptools/internal/providerdata"
)

// ErrUnsupported is returned by a capability method when the underlying
// provider cannot satisfy it yet. Callers probe via groupware.Supports[T] and
// fall back when the provider does not implement the required interface.
var ErrUnsupported = errors.New("calendar: provider does not support this capability")

// TimeRange is the half-open [Start, End) window used for event queries.
type TimeRange struct {
	Start time.Time
	End   time.Time
}

// Provider is the core capability every calendar backend must implement.
// ListEvents returns events whose start falls inside rng; callers cap and
// sort the merged set client-side.
type Provider interface {
	ListCalendars(ctx context.Context) ([]providerdata.Calendar, error)
	ListEvents(ctx context.Context, calendarID string, rng TimeRange) ([]providerdata.Event, error)
	GetEvent(ctx context.Context, calendarID, eventID string) (providerdata.Event, error)
	ProviderName() string
	Close() error
}

// EventMutator lets callers create, update, or delete events. Providers that
// only offer read access (for example ICS subscriptions) omit it.
type EventMutator interface {
	CreateEvent(ctx context.Context, calendarID string, ev providerdata.Event) (providerdata.Event, error)
	UpdateEvent(ctx context.Context, calendarID string, ev providerdata.Event) (providerdata.Event, error)
	DeleteEvent(ctx context.Context, calendarID, eventID string) error
}

// InviteResponder lets the authenticated user accept, decline, or tentatively
// respond to a meeting invitation they have received.
type InviteResponder interface {
	RespondToInvite(ctx context.Context, eventID string, resp providerdata.InviteResponse) error
}

// FreeBusyLooker reports busy windows for one or more participants so callers
// can schedule around them.
type FreeBusyLooker interface {
	QueryFreeBusy(ctx context.Context, participants []string, rng TimeRange) ([]providerdata.FreeBusySlot, error)
}

// RecurrenceExpander materialises individual occurrences of a recurring event
// for the given window.
type RecurrenceExpander interface {
	ExpandOccurrences(ctx context.Context, calendarID, eventID string, rng TimeRange) ([]providerdata.Event, error)
}

// ICSExporter renders an event as an RFC5545 iCalendar payload. Backends that
// cannot produce RFC5545 output return ErrUnsupported.
type ICSExporter interface {
	ExportICS(ctx context.Context, calendarID, eventID string) ([]byte, error)
}

// EventSearcher lets callers combine a time window with a backend-native
// free-text query and an explicit cap. Preserves the pre-refactor
// calendar_events byte-for-byte output without forcing every backend to
// implement server-side search.
type EventSearcher interface {
	SearchEvents(ctx context.Context, calendarID string, rng TimeRange, query string, maxResults int64) ([]providerdata.Event, error)
}
