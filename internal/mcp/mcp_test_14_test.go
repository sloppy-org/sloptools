package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	braingtd "github.com/sloppy-org/sloptools/internal/brain/gtd"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/store"
	"github.com/sloppy-org/sloptools/internal/tasks"
	"testing"
)

func TestTaskCreateCapabilityUnsupported(t *testing.T) {
	s, st, _ := newDomainServerForTest(t)
	account, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderExchangeEWS, "TU Graz", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	provider := &readOnlyTasksProvider{name: "exchange_ews_tasks"}
	s.newTasksProvider = func(_ context.Context, _ store.ExternalAccount) (tasks.Provider, error) {
		return provider, nil
	}

	got, err := s.callTool("task_create", map[string]interface{}{
		"account_id": account.ID,
		"list_id":    "list-1",
		"title":      "New task",
	})
	if err != nil {
		t.Fatalf("task_create: %v", err)
	}
	if got["error_code"] != "capability_unsupported" {
		t.Fatalf("error_code = %v, want capability_unsupported", got["error_code"])
	}
	if got["capability"] != "tasks.Mutator" {
		t.Fatalf("capability = %v, want tasks.Mutator", got["capability"])
	}
}

func TestTaskCreateReturnsPayload(t *testing.T) {
	s, st, _ := newDomainServerForTest(t)
	account, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderExchangeEWS, "TU Graz", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	provider := &fakeTasksProvider{name: "exchange_ews_tasks", hasMutator: true}
	s.newTasksProvider = func(_ context.Context, _ store.ExternalAccount) (tasks.Provider, error) {
		return provider, nil
	}

	got, err := s.callTool("task_create", map[string]interface{}{
		"account_id":  account.ID,
		"list_id":     "list-1",
		"title":       "New task",
		"notes":       "Do it now",
		"description": "Provider body",
		"start_at":    "2026-05-30T09:00:00Z",
		"due":         "2026-06-01T10:00:00Z",
		"priority":    "high",
		"section_id":  "sec-1",
		"parent_id":   "parent-1",
		"labels":      []interface{}{"waiting", "review"},
		"assignee_id": "user-1",
	})
	if err != nil {
		t.Fatalf("task_create: %v", err)
	}
	if got["created"] != true {
		t.Fatalf("created = %v, want true", got["created"])
	}
	if provider.createCalls != 1 {
		t.Fatalf("createCalls = %d, want 1", provider.createCalls)
	}
	task := mapValue(t, got["task"])
	if task["title"] != "New task" {
		t.Fatalf("title = %v, want New task", task["title"])
	}
	if task["notes"] != "Do it now" {
		t.Fatalf("notes = %v, want Do it now", task["notes"])
	}
	if task["description"] != "Provider body" {
		t.Fatalf("description = %v, want Provider body", task["description"])
	}
	if task["start_at"] != "2026-05-30T09:00:00Z" {
		t.Fatalf("start_at = %v, want 2026-05-30T09:00:00Z", task["start_at"])
	}
	if task["due"] != "2026-06-01T10:00:00Z" {
		t.Fatalf("due = %v, want 2026-06-01T10:00:00Z", task["due"])
	}
	if task["priority"] != "high" {
		t.Fatalf("priority = %v, want high", task["priority"])
	}
	if task["section_id"] != "sec-1" || task["parent_id"] != "parent-1" || task["assignee_id"] != "user-1" {
		t.Fatalf("Todoist metadata = %#v", task)
	}
	labels, ok := task["labels"].([]string)
	if !ok || len(labels) != 2 || labels[0] != "waiting" || labels[1] != "review" {
		t.Fatalf("labels = %#v", task["labels"])
	}
}

func TestTaskUpdateFullReplace(t *testing.T) {
	s, st, _ := newDomainServerForTest(t)
	account, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderExchangeEWS, "TU Graz", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	provider := &fakeTasksProvider{name: "exchange_ews_tasks", hasMutator: true}
	s.newTasksProvider = func(_ context.Context, _ store.ExternalAccount) (tasks.Provider, error) {
		return provider, nil
	}

	got, err := s.callTool("task_update", map[string]interface{}{
		"account_id":   account.ID,
		"list_id":      "list-1",
		"id":           "t-1",
		"title":        "Updated title",
		"notes":        "Updated notes",
		"follow_up_at": "2026-07-01",
		"deadline":     "2026-07-15",
		"priority":     "low",
		"section_id":   "sec-2",
		"labels":       "waiting,next",
		"assignee_id":  "user-2",
	})
	if err != nil {
		t.Fatalf("task_update: %v", err)
	}
	if got["updated"] != true {
		t.Fatalf("updated = %v, want true", got["updated"])
	}
	if provider.updateCalls != 1 {
		t.Fatalf("updateCalls = %d, want 1", provider.updateCalls)
	}
	task := mapValue(t, got["task"])
	if task["title"] != "Updated title" {
		t.Fatalf("title = %v, want Updated title", task["title"])
	}
	if task["due"] != "2026-07-15T00:00:00Z" {
		t.Fatalf("due = %v, want 2026-07-15T00:00:00Z", task["due"])
	}
	if task["start_at"] != "2026-07-01T00:00:00Z" {
		t.Fatalf("start_at = %v, want 2026-07-01T00:00:00Z", task["start_at"])
	}
	if task["section_id"] != "sec-2" || task["assignee_id"] != "user-2" {
		t.Fatalf("Todoist metadata = %#v", task)
	}
	labels, ok := task["labels"].([]string)
	if !ok || len(labels) != 2 || labels[0] != "waiting" || labels[1] != "next" {
		t.Fatalf("labels = %#v", task["labels"])
	}
}

func TestTaskCompleteDefaultsCompletedTrue(t *testing.T) {
	s, st, _ := newDomainServerForTest(t)
	account, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderExchangeEWS, "TU Graz", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	provider := &fakeTasksProvider{name: "exchange_ews_tasks", hasCompleter: true}
	s.newTasksProvider = func(_ context.Context, _ store.ExternalAccount) (tasks.Provider, error) {
		return provider, nil
	}

	got, err := s.callTool("task_complete", map[string]interface{}{
		"account_id": account.ID,
		"list_id":    "list-1",
		"id":         "t-1",
	})
	if err != nil {
		t.Fatalf("task_complete: %v", err)
	}
	if got["completed"] != true {
		t.Fatalf("completed = %v, want true", got["completed"])
	}
	if provider.completeCalls != 1 {
		t.Fatalf("completeCalls = %d, want 1", provider.completeCalls)
	}
	if provider.uncompleteCalls != 0 {
		t.Fatalf("uncompleteCalls = %d, want 0", provider.uncompleteCalls)
	}
}

func TestTaskCompleteUncompletesWhenFalse(t *testing.T) {
	s, st, _ := newDomainServerForTest(t)
	account, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderExchangeEWS, "TU Graz", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	provider := &fakeTasksProvider{name: "exchange_ews_tasks", hasCompleter: true}
	s.newTasksProvider = func(_ context.Context, _ store.ExternalAccount) (tasks.Provider, error) {
		return provider, nil
	}

	got, err := s.callTool("task_complete", map[string]interface{}{
		"account_id": account.ID,
		"list_id":    "list-1",
		"id":         "t-1",
		"completed":  false,
	})
	if err != nil {
		t.Fatalf("task_complete: %v", err)
	}
	if got["completed"] != false {
		t.Fatalf("completed = %v, want false", got["completed"])
	}
	if provider.uncompleteCalls != 1 {
		t.Fatalf("uncompleteCalls = %d, want 1", provider.uncompleteCalls)
	}
}

func TestTaskCompleteCapabilityUnsupported(t *testing.T) {
	s, st, _ := newDomainServerForTest(t)
	account, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderExchangeEWS, "TU Graz", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	provider := &readOnlyTasksProvider{name: "exchange_ews_tasks"}
	s.newTasksProvider = func(_ context.Context, _ store.ExternalAccount) (tasks.Provider, error) {
		return provider, nil
	}

	got, err := s.callTool("task_complete", map[string]interface{}{
		"account_id": account.ID,
		"list_id":    "list-1",
		"id":         "t-1",
	})
	if err != nil {
		t.Fatalf("task_complete: %v", err)
	}
	if got["error_code"] != "capability_unsupported" {
		t.Fatalf("error_code = %v, want capability_unsupported", got["error_code"])
	}
	if got["capability"] != "tasks.Completer" {
		t.Fatalf("capability = %v, want tasks.Completer", got["capability"])
	}
}

func TestTaskDeleteRemovesTask(t *testing.T) {
	s, st, _ := newDomainServerForTest(t)
	account, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderExchangeEWS, "TU Graz", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	provider := &fakeTasksProvider{
		name: "exchange_ews_tasks", hasMutator: true,
		tasksByList: map[string][]providerdata.TaskItem{
			"list-1": {{ID: "t-1", ListID: "list-1", Title: "Buy milk"}},
		},
	}
	s.newTasksProvider = func(_ context.Context, _ store.ExternalAccount) (tasks.Provider, error) {
		return provider, nil
	}

	got, err := s.callTool("task_delete", map[string]interface{}{
		"account_id": account.ID,
		"list_id":    "list-1",
		"id":         "t-1",
	})
	if err != nil {
		t.Fatalf("task_delete: %v", err)
	}
	if got["deleted"] != true {
		t.Fatalf("deleted = %v, want true", got["deleted"])
	}
	if got["id"] != "t-1" {
		t.Fatalf("id = %v, want t-1", got["id"])
	}
	if provider.deleteCalls != 1 {
		t.Fatalf("deleteCalls = %d, want 1", provider.deleteCalls)
	}
}

func TestTaskDeleteCapabilityUnsupported(t *testing.T) {
	s, st, _ := newDomainServerForTest(t)
	account, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderExchangeEWS, "TU Graz", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	provider := &readOnlyTasksProvider{name: "exchange_ews_tasks"}
	s.newTasksProvider = func(_ context.Context, _ store.ExternalAccount) (tasks.Provider, error) {
		return provider, nil
	}

	got, err := s.callTool("task_delete", map[string]interface{}{
		"account_id": account.ID,
		"list_id":    "list-1",
		"id":         "t-1",
	})
	if err != nil {
		t.Fatalf("task_delete: %v", err)
	}
	if got["error_code"] != "capability_unsupported" {
		t.Fatalf("error_code = %v, want capability_unsupported", got["error_code"])
	}
}

func TestTaskListCreateRequiresListManager(t *testing.T) {
	s, st, _ := newDomainServerForTest(t)
	account, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderExchangeEWS, "TU Graz", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	provider := &readOnlyTasksProvider{name: "exchange_ews_tasks"}
	s.newTasksProvider = func(_ context.Context, _ store.ExternalAccount) (tasks.Provider, error) {
		return provider, nil
	}

	got, err := s.callTool("task_list_create", map[string]interface{}{
		"account_id": account.ID,
		"name":       "New List",
	})
	if err != nil {
		t.Fatalf("task_list_create: %v", err)
	}
	if got["error_code"] != "capability_unsupported" {
		t.Fatalf("error_code = %v, want capability_unsupported", got["error_code"])
	}
	if got["capability"] != "tasks.ListManager" {
		t.Fatalf("capability = %v, want tasks.ListManager", got["capability"])
	}
}

func TestTaskListCreateSuccess(t *testing.T) {
	s, st, _ := newDomainServerForTest(t)
	account, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderExchangeEWS, "TU Graz", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	provider := &fakeTasksProvider{name: "exchange_ews_tasks", hasListManager: true}
	s.newTasksProvider = func(_ context.Context, _ store.ExternalAccount) (tasks.Provider, error) {
		return provider, nil
	}

	got, err := s.callTool("task_list_create", map[string]interface{}{
		"account_id": account.ID,
		"name":       "Projects",
	})
	if err != nil {
		t.Fatalf("task_list_create: %v", err)
	}
	if got["created"] != true {
		t.Fatalf("created = %v, want true", got["created"])
	}
	list := mapValue(t, got["task_list"])
	if list["name"] != "Projects" {
		t.Fatalf("name = %v, want Projects", list["name"])
	}
}

func TestTaskListDeleteRejectsPrimaryList(t *testing.T) {
	s, st, _ := newDomainServerForTest(t)
	account, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderExchangeEWS, "TU Graz", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	provider := &fakeTasksProvider{name: "exchange_ews_tasks", hasListManager: true}
	s.newTasksProvider = func(_ context.Context, _ store.ExternalAccount) (tasks.Provider, error) {
		return provider, nil
	}

	got, err := s.callTool("task_list_delete", map[string]interface{}{
		"account_id": account.ID,
		"list_id":    "primary",
	})
	if err != nil {
		t.Fatalf("task_list_delete: %v", err)
	}
	if got["error_code"] != "bad_request" {
		t.Fatalf("error_code = %v, want bad_request", got["error_code"])
	}
}

func TestBrainGTDWriteToolCreatesMissingCommitmentNotes(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	notePath := filepath.Join("brain", "gtd", "created.md")

	s := NewServer(t.TempDir())
	got, err := s.callTool("brain.gtd.write", map[string]interface{}{
		"config_path": configPath,
		"sphere":      "work",
		"path":        notePath,
		"commitment": map[string]interface{}{
			"title":       "Reply to Bea",
			"status":      "next",
			"next_action": "Send the reply",
			"context":     "email",
			"source_bindings": []interface{}{
				map[string]interface{}{"provider": "mail", "ref": "mail-42"},
			},
		},
	})
	if err != nil {
		t.Fatalf("brain.gtd.write: %v", err)
	}
	if got["valid"] != true {
		t.Fatalf("valid = %v, want true: %#v", got["valid"], got)
	}
	created, err := os.ReadFile(filepath.Join(tmp, "work", notePath))
	if err != nil {
		t.Fatalf("read created note: %v", err)
	}
	for _, want := range []string{"kind: commitment", "status: next", "source_bindings:", "provider: mail", "ref: mail-42", "## Summary", "## Next Action"} {
		if !strings.Contains(string(created), want) {
			t.Fatalf("created note missing %q:\n%s", want, string(created))
		}
	}
	if result := braingtd.ParseAndValidate(string(created)); len(result.Diagnostics) != 0 {
		t.Fatalf("created note invalid: %#v\n%s", result.Diagnostics, string(created))
	}
}
