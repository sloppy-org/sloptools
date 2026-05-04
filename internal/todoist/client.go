package todoist

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
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
	return func(c *Client) { c.baseURL = strings.TrimRight(strings.TrimSpace(raw), "/") }
}

func WithMoveBaseURL(raw string) Option {
	return func(c *Client) { c.moveBaseURL = strings.TrimRight(strings.TrimSpace(raw), "/") }
}

func WithHTTPClient(client *http.Client) Option {
	return func(c *Client) { c.httpClient = client }
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
	client := &Client{baseURL: defaultBaseURL, moveBaseURL: defaultMoveBaseURL, token: cleanToken, httpClient: http.DefaultClient, requestID: newRequestID}
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
	return c.taskAction(ctx, id, "close")
}
func (c *Client) ReopenTask(ctx context.Context, id string) error {
	return c.taskAction(ctx, id, "reopen")
}
func (c *Client) taskAction(ctx context.Context, id, action string) error {
	taskID := strings.TrimSpace(id)
	if taskID == "" {
		return ErrTaskIDRequired
	}
	return c.postNoContent(ctx, c.baseURL+"/tasks/"+url.PathEscape(taskID)+"/"+action)
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
	cleanMethod := strings.ToUpper(strings.TrimSpace(method))
	for attempt := 0; ; attempt++ {
		req, err := c.newRequest(ctx, cleanMethod, endpoint, query, body)
		if err != nil {
			return err
		}
		resp, err := c.httpClient.Do(req)
		if err != nil {
			if shouldRetryTodoistRequest(cleanMethod, attempt, 0) && todoistRetrySleep(ctx, nil, attempt) == nil {
				continue
			}
			return err
		}

		if resp.StatusCode != expect {
			payload, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			_ = resp.Body.Close()
			if shouldRetryTodoistRequest(cleanMethod, attempt, resp.StatusCode) && todoistRetrySleep(ctx, resp, attempt) == nil {
				continue
			}
			return &APIError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(payload))}
		}
		defer resp.Body.Close()
		if out == nil {
			return nil
		}
		if err := json.NewDecoder(io.LimitReader(resp.Body, 1024*1024)).Decode(out); err != nil {
			return err
		}
		return nil
	}
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
	if c.requestID != nil && cleanMethod != http.MethodGet && cleanMethod != http.MethodDelete {
		req.Header.Set("X-Request-Id", c.requestID())
	}
	return req, nil
}

func listTaskQuery(opts ListTasksOptions) (url.Values, error) {
	if strings.TrimSpace(opts.Filter) != "" && strings.TrimSpace(opts.DueFilter) != "" {
		return nil, errors.New("filter and due filter cannot be combined")
	}
	query := url.Values{}
	for key, value := range map[string]string{
		"project_id": opts.ProjectID,
		"section_id": opts.SectionID,
		"label":      opts.Label,
		"lang":       opts.Lang,
	} {
		if v := strings.TrimSpace(value); v != "" {
			query.Set(key, v)
		}
	}
	filter := strings.TrimSpace(opts.Filter)
	if filter == "" {
		filter = strings.TrimSpace(opts.DueFilter)
	}
	if filter != "" {
		query.Set("filter", filter)
	}
	ids := make([]string, 0, len(opts.IDs))
	for _, id := range opts.IDs {
		if clean := strings.TrimSpace(id); clean != "" {
			ids = append(ids, clean)
		}
	}
	if len(ids) > 0 {
		query.Set("ids", strings.Join(ids, ","))
	}
	return query, nil
}

func shouldRetryTodoistRequest(method string, attempt, status int) bool {
	if method != http.MethodGet || attempt >= 2 {
		return false
	}
	switch status {
	case 0, http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

func todoistRetrySleep(ctx context.Context, resp *http.Response, attempt int) error {
	delay := todoistRetryDelay(resp, attempt)
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func todoistRetryDelay(resp *http.Response, attempt int) time.Duration {
	if resp != nil {
		if seconds, err := strconv.Atoi(strings.TrimSpace(resp.Header.Get("Retry-After"))); err == nil && seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}
	switch attempt {
	case 0:
		return 200 * time.Millisecond
	case 1:
		return 500 * time.Millisecond
	}
	return time.Second
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
	for key, value := range map[string]string{
		"description": req.Description, "project_id": req.ProjectID, "section_id": req.SectionID,
		"parent_id": req.ParentID, "due_string": req.DueString, "due_date": req.DueDate,
		"due_datetime": req.DueDateTime, "due_lang": req.DueLang, "assignee_id": req.AssigneeID,
		"deadline_date": req.DeadlineDate,
	} {
		addOptionalString(body, key, value)
	}
	addOptionalStrings(body, "labels", req.Labels)
	addOptionalInt(body, "order", req.Order)
	addOptionalInt(body, "priority", req.Priority)
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
	for key, value := range map[string]*string{
		"content": req.Content, "description": req.Description, "due_string": req.DueString,
		"due_date": req.DueDate, "due_datetime": req.DueDateTime, "due_lang": req.DueLang,
		"assignee_id": req.AssigneeID, "deadline_date": req.DeadlineDate,
	} {
		addPointerString(body, key, value)
	}
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
	if value != nil {
		addOptionalString(body, key, *value)
	}
}

func addOptionalStrings(body map[string]any, key string, values []string) {
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
	if countNonEmptyPointers(&dueString, &dueDate, &dueDateTime) > 1 {
		return errors.New("only one due field may be set")
	}
	return nil
}

func validateDuePointers(dueString, dueDate, dueDateTime *string) error {
	if countNonEmptyPointers(dueString, dueDate, dueDateTime) > 1 {
		return errors.New("only one due field may be set")
	}
	return nil
}

func hasMoveRequest(projectID, sectionID, parentID *string) bool {
	return countNonEmptyPointers(projectID, sectionID, parentID) > 0
}

func validateMoveRequest(projectID, sectionID, parentID *string) error {
	if countNonEmptyPointers(projectID, sectionID, parentID) > 1 {
		return errors.New("only one of project_id, section_id, or parent_id may be set")
	}
	return nil
}

func countNonEmptyPointers(values ...*string) int {
	count := 0
	for _, value := range values {
		if value != nil && strings.TrimSpace(*value) != "" {
			count++
		}
	}
	return count
}

func newRequestID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "sloptools-todoist-request"
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", buf[:4], buf[4:6], buf[6:8], buf[8:10], buf[10:])
}
