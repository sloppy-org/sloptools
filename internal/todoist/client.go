package todoist

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

var (
	ErrTokenNotConfigured = errors.New("todoist token is not configured")
	ErrTaskIDRequired     = errors.New("task id is required")
	ErrTaskContentMissing = errors.New("task content is required")
)

type Client struct {
	baseURL     string
	moveBaseURL string
	token       string
	httpClient  *http.Client
	requestID   func() string
}

type Option func(*Client)

func WithBaseURL(raw string) Option {
	return func(c *Client) {
		c.baseURL = strings.TrimRight(strings.TrimSpace(raw), "/")
	}
}

func WithMoveBaseURL(raw string) Option {
	return func(c *Client) {
		c.moveBaseURL = strings.TrimRight(strings.TrimSpace(raw), "/")
	}
}

func WithHTTPClient(client *http.Client) Option {
	return func(c *Client) {
		c.httpClient = client
	}
}

func WithRequestIDGenerator(fn func() string) Option {
	return func(c *Client) {
		if fn != nil {
			c.requestID = fn
		}
	}
}

func NewClient(token string, opts ...Option) (*Client, error) {
	cleanToken := strings.TrimSpace(token)
	if cleanToken == "" {
		return nil, ErrTokenNotConfigured
	}
	client := &Client{
		baseURL:     defaultBaseURL,
		moveBaseURL: defaultMoveBaseURL,
		token:       cleanToken,
		httpClient:  http.DefaultClient,
		requestID:   newRequestID,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(client)
		}
	}
	if client.baseURL == "" {
		client.baseURL = defaultBaseURL
	}
	if client.moveBaseURL == "" {
		client.moveBaseURL = inferMoveBaseURL(client.baseURL)
	}
	if client.httpClient == nil {
		client.httpClient = http.DefaultClient
	}
	return client, nil
}

func NewClientFromEnv(label string, opts ...Option) (*Client, error) {
	value, ok := lookupTokenEnv(label)
	if !ok || strings.TrimSpace(value) == "" {
		return nil, fmt.Errorf("%w: %s", ErrTokenNotConfigured, TokenEnvVar(label))
	}
	return NewClient(value, opts...)
}

func lookupTokenEnv(label string) (string, bool) {
	return lookupEnv(TokenEnvVar(label))
}

var lookupEnv = os.LookupEnv

func inferMoveBaseURL(baseURL string) string {
	clean := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if clean == "" {
		return defaultMoveBaseURL
	}
	if strings.HasSuffix(clean, "/rest/v2") {
		return strings.TrimSuffix(clean, "/rest/v2") + "/api/v1"
	}
	return defaultMoveBaseURL
}

func (c *Client) ListProjects(ctx context.Context) ([]Project, error) {
	return listPaginated[Project](ctx, c, c.baseURL+"/projects", url.Values{})
}

func (c *Client) ListTasks(ctx context.Context, opts ListTasksOptions) ([]Task, error) {
	query, err := listTaskQuery(opts)
	if err != nil {
		return nil, err
	}
	return listPaginated[Task](ctx, c, c.baseURL+"/tasks", query)
}

func (c *Client) GetTask(ctx context.Context, id string) (TaskDetail, error) {
	taskID := strings.TrimSpace(id)
	if taskID == "" {
		return TaskDetail{}, ErrTaskIDRequired
	}
	var task Task
	if err := c.doJSON(ctx, http.MethodGet, c.baseURL+"/tasks/"+url.PathEscape(taskID), nil, nil, &task, http.StatusOK); err != nil {
		return TaskDetail{}, err
	}
	comments, err := c.listComments(ctx, taskID)
	if err != nil {
		return TaskDetail{}, err
	}
	return TaskDetail{Task: task, Comments: comments}, nil
}

func (c *Client) CompleteTask(ctx context.Context, id string) error {
	taskID := strings.TrimSpace(id)
	if taskID == "" {
		return ErrTaskIDRequired
	}
	return c.postNoContent(ctx, c.baseURL+"/tasks/"+url.PathEscape(taskID)+"/close")
}

func (c *Client) ReopenTask(ctx context.Context, id string) error {
	taskID := strings.TrimSpace(id)
	if taskID == "" {
		return ErrTaskIDRequired
	}
	return c.postNoContent(ctx, c.baseURL+"/tasks/"+url.PathEscape(taskID)+"/reopen")
}

func (c *Client) CreateTask(ctx context.Context, req CreateTaskRequest) (Task, error) {
	body, err := createTaskBody(req)
	if err != nil {
		return Task{}, err
	}
	var task Task
	if err := c.doJSON(ctx, http.MethodPost, c.baseURL+"/tasks", nil, body, &task, http.StatusOK); err != nil {
		return Task{}, err
	}
	return task, nil
}

func (c *Client) UpdateTask(ctx context.Context, id string, req UpdateTaskRequest) (Task, error) {
	taskID := strings.TrimSpace(id)
	if taskID == "" {
		return Task{}, ErrTaskIDRequired
	}
	if err := validateMoveRequest(req.ProjectID, req.SectionID, req.ParentID); err != nil {
		return Task{}, err
	}
	body, err := updateTaskBody(req)
	if err != nil {
		return Task{}, err
	}
	if len(body) > 0 {
		var updated Task
		if err := c.doJSON(ctx, http.MethodPost, c.baseURL+"/tasks/"+url.PathEscape(taskID), nil, body, &updated, http.StatusOK); err != nil {
			return Task{}, err
		}
	}
	if hasMoveRequest(req.ProjectID, req.SectionID, req.ParentID) {
		// Todoist's current REST v2 docs no longer expose task moves, so keep the
		// client on v2 for CRUD and use the documented move endpoint for relocations.
		moveBody := moveTaskBody(req.ProjectID, req.SectionID, req.ParentID)
		if err := c.doJSON(ctx, http.MethodPost, c.moveBaseURL+"/tasks/"+url.PathEscape(taskID)+"/move", nil, moveBody, nil, http.StatusOK); err != nil {
			return Task{}, err
		}
	}
	var task Task
	if err := c.doJSON(ctx, http.MethodGet, c.baseURL+"/tasks/"+url.PathEscape(taskID), nil, nil, &task, http.StatusOK); err != nil {
		return Task{}, err
	}
	return task, nil
}

func (c *Client) listComments(ctx context.Context, taskID string) ([]Comment, error) {
	query := url.Values{}
	query.Set("task_id", taskID)
	var comments []Comment
	if err := c.doJSON(ctx, http.MethodGet, c.baseURL+"/comments", query, nil, &comments, http.StatusOK); err != nil {
		return nil, err
	}
	return comments, nil
}

func (c *Client) postNoContent(ctx context.Context, endpoint string) error {
	return c.doJSON(ctx, http.MethodPost, endpoint, nil, map[string]any{}, nil, http.StatusNoContent)
}

func (c *Client) doJSON(ctx context.Context, method, endpoint string, query url.Values, body any, out any, expect int) error {
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := c.newRequest(ctx, method, endpoint, query, body)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != expect {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &APIError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(payload))}
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1024*1024)).Decode(out); err != nil {
		return err
	}
	return nil
}

func (c *Client) newRequest(ctx context.Context, method, endpoint string, query url.Values, body any) (*http.Request, error) {
	cleanMethod := strings.TrimSpace(method)
	if cleanMethod == "" {
		cleanMethod = http.MethodGet
	}
	u, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return nil, err
	}
	if len(query) > 0 {
		u.RawQuery = query.Encode()
	}
	var bodyReader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, cleanMethod, u.String(), bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if cleanMethod != http.MethodGet && cleanMethod != http.MethodDelete && c.requestID != nil {
		req.Header.Set("X-Request-Id", c.requestID())
	}
	return req, nil
}

func listTaskQuery(opts ListTasksOptions) (url.Values, error) {
	if strings.TrimSpace(opts.Filter) != "" && strings.TrimSpace(opts.DueFilter) != "" {
		return nil, errors.New("filter and due filter cannot be combined")
	}
	query := url.Values{}
	if v := strings.TrimSpace(opts.ProjectID); v != "" {
		query.Set("project_id", v)
	}
	if v := strings.TrimSpace(opts.SectionID); v != "" {
		query.Set("section_id", v)
	}
	if v := strings.TrimSpace(opts.Label); v != "" {
		query.Set("label", v)
	}
	filter := strings.TrimSpace(opts.Filter)
	if filter == "" {
		filter = strings.TrimSpace(opts.DueFilter)
	}
	if filter != "" {
		query.Set("filter", filter)
	}
	if v := strings.TrimSpace(opts.Lang); v != "" {
		query.Set("lang", v)
	}
	if len(opts.IDs) > 0 {
		ids := make([]string, 0, len(opts.IDs))
		for _, id := range opts.IDs {
			if clean := strings.TrimSpace(id); clean != "" {
				ids = append(ids, clean)
			}
		}
		if len(ids) > 0 {
			query.Set("ids", strings.Join(ids, ","))
		}
	}
	return query, nil
}

func createTaskBody(req CreateTaskRequest) (map[string]any, error) {
	content := strings.TrimSpace(req.Content)
	if content == "" {
		return nil, ErrTaskContentMissing
	}
	if err := validateDueValues(req.DueString, req.DueDate, req.DueDateTime); err != nil {
		return nil, err
	}
	body := map[string]any{"content": content}
	addOptionalString(body, "description", req.Description)
	addOptionalString(body, "project_id", req.ProjectID)
	addOptionalString(body, "section_id", req.SectionID)
	addOptionalString(body, "parent_id", req.ParentID)
	addOptionalStrings(body, "labels", req.Labels)
	addOptionalInt(body, "order", req.Order)
	addOptionalInt(body, "priority", req.Priority)
	addOptionalString(body, "due_string", req.DueString)
	addOptionalString(body, "due_date", req.DueDate)
	addOptionalString(body, "due_datetime", req.DueDateTime)
	addOptionalString(body, "due_lang", req.DueLang)
	addOptionalString(body, "assignee_id", req.AssigneeID)
	addOptionalString(body, "deadline_date", req.DeadlineDate)
	if err := addOptionalDuration(body, req.Duration); err != nil {
		return nil, err
	}
	return body, nil
}

func updateTaskBody(req UpdateTaskRequest) (map[string]any, error) {
	if err := validateDuePointers(req.DueString, req.DueDate, req.DueDateTime); err != nil {
		return nil, err
	}
	body := map[string]any{}
	addPointerString(body, "content", req.Content)
	addPointerString(body, "description", req.Description)
	addPointerString(body, "due_string", req.DueString)
	addPointerString(body, "due_date", req.DueDate)
	addPointerString(body, "due_datetime", req.DueDateTime)
	addPointerString(body, "due_lang", req.DueLang)
	addPointerString(body, "assignee_id", req.AssigneeID)
	addPointerString(body, "deadline_date", req.DeadlineDate)
	if req.Priority != nil && *req.Priority > 0 {
		body["priority"] = *req.Priority
	}
	if req.Labels != nil {
		body["labels"] = append([]string(nil), (*req.Labels)...)
	}
	if err := addOptionalDuration(body, req.Duration); err != nil {
		return nil, err
	}
	return body, nil
}

func moveTaskBody(projectID, sectionID, parentID *string) map[string]any {
	body := map[string]any{}
	addPointerString(body, "project_id", projectID)
	addPointerString(body, "section_id", sectionID)
	addPointerString(body, "parent_id", parentID)
	return body
}

func addOptionalString(body map[string]any, key, value string) {
	if clean := strings.TrimSpace(value); clean != "" {
		body[key] = clean
	}
}

func addPointerString(body map[string]any, key string, value *string) {
	if value != nil && strings.TrimSpace(*value) != "" {
		body[key] = strings.TrimSpace(*value)
	}
}

func addOptionalStrings(body map[string]any, key string, values []string) {
	if len(values) == 0 {
		return
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if clean := strings.TrimSpace(value); clean != "" {
			out = append(out, clean)
		}
	}
	if len(out) > 0 {
		body[key] = out
	}
}

func addOptionalInt(body map[string]any, key string, value int) {
	if value > 0 {
		body[key] = value
	}
}

func addOptionalDuration(body map[string]any, duration *Duration) error {
	if duration == nil {
		return nil
	}
	if duration.Amount <= 0 {
		return errors.New("duration amount must be positive")
	}
	unit := strings.TrimSpace(duration.Unit)
	if unit != "minute" && unit != "day" {
		return errors.New("duration unit must be minute or day")
	}
	body["duration"] = duration.Amount
	body["duration_unit"] = unit
	return nil
}

func validateDueValues(dueString, dueDate, dueDateTime string) error {
	values := 0
	for _, value := range []string{dueString, dueDate, dueDateTime} {
		if strings.TrimSpace(value) != "" {
			values++
		}
	}
	if values > 1 {
		return errors.New("only one due field may be set")
	}
	return nil
}

func validateDuePointers(dueString, dueDate, dueDateTime *string) error {
	values := 0
	for _, value := range []*string{dueString, dueDate, dueDateTime} {
		if value != nil && strings.TrimSpace(*value) != "" {
			values++
		}
	}
	if values > 1 {
		return errors.New("only one due field may be set")
	}
	return nil
}

func hasMoveRequest(projectID, sectionID, parentID *string) bool {
	for _, value := range []*string{projectID, sectionID, parentID} {
		if value != nil && strings.TrimSpace(*value) != "" {
			return true
		}
	}
	return false
}

func validateMoveRequest(projectID, sectionID, parentID *string) error {
	values := 0
	for _, value := range []*string{projectID, sectionID, parentID} {
		if value != nil && strings.TrimSpace(*value) != "" {
			values++
		}
	}
	if values > 1 {
		return errors.New("only one of project_id, section_id, or parent_id may be set")
	}
	return nil
}

func newRequestID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "sloptools-todoist-request"
	}
	return hex.EncodeToString(buf[:4]) + "-" +
		hex.EncodeToString(buf[4:6]) + "-" +
		hex.EncodeToString(buf[6:8]) + "-" +
		hex.EncodeToString(buf[8:10]) + "-" +
		hex.EncodeToString(buf[10:])
}
