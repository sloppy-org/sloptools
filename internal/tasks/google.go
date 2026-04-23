package tasks

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/googleauth"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"google.golang.org/api/option"
	gtasks "google.golang.org/api/tasks/v1"
)

const googleProviderName = "google_tasks"

// GoogleProvider implements Provider, Mutator, Completer, and ListManager
// against the Google Tasks v1 API using a shared googleauth.Session.
//
// The svcFn hook lets tests substitute an HTTP-backed stub service without
// forcing every production call site to thread mock plumbing through the
// constructor.
type GoogleProvider struct {
	auth    *googleauth.Session
	options []option.ClientOption
	svcFn   func(ctx context.Context) (*gtasks.Service, error)
	nowFn   func() time.Time
}

// Compile-time contract checks; the build fails fast if the adapter drifts
// from any of the task interfaces it claims to satisfy.
var (
	_ Provider    = (*GoogleProvider)(nil)
	_ Mutator     = (*GoogleProvider)(nil)
	_ Completer   = (*GoogleProvider)(nil)
	_ ListManager = (*GoogleProvider)(nil)
)

// NewGoogleProvider wraps an authenticated googleauth.Session. The
// ScopeTasks scope must already be part of the session's granted scopes;
// googleauth.DefaultScopes includes it.
func NewGoogleProvider(auth *googleauth.Session, opts ...option.ClientOption) *GoogleProvider {
	return &GoogleProvider{
		auth:    auth,
		options: opts,
		nowFn:   time.Now,
	}
}

// ProviderName identifies the backend in logs and error messages.
func (p *GoogleProvider) ProviderName() string { return googleProviderName }

// Close is a no-op: the shared googleauth.Session is owned by the
// registry, so the provider must not tear it down.
func (p *GoogleProvider) Close() error { return nil }

func (p *GoogleProvider) service(ctx context.Context) (*gtasks.Service, error) {
	if p == nil {
		return nil, fmt.Errorf("tasks: google provider is nil")
	}
	if p.svcFn != nil {
		return p.svcFn(ctx)
	}
	if p.auth == nil {
		return nil, fmt.Errorf("tasks: google auth session is not configured")
	}
	tokenSource, err := p.auth.TokenSource(ctx)
	if err != nil {
		return nil, err
	}
	opts := append([]option.ClientOption{option.WithTokenSource(tokenSource)}, p.options...)
	svc, err := gtasks.NewService(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("create google tasks service: %w", err)
	}
	return svc, nil
}

// ListTaskLists returns every task list visible to the authenticated user.
// The first list returned by the API is marked primary to match Google's
// default-list convention.
func (p *GoogleProvider) ListTaskLists(ctx context.Context) ([]providerdata.TaskList, error) {
	svc, err := p.service(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := svc.Tasklists.List().Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("list google task lists: %w", err)
	}
	lists := make([]providerdata.TaskList, 0, len(resp.Items))
	for i, item := range resp.Items {
		if item == nil {
			continue
		}
		lists = append(lists, providerdata.TaskList{
			ID:      item.Id,
			Name:    item.Title,
			Primary: i == 0,
		})
	}
	return lists, nil
}

// ListTasks returns all tasks in a list including completed items so that
// callers can render the full state without a second roundtrip.
func (p *GoogleProvider) ListTasks(ctx context.Context, listID string) ([]providerdata.TaskItem, error) {
	id := strings.TrimSpace(listID)
	if id == "" {
		return nil, fmt.Errorf("tasks: list id is required")
	}
	svc, err := p.service(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := svc.Tasks.List(id).ShowCompleted(true).ShowHidden(true).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("list google tasks: %w", err)
	}
	items := make([]providerdata.TaskItem, 0, len(resp.Items))
	for _, item := range resp.Items {
		if item == nil {
			continue
		}
		items = append(items, taskItemFromGoogle(item, id))
	}
	return items, nil
}

// GetTask fetches a single task by id.
func (p *GoogleProvider) GetTask(ctx context.Context, listID, id string) (providerdata.TaskItem, error) {
	listID = strings.TrimSpace(listID)
	id = strings.TrimSpace(id)
	if listID == "" || id == "" {
		return providerdata.TaskItem{}, fmt.Errorf("tasks: list id and task id are required")
	}
	svc, err := p.service(ctx)
	if err != nil {
		return providerdata.TaskItem{}, err
	}
	task, err := svc.Tasks.Get(listID, id).Context(ctx).Do()
	if err != nil {
		return providerdata.TaskItem{}, fmt.Errorf("get google task: %w", err)
	}
	return taskItemFromGoogle(task, listID), nil
}

// CreateTask inserts a new task into the given list.
func (p *GoogleProvider) CreateTask(ctx context.Context, listID string, t providerdata.TaskItem) (providerdata.TaskItem, error) {
	listID = strings.TrimSpace(listID)
	if listID == "" {
		return providerdata.TaskItem{}, fmt.Errorf("tasks: list id is required")
	}
	if strings.TrimSpace(t.Title) == "" {
		return providerdata.TaskItem{}, fmt.Errorf("tasks: title is required")
	}
	svc, err := p.service(ctx)
	if err != nil {
		return providerdata.TaskItem{}, err
	}
	created, err := svc.Tasks.Insert(listID, taskToGoogle(t)).Context(ctx).Do()
	if err != nil {
		return providerdata.TaskItem{}, fmt.Errorf("create google task: %w", err)
	}
	return taskItemFromGoogle(created, listID), nil
}

// UpdateTask replaces the task payload using the full update semantics of
// Google Tasks; callers should pass the whole task they want stored.
func (p *GoogleProvider) UpdateTask(ctx context.Context, listID string, t providerdata.TaskItem) (providerdata.TaskItem, error) {
	listID = strings.TrimSpace(listID)
	id := strings.TrimSpace(t.ID)
	if listID == "" || id == "" {
		return providerdata.TaskItem{}, fmt.Errorf("tasks: list id and task id are required")
	}
	svc, err := p.service(ctx)
	if err != nil {
		return providerdata.TaskItem{}, err
	}
	payload := taskToGoogle(t)
	payload.Id = id
	updated, err := svc.Tasks.Update(listID, id, payload).Context(ctx).Do()
	if err != nil {
		return providerdata.TaskItem{}, fmt.Errorf("update google task: %w", err)
	}
	return taskItemFromGoogle(updated, listID), nil
}

// DeleteTask permanently removes a task from the list.
func (p *GoogleProvider) DeleteTask(ctx context.Context, listID, id string) error {
	listID = strings.TrimSpace(listID)
	id = strings.TrimSpace(id)
	if listID == "" || id == "" {
		return fmt.Errorf("tasks: list id and task id are required")
	}
	svc, err := p.service(ctx)
	if err != nil {
		return err
	}
	if err := svc.Tasks.Delete(listID, id).Context(ctx).Do(); err != nil {
		return fmt.Errorf("delete google task: %w", err)
	}
	return nil
}

// CompleteTask marks a task completed. Google requires both status =
// "completed" and a completed timestamp to avoid surfacing the task in
// subsequent needsAction queries.
func (p *GoogleProvider) CompleteTask(ctx context.Context, listID, id string) error {
	return p.setCompletion(ctx, listID, id, true)
}

// UncompleteTask reverses CompleteTask by clearing both the status and the
// completed timestamp.
func (p *GoogleProvider) UncompleteTask(ctx context.Context, listID, id string) error {
	return p.setCompletion(ctx, listID, id, false)
}

func (p *GoogleProvider) setCompletion(ctx context.Context, listID, id string, completed bool) error {
	listID = strings.TrimSpace(listID)
	id = strings.TrimSpace(id)
	if listID == "" || id == "" {
		return fmt.Errorf("tasks: list id and task id are required")
	}
	svc, err := p.service(ctx)
	if err != nil {
		return err
	}
	existing, err := svc.Tasks.Get(listID, id).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("get google task for completion toggle: %w", err)
	}
	if completed {
		existing.Status = "completed"
		stamp := p.now().UTC().Format(time.RFC3339)
		existing.Completed = &stamp
	} else {
		existing.Status = "needsAction"
		existing.Completed = nil
		// NullFields marks "completed" explicitly so the Google client sends
		// JSON null rather than omitting the field, which is required to
		// clear a previously set completion timestamp.
		existing.NullFields = append(existing.NullFields, "Completed")
	}
	if _, err := svc.Tasks.Update(listID, id, existing).Context(ctx).Do(); err != nil {
		return fmt.Errorf("toggle google task completion: %w", err)
	}
	return nil
}

// CreateTaskList adds a new list with the given display name.
func (p *GoogleProvider) CreateTaskList(ctx context.Context, name string) (providerdata.TaskList, error) {
	title := strings.TrimSpace(name)
	if title == "" {
		return providerdata.TaskList{}, fmt.Errorf("tasks: list name is required")
	}
	svc, err := p.service(ctx)
	if err != nil {
		return providerdata.TaskList{}, err
	}
	created, err := svc.Tasklists.Insert(&gtasks.TaskList{Title: title}).Context(ctx).Do()
	if err != nil {
		return providerdata.TaskList{}, fmt.Errorf("create google task list: %w", err)
	}
	return providerdata.TaskList{ID: created.Id, Name: created.Title}, nil
}

// DeleteTaskList removes a list and all of its tasks.
func (p *GoogleProvider) DeleteTaskList(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("tasks: list id is required")
	}
	svc, err := p.service(ctx)
	if err != nil {
		return err
	}
	if err := svc.Tasklists.Delete(id).Context(ctx).Do(); err != nil {
		return fmt.Errorf("delete google task list: %w", err)
	}
	return nil
}

func (p *GoogleProvider) now() time.Time {
	if p == nil || p.nowFn == nil {
		return time.Now()
	}
	return p.nowFn()
}

func taskItemFromGoogle(item *gtasks.Task, listID string) providerdata.TaskItem {
	if item == nil {
		return providerdata.TaskItem{ListID: listID}
	}
	out := providerdata.TaskItem{
		ID:          item.Id,
		ListID:      listID,
		Title:       item.Title,
		Notes:       item.Notes,
		Completed:   item.Status == "completed",
		ProviderRef: item.SelfLink,
	}
	if due, ok := parseTaskTimestamp(item.Due); ok {
		out.Due = &due
	}
	if item.Completed != nil {
		if completed, ok := parseTaskTimestamp(*item.Completed); ok {
			out.CompletedAt = &completed
		}
	}
	return out
}

func taskToGoogle(t providerdata.TaskItem) *gtasks.Task {
	payload := &gtasks.Task{
		Title:  strings.TrimSpace(t.Title),
		Notes:  t.Notes,
		Status: "needsAction",
	}
	if t.Completed {
		payload.Status = "completed"
	}
	if t.Due != nil && !t.Due.IsZero() {
		payload.Due = t.Due.UTC().Format(time.RFC3339)
	}
	if t.CompletedAt != nil && !t.CompletedAt.IsZero() {
		stamp := t.CompletedAt.UTC().Format(time.RFC3339)
		payload.Completed = &stamp
	}
	return payload
}

func parseTaskTimestamp(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, true
	}
	if t, err := time.Parse("2006-01-02", raw); err == nil {
		return t, true
	}
	return time.Time{}, false
}
