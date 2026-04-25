package mcp

import (
	"context"
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
		"account_id": account.ID,
		"list_id":    "list-1",
		"title":      "New task",
		"notes":      "Do it now",
		"due":        "2026-06-01T10:00:00Z",
		"priority":   "high",
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
	if task["due"] != "2026-06-01T10:00:00Z" {
		t.Fatalf("due = %v, want 2026-06-01T10:00:00Z", task["due"])
	}
	if task["priority"] != "high" {
		t.Fatalf("priority = %v, want high", task["priority"])
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
		"account_id": account.ID,
		"list_id":    "list-1",
		"id":         "t-1",
		"title":      "Updated title",
		"notes":      "Updated notes",
		"due":        "2026-07-15",
		"priority":   "low",
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
