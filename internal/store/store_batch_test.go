package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func TestBatchRunLifecycle(t *testing.T) {
	s := newTestStore(t)

	workspace, err := s.CreateWorkspace("Batch Workspace", filepath.Join(t.TempDir(), "workspace"))
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	itemA, err := s.CreateItem("Fix login flow", ItemOptions{WorkspaceID: &workspace.ID})
	if err != nil {
		t.Fatalf("CreateItem(itemA) error: %v", err)
	}
	itemB, err := s.CreateItem("Repair flaky spec", ItemOptions{WorkspaceID: &workspace.ID})
	if err != nil {
		t.Fatalf("CreateItem(itemB) error: %v", err)
	}

	run, err := s.CreateBatchRun(workspace.ID, `{"worker":"codex","reviewer":"human"}`, "running")
	if err != nil {
		t.Fatalf("CreateBatchRun() error: %v", err)
	}
	if run.WorkspaceID != workspace.ID {
		t.Fatalf("CreateBatchRun() workspace_id = %d, want %d", run.WorkspaceID, workspace.ID)
	}
	if run.Status != "running" {
		t.Fatalf("CreateBatchRun() status = %q, want running", run.Status)
	}

	prNumber := int64(42)
	prURL := "https://example.test/pr/42"
	first, err := s.UpsertBatchRunItem(run.ID, itemA.ID, BatchRunItemUpdate{
		Status:   "done",
		PRNumber: &prNumber,
		PRURL:    &prURL,
	})
	if err != nil {
		t.Fatalf("UpsertBatchRunItem(done) error: %v", err)
	}
	if first.PRNumber == nil || *first.PRNumber != prNumber {
		t.Fatalf("done item pr_number = %v, want %d", first.PRNumber, prNumber)
	}
	if first.PRURL == nil || *first.PRURL != prURL {
		t.Fatalf("done item pr_url = %v, want %q", first.PRURL, prURL)
	}
	if first.StartedAt == nil || first.FinishedAt == nil {
		t.Fatalf("done item timestamps = %+v, want both started_at and finished_at", first)
	}

	errorMsg := "CI failed on review job"
	second, err := s.UpsertBatchRunItem(run.ID, itemB.ID, BatchRunItemUpdate{
		Status:   "failed",
		ErrorMsg: &errorMsg,
	})
	if err != nil {
		t.Fatalf("UpsertBatchRunItem(failed) error: %v", err)
	}
	if second.ErrorMsg == nil || *second.ErrorMsg != errorMsg {
		t.Fatalf("failed item error_msg = %v, want %q", second.ErrorMsg, errorMsg)
	}

	items, err := s.ListBatchRunItems(run.ID)
	if err != nil {
		t.Fatalf("ListBatchRunItems() error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("ListBatchRunItems() len = %d, want 2", len(items))
	}
	if items[0].ItemTitle == nil || *items[0].ItemTitle == "" {
		t.Fatalf("ListBatchRunItems() missing item title: %+v", items[0])
	}

	finishedAt := "2026-03-09T10:15:00Z"
	run, err = s.SetBatchRunStatus(run.ID, "completed", &finishedAt)
	if err != nil {
		t.Fatalf("SetBatchRunStatus() error: %v", err)
	}
	if run.Status != "completed" {
		t.Fatalf("SetBatchRunStatus() status = %q, want completed", run.Status)
	}
	if run.FinishedAt == nil || *run.FinishedAt != finishedAt {
		t.Fatalf("SetBatchRunStatus() finished_at = %v, want %q", run.FinishedAt, finishedAt)
	}

	listed, err := s.ListBatchRuns(&workspace.ID)
	if err != nil {
		t.Fatalf("ListBatchRuns() error: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != run.ID {
		t.Fatalf("ListBatchRuns() = %+v, want batch %d", listed, run.ID)
	}
}

func TestBatchRunValidationAndEmptyBatch(t *testing.T) {
	s := newTestStore(t)

	workspace, err := s.CreateWorkspace("Batch Workspace", filepath.Join(t.TempDir(), "workspace"))
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}

	if _, err := s.CreateBatchRun(workspace.ID, `{`, "running"); err == nil {
		t.Fatal("CreateBatchRun() with invalid config_json unexpectedly succeeded")
	}

	run, err := s.CreateBatchRun(workspace.ID, "", "")
	if err != nil {
		t.Fatalf("CreateBatchRun(defaults) error: %v", err)
	}
	if run.ConfigJSON != "{}" {
		t.Fatalf("CreateBatchRun(defaults) config_json = %q, want {}", run.ConfigJSON)
	}
	if run.Status != "running" {
		t.Fatalf("CreateBatchRun(defaults) status = %q, want running", run.Status)
	}

	items, err := s.ListBatchRunItems(run.ID)
	if err != nil {
		t.Fatalf("ListBatchRunItems(empty) error: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("ListBatchRunItems(empty) len = %d, want 0", len(items))
	}

	if _, err := s.UpsertBatchRunItem(run.ID, 9999, BatchRunItemUpdate{Status: "running"}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("UpsertBatchRunItem(missing item) error = %v, want sql.ErrNoRows", err)
	}
}
