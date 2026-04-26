package mcp

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/groupware"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/store"
	"github.com/sloppy-org/sloptools/internal/tasks"
)

func (s *Server) tasksProviderForTool(args map[string]interface{}) (store.ExternalAccount, tasks.Provider, error) {
	st, err := s.requireStore()
	if err != nil {
		return store.ExternalAccount{}, nil, err
	}
	account, err := accountForTool(st, args, "tasks-capable", isTasksCapableProvider)
	if err != nil {
		return store.ExternalAccount{}, nil, err
	}
	provider, err := s.tasksProviderForAccount(context.Background(), account)
	if err != nil {
		return store.ExternalAccount{}, nil, err
	}
	return account, provider, nil
}

func (s *Server) tasksProviderForAccount(ctx context.Context, account store.ExternalAccount) (tasks.Provider, error) {
	if s.newTasksProvider != nil {
		return s.newTasksProvider(ctx, account)
	}
	if s.groupware == nil {
		return nil, errors.New("groupware registry is not configured")
	}
	return s.groupware.TasksFor(ctx, account.ID)
}

func isTasksCapableProvider(provider string) bool {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case store.ExternalProviderGmail, store.ExternalProviderGoogleCalendar, store.ExternalProviderExchangeEWS:
		return true
	default:
		return false
	}
}

func firstTasksCapableAccount(st *store.Store, sphere string) (store.ExternalAccount, error) {
	return firstCapableAccount(st, sphere, "tasks-capable", isTasksCapableProvider)
}

func taskListPayload(list providerdata.TaskList, providerName string) map[string]interface{} {
	return map[string]interface{}{"id": list.ID, "name": list.Name, "primary": list.Primary, "provider": providerName}
}

func taskPayload(item providerdata.TaskItem, providerName string) map[string]interface{} {
	payload := map[string]interface{}{
		"id":           item.ID,
		"list_id":      item.ListID,
		"title":        item.Title,
		"notes":        item.Notes,
		"completed":    item.Completed,
		"priority":     item.Priority,
		"provider_ref": item.ProviderRef,
		"provider":     providerName,
	}
	if item.Due != nil {
		payload["due"] = item.Due.UTC().Format(time.RFC3339)
	}
	if item.CompletedAt != nil {
		payload["completed_at"] = item.CompletedAt.UTC().Format(time.RFC3339)
	}
	return payload
}

func (s *Server) dispatchTasks(method string, args map[string]interface{}) (map[string]interface{}, error) {
	switch method {
	case "task_list_list":
		return s.taskListList(args)
	case "task_list_create":
		return s.taskListCreate(args)
	case "task_list_delete":
		return s.taskListDelete(args)
	case "task_list":
		return s.taskList(args)
	case "task_get":
		return s.taskGet(args)
	case "task_create":
		return s.taskCreate(args)
	case "task_update":
		return s.taskUpdate(args)
	case "task_complete":
		return s.taskComplete(args)
	case "task_delete":
		return s.taskDelete(args)
	}
	return nil, fmt.Errorf("unknown task method: %s", method)
}

func (s *Server) taskListList(args map[string]interface{}) (map[string]interface{}, error) {
	account, provider, err := s.tasksProviderForTool(args)
	if err != nil {
		return nil, err
	}
	defer provider.Close()
	ctx := context.Background()
	lists, err := provider.ListTaskLists(ctx)
	if err != nil {
		return nil, err
	}
	sort.Slice(lists, func(i, j int) bool {
		return strings.ToLower(lists[i].Name) < strings.ToLower(lists[j].Name)
	})
	payloads := make([]map[string]interface{}, 0, len(lists))
	for _, list := range lists {
		payloads = append(payloads, taskListPayload(list, provider.ProviderName()))
	}
	return map[string]interface{}{"account_id": account.ID, "provider": provider.ProviderName(), "task_lists": payloads, "count": len(payloads)}, nil
}

func (s *Server) taskListCreate(args map[string]interface{}) (map[string]interface{}, error) {
	account, provider, err := s.tasksProviderForTool(args)
	if err != nil {
		return nil, err
	}
	defer provider.Close()
	name := strings.TrimSpace(strArg(args, "name"))
	if name == "" {
		return nil, errors.New("name is required")
	}
	manager, ok := groupware.Supports[tasks.ListManager](provider)
	if !ok {
		return map[string]interface{}{"account_id": account.ID, "provider": provider.ProviderName(), "error_code": "capability_unsupported", "capability": "tasks.ListManager", "error_detail": fmt.Sprintf("provider %s does not support task list management", provider.ProviderName())}, nil
	}
	created, err := manager.CreateTaskList(context.Background(), name)
	if err != nil {
		if errors.Is(err, tasks.ErrUnsupported) {
			return map[string]interface{}{"account_id": account.ID, "provider": provider.ProviderName(), "error_code": "capability_unsupported", "capability": "tasks.ListManager", "error_detail": err.Error()}, nil
		}
		return nil, err
	}
	return map[string]interface{}{"account_id": account.ID, "provider": provider.ProviderName(), "created": true, "task_list": taskListPayload(created, provider.ProviderName())}, nil
}

func (s *Server) taskListDelete(args map[string]interface{}) (map[string]interface{}, error) {
	account, provider, err := s.tasksProviderForTool(args)
	if err != nil {
		return nil, err
	}
	defer provider.Close()
	listID := strings.TrimSpace(strArg(args, "list_id"))
	if listID == "" {
		return nil, errors.New("list_id is required")
	}
	manager, ok := groupware.Supports[tasks.ListManager](provider)
	if !ok {
		return map[string]interface{}{"account_id": account.ID, "provider": provider.ProviderName(), "error_code": "capability_unsupported", "capability": "tasks.ListManager", "error_detail": fmt.Sprintf("provider %s does not support task list management", provider.ProviderName())}, nil
	}
	if strings.EqualFold(listID, "primary") {
		return map[string]interface{}{"account_id": account.ID, "provider": provider.ProviderName(), "error_code": "bad_request", "error_detail": "cannot delete the primary task list"}, nil
	}
	if err := manager.DeleteTaskList(context.Background(), listID); err != nil {
		if errors.Is(err, tasks.ErrUnsupported) {
			return map[string]interface{}{"account_id": account.ID, "provider": provider.ProviderName(), "error_code": "capability_unsupported", "capability": "tasks.ListManager", "error_detail": err.Error()}, nil
		}
		return nil, err
	}
	return map[string]interface{}{"account_id": account.ID, "provider": provider.ProviderName(), "id": listID, "deleted": true}, nil
}

func (s *Server) taskList(args map[string]interface{}) (map[string]interface{}, error) {
	account, provider, err := s.tasksProviderForTool(args)
	if err != nil {
		return nil, err
	}
	defer provider.Close()
	listID := strings.TrimSpace(strArg(args, "list_id"))
	if listID == "" {
		return nil, errors.New("list_id is required")
	}
	ctx := context.Background()
	items, err := provider.ListTasks(ctx, listID)
	if err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Completed != items[j].Completed {
			return !items[i].Completed
		}
		return strings.ToLower(items[i].Title) < strings.ToLower(items[j].Title)
	})
	payloads := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		payloads = append(payloads, taskPayload(item, provider.ProviderName()))
	}
	return map[string]interface{}{"account_id": account.ID, "provider": provider.ProviderName(), "list_id": listID, "tasks": payloads, "count": len(payloads)}, nil
}

func (s *Server) taskGet(args map[string]interface{}) (map[string]interface{}, error) {
	account, provider, err := s.tasksProviderForTool(args)
	if err != nil {
		return nil, err
	}
	defer provider.Close()
	listID := strings.TrimSpace(strArg(args, "list_id"))
	if listID == "" {
		return nil, errors.New("list_id is required")
	}
	id := strings.TrimSpace(strArg(args, "id"))
	if id == "" {
		return nil, errors.New("id is required")
	}
	ctx := context.Background()
	item, err := provider.GetTask(ctx, listID, id)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"account_id": account.ID, "provider": provider.ProviderName(), "task": taskPayload(item, provider.ProviderName())}, nil
}

func (s *Server) taskCreate(args map[string]interface{}) (map[string]interface{}, error) {
	account, provider, err := s.tasksProviderForTool(args)
	if err != nil {
		return nil, err
	}
	defer provider.Close()
	listID := strings.TrimSpace(strArg(args, "list_id"))
	if listID == "" {
		return nil, errors.New("list_id is required")
	}
	title := strings.TrimSpace(strArg(args, "title"))
	if title == "" {
		return nil, errors.New("title is required")
	}
	mutator, ok := groupware.Supports[tasks.Mutator](provider)
	if !ok {
		return map[string]interface{}{"account_id": account.ID, "provider": provider.ProviderName(), "error_code": "capability_unsupported", "capability": "tasks.Mutator", "error_detail": fmt.Sprintf("provider %s does not support task mutation", provider.ProviderName())}, nil
	}
	due := parseRFC3339OrDate(strArg(args, "due"))
	item := providerdata.TaskItem{
		ListID:      listID,
		Title:       title,
		Notes:       strings.TrimSpace(strArg(args, "notes")),
		Due:         &due,
		Priority:    strings.TrimSpace(strArg(args, "priority")),
		ProviderRef: "",
	}
	created, err := mutator.CreateTask(context.Background(), listID, item)
	if err != nil {
		if errors.Is(err, tasks.ErrUnsupported) {
			return map[string]interface{}{"account_id": account.ID, "provider": provider.ProviderName(), "error_code": "capability_unsupported", "capability": "tasks.Mutator", "error_detail": err.Error()}, nil
		}
		return nil, err
	}
	return map[string]interface{}{"account_id": account.ID, "provider": provider.ProviderName(), "created": true, "task": taskPayload(created, provider.ProviderName())}, nil
}

func (s *Server) taskUpdate(args map[string]interface{}) (map[string]interface{}, error) {
	account, provider, err := s.tasksProviderForTool(args)
	if err != nil {
		return nil, err
	}
	defer provider.Close()
	listID := strings.TrimSpace(strArg(args, "list_id"))
	if listID == "" {
		return nil, errors.New("list_id is required")
	}
	id := strings.TrimSpace(strArg(args, "id"))
	if id == "" {
		return nil, errors.New("id is required")
	}
	title := strings.TrimSpace(strArg(args, "title"))
	if title == "" {
		return nil, errors.New("title is required")
	}
	mutator, ok := groupware.Supports[tasks.Mutator](provider)
	if !ok {
		return map[string]interface{}{"account_id": account.ID, "provider": provider.ProviderName(), "error_code": "capability_unsupported", "capability": "tasks.Mutator", "error_detail": fmt.Sprintf("provider %s does not support task mutation", provider.ProviderName())}, nil
	}
	due := parseRFC3339OrDate(strArg(args, "due"))
	item := providerdata.TaskItem{
		ID:          id,
		ListID:      listID,
		Title:       title,
		Notes:       strings.TrimSpace(strArg(args, "notes")),
		Due:         &due,
		Priority:    strings.TrimSpace(strArg(args, "priority")),
		ProviderRef: "",
	}
	updated, err := mutator.UpdateTask(context.Background(), listID, item)
	if err != nil {
		if errors.Is(err, tasks.ErrUnsupported) {
			return map[string]interface{}{"account_id": account.ID, "provider": provider.ProviderName(), "error_code": "capability_unsupported", "capability": "tasks.Mutator", "error_detail": err.Error()}, nil
		}
		return nil, err
	}
	return map[string]interface{}{"account_id": account.ID, "provider": provider.ProviderName(), "updated": true, "task": taskPayload(updated, provider.ProviderName())}, nil
}

func (s *Server) taskComplete(args map[string]interface{}) (map[string]interface{}, error) {
	account, provider, err := s.tasksProviderForTool(args)
	if err != nil {
		return nil, err
	}
	defer provider.Close()
	listID := strings.TrimSpace(strArg(args, "list_id"))
	if listID == "" {
		return nil, errors.New("list_id is required")
	}
	id := strings.TrimSpace(strArg(args, "id"))
	if id == "" {
		return nil, errors.New("id is required")
	}
	completed := true
	if raw, ok := args["completed"]; ok {
		switch v := raw.(type) {
		case bool:
			completed = v
		case float64:
			completed = v == 1
		}
	}
	completer, ok := groupware.Supports[tasks.Completer](provider)
	if !ok {
		return map[string]interface{}{"account_id": account.ID, "provider": provider.ProviderName(), "error_code": "capability_unsupported", "capability": "tasks.Completer", "error_detail": fmt.Sprintf("provider %s does not support task completion", provider.ProviderName())}, nil
	}
	if completed {
		if err := completer.CompleteTask(context.Background(), listID, id); err != nil {
			if errors.Is(err, tasks.ErrUnsupported) {
				return map[string]interface{}{"account_id": account.ID, "provider": provider.ProviderName(), "error_code": "capability_unsupported", "capability": "tasks.Completer", "error_detail": err.Error()}, nil
			}
			return nil, err
		}
	} else {
		if err := completer.UncompleteTask(context.Background(), listID, id); err != nil {
			if errors.Is(err, tasks.ErrUnsupported) {
				return map[string]interface{}{"account_id": account.ID, "provider": provider.ProviderName(), "error_code": "capability_unsupported", "capability": "tasks.Completer", "error_detail": err.Error()}, nil
			}
			return nil, err
		}
	}
	return map[string]interface{}{"account_id": account.ID, "provider": provider.ProviderName(), "id": id, "list_id": listID, "completed": completed}, nil
}

func (s *Server) taskDelete(args map[string]interface{}) (map[string]interface{}, error) {
	account, provider, err := s.tasksProviderForTool(args)
	if err != nil {
		return nil, err
	}
	defer provider.Close()
	listID := strings.TrimSpace(strArg(args, "list_id"))
	if listID == "" {
		return nil, errors.New("list_id is required")
	}
	id := strings.TrimSpace(strArg(args, "id"))
	if id == "" {
		return nil, errors.New("id is required")
	}
	mutator, ok := groupware.Supports[tasks.Mutator](provider)
	if !ok {
		return map[string]interface{}{"account_id": account.ID, "provider": provider.ProviderName(), "error_code": "capability_unsupported", "capability": "tasks.Mutator", "error_detail": fmt.Sprintf("provider %s does not support task mutation", provider.ProviderName())}, nil
	}
	if err := mutator.DeleteTask(context.Background(), listID, id); err != nil {
		if errors.Is(err, tasks.ErrUnsupported) {
			return map[string]interface{}{"account_id": account.ID, "provider": provider.ProviderName(), "error_code": "capability_unsupported", "capability": "tasks.Mutator", "error_detail": err.Error()}, nil
		}
		return nil, err
	}
	return map[string]interface{}{"account_id": account.ID, "provider": provider.ProviderName(), "id": id, "list_id": listID, "deleted": true}, nil
}

func parseRFC3339OrDate(raw string) time.Time {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return time.Time{}
	}
	if value, err := time.Parse(time.RFC3339, clean); err == nil {
		return value.UTC()
	}
	if value, err := time.Parse("2006-01-02", clean); err == nil {
		return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
	}
	return time.Time{}
}
