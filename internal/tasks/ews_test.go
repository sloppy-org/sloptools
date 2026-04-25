package tasks

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/sloppy-org/sloptools/internal/ews"
	"github.com/sloppy-org/sloptools/internal/providerdata"
)

// fakeEWSClient captures EWS calls and replays canned task state.
type fakeEWSClient struct {
	tasks       map[string]ews.Task
	folders     []ews.Folder
	createCount int
}

func newFakeEWSClient(t *testing.T) *fakeEWSClient {
	t.Helper()
	return &fakeEWSClient{
		tasks:   map[string]ews.Task{},
		folders: []ews.Folder{{ID: "tasks", ChangeKey: "ck-primary", Name: "Tasks", Kind: ews.FolderKindTasks}},
	}
}

func (f *fakeEWSClient) GetTaskItem(ctx context.Context, itemID string) (ews.Task, error) {
	t, ok := f.tasks[itemID]
	if !ok {
		return ews.Task{}, fmt.Errorf("task %q not found", itemID)
	}
	return t, nil
}

func (f *fakeEWSClient) CreateTaskItem(ctx context.Context, parentFolderID string, item ews.TaskInput) (itemID, changeKey string, err error) {
	f.createCount++
	id := fmt.Sprintf("task-%d", f.createCount)
	changeKey = fmt.Sprintf("ck-%d", f.createCount)
	f.tasks[id] = ews.Task{
		ID:        id,
		ChangeKey: changeKey,
		Subject:   item.Subject,
		Body:      item.Body,
		Status:    item.Status,
	}
	return id, changeKey, nil
}

func (f *fakeEWSClient) UpdateTaskItem(ctx context.Context, itemID, changeKey string, updates ews.TaskUpdate) (newChangeKey string, err error) {
	t, ok := f.tasks[itemID]
	if !ok {
		return "", fmt.Errorf("task %q not found", itemID)
	}
	if updates.Subject != nil {
		t.Subject = *updates.Subject
	}
	if updates.Body != nil {
		t.Body = *updates.Body
	}
	if updates.Status != nil {
		t.Status = *updates.Status
	}
	if updates.IsComplete != nil {
		t.IsComplete = *updates.IsComplete
	}
	if updates.CompleteDate != nil {
		t.CompleteDate = updates.CompleteDate
	}
	if updates.DueDate != nil {
		t.DueDate = updates.DueDate
	}
	f.tasks[itemID] = t
	return fmt.Sprintf("ck-updated-%d", f.createCount), nil
}

func (f *fakeEWSClient) DeleteTaskItem(ctx context.Context, itemID string) error {
	delete(f.tasks, itemID)
	return nil
}

func (f *fakeEWSClient) ListTasksFolders(ctx context.Context) ([]ews.Folder, error) {
	return f.folders, nil
}

func (f *fakeEWSClient) CreateTasksFolder(ctx context.Context, input ews.TasksFolderInput) (folderID, changeKey string, err error) {
	id := fmt.Sprintf("folder-%d", f.createCount)
	f.createCount++
	folder := ews.Folder{ID: id, ChangeKey: fmt.Sprintf("ck-fold-%d", f.createCount), Name: input.DisplayName, Kind: ews.FolderKindTasks}
	f.folders = append(f.folders, folder)
	return id, folder.ChangeKey, nil
}

func (f *fakeEWSClient) DeleteFolder(ctx context.Context, folderID string) error {
	for i, fold := range f.folders {
		if fold.ID == folderID {
			f.folders = append(f.folders[:i], f.folders[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("folder %q not found", folderID)
}

func (f *fakeEWSClient) GetTasks(ctx context.Context, folderID string, offset, max int) ([]ews.Task, error) {
	out := make([]ews.Task, 0, len(f.tasks))
	for _, t := range f.tasks {
		out = append(out, t)
	}
	return out, nil
}

// compile-time check that fakeEWSClient satisfies the ews client interface
// used by EWSProvider methods.
var _ interface {
	GetTaskItem(ctx context.Context, itemID string) (ews.Task, error)
	CreateTaskItem(ctx context.Context, parentFolderID string, item ews.TaskInput) (itemID, changeKey string, err error)
	UpdateTaskItem(ctx context.Context, itemID, changeKey string, updates ews.TaskUpdate) (newChangeKey string, err error)
	DeleteTaskItem(ctx context.Context, itemID string) error
	ListTasksFolders(ctx context.Context) ([]ews.Folder, error)
	CreateTasksFolder(ctx context.Context, input ews.TasksFolderInput) (folderID, changeKey string, err error)
	DeleteFolder(ctx context.Context, folderID string) error
	GetTasks(ctx context.Context, folderID string, offset, max int) ([]ews.Task, error)
} = (*fakeEWSClient)(nil)

// testEWSProvider wraps EWSProvider with a fake client for testing.
type testEWSProvider struct {
	*EWSProvider
	client *fakeEWSClient
}

func newTestEWSProvider(t *testing.T) *testEWSProvider {
	t.Helper()
	fake := newFakeEWSClient(t)
	provider := NewEWSProvider(nil, "")
	// We need to inject the fake client; since EWSProvider.client is unexported,
	// we'll use reflection or a different approach.
	// Instead, we'll test the mapper functions directly and test the adapter
	// through its public interface by creating a real-ish provider.
	// For simplicity, we test the mappers and the provider structure.
	_ = provider
	return &testEWSProvider{EWSProvider: provider, client: fake}
}

func TestEWSProviderName(t *testing.T) {
	p := NewEWSProvider(nil, "")
	if p.ProviderName() != "exchange_ews" {
		t.Fatalf("ProviderName() = %q, want exchange_ews", p.ProviderName())
	}
}

func TestEWSProviderClose(t *testing.T) {
	p := NewEWSProvider(nil, "")
	if err := p.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}
}

func TestTaskItemFromEWS(t *testing.T) {
	now := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	complete := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name            string
		input           ews.Task
		listID          string
		wantID          string
		wantTitle       string
		wantCompleted   bool
		wantDue         *time.Time
		wantCompletedAt *time.Time
	}{
		{
			name: "not started task",
			input: ews.Task{
				ID: "task-1", ChangeKey: "ck-1",
				Subject: "Buy milk", Status: "NotStarted",
				DueDate: &now,
			},
			listID: "tasks",
			wantID: "task-1", wantTitle: "Buy milk", wantCompleted: false, wantDue: &now,
		},
		{
			name: "completed task",
			input: ews.Task{
				ID: "task-2", ChangeKey: "ck-2",
				Subject: "Done", Status: "Completed", IsComplete: true,
				CompleteDate: &complete,
			},
			listID: "tasks",
			wantID: "task-2", wantTitle: "Done", wantCompleted: true, wantCompletedAt: &complete,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := taskItemFromEWS(tt.input, tt.listID)
			if got.ID != tt.wantID {
				t.Errorf("ID = %q, want %q", got.ID, tt.wantID)
			}
			if got.Title != tt.wantTitle {
				t.Errorf("Title = %q, want %q", got.Title, tt.wantTitle)
			}
			if got.Completed != tt.wantCompleted {
				t.Errorf("Completed = %v, want %v", got.Completed, tt.wantCompleted)
			}
			if tt.wantDue != nil {
				if got.Due == nil || !got.Due.Equal(*tt.wantDue) {
					t.Errorf("Due = %v, want %v", got.Due, tt.wantDue)
				}
			}
			if tt.wantCompletedAt != nil {
				if got.CompletedAt == nil || !got.CompletedAt.Equal(*tt.wantCompletedAt) {
					t.Errorf("CompletedAt = %v, want %v", got.CompletedAt, tt.wantCompletedAt)
				}
			}
		})
	}
}

func TestTaskToEWS(t *testing.T) {
	due := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		input providerdata.TaskItem
		want  ews.TaskInput
	}{
		{
			name: "active task",
			input: providerdata.TaskItem{
				Title: "Buy groceries", Notes: "Milk and eggs",
				Completed: false, Due: &due, Priority: "normal",
			},
			want: ews.TaskInput{Subject: "Buy groceries", Body: "Milk and eggs", Status: "NotStarted", Importance: "Normal"},
		},
		{
			name: "completed task",
			input: providerdata.TaskItem{
				Title: "Done", Completed: true,
			},
			want: ews.TaskInput{Subject: "Done", Status: "Completed", Importance: "Normal"},
		},
		{
			name: "high priority",
			input: providerdata.TaskItem{
				Title: "Urgent", Priority: "high",
			},
			want: ews.TaskInput{Subject: "Urgent", Status: "NotStarted", Importance: "High"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := taskToEWS(tt.input)
			if got.Subject != tt.want.Subject {
				t.Errorf("Subject = %q, want %q", got.Subject, tt.want.Subject)
			}
			if got.Status != tt.want.Status {
				t.Errorf("Status = %q, want %q", got.Status, tt.want.Status)
			}
			if got.Importance != tt.want.Importance {
				t.Errorf("Importance = %q, want %q", got.Importance, tt.want.Importance)
			}
		})
	}
}

func TestMapPriorityToEWS(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"high", "High"},
		{"High", "High"},
		{"low", "Low"},
		{"Low", "Low"},
		{"normal", "Normal"},
		{"", "Normal"},
		{"unknown", "Normal"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := mapPriorityToEWS(tt.input)
			if got != tt.want {
				t.Errorf("mapPriorityToEWS(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestMapEWSStatus(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Completed", "complete"},
		{"completed", "complete"},
		{"NotStarted", "notstarted"},
		{"inprogress", "in-progress"},
		{"WaitingOnOthers", "waiting"},
		{"", "notstarted"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := mapEWSStatus(tt.input)
			if got != tt.want {
				t.Errorf("mapEWSStatus(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
