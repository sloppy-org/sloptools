package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/sloppy-org/sloptools/internal/brain"
	"github.com/sloppy-org/sloptools/internal/calendar"
	"github.com/sloppy-org/sloptools/internal/calendarbrief"
	"github.com/sloppy-org/sloptools/internal/groupware"
	"github.com/sloppy-org/sloptools/internal/mcp"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/store"
)

// cmdBrief dispatches `sloptools brief <subcommand>`. `watch` runs the
// long-lived calendar-trigger ticker described in issue #92; `tick-once`
// scans the window once and exits, mirroring `meetings ingest-once` so the
// daemon contract stays scriptable.
func cmdBrief(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "brief <watch|tick-once> [flags]")
		return 2
	}
	switch args[0] {
	case "watch":
		return cmdBriefWatch(args[1:], false)
	case "tick-once":
		return cmdBriefWatch(args[1:], true)
	default:
		fmt.Fprintf(os.Stderr, "unknown brief subcommand: %s\n", args[0])
		return 2
	}
}

func cmdBriefWatch(args []string, oneShot bool) int {
	fs := flag.NewFlagSet("brief watch", flag.ContinueOnError)
	sphere := fs.String("sphere", "", "vault sphere (work|private)")
	accountID := fs.Int64("account-id", 0, "calendar account id; 0 falls back to all enabled Google calendar accounts in the sphere")
	configPath := fs.String("vault-config", "", "vault config path; defaults to ~/.config/sloptools/vaults.toml")
	dataDir := fs.String("data-dir", filepath.Join(os.Getenv("HOME"), ".local", "share", "sloppy"), "data dir holding sloppy.db")
	interval := fs.Duration("interval", time.Minute, "tick interval for the ticker")
	window := fs.Duration("window", calendarbrief.DefaultWindow, "calendar look-ahead window")
	notifyURL := fs.String("notify-url", "", "optional slopshell HTTP endpoint that receives notification JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*sphere) == "" {
		fmt.Fprintln(os.Stderr, "--sphere is required")
		return 2
	}
	cfg, err := brain.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	vault, ok := cfg.Vault(brain.Sphere(*sphere))
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown vault %q\n", *sphere)
		return 1
	}
	st, err := store.New(filepath.Join(*dataDir, "sloppy.db"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer st.Close()
	server := mcp.NewServerWithStoreAndBrainConfig(".", st, *configPath)
	resolver := calendarbrief.NewVaultResolver(vault)
	emitter, err := newBriefEmitter(*notifyURL, os.Stdout)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	source := &briefCalendarSource{store: st, registry: groupware.NewRegistry(st, ""), sphere: *sphere, accountID: *accountID}
	trigger, err := calendarbrief.New(calendarbrief.Config{
		List:    source.List,
		Resolve: resolver.Resolve,
		Build:   briefBuilder(server, *sphere, *configPath),
		Emit:    emitter,
		Window:  *window,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if oneShot {
		if err := trigger.Tick(ctx); err != nil && !isContextCancelled(err) {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
	logErr := func(e error) { fmt.Fprintf(os.Stderr, "brief tick error: %v\n", e) }
	if err := trigger.Run(ctx, *interval, logErr); err != nil && !isContextCancelled(err) {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

// briefCalendarSource lists calendar events for the configured sphere
// directly through the groupware registry so attendees survive the trip
// (the calendar_events MCP verb strips them for compactness).
type briefCalendarSource struct {
	store     *store.Store
	registry  *groupware.Registry
	sphere    string
	accountID int64
}

func (b *briefCalendarSource) List(ctx context.Context, start, end time.Time) ([]calendarbrief.Event, error) {
	accounts, err := b.resolveAccounts()
	if err != nil {
		return nil, err
	}
	rng := calendar.TimeRange{Start: start, End: end}
	var out []calendarbrief.Event
	for _, account := range accounts {
		provider, err := b.registry.CalendarFor(ctx, account.ID)
		if err != nil {
			return nil, fmt.Errorf("calendar provider for account %d: %w", account.ID, err)
		}
		cals, err := provider.ListCalendars(ctx)
		if err != nil {
			return nil, fmt.Errorf("list calendars for %q: %w", account.Label, err)
		}
		for _, cal := range cals {
			items, err := provider.ListEvents(ctx, cal.ID, rng)
			if err != nil {
				return nil, fmt.Errorf("list events for %q: %w", cal.Name, err)
			}
			for _, ev := range items {
				out = append(out, briefEventFromProvider(ev))
			}
		}
	}
	return out, nil
}

func (b *briefCalendarSource) resolveAccounts() ([]store.ExternalAccount, error) {
	if b.accountID > 0 {
		account, err := b.store.GetExternalAccount(b.accountID)
		if err != nil {
			return nil, err
		}
		if !account.Enabled {
			return nil, fmt.Errorf("account %d is disabled", b.accountID)
		}
		return []store.ExternalAccount{account}, nil
	}
	accounts, err := calendar.GoogleCalendarAccounts(b.store)
	if err != nil {
		return nil, err
	}
	if b.sphere == "" {
		return accounts, nil
	}
	filtered := make([]store.ExternalAccount, 0, len(accounts))
	for _, account := range accounts {
		if strings.EqualFold(account.Sphere, b.sphere) {
			filtered = append(filtered, account)
		}
	}
	return filtered, nil
}

func briefEventFromProvider(ev providerdata.Event) calendarbrief.Event {
	attendees := make([]calendarbrief.Attendee, 0, len(ev.Attendees))
	for _, att := range ev.Attendees {
		attendees = append(attendees, calendarbrief.Attendee{Email: att.Email, Name: att.Name})
	}
	return calendarbrief.Event{
		ID:          ev.ID,
		Summary:     ev.Summary,
		Description: ev.Description,
		Start:       ev.Start,
		End:         ev.End,
		AllDay:      ev.AllDay,
		Attendees:   attendees,
	}
}

// briefBuilder calls the in-process brain.people.brief MCP tool so the
// notification carries the same payload a slopshell client would receive
// when invoking the tool directly.
func briefBuilder(server *mcp.Server, sphere, configPath string) calendarbrief.BriefBuilder {
	return func(_ context.Context, person calendarbrief.Person) (map[string]interface{}, error) {
		args := map[string]interface{}{
			"sphere":      sphere,
			"name":        person.Name,
			"config_path": configPath,
		}
		if person.Email != "" {
			args["email"] = person.Email
		}
		brief, err := server.CallTool("brain.people.brief", args)
		if err != nil {
			return nil, fmt.Errorf("brain.people.brief %s: %w", person.Name, err)
		}
		return brief, nil
	}
}

// newBriefEmitter chooses between the JSON-line stdout sink (default) and
// an HTTP POST sink when --notify-url is supplied. Both write JSON in the
// same shape so slopshell sees one wire format regardless of transport.
func newBriefEmitter(notifyURL string, sink *os.File) (calendarbrief.Emitter, error) {
	if strings.TrimSpace(notifyURL) == "" {
		return jsonLineEmitter(sink), nil
	}
	client := &http.Client{Timeout: 10 * time.Second}
	return httpPostEmitter(client, notifyURL), nil
}

func jsonLineEmitter(sink *os.File) calendarbrief.Emitter {
	var mu sync.Mutex
	return func(_ context.Context, n calendarbrief.Notification) error {
		body, err := json.Marshal(n)
		if err != nil {
			return err
		}
		mu.Lock()
		defer mu.Unlock()
		if _, err := sink.Write(append(body, '\n')); err != nil {
			return err
		}
		return nil
	}
}

func httpPostEmitter(client *http.Client, url string) calendarbrief.Emitter {
	return func(ctx context.Context, n calendarbrief.Notification) error {
		body, err := json.Marshal(n)
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			return fmt.Errorf("notify endpoint %s: %s", url, resp.Status)
		}
		return nil
	}
}
