package tasks

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/todoist"
)

func newTodoistProviderTestClient(t *testing.T, handler http.HandlerFunc) *TodoistProvider {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := todoist.NewClient(
		"token-123",
		todoist.WithBaseURL(server.URL+"/rest/v2"),
		todoist.WithMoveBaseURL(server.URL+"/api/v1"),
		todoist.WithHTTPClient(server.Client()),
		todoist.WithRequestIDGenerator(func() string { return "req-123" }),
	)
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	return NewTodoistProvider(client)
}

func TestTaskItemFromTodoistMapsMetadataAndComments(t *testing.T) {
	projectID := "project-1"
	sectionID := "section-1"
	parentID := "parent-1"
	assigneeID := "user-1"
	assignerID := "user-2"
	dueDateTime := "2026-05-04T09:00:00Z"
	completedAt := "2026-05-05T10:00:00Z"
	task := todoist.Task{
		ID:          "task-1",
		Content:     "Review proposal",
		Description: "Check budget section",
		Due:         &todoist.Due{DateTime: &dueDateTime, Date: "2026-05-04"},
		Deadline:    &todoist.Deadline{Date: "2026-05-10"},
		ProjectID:   &projectID,
		SectionID:   &sectionID,
		ParentID:    &parentID,
		AssigneeID:  &assigneeID,
		AssignerID:  &assignerID,
		Checked:     true,
		CompletedAt: &completedAt,
		Labels:      []string{"waiting", "review"},
		Priority:    4,
		URL:         "https://todoist.com/showTask?id=task-1",
	}
	comments := []todoist.Comment{{
		ID:       "comment-1",
		TaskID:   &task.ID,
		PostedAt: "2026-05-06T12:00:00Z",
		Content:  "Remember appendix",
		Attachment: &todoist.CommentAttachment{
			FileName:     "notes.txt",
			FileType:     "text/plain",
			FileURL:      "https://example.test/notes.txt",
			ResourceType: "file",
		},
	}}

	item := taskItemFromTodoist(task, "", comments)
	if item.ListID != projectID || item.Title != "Review proposal" || item.Notes != "Check budget section" {
		t.Fatalf("basic fields = %+v", item)
	}
	if item.ProjectID != projectID || item.SectionID != sectionID || item.ParentID != parentID {
		t.Fatalf("source fields = %+v", item)
	}
	if item.AssigneeID != assigneeID || item.AssignerID != assignerID {
		t.Fatalf("assignee fields = %+v", item)
	}
	if item.ProviderRef != "task-1" || item.ProviderURL != "https://todoist.com/showTask?id=task-1" {
		t.Fatalf("provider fields = %+v", item)
	}
	if len(item.Labels) != 2 || item.Labels[0] != "waiting" || item.Labels[1] != "review" {
		t.Fatalf("labels = %+v", item.Labels)
	}
	if len(item.Comments) != 1 || item.Comments[0].ID != "comment-1" || item.Comments[0].Attachment == nil {
		t.Fatalf("comments = %+v", item.Comments)
	}
	wantStart := time.Date(2026, time.May, 4, 9, 0, 0, 0, time.UTC)
	if item.StartAt == nil || !item.StartAt.Equal(wantStart) {
		t.Fatalf("StartAt = %v, want %v", item.StartAt, wantStart)
	}
	wantDue := time.Date(2026, time.May, 10, 0, 0, 0, 0, time.UTC)
	if item.Due == nil || !item.Due.Equal(wantDue) {
		t.Fatalf("Due = %v, want %v", item.Due, wantDue)
	}
	wantCompleted := time.Date(2026, time.May, 5, 10, 0, 0, 0, time.UTC)
	if item.CompletedAt == nil || !item.CompletedAt.Equal(wantCompleted) || !item.Completed {
		t.Fatalf("completion fields = %+v", item)
	}
}

func TestTodoistProviderListsAndReadsMetadata(t *testing.T) {
	provider := newTodoistProviderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/v2/projects":
			_ = json.NewEncoder(w).Encode([]todoist.Project{{
				ID: "proj-1", Name: "Inbox", Color: "berry", Order: 7, IsInboxProject: true, URL: "https://todoist.com/app/project/proj-1",
			}})
		case r.Method == http.MethodGet && r.URL.Path == "/rest/v2/tasks":
			if got := r.URL.Query().Get("project_id"); got != "proj-1" {
				t.Fatalf("project_id = %q, want proj-1", got)
			}
			_ = json.NewEncoder(w).Encode([]todoist.Task{{
				ID: "task-1", Content: "Follow up", Description: "Bring notes", ProjectID: strPtr("proj-1"), SectionID: strPtr("sec-1"), Labels: []string{"waiting"},
				AssigneeID: strPtr("user-1"), URL: "https://todoist.com/showTask?id=task-1",
			}})
		case r.Method == http.MethodGet && r.URL.Path == "/rest/v2/tasks/task-1":
			_ = json.NewEncoder(w).Encode(todoist.Task{
				ID: "task-1", Content: "Follow up", Description: "Bring notes", ProjectID: strPtr("proj-1"), SectionID: strPtr("sec-1"), Labels: []string{"waiting"},
				AssigneeID: strPtr("user-1"), URL: "https://todoist.com/showTask?id=task-1",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/rest/v2/comments":
			if got := r.URL.Query().Get("task_id"); got != "task-1" {
				t.Fatalf("task_id = %q, want task-1", got)
			}
			_ = json.NewEncoder(w).Encode([]todoist.Comment{{ID: "comment-1", TaskID: strPtr("task-1"), PostedAt: "2026-05-06T12:00:00Z", Content: "Remember appendix"}})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	})

	lists, err := provider.ListTaskLists(context.Background())
	if err != nil {
		t.Fatalf("ListTaskLists() error: %v", err)
	}
	if len(lists) != 1 || lists[0].ID != "proj-1" || !lists[0].Primary || !lists[0].IsInboxProject {
		t.Fatalf("lists = %+v", lists)
	}
	if lists[0].ProviderURL != "https://todoist.com/app/project/proj-1" || lists[0].Color != "berry" {
		t.Fatalf("list metadata = %+v", lists[0])
	}

	items, err := provider.ListTasks(context.Background(), "proj-1")
	if err != nil {
		t.Fatalf("ListTasks() error: %v", err)
	}
	if len(items) != 1 || items[0].ProviderRef != "task-1" || items[0].ProviderURL == "" {
		t.Fatalf("items = %+v", items)
	}
	if len(items[0].Labels) != 1 || items[0].Labels[0] != "waiting" || items[0].ProjectID != "proj-1" {
		t.Fatalf("item metadata = %+v", items[0])
	}

	detail, err := provider.GetTask(context.Background(), "proj-1", "task-1")
	if err != nil {
		t.Fatalf("GetTask() error: %v", err)
	}
	if detail.ProviderRef != "task-1" || len(detail.Comments) != 1 || detail.Comments[0].ID != "comment-1" {
		t.Fatalf("detail = %+v", detail)
	}
}

func TestTodoistProviderMutationsAndLists(t *testing.T) {
	var calls []string
	provider := newTodoistProviderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/rest/v2/tasks":
			body, _ := io.ReadAll(r.Body)
			text := string(body)
			for _, want := range []string{`"content":"Buy milk"`, `"project_id":"proj-1"`, `"section_id":"sec-1"`, `"labels":["home","errands"]`, `"due_datetime":"2026-05-01T09:00:00Z"`, `"deadline_date":"2026-05-10"`, `"assignee_id":"user-1"`} {
				if !strings.Contains(text, want) {
					t.Fatalf("create body missing %q: %s", want, text)
				}
			}
			_ = json.NewEncoder(w).Encode(todoist.Task{ID: "task-1", Content: "Buy milk", ProjectID: strPtr("proj-1"), URL: "https://todoist.com/showTask?id=task-1"})
		case r.Method == http.MethodPost && r.URL.Path == "/rest/v2/tasks/task-1":
			body, _ := io.ReadAll(r.Body)
			text := string(body)
			for _, want := range []string{`"content":"Buy oat milk"`, `"labels":["groceries"]`, `"assignee_id":"user-2"`} {
				if !strings.Contains(text, want) {
					t.Fatalf("update body missing %q: %s", want, text)
				}
			}
			_ = json.NewEncoder(w).Encode(todoist.Task{ID: "task-1", Content: "Buy oat milk", ProjectID: strPtr("proj-2"), SectionID: strPtr("sec-2"), URL: "https://todoist.com/showTask?id=task-1"})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/tasks/task-1/move":
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), `"section_id":"sec-2"`) {
				t.Fatalf("move body = %s", string(body))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		case r.Method == http.MethodGet && r.URL.Path == "/rest/v2/tasks/task-1":
			_ = json.NewEncoder(w).Encode(todoist.Task{ID: "task-1", Content: "Buy oat milk", ProjectID: strPtr("proj-2"), SectionID: strPtr("sec-2"), URL: "https://todoist.com/showTask?id=task-1"})
		case r.Method == http.MethodPost && r.URL.Path == "/rest/v2/tasks/task-1/close":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/rest/v2/tasks/task-1/reopen":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/v2/tasks/task-1":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/rest/v2/projects":
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), `"name":"Work"`) {
				t.Fatalf("project create body = %s", string(body))
			}
			_ = json.NewEncoder(w).Encode(todoist.Project{ID: "proj-3", Name: "Work", URL: "https://todoist.com/app/project/proj-3"})
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/v2/projects/proj-3":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	})

	created, err := provider.CreateTask(context.Background(), "proj-1", providerdata.TaskItem{
		Title:       "Buy milk",
		Description: "2L whole milk",
		SectionID:   "sec-1",
		Labels:      []string{"home", "errands"},
		AssigneeID:  "user-1",
		StartAt:     ptrTime(time.Date(2026, time.May, 1, 9, 0, 0, 0, time.UTC)),
		Due:         ptrTime(time.Date(2026, time.May, 10, 0, 0, 0, 0, time.UTC)),
		Priority:    "p1",
	})
	if err != nil {
		t.Fatalf("CreateTask() error: %v", err)
	}
	if created.ID != "task-1" || created.ProviderRef != "task-1" {
		t.Fatalf("created = %+v", created)
	}

	updated, err := provider.UpdateTask(context.Background(), "proj-1", providerdata.TaskItem{
		ID:          "task-1",
		ListID:      "proj-2",
		Title:       "Buy oat milk",
		SectionID:   "sec-2",
		Labels:      []string{"groceries"},
		AssigneeID:  "user-2",
		Description: "1L oat milk",
	})
	if err != nil {
		t.Fatalf("UpdateTask() error: %v", err)
	}
	if updated.Title != "Buy oat milk" || updated.ProviderRef != "task-1" || updated.ListID != "proj-2" {
		t.Fatalf("updated = %+v", updated)
	}

	if err := provider.CompleteTask(context.Background(), "proj-1", "task-1"); err != nil {
		t.Fatalf("CompleteTask() error: %v", err)
	}
	if err := provider.UncompleteTask(context.Background(), "proj-1", "task-1"); err != nil {
		t.Fatalf("UncompleteTask() error: %v", err)
	}
	if err := provider.DeleteTask(context.Background(), "proj-1", "task-1"); err != nil {
		t.Fatalf("DeleteTask() error: %v", err)
	}

	createdList, err := provider.CreateTaskList(context.Background(), "Work")
	if err != nil {
		t.Fatalf("CreateTaskList() error: %v", err)
	}
	if createdList.ID != "proj-3" || createdList.ProviderURL == "" {
		t.Fatalf("created list = %+v", createdList)
	}
	if err := provider.DeleteTaskList(context.Background(), "proj-3"); err != nil {
		t.Fatalf("DeleteTaskList() error: %v", err)
	}

	if len(calls) == 0 {
		t.Fatal("expected HTTP calls")
	}
}

func ptrTime(t time.Time) *time.Time { return &t }
