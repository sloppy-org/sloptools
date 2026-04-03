package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestTimeEntrySwitchAndSummaryLifecycle(t *testing.T) {
	s := newTestStore(t)

	workspace, err := s.CreateWorkspace("Slopshell", filepath.Join(t.TempDir(), "slopshell"), SphereWork)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}

	start := time.Date(2026, 3, 9, 8, 0, 0, 0, time.UTC)
	middle := start.Add(90 * time.Minute)
	end := middle.Add(30 * time.Minute)

	first, changed, err := s.SwitchActiveTimeEntry(start, &workspace.ID, SphereWork, "workspace_switch", nil)
	if err != nil {
		t.Fatalf("SwitchActiveTimeEntry(first) error: %v", err)
	}
	if !changed {
		t.Fatal("expected first switch to create an entry")
	}
	second, changed, err := s.SwitchActiveTimeEntry(middle, nil, SphereWork, "workspace_switch", nil)
	if err != nil {
		t.Fatalf("SwitchActiveTimeEntry(second) error: %v", err)
	}
	if !changed {
		t.Fatal("expected second switch to create a new entry")
	}
	if _, changed, err := s.SwitchActiveTimeEntry(middle.Add(10*time.Minute), nil, SphereWork, "workspace_switch", nil); err != nil {
		t.Fatalf("SwitchActiveTimeEntry(no-op) error: %v", err)
	} else if changed {
		t.Fatal("expected identical context switch to be a no-op")
	}
	if stopped, err := s.StopActiveTimeEntries(end); err != nil {
		t.Fatalf("StopActiveTimeEntries() error: %v", err)
	} else if stopped != 1 {
		t.Fatalf("StopActiveTimeEntries() = %d, want 1", stopped)
	}

	entries, err := s.ListTimeEntries(TimeEntryListFilter{})
	if err != nil {
		t.Fatalf("ListTimeEntries() error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("ListTimeEntries() len = %d, want 2", len(entries))
	}
	if entries[0].ID != first.ID {
		t.Fatalf("first entry id = %d, want %d", entries[0].ID, first.ID)
	}
	if entries[0].EndedAt == nil || *entries[0].EndedAt != middle.Format(time.RFC3339) {
		t.Fatalf("first entry ended_at = %v, want %s", entries[0].EndedAt, middle.Format(time.RFC3339))
	}
	if entries[1].ID != second.ID {
		t.Fatalf("second entry id = %d, want %d", entries[1].ID, second.ID)
	}
	if entries[1].EndedAt == nil || *entries[1].EndedAt != end.Format(time.RFC3339) {
		t.Fatalf("second entry ended_at = %v, want %s", entries[1].EndedAt, end.Format(time.RFC3339))
	}

	workspaceSummary, err := s.SummarizeTimeEntries(TimeEntryListFilter{
		From: &start,
		To:   &end,
	}, "workspace", end)
	if err != nil {
		t.Fatalf("SummarizeTimeEntries(workspace) error: %v", err)
	}
	if len(workspaceSummary) != 2 {
		t.Fatalf("workspace summary len = %d, want 2", len(workspaceSummary))
	}
	if got := workspaceSummary[0].Label; got != workspace.Name {
		t.Fatalf("workspace summary[0] label = %q, want %q", got, workspace.Name)
	}
	if got := workspaceSummary[0].Seconds; got != 90*60 {
		t.Fatalf("workspace summary[0] seconds = %d, want %d", got, 90*60)
	}
	if got := workspaceSummary[1].Label; got != "No workspace" {
		t.Fatalf("workspace summary[1] label = %q, want %q", got, "No workspace")
	}
	if got := workspaceSummary[1].Seconds; got != 30*60 {
		t.Fatalf("workspace summary[1] seconds = %d, want %d", got, 30*60)
	}
}

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
