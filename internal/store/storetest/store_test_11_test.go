package store_test

import (
	"database/sql"
	"errors"
	. "github.com/sloppy-org/sloptools/internal/store"
	_ "modernc.org/sqlite"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var _ *Store

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
	msg3, err := s.AddChatMessage(session.ID, "assistant", "m3", "p3", "markdown", WithThreadKey("thread-b"), WithProviderMetadata("OpenAI", "gpt-5.3-codex-spark", 321))
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
	columns, err := s.TableColumnNames("chat_sessions")
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
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM chat_sessions WHERE workspace_id = ?`, session.WorkspaceID).Scan(&sessionCount); err != nil {
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
	if err := s.SetActiveWorkspaceID(WorkspaceIDString(project.ID)); err != nil {
		t.Fatalf("SetActiveWorkspaceID() error: %v", err)
	}
	if _, err := s.DB().Exec(`UPDATE workspaces SET is_active = 0`); err != nil {
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
	if got := NormalizeWorkspaceCompatName("  hello  "); got != "hello" {
		t.Fatalf("NormalizeWorkspaceCompatName() = %q, want hello", got)
	}
	if got := NormalizeWorkspaceCompatChatModel("  Spark "); got != "spark" {
		t.Fatalf("NormalizeWorkspaceCompatChatModel() = %q, want spark", got)
	}
	if got := NormalizeWorkspaceCompatChatModelReasoningEffort(" High "); got != "high" {
		t.Fatalf("NormalizeWorkspaceCompatChatModelReasoningEffort() = %q, want high", got)
	}
	if got := NormalizeChatMode("plan"); got != "plan" {
		t.Fatalf("NormalizeChatMode(plan) = %q, want plan", got)
	}
	if got := NormalizeChatMode("other"); got != "chat" {
		t.Fatalf("NormalizeChatMode(default) = %q, want chat", got)
	}
	if got := NormalizeChatRole("assistant"); got != "assistant" {
		t.Fatalf("NormalizeChatRole(assistant) = %q, want assistant", got)
	}
	if got := NormalizeChatRole("weird"); got != "user" {
		t.Fatalf("NormalizeChatRole(default) = %q, want user", got)
	}
	if got := NormalizeRenderFormat("canvas"); got != "text" {
		t.Fatalf("NormalizeRenderFormat(canvas) = %q, want text", got)
	}
	if got := NormalizeRenderFormat("unknown"); got != "markdown" {
		t.Fatalf("NormalizeRenderFormat(default) = %q, want markdown", got)
	}
	if got := StringsJoin([]string{"a", "b", "c"}, ","); got != "a,b,c" {
		t.Fatalf("StringsJoin() = %q, want a,b,c", got)
	}
	if got := BoolToInt(true); got != 1 {
		t.Fatalf("BoolToInt(true) = %d, want 1", got)
	}
	if got := BoolToInt(false); got != 0 {
		t.Fatalf("BoolToInt(false) = %d, want 0", got)
	}
}

func TestStoreDeleteProjectRemovesAssociatedSessions(t *testing.T) {
	s := newTestStore(t)
	root := filepath.Join(t.TempDir(), "meeting-temp")
	project, err := s.CreateEnrichedWorkspace("Meeting Temp", "meeting-key", root, "meeting", "", "", false)
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}
	if err := s.SetActiveWorkspaceID(WorkspaceIDString(project.ID)); err != nil {
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
	if _, err := s.AddParticipantSegment(ParticipantSegment{SessionID: participantSession.ID, StartTS: 100, EndTS: 101, Text: "text artifact only", Status: "final"}); err != nil {
		t.Fatalf("AddParticipantSegment() error: %v", err)
	}
	if err := s.AddParticipantEvent(participantSession.ID, 0, "segment_committed", `{"text":"text artifact only"}`); err != nil {
		t.Fatalf("AddParticipantEvent() error: %v", err)
	}
	if err := s.UpsertParticipantRoomState(participantSession.ID, "summary", `["Acme"]`, `["Decision"]`); err != nil {
		t.Fatalf("UpsertParticipantRoomState() error: %v", err)
	}
	if err := s.DeleteEnrichedWorkspace(WorkspaceIDString(project.ID)); err != nil {
		t.Fatalf("DeleteProject() error: %v", err)
	}
	if _, err := s.GetEnrichedWorkspace(WorkspaceIDString(project.ID)); !errors.Is(err, sql.ErrNoRows) {
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
	workspaceSummary, err := s.SummarizeTimeEntries(TimeEntryListFilter{From: &start, To: &end}, "workspace", end)
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
