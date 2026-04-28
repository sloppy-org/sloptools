package tasks

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/ews"
	"github.com/sloppy-org/sloptools/internal/providerdata"
)

const ewsProviderName = "exchange_ews"

// EWSProvider implements Provider, Mutator, Completer, and ListManager
// against Exchange EWS Task items.
type EWSProvider struct {
	client          *ews.Client
	rootTasksFolder string // defaults to "tasks" (distinguished tasks folder)
}

// Compile-time contract checks.
var (
	_ Provider    = (*EWSProvider)(nil)
	_ Mutator     = (*EWSProvider)(nil)
	_ Completer   = (*EWSProvider)(nil)
	_ ListManager = (*EWSProvider)(nil)
)

// NewEWSProvider wraps an EWS client for task operations. The rootTasksFolder
// parameter may be empty to default to the distinguished "tasks" folder.
func NewEWSProvider(client *ews.Client, rootTasksFolder string) *EWSProvider {
	if rootTasksFolder == "" {
		rootTasksFolder = string(ews.FolderKindTasks)
	}
	return &EWSProvider{
		client:          client,
		rootTasksFolder: rootTasksFolder,
	}
}

// ProviderName identifies the backend in logs and error messages.
func (p *EWSProvider) ProviderName() string { return ewsProviderName }

// Close releases any long-lived resources; currently a no-op since the EWS
// client is shared with other feature providers.
func (p *EWSProvider) Close() error { return nil }

// ListTaskLists returns the primary tasks folder plus any discovered subfolders.
func (p *EWSProvider) ListTaskLists(ctx context.Context) ([]providerdata.TaskList, error) {
	folders, err := p.client.ListTasksFolders(ctx)
	if err != nil {
		return nil, fmt.Errorf("ews list tasks folders: %w", err)
	}
	lists := make([]providerdata.TaskList, 0, len(folders)+1)
	// Prepend the primary tasks folder.
	lists = append(lists, providerdata.TaskList{
		ID:      p.rootTasksFolder,
		Name:    "Tasks",
		Primary: true,
	})
	// Append discovered subfolders.
	for _, f := range folders {
		lists = append(lists, providerdata.TaskList{
			ID:      f.ID,
			Name:    f.Name,
			Primary: false,
		})
	}
	return lists, nil
}

// ListTasks returns all tasks in a list.
func (p *EWSProvider) ListTasks(ctx context.Context, listID string) ([]providerdata.TaskItem, error) {
	listID = strings.TrimSpace(listID)
	if listID == "" {
		return nil, fmt.Errorf("tasks: list id is required")
	}
	rawItems, err := p.client.GetTasks(ctx, listID, 0, 0)
	if err != nil {
		return nil, err
	}
	out := make([]providerdata.TaskItem, 0, len(rawItems))
	for _, raw := range rawItems {
		out = append(out, taskItemFromEWS(raw, listID))
	}
	return out, nil
}

// GetTask fetches a single task by id.
func (p *EWSProvider) GetTask(ctx context.Context, listID, id string) (providerdata.TaskItem, error) {
	listID = strings.TrimSpace(listID)
	id = strings.TrimSpace(id)
	if listID == "" || id == "" {
		return providerdata.TaskItem{}, fmt.Errorf("tasks: list id and task id are required")
	}
	raw, err := p.client.GetTaskItem(ctx, id)
	if err != nil {
		return providerdata.TaskItem{}, err
	}
	return taskItemFromEWS(raw, listID), nil
}

// CreateTask inserts a new task into the given list.
func (p *EWSProvider) CreateTask(ctx context.Context, listID string, t providerdata.TaskItem) (providerdata.TaskItem, error) {
	listID = strings.TrimSpace(listID)
	if listID == "" {
		return providerdata.TaskItem{}, fmt.Errorf("tasks: list id is required")
	}
	if strings.TrimSpace(t.Title) == "" {
		return providerdata.TaskItem{}, fmt.Errorf("tasks: title is required")
	}
	input := taskToEWS(t)
	itemID, _, err := p.client.CreateTaskItem(ctx, listID, input)
	if err != nil {
		return providerdata.TaskItem{}, fmt.Errorf("ews create task: %w", err)
	}
	raw, err := p.client.GetTaskItem(ctx, itemID)
	if err != nil {
		return providerdata.TaskItem{}, fmt.Errorf("ews get created task: %w", err)
	}
	return taskItemFromEWS(raw, listID), nil
}

// UpdateTask replaces the task payload using full-replace semantics.
func (p *EWSProvider) UpdateTask(ctx context.Context, listID string, t providerdata.TaskItem) (providerdata.TaskItem, error) {
	listID = strings.TrimSpace(listID)
	id := strings.TrimSpace(t.ID)
	if listID == "" || id == "" {
		return providerdata.TaskItem{}, fmt.Errorf("tasks: list id and task id are required")
	}
	// Fetch current item for change key.
	current, err := p.client.GetTaskItem(ctx, id)
	if err != nil {
		return providerdata.TaskItem{}, fmt.Errorf("ews get task for update: %w", err)
	}
	updates := taskUpdateFromProvider(t)
	_, err = p.client.UpdateTaskItem(ctx, id, current.ChangeKey, updates)
	if err != nil {
		return providerdata.TaskItem{}, fmt.Errorf("ews update task: %w", err)
	}
	raw, err := p.client.GetTaskItem(ctx, id)
	if err != nil {
		return providerdata.TaskItem{}, fmt.Errorf("ews get updated task: %w", err)
	}
	return taskItemFromEWS(raw, listID), nil
}

// DeleteTask permanently removes a task from the list.
func (p *EWSProvider) DeleteTask(ctx context.Context, listID, id string) error {
	listID = strings.TrimSpace(listID)
	id = strings.TrimSpace(id)
	if listID == "" || id == "" {
		return fmt.Errorf("tasks: list id and task id are required")
	}
	return p.client.DeleteTaskItem(ctx, id)
}

// CompleteTask marks a task completed.
func (p *EWSProvider) CompleteTask(ctx context.Context, listID, id string) error {
	now := time.Now().UTC()
	updates := ews.TaskUpdate{
		Status:       strPtr("Completed"),
		IsComplete:   boolPtr(true),
		CompleteDate: &now,
	}
	_, err := p.client.UpdateTaskItem(ctx, id, "", updates)
	if err != nil {
		return fmt.Errorf("ews complete task: %w", err)
	}
	return nil
}

// UncompleteTask reverses CompleteTask.
func (p *EWSProvider) UncompleteTask(ctx context.Context, listID, id string) error {
	updates := ews.TaskUpdate{
		Status:       strPtr("NotStarted"),
		IsComplete:   boolPtr(false),
		CompleteDate: timePtr(time.Time{}),
	}
	_, err := p.client.UpdateTaskItem(ctx, id, "", updates)
	if err != nil {
		return fmt.Errorf("ews uncomplete task: %w", err)
	}
	return nil
}

// CreateTaskList adds a new list (tasks subfolder) with the given display name.
func (p *EWSProvider) CreateTaskList(ctx context.Context, name string) (providerdata.TaskList, error) {
	title := strings.TrimSpace(name)
	if title == "" {
		return providerdata.TaskList{}, fmt.Errorf("tasks: list name is required")
	}
	folderID, _, err := p.client.CreateTasksFolder(ctx, ews.TasksFolderInput{
		ParentFolderID: p.rootTasksFolder,
		DisplayName:    title,
	})
	if err != nil {
		return providerdata.TaskList{}, fmt.Errorf("ews create tasks folder: %w", err)
	}
	return providerdata.TaskList{ID: folderID, Name: title}, nil
}

// DeleteTaskList removes a list (tasks subfolder). The primary folder is
// undeletable and will return an error.
func (p *EWSProvider) DeleteTaskList(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("tasks: list id is required")
	}
	if id == p.rootTasksFolder {
		return fmt.Errorf("tasks: cannot delete primary tasks folder")
	}
	return p.client.DeleteFolder(ctx, id)
}

// -- Mappers --

func taskItemFromEWS(t ews.Task, listID string) providerdata.TaskItem {
	out := providerdata.TaskItem{
		ID:          t.ID,
		ListID:      listID,
		Title:       t.Subject,
		Notes:       t.Body,
		Completed:   t.IsComplete,
		ProviderRef: t.ID,
	}
	if t.StartDate != nil && !t.StartDate.IsZero() {
		out.StartAt = t.StartDate
	}
	out.Priority = mapEWSStatus(t.Status)
	if t.DueDate != nil && !t.DueDate.IsZero() {
		out.Due = t.DueDate
	}
	if t.CompleteDate != nil && !t.CompleteDate.IsZero() {
		out.CompletedAt = t.CompleteDate
	}
	if strings.EqualFold(t.Status, "Completed") {
		out.Completed = true
	}
	return out
}

func taskToEWS(t providerdata.TaskItem) ews.TaskInput {
	input := ews.TaskInput{
		Subject: strings.TrimSpace(t.Title),
		Body:    t.Notes,
		Status:  "NotStarted",
	}
	if t.Completed {
		input.Status = "Completed"
	}
	if t.Due != nil && !t.Due.IsZero() {
		input.DueDate = t.Due
	}
	if p := mapPriorityToEWS(t.Priority); p != "" {
		input.Importance = p
	}
	return input
}

func taskUpdateFromProvider(t providerdata.TaskItem) ews.TaskUpdate {
	updates := ews.TaskUpdate{}
	if t.Title != "" {
		updates.Subject = &t.Title
	}
	if t.Notes != "" {
		updates.Body = &t.Notes
	}
	if t.Due != nil {
		updates.DueDate = t.Due
	}
	if t.Completed {
		updates.IsComplete = boolPtr(true)
		now := time.Now().UTC()
		updates.CompleteDate = &now
		updates.Status = strPtr("Completed")
	} else {
		updates.IsComplete = boolPtr(false)
		updates.Status = strPtr("NotStarted")
		updates.CompleteDate = timePtr(time.Time{})
	}
	if p := mapPriorityToEWS(t.Priority); p != "" {
		updates.Importance = &p
	}
	return updates
}

func mapEWSStatus(status string) string {
	switch strings.TrimSpace(strings.ToLower(status)) {
	case "completed":
		return "complete"
	case "inprogress":
		return "in-progress"
	case "waitingonothers":
		return "waiting"
	default:
		return "notstarted"
	}
}

func mapPriorityToEWS(priority string) string {
	switch strings.TrimSpace(strings.ToLower(priority)) {
	case "high":
		return "High"
	case "low":
		return "Low"
	default:
		return "Normal"
	}
}

// -- Helpers --

func strPtr(s string) *string        { return &s }
func boolPtr(b bool) *bool           { return &b }
func timePtr(t time.Time) *time.Time { return &t }
