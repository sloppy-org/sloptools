package sync_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/sloppy-org/sloptools/internal/store"
	tabsync "github.com/sloppy-org/sloptools/internal/sync"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.New(filepath.Join(t.TempDir(), "slopshell.db"))
	if err != nil {
		t.Fatalf("store.New() error: %v", err)
	}
	t.Cleanup(func() {
		_ = s.Close()
	})
	return s
}

func TestStoreSinkUpsertItemUsesContainerMappingAndBinding(t *testing.T) {
	s := newTestStore(t)
	sink := tabsync.NewStoreSink(s)

	account, err := s.CreateExternalAccount(store.SphereWork, store.ExternalProviderTodoist, "todo", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}
	workspace, err := s.CreateWorkspace("sync-target", filepath.Join(t.TempDir(), "workspace"), store.SphereWork)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	if _, err := s.SetContainerMapping(account.Provider, "project", "alpha", &workspace.ID, nil); err != nil {
		t.Fatalf("SetContainerMapping() error: %v", err)
	}

	item, err := sink.UpsertItem(context.Background(), store.Item{
		Title: "Follow up with provider",
	}, store.ExternalBinding{
		AccountID:    account.ID,
		Provider:     account.Provider,
		ObjectType:   "task",
		RemoteID:     "remote-1",
		ContainerRef: stringPtr("alpha"),
	})
	if err != nil {
		t.Fatalf("UpsertItem(create) error: %v", err)
	}
	if item.WorkspaceID == nil || *item.WorkspaceID != workspace.ID {
		t.Fatalf("item.WorkspaceID = %v, want %d", item.WorkspaceID, workspace.ID)
	}
	if item.Sphere != store.SphereWork {
		t.Fatalf("item.Sphere = %q, want %q", item.Sphere, store.SphereWork)
	}

	binding, err := s.GetBindingByRemote(account.ID, account.Provider, "task", "remote-1")
	if err != nil {
		t.Fatalf("GetBindingByRemote() error: %v", err)
	}
	if binding.ItemID == nil || *binding.ItemID != item.ID {
		t.Fatalf("binding.ItemID = %v, want %d", binding.ItemID, item.ID)
	}

	updated, err := sink.UpsertItem(context.Background(), store.Item{
		Title: "Updated provider title",
		State: store.ItemStateWaiting,
	}, store.ExternalBinding{
		AccountID:    account.ID,
		Provider:     account.Provider,
		ObjectType:   "task",
		RemoteID:     "remote-1",
		ContainerRef: stringPtr("alpha"),
	})
	if err != nil {
		t.Fatalf("UpsertItem(update) error: %v", err)
	}
	if updated.ID != item.ID {
		t.Fatalf("updated.ID = %d, want %d", updated.ID, item.ID)
	}
	if updated.Title != "Updated provider title" {
		t.Fatalf("updated.Title = %q, want updated title", updated.Title)
	}
	if updated.State != store.ItemStateWaiting {
		t.Fatalf("updated.State = %q, want %q", updated.State, store.ItemStateWaiting)
	}
}

func TestStoreSinkUpsertArtifactLinksWorkspaceAndTracksBinding(t *testing.T) {
	s := newTestStore(t)
	sink := tabsync.NewStoreSink(s)

	account, err := s.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderIMAP, "mail", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}
	workspace, err := s.CreateWorkspace("mailbox", filepath.Join(t.TempDir(), "mailbox"), store.SpherePrivate)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	if _, err := s.SetContainerMapping(account.Provider, "folder", "inbox", &workspace.ID, nil); err != nil {
		t.Fatalf("SetContainerMapping() error: %v", err)
	}

	title := "Subject line"
	artifact, err := sink.UpsertArtifact(context.Background(), store.Artifact{
		Kind:  store.ArtifactKindEmail,
		Title: &title,
	}, store.ExternalBinding{
		AccountID:    account.ID,
		Provider:     account.Provider,
		ObjectType:   "message",
		RemoteID:     "msg-1",
		ContainerRef: stringPtr("inbox"),
	})
	if err != nil {
		t.Fatalf("UpsertArtifact() error: %v", err)
	}

	links, err := s.ListArtifactWorkspaceLinks(workspace.ID)
	if err != nil {
		t.Fatalf("ListArtifactWorkspaceLinks() error: %v", err)
	}
	if len(links) != 1 || links[0].ArtifactID != artifact.ID {
		t.Fatalf("workspace links = %#v, want artifact %d", links, artifact.ID)
	}

	binding, err := s.GetBindingByRemote(account.ID, account.Provider, "message", "msg-1")
	if err != nil {
		t.Fatalf("GetBindingByRemote() error: %v", err)
	}
	if binding.ArtifactID == nil || *binding.ArtifactID != artifact.ID {
		t.Fatalf("binding.ArtifactID = %v, want %d", binding.ArtifactID, artifact.ID)
	}
}

func stringPtr(value string) *string {
	return &value
}
