package store

import (
	"path/filepath"
	"testing"
)

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
