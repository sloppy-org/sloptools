package store_test

import (
	"database/sql"
	"errors"
	. "github.com/sloppy-org/sloptools/internal/store"
	_ "modernc.org/sqlite"
	"path/filepath"
	"testing"
)

var _ *Store

func TestActiveWorkspaceReturnsCurrentSelection(t *testing.T) {
	s := newTestStore(t)
	alpha, err := s.CreateWorkspace("Alpha", filepath.Join(t.TempDir(), "alpha"), SpherePrivate)
	if err != nil {
		t.Fatalf("CreateWorkspace(alpha) error: %v", err)
	}
	beta, err := s.CreateWorkspace("Beta", filepath.Join(t.TempDir(), "beta"), SphereWork)
	if err != nil {
		t.Fatalf("CreateWorkspace(beta) error: %v", err)
	}
	if err := s.SetActiveWorkspace(beta.ID); err != nil {
		t.Fatalf("SetActiveWorkspace(beta) error: %v", err)
	}
	active, err := s.ActiveWorkspace()
	if err != nil {
		t.Fatalf("ActiveWorkspace() error: %v", err)
	}
	if active.ID != beta.ID {
		t.Fatalf("ActiveWorkspace() = %d, want %d", active.ID, beta.ID)
	}
	if err := s.SetActiveWorkspace(alpha.ID); err != nil {
		t.Fatalf("SetActiveWorkspace(alpha) error: %v", err)
	}
	active, err = s.ActiveWorkspace()
	if err != nil {
		t.Fatalf("ActiveWorkspace() second error: %v", err)
	}
	if active.ID != alpha.ID {
		t.Fatalf("ActiveWorkspace() second = %d, want %d", active.ID, alpha.ID)
	}
}

func TestEnsureDailyWorkspaceIsIdempotentAndRenamePromotesIt(t *testing.T) {
	s := newTestStore(t)
	dirPath := filepath.Join(t.TempDir(), "daily", "2026", "03", "11")
	first, err := s.EnsureDailyWorkspace("2026-03-11", dirPath)
	if err != nil {
		t.Fatalf("EnsureDailyWorkspace(first) error: %v", err)
	}
	if !first.IsDaily {
		t.Fatal("first workspace is_daily = false, want true")
	}
	if first.DailyDate == nil || *first.DailyDate != "2026-03-11" {
		t.Fatalf("first workspace daily_date = %v, want 2026-03-11", first.DailyDate)
	}
	if first.Name != "2026/03/11" {
		t.Fatalf("first workspace name = %q, want 2026/03/11", first.Name)
	}
	if first.DirPath != dirPath {
		t.Fatalf("first workspace dir_path = %q, want %q", first.DirPath, dirPath)
	}
	second, err := s.EnsureDailyWorkspace("2026-03-11", dirPath)
	if err != nil {
		t.Fatalf("EnsureDailyWorkspace(second) error: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("EnsureDailyWorkspace(second) id = %d, want %d", second.ID, first.ID)
	}
	updated, err := s.UpdateWorkspaceName(first.ID, "DEMO-2025-prep")
	if err != nil {
		t.Fatalf("UpdateWorkspaceName() error: %v", err)
	}
	if updated.IsDaily {
		t.Fatal("renamed workspace is_daily = true, want false")
	}
	if updated.DailyDate == nil || *updated.DailyDate != "2026-03-11" {
		t.Fatalf("renamed workspace daily_date = %v, want 2026-03-11", updated.DailyDate)
	}
	if _, err := s.DailyWorkspaceForDate("2026-03-11"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("DailyWorkspaceForDate(after rename) error = %v, want sql.ErrNoRows", err)
	}
}

func TestFocusedWorkspaceIDPersistsSelection(t *testing.T) {
	s := newTestStore(t)
	workspace, err := s.CreateWorkspace("Focused", filepath.Join(t.TempDir(), "focused"))
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	if err := s.SetFocusedWorkspaceID(workspace.ID); err != nil {
		t.Fatalf("SetFocusedWorkspaceID() error: %v", err)
	}
	got, err := s.FocusedWorkspaceID()
	if err != nil {
		t.Fatalf("FocusedWorkspaceID() error: %v", err)
	}
	if got != workspace.ID {
		t.Fatalf("FocusedWorkspaceID() = %d, want %d", got, workspace.ID)
	}
}

func TestFocusedWorkspaceIDClearsToZero(t *testing.T) {
	s := newTestStore(t)
	workspace, err := s.CreateWorkspace("Focused", filepath.Join(t.TempDir(), "focused"))
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	if err := s.SetFocusedWorkspaceID(workspace.ID); err != nil {
		t.Fatalf("SetFocusedWorkspaceID(workspace) error: %v", err)
	}
	if err := s.SetFocusedWorkspaceID(0); err != nil {
		t.Fatalf("SetFocusedWorkspaceID(0) error: %v", err)
	}
	got, err := s.FocusedWorkspaceID()
	if err != nil {
		t.Fatalf("FocusedWorkspaceID() error: %v", err)
	}
	if got != 0 {
		t.Fatalf("FocusedWorkspaceID() = %d, want 0", got)
	}
}

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

func contextIDByNameForTest(t *testing.T, s *Store, name string) int64 {
	t.Helper()
	var contextID int64
	if err := s.DB().QueryRow(`SELECT id FROM contexts WHERE lower(name) = lower(?)`, name).Scan(&contextID); err != nil {
		t.Fatalf("context lookup %q: %v", name, err)
	}
	return contextID
}
