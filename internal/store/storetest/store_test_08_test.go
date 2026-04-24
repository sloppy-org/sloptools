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

func TestWorkspaceRuntimeUpdates(t *testing.T) {
	s := newTestStore(t)
	if err := s.UpdateWorkspaceChatModel(0, "spark"); err == nil {
		t.Fatal("UpdateWorkspaceChatModel(0) unexpectedly succeeded")
	}
	if err := s.UpdateWorkspaceChatModelReasoningEffort(0, "low"); err == nil {
		t.Fatal("UpdateWorkspaceChatModelReasoningEffort(0) unexpectedly succeeded")
	}
	if err := s.UpdateWorkspaceChatModel(9999, "spark"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("UpdateWorkspaceChatModel(missing) error = %v, want sql.ErrNoRows", err)
	}
	if err := s.UpdateWorkspaceChatModelReasoningEffort(9999, "low"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("UpdateWorkspaceChatModelReasoningEffort(missing) error = %v, want sql.ErrNoRows", err)
	}
	workspace, err := s.CreateWorkspace("Runtime", filepath.Join(t.TempDir(), "runtime"))
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	if err := s.UpdateWorkspaceChatModel(workspace.ID, " GPT-5.4 "); err != nil {
		t.Fatalf("UpdateWorkspaceChatModel() error: %v", err)
	}
	if err := s.UpdateWorkspaceChatModelReasoningEffort(workspace.ID, " HIGH "); err != nil {
		t.Fatalf("UpdateWorkspaceChatModelReasoningEffort(high) error: %v", err)
	}
	updated, err := s.GetWorkspace(workspace.ID)
	if err != nil {
		t.Fatalf("GetWorkspace(updated) error: %v", err)
	}
	if updated.ChatModel != "gpt" {
		t.Fatalf("ChatModel = %q, want gpt", updated.ChatModel)
	}
	if updated.ChatModelReasoningEffort != "high" {
		t.Fatalf("ChatModelReasoningEffort = %q, want high", updated.ChatModelReasoningEffort)
	}
	if err := s.UpdateWorkspaceChatModel(workspace.ID, " "); err != nil {
		t.Fatalf("UpdateWorkspaceChatModel(default) error: %v", err)
	}
	if err := s.UpdateWorkspaceChatModelReasoningEffort(workspace.ID, "turbo"); err != nil {
		t.Fatalf("UpdateWorkspaceChatModelReasoningEffort(invalid) error: %v", err)
	}
	updated, err = s.GetWorkspace(workspace.ID)
	if err != nil {
		t.Fatalf("GetWorkspace(reset) error: %v", err)
	}
	if updated.ChatModel != "local" {
		t.Fatalf("ChatModel = %q, want local", updated.ChatModel)
	}
	if updated.ChatModelReasoningEffort != "none" {
		t.Fatalf("ChatModelReasoningEffort = %q, want none", updated.ChatModelReasoningEffort)
	}
}

func TestListLabelsIncludesRootBeforeChild(t *testing.T) {
	s := newTestStore(t)
	parent, err := s.CreateLabel("Home", nil)
	if err != nil {
		t.Fatalf("CreateLabel(parent) error: %v", err)
	}
	_, err = s.CreateLabel("Alpha", nil)
	if err != nil {
		t.Fatalf("CreateLabel(root) error: %v", err)
	}
	child, err := s.CreateLabel("Chores", &parent.ID)
	if err != nil {
		t.Fatalf("CreateLabel(child) error: %v", err)
	}
	labels, err := s.ListLabels()
	if err != nil {
		t.Fatalf("ListLabels() error: %v", err)
	}
	indexByID := make(map[int64]int, len(labels))
	labelByID := make(map[int64]Label, len(labels))
	for i, label := range labels {
		indexByID[label.ID] = i
		labelByID[label.ID] = label
	}
	if got := labelByID[parent.ID]; got.ParentID != nil {
		t.Fatalf("parent label ParentID = %v, want nil", got.ParentID)
	}
	if got := labelByID[child.ID]; got.ParentID == nil || *got.ParentID != parent.ID {
		t.Fatalf("child label ParentID = %v, want %d", got.ParentID, parent.ID)
	}
	if indexByID[parent.ID] >= indexByID[child.ID] {
		t.Fatalf("parent index = %d, child index = %d, want parent before child", indexByID[parent.ID], indexByID[child.ID])
	}
}

func TestDeleteMailTriageReviewRemovesRecord(t *testing.T) {
	s := newTestStore(t)
	account, err := s.CreateExternalAccount(SphereWork, ExternalProviderExchangeEWS, "TU Graz", nil)
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}
	review, err := s.CreateMailTriageReview(MailTriageReviewInput{AccountID: account.ID, Provider: account.Provider, MessageID: "m-delete", Folder: "Posteingang", Subject: "Delete me", Sender: "alice@example.com", Action: "archive"})
	if err != nil {
		t.Fatalf("CreateMailTriageReview() error: %v", err)
	}
	if err := s.DeleteMailTriageReview(0); err == nil {
		t.Fatal("DeleteMailTriageReview(0) unexpectedly succeeded")
	}
	if err := s.DeleteMailTriageReview(review.ID); err != nil {
		t.Fatalf("DeleteMailTriageReview() error: %v", err)
	}
	if _, err := s.GetMailTriageReview(review.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetMailTriageReview(deleted) error = %v, want sql.ErrNoRows", err)
	}
}

func TestChatSessionLookupByWorkspacePathAndList(t *testing.T) {
	s := newTestStore(t)
	firstPath := filepath.Join(t.TempDir(), "workspace-one")
	firstWorkspace, err := s.CreateEnrichedWorkspace("Workspace One", firstPath, firstPath, "managed", "", "", false)
	if err != nil {
		t.Fatalf("CreateEnrichedWorkspace(first) error: %v", err)
	}
	secondPath := filepath.Join(t.TempDir(), "workspace-two")
	secondWorkspace, err := s.CreateEnrichedWorkspace("Workspace Two", secondPath, secondPath, "linked", "", "", false)
	if err != nil {
		t.Fatalf("CreateEnrichedWorkspace(second) error: %v", err)
	}
	firstSession, err := s.GetOrCreateChatSessionForWorkspace(firstWorkspace.ID)
	if err != nil {
		t.Fatalf("GetOrCreateChatSessionForWorkspace(first) error: %v", err)
	}
	secondSession, err := s.GetOrCreateChatSessionForWorkspace(secondWorkspace.ID)
	if err != nil {
		t.Fatalf("GetOrCreateChatSessionForWorkspace(second) error: %v", err)
	}
	byPath, err := s.GetChatSessionByWorkspacePath(firstWorkspace.WorkspacePath)
	if err != nil {
		t.Fatalf("GetChatSessionByWorkspacePath() error: %v", err)
	}
	if byPath.ID != firstSession.ID {
		t.Fatalf("GetChatSessionByWorkspacePath() id = %q, want %q", byPath.ID, firstSession.ID)
	}
	sessions, err := s.ListChatSessions()
	if err != nil {
		t.Fatalf("ListChatSessions() error: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("ListChatSessions() len = %d, want 2", len(sessions))
	}
	sessionByID := make(map[string]ChatSession, len(sessions))
	for _, session := range sessions {
		sessionByID[session.ID] = session
	}
	if got := sessionByID[firstSession.ID]; got.WorkspacePath != firstWorkspace.WorkspacePath {
		t.Fatalf("first session WorkspacePath = %q, want %q", got.WorkspacePath, firstWorkspace.WorkspacePath)
	}
	if got := sessionByID[secondSession.ID]; got.WorkspacePath != secondWorkspace.WorkspacePath {
		t.Fatalf("second session WorkspacePath = %q, want %q", got.WorkspacePath, secondWorkspace.WorkspacePath)
	}
	if _, err := s.GetChatSessionByWorkspacePath(filepath.Join(t.TempDir(), "missing")); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetChatSessionByWorkspacePath(missing) error = %v, want sql.ErrNoRows", err)
	}
}

func TestWorkspaceCompatLookupHelpers(t *testing.T) {
	s := newTestStore(t)
	rootPath := filepath.Join(t.TempDir(), "compat-root")
	workspace, err := s.CreateEnrichedWorkspace("Compat", "compat-key", rootPath, "managed", "https://mcp.test", "canvas-1", true)
	if err != nil {
		t.Fatalf("CreateEnrichedWorkspace() error: %v", err)
	}
	workspaces, err := s.ListEnrichedWorkspaces()
	if err != nil {
		t.Fatalf("ListEnrichedWorkspaces() error: %v", err)
	}
	if len(workspaces) != 1 {
		t.Fatalf("ListEnrichedWorkspaces() len = %d, want 1", len(workspaces))
	}
	if workspaces[0].ID != workspace.ID {
		t.Fatalf("ListEnrichedWorkspaces()[0].ID = %d, want %d", workspaces[0].ID, workspace.ID)
	}
	if workspaces[0].Kind != "managed" {
		t.Fatalf("ListEnrichedWorkspaces()[0].Kind = %q, want managed", workspaces[0].Kind)
	}
	byRoot, err := s.GetWorkspaceByRootPath(rootPath)
	if err != nil {
		t.Fatalf("GetWorkspaceByRootPath() error: %v", err)
	}
	if byRoot.ID != workspace.ID {
		t.Fatalf("GetWorkspaceByRootPath() = %d, want %d", byRoot.ID, workspace.ID)
	}
	byCanvas, err := s.GetWorkspaceByCanvasSession("canvas-1")
	if err != nil {
		t.Fatalf("GetWorkspaceByCanvasSession() error: %v", err)
	}
	if byCanvas.ID != workspace.ID {
		t.Fatalf("GetWorkspaceByCanvasSession() = %d, want %d", byCanvas.ID, workspace.ID)
	}
	activeID, err := s.ActiveWorkspaceID()
	if err != nil {
		t.Fatalf("ActiveWorkspaceID() error: %v", err)
	}
	if activeID != WorkspaceIDString(workspace.ID) {
		t.Fatalf("ActiveWorkspaceID() = %q, want %q", activeID, WorkspaceIDString(workspace.ID))
	}
	activeWorkspace, err := s.ActiveEnrichedWorkspace()
	if err != nil {
		t.Fatalf("ActiveEnrichedWorkspace() error: %v", err)
	}
	if activeWorkspace.ID != workspace.ID {
		t.Fatalf("ActiveEnrichedWorkspace() = %d, want %d", activeWorkspace.ID, workspace.ID)
	}
	listed, err := s.ListWorkspacesForID(WorkspaceIDString(workspace.ID))
	if err != nil {
		t.Fatalf("ListWorkspacesForID() error: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != workspace.ID {
		t.Fatalf("ListWorkspacesForID() = %+v, want [%d]", listed, workspace.ID)
	}
	noOp, err := s.SetWorkspaceNoOp(workspace.ID, nil)
	if err != nil {
		t.Fatalf("SetWorkspaceNoOp() error: %v", err)
	}
	if noOp.ID != workspace.ID {
		t.Fatalf("SetWorkspaceNoOp() = %d, want %d", noOp.ID, workspace.ID)
	}
	foundID, err := s.FindWorkspaceByPath(filepath.Join(rootPath, "nested", "file.txt"))
	if err != nil {
		t.Fatalf("FindWorkspaceByPath() error: %v", err)
	}
	if foundID == nil || *foundID != workspace.ID {
		t.Fatalf("FindWorkspaceByPath() = %v, want %d", foundID, workspace.ID)
	}
	if got := s.AppServerModelProfileForWorkspacePath(workspace.WorkspacePath); got != "local" {
		t.Fatalf("AppServerModelProfileForWorkspacePath() = %q, want local", got)
	}
}

func TestWorkspaceCompatUpdateOperations(t *testing.T) {
	s := newTestStore(t)
	rootPath := filepath.Join(t.TempDir(), "compat-update")
	workspace, err := s.CreateEnrichedWorkspace("Before", "before-key", rootPath, "workspace", "", "", false)
	if err != nil {
		t.Fatalf("CreateEnrichedWorkspace() error: %v", err)
	}
	workspaceID := WorkspaceIDString(workspace.ID)
	if err := s.TouchWorkspace(workspaceID); err != nil {
		t.Fatalf("TouchWorkspace() error: %v", err)
	}
	if _, err := s.UpdateWorkspaceMCPURL(workspace.ID, " ws://127.0.0.1:9420/mcp "); err != nil {
		t.Fatalf("UpdateWorkspaceMCPURL() error: %v", err)
	}
	if _, err := s.UpdateWorkspaceCanvasSession(workspace.ID, " local "); err != nil {
		t.Fatalf("UpdateWorkspaceCanvasSession() error: %v", err)
	}
	if err := s.UpdateWorkspaceTransport(workspaceID, "http://mcp-2.test", "canvas-2"); err != nil {
		t.Fatalf("UpdateWorkspaceTransport() error: %v", err)
	}
	if err := s.UpdateWorkspaceRuntime(workspaceID, "http://mcp-3.test", "canvas-3"); err != nil {
		t.Fatalf("UpdateWorkspaceRuntime() error: %v", err)
	}
	if err := s.UpdateEnrichedWorkspaceMCPURL(workspaceID, "http://mcp-4.test"); err != nil {
		t.Fatalf("UpdateEnrichedWorkspaceMCPURL() error: %v", err)
	}
	if err := s.UpdateEnrichedWorkspaceCanvasSession(workspaceID, "canvas-4"); err != nil {
		t.Fatalf("UpdateEnrichedWorkspaceCanvasSession() error: %v", err)
	}
	if err := s.UpdateEnrichedWorkspaceChatModel(workspaceID, " GPT "); err != nil {
		t.Fatalf("UpdateEnrichedWorkspaceChatModel() error: %v", err)
	}
	if err := s.UpdateEnrichedWorkspaceChatModelReasoningEffort(workspaceID, " medium "); err != nil {
		t.Fatalf("UpdateEnrichedWorkspaceChatModelReasoningEffort() error: %v", err)
	}
	if err := s.UpdateWorkspaceKind(workspaceID, " Meeting "); err != nil {
		t.Fatalf("UpdateWorkspaceKind() error: %v", err)
	}
	renamedRoot := filepath.Join(rootPath, "renamed")
	if err := s.RenameWorkspace(workspaceID, " After ", " after-key ", renamedRoot, " linked "); err != nil {
		t.Fatalf("RenameWorkspace() error: %v", err)
	}
	finalRoot := filepath.Join(rootPath, "final")
	if err := s.UpdateWorkspaceLocation2(workspaceID, " Final ", " final-key ", finalRoot, " task "); err != nil {
		t.Fatalf("UpdateWorkspaceLocation2() error: %v", err)
	}
	updated, err := s.GetEnrichedWorkspace(workspaceID)
	if err != nil {
		t.Fatalf("GetEnrichedWorkspace() error: %v", err)
	}
	if updated.Name != "Final" {
		t.Fatalf("Name = %q, want Final", updated.Name)
	}
	if updated.WorkspacePath != "final-key" {
		t.Fatalf("WorkspacePath = %q, want final-key", updated.WorkspacePath)
	}
	if updated.RootPath != finalRoot {
		t.Fatalf("RootPath = %q, want %q", updated.RootPath, finalRoot)
	}
	if updated.DirPath != finalRoot {
		t.Fatalf("DirPath = %q, want %q", updated.DirPath, finalRoot)
	}
	if updated.Kind != "task" {
		t.Fatalf("Kind = %q, want task", updated.Kind)
	}
	if updated.MCPURL != "http://mcp-4.test" {
		t.Fatalf("MCPURL = %q, want http://mcp-4.test", updated.MCPURL)
	}
	if updated.CanvasSessionID != "canvas-4" {
		t.Fatalf("CanvasSessionID = %q, want canvas-4", updated.CanvasSessionID)
	}
	if updated.ChatModel != "gpt" {
		t.Fatalf("ChatModel = %q, want gpt", updated.ChatModel)
	}
	if updated.ChatModelReasoningEffort != "medium" {
		t.Fatalf("ChatModelReasoningEffort = %q, want medium", updated.ChatModelReasoningEffort)
	}
	if err := s.UpdateWorkspaceTransport("bad", "", ""); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("UpdateWorkspaceTransport(invalid) error = %v, want sql.ErrNoRows", err)
	}
	if err := s.UpdateEnrichedWorkspaceChatModel("bad", "spark"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("UpdateEnrichedWorkspaceChatModel(invalid) error = %v, want sql.ErrNoRows", err)
	}
}

func TestWorkspaceCompatSmallHelpers(t *testing.T) {
	if got := ParseWorkspaceTimestamp("2026-03-21T15:04:05Z"); got != 1774105445 {
		t.Fatalf("ParseWorkspaceTimestamp(RFC3339) = %d, want 1774105445", got)
	}
	if got := ParseWorkspaceTimestamp("2026-03-21 15:04:05"); got != 1774105445 {
		t.Fatalf("ParseWorkspaceTimestamp(SQLite) = %d, want 1774105445", got)
	}
	if got := ParseWorkspaceTimestamp("not-a-time"); got != 0 {
		t.Fatalf("ParseWorkspaceTimestamp(invalid) = %d, want 0", got)
	}
	if got := InvalidWorkspaceIDError("  nope ").Error(); got != "invalid workspace id: nope" {
		t.Fatalf("InvalidWorkspaceIDError() = %q, want %q", got, "invalid workspace id: nope")
	}
}

func TestStoreMigratesLegacyPrimaryArtifactIntoItemArtifactLinks(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "slopshell.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error: %v", err)
	}
	schema := `
CREATE TABLE workspaces (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  dir_path TEXT NOT NULL UNIQUE,
  is_active INTEGER NOT NULL DEFAULT 0,
  is_daily INTEGER NOT NULL DEFAULT 0,
  daily_date TEXT,
  mcp_url TEXT NOT NULL DEFAULT '',
  canvas_session_id TEXT NOT NULL DEFAULT '',
  chat_model TEXT NOT NULL DEFAULT '',
  chat_model_reasoning_effort TEXT NOT NULL DEFAULT '',
  companion_config_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE TABLE actors (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  kind TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE TABLE artifacts (
  id INTEGER PRIMARY KEY,
  kind TEXT NOT NULL,
  ref_path TEXT,
  ref_url TEXT,
  title TEXT,
  meta_json TEXT,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE TABLE items (
  id INTEGER PRIMARY KEY,
  title TEXT NOT NULL,
  state TEXT NOT NULL DEFAULT 'inbox' CHECK (state IN ('inbox', 'waiting', 'someday', 'done')),
  workspace_id INTEGER REFERENCES workspaces(id) ON DELETE SET NULL,
  artifact_id INTEGER REFERENCES artifacts(id) ON DELETE SET NULL,
  actor_id INTEGER REFERENCES actors(id) ON DELETE SET NULL,
  visible_after TEXT,
  follow_up_at TEXT,
  source TEXT,
  source_ref TEXT,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
INSERT INTO artifacts (id, kind, title) VALUES (7, 'markdown', 'Legacy note');
INSERT INTO items (id, title, artifact_id) VALUES (9, 'Legacy item', 7);
`
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		t.Fatalf("seed schema: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close seeded db: %v", err)
	}
	s, err := New(dbPath)
	if err != nil {
		t.Fatalf("store.New() error: %v", err)
	}
	t.Cleanup(func() {
		_ = s.Close()
	})
	links, err := s.ListItemArtifactLinks(9)
	if err != nil {
		t.Fatalf("ListItemArtifactLinks() error: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("ListItemArtifactLinks() len = %d, want 1", len(links))
	}
	if links[0].ArtifactID != 7 || links[0].Role != "source" {
		t.Fatalf("ListItemArtifactLinks() = %+v, want source link to artifact 7", links)
	}
}
