package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestContextPrefixQueriesAcrossWorkspacesItemsAndArtifacts(t *testing.T) {
	s := newTestStore(t)

	work, err := s.CreateLabel("Work", nil)
	if err != nil {
		t.Fatalf("CreateLabel(work) error: %v", err)
	}
	w7x, err := s.CreateLabel("W7x", &work.ID)
	if err != nil {
		t.Fatalf("CreateLabel(w7x) error: %v", err)
	}
	privateCtx, err := s.CreateLabel("Private", nil)
	if err != nil {
		t.Fatalf("CreateLabel(private) error: %v", err)
	}

	workspaceDir := filepath.Join(t.TempDir(), "w7x")
	workspace, err := s.CreateWorkspace("W7x Workspace", workspaceDir)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	if err := s.LinkLabelToWorkspace(w7x.ID, workspace.ID); err != nil {
		t.Fatalf("LinkLabelToWorkspace() error: %v", err)
	}
	privateWorkspace, err := s.CreateWorkspace("Private Workspace", filepath.Join(t.TempDir(), "private"))
	if err != nil {
		t.Fatalf("CreateWorkspace(private) error: %v", err)
	}
	if err := s.LinkLabelToWorkspace(privateCtx.ID, privateWorkspace.ID); err != nil {
		t.Fatalf("LinkLabelToWorkspace(private) error: %v", err)
	}

	past := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339)
	workspaceItem, err := s.CreateItem("Workspace context item", ItemOptions{
		State:        ItemStateInbox,
		WorkspaceID:  &workspace.ID,
		VisibleAfter: &past,
	})
	if err != nil {
		t.Fatalf("CreateItem(workspace) error: %v", err)
	}
	privateItem, err := s.CreateItem("Private context item", ItemOptions{
		State:        ItemStateInbox,
		VisibleAfter: &past,
	})
	if err != nil {
		t.Fatalf("CreateItem(private) error: %v", err)
	}
	if err := s.LinkLabelToItem(privateCtx.ID, privateItem.ID); err != nil {
		t.Fatalf("LinkLabelToItem(private) error: %v", err)
	}

	workspaceArtifactPath := filepath.Join(workspaceDir, "notes.md")
	workspaceArtifactTitle := "Workspace artifact"
	workspaceArtifact, err := s.CreateArtifact(ArtifactKindMarkdown, &workspaceArtifactPath, nil, &workspaceArtifactTitle, nil)
	if err != nil {
		t.Fatalf("CreateArtifact(workspace) error: %v", err)
	}
	directArtifactTitle := "Direct context artifact"
	directArtifact, err := s.CreateArtifact(ArtifactKindMarkdown, nil, nil, &directArtifactTitle, nil)
	if err != nil {
		t.Fatalf("CreateArtifact(direct) error: %v", err)
	}
	if err := s.LinkLabelToArtifact(w7x.ID, directArtifact.ID); err != nil {
		t.Fatalf("LinkLabelToArtifact() error: %v", err)
	}
	privateArtifactTitle := "Private artifact"
	privateArtifact, err := s.CreateArtifact(ArtifactKindMarkdown, nil, nil, &privateArtifactTitle, nil)
	if err != nil {
		t.Fatalf("CreateArtifact(private) error: %v", err)
	}
	if err := s.LinkLabelToArtifact(privateCtx.ID, privateArtifact.ID); err != nil {
		t.Fatalf("LinkLabelToArtifact(private) error: %v", err)
	}

	workspaces, err := s.ListWorkspacesByContextPrefix("work/w7x")
	if err != nil {
		t.Fatalf("ListWorkspacesByContextPrefix() error: %v", err)
	}
	if len(workspaces) != 1 || workspaces[0].ID != workspace.ID {
		t.Fatalf("ListWorkspacesByContextPrefix() = %+v, want workspace %d", workspaces, workspace.ID)
	}

	items, err := s.ListItemsByContextPrefix("work")
	if err != nil {
		t.Fatalf("ListItemsByContextPrefix(work) error: %v", err)
	}
	if len(items) != 1 || items[0].ID != workspaceItem.ID {
		t.Fatalf("ListItemsByContextPrefix(work) = %+v, want item %d", items, workspaceItem.ID)
	}

	artifacts, err := s.ListArtifactsByContextPrefix("w7x")
	if err != nil {
		t.Fatalf("ListArtifactsByContextPrefix(w7x) error: %v", err)
	}
	if len(artifacts) != 2 {
		t.Fatalf("ListArtifactsByContextPrefix(w7x) len = %d, want 2", len(artifacts))
	}
	seenArtifacts := map[int64]bool{}
	for _, artifact := range artifacts {
		seenArtifacts[artifact.ID] = true
	}
	if !seenArtifacts[workspaceArtifact.ID] || !seenArtifacts[directArtifact.ID] || seenArtifacts[privateArtifact.ID] {
		t.Fatalf("ListArtifactsByContextPrefix(w7x) ids = %#v", seenArtifacts)
	}
}

func TestContextPrefixQueriesMatchFlatContextNames(t *testing.T) {
	s := newTestStore(t)

	march11, err := s.CreateLabel("2026/03/11", nil)
	if err != nil {
		t.Fatalf("CreateLabel(march11) error: %v", err)
	}
	march12, err := s.CreateLabel("2026/03/12", nil)
	if err != nil {
		t.Fatalf("CreateLabel(march12) error: %v", err)
	}
	april01, err := s.CreateLabel("2026/04/01", nil)
	if err != nil {
		t.Fatalf("CreateLabel(april01) error: %v", err)
	}

	past := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339)
	march11Item, err := s.CreateItem("March 11 item", ItemOptions{State: ItemStateInbox, VisibleAfter: &past})
	if err != nil {
		t.Fatalf("CreateItem(march11) error: %v", err)
	}
	if err := s.LinkLabelToItem(march11.ID, march11Item.ID); err != nil {
		t.Fatalf("LinkLabelToItem(march11) error: %v", err)
	}
	march12Item, err := s.CreateItem("March 12 item", ItemOptions{State: ItemStateInbox, VisibleAfter: &past})
	if err != nil {
		t.Fatalf("CreateItem(march12) error: %v", err)
	}
	if err := s.LinkLabelToItem(march12.ID, march12Item.ID); err != nil {
		t.Fatalf("LinkLabelToItem(march12) error: %v", err)
	}
	aprilItem, err := s.CreateItem("April 1 item", ItemOptions{State: ItemStateInbox, VisibleAfter: &past})
	if err != nil {
		t.Fatalf("CreateItem(april) error: %v", err)
	}
	if err := s.LinkLabelToItem(april01.ID, aprilItem.ID); err != nil {
		t.Fatalf("LinkLabelToItem(april) error: %v", err)
	}

	items, err := s.ListItemsByContextPrefix("2026/03")
	if err != nil {
		t.Fatalf("ListItemsByContextPrefix(2026/03) error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("ListItemsByContextPrefix(2026/03) len = %d, want 2", len(items))
	}
	seen := map[int64]bool{}
	for _, item := range items {
		seen[item.ID] = true
	}
	if !seen[march11Item.ID] || !seen[march12Item.ID] || seen[aprilItem.ID] {
		t.Fatalf("ListItemsByContextPrefix(2026/03) ids = %#v", seen)
	}

	exact, err := s.ListItemsByContextPrefix("2026/03/11")
	if err != nil {
		t.Fatalf("ListItemsByContextPrefix(2026/03/11) error: %v", err)
	}
	if len(exact) != 1 || exact[0].ID != march11Item.ID {
		t.Fatalf("ListItemsByContextPrefix(2026/03/11) = %+v, want item %d", exact, march11Item.ID)
	}
}

func TestArtifactContextPrefixQueriesCanCombineDateAndTopicContexts(t *testing.T) {
	s := newTestStore(t)

	plasmaWorkspace, err := s.EnsureDailyWorkspace("2026-03-11", filepath.Join(t.TempDir(), "daily", "2026", "03", "11", "plasma"))
	if err != nil {
		t.Fatalf("EnsureDailyWorkspace(plasma) error: %v", err)
	}
	healthWorkspace, err := s.CreateWorkspace("Health notes", filepath.Join(t.TempDir(), "health"))
	if err != nil {
		t.Fatalf("CreateWorkspace(health) error: %v", err)
	}

	workRootID := contextIDByNameForTest(t, s, "work")
	workRoot, err := s.GetLabel(workRootID)
	if err != nil {
		t.Fatalf("GetLabel(work) error: %v", err)
	}
	privateRootID := contextIDByNameForTest(t, s, "private")
	privateRoot, err := s.GetLabel(privateRootID)
	if err != nil {
		t.Fatalf("GetLabel(private) error: %v", err)
	}
	plasmaContext, err := s.CreateLabel("work/plasma", &workRoot.ID)
	if err != nil {
		t.Fatalf("CreateLabel(work/plasma) error: %v", err)
	}
	healthContext, err := s.CreateLabel("private/health", &privateRoot.ID)
	if err != nil {
		t.Fatalf("CreateLabel(private/health) error: %v", err)
	}
	marchDay := mustGetLabelByName(t, s, "2026/03/11")
	if err := s.LinkLabelToWorkspace(plasmaContext.ID, plasmaWorkspace.ID); err != nil {
		t.Fatalf("LinkLabelToWorkspace(plasma) error: %v", err)
	}
	if err := s.LinkLabelToWorkspace(marchDay.ID, healthWorkspace.ID); err != nil {
		t.Fatalf("LinkLabelToWorkspace(march day) error: %v", err)
	}
	if err := s.LinkLabelToWorkspace(healthContext.ID, healthWorkspace.ID); err != nil {
		t.Fatalf("LinkLabelToWorkspace(health) error: %v", err)
	}

	plasmaPath := filepath.Join(plasmaWorkspace.DirPath, "plan.md")
	plasmaTitle := "Plasma plan"
	plasmaArtifact, err := s.CreateArtifact(ArtifactKindMarkdown, &plasmaPath, nil, &plasmaTitle, nil)
	if err != nil {
		t.Fatalf("CreateArtifact(plasma) error: %v", err)
	}
	healthPath := filepath.Join(healthWorkspace.DirPath, "notes.md")
	healthTitle := "Health notes"
	_, err = s.CreateArtifact(ArtifactKindMarkdown, &healthPath, nil, &healthTitle, nil)
	if err != nil {
		t.Fatalf("CreateArtifact(health) error: %v", err)
	}

	artifacts, err := s.ListArtifactsByContextPrefix("2026/03/11 + work/plasma")
	if err != nil {
		t.Fatalf("ListArtifactsByContextPrefix(combined) error: %v", err)
	}
	if len(artifacts) != 1 || artifacts[0].ID != plasmaArtifact.ID {
		t.Fatalf("ListArtifactsByContextPrefix(combined) = %+v, want artifact %d", artifacts, plasmaArtifact.ID)
	}
}
