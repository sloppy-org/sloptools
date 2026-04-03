package store

import (
	"database/sql"
	"errors"
	"testing"
)

func TestExternalContainerMappingStoreCRUD(t *testing.T) {
	s := newTestStore(t)

	workspace, err := s.CreateWorkspace("Work", t.TempDir())
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	sphere := SphereWork
	mapping, err := s.SetContainerMapping(ExternalProviderTodoist, "project", " Slopshell ", &workspace.ID, &sphere)
	if err != nil {
		t.Fatalf("SetContainerMapping() error: %v", err)
	}
	if mapping.Provider != ExternalProviderTodoist {
		t.Fatalf("provider = %q, want %q", mapping.Provider, ExternalProviderTodoist)
	}
	if mapping.ContainerType != "project" {
		t.Fatalf("container_type = %q, want project", mapping.ContainerType)
	}
	if mapping.ContainerRef != "Slopshell" {
		t.Fatalf("container_ref = %q, want Slopshell", mapping.ContainerRef)
	}
	if mapping.WorkspaceID == nil || *mapping.WorkspaceID != workspace.ID {
		t.Fatalf("workspace_id = %v, want %d", mapping.WorkspaceID, workspace.ID)
	}
	if mapping.Sphere == nil || *mapping.Sphere != SphereWork {
		t.Fatalf("sphere = %v, want %q", mapping.Sphere, SphereWork)
	}

	got, err := s.GetContainerMapping(ExternalProviderTodoist, "project", "slopshell")
	if err != nil {
		t.Fatalf("GetContainerMapping() error: %v", err)
	}
	if got.ID != mapping.ID {
		t.Fatalf("GetContainerMapping() id = %d, want %d", got.ID, mapping.ID)
	}

	privateSphere := SpherePrivate
	updated, err := s.SetContainerMapping(ExternalProviderTodoist, "project", "slopshell", nil, &privateSphere)
	if err != nil {
		t.Fatalf("SetContainerMapping(update) error: %v", err)
	}
	if updated.ID != mapping.ID {
		t.Fatalf("updated id = %d, want %d", updated.ID, mapping.ID)
	}
	if updated.WorkspaceID != nil {
		t.Fatalf("updated workspace_id = %v, want nil", updated.WorkspaceID)
	}
	if updated.Sphere == nil || *updated.Sphere != SpherePrivate {
		t.Fatalf("updated sphere = %v, want %q", updated.Sphere, SpherePrivate)
	}

	other, err := s.SetContainerMapping(ExternalProviderGoogleCalendar, "calendar", "Family", nil, &privateSphere)
	if err != nil {
		t.Fatalf("SetContainerMapping(other) error: %v", err)
	}

	allMappings, err := s.ListContainerMappings("")
	if err != nil {
		t.Fatalf("ListContainerMappings(all) error: %v", err)
	}
	if len(allMappings) != 2 {
		t.Fatalf("ListContainerMappings(all) len = %d, want 2", len(allMappings))
	}

	todoistMappings, err := s.ListContainerMappings(ExternalProviderTodoist)
	if err != nil {
		t.Fatalf("ListContainerMappings(todoist) error: %v", err)
	}
	if len(todoistMappings) != 1 || todoistMappings[0].ID != updated.ID {
		t.Fatalf("ListContainerMappings(todoist) = %+v, want updated mapping", todoistMappings)
	}

	if err := s.DeleteContainerMapping(other.ID); err != nil {
		t.Fatalf("DeleteContainerMapping() error: %v", err)
	}
	if _, err := s.GetContainerMapping(ExternalProviderGoogleCalendar, "calendar", "Family"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetContainerMapping(deleted) error = %v, want sql.ErrNoRows", err)
	}
}

func TestExternalContainerMappingStoreRejectsInvalidInput(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.SetContainerMapping("", "project", "Slopshell", nil, nil); err == nil {
		t.Fatal("expected missing provider error")
	}
	if _, err := s.SetContainerMapping(ExternalProviderTodoist, "board", "Slopshell", nil, nil); err == nil {
		t.Fatal("expected invalid container type error")
	}
	if _, err := s.SetContainerMapping(ExternalProviderTodoist, "project", "", nil, nil); err == nil {
		t.Fatal("expected missing container ref error")
	}
	if _, err := s.SetContainerMapping(ExternalProviderTodoist, "project", "Slopshell", nil, nil); err == nil {
		t.Fatal("expected missing target error")
	}
	badSphere := "office"
	if _, err := s.SetContainerMapping(ExternalProviderTodoist, "project", "Slopshell", nil, &badSphere); err == nil {
		t.Fatal("expected invalid sphere error")
	}
	missingWorkspaceID := int64(999999)
	if _, err := s.SetContainerMapping(ExternalProviderTodoist, "project", "Slopshell", &missingWorkspaceID, nil); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing workspace error = %v, want sql.ErrNoRows", err)
	}
	if _, err := s.ListContainerMappings("smtp"); err == nil {
		t.Fatal("expected invalid provider filter error")
	}
}
