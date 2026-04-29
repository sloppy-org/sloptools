package todoist

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

func (c *Client) ListSections(ctx context.Context, projectID string) ([]Section, error) {
	query := url.Values{}
	if clean := strings.TrimSpace(projectID); clean != "" {
		query.Set("project_id", clean)
	}
	return listPaginated[Section](ctx, c, c.baseURL+"/sections", query)
}

func (c *Client) ListLabels(ctx context.Context) ([]Label, error) {
	return listPaginated[Label](ctx, c, c.baseURL+"/labels", url.Values{})
}

func (c *Client) CreateProject(ctx context.Context, name string) (Project, error) {
	title := strings.TrimSpace(name)
	if title == "" {
		return Project{}, fmt.Errorf("project name is required")
	}
	var project Project
	if err := c.doJSON(ctx, http.MethodPost, c.baseURL+"/projects", nil, map[string]any{"name": title}, &project, http.StatusOK); err != nil {
		return Project{}, err
	}
	return project, nil
}

func (c *Client) DeleteProject(ctx context.Context, id string) error {
	projectID := strings.TrimSpace(id)
	if projectID == "" {
		return ErrTaskIDRequired
	}
	return c.doJSON(ctx, http.MethodDelete, c.baseURL+"/projects/"+url.PathEscape(projectID), nil, nil, nil, http.StatusNoContent)
}

func (c *Client) DeleteTask(ctx context.Context, id string) error {
	taskID := strings.TrimSpace(id)
	if taskID == "" {
		return ErrTaskIDRequired
	}
	return c.doJSON(ctx, http.MethodDelete, c.baseURL+"/tasks/"+url.PathEscape(taskID), nil, nil, nil, http.StatusNoContent)
}

func (c *Client) ListComments(ctx context.Context, taskID string) ([]Comment, error) {
	return c.listComments(ctx, taskID)
}
