package store_test

import (
	"database/sql"
	"errors"
	. "github.com/sloppy-org/sloptools/internal/store"
	_ "modernc.org/sqlite"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

var _ *Store

func TestItemSummaryFiltersByContextIncludingDescendants(t *testing.T) {
	s := newTestStore(t)
	parent, err := s.CreateLabel("Work", nil)
	if err != nil {
		t.Fatalf("CreateLabel(parent) error: %v", err)
	}
	child, err := s.CreateLabel("W7x", &parent.ID)
	if err != nil {
		t.Fatalf("CreateLabel(child) error: %v", err)
	}
	privateCtx, err := s.CreateLabel("Private", nil)
	if err != nil {
		t.Fatalf("CreateLabel(private) error: %v", err)
	}
	workspace, err := s.CreateWorkspace("Alpha", filepath.Join(t.TempDir(), "alpha"))
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	if err := s.LinkLabelToWorkspace(child.ID, workspace.ID); err != nil {
		t.Fatalf("LinkLabelToWorkspace() error: %v", err)
	}
	now := time.Date(2026, time.March, 8, 10, 0, 0, 0, time.UTC)
	past := now.Add(-1 * time.Hour).Format(time.RFC3339)
	workspaceItem, err := s.CreateItem("Workspace child context item", ItemOptions{State: ItemStateInbox, WorkspaceID: &workspace.ID, VisibleAfter: &past})
	if err != nil {
		t.Fatalf("CreateItem(workspace) error: %v", err)
	}
	privateItem, err := s.CreateItem("Private context item", ItemOptions{State: ItemStateInbox, VisibleAfter: &past})
	if err != nil {
		t.Fatalf("CreateItem(private) error: %v", err)
	}
	if err := s.LinkLabelToItem(privateCtx.ID, privateItem.ID); err != nil {
		t.Fatalf("LinkLabelToItem() error: %v", err)
	}
	directChildItem, err := s.CreateItem("Direct child context item", ItemOptions{State: ItemStateInbox, VisibleAfter: &past})
	if err != nil {
		t.Fatalf("CreateItem(direct child) error: %v", err)
	}
	if err := s.LinkLabelToItem(child.ID, directChildItem.ID); err != nil {
		t.Fatalf("LinkLabelToItem(child) error: %v", err)
	}
	parentFilter := ItemListFilter{LabelID: &parent.ID}
	parentItems, err := s.ListInboxItemsFiltered(now, parentFilter)
	if err != nil {
		t.Fatalf("ListInboxItemsFiltered(parent) error: %v", err)
	}
	if len(parentItems) != 2 {
		t.Fatalf("ListInboxItemsFiltered(parent) len = %d, want 2", len(parentItems))
	}
	gotIDs := map[int64]bool{}
	for _, item := range parentItems {
		gotIDs[item.ID] = true
	}
	if !gotIDs[workspaceItem.ID] || !gotIDs[directChildItem.ID] {
		t.Fatalf("ListInboxItemsFiltered(parent) = %+v, want items %d and %d", parentItems, workspaceItem.ID, directChildItem.ID)
	}
	counts, err := s.CountItemsByStateFiltered(now, parentFilter)
	if err != nil {
		t.Fatalf("CountItemsByStateFiltered(parent) error: %v", err)
	}
	if got := counts[ItemStateInbox]; got != 2 {
		t.Fatalf("CountItemsByStateFiltered(parent)[inbox] = %d, want 2", got)
	}
}

func TestFindWorkspaceContainingPathPrefersDeepestMatch(t *testing.T) {
	s := newTestStore(t)
	rootDir := filepath.Join(t.TempDir(), "workspace-root")
	nestedDir := filepath.Join(rootDir, "nested")
	rootWorkspace, err := s.CreateWorkspace("Root", rootDir)
	if err != nil {
		t.Fatalf("CreateWorkspace(root) error: %v", err)
	}
	nestedWorkspace, err := s.CreateWorkspace("Nested", nestedDir)
	if err != nil {
		t.Fatalf("CreateWorkspace(nested) error: %v", err)
	}
	insideNested := filepath.Join(nestedDir, "docs", "note.md")
	gotID, err := s.FindWorkspaceContainingPath(insideNested)
	if err != nil {
		t.Fatalf("FindWorkspaceContainingPath(inside nested) error: %v", err)
	}
	if gotID == nil || *gotID != nestedWorkspace.ID {
		t.Fatalf("FindWorkspaceContainingPath(inside nested) = %v, want %d", gotID, nestedWorkspace.ID)
	}
	insideRootOnly := filepath.Join(rootDir, "readme.md")
	gotID, err = s.FindWorkspaceContainingPath(insideRootOnly)
	if err != nil {
		t.Fatalf("FindWorkspaceContainingPath(inside root) error: %v", err)
	}
	if gotID == nil || *gotID != rootWorkspace.ID {
		t.Fatalf("FindWorkspaceContainingPath(inside root) = %v, want %d", gotID, rootWorkspace.ID)
	}
	gotID, err = s.FindWorkspaceContainingPath(filepath.Join(t.TempDir(), "outside.md"))
	if err != nil {
		t.Fatalf("FindWorkspaceContainingPath(outside) error: %v", err)
	}
	if gotID != nil {
		t.Fatalf("FindWorkspaceContainingPath(outside) = %v, want nil", *gotID)
	}
}

func TestFindWorkspaceByGitRemoteMatchesUniqueWorkspace(t *testing.T) {
	s := newTestStore(t)
	repoA := filepath.Join(t.TempDir(), "workspace-a")
	repoB := filepath.Join(t.TempDir(), "workspace-b")
	repoC := filepath.Join(t.TempDir(), "workspace-c")
	initGitRepoWithRemote(t, repoA, "git@github.com:owner/alpha.git")
	initGitRepoWithRemote(t, repoB, "https://github.com/owner/beta.git")
	initGitRepoWithRemote(t, repoC, "ssh://git@github.com/owner/alpha.git")
	workspaceA, err := s.CreateWorkspace("Alpha A", repoA)
	if err != nil {
		t.Fatalf("CreateWorkspace(alpha a) error: %v", err)
	}
	if _, err := s.CreateWorkspace("Beta", repoB); err != nil {
		t.Fatalf("CreateWorkspace(beta) error: %v", err)
	}
	if _, err := s.CreateWorkspace("Alpha C", repoC); err != nil {
		t.Fatalf("CreateWorkspace(alpha c) error: %v", err)
	}
	gotID, err := s.FindWorkspaceByGitRemote("owner/beta")
	if err != nil {
		t.Fatalf("FindWorkspaceByGitRemote(beta) error: %v", err)
	}
	if gotID == nil {
		t.Fatal("FindWorkspaceByGitRemote(beta) = nil, want workspace ID")
	}
	gotWorkspace, err := s.GetWorkspace(*gotID)
	if err != nil {
		t.Fatalf("GetWorkspace(beta) error: %v", err)
	}
	if gotWorkspace.Name != "Beta" {
		t.Fatalf("FindWorkspaceByGitRemote(beta) picked %q, want Beta", gotWorkspace.Name)
	}
	gotID, err = s.FindWorkspaceByGitRemote("owner/alpha")
	if err != nil {
		t.Fatalf("FindWorkspaceByGitRemote(alpha) error: %v", err)
	}
	if gotID != nil {
		t.Fatalf("FindWorkspaceByGitRemote(alpha) = %v, want nil for ambiguous match", *gotID)
	}
	gotID, err = s.FindWorkspaceByGitRemote("owner/missing")
	if err != nil {
		t.Fatalf("FindWorkspaceByGitRemote(missing) error: %v", err)
	}
	if gotID != nil {
		t.Fatalf("FindWorkspaceByGitRemote(missing) = %v, want nil", *gotID)
	}
	if workspaceA.ID == 0 {
		t.Fatal("expected created workspace ID")
	}
}

func TestGitHubRepoForWorkspace(t *testing.T) {
	s := newTestStore(t)
	repoDir := filepath.Join(t.TempDir(), "repo")
	initGitRepoWithRemote(t, repoDir, "https://github.com/owner/tabula.git")
	workspace, err := s.CreateWorkspace("Repo", repoDir)
	if err != nil {
		t.Fatalf("CreateWorkspace(repo) error: %v", err)
	}
	repo, err := s.GitHubRepoForWorkspace(workspace.ID)
	if err != nil {
		t.Fatalf("GitHubRepoForWorkspace() error: %v", err)
	}
	if repo != "owner/tabula" {
		t.Fatalf("GitHubRepoForWorkspace() = %q, want %q", repo, "owner/tabula")
	}
	missingRemoteDir := filepath.Join(t.TempDir(), "no-remote")
	if err := exec.Command("git", "init", missingRemoteDir).Run(); err != nil {
		t.Fatalf("git init %s: %v", missingRemoteDir, err)
	}
	noRemoteWorkspace, err := s.CreateWorkspace("No Remote", missingRemoteDir)
	if err != nil {
		t.Fatalf("CreateWorkspace(no remote) error: %v", err)
	}
	repo, err = s.GitHubRepoForWorkspace(noRemoteWorkspace.ID)
	if err != nil {
		t.Fatalf("GitHubRepoForWorkspace(no remote) error: %v", err)
	}
	if repo != "" {
		t.Fatalf("GitHubRepoForWorkspace(no remote) = %q, want empty", repo)
	}
}

func TestSourceItemUpsertAndSyncState(t *testing.T) {
	s := newTestStore(t)
	workspaceDir := filepath.Join(t.TempDir(), "workspace")
	workspace, err := s.CreateWorkspace("Workspace", workspaceDir)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	artifactTitle := "Issue #12"
	artifactURL := "https://github.com/owner/tabula/issues/12"
	artifact, err := s.CreateArtifact(ArtifactKindGitHubIssue, nil, &artifactURL, &artifactTitle, nil)
	if err != nil {
		t.Fatalf("CreateArtifact() error: %v", err)
	}
	item, err := s.UpsertItemFromSource("github", "owner/tabula#12", "Initial issue title", &workspace.ID)
	if err != nil {
		t.Fatalf("UpsertItemFromSource(create) error: %v", err)
	}
	if item.State != ItemStateInbox {
		t.Fatalf("created item state = %q, want %q", item.State, ItemStateInbox)
	}
	if err := s.UpdateItemArtifact(item.ID, &artifact.ID); err != nil {
		t.Fatalf("UpdateItemArtifact() error: %v", err)
	}
	gotBySource, err := s.GetItemBySource("github", "owner/tabula#12")
	if err != nil {
		t.Fatalf("GetItemBySource() error: %v", err)
	}
	if gotBySource.ID != item.ID {
		t.Fatalf("GetItemBySource() ID = %d, want %d", gotBySource.ID, item.ID)
	}
	if gotBySource.ArtifactID == nil || *gotBySource.ArtifactID != artifact.ID {
		t.Fatalf("GetItemBySource().ArtifactID = %v, want %d", gotBySource.ArtifactID, artifact.ID)
	}
	updatedItem, err := s.UpsertItemFromSource("github", "owner/tabula#12", "Renamed issue title", nil)
	if err != nil {
		t.Fatalf("UpsertItemFromSource(update) error: %v", err)
	}
	if updatedItem.ID != item.ID {
		t.Fatalf("updated item ID = %d, want %d", updatedItem.ID, item.ID)
	}
	if updatedItem.Title != "Renamed issue title" {
		t.Fatalf("updated title = %q, want %q", updatedItem.Title, "Renamed issue title")
	}
	if updatedItem.WorkspaceID != nil {
		t.Fatalf("updated WorkspaceID = %v, want nil", updatedItem.WorkspaceID)
	}
	items, err := s.ListItemsByState(ItemStateInbox)
	if err != nil {
		t.Fatalf("ListItemsByState(inbox) error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("ListItemsByState(inbox) len = %d, want 1", len(items))
	}
	visibleAfter := "2026-03-10T09:00:00Z"
	followUpAt := "2026-03-10T10:00:00Z"
	if err := s.UpdateItemTimes(item.ID, &visibleAfter, &followUpAt); err != nil {
		t.Fatalf("UpdateItemTimes() error: %v", err)
	}
	if err := s.SyncItemStateBySource("github", "owner/tabula#12", ItemStateDone); err != nil {
		t.Fatalf("SyncItemStateBySource(done) error: %v", err)
	}
	doneItem, err := s.GetItem(item.ID)
	if err != nil {
		t.Fatalf("GetItem(done) error: %v", err)
	}
	if doneItem.State != ItemStateDone {
		t.Fatalf("done item state = %q, want %q", doneItem.State, ItemStateDone)
	}
	if err := s.SyncItemStateBySource("github", "owner/tabula#12", ItemStateInbox); err != nil {
		t.Fatalf("SyncItemStateBySource(reopen) error: %v", err)
	}
	reopenedItem, err := s.GetItem(item.ID)
	if err != nil {
		t.Fatalf("GetItem(reopened) error: %v", err)
	}
	if reopenedItem.State != ItemStateInbox {
		t.Fatalf("reopened item state = %q, want %q", reopenedItem.State, ItemStateInbox)
	}
	if reopenedItem.VisibleAfter != nil || reopenedItem.FollowUpAt != nil {
		t.Fatalf("reopened item timestamps = visible_after:%v follow_up_at:%v, want nil", reopenedItem.VisibleAfter, reopenedItem.FollowUpAt)
	}
	if _, err := s.GetItemBySource("github", "owner/tabula#404"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetItemBySource(missing) error = %v, want sql.ErrNoRows", err)
	}
	if err := s.SyncItemStateBySource("github", "owner/tabula#404", ItemStateDone); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("SyncItemStateBySource(missing) error = %v, want sql.ErrNoRows", err)
	}
}

func TestUpdateItemSource(t *testing.T) {
	s := newTestStore(t)
	item, err := s.CreateItem("Promote me", ItemOptions{})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}
	if err := s.UpdateItemSource(item.ID, "github", "owner/tabula#77"); err != nil {
		t.Fatalf("UpdateItemSource() error: %v", err)
	}
	updated, err := s.GetItem(item.ID)
	if err != nil {
		t.Fatalf("GetItem() error: %v", err)
	}
	if updated.Source == nil || *updated.Source != "github" {
		t.Fatalf("updated.Source = %v, want github", updated.Source)
	}
	if updated.SourceRef == nil || *updated.SourceRef != "owner/tabula#77" {
		t.Fatalf("updated.SourceRef = %v, want owner/tabula#77", updated.SourceRef)
	}
	other, err := s.CreateItem("Other item", ItemOptions{})
	if err != nil {
		t.Fatalf("CreateItem(other) error: %v", err)
	}
	if err := s.UpdateItemSource(other.ID, "github", "owner/tabula#77"); err == nil {
		t.Fatal("expected duplicate source/source_ref error")
	}
	if err := s.UpdateItemSource(9999, "github", "owner/tabula#88"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("UpdateItemSource(missing) error = %v, want sql.ErrNoRows", err)
	}
}

func TestInferWorkspaceForArtifact(t *testing.T) {
	s := newTestStore(t)
	docWorkspaceDir := filepath.Join(t.TempDir(), "docs")
	docWorkspace, err := s.CreateWorkspace("Docs", docWorkspaceDir)
	if err != nil {
		t.Fatalf("CreateWorkspace(docs) error: %v", err)
	}
	repoDir := filepath.Join(t.TempDir(), "repo")
	initGitRepoWithRemote(t, repoDir, "https://github.com/owner/tabula.git")
	repoWorkspace, err := s.CreateWorkspace("Repo", repoDir)
	if err != nil {
		t.Fatalf("CreateWorkspace(repo) error: %v", err)
	}
	docPath := filepath.Join(docWorkspaceDir, "notes", "draft.md")
	docArtifact, err := s.CreateArtifact(ArtifactKindMarkdown, &docPath, nil, nil, nil)
	if err != nil {
		t.Fatalf("CreateArtifact(doc) error: %v", err)
	}
	inferredDoc, err := s.InferWorkspaceForArtifact(docArtifact)
	if err != nil {
		t.Fatalf("InferWorkspaceForArtifact(doc) error: %v", err)
	}
	if inferredDoc == nil || *inferredDoc != docWorkspace.ID {
		t.Fatalf("InferWorkspaceForArtifact(doc) = %v, want %d", inferredDoc, docWorkspace.ID)
	}
	issueURL := "https://github.com/owner/tabula/issues/214"
	ghArtifact, err := s.CreateArtifact(ArtifactKindGitHubIssue, nil, &issueURL, nil, nil)
	if err != nil {
		t.Fatalf("CreateArtifact(github) error: %v", err)
	}
	inferredGitHub, err := s.InferWorkspaceForArtifact(ghArtifact)
	if err != nil {
		t.Fatalf("InferWorkspaceForArtifact(github) error: %v", err)
	}
	if inferredGitHub == nil || *inferredGitHub != repoWorkspace.ID {
		t.Fatalf("InferWorkspaceForArtifact(github) = %v, want %d", inferredGitHub, repoWorkspace.ID)
	}
	metaJSON := `{"source_ref":"owner/tabula#PR-214"}`
	prArtifact, err := s.CreateArtifact(ArtifactKindGitHubPR, nil, nil, nil, &metaJSON)
	if err != nil {
		t.Fatalf("CreateArtifact(github pr) error: %v", err)
	}
	inferredPR, err := s.InferWorkspaceForArtifact(prArtifact)
	if err != nil {
		t.Fatalf("InferWorkspaceForArtifact(github pr) error: %v", err)
	}
	if inferredPR == nil || *inferredPR != repoWorkspace.ID {
		t.Fatalf("InferWorkspaceForArtifact(github pr) = %v, want %d", inferredPR, repoWorkspace.ID)
	}
	unknownURL := "https://github.com/owner/unknown/issues/1"
	unknownArtifact, err := s.CreateArtifact(ArtifactKindGitHubIssue, nil, &unknownURL, nil, nil)
	if err != nil {
		t.Fatalf("CreateArtifact(unknown github) error: %v", err)
	}
	inferredUnknown, err := s.InferWorkspaceForArtifact(unknownArtifact)
	if err != nil {
		t.Fatalf("InferWorkspaceForArtifact(unknown github) error: %v", err)
	}
	if inferredUnknown != nil {
		t.Fatalf("InferWorkspaceForArtifact(unknown github) = %v, want nil", *inferredUnknown)
	}
}

func TestWorkspaceArtifactLinksIncludeLinkedArtifactsInWorkspaceListings(t *testing.T) {
	s := newTestStore(t)
	sourceDir := filepath.Join(t.TempDir(), "source")
	targetDir := filepath.Join(t.TempDir(), "target")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	sourceWorkspace, err := s.CreateWorkspace("Source", sourceDir)
	if err != nil {
		t.Fatalf("CreateWorkspace(source) error: %v", err)
	}
	targetWorkspace, err := s.CreateWorkspace("Target", targetDir)
	if err != nil {
		t.Fatalf("CreateWorkspace(target) error: %v", err)
	}
	sourcePath := filepath.Join(sourceDir, "results.pdf")
	if err := os.WriteFile(sourcePath, []byte("pdf"), 0o644); err != nil {
		t.Fatalf("write results.pdf: %v", err)
	}
	sourceTitle := "results.pdf"
	sourceArtifact, err := s.CreateArtifact(ArtifactKindPDF, &sourcePath, nil, &sourceTitle, nil)
	if err != nil {
		t.Fatalf("CreateArtifact(source) error: %v", err)
	}
	targetPath := filepath.Join(targetDir, "notes.md")
	if err := os.WriteFile(targetPath, []byte("# notes\n"), 0o644); err != nil {
		t.Fatalf("write notes.md: %v", err)
	}
	targetTitle := "notes.md"
	targetArtifact, err := s.CreateArtifact(ArtifactKindMarkdown, &targetPath, nil, &targetTitle, nil)
	if err != nil {
		t.Fatalf("CreateArtifact(target) error: %v", err)
	}
	if err := s.LinkArtifactToWorkspace(targetWorkspace.ID, sourceArtifact.ID); err != nil {
		t.Fatalf("LinkArtifactToWorkspace() error: %v", err)
	}
	if err := s.LinkArtifactToWorkspace(targetWorkspace.ID, sourceArtifact.ID); err != nil {
		t.Fatalf("LinkArtifactToWorkspace(duplicate) error: %v", err)
	}
	links, err := s.ListArtifactWorkspaceLinks(targetWorkspace.ID)
	if err != nil {
		t.Fatalf("ListArtifactWorkspaceLinks() error: %v", err)
	}
	if len(links) != 1 || links[0].ArtifactID != sourceArtifact.ID {
		t.Fatalf("ListArtifactWorkspaceLinks() = %+v, want source artifact %d", links, sourceArtifact.ID)
	}
	linked, err := s.ListLinkedArtifacts(targetWorkspace.ID)
	if err != nil {
		t.Fatalf("ListLinkedArtifacts() error: %v", err)
	}
	if len(linked) != 1 || linked[0].ID != sourceArtifact.ID {
		t.Fatalf("ListLinkedArtifacts() = %+v, want source artifact %d", linked, sourceArtifact.ID)
	}
	targetArtifacts, err := s.ListArtifactsForWorkspace(targetWorkspace.ID)
	if err != nil {
		t.Fatalf("ListArtifactsForWorkspace(target) error: %v", err)
	}
	if len(targetArtifacts) != 2 {
		t.Fatalf("ListArtifactsForWorkspace(target) len = %d, want 2", len(targetArtifacts))
	}
	targetSeen := map[int64]bool{}
	for _, artifact := range targetArtifacts {
		targetSeen[artifact.ID] = true
	}
	if !targetSeen[sourceArtifact.ID] || !targetSeen[targetArtifact.ID] {
		t.Fatalf("ListArtifactsForWorkspace(target) ids = %#v", targetSeen)
	}
	sourceArtifacts, err := s.ListArtifactsForWorkspace(sourceWorkspace.ID)
	if err != nil {
		t.Fatalf("ListArtifactsForWorkspace(source) error: %v", err)
	}
	if len(sourceArtifacts) != 1 || sourceArtifacts[0].ID != sourceArtifact.ID {
		t.Fatalf("ListArtifactsForWorkspace(source) = %+v, want source artifact only", sourceArtifacts)
	}
}
