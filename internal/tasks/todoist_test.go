package tasks

import (
	"testing"
	"time"

	"github.com/sloppy-org/sloptools/internal/todoist"
)

func TestTaskItemFromTodoistMapsStartAndDeadline(t *testing.T) {
	projectID := "project-1"
	dueDateTime := "2026-05-04T09:00:00Z"
	completedAt := "2026-05-05T10:00:00Z"
	item := taskItemFromTodoist(todoist.Task{
		ID:          "task-1",
		Content:     "Review proposal",
		Description: "Check budget section",
		Due:         &todoist.Due{DateTime: &dueDateTime, Date: "2026-05-04"},
		Deadline:    &todoist.Deadline{Date: "2026-05-10"},
		ProjectID:   &projectID,
		Checked:     true,
		CompletedAt: &completedAt,
		Priority:    4,
		URL:         "https://todoist.com/showTask?id=task-1",
	}, "")

	if item.ListID != projectID || item.Title != "Review proposal" || item.Notes != "Check budget section" {
		t.Fatalf("basic fields = %+v", item)
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
	if item.ProviderRef != "https://todoist.com/showTask?id=task-1" || item.Priority != "4" {
		t.Fatalf("provider fields = %+v", item)
	}
}
