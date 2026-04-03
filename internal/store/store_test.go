package store

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(filepath.Join(t.TempDir(), "slopshell.db"))
	if err != nil {
		t.Fatalf("store.New() error: %v", err)
	}
	t.Cleanup(func() {
		_ = s.Close()
	})
	return s
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(target)) {
			return true
		}
	}
	return false
}

func TestStoreAdminPasswordAndAuthLifecycle(t *testing.T) {
	s := newTestStore(t)

	if s.HasAdminPassword() {
		t.Fatalf("HasAdminPassword() = true, want false")
	}
	if err := s.SetAdminPassword("short"); err == nil {
		t.Fatalf("expected short password error")
	}
	if err := s.SetAdminPassword("very-strong-pass"); err != nil {
		t.Fatalf("SetAdminPassword() error: %v", err)
	}
	if !s.HasAdminPassword() {
		t.Fatalf("HasAdminPassword() = false, want true")
	}
	if !s.VerifyAdminPassword("very-strong-pass") {
		t.Fatalf("VerifyAdminPassword(correct) = false, want true")
	}
	if s.VerifyAdminPassword("wrong-password") {
		t.Fatalf("VerifyAdminPassword(wrong) = true, want false")
	}

	if err := s.AddAuthSession("tok-1"); err != nil {
		t.Fatalf("AddAuthSession() error: %v", err)
	}
	if !s.HasAuthSession("tok-1") {
		t.Fatalf("HasAuthSession(tok-1) = false, want true")
	}
	if err := s.SetAdminPassword("another-strong-pass"); err != nil {
		t.Fatalf("SetAdminPassword(second) error: %v", err)
	}
	if s.HasAuthSession("tok-1") {
		t.Fatalf("expected auth sessions to be cleared when admin password changes")
	}
	if err := s.AddAuthSession(""); err == nil {
		t.Fatalf("expected AddAuthSession(empty) to fail")
	}
	if err := s.DeleteAuthSession(""); err != nil {
		t.Fatalf("DeleteAuthSession(empty) should be noop: %v", err)
	}
}

func TestStoreHostAndRemoteSessionCRUD(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.AddHost(HostConfig{}); err == nil {
		t.Fatalf("expected AddHost() validation error")
	}

	h2, err := s.AddHost(HostConfig{
		Name:       "zeta",
		Hostname:   "zeta.local",
		Port:       0,
		Username:   "u2",
		KeyPath:    "/tmp/key2",
		ProjectDir: "/tmp/p2",
	})
	if err != nil {
		t.Fatalf("AddHost(zeta) error: %v", err)
	}
	if h2.Port != 22 {
		t.Fatalf("default port = %d, want 22", h2.Port)
	}
	h1, err := s.AddHost(HostConfig{
		Name:       "alpha",
		Hostname:   "alpha.local",
		Port:       2202,
		Username:   "u1",
		KeyPath:    "/tmp/key1",
		ProjectDir: "/tmp/p1",
	})
	if err != nil {
		t.Fatalf("AddHost(alpha) error: %v", err)
	}

	list, err := s.ListHosts()
	if err != nil {
		t.Fatalf("ListHosts() error: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("ListHosts() len = %d, want 2", len(list))
	}
	if list[0].Name != "alpha" || list[1].Name != "zeta" {
		t.Fatalf("ListHosts() should be name-sorted, got %#v", []string{list[0].Name, list[1].Name})
	}

	updated, err := s.UpdateHost(h1.ID, map[string]interface{}{"username": "updated-user", "port": 2222})
	if err != nil {
		t.Fatalf("UpdateHost() error: %v", err)
	}
	if updated.Username != "updated-user" || updated.Port != 2222 {
		t.Fatalf("UpdateHost() did not apply fields: %+v", updated)
	}

	if err := s.AddRemoteSession("sid-1", h1.ID); err != nil {
		t.Fatalf("AddRemoteSession(sid-1) error: %v", err)
	}
	if err := s.AddRemoteSession("sid-2", h2.ID); err != nil {
		t.Fatalf("AddRemoteSession(sid-2) error: %v", err)
	}
	remote, err := s.ListRemoteSessions()
	if err != nil {
		t.Fatalf("ListRemoteSessions() error: %v", err)
	}
	if len(remote) != 2 {
		t.Fatalf("ListRemoteSessions() len = %d, want 2", len(remote))
	}
	if err := s.DeleteRemoteSession("sid-1"); err != nil {
		t.Fatalf("DeleteRemoteSession() error: %v", err)
	}
	remote, err = s.ListRemoteSessions()
	if err != nil {
		t.Fatalf("ListRemoteSessions(after delete) error: %v", err)
	}
	if len(remote) != 1 {
		t.Fatalf("ListRemoteSessions() len after delete = %d, want 1", len(remote))
	}

	if err := s.DeleteHost(h1.ID); err != nil {
		t.Fatalf("DeleteHost() error: %v", err)
	}
	if _, err := s.GetHost(h1.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetHost(deleted) error = %v, want sql.ErrNoRows", err)
	}
}

func TestStoreProjectCompanionConfigPersistsAcrossReopen(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "slopshell.db")
	s, err := New(dbPath)
	if err != nil {
		t.Fatalf("store.New() error: %v", err)
	}

	root := filepath.Join(t.TempDir(), "workspace")
	project, err := s.CreateEnrichedWorkspace("Workspace A", "key-a", root, "managed", "", "", false)
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}
	if err := s.UpdateEnrichedWorkspaceCompanionConfig(workspaceIDString(project.ID), `{"companion_enabled":false,"language":"de","idle_surface":"black"}`); err != nil {
		t.Fatalf("UpdateProjectCompanionConfig() error: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}

	reopened, err := New(dbPath)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer func() {
		_ = reopened.Close()
	}()
	got, err := reopened.GetEnrichedWorkspace(workspaceIDString(project.ID))
	if err != nil {
		t.Fatalf("GetProject() after reopen error: %v", err)
	}
	if strings.TrimSpace(got.CompanionConfigJSON) != `{"companion_enabled":false,"language":"de","idle_surface":"black"}` {
		t.Fatalf("CompanionConfigJSON after reopen = %q", got.CompanionConfigJSON)
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("expected db at %s: %v", dbPath, err)
	}
}

func TestStoreChatSessionMessageAndThreading(t *testing.T) {
	s := newTestStore(t)
	root := filepath.Join(t.TempDir(), "workspace-default")
	project, err := s.CreateEnrichedWorkspace("Default", root, root, "managed", "", "", true)
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}
	if err := s.SetActiveWorkspace(project.ID); err != nil {
		t.Fatalf("SetActiveWorkspace() error: %v", err)
	}

	session, err := s.GetOrCreateChatSession("  ")
	if err != nil {
		t.Fatalf("GetOrCreateChatSession(active workspace) error: %v", err)
	}
	if session.WorkspacePath != project.WorkspacePath {
		t.Fatalf("project key = %q, want %q", session.WorkspacePath, project.WorkspacePath)
	}
	if session.WorkspaceID <= 0 {
		t.Fatalf("workspace_id = %d, want positive id", session.WorkspaceID)
	}
	same, err := s.GetOrCreateChatSession(project.WorkspacePath)
	if err != nil {
		t.Fatalf("GetOrCreateChatSession(existing) error: %v", err)
	}
	if same.ID != session.ID {
		t.Fatalf("expected same chat session id, got %q vs %q", same.ID, session.ID)
	}

	updatedSession, err := s.UpdateChatSessionMode(session.ID, "plan")
	if err != nil {
		t.Fatalf("UpdateChatSessionMode(plan) error: %v", err)
	}
	if updatedSession.Mode != "plan" {
		t.Fatalf("mode = %q, want plan", updatedSession.Mode)
	}
	updatedSession, err = s.UpdateChatSessionMode(session.ID, "review")
	if err != nil {
		t.Fatalf("UpdateChatSessionMode(review) error: %v", err)
	}
	if updatedSession.Mode != "review" {
		t.Fatalf("mode = %q, want review", updatedSession.Mode)
	}
	updatedSession, err = s.UpdateChatSessionMode(session.ID, "not-a-mode")
	if err != nil {
		t.Fatalf("UpdateChatSessionMode(invalid) error: %v", err)
	}
	if updatedSession.Mode != "chat" {
		t.Fatalf("mode = %q, want chat fallback", updatedSession.Mode)
	}

	if err := s.UpdateChatSessionThread(session.ID, "thread-1"); err != nil {
		t.Fatalf("UpdateChatSessionThread() error: %v", err)
	}
	sessionWithThread, err := s.GetChatSession(session.ID)
	if err != nil {
		t.Fatalf("GetChatSession() error: %v", err)
	}
	if sessionWithThread.AppThreadID != "thread-1" {
		t.Fatalf("AppThreadID = %q, want thread-1", sessionWithThread.AppThreadID)
	}

	msg1, err := s.AddChatMessage(session.ID, "invalid-role", "m1", "p1", "canvas")
	if err != nil {
		t.Fatalf("AddChatMessage(msg1) error: %v", err)
	}
	if msg1.Role != "user" {
		t.Fatalf("msg1 role = %q, want user", msg1.Role)
	}
	if msg1.RenderFormat != "text" {
		t.Fatalf("msg1 render format = %q, want text", msg1.RenderFormat)
	}

	msg2, err := s.AddChatMessage(session.ID, "assistant", "m2", "p2", "markdown", WithThreadKey("thread-a"))
	if err != nil {
		t.Fatalf("AddChatMessage(msg2) error: %v", err)
	}
	if msg2.ThreadKey != "thread-a" {
		t.Fatalf("msg2 thread key = %q, want thread-a", msg2.ThreadKey)
	}
	msg3, err := s.AddChatMessage(
		session.ID,
		"assistant",
		"m3",
		"p3",
		"markdown",
		WithThreadKey("thread-b"),
		WithProviderMetadata("OpenAI", "gpt-5.3-codex-spark", 321),
	)
	if err != nil {
		t.Fatalf("AddChatMessage(msg3) error: %v", err)
	}
	if msg3.Provider != "openai" {
		t.Fatalf("msg3 provider = %q, want openai", msg3.Provider)
	}
	if msg3.ProviderModel != "gpt-5.3-codex-spark" {
		t.Fatalf("msg3 provider_model = %q, want gpt-5.3-codex-spark", msg3.ProviderModel)
	}
	if msg3.ProviderLatency != 321 {
		t.Fatalf("msg3 provider_latency = %d, want 321", msg3.ProviderLatency)
	}

	defaultThreadMessages, err := s.ListChatMessages(session.ID, 10)
	if err != nil {
		t.Fatalf("ListChatMessages(default thread) error: %v", err)
	}
	if len(defaultThreadMessages) != 1 {
		t.Fatalf("default thread message count = %d, want 1", len(defaultThreadMessages))
	}

	threadMessages, err := s.ListChatMessages(session.ID, 10, WithThreadKey("thread-a"))
	if err != nil {
		t.Fatalf("ListChatMessages(thread-a) error: %v", err)
	}
	if len(threadMessages) != 1 || threadMessages[0].ID != msg2.ID {
		t.Fatalf("thread-a message selection mismatch")
	}
	threadMessages, err = s.ListChatMessages(session.ID, 10, WithThreadKey("thread-b"))
	if err != nil {
		t.Fatalf("ListChatMessages(thread-b) error: %v", err)
	}
	if len(threadMessages) != 1 || threadMessages[0].ID != msg3.ID {
		t.Fatalf("thread-b message selection mismatch")
	}
	if threadMessages[0].Provider != "openai" {
		t.Fatalf("thread-b provider = %q, want openai", threadMessages[0].Provider)
	}

	if err := s.UpdateChatMessageContent(msg2.ID, "m2-updated", "p2-updated", "canvas", WithProviderMetadata("local", "qwen3.5-9b", 27)); err != nil {
		t.Fatalf("UpdateChatMessageContent() error: %v", err)
	}
	threadMessages, err = s.ListChatMessages(session.ID, 10, WithThreadKey("thread-a"))
	if err != nil {
		t.Fatalf("ListChatMessages(thread-a after update) error: %v", err)
	}
	if threadMessages[0].RenderFormat != "text" {
		t.Fatalf("updated message render format = %q, want text", threadMessages[0].RenderFormat)
	}
	if threadMessages[0].Provider != "local" {
		t.Fatalf("updated message provider = %q, want local", threadMessages[0].Provider)
	}
	if threadMessages[0].ProviderModel != "qwen3.5-9b" {
		t.Fatalf("updated message provider_model = %q, want qwen3.5-9b", threadMessages[0].ProviderModel)
	}
	if threadMessages[0].ProviderLatency != 27 {
		t.Fatalf("updated message provider_latency = %d, want 27", threadMessages[0].ProviderLatency)
	}
	if err := s.UpdateChatMessageContent(0, "x", "y", "markdown"); err == nil {
		t.Fatalf("expected invalid message id validation error")
	}

	if err := s.AddChatEvent(session.ID, "turn-1", "turn_started", `{"ok":true}`); err != nil {
		t.Fatalf("AddChatEvent() error: %v", err)
	}

	if err := s.ResetChatSessionThread(session.ID); err != nil {
		t.Fatalf("ResetChatSessionThread() error: %v", err)
	}
	sessionReset, err := s.GetChatSession(session.ID)
	if err != nil {
		t.Fatalf("GetChatSession(after reset) error: %v", err)
	}
	if sessionReset.AppThreadID != "" {
		t.Fatalf("expected AppThreadID to be cleared")
	}
	if err := s.ResetAllChatSessionThreads(); err != nil {
		t.Fatalf("ResetAllChatSessionThreads() error: %v", err)
	}

	if err := s.ClearChatMessages(session.ID); err != nil {
		t.Fatalf("ClearChatMessages() error: %v", err)
	}
	remaining, err := s.ListChatMessages(session.ID, 10)
	if err != nil {
		t.Fatalf("ListChatMessages(after clear) error: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("remaining messages = %d, want 0", len(remaining))
	}
	if err := s.ClearAllChatMessages(); err != nil {
		t.Fatalf("ClearAllChatMessages() error: %v", err)
	}
	if err := s.ClearAllChatEvents(); err != nil {
		t.Fatalf("ClearAllChatEvents() error: %v", err)
	}
}

func TestStoreChatSessionsKeyToWorkspace(t *testing.T) {
	s := newTestStore(t)
	root := filepath.Join(t.TempDir(), "workspace-alpha")
	project, err := s.CreateEnrichedWorkspace("Alpha", "alpha-key", root, "managed", "", "", false)
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}

	session, err := s.GetOrCreateChatSession(project.WorkspacePath)
	if err != nil {
		t.Fatalf("GetOrCreateChatSession() error: %v", err)
	}
	if session.WorkspaceID <= 0 {
		t.Fatalf("workspace_id = %d, want positive id", session.WorkspaceID)
	}

	columns, err := s.tableColumnNames("chat_sessions")
	if err != nil {
		t.Fatalf("tableColumnNames(chat_sessions) error: %v", err)
	}
	if containsString(columns, "workspace_path") {
		t.Fatalf("chat_sessions columns still include workspace_path: %v", columns)
	}
	if !containsString(columns, "workspace_id") {
		t.Fatalf("chat_sessions columns missing workspace_id: %v", columns)
	}

	byWorkspace, err := s.GetChatSessionByWorkspaceID(session.WorkspaceID)
	if err != nil {
		t.Fatalf("GetChatSessionByWorkspaceID() error: %v", err)
	}
	if byWorkspace.ID != session.ID {
		t.Fatalf("GetChatSessionByWorkspaceID() = %q, want %q", byWorkspace.ID, session.ID)
	}

	var sessionCount int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM chat_sessions WHERE workspace_id = ?`, session.WorkspaceID).Scan(&sessionCount); err != nil {
		t.Fatalf("count chat_sessions by workspace_id: %v", err)
	}
	if sessionCount != 1 {
		t.Fatalf("chat session count for workspace %d = %d, want 1", session.WorkspaceID, sessionCount)
	}
}

func TestGetOrCreateChatSessionBlankRefRequiresActiveWorkspace(t *testing.T) {
	s := newTestStore(t)
	root := filepath.Join(t.TempDir(), "workspace-default")
	project, err := s.CreateEnrichedWorkspace("Default", root, root, "managed", "", "", true)
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}
	if err := s.SetActiveWorkspaceID(workspaceIDString(project.ID)); err != nil {
		t.Fatalf("SetActiveWorkspaceID() error: %v", err)
	}
	if _, err := s.db.Exec(`UPDATE workspaces SET is_active = 0`); err != nil {
		t.Fatalf("clear active workspace: %v", err)
	}

	if _, err := s.GetOrCreateChatSession("  "); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetOrCreateChatSession(blank) error = %v, want sql.ErrNoRows", err)
	}
}

func TestGetOrCreateChatSessionCreatesWorkspaceForLegacyProjectFallback(t *testing.T) {
	s := newTestStore(t)
	projectRoot := filepath.Join(t.TempDir(), "project-root")
	project, err := s.CreateEnrichedWorkspace("Alpha", "alpha-key", projectRoot, "managed", "", "", false)
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}

	session, err := s.GetOrCreateChatSession(project.WorkspacePath)
	if err != nil {
		t.Fatalf("GetOrCreateChatSession() error: %v", err)
	}
	if session.WorkspaceID <= 0 {
		t.Fatalf("workspace_id = %d, want positive id", session.WorkspaceID)
	}
	workspace, err := s.GetWorkspace(session.WorkspaceID)
	if err != nil {
		t.Fatalf("GetWorkspace() error: %v", err)
	}
	if workspace.DirPath != project.RootPath {
		t.Fatalf("workspace dir_path = %q, want %q", workspace.DirPath, project.RootPath)
	}
}

func TestStoreSchemaAndHelperNormalizers(t *testing.T) {
	s := newTestStore(t)

	columns, err := s.TableColumns()
	if err != nil {
		t.Fatalf("TableColumns() error: %v", err)
	}
	if len(columns) == 0 {
		t.Fatalf("TableColumns() should not be empty")
	}
	chatMessageCols := strings.Join(columns["chat_messages"], ",")
	if !strings.Contains(chatMessageCols, "thread_key") {
		t.Fatalf("chat_messages missing thread_key column: %q", chatMessageCols)
	}
	if !strings.Contains(chatMessageCols, "provider") || !strings.Contains(chatMessageCols, "provider_model") || !strings.Contains(chatMessageCols, "provider_latency_ms") {
		t.Fatalf("chat_messages missing provider columns: %q", chatMessageCols)
	}
	workspaceCols := strings.Join(columns["workspaces"], ",")
	if !strings.Contains(workspaceCols, "canvas_session_id") {
		t.Fatalf("workspaces missing canvas_session_id column: %q", workspaceCols)
	}
	if !strings.Contains(workspaceCols, "chat_model_reasoning_effort") {
		t.Fatalf("workspaces missing chat_model_reasoning_effort column: %q", workspaceCols)
	}

	if got := normalizeWorkspaceCompatName("  hello  "); got != "hello" {
		t.Fatalf("normalizeWorkspaceCompatName() = %q, want hello", got)
	}
	if got := normalizeWorkspaceCompatChatModel("  Spark "); got != "spark" {
		t.Fatalf("normalizeWorkspaceCompatChatModel() = %q, want spark", got)
	}
	if got := normalizeWorkspaceCompatChatModelReasoningEffort(" High "); got != "high" {
		t.Fatalf("normalizeWorkspaceCompatChatModelReasoningEffort() = %q, want high", got)
	}
	if got := normalizeChatMode("plan"); got != "plan" {
		t.Fatalf("normalizeChatMode(plan) = %q, want plan", got)
	}
	if got := normalizeChatMode("other"); got != "chat" {
		t.Fatalf("normalizeChatMode(default) = %q, want chat", got)
	}
	if got := normalizeChatRole("assistant"); got != "assistant" {
		t.Fatalf("normalizeChatRole(assistant) = %q, want assistant", got)
	}
	if got := normalizeChatRole("weird"); got != "user" {
		t.Fatalf("normalizeChatRole(default) = %q, want user", got)
	}
	if got := normalizeRenderFormat("canvas"); got != "text" {
		t.Fatalf("normalizeRenderFormat(canvas) = %q, want text", got)
	}
	if got := normalizeRenderFormat("unknown"); got != "markdown" {
		t.Fatalf("normalizeRenderFormat(default) = %q, want markdown", got)
	}
	if got := stringsJoin([]string{"a", "b", "c"}, ","); got != "a,b,c" {
		t.Fatalf("stringsJoin() = %q, want a,b,c", got)
	}
	if got := boolToInt(true); got != 1 {
		t.Fatalf("boolToInt(true) = %d, want 1", got)
	}
	if got := boolToInt(false); got != 0 {
		t.Fatalf("boolToInt(false) = %d, want 0", got)
	}
}

func TestStoreDeleteProjectRemovesAssociatedSessions(t *testing.T) {
	s := newTestStore(t)
	root := filepath.Join(t.TempDir(), "meeting-temp")
	project, err := s.CreateEnrichedWorkspace("Meeting Temp", "meeting-key", root, "meeting", "", "", false)
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}
	if err := s.SetActiveWorkspaceID(workspaceIDString(project.ID)); err != nil {
		t.Fatalf("SetActiveWorkspaceID() error: %v", err)
	}
	chatSession, err := s.GetOrCreateChatSession(project.WorkspacePath)
	if err != nil {
		t.Fatalf("GetOrCreateChatSession() error: %v", err)
	}
	if _, err := s.AddChatMessage(chatSession.ID, "assistant", "saved output", "saved output", "markdown"); err != nil {
		t.Fatalf("AddChatMessage() error: %v", err)
	}
	if err := s.AddChatEvent(chatSession.ID, "turn-1", "assistant_output", `{"ok":true}`); err != nil {
		t.Fatalf("AddChatEvent() error: %v", err)
	}
	participantSession, err := s.AddParticipantSession(project.WorkspacePath, "{}")
	if err != nil {
		t.Fatalf("AddParticipantSession() error: %v", err)
	}
	if _, err := s.AddParticipantSegment(ParticipantSegment{
		SessionID: participantSession.ID,
		StartTS:   100,
		EndTS:     101,
		Text:      "text artifact only",
		Status:    "final",
	}); err != nil {
		t.Fatalf("AddParticipantSegment() error: %v", err)
	}
	if err := s.AddParticipantEvent(participantSession.ID, 0, "segment_committed", `{"text":"text artifact only"}`); err != nil {
		t.Fatalf("AddParticipantEvent() error: %v", err)
	}
	if err := s.UpsertParticipantRoomState(participantSession.ID, "summary", `["Acme"]`, `["Decision"]`); err != nil {
		t.Fatalf("UpsertParticipantRoomState() error: %v", err)
	}

	if err := s.DeleteEnrichedWorkspace(workspaceIDString(project.ID)); err != nil {
		t.Fatalf("DeleteProject() error: %v", err)
	}
	if _, err := s.GetEnrichedWorkspace(workspaceIDString(project.ID)); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetProject(deleted) error = %v, want sql.ErrNoRows", err)
	}
	if _, err := s.GetChatSession(chatSession.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetChatSession(deleted) error = %v, want sql.ErrNoRows", err)
	}
	if _, err := s.GetParticipantSession(participantSession.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetParticipantSession(deleted) error = %v, want sql.ErrNoRows", err)
	}
	activeID, err := s.ActiveWorkspaceID()
	if err != nil {
		t.Fatalf("ActiveWorkspaceID() error: %v", err)
	}
	if activeID != "" {
		t.Fatalf("ActiveWorkspaceID() = %q, want empty", activeID)
	}
}
