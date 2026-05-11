package inbox

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/brain"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/store"
	"github.com/sloppy-org/sloptools/internal/tasks"
)

type Handler struct {
	Store           *store.Store
	BrainConfigPath string
	TaskProvider    func(context.Context, store.ExternalAccount) (tasks.Provider, error)
}

func (h Handler) Dispatch(method string, args map[string]interface{}) (map[string]interface{}, error) {
	switch method {
	case "inbox.source_list":
		return h.sourceList(args)
	case "inbox.item_list":
		if IsFileSourceID(strArg(args, "source_id")) {
			return h.fileItemList(args)
		}
		return h.taskItemList(args)
	case "inbox.item_plan":
		if IsFileSourceID(strArg(args, "source_id")) {
			return h.fileItemPlan(args)
		}
		return h.taskItemPlan(args)
	case "inbox.item_ack":
		targetRef := strings.TrimSpace(strArg(args, "target_ref"))
		if targetRef == "" {
			return nil, errors.New("target_ref is required")
		}
		if IsFileSourceID(strArg(args, "source_id")) {
			return h.fileItemAck(args, targetRef)
		}
		return h.taskItemAck(args, targetRef)
	default:
		return nil, fmt.Errorf("unknown inbox method: %s", method)
	}
}

func (h Handler) sourceList(args map[string]interface{}) (map[string]interface{}, error) {
	taskSources, err := h.taskSources(args)
	if err != nil {
		return nil, err
	}
	cfg, err := brain.LoadConfig(h.BrainConfigPath)
	if err != nil {
		return nil, err
	}
	fileSources, err := FileSources(cfg, strings.TrimSpace(strArg(args, "sphere")))
	if err != nil {
		return nil, err
	}
	payloads := make([]map[string]interface{}, 0, len(taskSources)+len(fileSources))
	payloads = append(payloads, taskSources...)
	for _, source := range fileSources {
		payloads = append(payloads, fileSourcePayload(source))
	}
	return map[string]interface{}{"sources": payloads, "count": len(payloads)}, nil
}

func (h Handler) taskSources(args map[string]interface{}) ([]map[string]interface{}, error) {
	accounts, err := h.Store.ListExternalAccounts(strings.TrimSpace(strArg(args, "sphere")))
	if err != nil {
		return nil, err
	}
	out := make([]map[string]interface{}, 0, len(accounts))
	for _, account := range accounts {
		if !account.Enabled || !isTasksCapable(account.Provider) {
			continue
		}
		provider, err := h.TaskProvider(context.Background(), account)
		if err != nil {
			return nil, err
		}
		list, items, err := findTaskInbox(context.Background(), provider, "")
		closeErr := provider.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, closeErr
		}
		out = append(out, taskSourcePayload(account, list, len(IncompleteTasks(items))))
	}
	sort.Slice(out, func(i, j int) bool { return fmt.Sprint(out[i]["id"]) < fmt.Sprint(out[j]["id"]) })
	return out, nil
}

func (h Handler) taskItemList(args map[string]interface{}) (map[string]interface{}, error) {
	account, provider, list, err := h.taskInboxForArgs(args)
	if err != nil {
		return nil, err
	}
	defer provider.Close()
	items, err := provider.ListTasks(context.Background(), list.ID)
	if err != nil {
		return nil, err
	}
	items = IncompleteTasks(items)
	SortTasks(items)
	limit := intArg(args, "limit", 20)
	if limit <= 0 {
		limit = 20
	}
	if len(items) > limit {
		items = items[:limit]
	}
	payloads := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		payloads = append(payloads, taskItemPayload(account, provider.ProviderName(), item))
	}
	return map[string]interface{}{"source": taskSourcePayload(account, list, len(items)), "items": payloads, "count": len(payloads)}, nil
}

func (h Handler) taskItemPlan(args map[string]interface{}) (map[string]interface{}, error) {
	account, provider, list, err := h.taskInboxForArgs(args)
	if err != nil {
		return nil, err
	}
	defer provider.Close()
	id := strings.TrimSpace(strArg(args, "id"))
	if id == "" {
		return nil, errors.New("id is required")
	}
	item, err := provider.GetTask(context.Background(), list.ID, id)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"source": taskSourcePayload(account, list, 0), "item": taskItemPayload(account, provider.ProviderName(), item), "plan": ClassifyTask(account.Sphere, item, strArg(args, "context"))}, nil
}

func (h Handler) taskItemAck(args map[string]interface{}, targetRef string) (map[string]interface{}, error) {
	account, provider, list, err := h.taskInboxForArgs(args)
	if err != nil {
		return nil, err
	}
	defer provider.Close()
	id := strings.TrimSpace(strArg(args, "id"))
	if id == "" {
		return nil, errors.New("id is required")
	}
	completer, ok := provider.(tasks.Completer)
	if !ok {
		return map[string]interface{}{"account_id": account.ID, "provider": provider.ProviderName(), "error_code": "capability_unsupported", "capability": "tasks.Completer", "error_detail": fmt.Sprintf("provider %s does not support task completion", provider.ProviderName())}, nil
	}
	if err := completer.CompleteTask(context.Background(), list.ID, id); err != nil {
		if errors.Is(err, tasks.ErrUnsupported) {
			return map[string]interface{}{"account_id": account.ID, "provider": provider.ProviderName(), "error_code": "capability_unsupported", "capability": "tasks.Completer", "error_detail": err.Error()}, nil
		}
		return nil, err
	}
	return map[string]interface{}{"account_id": account.ID, "provider": provider.ProviderName(), "source_id": TaskSourceID(account, list), "id": id, "list_id": list.ID, "target_ref": targetRef, "acknowledged": true}, nil
}

func (h Handler) fileItemList(args map[string]interface{}) (map[string]interface{}, error) {
	source, err := h.fileSource(args)
	if err != nil {
		return nil, err
	}
	items, err := ListBareFiles(source)
	if err != nil {
		return nil, err
	}
	limit := intArg(args, "limit", 20)
	if limit <= 0 {
		limit = 20
	}
	if len(items) > limit {
		items = items[:limit]
	}
	payloads := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		payloads = append(payloads, fileItemPayload(source, item))
	}
	return map[string]interface{}{"source": fileSourcePayload(source), "items": payloads, "count": len(payloads)}, nil
}

func (h Handler) fileItemPlan(args map[string]interface{}) (map[string]interface{}, error) {
	source, item, err := h.fileItem(args)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"source": fileSourcePayload(source), "item": fileItemPayload(source, item), "plan": ClassifyFile(source, item, strArg(args, "context"))}, nil
}

func (h Handler) fileItemAck(args map[string]interface{}, targetRef string) (map[string]interface{}, error) {
	source, item, err := h.fileItem(args)
	if err != nil {
		return nil, err
	}
	rel, err := MoveFile(source, item, strings.TrimSpace(strArg(args, "target_path")))
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"source_id": FileSourceID(source.Sphere), "id": item.ID, "target_ref": targetRef, "target_path": rel, "acknowledged": true}, nil
}

func (h Handler) taskInboxForArgs(args map[string]interface{}) (store.ExternalAccount, tasks.Provider, providerdata.TaskList, error) {
	args = withTaskSourceDefaults(args)
	account, err := accountForTaskTool(h.Store, args)
	if err != nil {
		return store.ExternalAccount{}, nil, providerdata.TaskList{}, err
	}
	provider, err := h.TaskProvider(context.Background(), account)
	if err != nil {
		return store.ExternalAccount{}, nil, providerdata.TaskList{}, err
	}
	list, _, err := findTaskInbox(context.Background(), provider, strings.TrimSpace(strArg(args, "list_id")))
	if err != nil {
		provider.Close()
		return store.ExternalAccount{}, nil, providerdata.TaskList{}, err
	}
	return account, provider, list, nil
}

func (h Handler) fileSource(args map[string]interface{}) (FileSource, error) {
	cfg, err := brain.LoadConfig(h.BrainConfigPath)
	if err != nil {
		return FileSource{}, err
	}
	return FileSourceForID(cfg, strArg(args, "source_id"))
}

func (h Handler) fileItem(args map[string]interface{}) (FileSource, FileItem, error) {
	source, err := h.fileSource(args)
	if err != nil {
		return FileSource{}, FileItem{}, err
	}
	item, err := FileItemForID(source, strings.TrimSpace(strArg(args, "id")))
	return source, item, err
}

func findTaskInbox(ctx context.Context, provider tasks.Provider, listID string) (providerdata.TaskList, []providerdata.TaskItem, error) {
	lists, err := provider.ListTaskLists(ctx)
	if err != nil {
		return providerdata.TaskList{}, nil, err
	}
	list, ok := ChooseTaskInboxList(lists, listID)
	if !ok {
		return providerdata.TaskList{}, nil, errors.New("no task inbox list found")
	}
	items, err := provider.ListTasks(ctx, list.ID)
	return list, items, err
}

func taskSourcePayload(account store.ExternalAccount, list providerdata.TaskList, count int) map[string]interface{} {
	return map[string]interface{}{"id": TaskSourceID(account, list), "type": "google_tasks", "sphere": account.Sphere, "account_id": account.ID, "provider": account.Provider, "list_id": list.ID, "name": list.Name, "ack_action": "task_complete", "mode": "active", "pending_count": count}
}

func taskItemPayload(account store.ExternalAccount, providerName string, item providerdata.TaskItem) map[string]interface{} {
	payload := map[string]interface{}{"id": item.ID, "list_id": item.ListID, "title": item.Title, "notes": item.Notes, "description": item.Description, "completed": item.Completed, "priority": item.Priority, "provider_ref": item.ProviderRef, "provider_url": item.ProviderURL, "provider": providerName, "source_ref": fmt.Sprintf("tasks:%s:%d:%s:%s", account.Sphere, account.ID, item.ListID, item.ID), "ack_action": "task_complete"}
	if item.Due != nil {
		payload["due"] = item.Due.UTC().Format(time.RFC3339)
	}
	return payload
}

func fileSourcePayload(source FileSource) map[string]interface{} {
	return map[string]interface{}{"id": FileSourceID(source.Sphere), "type": "file", "sphere": source.Sphere, "name": source.Sphere + " root capture", "path": "", "mode": "active", "scope": "bare_files_only", "subdirectories": "ignored_unless_explicit", "ack_action": "move_file", "pending_count": source.Count}
}

func fileItemPayload(source FileSource, item FileItem) map[string]interface{} {
	return map[string]interface{}{"id": item.ID, "path": item.Path, "name": item.ID, "size": item.Size, "modified_at": item.ModTime.UTC().Format(time.RFC3339), "source_ref": fmt.Sprintf("file:%s:%s", source.Sphere, item.Path), "ack_action": "move_file"}
}

func withTaskSourceDefaults(args map[string]interface{}) map[string]interface{} {
	parsed := ParseTaskSourceID(strArg(args, "source_id"))
	if parsed.AccountID == 0 && parsed.ListID == "" {
		return args
	}
	out := cloneArgs(args)
	if parsed.AccountID != 0 && out["account_id"] == nil {
		out["account_id"] = float64(parsed.AccountID)
	}
	if parsed.ListID != "" && strings.TrimSpace(strArg(out, "list_id")) == "" {
		out["list_id"] = parsed.ListID
	}
	return out
}

func accountForTaskTool(st *store.Store, args map[string]interface{}) (store.ExternalAccount, error) {
	if id, ok := int64Arg(args, "account_id"); ok {
		account, err := st.GetExternalAccount(id)
		if err != nil {
			return store.ExternalAccount{}, err
		}
		if !account.Enabled || !isTasksCapable(account.Provider) {
			return store.ExternalAccount{}, fmt.Errorf("account %d provider %q does not support tasks", account.ID, account.Provider)
		}
		return account, nil
	}
	accounts, err := st.ListExternalAccounts(strings.TrimSpace(strArg(args, "sphere")))
	if err != nil {
		return store.ExternalAccount{}, err
	}
	for _, account := range accounts {
		if account.Enabled && isTasksCapable(account.Provider) {
			return account, nil
		}
	}
	return store.ExternalAccount{}, errors.New("no enabled tasks-capable account is configured")
}

func isTasksCapable(provider string) bool {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case store.ExternalProviderGmail, store.ExternalProviderGoogleCalendar, store.ExternalProviderExchangeEWS, store.ExternalProviderTodoist:
		return true
	default:
		return false
	}
}

func strArg(args map[string]interface{}, key string) string {
	v, _ := args[key].(string)
	return v
}

func intArg(args map[string]interface{}, key string, def int) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	default:
		return def
	}
}

func int64Arg(args map[string]interface{}, key string) (int64, bool) {
	switch v := args[key].(type) {
	case float64:
		return int64(v), true
	case int:
		return int64(v), true
	case int64:
		return v, true
	default:
		return 0, false
	}
}

func cloneArgs(args map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(args)+2)
	for key, value := range args {
		out[key] = value
	}
	return out
}
