package calendarbrief

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sloppy-org/sloptools/internal/brain"
)

type stubChannel struct {
	notifications []Notification
}

func (s *stubChannel) emit(_ context.Context, n Notification) error {
	s.notifications = append(s.notifications, n)
	return nil
}

func resolverFromMap(known map[string]Person) PersonResolver {
	return func(_ context.Context, att Attendee) (Person, bool, error) {
		if person, ok := known[att.Email]; ok {
			return person, true, nil
		}
		if person, ok := known[att.Name]; ok {
			return person, true, nil
		}
		return Person{}, false, nil
	}
}

func passthroughBuilder(_ context.Context, person Person) (map[string]interface{}, error) {
	return map[string]interface{}{
		"person":      person.Name,
		"person_path": person.Path,
	}, nil
}

func newTestTrigger(t *testing.T, cfg Config) (*Trigger, *stubChannel) {
	t.Helper()
	channel := &stubChannel{}
	if cfg.Emit == nil {
		cfg.Emit = channel.emit
	}
	if cfg.Build == nil {
		cfg.Build = passthroughBuilder
	}
	tr, err := New(cfg)
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	return tr, channel
}

func TestNewRejectsMissingDependencies(t *testing.T) {
	noop := func(context.Context, time.Time, time.Time) ([]Event, error) { return nil, nil }
	resolveNoop := func(context.Context, Attendee) (Person, bool, error) { return Person{}, false, nil }
	emitNoop := func(context.Context, Notification) error { return nil }
	cases := []struct {
		name string
		cfg  Config
	}{
		{"missing list", Config{Resolve: resolveNoop, Build: passthroughBuilder, Emit: emitNoop}},
		{"missing resolve", Config{List: noop, Build: passthroughBuilder, Emit: emitNoop}},
		{"missing build", Config{List: noop, Resolve: resolveNoop, Emit: emitNoop}},
		{"missing emit", Config{List: noop, Resolve: resolveNoop, Build: passthroughBuilder}},
	}
	for _, tc := range cases {
		if _, err := New(tc.cfg); err == nil {
			t.Fatalf("%s: expected error", tc.name)
		}
	}
}

func TestTickEmitsBriefForResolvableAttendee(t *testing.T) {
	now := time.Date(2026, 5, 4, 9, 0, 0, 0, time.UTC)
	person := Person{Name: "Ada Lovelace", Path: "/vault/brain/people/Ada Lovelace.md"}
	event := Event{
		ID:      "evt-1",
		Summary: "Plasma Orga sync",
		Start:   now.Add(7 * time.Minute),
		End:     now.Add(37 * time.Minute),
		Attendees: []Attendee{
			{Email: "ada@example.com", Name: "Ada Lovelace"},
			{Email: "stranger@example.com", Name: "Unknown"},
		},
	}
	cfg := Config{
		List: func(_ context.Context, _, _ time.Time) ([]Event, error) {
			return []Event{event}, nil
		},
		Resolve: resolverFromMap(map[string]Person{"ada@example.com": person}),
		Now:     func() time.Time { return now },
	}
	tr, channel := newTestTrigger(t, cfg)
	if err := tr.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(channel.notifications) != 1 {
		t.Fatalf("notifications = %d, want 1", len(channel.notifications))
	}
	got := channel.notifications[0]
	if got.EventID != "evt-1" || got.EventTitle != "Plasma Orga sync" || !got.EventStart.Equal(event.Start) {
		t.Fatalf("notification metadata = %#v", got)
	}
	if len(got.Briefs) != 1 || got.Briefs[0]["person"] != "Ada Lovelace" {
		t.Fatalf("briefs = %#v, want one for Ada", got.Briefs)
	}
}

func TestTickEmitsAtMostOnceAcrossTicks(t *testing.T) {
	now := time.Date(2026, 5, 4, 9, 0, 0, 0, time.UTC)
	person := Person{Name: "Ada Lovelace", Path: "/vault/brain/people/Ada Lovelace.md"}
	event := Event{
		ID:        "evt-dup",
		Summary:   "Sync",
		Start:     now.Add(5 * time.Minute),
		Attendees: []Attendee{{Email: "ada@example.com", Name: "Ada Lovelace"}},
	}
	cfg := Config{
		List: func(_ context.Context, _, _ time.Time) ([]Event, error) {
			return []Event{event}, nil
		},
		Resolve: resolverFromMap(map[string]Person{"ada@example.com": person}),
		Now:     func() time.Time { return now },
	}
	tr, channel := newTestTrigger(t, cfg)
	for i := 0; i < 3; i++ {
		if err := tr.Tick(context.Background()); err != nil {
			t.Fatalf("Tick %d: %v", i, err)
		}
	}
	if len(channel.notifications) != 1 {
		t.Fatalf("notifications = %d after three ticks, want 1", len(channel.notifications))
	}
}

func TestTickSilentForEventsWithoutResolvableParticipants(t *testing.T) {
	now := time.Date(2026, 5, 4, 9, 0, 0, 0, time.UTC)
	event := Event{
		ID:        "evt-noise",
		Summary:   "External webinar",
		Start:     now.Add(3 * time.Minute),
		Attendees: []Attendee{{Email: "stranger@example.com", Name: "Stranger"}},
	}
	cfg := Config{
		List: func(_ context.Context, _, _ time.Time) ([]Event, error) {
			return []Event{event}, nil
		},
		Resolve: resolverFromMap(nil),
		Now:     func() time.Time { return now },
	}
	tr, channel := newTestTrigger(t, cfg)
	if err := tr.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(channel.notifications) != 0 {
		t.Fatalf("notifications = %#v, want empty", channel.notifications)
	}
	if tr.alreadyEmitted("evt-noise") {
		t.Fatalf("dedup set must not record events that produced no brief")
	}
}

func TestTickSkipRules(t *testing.T) {
	now := time.Date(2026, 5, 4, 9, 0, 0, 0, time.UTC)
	person := Person{Name: "Ada", Path: "/vault/brain/people/Ada.md"}
	resolvable := []Attendee{{Email: "ada@example.com", Name: "Ada"}}
	cases := []struct {
		name  string
		event Event
	}{
		{
			name: "maker time block",
			event: Event{
				ID: "skip-maker", Summary: "Maker time — deep work",
				Start: now.Add(5 * time.Minute), Attendees: resolvable,
			},
		},
		{
			name: "family floor (Emil)",
			event: Event{
				ID: "skip-emil", Summary: "Emil bedtime",
				Start: now.Add(5 * time.Minute), Attendees: resolvable,
			},
		},
		{
			name: "family floor (Mama)",
			event: Event{
				ID: "skip-mama", Summary: "Call Mama",
				Start: now.Add(5 * time.Minute), Attendees: resolvable,
			},
		},
		{
			name: "all-day Kleinkram rotation",
			event: Event{
				ID: "skip-kleinkram", Summary: "Kleinkram rotation",
				AllDay: true, Start: now.Add(2 * time.Minute), Attendees: resolvable,
			},
		},
		{
			name: "no_brief: true marker",
			event: Event{
				ID: "skip-nobrief", Summary: "Standup",
				Description: "Agenda items below.\nno_brief: true\n",
				Start:       now.Add(5 * time.Minute), Attendees: resolvable,
			},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{
				List: func(_ context.Context, _, _ time.Time) ([]Event, error) {
					return []Event{tc.event}, nil
				},
				Resolve: resolverFromMap(map[string]Person{"ada@example.com": person}),
				Now:     func() time.Time { return now },
			}
			tr, channel := newTestTrigger(t, cfg)
			if err := tr.Tick(context.Background()); err != nil {
				t.Fatalf("Tick: %v", err)
			}
			if len(channel.notifications) != 0 {
				t.Fatalf("expected skip, got %#v", channel.notifications)
			}
		})
	}
}

func TestTickKleinkramTimedEventStillEmits(t *testing.T) {
	now := time.Date(2026, 5, 4, 9, 0, 0, 0, time.UTC)
	person := Person{Name: "Ada", Path: "/vault/brain/people/Ada.md"}
	event := Event{
		ID:        "kleinkram-meet",
		Summary:   "Kleinkram coordination call",
		Start:     now.Add(5 * time.Minute),
		Attendees: []Attendee{{Email: "ada@example.com", Name: "Ada"}},
	}
	cfg := Config{
		List: func(_ context.Context, _, _ time.Time) ([]Event, error) {
			return []Event{event}, nil
		},
		Resolve: resolverFromMap(map[string]Person{"ada@example.com": person}),
		Now:     func() time.Time { return now },
	}
	tr, channel := newTestTrigger(t, cfg)
	if err := tr.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(channel.notifications) != 1 {
		t.Fatalf("kleinkram skip must be all-day-only; got %d notifications", len(channel.notifications))
	}
}

func TestTickRequestsExactlyTheConfiguredWindow(t *testing.T) {
	now := time.Date(2026, 5, 4, 9, 0, 0, 0, time.UTC)
	var seenStart, seenEnd time.Time
	cfg := Config{
		Window: 10 * time.Minute,
		List: func(_ context.Context, start, end time.Time) ([]Event, error) {
			seenStart, seenEnd = start, end
			return nil, nil
		},
		Resolve: resolverFromMap(nil),
		Now:     func() time.Time { return now },
	}
	tr, _ := newTestTrigger(t, cfg)
	if err := tr.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if !seenStart.Equal(now) || !seenEnd.Equal(now.Add(10*time.Minute)) {
		t.Fatalf("window = [%s, %s], want [%s, %s]", seenStart, seenEnd, now, now.Add(10*time.Minute))
	}
}

func TestTickPropagatesListError(t *testing.T) {
	want := errors.New("calendar offline")
	cfg := Config{
		List:    func(context.Context, time.Time, time.Time) ([]Event, error) { return nil, want },
		Resolve: resolverFromMap(nil),
		Now:     func() time.Time { return time.Now() },
	}
	tr, _ := newTestTrigger(t, cfg)
	if err := tr.Tick(context.Background()); !errors.Is(err, want) {
		t.Fatalf("Tick err = %v, want wrapping %v", err, want)
	}
}

func TestTickEvictsOldEntriesAfterRetention(t *testing.T) {
	now := time.Date(2026, 5, 4, 9, 0, 0, 0, time.UTC)
	person := Person{Name: "Ada", Path: "/vault/brain/people/Ada.md"}
	first := Event{
		ID: "evt-old", Summary: "Old sync",
		Start:     now.Add(5 * time.Minute),
		Attendees: []Attendee{{Email: "ada@example.com", Name: "Ada"}},
	}
	tickNow := now
	cfg := Config{
		Retention: time.Hour,
		List: func(_ context.Context, _, _ time.Time) ([]Event, error) {
			if tickNow.Equal(now) {
				return []Event{first}, nil
			}
			return nil, nil
		},
		Resolve: resolverFromMap(map[string]Person{"ada@example.com": person}),
		Now:     func() time.Time { return tickNow },
	}
	tr, _ := newTestTrigger(t, cfg)
	if err := tr.Tick(context.Background()); err != nil {
		t.Fatalf("first Tick: %v", err)
	}
	if !tr.alreadyEmitted("evt-old") {
		t.Fatalf("first tick must record emission")
	}
	tickNow = now.Add(2 * time.Hour)
	if err := tr.Tick(context.Background()); err != nil {
		t.Fatalf("second Tick: %v", err)
	}
	if tr.alreadyEmitted("evt-old") {
		t.Fatalf("entry must be evicted after retention window")
	}
}

func TestTickDeduplicatesSamePersonAcrossAttendees(t *testing.T) {
	now := time.Date(2026, 5, 4, 9, 0, 0, 0, time.UTC)
	person := Person{Name: "Ada", Path: "/vault/brain/people/Ada.md"}
	event := Event{
		ID: "evt-dup-attendees", Summary: "Sync",
		Start: now.Add(2 * time.Minute),
		Attendees: []Attendee{
			{Email: "ada@example.com", Name: "Ada"},
			{Email: "ada.alt@example.com", Name: "Ada Lovelace"},
		},
	}
	cfg := Config{
		List: func(_ context.Context, _, _ time.Time) ([]Event, error) {
			return []Event{event}, nil
		},
		Resolve: resolverFromMap(map[string]Person{
			"ada@example.com":     person,
			"ada.alt@example.com": person,
		}),
		Now: func() time.Time { return now },
	}
	tr, channel := newTestTrigger(t, cfg)
	if err := tr.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(channel.notifications) != 1 || len(channel.notifications[0].Briefs) != 1 {
		t.Fatalf("expected one notification with one brief, got %#v", channel.notifications)
	}
}

func writePersonNote(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func TestVaultResolverMatchesByName(t *testing.T) {
	root := t.TempDir()
	peopleDir := filepath.Join(root, "brain", "people")
	if err := os.MkdirAll(peopleDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writePersonNote(t, peopleDir, "Ada Lovelace", "# Ada\n")
	resolver := NewVaultResolver(brain.Vault{Root: root, Brain: "brain"})
	got, ok, err := resolver.Resolve(context.Background(), Attendee{Name: "Ada Lovelace"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !ok || got.Name != "Ada Lovelace" {
		t.Fatalf("resolved = %#v, want Ada Lovelace", got)
	}
}

func TestVaultResolverFallsBackToEmail(t *testing.T) {
	root := t.TempDir()
	peopleDir := filepath.Join(root, "brain", "people")
	if err := os.MkdirAll(peopleDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writePersonNote(t, peopleDir, "Ada Lovelace", "---\nemail: ada@example.com\n---\n# Ada\n")
	resolver := NewVaultResolver(brain.Vault{Root: root, Brain: "brain"})
	got, ok, err := resolver.Resolve(context.Background(), Attendee{Email: "ada@example.com"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !ok || got.Name != "Ada Lovelace" || got.Email != "ada@example.com" {
		t.Fatalf("resolved = %#v, want Ada by email", got)
	}
}

func TestVaultResolverReturnsOkFalseWithoutMatch(t *testing.T) {
	root := t.TempDir()
	peopleDir := filepath.Join(root, "brain", "people")
	if err := os.MkdirAll(peopleDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writePersonNote(t, peopleDir, "Bob Builder", "---\nemail: bob@example.com\n---\n")
	resolver := NewVaultResolver(brain.Vault{Root: root, Brain: "brain"})
	_, ok, err := resolver.Resolve(context.Background(), Attendee{Name: "Charlie", Email: "charlie@example.com"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if ok {
		t.Fatalf("expected ok=false for unknown attendee")
	}
}
