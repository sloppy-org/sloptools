// Package calendarbrief implements the pre-meeting people-brief trigger
// described in issue #92. A long-lived ticker scans the next-15-minute
// calendar window and, for every event with at least one named participant
// resolvable to a brain/people/ note, emits a structured notification onto
// the configured channel. The trigger emits at most one notification per
// event ID; events without resolvable participants produce no traffic.
package calendarbrief

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Event is the trimmed view of a calendar event the trigger reasons about.
// Backends populate this struct from providerdata.Event before dispatch so
// the trigger stays free of provider-specific types.
type Event struct {
	ID          string
	Summary     string
	Description string
	Start       time.Time
	End         time.Time
	AllDay      bool
	Attendees   []Attendee
}

// Attendee is a single calendar invitee.
type Attendee struct {
	Email string
	Name  string
}

// Person is a brain/people/ note resolved for a calendar attendee.
type Person struct {
	Name  string
	Path  string
	Email string
}

// Lister returns the events whose start time falls inside [start, end).
type Lister func(ctx context.Context, start, end time.Time) ([]Event, error)

// PersonResolver maps an attendee to a brain/people/ note. Returns ok=false
// when no canonical note exists for the attendee.
type PersonResolver func(ctx context.Context, attendee Attendee) (Person, bool, error)

// BriefBuilder assembles the people.brief payload for a single resolved
// person. Implementations typically delegate to the brain.people.brief tool.
type BriefBuilder func(ctx context.Context, person Person) (map[string]interface{}, error)

// Notification is the payload the trigger pushes on the configured channel.
// Slopshell renders this into a non-blocking toast or sidebar entry.
type Notification struct {
	EventID    string                   `json:"event_id"`
	EventTitle string                   `json:"event_title"`
	EventStart time.Time                `json:"event_start"`
	Briefs     []map[string]interface{} `json:"briefs"`
}

// Emitter receives the notification and is responsible for delivering it to
// the slopshell notification channel. The trigger does not retry on error.
type Emitter func(ctx context.Context, n Notification) error

// Config bundles the dependencies a Trigger needs.
type Config struct {
	List      Lister
	Resolve   PersonResolver
	Build     BriefBuilder
	Emit      Emitter
	Window    time.Duration // look-ahead, default 15m
	Retention time.Duration // dedup retention, default 24h
	Now       func() time.Time
}

// DefaultWindow matches issue #92 ("next-15-minute calendar window").
const DefaultWindow = 15 * time.Minute

// DefaultRetention keeps the dedup set for a full work day so a daemon
// restart inside the same shift does not double-fire.
const DefaultRetention = 24 * time.Hour

// Trigger scans the calendar window and emits at most one brief notification
// per event ID. The dedup set is mutex-guarded; the daemon is expected to
// call Tick from a single goroutine but concurrent Tick is safe.
type Trigger struct {
	cfg     Config
	mu      sync.Mutex
	emitted map[string]time.Time // event ID -> event start time
}

// New validates the configuration and returns a ready Trigger.
func New(cfg Config) (*Trigger, error) {
	if cfg.List == nil {
		return nil, errors.New("calendarbrief: List is required")
	}
	if cfg.Resolve == nil {
		return nil, errors.New("calendarbrief: Resolve is required")
	}
	if cfg.Build == nil {
		return nil, errors.New("calendarbrief: Build is required")
	}
	if cfg.Emit == nil {
		return nil, errors.New("calendarbrief: Emit is required")
	}
	if cfg.Window <= 0 {
		cfg.Window = DefaultWindow
	}
	if cfg.Retention <= 0 {
		cfg.Retention = DefaultRetention
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Trigger{cfg: cfg, emitted: map[string]time.Time{}}, nil
}

// Tick scans the calendar window and emits notifications for all newly
// briefable events. Errors from individual events do not stop the loop;
// the first encountered error is returned after the loop completes so a
// single misbehaving event cannot silence the rest of the window.
func (t *Trigger) Tick(ctx context.Context) error {
	now := t.cfg.Now()
	end := now.Add(t.cfg.Window)
	events, err := t.cfg.List(ctx, now, end)
	if err != nil {
		return fmt.Errorf("calendarbrief: list events: %w", err)
	}
	var firstErr error
	for _, ev := range events {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := t.processEvent(ctx, ev); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	t.evict(now)
	return firstErr
}

func (t *Trigger) processEvent(ctx context.Context, ev Event) error {
	if shouldSkip(ev) {
		return nil
	}
	if t.alreadyEmitted(ev.ID) {
		return nil
	}
	briefs, err := t.collectBriefs(ctx, ev)
	if err != nil {
		return err
	}
	if len(briefs) == 0 {
		return nil
	}
	n := Notification{
		EventID:    ev.ID,
		EventTitle: ev.Summary,
		EventStart: ev.Start,
		Briefs:     briefs,
	}
	if err := t.cfg.Emit(ctx, n); err != nil {
		return fmt.Errorf("calendarbrief: emit %s: %w", ev.ID, err)
	}
	t.markEmitted(ev.ID, ev.Start)
	return nil
}

func (t *Trigger) collectBriefs(ctx context.Context, ev Event) ([]map[string]interface{}, error) {
	seen := map[string]struct{}{}
	var out []map[string]interface{}
	for _, att := range ev.Attendees {
		person, ok, err := t.cfg.Resolve(ctx, att)
		if err != nil {
			return nil, fmt.Errorf("calendarbrief: resolve %q: %w", att.Email, err)
		}
		if !ok {
			continue
		}
		if _, dup := seen[person.Path]; dup {
			continue
		}
		seen[person.Path] = struct{}{}
		brief, err := t.cfg.Build(ctx, person)
		if err != nil {
			return nil, fmt.Errorf("calendarbrief: build brief for %q: %w", person.Name, err)
		}
		if brief == nil {
			continue
		}
		out = append(out, brief)
	}
	return out, nil
}

func (t *Trigger) alreadyEmitted(id string) bool {
	if id == "" {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	_, ok := t.emitted[id]
	return ok
}

func (t *Trigger) markEmitted(id string, start time.Time) {
	if id == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.emitted[id] = start
}

func (t *Trigger) evict(now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	cutoff := now.Add(-t.cfg.Retention)
	for id, start := range t.emitted {
		if start.Before(cutoff) {
			delete(t.emitted, id)
		}
	}
}

// Run drives Tick on the supplied interval until ctx is cancelled. Tick
// errors that are not context cancellations are reported via logErr (when
// non-nil) without stopping the loop, so transient calendar API failures do
// not kill the trigger.
func (t *Trigger) Run(ctx context.Context, interval time.Duration, logErr func(error)) error {
	if interval <= 0 {
		return errors.New("calendarbrief: Run interval must be positive")
	}
	if err := t.tickAndLog(ctx, logErr); err != nil {
		return err
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := t.tickAndLog(ctx, logErr); err != nil {
				return err
			}
		}
	}
}

func (t *Trigger) tickAndLog(ctx context.Context, logErr func(error)) error {
	err := t.Tick(ctx)
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if logErr != nil {
		logErr(err)
	}
	return nil
}
