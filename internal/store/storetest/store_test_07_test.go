package store_test

import (
	. "github.com/sloppy-org/sloptools/internal/store"
	_ "modernc.org/sqlite"
	"path/filepath"
	"testing"
	"time"
)

var _ *Store

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
	item, err := s.CreateItem("Daily item", ItemOptions{State: ItemStateInbox, WorkspaceID: &marchWorkspace.ID, VisibleAfter: &past})
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
	plasmaItem, err := s.CreateItem("Plasma daily item", ItemOptions{State: ItemStateInbox, WorkspaceID: &plasmaWorkspace.ID, VisibleAfter: &past})
	if err != nil {
		t.Fatalf("CreateItem(plasma) error: %v", err)
	}
	_, err = s.CreateItem("Health daily item", ItemOptions{State: ItemStateInbox, WorkspaceID: &healthWorkspace.ID, VisibleAfter: &past})
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
	if err := s.DB().QueryRow(query, entityID, contextID).Scan(&count); err != nil {
		t.Fatalf("count context link %s error: %v", linkTable, err)
	}
	return count > 0
}

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
	workspaceItem, err := s.CreateItem("Workspace context item", ItemOptions{State: ItemStateInbox, WorkspaceID: &workspace.ID, VisibleAfter: &past})
	if err != nil {
		t.Fatalf("CreateItem(workspace) error: %v", err)
	}
	privateItem, err := s.CreateItem("Private context item", ItemOptions{State: ItemStateInbox, VisibleAfter: &past})
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

func TestExternalProviderHelpers(t *testing.T) {
	t.Parallel()
	cases := []struct {
		provider     string
		email        bool
		managedEmail bool
		calendar     bool
		task         bool
		displayName  string
	}{{provider: " Gmail ", email: true, managedEmail: true, displayName: "Gmail"}, {provider: ExternalProviderIMAP, email: true, displayName: "IMAP"}, {provider: ExternalProviderExchange, email: true, managedEmail: true, displayName: "Exchange"}, {provider: ExternalProviderExchangeEWS, email: true, managedEmail: true, calendar: true, task: true, displayName: "Exchange EWS"}, {provider: ExternalProviderGoogleCalendar, calendar: true, displayName: "Google Calendar"}, {provider: ExternalProviderICS, calendar: true, displayName: "ICS"}, {provider: ExternalProviderTodoist, task: true, displayName: "Todoist"}, {provider: ExternalProviderEvernote, displayName: "Evernote"}, {provider: ExternalProviderBear, displayName: "Bear"}, {provider: ExternalProviderZotero, displayName: "Zotero"}, {provider: " custom ", displayName: "custom"}}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.displayName, func(t *testing.T) {
			t.Parallel()
			if got := IsEmailProvider(tc.provider); got != tc.email {
				t.Fatalf("IsEmailProvider(%q) = %t, want %t", tc.provider, got, tc.email)
			}
			if got := IsManagedEmailProvider(tc.provider); got != tc.managedEmail {
				t.Fatalf("IsManagedEmailProvider(%q) = %t, want %t", tc.provider, got, tc.managedEmail)
			}
			if got := IsCalendarProvider(tc.provider); got != tc.calendar {
				t.Fatalf("IsCalendarProvider(%q) = %t, want %t", tc.provider, got, tc.calendar)
			}
			if got := IsTaskProvider(tc.provider); got != tc.task {
				t.Fatalf("IsTaskProvider(%q) = %t, want %t", tc.provider, got, tc.task)
			}
			if got := ExternalProviderDisplayName(tc.provider); got != tc.displayName {
				t.Fatalf("ExternalProviderDisplayName(%q) = %q, want %q", tc.provider, got, tc.displayName)
			}
		})
	}
}
