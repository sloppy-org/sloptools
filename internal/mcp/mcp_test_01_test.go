package mcp

import (
	"context"
	"fmt"
	tabcalendar "github.com/sloppy-org/sloptools/internal/calendar"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/store"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type stubCalendarProvider struct {
	name      string
	calendars []providerdata.Calendar
	events    map[string][]providerdata.Event
	created   providerdata.Event
	lastEvent providerdata.Event
	lastCalID string
}

func (s *stubCalendarProvider) ProviderName() string {
	if s.name == "" {
		return "google_calendar"
	}
	return s.name
}

func (s *stubCalendarProvider) Close() error {
	return nil
}

func (s *stubCalendarProvider) ListCalendars(context.Context) ([]providerdata.Calendar, error) {
	return append([]providerdata.Calendar(nil), s.calendars...), nil
}

func (s *stubCalendarProvider) ListEvents(_ context.Context, calendarID string, _ tabcalendar.TimeRange) ([]providerdata.Event, error) {
	return append([]providerdata.Event(nil), s.events[calendarID]...), nil
}

func (s *stubCalendarProvider) GetEvent(_ context.Context, calendarID, eventID string) (providerdata.Event, error) {
	for _, ev := range s.events[calendarID] {
		if ev.ID == eventID {
			return ev, nil
		}
	}
	return providerdata.Event{}, fmt.Errorf("event %s not found", eventID)
}

func (s *stubCalendarProvider) CreateEvent(_ context.Context, calendarID string, ev providerdata.Event) (providerdata.Event, error) {
	s.lastCalID = calendarID
	s.lastEvent = ev
	created := s.created
	if created.ID == "" {
		created = providerdata.Event{ID: "evt-created", CalendarID: calendarID, Summary: ev.Summary, Description: ev.Description, Location: ev.Location, Start: ev.Start, End: ev.End, AllDay: ev.AllDay, Attendees: append([]providerdata.Attendee(nil), ev.Attendees...), Status: "confirmed"}
	}
	return created, nil
}

func (s *stubCalendarProvider) UpdateEvent(_ context.Context, calendarID string, ev providerdata.Event) (providerdata.Event, error) {
	ev.CalendarID = calendarID
	return ev, nil
}

func (s *stubCalendarProvider) DeleteEvent(_ context.Context, _, _ string) error {
	return nil
}

var (
	_ tabcalendar.Provider     = (*stubCalendarProvider)(nil)
	_ tabcalendar.EventMutator = (*stubCalendarProvider)(nil)
)

func TestCalendarListUsesGmailFallback(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "sloppy.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
	})
	if _, err := st.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderGmail, "Gmail", map[string]any{}); err != nil {
		t.Fatalf("CreateExternalAccount(gmail): %v", err)
	}
	stub := &stubCalendarProvider{calendars: []providerdata.Calendar{{ID: "primary", Name: "Primary", Primary: true}}}
	s := NewServerWithStore(t.TempDir(), st)
	s.newCalendarProvider = func(context.Context, store.ExternalAccount) (tabcalendar.Provider, error) {
		return stub, nil
	}
	got, err := s.callTool("calendar_list", map[string]interface{}{})
	if err != nil {
		t.Fatalf("calendar_list failed: %v", err)
	}
	calendars, _ := got["calendars"].([]map[string]interface{})
	if len(calendars) != 1 {
		t.Fatalf("calendar count = %d, want 1", len(calendars))
	}
	if strFromAny(calendars[0]["sphere"]) != store.SpherePrivate {
		t.Fatalf("sphere = %q, want %q", strFromAny(calendars[0]["sphere"]), store.SpherePrivate)
	}
}

func TestCalendarEventsReturnsStructuredEvents(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "sloppy.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
	})
	if _, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderGoogleCalendar, "Work", map[string]any{}); err != nil {
		t.Fatalf("CreateExternalAccount(calendar): %v", err)
	}
	start := time.Date(2026, time.March, 16, 9, 0, 0, 0, time.UTC)
	stub := &stubCalendarProvider{calendars: []providerdata.Calendar{{ID: "work", Name: "Work"}}, events: map[string][]providerdata.Event{"work": {{ID: "evt-1", CalendarID: "work", Summary: "Standup", Start: start, End: start.Add(time.Hour), Organizer: "alice@example.com"}}}}
	s := NewServerWithStore(t.TempDir(), st)
	s.newCalendarProvider = func(context.Context, store.ExternalAccount) (tabcalendar.Provider, error) {
		return stub, nil
	}
	got, err := s.callTool("calendar_events", map[string]interface{}{"calendar_id": "work", "days": 7, "limit": 10})
	if err != nil {
		t.Fatalf("calendar_events failed: %v", err)
	}
	events, _ := got["events"].([]map[string]interface{})
	if len(events) != 1 {
		t.Fatalf("event count = %d, want 1", len(events))
	}
	if strFromAny(events[0]["summary"]) != "Standup" {
		t.Fatalf("summary = %q, want Standup", strFromAny(events[0]["summary"]))
	}
	if strFromAny(events[0]["provider"]) != "google_calendar" {
		t.Fatalf("provider = %q, want google_calendar", strFromAny(events[0]["provider"]))
	}
	if strFromAny(events[0]["calendar_name"]) != "Work" {
		t.Fatalf("calendar_name = %q, want Work", strFromAny(events[0]["calendar_name"]))
	}
}

func TestCalendarEventsAcceptsPastRange(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "sloppy.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
	})
	if _, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderGoogleCalendar, "Work", map[string]any{}); err != nil {
		t.Fatalf("CreateExternalAccount(calendar): %v", err)
	}
	stub := &stubCalendarProvider{
		calendars: []providerdata.Calendar{{ID: "work", Name: "Work"}},
		events: map[string][]providerdata.Event{"work": {{
			ID:         "evt-1",
			CalendarID: "work",
			Summary:    "Masterprüfung",
			Start:      time.Date(2021, time.June, 25, 9, 0, 0, 0, time.UTC),
			End:        time.Date(2021, time.June, 25, 10, 0, 0, 0, time.UTC),
		}}},
	}
	s := NewServerWithStore(t.TempDir(), st)
	s.newCalendarProvider = func(context.Context, store.ExternalAccount) (tabcalendar.Provider, error) {
		return stub, nil
	}
	got, err := s.callTool("calendar_events", map[string]interface{}{"calendar_id": "work", "start": "2021-01-01", "end": "2022-01-01", "query": "Master", "limit": 10})
	if err != nil {
		t.Fatalf("calendar_events failed: %v", err)
	}
	if strFromAny(got["start"]) == "" || strFromAny(got["end"]) == "" {
		t.Fatalf("missing returned range: %#v", got)
	}
	events, _ := got["events"].([]map[string]interface{})
	if len(events) != 1 {
		t.Fatalf("event count = %d, want 1", len(events))
	}
}

func TestCalendarEventCreateUsesPreferredSphereCalendar(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "sloppy.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
	})
	if _, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderGoogleCalendar, "Work Calendar", map[string]any{}); err != nil {
		t.Fatalf("CreateExternalAccount(work): %v", err)
	}
	if _, err := st.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderGoogleCalendar, "Family", map[string]any{}); err != nil {
		t.Fatalf("CreateExternalAccount(private): %v", err)
	}
	stub := &stubCalendarProvider{calendars: []providerdata.Calendar{{ID: "work", Name: "Work Calendar"}, {ID: "family", Name: "Family"}}}
	s := NewServerWithStore(t.TempDir(), st)
	s.newCalendarProvider = func(context.Context, store.ExternalAccount) (tabcalendar.Provider, error) {
		return stub, nil
	}
	got, err := s.callTool("calendar_event_create", map[string]interface{}{"sphere": store.SpherePrivate, "summary": "Masterprüfung David Obermeier", "start": "2026-04-20T16:00:00+02:00", "duration_minutes": 60})
	if err != nil {
		t.Fatalf("calendar_event_create failed: %v", err)
	}
	event, _ := got["event"].(map[string]interface{})
	if strFromAny(event["calendar_id"]) != "family" {
		t.Fatalf("calendar_id = %q, want family", strFromAny(event["calendar_id"]))
	}
	if strFromAny(event["summary"]) != "Masterprüfung David Obermeier" {
		t.Fatalf("summary = %q", strFromAny(event["summary"]))
	}
	if stub.lastCalID != "family" {
		t.Fatalf("CreateEvent calendar = %q, want family", stub.lastCalID)
	}
}

func TestCalendarListRoutesByAccountID(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "sloppy.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
	})
	workAcct, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderGoogleCalendar, "Work", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount(work): %v", err)
	}
	privateAcct, err := st.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderGoogleCalendar, "Private", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount(private): %v", err)
	}
	callsByID := make(map[int64]int)
	s := NewServerWithStore(t.TempDir(), st)
	s.newCalendarProvider = func(_ context.Context, account store.ExternalAccount) (tabcalendar.Provider, error) {
		callsByID[account.ID]++
		return &stubCalendarProvider{calendars: []providerdata.Calendar{{ID: fmt.Sprintf("cal-%d", account.ID), Name: account.Label}}}, nil
	}
	if _, err := s.callTool("calendar_list", map[string]interface{}{"account_id": privateAcct.ID}); err != nil {
		t.Fatalf("calendar_list(private) failed: %v", err)
	}
	if callsByID[privateAcct.ID] != 1 || callsByID[workAcct.ID] != 0 {
		t.Fatalf("account_id routing missed: private=%d work=%d", callsByID[privateAcct.ID], callsByID[workAcct.ID])
	}
	callsByID = make(map[int64]int)
	if _, err := s.callTool("calendar_list", map[string]interface{}{}); err != nil {
		t.Fatalf("calendar_list(default) failed: %v", err)
	}
	if callsByID[privateAcct.ID] == 0 || callsByID[workAcct.ID] == 0 {
		t.Fatalf("default calendar_list should visit both accounts, got: private=%d work=%d", callsByID[privateAcct.ID], callsByID[workAcct.ID])
	}
}

func newDomainServerForTest(t *testing.T) (*Server, *store.Store, string) {
	t.Helper()
	projectDir := t.TempDir()
	st, err := store.New(filepath.Join(t.TempDir(), "sloppy.db"))
	if err != nil {
		t.Fatalf("store.New() error: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
	})
	return NewServerWithStore(projectDir, st), st, projectDir
}

func TestWorkspaceTools(t *testing.T) {
	s, st, projectDir := newDomainServerForTest(t)
	alpha, err := st.CreateWorkspace("Alpha", projectDir, store.SphereWork)
	if err != nil {
		t.Fatalf("CreateWorkspace(alpha) error: %v", err)
	}
	beta, err := st.CreateWorkspace("Beta", filepath.Join(projectDir, "beta"), store.SpherePrivate)
	if err != nil {
		t.Fatalf("CreateWorkspace(beta) error: %v", err)
	}
	if err := st.SetActiveWorkspace(beta.ID); err != nil {
		t.Fatalf("SetActiveWorkspace(beta) error: %v", err)
	}
	if _, err := st.CreateItem("Inbox item", store.ItemOptions{State: store.ItemStateInbox, WorkspaceID: &alpha.ID}); err != nil {
		t.Fatalf("CreateItem(inbox) error: %v", err)
	}
	if _, err := st.CreateItem("Waiting item", store.ItemOptions{State: store.ItemStateWaiting, WorkspaceID: &alpha.ID}); err != nil {
		t.Fatalf("CreateItem(waiting) error: %v", err)
	}
	if _, err := st.CreateItem("Done item", store.ItemOptions{State: store.ItemStateDone, WorkspaceID: &alpha.ID}); err != nil {
		t.Fatalf("CreateItem(done) error: %v", err)
	}
	listed, err := s.callTool("workspace_list", map[string]interface{}{})
	if err != nil {
		t.Fatalf("workspace_list failed: %v", err)
	}
	workspaces, _ := listed["workspaces"].([]store.Workspace)
	if len(workspaces) != 2 {
		t.Fatalf("workspace_list len = %d, want 2", len(workspaces))
	}
	if got, _ := listed["active_workspace_id"].(int64); got != beta.ID {
		t.Fatalf("active_workspace_id = %d, want %d", got, beta.ID)
	}
	activated, err := s.callTool("workspace_activate", map[string]interface{}{"workspace_id": alpha.ID})
	if err != nil {
		t.Fatalf("workspace_activate failed: %v", err)
	}
	workspace, _ := activated["workspace"].(store.Workspace)
	if workspace.ID != alpha.ID || !workspace.IsActive {
		t.Fatalf("workspace_activate returned %+v", workspace)
	}
	got, err := s.callTool("workspace_get", map[string]interface{}{"workspace_id": alpha.ID})
	if err != nil {
		t.Fatalf("workspace_get failed: %v", err)
	}
	if openCount, _ := got["open_count"].(int); openCount != 2 {
		t.Fatalf("open_count = %d, want 2", openCount)
	}
	counts, _ := got["item_counts"].(map[string]int)
	if counts[store.ItemStateDone] != 1 {
		t.Fatalf("done count = %d, want 1", counts[store.ItemStateDone])
	}
}

func TestItemToolsRoundTrip(t *testing.T) {
	s, st, projectDir := newDomainServerForTest(t)
	workspace, err := st.CreateWorkspace("Alpha", projectDir, store.SphereWork)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	actor, err := st.CreateActor("Ada", store.ActorKindHuman)
	if err != nil {
		t.Fatalf("CreateActor() error: %v", err)
	}
	artifactPath := filepath.Join(projectDir, "notes.md")
	if err := os.WriteFile(artifactPath, []byte("artifact body\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(notes.md) error: %v", err)
	}
	refPath := "notes.md"
	title := "Notes"
	artifact, err := st.CreateArtifact(store.ArtifactKindMarkdown, &refPath, nil, &title, nil)
	if err != nil {
		t.Fatalf("CreateArtifact() error: %v", err)
	}
	created, err := s.callTool("item_create", map[string]interface{}{"title": "Read paper", "workspace_id": workspace.ID, "artifact_id": artifact.ID, "sphere": store.SphereWork})
	if err != nil {
		t.Fatalf("item_create failed: %v", err)
	}
	item, _ := created["item"].(store.Item)
	if item.State != store.ItemStateInbox {
		t.Fatalf("created state = %q, want %q", item.State, store.ItemStateInbox)
	}
	assigned, err := s.callTool("item_assign", map[string]interface{}{"item_id": item.ID, "actor_id": actor.ID})
	if err != nil {
		t.Fatalf("item_assign failed: %v", err)
	}
	item, _ = assigned["item"].(store.Item)
	if item.ActorID == nil || *item.ActorID != actor.ID || item.State != store.ItemStateWaiting {
		t.Fatalf("assigned item = %+v", item)
	}
	followUp := "2026-03-09T10:11:12Z"
	updated, err := s.callTool("item_update", map[string]interface{}{"item_id": item.ID, "title": "Read paper carefully", "follow_up_at": followUp})
	if err != nil {
		t.Fatalf("item_update failed: %v", err)
	}
	item, _ = updated["item"].(store.Item)
	if item.Title != "Read paper carefully" || item.FollowUpAt == nil || *item.FollowUpAt != followUp {
		t.Fatalf("updated item = %+v", item)
	}
	listed, err := s.callTool("item_list", map[string]interface{}{"state": store.ItemStateWaiting, "workspace_id": workspace.ID})
	if err != nil {
		t.Fatalf("item_list failed: %v", err)
	}
	items, _ := listed["items"].([]store.Item)
	if len(items) != 1 || items[0].ID != item.ID {
		t.Fatalf("item_list items = %+v", items)
	}
	got, err := s.callTool("item_get", map[string]interface{}{"item_id": item.ID})
	if err != nil {
		t.Fatalf("item_get failed: %v", err)
	}
	if gotItem, _ := got["item"].(store.Item); gotItem.ID != item.ID {
		t.Fatalf("item_get item = %+v", gotItem)
	}
	if gotActor, _ := got["actor"].(store.Actor); gotActor.ID != actor.ID {
		t.Fatalf("item_get actor = %+v", gotActor)
	}
	if gotArtifact, _ := got["artifact"].(store.Artifact); gotArtifact.ID != artifact.ID {
		t.Fatalf("item_get artifact = %+v", gotArtifact)
	}
	artifacts, _ := got["artifacts"].([]store.ItemArtifact)
	if len(artifacts) != 1 || artifacts[0].Artifact.ID != artifact.ID || artifacts[0].Role != "source" {
		t.Fatalf("item_get artifacts = %+v", artifacts)
	}
	triaged, err := s.callTool("item_triage", map[string]interface{}{"item_id": item.ID, "action": "later", "visible_after": "2026-03-10T09:00:00Z"})
	if err != nil {
		t.Fatalf("item_triage failed: %v", err)
	}
	item, _ = triaged["item"].(store.Item)
	if item.VisibleAfter == nil || *item.VisibleAfter != "2026-03-10T09:00:00Z" {
		t.Fatalf("triaged item = %+v", item)
	}
}

func TestArtifactAndActorTools(t *testing.T) {
	s, st, projectDir := newDomainServerForTest(t)
	workspace, err := st.CreateWorkspace("Alpha", projectDir, store.SphereWork)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	createdActor, err := s.callTool("actor_create", map[string]interface{}{"name": "Codex", "kind": store.ActorKindAgent})
	if err != nil {
		t.Fatalf("actor_create failed: %v", err)
	}
	actor, _ := createdActor["actor"].(store.Actor)
	if actor.Name != "Codex" || actor.Kind != store.ActorKindAgent {
		t.Fatalf("actor_create returned %+v", actor)
	}
	listedActors, err := s.callTool("actor_list", map[string]interface{}{})
	if err != nil {
		t.Fatalf("actor_list failed: %v", err)
	}
	actors, _ := listedActors["actors"].([]store.Actor)
	if len(actors) != 1 || actors[0].ID != actor.ID {
		t.Fatalf("actor_list returned %+v", actors)
	}
	artifactPath := filepath.Join(projectDir, "paper.md")
	if err := os.WriteFile(artifactPath, []byte("# Paper\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(paper.md) error: %v", err)
	}
	refPath := "paper.md"
	title := "Paper"
	artifact, err := st.CreateArtifact(store.ArtifactKindMarkdown, &refPath, nil, &title, nil)
	if err != nil {
		t.Fatalf("CreateArtifact() error: %v", err)
	}
	if err := st.LinkArtifactToWorkspace(workspace.ID, artifact.ID); err != nil {
		t.Fatalf("LinkArtifactToWorkspace() error: %v", err)
	}
	item, err := st.CreateItem("Review paper", store.ItemOptions{State: store.ItemStateInbox, WorkspaceID: &workspace.ID, ArtifactID: &artifact.ID})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}
	listedArtifacts, err := s.callTool("artifact_list", map[string]interface{}{"workspace_id": workspace.ID, "kind": string(store.ArtifactKindMarkdown)})
	if err != nil {
		t.Fatalf("artifact_list failed: %v", err)
	}
	artifacts, _ := listedArtifacts["artifacts"].([]store.Artifact)
	if len(artifacts) != 1 || artifacts[0].ID != artifact.ID {
		t.Fatalf("artifact_list returned %+v", artifacts)
	}
	gotArtifact, err := s.callTool("artifact_get", map[string]interface{}{"artifact_id": artifact.ID})
	if err != nil {
		t.Fatalf("artifact_get failed: %v", err)
	}
	if content, _ := gotArtifact["content_text"].(string); content != "# Paper\n" {
		t.Fatalf("artifact_get content_text = %q", content)
	}
	gotItems, _ := gotArtifact["items"].([]store.Item)
	if len(gotItems) != 1 || gotItems[0].ID != item.ID {
		t.Fatalf("artifact_get items = %+v", gotItems)
	}
}

func TestWorkspaceWatchTools(t *testing.T) {
	s, st, projectDir := newDomainServerForTest(t)
	workspace, err := st.CreateWorkspace("Alpha", projectDir, store.SphereWork)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	started, err := s.callTool("workspace_watch_start", map[string]interface{}{"workspace_id": workspace.ID, "poll_interval_seconds": 15, "config_json": `{"worker":"codex"}`})
	if err != nil {
		t.Fatalf("workspace_watch_start failed: %v", err)
	}
	watch, _ := started["watch"].(store.WorkspaceWatch)
	if !watch.Enabled || watch.PollIntervalSeconds != 15 {
		t.Fatalf("workspace_watch_start returned %+v", watch)
	}
	status, err := s.callTool("workspace_watch_status", map[string]interface{}{"workspace_id": workspace.ID})
	if err != nil {
		t.Fatalf("workspace_watch_status failed: %v", err)
	}
	if got, _ := status["watch"].(store.WorkspaceWatch); got.WorkspaceID != workspace.ID {
		t.Fatalf("workspace_watch_status returned %+v", got)
	}
	stopped, err := s.callTool("workspace_watch_stop", map[string]interface{}{"workspace_id": workspace.ID})
	if err != nil {
		t.Fatalf("workspace_watch_stop failed: %v", err)
	}
	if got, _ := stopped["watch"].(store.WorkspaceWatch); got.Enabled {
		t.Fatalf("workspace_watch_stop returned %+v", got)
	}
}
