package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sloppy-org/sloptools/internal/providerdata"
	"google.golang.org/api/option"
	gtasks "google.golang.org/api/tasks/v1"
)

// fakeTasksServer captures traffic from the adapter and plays back canned
// task-list state so each capability can be exercised end-to-end without
// touching Google infrastructure.
type fakeTasksServer struct {
	t         *testing.T
	server    *httptest.Server
	requests  []recordedRequest
	taskLists map[string]*gtasks.TaskList
	tasks     map[string]map[string]*gtasks.Task
	listOrder []string
	insertSeq int
}

type recordedRequest struct {
	Method string
	Path   string
	Body   string
}

func newFakeTasksServer(t *testing.T) *fakeTasksServer {
	t.Helper()
	f := &fakeTasksServer{
		t:         t,
		taskLists: map[string]*gtasks.TaskList{},
		tasks:     map[string]map[string]*gtasks.Task{},
	}
	f.server = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeTasksServer) seedList(id, title string) {
	f.taskLists[id] = &gtasks.TaskList{Id: id, Title: title}
	f.tasks[id] = map[string]*gtasks.Task{}
	f.listOrder = append(f.listOrder, id)
}

func (f *fakeTasksServer) seedTask(listID string, task *gtasks.Task) {
	if _, ok := f.tasks[listID]; !ok {
		f.tasks[listID] = map[string]*gtasks.Task{}
	}
	f.tasks[listID][task.Id] = task
}

func (f *fakeTasksServer) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	_ = r.Body.Close()
	f.requests = append(f.requests, recordedRequest{Method: r.Method, Path: r.URL.Path, Body: string(body)})
	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/tasks/v1/users/@me/lists":
		items := make([]*gtasks.TaskList, 0, len(f.listOrder))
		for _, id := range f.listOrder {
			items = append(items, f.taskLists[id])
		}
		_ = json.NewEncoder(w).Encode(&gtasks.TaskLists{Items: items})
	case r.Method == http.MethodPost && r.URL.Path == "/tasks/v1/users/@me/lists":
		var incoming gtasks.TaskList
		if err := json.Unmarshal(body, &incoming); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		incoming.Id = fmt.Sprintf("list-%d", len(f.taskLists)+1)
		f.seedList(incoming.Id, incoming.Title)
		_ = json.NewEncoder(w).Encode(&incoming)
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/tasks/v1/users/@me/lists/"):
		id := strings.TrimPrefix(r.URL.Path, "/tasks/v1/users/@me/lists/")
		delete(f.taskLists, id)
		delete(f.tasks, id)
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/tasks/v1/lists/"):
		listID, taskID, ok := splitListAndTask(r.URL.Path)
		if !ok {
			listID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/tasks/v1/lists/"), "/tasks")
			items := make([]*gtasks.Task, 0)
			for _, task := range f.tasks[listID] {
				items = append(items, task)
			}
			_ = json.NewEncoder(w).Encode(&gtasks.Tasks{Items: items})
			return
		}
		task, ok := f.tasks[listID][taskID]
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(task)
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/tasks"):
		listID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/tasks/v1/lists/"), "/tasks")
		var incoming gtasks.Task
		if err := json.Unmarshal(body, &incoming); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.insertSeq++
		incoming.Id = fmt.Sprintf("task-%d", f.insertSeq)
		if _, ok := f.tasks[listID]; !ok {
			f.tasks[listID] = map[string]*gtasks.Task{}
		}
		f.tasks[listID][incoming.Id] = &incoming
		_ = json.NewEncoder(w).Encode(&incoming)
	case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/tasks/"):
		listID, taskID, _ := splitListAndTask(r.URL.Path)
		var incoming gtasks.Task
		if err := json.Unmarshal(body, &incoming); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		incoming.Id = taskID
		f.tasks[listID][taskID] = &incoming
		_ = json.NewEncoder(w).Encode(&incoming)
	case r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/tasks/"):
		listID, taskID, _ := splitListAndTask(r.URL.Path)
		delete(f.tasks[listID], taskID)
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, fmt.Sprintf("unexpected %s %s", r.Method, r.URL.Path), http.StatusNotImplemented)
	}
}

func splitListAndTask(path string) (string, string, bool) {
	const prefix = "/tasks/v1/lists/"
	if !strings.HasPrefix(path, prefix) {
		return "", "", false
	}
	parts := strings.Split(strings.TrimPrefix(path, prefix), "/")
	// Expected shape: <listID>/tasks/<taskID>
	if len(parts) < 3 || parts[1] != "tasks" {
		return "", "", false
	}
	return parts[0], parts[2], true
}

func newProviderForTest(t *testing.T, fake *fakeTasksServer) *GoogleProvider {
	t.Helper()
	provider := NewGoogleProvider(nil)
	provider.svcFn = func(ctx context.Context) (*gtasks.Service, error) {
		return gtasks.NewService(ctx,
			option.WithHTTPClient(fake.server.Client()),
			option.WithEndpoint(fake.server.URL),
		)
	}
	provider.nowFn = func() time.Time {
		return time.Date(2026, 4, 23, 10, 0, 0, 0, time.UTC)
	}
	return provider
}

func TestGoogleProviderIdentity(t *testing.T) {
	provider := NewGoogleProvider(nil)
	if got := provider.ProviderName(); got != "google_tasks" {
		t.Fatalf("ProviderName() = %q", got)
	}
	if err := provider.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}
}

func TestGoogleProviderListTaskLists(t *testing.T) {
	fake := newFakeTasksServer(t)
	fake.seedList("list-a", "Inbox")
	fake.seedList("list-b", "Chores")

	lists, err := newProviderForTest(t, fake).ListTaskLists(context.Background())
	if err != nil {
		t.Fatalf("ListTaskLists() error: %v", err)
	}
	if len(lists) != 2 {
		t.Fatalf("ListTaskLists() len = %d, want 2", len(lists))
	}
	if !lists[0].Primary {
		t.Fatalf("first list not marked primary: %+v", lists[0])
	}
	if lists[1].Primary {
		t.Fatalf("second list must not be primary")
	}
	if lists[0].Name != "Inbox" || lists[1].Name != "Chores" {
		t.Fatalf("ListTaskLists() names = %+v", lists)
	}
}

func TestGoogleProviderTaskCRUD(t *testing.T) {
	fake := newFakeTasksServer(t)
	fake.seedList("list-a", "Inbox")
	provider := newProviderForTest(t, fake)
	ctx := context.Background()

	due := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	created, err := provider.CreateTask(ctx, "list-a", providerdata.TaskItem{Title: "Write docs", Due: &due})
	if err != nil {
		t.Fatalf("CreateTask() error: %v", err)
	}
	if created.ID == "" {
		t.Fatalf("CreateTask() returned empty id")
	}
	if created.Title != "Write docs" {
		t.Fatalf("CreateTask() title = %q", created.Title)
	}
	if created.Due == nil || !created.Due.Equal(due) {
		t.Fatalf("CreateTask() due = %v", created.Due)
	}

	fetched, err := provider.GetTask(ctx, "list-a", created.ID)
	if err != nil {
		t.Fatalf("GetTask() error: %v", err)
	}
	if fetched.Title != created.Title {
		t.Fatalf("GetTask() title = %q", fetched.Title)
	}

	listed, err := provider.ListTasks(ctx, "list-a")
	if err != nil {
		t.Fatalf("ListTasks() error: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("ListTasks() = %+v", listed)
	}

	fetched.Title = "Write docs (updated)"
	fetched.Notes = "fresh notes"
	updated, err := provider.UpdateTask(ctx, "list-a", fetched)
	if err != nil {
		t.Fatalf("UpdateTask() error: %v", err)
	}
	if updated.Title != "Write docs (updated)" || updated.Notes != "fresh notes" {
		t.Fatalf("UpdateTask() = %+v", updated)
	}

	if err := provider.DeleteTask(ctx, "list-a", created.ID); err != nil {
		t.Fatalf("DeleteTask() error: %v", err)
	}
	if _, ok := fake.tasks["list-a"][created.ID]; ok {
		t.Fatalf("DeleteTask did not remove task from server state")
	}
}

func TestGoogleProviderCompletionToggle(t *testing.T) {
	fake := newFakeTasksServer(t)
	fake.seedList("list-a", "Inbox")
	fake.seedTask("list-a", &gtasks.Task{Id: "task-1", Title: "Do thing", Status: "needsAction"})
	provider := newProviderForTest(t, fake)
	ctx := context.Background()

	if err := provider.CompleteTask(ctx, "list-a", "task-1"); err != nil {
		t.Fatalf("CompleteTask() error: %v", err)
	}
	got := fake.tasks["list-a"]["task-1"]
	if got.Status != "completed" {
		t.Fatalf("CompleteTask status = %q, want completed", got.Status)
	}
	if got.Completed == nil || *got.Completed == "" {
		t.Fatalf("CompleteTask did not set completed timestamp")
	}
	ts, err := time.Parse(time.RFC3339, *got.Completed)
	if err != nil {
		t.Fatalf("completed timestamp not RFC3339: %v", err)
	}
	if !ts.Equal(time.Date(2026, 4, 23, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("CompleteTask timestamp = %s, want fixed fake clock", ts)
	}

	if err := provider.UncompleteTask(ctx, "list-a", "task-1"); err != nil {
		t.Fatalf("UncompleteTask() error: %v", err)
	}
	got = fake.tasks["list-a"]["task-1"]
	if got.Status != "needsAction" {
		t.Fatalf("UncompleteTask status = %q, want needsAction", got.Status)
	}
	if got.Completed != nil && *got.Completed != "" {
		t.Fatalf("UncompleteTask did not clear completed timestamp: %v", *got.Completed)
	}
	// The PUT payload for uncomplete must send a null for completed so the
	// server actually clears the existing value. Inspect the most recent PUT
	// body to confirm the serializer honored the NullFields hint.
	lastPut := lastRequestOfMethod(fake.requests, http.MethodPut)
	if lastPut == nil {
		t.Fatalf("UncompleteTask produced no PUT request")
	}
	if !strings.Contains(lastPut.Body, `"completed":null`) {
		t.Fatalf("UncompleteTask PUT body missing completed:null, got: %s", lastPut.Body)
	}
}

func TestGoogleProviderListManager(t *testing.T) {
	fake := newFakeTasksServer(t)
	provider := newProviderForTest(t, fake)
	ctx := context.Background()

	list, err := provider.CreateTaskList(ctx, "New List")
	if err != nil {
		t.Fatalf("CreateTaskList() error: %v", err)
	}
	if list.ID == "" || list.Name != "New List" {
		t.Fatalf("CreateTaskList() = %+v", list)
	}

	if err := provider.DeleteTaskList(ctx, list.ID); err != nil {
		t.Fatalf("DeleteTaskList() error: %v", err)
	}
	if _, ok := fake.taskLists[list.ID]; ok {
		t.Fatalf("DeleteTaskList did not remove list from server state")
	}
}

func TestGoogleProviderValidationErrors(t *testing.T) {
	provider := NewGoogleProvider(nil)
	ctx := context.Background()
	cases := []struct {
		name string
		call func() error
	}{
		{"ListTasksEmpty", func() error { _, err := provider.ListTasks(ctx, ""); return err }},
		{"GetTaskEmptyIDs", func() error { _, err := provider.GetTask(ctx, "", ""); return err }},
		{"CreateTaskEmptyList", func() error {
			_, err := provider.CreateTask(ctx, "", providerdata.TaskItem{Title: "t"})
			return err
		}},
		{"CreateTaskEmptyTitle", func() error {
			_, err := provider.CreateTask(ctx, "list", providerdata.TaskItem{Title: "   "})
			return err
		}},
		{"UpdateTaskEmptyIDs", func() error {
			_, err := provider.UpdateTask(ctx, "", providerdata.TaskItem{Title: "t"})
			return err
		}},
		{"DeleteTaskEmptyIDs", func() error { return provider.DeleteTask(ctx, "", "") }},
		{"CompleteTaskEmptyIDs", func() error { return provider.CompleteTask(ctx, "", "") }},
		{"UncompleteTaskEmptyIDs", func() error { return provider.UncompleteTask(ctx, "", "") }},
		{"CreateTaskListEmptyName", func() error { _, err := provider.CreateTaskList(ctx, "  "); return err }},
		{"DeleteTaskListEmptyID", func() error { return provider.DeleteTaskList(ctx, "") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); err == nil {
				t.Fatalf("%s returned nil error", tc.name)
			}
		})
	}
}

func TestParseTaskTimestampFormats(t *testing.T) {
	cases := []struct {
		name   string
		input  string
		wantOK bool
	}{
		{"rfc3339", "2026-05-01T10:00:00Z", true},
		{"date-only", "2026-05-01", true},
		{"empty", "   ", false},
		{"garbage", "not-a-date", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseTaskTimestamp(tc.input)
			if ok != tc.wantOK {
				t.Fatalf("parseTaskTimestamp(%q) ok = %v, want %v", tc.input, ok, tc.wantOK)
			}
			if tc.wantOK && got.IsZero() {
				t.Fatalf("parseTaskTimestamp(%q) returned zero time", tc.input)
			}
		})
	}
}

func lastRequestOfMethod(requests []recordedRequest, method string) *recordedRequest {
	for i := len(requests) - 1; i >= 0; i-- {
		if requests[i].Method == method {
			return &requests[i]
		}
	}
	return nil
}
