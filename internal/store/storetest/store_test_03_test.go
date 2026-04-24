package store_test

import (
	"database/sql"
	"errors"
	"fmt"
	. "github.com/sloppy-org/sloptools/internal/store"
	_ "modernc.org/sqlite"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

var _ *Store

func TestSphereInheritanceAndMutators(t *testing.T) {
	s := newTestStore(t)
	if got, err := s.ActiveSphere(); err != nil {
		t.Fatalf("ActiveSphere() error: %v", err)
	} else if got != SpherePrivate {
		t.Fatalf("default ActiveSphere() = %q, want %q", got, SpherePrivate)
	}
	if err := s.SetActiveSphere(SphereWork); err != nil {
		t.Fatalf("SetActiveSphere() error: %v", err)
	}
	workspace, err := s.CreateWorkspace("Work", filepath.Join(t.TempDir(), "work"), SphereWork)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	if _, err := s.CreateWorkspace("Bad", filepath.Join(t.TempDir(), "bad"), "office"); err == nil {
		t.Fatal("expected invalid workspace sphere error")
	}
	item, err := s.CreateItem("Capture", ItemOptions{})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}
	if item.Sphere != SphereWork {
		t.Fatalf("CreateItem().Sphere = %q, want %q", item.Sphere, SphereWork)
	}
	workspaceItem, err := s.CreateItem("Workspace item", ItemOptions{WorkspaceID: &workspace.ID})
	if err != nil {
		t.Fatalf("CreateItem(workspace) error: %v", err)
	}
	if workspaceItem.Sphere != SphereWork {
		t.Fatalf("CreateItem(workspace).Sphere = %q, want %q", workspaceItem.Sphere, SphereWork)
	}
	if err := s.SetItemSphere(item.ID, SpherePrivate); err != nil {
		t.Fatalf("SetItemSphere() error: %v", err)
	}
	if updated, err := s.GetItem(item.ID); err != nil {
		t.Fatalf("GetItem(updated) error: %v", err)
	} else if updated.Sphere != SpherePrivate {
		t.Fatalf("SetItemSphere().Sphere = %q, want %q", updated.Sphere, SpherePrivate)
	}
	if err := s.SetItemSphere(workspaceItem.ID, SpherePrivate); err == nil {
		t.Fatal("expected workspace-backed item sphere error")
	}
	if _, err := s.SetWorkspaceSphere(workspace.ID, SpherePrivate); err != nil {
		t.Fatalf("SetWorkspaceSphere() error: %v", err)
	}
	if refreshed, err := s.GetItem(workspaceItem.ID); err != nil {
		t.Fatalf("GetItem(workspaceItem) error: %v", err)
	} else if refreshed.Sphere != SpherePrivate {
		t.Fatalf("workspace-backed item sphere = %q, want %q", refreshed.Sphere, SpherePrivate)
	}
	if _, err := s.AddSphereAccount(SphereWork, ExternalProviderGmail, "Work Gmail", map[string]any{"username": "alice@example.com"}); err != nil {
		t.Fatalf("AddSphereAccount() error: %v", err)
	}
	accounts, err := s.ListSphereAccounts(SphereWork)
	if err != nil {
		t.Fatalf("ListSphereAccounts() error: %v", err)
	}
	if len(accounts) != 1 {
		t.Fatalf("ListSphereAccounts() len = %d, want 1", len(accounts))
	}
	if err := s.RemoveSphereAccount(accounts[0].ID); err != nil {
		t.Fatalf("RemoveSphereAccount() error: %v", err)
	}
}

func TestDomainConcurrentWorkspaceCreates(t *testing.T) {
	s := newTestStore(t)
	const count = 12
	baseDir := t.TempDir()
	errCh := make(chan error, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.CreateWorkspace("Workspace", filepath.Join(baseDir, fmt.Sprintf("workspace-%02d", i)))
			errCh <- err
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("CreateWorkspace() concurrent error: %v", err)
		}
	}
	workspaces, err := s.ListWorkspaces()
	if err != nil {
		t.Fatalf("ListWorkspaces() error: %v", err)
	}
	if len(workspaces) != count {
		t.Fatalf("ListWorkspaces() len = %d, want %d", len(workspaces), count)
	}
}

func TestItemTriageOperations(t *testing.T) {
	s := newTestStore(t)
	actor, err := s.CreateActor("Codex", ActorKindAgent)
	if err != nil {
		t.Fatalf("CreateActor() error: %v", err)
	}
	laterItem, err := s.CreateItem("Later item", ItemOptions{})
	if err != nil {
		t.Fatalf("CreateItem(later) error: %v", err)
	}
	visibleAfter := "2026-03-10T09:00:00Z"
	if err := s.TriageItemLater(laterItem.ID, visibleAfter); err != nil {
		t.Fatalf("TriageItemLater() error: %v", err)
	}
	gotLater, err := s.GetItem(laterItem.ID)
	if err != nil {
		t.Fatalf("GetItem(later) error: %v", err)
	}
	if gotLater.State != ItemStateWaiting {
		t.Fatalf("later state = %q, want %q", gotLater.State, ItemStateWaiting)
	}
	if gotLater.VisibleAfter == nil || *gotLater.VisibleAfter != visibleAfter {
		t.Fatalf("later visible_after = %v, want %q", gotLater.VisibleAfter, visibleAfter)
	}
	delegateItem, err := s.CreateItem("Delegate item", ItemOptions{VisibleAfter: &visibleAfter})
	if err != nil {
		t.Fatalf("CreateItem(delegate) error: %v", err)
	}
	if err := s.TriageItemDelegate(delegateItem.ID, actor.ID); err != nil {
		t.Fatalf("TriageItemDelegate() error: %v", err)
	}
	gotDelegate, err := s.GetItem(delegateItem.ID)
	if err != nil {
		t.Fatalf("GetItem(delegate) error: %v", err)
	}
	if gotDelegate.State != ItemStateWaiting {
		t.Fatalf("delegate state = %q, want %q", gotDelegate.State, ItemStateWaiting)
	}
	if gotDelegate.ActorID == nil || *gotDelegate.ActorID != actor.ID {
		t.Fatalf("delegate actor = %v, want %d", gotDelegate.ActorID, actor.ID)
	}
	if gotDelegate.VisibleAfter != nil {
		t.Fatalf("delegate visible_after = %v, want nil", gotDelegate.VisibleAfter)
	}
	somedayItem, err := s.CreateItem("Someday item", ItemOptions{ActorID: &actor.ID, VisibleAfter: &visibleAfter, FollowUpAt: &visibleAfter})
	if err != nil {
		t.Fatalf("CreateItem(someday) error: %v", err)
	}
	if err := s.TriageItemSomeday(somedayItem.ID); err != nil {
		t.Fatalf("TriageItemSomeday() error: %v", err)
	}
	gotSomeday, err := s.GetItem(somedayItem.ID)
	if err != nil {
		t.Fatalf("GetItem(someday) error: %v", err)
	}
	if gotSomeday.State != ItemStateSomeday {
		t.Fatalf("someday state = %q, want %q", gotSomeday.State, ItemStateSomeday)
	}
	if gotSomeday.ActorID == nil || *gotSomeday.ActorID != actor.ID {
		t.Fatalf("someday actor = %v, want %d", gotSomeday.ActorID, actor.ID)
	}
	if gotSomeday.VisibleAfter != nil || gotSomeday.FollowUpAt != nil {
		t.Fatalf("someday timestamps = visible_after:%v follow_up_at:%v, want nil", gotSomeday.VisibleAfter, gotSomeday.FollowUpAt)
	}
	doneItem, err := s.CreateItem("Done item", ItemOptions{})
	if err != nil {
		t.Fatalf("CreateItem(done) error: %v", err)
	}
	if err := s.TriageItemDone(doneItem.ID); err != nil {
		t.Fatalf("TriageItemDone() error: %v", err)
	}
	gotDone, err := s.GetItem(doneItem.ID)
	if err != nil {
		t.Fatalf("GetItem(done) error: %v", err)
	}
	if gotDone.State != ItemStateDone {
		t.Fatalf("done state = %q, want %q", gotDone.State, ItemStateDone)
	}
	deleteItem, err := s.CreateItem("Delete me", ItemOptions{})
	if err != nil {
		t.Fatalf("CreateItem(delete) error: %v", err)
	}
	if err := s.TriageItemDelete(deleteItem.ID); err != nil {
		t.Fatalf("TriageItemDelete() error: %v", err)
	}
	if _, err := s.GetItem(deleteItem.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetItem(deleted) error = %v, want sql.ErrNoRows", err)
	}
	if err := s.TriageItemLater(laterItem.ID, "tomorrow morning"); err == nil {
		t.Fatal("expected invalid visible_after error")
	}
	if err := s.TriageItemDelegate(999999, actor.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("TriageItemDelegate(missing item) error = %v, want sql.ErrNoRows", err)
	}
	if err := s.TriageItemDelegate(laterItem.ID, 999999); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("TriageItemDelegate(missing actor) error = %v, want sql.ErrNoRows", err)
	}
	if err := s.TriageItemSomeday(doneItem.ID); err == nil {
		t.Fatal("expected done item triage rejection")
	}
}

func TestUpdateItemStateInboxClearsDeferredTimes(t *testing.T) {
	s := newTestStore(t)
	visibleAfter := "2026-03-10T09:00:00Z"
	followUpAt := "2026-03-10T10:00:00Z"
	item, err := s.CreateItem("Deferred item", ItemOptions{State: ItemStateWaiting, VisibleAfter: &visibleAfter, FollowUpAt: &followUpAt})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}
	if err := s.UpdateItemState(item.ID, ItemStateInbox); err != nil {
		t.Fatalf("UpdateItemState(inbox) error: %v", err)
	}
	got, err := s.GetItem(item.ID)
	if err != nil {
		t.Fatalf("GetItem() error: %v", err)
	}
	if got.State != ItemStateInbox {
		t.Fatalf("state = %q, want %q", got.State, ItemStateInbox)
	}
	if got.VisibleAfter != nil || got.FollowUpAt != nil {
		t.Fatalf("timestamps after reopen = visible_after:%v follow_up_at:%v, want nil", got.VisibleAfter, got.FollowUpAt)
	}
}

func TestUpdateItemInboxClearsDeferredTimesByDefault(t *testing.T) {
	s := newTestStore(t)
	visibleAfter := "2026-03-10T09:00:00Z"
	followUpAt := "2026-03-10T10:00:00Z"
	item, err := s.CreateItem("Deferred item", ItemOptions{State: ItemStateWaiting, VisibleAfter: &visibleAfter, FollowUpAt: &followUpAt})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}
	state := ItemStateInbox
	if err := s.UpdateItem(item.ID, ItemUpdate{State: &state}); err != nil {
		t.Fatalf("UpdateItem(inbox) error: %v", err)
	}
	got, err := s.GetItem(item.ID)
	if err != nil {
		t.Fatalf("GetItem() error: %v", err)
	}
	if got.State != ItemStateInbox {
		t.Fatalf("state = %q, want %q", got.State, ItemStateInbox)
	}
	if got.VisibleAfter != nil || got.FollowUpAt != nil {
		t.Fatalf("timestamps after update = visible_after:%v follow_up_at:%v, want nil", got.VisibleAfter, got.FollowUpAt)
	}
}

func TestResurfaceDueItems(t *testing.T) {
	s := newTestStore(t)
	now := time.Date(2026, time.March, 8, 10, 0, 0, 0, time.UTC)
	past := now.Add(-30 * time.Minute).Format(time.RFC3339)
	future := now.Add(30 * time.Minute).Format(time.RFC3339)
	pastVisible, err := s.CreateItem("past visible_after", ItemOptions{State: ItemStateWaiting, VisibleAfter: &past})
	if err != nil {
		t.Fatalf("CreateItem(past visible_after) error: %v", err)
	}
	pastFollowUp, err := s.CreateItem("past follow_up_at", ItemOptions{State: ItemStateWaiting, FollowUpAt: &past})
	if err != nil {
		t.Fatalf("CreateItem(past follow_up_at) error: %v", err)
	}
	futureWaiting, err := s.CreateItem("future waiting", ItemOptions{State: ItemStateWaiting, VisibleAfter: &future, FollowUpAt: &future})
	if err != nil {
		t.Fatalf("CreateItem(future waiting) error: %v", err)
	}
	bothTimes, err := s.CreateItem("both timestamps", ItemOptions{State: ItemStateWaiting, VisibleAfter: &future, FollowUpAt: &past})
	if err != nil {
		t.Fatalf("CreateItem(both timestamps) error: %v", err)
	}
	inboxItem, err := s.CreateItem("already inbox", ItemOptions{State: ItemStateInbox, VisibleAfter: &past})
	if err != nil {
		t.Fatalf("CreateItem(already inbox) error: %v", err)
	}
	doneItem, err := s.CreateItem("already done", ItemOptions{State: ItemStateDone, VisibleAfter: &past})
	if err != nil {
		t.Fatalf("CreateItem(already done) error: %v", err)
	}
	count, err := s.ResurfaceDueItems(now)
	if err != nil {
		t.Fatalf("ResurfaceDueItems() error: %v", err)
	}
	if count != 3 {
		t.Fatalf("ResurfaceDueItems() count = %d, want 3", count)
	}
	for _, tc := range []struct {
		name string
		id   int64
		want string
	}{{name: "past visible_after", id: pastVisible.ID, want: ItemStateInbox}, {name: "past follow_up_at", id: pastFollowUp.ID, want: ItemStateInbox}, {name: "both timestamps", id: bothTimes.ID, want: ItemStateInbox}, {name: "future waiting", id: futureWaiting.ID, want: ItemStateWaiting}, {name: "already inbox", id: inboxItem.ID, want: ItemStateInbox}, {name: "already done", id: doneItem.ID, want: ItemStateDone}} {
		item, err := s.GetItem(tc.id)
		if err != nil {
			t.Fatalf("GetItem(%s) error: %v", tc.name, err)
		}
		if item.State != tc.want {
			t.Fatalf("%s state = %q, want %q", tc.name, item.State, tc.want)
		}
	}
}

func TestItemStateSummariesAndCounts(t *testing.T) {
	s := newTestStore(t)
	now := time.Date(2026, time.March, 8, 10, 0, 0, 0, time.UTC)
	past := now.Add(-1 * time.Hour).Format(time.RFC3339)
	future := now.Add(2 * time.Hour).Format(time.RFC3339)
	workspace, err := s.CreateWorkspace("Alpha", filepath.Join(t.TempDir(), "alpha"))
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	actor, err := s.CreateActor("Alice", ActorKindHuman)
	if err != nil {
		t.Fatalf("CreateActor() error: %v", err)
	}
	artifactTitle := "Inbox plan"
	artifact, err := s.CreateArtifact(ArtifactKindIdeaNote, nil, nil, &artifactTitle, nil)
	if err != nil {
		t.Fatalf("CreateArtifact() error: %v", err)
	}
	visibleInbox, err := s.CreateItem("Visible inbox", ItemOptions{State: ItemStateInbox, WorkspaceID: &workspace.ID, ArtifactID: &artifact.ID, ActorID: &actor.ID, VisibleAfter: &past})
	if err != nil {
		t.Fatalf("CreateItem(visible inbox) error: %v", err)
	}
	if _, err := s.CreateItem("Hidden inbox", ItemOptions{State: ItemStateInbox, VisibleAfter: &future}); err != nil {
		t.Fatalf("CreateItem(hidden inbox) error: %v", err)
	}
	waitingItem, err := s.CreateItem("Waiting item", ItemOptions{State: ItemStateWaiting})
	if err != nil {
		t.Fatalf("CreateItem(waiting) error: %v", err)
	}
	somedayItem, err := s.CreateItem("Someday item", ItemOptions{State: ItemStateSomeday})
	if err != nil {
		t.Fatalf("CreateItem(someday) error: %v", err)
	}
	doneItem, err := s.CreateItem("Done item", ItemOptions{State: ItemStateDone})
	if err != nil {
		t.Fatalf("CreateItem(done) error: %v", err)
	}
	inboxItems, err := s.ListInboxItems(now)
	if err != nil {
		t.Fatalf("ListInboxItems() error: %v", err)
	}
	if len(inboxItems) != 1 {
		t.Fatalf("ListInboxItems() len = %d, want 1", len(inboxItems))
	}
	if inboxItems[0].ID != visibleInbox.ID {
		t.Fatalf("ListInboxItems() ID = %d, want %d", inboxItems[0].ID, visibleInbox.ID)
	}
	if inboxItems[0].ArtifactTitle == nil || *inboxItems[0].ArtifactTitle != artifactTitle {
		t.Fatalf("ListInboxItems() ArtifactTitle = %v, want %q", inboxItems[0].ArtifactTitle, artifactTitle)
	}
	if inboxItems[0].ArtifactKind == nil || *inboxItems[0].ArtifactKind != ArtifactKindIdeaNote {
		t.Fatalf("ListInboxItems() ArtifactKind = %v, want %q", inboxItems[0].ArtifactKind, ArtifactKindIdeaNote)
	}
	if inboxItems[0].ActorName == nil || *inboxItems[0].ActorName != "Alice" {
		t.Fatalf("ListInboxItems() ActorName = %v, want Alice", inboxItems[0].ActorName)
	}
	waitingItems, err := s.ListWaitingItems()
	if err != nil {
		t.Fatalf("ListWaitingItems() error: %v", err)
	}
	if len(waitingItems) != 1 || waitingItems[0].ID != waitingItem.ID {
		t.Fatalf("ListWaitingItems() = %+v, want waiting item %d", waitingItems, waitingItem.ID)
	}
	somedayItems, err := s.ListSomedayItems()
	if err != nil {
		t.Fatalf("ListSomedayItems() error: %v", err)
	}
	if len(somedayItems) != 1 || somedayItems[0].ID != somedayItem.ID {
		t.Fatalf("ListSomedayItems() = %+v, want someday item %d", somedayItems, somedayItem.ID)
	}
	doneItems, err := s.ListDoneItems(1)
	if err != nil {
		t.Fatalf("ListDoneItems() error: %v", err)
	}
	if len(doneItems) != 1 || doneItems[0].ID != doneItem.ID {
		t.Fatalf("ListDoneItems() = %+v, want done item %d", doneItems, doneItem.ID)
	}
	counts, err := s.CountItemsByState(now)
	if err != nil {
		t.Fatalf("CountItemsByState() error: %v", err)
	}
	if got := counts[ItemStateInbox]; got != 1 {
		t.Fatalf("CountItemsByState()[inbox] = %d, want 1", got)
	}
	if got := counts[ItemStateWaiting]; got != 1 {
		t.Fatalf("CountItemsByState()[waiting] = %d, want 1", got)
	}
	if got := counts[ItemStateSomeday]; got != 1 {
		t.Fatalf("CountItemsByState()[someday] = %d, want 1", got)
	}
	if got := counts[ItemStateDone]; got != 1 {
		t.Fatalf("CountItemsByState()[done] = %d, want 1", got)
	}
}

func TestItemSummaryFilters(t *testing.T) {
	s := newTestStore(t)
	now := time.Date(2026, time.March, 8, 10, 0, 0, 0, time.UTC)
	past := now.Add(-1 * time.Hour).Format(time.RFC3339)
	workspace, err := s.CreateWorkspace("Alpha", filepath.Join(t.TempDir(), "alpha"))
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	sourceTodoist := ExternalProviderTodoist
	sourceExchange := ExternalProviderExchange
	unassignedItem, err := s.CreateItem("Unassigned todoist item", ItemOptions{State: ItemStateInbox, VisibleAfter: &past, Source: &sourceTodoist})
	if err != nil {
		t.Fatalf("CreateItem(unassigned) error: %v", err)
	}
	if _, err := s.CreateItem("Workspace todoist item", ItemOptions{State: ItemStateInbox, WorkspaceID: &workspace.ID, VisibleAfter: &past, Source: &sourceTodoist}); err != nil {
		t.Fatalf("CreateItem(workspace todoist) error: %v", err)
	}
	if _, err := s.CreateItem("Workspace exchange item", ItemOptions{State: ItemStateInbox, WorkspaceID: &workspace.ID, VisibleAfter: &past, Source: &sourceExchange}); err != nil {
		t.Fatalf("CreateItem(workspace exchange) error: %v", err)
	}
	todoistItems, err := s.ListInboxItemsFiltered(now, ItemListFilter{Source: ExternalProviderTodoist})
	if err != nil {
		t.Fatalf("ListInboxItemsFiltered(todoist) error: %v", err)
	}
	if len(todoistItems) != 2 {
		t.Fatalf("ListInboxItemsFiltered(todoist) len = %d, want 2", len(todoistItems))
	}
	unassignedItems, err := s.ListInboxItemsFiltered(now, ItemListFilter{WorkspaceUnassigned: true})
	if err != nil {
		t.Fatalf("ListInboxItemsFiltered(unassigned) error: %v", err)
	}
	if len(unassignedItems) != 1 || unassignedItems[0].ID != unassignedItem.ID {
		t.Fatalf("ListInboxItemsFiltered(unassigned) = %+v, want only item %d", unassignedItems, unassignedItem.ID)
	}
	workspaceItems, err := s.ListInboxItemsFiltered(now, ItemListFilter{WorkspaceID: &workspace.ID})
	if err != nil {
		t.Fatalf("ListInboxItemsFiltered(workspace) error: %v", err)
	}
	if len(workspaceItems) != 2 {
		t.Fatalf("ListInboxItemsFiltered(workspace) len = %d, want 2", len(workspaceItems))
	}
	counts, err := s.CountItemsByStateFiltered(now, ItemListFilter{Source: ExternalProviderTodoist})
	if err != nil {
		t.Fatalf("CountItemsByStateFiltered(todoist) error: %v", err)
	}
	if got := counts[ItemStateInbox]; got != 2 {
		t.Fatalf("CountItemsByStateFiltered(todoist)[inbox] = %d, want 2", got)
	}
}
