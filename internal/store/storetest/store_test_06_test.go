package store_test

import (
	"database/sql"
	"errors"
	. "github.com/sloppy-org/sloptools/internal/store"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

var _ *Store

func TestExternalBindingStoreListsMissingContainerRef(t *testing.T) {
	s := newTestStore(t)
	account, err := s.CreateExternalAccount(SphereWork, ExternalProviderExchangeEWS, "Work Exchange", map[string]any{"endpoint": "https://exchange.example.com/EWS/Exchange.asmx", "username": "alice@example.com"})
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}
	item, err := s.CreateItem("Follow up", ItemOptions{})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}
	missing, err := s.UpsertExternalBinding(ExternalBinding{AccountID: account.ID, Provider: ExternalProviderExchangeEWS, ObjectType: "email", RemoteID: "msg-missing", ItemID: &item.ID})
	if err != nil {
		t.Fatalf("UpsertExternalBinding(missing) error: %v", err)
	}
	containerRef := "Posteingang"
	present, err := s.UpsertExternalBinding(ExternalBinding{AccountID: account.ID, Provider: ExternalProviderExchangeEWS, ObjectType: "email", RemoteID: "msg-present", ItemID: &item.ID, ContainerRef: &containerRef})
	if err != nil {
		t.Fatalf("UpsertExternalBinding(present) error: %v", err)
	}
	results, err := s.ListBindingsMissingContainerRef(account.ID, ExternalProviderExchangeEWS, "email", 10)
	if err != nil {
		t.Fatalf("ListBindingsMissingContainerRef() error: %v", err)
	}
	if len(results) != 1 || results[0].ID != missing.ID {
		t.Fatalf("ListBindingsMissingContainerRef() = %+v, want missing binding only", results)
	}
	if results[0].ContainerRef != nil {
		t.Fatalf("missing binding container_ref = %v, want nil", results[0].ContainerRef)
	}
	if present.ContainerRef == nil || *present.ContainerRef != containerRef {
		t.Fatalf("present binding container_ref = %v, want %q", present.ContainerRef, containerRef)
	}
}

func TestApplyExternalBindingReconcileUpdatesRewritesRemoteIDAndState(t *testing.T) {
	s := newTestStore(t)
	account, err := s.CreateExternalAccount(SphereWork, ExternalProviderExchangeEWS, "Work Exchange", map[string]any{"endpoint": "https://exchange.example.com/EWS/Exchange.asmx", "username": "alice@example.com"})
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}
	item, err := s.CreateItem("Follow up", ItemOptions{})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}
	title := "Mail"
	artifact, err := s.CreateArtifact(ArtifactKindEmail, nil, nil, &title, nil)
	if err != nil {
		t.Fatalf("CreateArtifact() error: %v", err)
	}
	containerRef := "Posteingang"
	if _, err := s.UpsertExternalBinding(ExternalBinding{AccountID: account.ID, Provider: ExternalProviderExchangeEWS, ObjectType: "email", RemoteID: "msg-1", ItemID: &item.ID, ArtifactID: &artifact.ID, ContainerRef: &containerRef}); err != nil {
		t.Fatalf("UpsertExternalBinding() error: %v", err)
	}
	trashRef := "Gelöschte Elemente"
	doneState := ItemStateDone
	if err := s.ApplyExternalBindingReconcileUpdates(account.ID, ExternalProviderExchangeEWS, []ExternalBindingReconcileUpdate{{ObjectType: "email", OldRemoteID: "msg-1", NewRemoteID: "msg-1-trash", ContainerRef: &trashRef, FollowUpItemState: &doneState}}); err != nil {
		t.Fatalf("ApplyExternalBindingReconcileUpdates() error: %v", err)
	}
	if _, err := s.GetBindingByRemote(account.ID, ExternalProviderExchangeEWS, "email", "msg-1"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("old GetBindingByRemote() error = %v, want sql.ErrNoRows", err)
	}
	binding, err := s.GetBindingByRemote(account.ID, ExternalProviderExchangeEWS, "email", "msg-1-trash")
	if err != nil {
		t.Fatalf("GetBindingByRemote(new) error: %v", err)
	}
	if binding.ContainerRef == nil || *binding.ContainerRef != trashRef {
		t.Fatalf("binding container_ref = %v, want %q", binding.ContainerRef, trashRef)
	}
	updatedItem, err := s.GetItem(item.ID)
	if err != nil {
		t.Fatalf("GetItem() error: %v", err)
	}
	if updatedItem.State != ItemStateDone {
		t.Fatalf("item state = %q, want %q", updatedItem.State, ItemStateDone)
	}
}

func TestExternalBindingStoreRejectsInvalidInput(t *testing.T) {
	s := newTestStore(t)
	account, err := s.CreateExternalAccount(SphereWork, ExternalProviderIMAP, "Work IMAP", map[string]any{"host": "imap.example.com", "port": 993, "username": "alice@example.com"})
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}
	if _, err := s.UpsertExternalBinding(ExternalBinding{}); err == nil {
		t.Fatal("expected missing account error")
	}
	if _, err := s.UpsertExternalBinding(ExternalBinding{AccountID: account.ID, Provider: ExternalProviderGmail, ObjectType: "email", RemoteID: "msg-1"}); err == nil {
		t.Fatal("expected provider mismatch error")
	}
	if _, err := s.UpsertExternalBinding(ExternalBinding{AccountID: account.ID, Provider: ExternalProviderIMAP, RemoteID: "msg-1"}); err == nil {
		t.Fatal("expected missing object_type error")
	}
	if _, err := s.UpsertExternalBinding(ExternalBinding{AccountID: account.ID, Provider: ExternalProviderIMAP, ObjectType: "email"}); err == nil {
		t.Fatal("expected missing remote_id error")
	}
	badRemoteUpdatedAt := "tomorrow morning"
	if _, err := s.UpsertExternalBinding(ExternalBinding{AccountID: account.ID, Provider: ExternalProviderIMAP, ObjectType: "email", RemoteID: "msg-1", RemoteUpdatedAt: &badRemoteUpdatedAt}); err == nil {
		t.Fatal("expected invalid remote_updated_at error")
	}
	if _, err := s.GetBindingByRemote(account.ID, "", "email", "msg-1"); err == nil {
		t.Fatal("expected missing provider lookup error")
	}
	if _, err := s.GetBindingByRemote(account.ID, ExternalProviderIMAP, "", "msg-1"); err == nil {
		t.Fatal("expected missing object_type lookup error")
	}
	if _, err := s.LatestBindingRemoteUpdatedAt(account.ID, ExternalProviderIMAP, ""); err == nil {
		t.Fatal("expected missing object_type latest lookup error")
	}
	if _, err := s.GetBindingByRemote(account.ID, ExternalProviderIMAP, "email", ""); err == nil {
		t.Fatal("expected missing remote_id lookup error")
	}
	if _, err := s.ListStaleBindings("smtp", time.Now()); err == nil {
		t.Fatal("expected invalid stale provider error")
	}
	if err := s.DeleteBinding(999999); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("DeleteBinding(missing) error = %v, want sql.ErrNoRows", err)
	}
}

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

func TestStoreFileLineLimits(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(filename)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", dir, err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		lines := strings.Count(string(data), "\n")
		if len(data) > 0 && data[len(data)-1] != '\n' {
			lines++
		}
		if lines > 1000 {
			t.Fatalf("%s has %d lines, want <= 1000", name, lines)
		}
	}
}

func TestUpdateItemReviewDispatch(t *testing.T) {
	s := newTestStore(t)
	item, err := s.CreateItem("Review parser PR", ItemOptions{})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}
	target := ItemReviewTargetGitHub
	reviewer := "alice"
	if err := s.UpdateItemReviewDispatch(item.ID, &target, &reviewer); err != nil {
		t.Fatalf("UpdateItemReviewDispatch() error: %v", err)
	}
	got, err := s.GetItem(item.ID)
	if err != nil {
		t.Fatalf("GetItem() error: %v", err)
	}
	if got.ReviewTarget == nil || *got.ReviewTarget != target {
		t.Fatalf("review_target = %v, want %q", got.ReviewTarget, target)
	}
	if got.Reviewer == nil || *got.Reviewer != reviewer {
		t.Fatalf("reviewer = %v, want %q", got.Reviewer, reviewer)
	}
	if got.ReviewedAt == nil || *got.ReviewedAt == "" {
		t.Fatalf("reviewed_at = %v, want timestamp", got.ReviewedAt)
	}
	if err := s.UpdateItemReviewDispatch(item.ID, nil, nil); err != nil {
		t.Fatalf("UpdateItemReviewDispatch(clear) error: %v", err)
	}
	cleared, err := s.GetItem(item.ID)
	if err != nil {
		t.Fatalf("GetItem(clear) error: %v", err)
	}
	if cleared.ReviewTarget != nil || cleared.Reviewer != nil || cleared.ReviewedAt != nil {
		t.Fatalf("cleared dispatch = %+v", cleared)
	}
}

func TestUpdateItemReviewDispatchRejectsReviewerWithoutTarget(t *testing.T) {
	s := newTestStore(t)
	item, err := s.CreateItem("Review parser PR", ItemOptions{})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}
	reviewer := "alice"
	if err := s.UpdateItemReviewDispatch(item.ID, nil, &reviewer); err == nil {
		t.Fatal("expected reviewer-without-target error")
	}
}

func TestStoreAuthHelpersHandleMissingAndDeletedSessions(t *testing.T) {
	s := newTestStore(t)
	if s.VerifyAdminPassword("anything") {
		t.Fatal("VerifyAdminPassword() = true, want false before password is set")
	}
	if s.HasAuthSession("") {
		t.Fatal("HasAuthSession(\"\") = true, want false")
	}
	if s.HasAuthSession("missing") {
		t.Fatal("HasAuthSession(missing) = true, want false")
	}
	if err := s.AddAuthSession("tok-edge"); err != nil {
		t.Fatalf("AddAuthSession() error: %v", err)
	}
	if err := s.DeleteAuthSession("tok-edge"); err != nil {
		t.Fatalf("DeleteAuthSession() error: %v", err)
	}
	if s.HasAuthSession("tok-edge") {
		t.Fatal("HasAuthSession(tok-edge) = true, want false after delete")
	}
}

func TestStoreUpdateHostNoopAndUnknownFieldsKeepExistingRecord(t *testing.T) {
	s := newTestStore(t)
	original, err := s.AddHost(HostConfig{Name: "alpha", Hostname: "alpha.local", Port: 2202, Username: "u1", KeyPath: "/tmp/key1", ProjectDir: "/tmp/p1"})
	if err != nil {
		t.Fatalf("AddHost() error: %v", err)
	}
	unchanged, err := s.UpdateHost(original.ID, map[string]interface{}{})
	if err != nil {
		t.Fatalf("UpdateHost(empty) error: %v", err)
	}
	if unchanged != original {
		t.Fatalf("UpdateHost(empty) = %+v, want %+v", unchanged, original)
	}
	ignored, err := s.UpdateHost(original.ID, map[string]interface{}{"unsupported": "value"})
	if err != nil {
		t.Fatalf("UpdateHost(unsupported) error: %v", err)
	}
	if ignored != original {
		t.Fatalf("UpdateHost(unsupported) = %+v, want %+v", ignored, original)
	}
}

func TestBatchRunLifecycle(t *testing.T) {
	s := newTestStore(t)
	workspace, err := s.CreateWorkspace("Batch Workspace", filepath.Join(t.TempDir(), "workspace"))
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	itemA, err := s.CreateItem("Fix login flow", ItemOptions{WorkspaceID: &workspace.ID})
	if err != nil {
		t.Fatalf("CreateItem(itemA) error: %v", err)
	}
	itemB, err := s.CreateItem("Repair flaky spec", ItemOptions{WorkspaceID: &workspace.ID})
	if err != nil {
		t.Fatalf("CreateItem(itemB) error: %v", err)
	}
	run, err := s.CreateBatchRun(workspace.ID, `{"worker":"codex","reviewer":"human"}`, "running")
	if err != nil {
		t.Fatalf("CreateBatchRun() error: %v", err)
	}
	if run.WorkspaceID != workspace.ID {
		t.Fatalf("CreateBatchRun() workspace_id = %d, want %d", run.WorkspaceID, workspace.ID)
	}
	if run.Status != "running" {
		t.Fatalf("CreateBatchRun() status = %q, want running", run.Status)
	}
	prNumber := int64(42)
	prURL := "https://example.test/pr/42"
	first, err := s.UpsertBatchRunItem(run.ID, itemA.ID, BatchRunItemUpdate{Status: "done", PRNumber: &prNumber, PRURL: &prURL})
	if err != nil {
		t.Fatalf("UpsertBatchRunItem(done) error: %v", err)
	}
	if first.PRNumber == nil || *first.PRNumber != prNumber {
		t.Fatalf("done item pr_number = %v, want %d", first.PRNumber, prNumber)
	}
	if first.PRURL == nil || *first.PRURL != prURL {
		t.Fatalf("done item pr_url = %v, want %q", first.PRURL, prURL)
	}
	if first.StartedAt == nil || first.FinishedAt == nil {
		t.Fatalf("done item timestamps = %+v, want both started_at and finished_at", first)
	}
	errorMsg := "CI failed on review job"
	second, err := s.UpsertBatchRunItem(run.ID, itemB.ID, BatchRunItemUpdate{Status: "failed", ErrorMsg: &errorMsg})
	if err != nil {
		t.Fatalf("UpsertBatchRunItem(failed) error: %v", err)
	}
	if second.ErrorMsg == nil || *second.ErrorMsg != errorMsg {
		t.Fatalf("failed item error_msg = %v, want %q", second.ErrorMsg, errorMsg)
	}
	items, err := s.ListBatchRunItems(run.ID)
	if err != nil {
		t.Fatalf("ListBatchRunItems() error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("ListBatchRunItems() len = %d, want 2", len(items))
	}
	if items[0].ItemTitle == nil || *items[0].ItemTitle == "" {
		t.Fatalf("ListBatchRunItems() missing item title: %+v", items[0])
	}
	finishedAt := "2026-03-09T10:15:00Z"
	run, err = s.SetBatchRunStatus(run.ID, "completed", &finishedAt)
	if err != nil {
		t.Fatalf("SetBatchRunStatus() error: %v", err)
	}
	if run.Status != "completed" {
		t.Fatalf("SetBatchRunStatus() status = %q, want completed", run.Status)
	}
	if run.FinishedAt == nil || *run.FinishedAt != finishedAt {
		t.Fatalf("SetBatchRunStatus() finished_at = %v, want %q", run.FinishedAt, finishedAt)
	}
	listed, err := s.ListBatchRuns(&workspace.ID)
	if err != nil {
		t.Fatalf("ListBatchRuns() error: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != run.ID {
		t.Fatalf("ListBatchRuns() = %+v, want batch %d", listed, run.ID)
	}
}

func TestBatchRunValidationAndEmptyBatch(t *testing.T) {
	s := newTestStore(t)
	workspace, err := s.CreateWorkspace("Batch Workspace", filepath.Join(t.TempDir(), "workspace"))
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	if _, err := s.CreateBatchRun(workspace.ID, `{`, "running"); err == nil {
		t.Fatal("CreateBatchRun() with invalid config_json unexpectedly succeeded")
	}
	run, err := s.CreateBatchRun(workspace.ID, "", "")
	if err != nil {
		t.Fatalf("CreateBatchRun(defaults) error: %v", err)
	}
	if run.ConfigJSON != "{}" {
		t.Fatalf("CreateBatchRun(defaults) config_json = %q, want {}", run.ConfigJSON)
	}
	if run.Status != "running" {
		t.Fatalf("CreateBatchRun(defaults) status = %q, want running", run.Status)
	}
	items, err := s.ListBatchRunItems(run.ID)
	if err != nil {
		t.Fatalf("ListBatchRunItems(empty) error: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("ListBatchRunItems(empty) len = %d, want 0", len(items))
	}
	if _, err := s.UpsertBatchRunItem(run.ID, 9999, BatchRunItemUpdate{Status: "running"}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("UpsertBatchRunItem(missing item) error = %v, want sql.ErrNoRows", err)
	}
}
