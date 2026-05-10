package mcp

import (
	"context"
	"fmt"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/store"
	"github.com/sloppy-org/sloptools/internal/tasks"
	"testing"
	"time"
)

type fakeTasksProvider struct {
	name            string
	taskLists       []providerdata.TaskList
	tasksByList     map[string][]providerdata.TaskItem
	allTasks        []providerdata.TaskItem
	getTaskByID     map[string]providerdata.TaskItem
	hasMutator      bool
	hasCompleter    bool
	hasListManager  bool
	closeCalls      int
	listCalls       int
	listAllCalls    int
	createCalls     int
	updateCalls     int
	deleteCalls     int
	completeCalls   int
	uncompleteCalls int
}

func (f *fakeTasksProvider) ListTaskLists(_ context.Context) ([]providerdata.TaskList, error) {
	f.listCalls++
	out := make([]providerdata.TaskList, len(f.taskLists))
	copy(out, f.taskLists)
	return out, nil
}

func (f *fakeTasksProvider) ListTasks(_ context.Context, listID string) ([]providerdata.TaskItem, error) {
	f.listCalls++
	items, ok := f.tasksByList[listID]
	if !ok {
		return nil, nil
	}
	out := make([]providerdata.TaskItem, len(items))
	copy(out, items)
	return out, nil
}

func (f *fakeTasksProvider) GetTask(_ context.Context, listID, id string) (providerdata.TaskItem, error) {
	f.listCalls++
	if item, ok := f.getTaskByID[id]; ok {
		return item, nil
	}
	return providerdata.TaskItem{}, fmt.Errorf("task %q not found", id)
}

func (f *fakeTasksProvider) ListAllTasks(_ context.Context) ([]providerdata.TaskItem, error) {
	f.listAllCalls++
	if f.allTasks == nil {
		return nil, tasks.ErrUnsupported
	}
	out := make([]providerdata.TaskItem, len(f.allTasks))
	copy(out, f.allTasks)
	return out, nil
}

func (f *fakeTasksProvider) CreateTask(_ context.Context, listID string, t providerdata.TaskItem) (providerdata.TaskItem, error) {
	if !f.hasMutator {
		return providerdata.TaskItem{}, tasks.ErrUnsupported
	}
	f.createCalls++
	t.ID = fmt.Sprintf("task-%d", f.createCalls)
	t.ListID = listID
	t.ProviderRef = t.ID
	if f.tasksByList == nil {
		f.tasksByList = make(map[string][]providerdata.TaskItem)
	}
	f.tasksByList[listID] = append(f.tasksByList[listID], t)
	return t, nil
}

func (f *fakeTasksProvider) UpdateTask(_ context.Context, listID string, t providerdata.TaskItem) (providerdata.TaskItem, error) {
	if !f.hasMutator {
		return providerdata.TaskItem{}, tasks.ErrUnsupported
	}
	f.updateCalls++
	t.ListID = listID
	t.ProviderRef = t.ID
	if f.tasksByList == nil {
		f.tasksByList = make(map[string][]providerdata.TaskItem)
	}
	f.tasksByList[listID] = append(f.tasksByList[listID], t)
	return t, nil
}

func (f *fakeTasksProvider) DeleteTask(_ context.Context, listID, id string) error {
	if !f.hasMutator {
		return tasks.ErrUnsupported
	}
	f.deleteCalls++
	if f.tasksByList == nil {
		return fmt.Errorf("task %q not found", id)
	}
	tasks := f.tasksByList[listID]
	for i, item := range tasks {
		if item.ID == id {
			f.tasksByList[listID] = append(tasks[:i], tasks[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("task %q not found", id)
}

func (f *fakeTasksProvider) CompleteTask(_ context.Context, listID, id string) error {
	if !f.hasCompleter {
		return tasks.ErrUnsupported
	}
	f.completeCalls++
	return nil
}

func (f *fakeTasksProvider) UncompleteTask(_ context.Context, listID, id string) error {
	if !f.hasCompleter {
		return tasks.ErrUnsupported
	}
	f.uncompleteCalls++
	return nil
}

func (f *fakeTasksProvider) CreateTaskList(_ context.Context, name string) (providerdata.TaskList, error) {
	if !f.hasListManager {
		return providerdata.TaskList{}, tasks.ErrUnsupported
	}
	tl := providerdata.TaskList{ID: fmt.Sprintf("list-%d", len(f.taskLists)+1), Name: name}
	f.taskLists = append(f.taskLists, tl)
	return tl, nil
}

func (f *fakeTasksProvider) DeleteTaskList(_ context.Context, id string) error {
	if !f.hasListManager {
		return tasks.ErrUnsupported
	}
	for i, tl := range f.taskLists {
		if tl.ID == id {
			f.taskLists = append(f.taskLists[:i], f.taskLists[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("task list %q not found", id)
}

func (f *fakeTasksProvider) ProviderName() string {
	if f.name == "" {
		return "fake_tasks"
	}
	return f.name
}

func (f *fakeTasksProvider) Close() error {
	f.closeCalls++
	return nil
}

type readOnlyTasksProvider struct {
	name        string
	taskLists   []providerdata.TaskList
	tasksByList map[string][]providerdata.TaskItem
}

func (p *readOnlyTasksProvider) ListTaskLists(_ context.Context) ([]providerdata.TaskList, error) {
	out := make([]providerdata.TaskList, len(p.taskLists))
	copy(out, p.taskLists)
	return out, nil
}

func (p *readOnlyTasksProvider) ListTasks(_ context.Context, listID string) ([]providerdata.TaskItem, error) {
	items, ok := p.tasksByList[listID]
	if !ok {
		return nil, nil
	}
	out := make([]providerdata.TaskItem, len(items))
	copy(out, items)
	return out, nil
}

func (p *readOnlyTasksProvider) GetTask(_ context.Context, listID, id string) (providerdata.TaskItem, error) {
	items, ok := p.tasksByList[listID]
	if !ok {
		return providerdata.TaskItem{}, fmt.Errorf("task %q not found", id)
	}
	for _, item := range items {
		if item.ID == id {
			return item, nil
		}
	}
	return providerdata.TaskItem{}, fmt.Errorf("task %q not found", id)
}

func (p *readOnlyTasksProvider) ProviderName() string { return p.name }
func (p *readOnlyTasksProvider) Close() error         { return nil }

func TestTaskListListRoutesByAccountID(t *testing.T) {
	s, st, _ := newDomainServerForTest(t)
	work, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderExchangeEWS, "Work EWS", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount(work): %v", err)
	}
	private, err := st.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderGoogleCalendar, "Personal", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount(private): %v", err)
	}
	workProvider := &fakeTasksProvider{
		name:      "exchange_ews_tasks",
		taskLists: []providerdata.TaskList{{ID: "list-1", Name: "Inbox", Primary: true}},
		tasksByList: map[string][]providerdata.TaskItem{
			"list-1": {{ID: "t-1", ListID: "list-1", Title: "Buy milk", Completed: false}},
		},
	}
	privateProvider := &fakeTasksProvider{name: "google_tasks", taskLists: []providerdata.TaskList{{ID: "list-2", Name: "Personal"}}}
	s.newTasksProvider = func(_ context.Context, account store.ExternalAccount) (tasks.Provider, error) {
		switch account.ID {
		case work.ID:
			return workProvider, nil
		case private.ID:
			return privateProvider, nil
		}
		return nil, fmt.Errorf("unexpected account: %d", account.ID)
	}

	got, err := s.callTool("sloppy_tasks", map[string]interface{}{"action": "list_lists", "account_id": work.ID})
	if err != nil {
		t.Fatalf("task_list_list: %v", err)
	}
	if got["account_id"] != work.ID {
		t.Fatalf("account_id = %v, want %d", got["account_id"], work.ID)
	}
	if got["provider"] != "exchange_ews_tasks" {
		t.Fatalf("provider = %v, want exchange_ews_tasks", got["provider"])
	}
	if got["count"].(int) != 1 {
		t.Fatalf("count = %v, want 1", got["count"])
	}
	listPayload, _ := got["task_lists"].([]map[string]interface{})
	if listPayload[0]["name"] != "Inbox" {
		t.Fatalf("first list name = %v, want Inbox", listPayload[0]["name"])
	}
	if workProvider.listCalls != 1 {
		t.Fatalf("workProvider.listCalls = %d, want 1", workProvider.listCalls)
	}
	if workProvider.closeCalls != 1 {
		t.Fatalf("workProvider.closeCalls = %d, want 1", workProvider.closeCalls)
	}
}

func TestTaskListEnumeratesTasks(t *testing.T) {
	s, st, _ := newDomainServerForTest(t)
	account, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderExchangeEWS, "TU Graz", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	provider := &fakeTasksProvider{
		name:      "exchange_ews_tasks",
		taskLists: []providerdata.TaskList{{ID: "list-1", Name: "Inbox", Primary: true}},
		tasksByList: map[string][]providerdata.TaskItem{
			"list-1": {
				{ID: "t-1", ListID: "list-1", Title: "Buy milk", Completed: false},
				{ID: "t-2", ListID: "list-1", Title: "Send report", Completed: true},
			},
		},
	}
	s.newTasksProvider = func(_ context.Context, _ store.ExternalAccount) (tasks.Provider, error) {
		return provider, nil
	}

	got, err := s.callTool("sloppy_tasks", map[string]interface{}{"action": "list", "account_id": account.ID, "list_id": "list-1"})
	if err != nil {
		t.Fatalf("task_list: %v", err)
	}
	if got["list_id"] != "list-1" {
		t.Fatalf("list_id = %v, want list-1", got["list_id"])
	}
	if got["count"].(int) != 2 {
		t.Fatalf("count = %v, want 2", got["count"])
	}
	tasksArr, _ := got["tasks"].([]map[string]interface{})
	if tasksArr[0]["title"] != "Buy milk" {
		t.Fatalf("first task = %v, want Buy milk (incomplete tasks sorted first)", tasksArr[0]["title"])
	}
	if tasksArr[1]["title"] != "Send report" {
		t.Fatalf("second task = %v, want Send report (completed tasks sorted last)", tasksArr[1]["title"])
	}
}

func TestTaskGetReturnsPayload(t *testing.T) {
	s, st, _ := newDomainServerForTest(t)
	account, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderGoogleCalendar, "Google", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	dueTime := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	provider := &fakeTasksProvider{
		name:      "google_tasks",
		taskLists: []providerdata.TaskList{{ID: "list-1", Name: "Primary", Primary: true}},
		tasksByList: map[string][]providerdata.TaskItem{
			"list-1": {{ID: "t-1", ListID: "list-1", Title: "Review PR", Due: &dueTime, Priority: "high"}},
		},
		getTaskByID: map[string]providerdata.TaskItem{
			"t-1": {ID: "t-1", ListID: "list-1", Title: "Review PR", Due: &dueTime, Priority: "high"},
		},
	}
	s.newTasksProvider = func(_ context.Context, _ store.ExternalAccount) (tasks.Provider, error) {
		return provider, nil
	}

	got, err := s.callTool("sloppy_tasks", map[string]interface{}{"action": "get", "account_id": account.ID, "list_id": "list-1", "id": "t-1"})
	if err != nil {
		t.Fatalf("task_get: %v", err)
	}
	task, ok := got["task"].(map[string]interface{})
	if !ok {
		t.Fatalf("task type = %T, want map[string]interface{}", got["task"])
	}
	if task["title"] != "Review PR" {
		t.Fatalf("title = %v, want Review PR", task["title"])
	}
	if task["priority"] != "high" {
		t.Fatalf("priority = %v, want high", task["priority"])
	}
	if task["due"] != "2026-05-01T12:00:00Z" {
		t.Fatalf("due = %v, want 2026-05-01T12:00:00Z", task["due"])
	}
}

func TestTaskGetIncludesTodoistMetadata(t *testing.T) {
	s, st, _ := newDomainServerForTest(t)
	account, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderTodoist, "Todoist", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	provider := &fakeTasksProvider{
		name:      "todoist",
		taskLists: []providerdata.TaskList{{ID: "proj-1", Name: "Inbox", Primary: true, ProviderURL: "https://todoist.com/app/project/proj-1"}},
		getTaskByID: map[string]providerdata.TaskItem{
			"task-1": {
				ID:           "task-1",
				ListID:       "proj-1",
				Title:        "Review proposal",
				Notes:        "Check budget section",
				Description:  "Check budget section",
				ProjectID:    "proj-1",
				SectionID:    "sec-1",
				ParentID:     "parent-1",
				Labels:       []string{"waiting", "review"},
				AssigneeID:   "user-1",
				AssignerID:   "user-2",
				AssigneeName: "Alice",
				ProviderRef:  "task-1",
				ProviderURL:  "https://todoist.com/showTask?id=task-1",
				Comments: []providerdata.TaskComment{{
					ID:        "comment-1",
					TaskID:    "task-1",
					ProjectID: "proj-1",
					Content:   "Remember appendix",
					PostedAt:  time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC),
				}},
			},
		},
	}
	s.newTasksProvider = func(_ context.Context, _ store.ExternalAccount) (tasks.Provider, error) {
		return provider, nil
	}

	got, err := s.callTool("sloppy_tasks", map[string]interface{}{"action": "get", "account_id": account.ID, "list_id": "proj-1", "id": "task-1"})
	if err != nil {
		t.Fatalf("task_get: %v", err)
	}
	task, ok := got["task"].(map[string]interface{})
	if !ok {
		t.Fatalf("task type = %T, want map[string]interface{}", got["task"])
	}
	if task["provider_ref"] != "task-1" || task["provider_url"] != "https://todoist.com/showTask?id=task-1" {
		t.Fatalf("provider fields = %#v", task)
	}
	if task["project_id"] != "proj-1" || task["section_id"] != "sec-1" || task["parent_id"] != "parent-1" {
		t.Fatalf("project fields = %#v", task)
	}
	if labels, ok := task["labels"].([]string); !ok || len(labels) != 2 || labels[0] != "waiting" {
		t.Fatalf("labels = %#v", task["labels"])
	}
	comments, ok := task["comments"].([]map[string]interface{})
	if !ok || len(comments) != 1 || comments[0]["id"] != "comment-1" {
		t.Fatalf("comments = %#v", task["comments"])
	}
}

func ptrString(v string) *string { return &v }

func TestTaskListListPicksFirstEnabledAccountForSphere(t *testing.T) {
	s, st, _ := newDomainServerForTest(t)
	disabled, err := st.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderGoogleCalendar, "Disabled Google", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount(disabled): %v", err)
	}
	if err := st.UpdateExternalAccount(disabled.ID, store.ExternalAccountUpdate{Enabled: ptrBool(false)}); err != nil {
		t.Fatalf("disable account: %v", err)
	}
	enabled, err := st.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderGoogleCalendar, "Enabled Google", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount(enabled): %v", err)
	}
	enabledProvider := &fakeTasksProvider{name: "google_tasks", taskLists: []providerdata.TaskList{{ID: "list-1", Name: "Primary"}}}
	s.newTasksProvider = func(_ context.Context, account store.ExternalAccount) (tasks.Provider, error) {
		if account.ID != enabled.ID {
			return nil, fmt.Errorf("unexpected account selected: %d", account.ID)
		}
		return enabledProvider, nil
	}

	got, err := s.callTool("sloppy_tasks", map[string]interface{}{"action": "list_lists", "sphere": "private"})
	if err != nil {
		t.Fatalf("task_list_list(sphere=private): %v", err)
	}
	if got["account_id"] != enabled.ID {
		t.Fatalf("account_id = %v, want %d", got["account_id"], enabled.ID)
	}
	if enabledProvider.listCalls != 1 {
		t.Fatalf("enabledProvider.listCalls = %d, want 1", enabledProvider.listCalls)
	}
}

func TestTaskListListKeepsTodoistWorkScoped(t *testing.T) {
	s, st, _ := newDomainServerForTest(t)
	work, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderTodoist, "Work Todoist", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount(work): %v", err)
	}
	private, err := st.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderTodoist, "Private Todoist", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount(private): %v", err)
	}
	workProvider := &fakeTasksProvider{name: "todoist", taskLists: []providerdata.TaskList{{ID: "work-proj", Name: "Work"}}}
	s.newTasksProvider = func(_ context.Context, account store.ExternalAccount) (tasks.Provider, error) {
		if account.ID == private.ID {
			return nil, fmt.Errorf("private Todoist selected for work-scoped request")
		}
		if account.ID != work.ID {
			return nil, fmt.Errorf("unexpected account selected: %d", account.ID)
		}
		return workProvider, nil
	}

	got, err := s.callTool("sloppy_tasks", map[string]interface{}{"action": "list_lists", "sphere": "work"})
	if err != nil {
		t.Fatalf("task_list_list(sphere=work): %v", err)
	}
	if got["account_id"] != work.ID {
		t.Fatalf("account_id = %v, want %d", got["account_id"], work.ID)
	}
}

func TestTaskListListWithoutAnyTasksAccountErrors(t *testing.T) {
	s, _, _ := newDomainServerForTest(t)
	if _, err := s.callTool("sloppy_tasks", map[string]interface{}{"action": "list_lists"}); err == nil {
		t.Fatal("task_list_list without any tasks-capable account should error")
	}
}

func TestTasksProviderForToolRejectsUnsupportedProvider(t *testing.T) {
	s, st, _ := newDomainServerForTest(t)
	imap, err := st.CreateExternalAccount(store.SpherePrivate, "imap", "imap.example.com", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount(imap): %v", err)
	}
	_, err = s.callTool("sloppy_tasks", map[string]interface{}{"action": "list_lists", "account_id": imap.ID})
	if err == nil {
		t.Fatal("task_list_list with imap account should error: imap is not tasks-capable")
	}
}
