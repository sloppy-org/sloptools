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

const todoistProviderName = "todoist"

type TodoistProvider struct {
	client *todoist.Client
}

var _ Provider = (*TodoistProvider)(nil)

func NewTodoistProvider(client *todoist.Client) *TodoistProvider {
	return &TodoistProvider{client: client}
}

func (p *TodoistProvider) ProviderName() string { return todoistProviderName }

func (p *TodoistProvider) Close() error { return nil }

func (p *TodoistProvider) ListTaskLists(ctx context.Context) ([]providerdata.TaskList, error) {
	if p == nil || p.client == nil {
		return nil, fmt.Errorf("todoist client is not configured")
	}
	projects, err := p.client.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]providerdata.TaskList, 0, len(projects))
	for _, project := range projects {
		out = append(out, providerdata.TaskList{
			ID:      project.ID,
			Name:    project.Name,
			Primary: project.IsInboxProject || project.InboxProject || project.IsTeamInbox,
		})
	}
	return out, nil
}

func (p *TodoistProvider) ListTasks(ctx context.Context, listID string) ([]providerdata.TaskItem, error) {
	if p == nil || p.client == nil {
		return nil, fmt.Errorf("todoist client is not configured")
	}
	items, err := p.client.ListTasks(ctx, todoist.ListTasksOptions{ProjectID: strings.TrimSpace(listID)})
	if err != nil {
		return nil, err
	}
	out := make([]providerdata.TaskItem, 0, len(items))
	for _, item := range items {
		out = append(out, taskItemFromTodoist(item, listID))
	}
	return out, nil
}

func (p *TodoistProvider) GetTask(ctx context.Context, listID, id string) (providerdata.TaskItem, error) {
	if p == nil || p.client == nil {
		return providerdata.TaskItem{}, fmt.Errorf("todoist client is not configured")
	}
	detail, err := p.client.GetTask(ctx, id)
	if err != nil {
		return providerdata.TaskItem{}, err
	}
	return taskItemFromTodoist(detail.Task, listID), nil
}

func taskItemFromTodoist(item todoist.Task, listID string) providerdata.TaskItem {
	start := todoistDue(item)
	due := todoistDeadline(item)
	projectID := strings.TrimSpace(listID)
	if projectID == "" && item.ProjectID != nil {
		projectID = *item.ProjectID
	}
	completedAt := todoistTime(item.CompletedAt)
	return providerdata.TaskItem{
		ID:          item.ID,
		ListID:      projectID,
		Title:       item.Content,
		Notes:       item.Description,
		StartAt:     start,
		Due:         due,
		CompletedAt: completedAt,
		Completed:   item.IsCompleted || item.Checked,
		Priority:    strconv.Itoa(item.Priority),
		ProviderRef: item.URL,
	}
}

func todoistDue(item todoist.Task) *time.Time {
	if item.Due == nil {
		return nil
	}
	for _, raw := range []string{ptrString(item.Due.DateTime), item.Due.Date} {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		if t, err := time.Parse(time.RFC3339, raw); err == nil {
			return &t
		}
		if t, err := time.Parse("2006-01-02", raw); err == nil {
			return &t
		}
	}
	return nil
}

func todoistDeadline(item todoist.Task) *time.Time {
	if item.Deadline == nil {
		return nil
	}
	return todoistDate(item.Deadline.Date)
}

func todoistTime(value *string) *time.Time {
	if value == nil {
		return nil
	}
	if t, err := time.Parse(time.RFC3339, strings.TrimSpace(*value)); err == nil {
		return &t
	}
	return nil
}

func todoistDate(value string) *time.Time {
	clean := strings.TrimSpace(value)
	if clean == "" {
		return nil
	}
	if t, err := time.Parse("2006-01-02", clean); err == nil {
		return &t
	}
	return nil
}

func ptrString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
