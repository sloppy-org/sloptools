package todoist

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := NewClient(
		"token-123",
		WithBaseURL(server.URL+"/rest/v2"),
		WithMoveBaseURL(server.URL+"/api/v1"),
		WithHTTPClient(server.Client()),
		WithRequestIDGenerator(func() string { return "req-123" }),
	)
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	return client
}

func TestTokenEnvVarAndNewClientFromEnv(t *testing.T) {
	if got, want := TokenEnvVar("Work Main"), "SLOPSHELL_TODOIST_TOKEN_WORK_MAIN"; got != want {
		t.Fatalf("TokenEnvVar() = %q, want %q", got, want)
	}

	t.Setenv(TokenEnvVar("Work Main"), "todo-token")

	client, err := NewClientFromEnv("Work Main")
	if err != nil {
		t.Fatalf("NewClientFromEnv() error: %v", err)
	}
	if client.token != "todo-token" {
		t.Fatalf("token = %q, want %q", client.token, "todo-token")
	}

	if _, err := NewClientFromEnv("Missing"); !errors.Is(err, ErrTokenNotConfigured) {
		t.Fatalf("NewClientFromEnv(missing) error = %v, want ErrTokenNotConfigured", err)
	}
}

func TestListProjectsAndTasks(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth != "Bearer token-123" {
			t.Fatalf("Authorization = %q, want Bearer token-123", auth)
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/v2/projects":
			_ = json.NewEncoder(w).Encode([]Project{{ID: "proj-1", Name: "Inbox"}})
		case r.Method == http.MethodGet && r.URL.Path == "/rest/v2/tasks":
			if got := r.URL.Query().Get("project_id"); got != "proj-1" {
				t.Fatalf("project_id = %q, want proj-1", got)
			}
			if got := r.URL.Query().Get("label"); got != "waiting" {
				t.Fatalf("label = %q, want waiting", got)
			}
			if got := r.URL.Query().Get("filter"); got != "due before: 2026-03-10" {
				t.Fatalf("filter = %q", got)
			}
			_ = json.NewEncoder(w).Encode([]Task{{ID: "task-1", Content: "Follow up"}})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	})

	projects, err := client.ListProjects(context.Background())
	if err != nil {
		t.Fatalf("ListProjects() error: %v", err)
	}
	if len(projects) != 1 || projects[0].ID != "proj-1" {
		t.Fatalf("projects = %#v", projects)
	}

	tasks, err := client.ListTasks(context.Background(), ListTasksOptions{
		ProjectID: "proj-1",
		Label:     "waiting",
		DueFilter: "due before: 2026-03-10",
	})
	if err != nil {
		t.Fatalf("ListTasks() error: %v", err)
	}
	if len(tasks) != 1 || tasks[0].ID != "task-1" {
		t.Fatalf("tasks = %#v", tasks)
	}
}

func TestGetTaskIncludesComments(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/v2/tasks/task-1":
			_ = json.NewEncoder(w).Encode(Task{ID: "task-1", Content: "Write summary"})
		case r.Method == http.MethodGet && r.URL.Path == "/rest/v2/comments":
			if got := r.URL.Query().Get("task_id"); got != "task-1" {
				t.Fatalf("task_id = %q, want task-1", got)
			}
			_ = json.NewEncoder(w).Encode([]Comment{{ID: "comment-1", Content: "Remember appendix"}})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	})

	detail, err := client.GetTask(context.Background(), "task-1")
	if err != nil {
		t.Fatalf("GetTask() error: %v", err)
	}
	if detail.Task.ID != "task-1" || len(detail.Comments) != 1 || detail.Comments[0].ID != "comment-1" {
		t.Fatalf("detail = %#v", detail)
	}
}

func TestCreateTaskSendsExpectedBody(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/rest/v2/tasks" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
		if got := r.Header.Get("X-Request-Id"); got != "req-123" {
			t.Fatalf("X-Request-Id = %q, want req-123", got)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll(body) error: %v", err)
		}
		text := string(body)
		for _, want := range []string{
			`"content":"Buy milk"`,
			`"project_id":"proj-1"`,
			`"labels":["home","errands"]`,
			`"due_string":"tomorrow 9am"`,
			`"due_lang":"en"`,
			`"duration":15`,
			`"duration_unit":"minute"`,
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("request body missing %q: %s", want, text)
			}
		}
		_ = json.NewEncoder(w).Encode(Task{ID: "task-2", Content: "Buy milk"})
	})

	task, err := client.CreateTask(context.Background(), CreateTaskRequest{
		Content:     "Buy milk",
		ProjectID:   "proj-1",
		Labels:      []string{"home", "errands"},
		DueString:   "tomorrow 9am",
		DueLang:     "en",
		Duration:    &Duration{Amount: 15, Unit: "minute"},
		Description: "2L whole milk",
	})
	if err != nil {
		t.Fatalf("CreateTask() error: %v", err)
	}
	if task.ID != "task-2" {
		t.Fatalf("created task = %#v", task)
	}
}

func TestCompleteAndReopenTask(t *testing.T) {
	var calls []string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path)
		if got := r.Header.Get("X-Request-Id"); got != "req-123" {
			t.Fatalf("X-Request-Id = %q, want req-123", got)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	if err := client.CompleteTask(context.Background(), "task-1"); err != nil {
		t.Fatalf("CompleteTask() error: %v", err)
	}
	if err := client.ReopenTask(context.Background(), "task-1"); err != nil {
		t.Fatalf("ReopenTask() error: %v", err)
	}
	if len(calls) != 2 || calls[0] != "/rest/v2/tasks/task-1/close" || calls[1] != "/rest/v2/tasks/task-1/reopen" {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestUpdateTaskMovesProjectAndReturnsRefreshedTask(t *testing.T) {
	title := "Updated task"
	labels := []string{"p1"}
	projectID := "proj-2"
	var updateSeen, moveSeen bool

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/rest/v2/tasks/task-1":
			updateSeen = true
			body, _ := io.ReadAll(r.Body)
			text := string(body)
			if !strings.Contains(text, `"content":"Updated task"`) || !strings.Contains(text, `"labels":["p1"]`) {
				t.Fatalf("update body = %s", text)
			}
			_ = json.NewEncoder(w).Encode(Task{ID: "task-1", Content: "Updated task"})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/tasks/task-1/move":
			moveSeen = true
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), `"project_id":"proj-2"`) {
				t.Fatalf("move body = %s", string(body))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		case r.Method == http.MethodGet && r.URL.Path == "/rest/v2/tasks/task-1":
			_ = json.NewEncoder(w).Encode(Task{ID: "task-1", Content: "Updated task", ProjectID: &projectID})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	})

	task, err := client.UpdateTask(context.Background(), "task-1", UpdateTaskRequest{
		Content:   &title,
		Labels:    &labels,
		ProjectID: &projectID,
	})
	if err != nil {
		t.Fatalf("UpdateTask() error: %v", err)
	}
	if !updateSeen || !moveSeen {
		t.Fatalf("updateSeen=%v moveSeen=%v", updateSeen, moveSeen)
	}
	if task.ProjectID == nil || *task.ProjectID != "proj-2" {
		t.Fatalf("task = %#v", task)
	}
}

func TestValidationAndAPIErrors(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad request", http.StatusBadRequest)
	})

	if _, err := client.CreateTask(context.Background(), CreateTaskRequest{
		Content:     "task",
		DueString:   "today",
		DueDate:     "2026-03-10",
		ProjectID:   "proj",
		Description: "invalid",
	}); err == nil || !strings.Contains(err.Error(), "only one due field") {
		t.Fatalf("CreateTask(multi due) error = %v", err)
	}

	sectionID := "sec-1"
	projectID := "proj-1"
	if _, err := client.UpdateTask(context.Background(), "task-1", UpdateTaskRequest{
		ProjectID: &projectID,
		SectionID: &sectionID,
	}); err == nil || !strings.Contains(err.Error(), "only one of project_id") {
		t.Fatalf("UpdateTask(multi move) error = %v", err)
	}

	if _, err := client.ListProjects(context.Background()); err == nil {
		t.Fatal("ListProjects() error = nil, want APIError")
	} else {
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadRequest {
			t.Fatalf("ListProjects() error = %v, want APIError 400", err)
		}
	}
}
