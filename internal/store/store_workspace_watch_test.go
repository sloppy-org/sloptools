package store

import (
	"path/filepath"
	"testing"
)

func TestWorkspaceWatchStoreLifecycle(t *testing.T) {
	s := newTestStore(t)

	workspace, err := s.CreateWorkspace("Watch", filepath.Join(t.TempDir(), "watch"), SphereWork)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	run, err := s.CreateBatchRun(workspace.ID, `{"worker":"codex"}`, "running")
	if err != nil {
		t.Fatalf("CreateBatchRun() error: %v", err)
	}

	watch, err := s.UpsertWorkspaceWatch(workspace.ID, `{"worker":"codex"}`, 45, true, &run.ID)
	if err != nil {
		t.Fatalf("UpsertWorkspaceWatch() error: %v", err)
	}
	if !watch.Enabled {
		t.Fatal("watch enabled = false, want true")
	}
	if watch.PollIntervalSeconds != 45 {
		t.Fatalf("poll_interval_seconds = %d, want 45", watch.PollIntervalSeconds)
	}
	if watch.CurrentBatchID == nil || *watch.CurrentBatchID != run.ID {
		t.Fatalf("current_batch_id = %v, want %d", watch.CurrentBatchID, run.ID)
	}

	got, err := s.GetWorkspaceWatch(workspace.ID)
	if err != nil {
		t.Fatalf("GetWorkspaceWatch() error: %v", err)
	}
	if got.ConfigJSON != `{"worker":"codex"}` {
		t.Fatalf("config_json = %q, want worker config", got.ConfigJSON)
	}

	disabled, err := s.SetWorkspaceWatchEnabled(workspace.ID, false)
	if err != nil {
		t.Fatalf("SetWorkspaceWatchEnabled(false) error: %v", err)
	}
	if disabled.Enabled {
		t.Fatal("watch enabled = true, want false")
	}

	cleared, err := s.SetWorkspaceWatchBatch(workspace.ID, nil)
	if err != nil {
		t.Fatalf("SetWorkspaceWatchBatch(nil) error: %v", err)
	}
	if cleared.CurrentBatchID != nil {
		t.Fatalf("current_batch_id = %v, want nil", cleared.CurrentBatchID)
	}

	listed, err := s.ListWorkspaceWatches(false)
	if err != nil {
		t.Fatalf("ListWorkspaceWatches(false) error: %v", err)
	}
	if len(listed) != 1 || listed[0].WorkspaceID != workspace.ID {
		t.Fatalf("ListWorkspaceWatches(false) = %+v, want single watch", listed)
	}

	enabledOnly, err := s.ListWorkspaceWatches(true)
	if err != nil {
		t.Fatalf("ListWorkspaceWatches(true) error: %v", err)
	}
	if len(enabledOnly) != 0 {
		t.Fatalf("ListWorkspaceWatches(true) len = %d, want 0", len(enabledOnly))
	}
}
