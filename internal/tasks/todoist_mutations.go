package tasks

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/todoist"
)

var (
	_ Mutator     = (*TodoistProvider)(nil)
	_ Completer   = (*TodoistProvider)(nil)
	_ ListManager = (*TodoistProvider)(nil)
)

func (p *TodoistProvider) CreateTask(ctx context.Context, listID string, t providerdata.TaskItem) (providerdata.TaskItem, error) {
	if err := p.ensureClient(); err != nil {
		return providerdata.TaskItem{}, err
	}
	req, err := todoistCreateRequest(listID, t)
	if err != nil {
		return providerdata.TaskItem{}, err
	}
	created, err := p.client.CreateTask(ctx, req)
	if err != nil {
		return providerdata.TaskItem{}, err
	}
	return taskItemFromTodoist(created, listID, nil), nil
}

func (p *TodoistProvider) UpdateTask(ctx context.Context, listID string, t providerdata.TaskItem) (providerdata.TaskItem, error) {
	if err := p.ensureClient(); err != nil {
		return providerdata.TaskItem{}, err
	}
	if strings.TrimSpace(t.ListID) == "" {
		t.ListID = listID
	}
	req, err := todoistUpdateRequest(t)
	if err != nil {
		return providerdata.TaskItem{}, err
	}
	updated, err := p.client.UpdateTask(ctx, strings.TrimSpace(t.ID), req)
	if err != nil {
		return providerdata.TaskItem{}, err
	}
	return taskItemFromTodoist(updated, listID, nil), nil
}

func (p *TodoistProvider) DeleteTask(ctx context.Context, listID, id string) error {
	if err := p.ensureClient(); err != nil {
		return err
	}
	return p.client.DeleteTask(ctx, id)
}

func (p *TodoistProvider) CompleteTask(ctx context.Context, listID, id string) error {
	if err := p.ensureClient(); err != nil {
		return err
	}
	return p.client.CompleteTask(ctx, id)
}

func (p *TodoistProvider) UncompleteTask(ctx context.Context, listID, id string) error {
	if err := p.ensureClient(); err != nil {
		return err
	}
	return p.client.ReopenTask(ctx, id)
}

func (p *TodoistProvider) CreateTaskList(ctx context.Context, name string) (providerdata.TaskList, error) {
	if err := p.ensureClient(); err != nil {
		return providerdata.TaskList{}, err
	}
	project, err := p.client.CreateProject(ctx, name)
	if err != nil {
		return providerdata.TaskList{}, err
	}
	return providerdata.TaskList{
		ID:          project.ID,
		Name:        project.Name,
		Color:       project.Color,
		Order:       project.Order,
		ParentID:    project.ParentID,
		IsShared:    project.IsShared,
		IsFavorite:  project.IsFavorite,
		ViewStyle:   project.ViewStyle,
		ProviderURL: project.URL,
	}, nil
}

func (p *TodoistProvider) DeleteTaskList(ctx context.Context, id string) error {
	if err := p.ensureClient(); err != nil {
		return err
	}
	return p.client.DeleteProject(ctx, id)
}

func (p *TodoistProvider) ensureClient() error {
	if p == nil || p.client == nil {
		return fmt.Errorf("todoist client is not configured")
	}
	return nil
}

func todoistCreateRequest(listID string, t providerdata.TaskItem) (todoist.CreateTaskRequest, error) {
	req := todoist.CreateTaskRequest{
		Content:      strings.TrimSpace(t.Title),
		Description:  todoistTaskDescription(t),
		ProjectID:    strings.TrimSpace(listID),
		SectionID:    strings.TrimSpace(t.SectionID),
		ParentID:     strings.TrimSpace(t.ParentID),
		Labels:       append([]string(nil), t.Labels...),
		Priority:     todoistPriority(t.Priority),
		AssigneeID:   strings.TrimSpace(t.AssigneeID),
		DeadlineDate: todoistDeadlineDate(t.Due),
	}
	if dueDate, dueDateTime, dueLang := todoistDueFields(t.StartAt); dueDate != "" || dueDateTime != "" {
		req.DueDate = dueDate
		req.DueDateTime = dueDateTime
		req.DueLang = dueLang
	}
	return req, nil
}

func todoistUpdateRequest(t providerdata.TaskItem) (todoist.UpdateTaskRequest, error) {
	req := todoist.UpdateTaskRequest{
		Content:      stringPtr(strings.TrimSpace(t.Title)),
		Description:  stringPtr(todoistTaskDescription(t)),
		Labels:       stringSlicePtr(t.Labels),
		Priority:     intPtr(todoistPriority(t.Priority)),
		AssigneeID:   stringPtr(strings.TrimSpace(t.AssigneeID)),
		DeadlineDate: stringPtr(todoistDeadlineDate(t.Due)),
	}
	if dueDate, dueDateTime, dueLang := todoistDueFields(t.StartAt); dueDate != "" || dueDateTime != "" {
		req.DueDate = stringPtr(dueDate)
		req.DueDateTime = stringPtr(dueDateTime)
		req.DueLang = stringPtr(dueLang)
	}
	if sectionID := strings.TrimSpace(t.SectionID); sectionID != "" {
		req.SectionID = stringPtr(sectionID)
	} else if parentID := strings.TrimSpace(t.ParentID); parentID != "" {
		req.ParentID = stringPtr(parentID)
	} else if projectID := strings.TrimSpace(t.ListID); projectID != "" {
		req.ProjectID = stringPtr(projectID)
	}
	return req, nil
}

func todoistTaskDescription(t providerdata.TaskItem) string {
	if clean := strings.TrimSpace(t.Description); clean != "" {
		return clean
	}
	return strings.TrimSpace(t.Notes)
}

func todoistPriority(raw string) int {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "p1", "high":
		return 1
	case "2", "p2":
		return 2
	case "3", "p3":
		return 3
	case "4", "p4", "low":
		return 4
	default:
		if value, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil && value >= 1 && value <= 4 {
			return value
		}
		return 0
	}
}

func todoistDueFields(startAt *time.Time) (string, string, string) {
	if startAt == nil || startAt.IsZero() {
		return "", "", ""
	}
	utc := startAt.UTC()
	if utc.Hour() == 0 && utc.Minute() == 0 && utc.Second() == 0 && utc.Nanosecond() == 0 {
		return utc.Format("2006-01-02"), "", "en"
	}
	return "", utc.Format(time.RFC3339), "en"
}

func todoistDeadlineDate(due *time.Time) string {
	if due == nil || due.IsZero() {
		return ""
	}
	return due.UTC().Format("2006-01-02")
}

func stringPtr(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	clean := strings.TrimSpace(value)
	return &clean
}

func stringSlicePtr(values []string) *[]string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if clean := strings.TrimSpace(value); clean != "" {
			out = append(out, clean)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return &out
}

func intPtr(value int) *int {
	if value <= 0 {
		return nil
	}
	return &value
}
