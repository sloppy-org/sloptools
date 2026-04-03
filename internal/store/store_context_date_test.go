package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestEnsureDateContextHierarchyRepairsParentChain(t *testing.T) {
	s := newTestStore(t)

	year, err := s.CreateLabel("2026", nil)
	if err != nil {
		t.Fatalf("CreateLabel(year) error: %v", err)
	}
	month, err := s.CreateLabel("2026/03", nil)
	if err != nil {
		t.Fatalf("CreateLabel(month) error: %v", err)
	}
	day, err := s.CreateLabel("2026/03/11", nil)
	if err != nil {
		t.Fatalf("CreateLabel(day) error: %v", err)
	}

	dayID, err := s.EnsureDateContextHierarchy(time.Date(2026, time.March, 11, 16, 4, 0, 0, time.FixedZone("CET", 3600)))
	if err != nil {
		t.Fatalf("EnsureDateContextHierarchy() error: %v", err)
	}
	if dayID != day.ID {
		t.Fatalf("EnsureDateContextHierarchy() = %d, want existing day context %d", dayID, day.ID)
	}

	updatedYear := mustGetLabelByName(t, s, "2026")
	updatedMonth := mustGetLabelByName(t, s, "2026/03")
	updatedDay := mustGetLabelByName(t, s, "2026/03/11")
	if updatedYear.ParentID != nil {
		t.Fatalf("year parent_id = %v, want nil", updatedYear.ParentID)
	}
	if updatedMonth.ParentID == nil || *updatedMonth.ParentID != year.ID {
		t.Fatalf("month parent_id = %v, want %d", updatedMonth.ParentID, year.ID)
	}
	if updatedDay.ParentID == nil || *updatedDay.ParentID != month.ID {
		t.Fatalf("day parent_id = %v, want %d", updatedDay.ParentID, month.ID)
	}
}

func TestEnsureDailyWorkspaceLinksDateContextHierarchy(t *testing.T) {
	s := newTestStore(t)

	dirPath := filepath.Join(t.TempDir(), "daily", "2026", "03", "11")
	workspace, err := s.EnsureDailyWorkspace("2026-03-11", dirPath)
	if err != nil {
		t.Fatalf("EnsureDailyWorkspace() error: %v", err)
	}

	year := mustGetLabelByName(t, s, "2026")
	month := mustGetLabelByName(t, s, "2026/03")
	day := mustGetLabelByName(t, s, "2026/03/11")
	if month.ParentID == nil || *month.ParentID != year.ID {
		t.Fatalf("month parent_id = %v, want %d", month.ParentID, year.ID)
	}
	if day.ParentID == nil || *day.ParentID != month.ID {
		t.Fatalf("day parent_id = %v, want %d", day.ParentID, month.ID)
	}
	if !hasContextLink(t, s, "context_workspaces", "workspace_id", workspace.ID, day.ID) {
		t.Fatalf("workspace %d missing date context link %d", workspace.ID, day.ID)
	}

	workspaces, err := s.ListWorkspacesByContextPrefix("2026/03")
	if err != nil {
		t.Fatalf("ListWorkspacesByContextPrefix() error: %v", err)
	}
	if len(workspaces) != 1 || workspaces[0].ID != workspace.ID {
		t.Fatalf("ListWorkspacesByContextPrefix(2026/03) = %+v, want workspace %d", workspaces, workspace.ID)
	}
}

func TestDailyWorkspaceItemsInheritAndRefreshDateContexts(t *testing.T) {
	s := newTestStore(t)

	marchWorkspace, err := s.EnsureDailyWorkspace("2026-03-11", filepath.Join(t.TempDir(), "daily", "2026", "03", "11"))
	if err != nil {
		t.Fatalf("EnsureDailyWorkspace(march) error: %v", err)
	}
	aprilWorkspace, err := s.EnsureDailyWorkspace("2026-04-01", filepath.Join(t.TempDir(), "daily", "2026", "04", "01"))
	if err != nil {
		t.Fatalf("EnsureDailyWorkspace(april) error: %v", err)
	}

	past := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339)
	item, err := s.CreateItem("Daily item", ItemOptions{
		State:        ItemStateInbox,
		WorkspaceID:  &marchWorkspace.ID,
		VisibleAfter: &past,
	})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}

	marchItems, err := s.ListItemsByContextPrefix("2026/03/11")
	if err != nil {
		t.Fatalf("ListItemsByContextPrefix(march) error: %v", err)
	}
	if len(marchItems) != 1 || marchItems[0].ID != item.ID {
		t.Fatalf("ListItemsByContextPrefix(2026/03/11) = %+v, want item %d", marchItems, item.ID)
	}

	if err := s.SetItemWorkspace(item.ID, &aprilWorkspace.ID); err != nil {
		t.Fatalf("SetItemWorkspace(april) error: %v", err)
	}

	marchItems, err = s.ListItemsByContextPrefix("2026/03/11")
	if err != nil {
		t.Fatalf("ListItemsByContextPrefix(march after move) error: %v", err)
	}
	if len(marchItems) != 0 {
		t.Fatalf("ListItemsByContextPrefix(2026/03/11) len = %d, want 0 after move", len(marchItems))
	}

	aprilItems, err := s.ListItemsByContextPrefix("2026/04")
	if err != nil {
		t.Fatalf("ListItemsByContextPrefix(april) error: %v", err)
	}
	if len(aprilItems) != 1 || aprilItems[0].ID != item.ID {
		t.Fatalf("ListItemsByContextPrefix(2026/04) = %+v, want item %d", aprilItems, item.ID)
	}

	if err := s.SetItemWorkspace(item.ID, nil); err != nil {
		t.Fatalf("SetItemWorkspace(nil) error: %v", err)
	}
	aprilItems, err = s.ListItemsByContextPrefix("2026/04")
	if err != nil {
		t.Fatalf("ListItemsByContextPrefix(april after clear) error: %v", err)
	}
	if len(aprilItems) != 0 {
		t.Fatalf("ListItemsByContextPrefix(2026/04) len = %d, want 0 after clear", len(aprilItems))
	}
}

func TestDateAndTopicContextQueriesCanBeCombined(t *testing.T) {
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

	past := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339)
	plasmaItem, err := s.CreateItem("Plasma daily item", ItemOptions{
		State:        ItemStateInbox,
		WorkspaceID:  &plasmaWorkspace.ID,
		VisibleAfter: &past,
	})
	if err != nil {
		t.Fatalf("CreateItem(plasma) error: %v", err)
	}
	_, err = s.CreateItem("Health daily item", ItemOptions{
		State:        ItemStateInbox,
		WorkspaceID:  &healthWorkspace.ID,
		VisibleAfter: &past,
	})
	if err != nil {
		t.Fatalf("CreateItem(health) error: %v", err)
	}

	workspaces, err := s.ListWorkspacesByContextPrefix("2026/03/11 + work/plasma")
	if err != nil {
		t.Fatalf("ListWorkspacesByContextPrefix(combined) error: %v", err)
	}
	if len(workspaces) != 1 || workspaces[0].ID != plasmaWorkspace.ID {
		t.Fatalf("ListWorkspacesByContextPrefix(combined) = %+v, want workspace %d", workspaces, plasmaWorkspace.ID)
	}

	items, err := s.ListItemsByContextPrefix("2026/03/11 + work/plasma")
	if err != nil {
		t.Fatalf("ListItemsByContextPrefix(combined) error: %v", err)
	}
	if len(items) != 1 || items[0].ID != plasmaItem.ID {
		t.Fatalf("ListItemsByContextPrefix(combined) = %+v, want item %d", items, plasmaItem.ID)
	}
}

func mustGetLabelByName(t *testing.T, s *Store, name string) Label {
	t.Helper()
	contextID := contextIDByNameForTest(t, s, name)
	label, err := s.GetLabel(contextID)
	if err != nil {
		t.Fatalf("GetLabel(%q) error: %v", name, err)
	}
	return label
}

func hasContextLink(t *testing.T, s *Store, linkTable, entityColumn string, entityID, contextID int64) bool {
	t.Helper()
	var count int
	query := `SELECT COUNT(1) FROM ` + linkTable + ` WHERE ` + entityColumn + ` = ? AND context_id = ?`
	if err := s.db.QueryRow(query, entityID, contextID).Scan(&count); err != nil {
		t.Fatalf("count context link %s error: %v", linkTable, err)
	}
	return count > 0
}
