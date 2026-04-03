package store

import (
	"database/sql"
	"errors"
	"testing"
	"time"
)

func TestExternalBindingStoreCRUDAndQueries(t *testing.T) {
	s := newTestStore(t)

	account, err := s.CreateExternalAccount(SphereWork, ExternalProviderGmail, "Work Gmail", map[string]any{
		"username":   "alice@example.com",
		"token_file": "gmail-work.json",
	})
	if err != nil {
		t.Fatalf("CreateExternalAccount(work) error: %v", err)
	}
	otherAccount, err := s.CreateExternalAccount(SpherePrivate, ExternalProviderTodoist, "Personal Todoist", map[string]any{
		"username": "alice",
	})
	if err != nil {
		t.Fatalf("CreateExternalAccount(todoist) error: %v", err)
	}
	item, err := s.CreateItem("Follow up", ItemOptions{})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}
	title := "Imported thread"
	artifact, err := s.CreateArtifact(ArtifactKindEmail, nil, nil, &title, nil)
	if err != nil {
		t.Fatalf("CreateArtifact() error: %v", err)
	}

	containerRef := "INBOX/Work"
	remoteUpdatedAt := "2026-03-08T12:00:00Z"
	created, err := s.UpsertExternalBinding(ExternalBinding{
		AccountID:       account.ID,
		Provider:        " GMAIL ",
		ObjectType:      " Email ",
		RemoteID:        " msg-1 ",
		ItemID:          &item.ID,
		ArtifactID:      &artifact.ID,
		ContainerRef:    &containerRef,
		RemoteUpdatedAt: &remoteUpdatedAt,
	})
	if err != nil {
		t.Fatalf("UpsertExternalBinding(create) error: %v", err)
	}
	if created.ID == 0 {
		t.Fatal("expected created binding ID")
	}
	if created.Provider != ExternalProviderGmail {
		t.Fatalf("binding provider = %q, want %q", created.Provider, ExternalProviderGmail)
	}
	if created.ObjectType != "email" {
		t.Fatalf("binding object_type = %q, want email", created.ObjectType)
	}
	if created.RemoteID != "msg-1" {
		t.Fatalf("binding remote_id = %q, want msg-1", created.RemoteID)
	}
	if created.ItemID == nil || *created.ItemID != item.ID {
		t.Fatalf("binding item_id = %v, want %d", created.ItemID, item.ID)
	}
	if created.ArtifactID == nil || *created.ArtifactID != artifact.ID {
		t.Fatalf("binding artifact_id = %v, want %d", created.ArtifactID, artifact.ID)
	}
	if created.ContainerRef == nil || *created.ContainerRef != containerRef {
		t.Fatalf("binding container_ref = %v, want %q", created.ContainerRef, containerRef)
	}
	if created.RemoteUpdatedAt == nil || *created.RemoteUpdatedAt != "2026-03-08T12:00:00Z" {
		t.Fatalf("binding remote_updated_at = %v, want normalized timestamp", created.RemoteUpdatedAt)
	}
	if created.LastSyncedAt == "" {
		t.Fatal("expected last_synced_at")
	}

	got, err := s.GetBindingByRemote(account.ID, ExternalProviderGmail, "email", "msg-1")
	if err != nil {
		t.Fatalf("GetBindingByRemote() error: %v", err)
	}
	if got.ID != created.ID {
		t.Fatalf("GetBindingByRemote() id = %d, want %d", got.ID, created.ID)
	}
	latestRemoteAt, err := s.LatestBindingRemoteUpdatedAt(account.ID, ExternalProviderGmail, "email")
	if err != nil {
		t.Fatalf("LatestBindingRemoteUpdatedAt(created) error: %v", err)
	}
	if latestRemoteAt == nil || *latestRemoteAt != "2026-03-08T12:00:00Z" {
		t.Fatalf("LatestBindingRemoteUpdatedAt(created) = %v, want 2026-03-08T12:00:00Z", latestRemoteAt)
	}

	updatedRemoteAt := "2026-03-08T13:15:00Z"
	updated, err := s.UpsertExternalBinding(ExternalBinding{
		AccountID:       account.ID,
		Provider:        ExternalProviderGmail,
		ObjectType:      "email",
		RemoteID:        "msg-1",
		ItemID:          &item.ID,
		ContainerRef:    &containerRef,
		RemoteUpdatedAt: &updatedRemoteAt,
	})
	if err != nil {
		t.Fatalf("UpsertExternalBinding(update) error: %v", err)
	}
	if updated.ID != created.ID {
		t.Fatalf("updated binding ID = %d, want %d", updated.ID, created.ID)
	}
	if updated.ArtifactID != nil {
		t.Fatalf("updated binding artifact_id = %v, want nil after update", updated.ArtifactID)
	}
	if updated.RemoteUpdatedAt == nil || *updated.RemoteUpdatedAt != updatedRemoteAt {
		t.Fatalf("updated remote_updated_at = %v, want %q", updated.RemoteUpdatedAt, updatedRemoteAt)
	}
	if updated.LastSyncedAt == "" {
		t.Fatal("expected updated last_synced_at")
	}
	latestRemoteAt, err = s.LatestBindingRemoteUpdatedAt(account.ID, ExternalProviderGmail, "email")
	if err != nil {
		t.Fatalf("LatestBindingRemoteUpdatedAt(updated) error: %v", err)
	}
	if latestRemoteAt == nil || *latestRemoteAt != updatedRemoteAt {
		t.Fatalf("LatestBindingRemoteUpdatedAt(updated) = %v, want %q", latestRemoteAt, updatedRemoteAt)
	}

	otherRemoteAt := "2026-03-08T08:30:00Z"
	second, err := s.UpsertExternalBinding(ExternalBinding{
		AccountID:       otherAccount.ID,
		Provider:        ExternalProviderTodoist,
		ObjectType:      "task",
		RemoteID:        "task-7",
		ItemID:          &item.ID,
		ArtifactID:      &artifact.ID,
		RemoteUpdatedAt: &otherRemoteAt,
	})
	if err != nil {
		t.Fatalf("UpsertExternalBinding(second) error: %v", err)
	}

	itemBindings, err := s.GetBindingsByItem(item.ID)
	if err != nil {
		t.Fatalf("GetBindingsByItem() error: %v", err)
	}
	if len(itemBindings) != 2 {
		t.Fatalf("GetBindingsByItem() len = %d, want 2", len(itemBindings))
	}
	if itemBindings[0].ID != updated.ID || itemBindings[1].ID != second.ID {
		t.Fatalf("GetBindingsByItem() order = %+v", itemBindings)
	}

	artifactBindings, err := s.GetBindingsByArtifact(artifact.ID)
	if err != nil {
		t.Fatalf("GetBindingsByArtifact() error: %v", err)
	}
	if len(artifactBindings) != 1 || artifactBindings[0].ID != second.ID {
		t.Fatalf("GetBindingsByArtifact() = %+v, want only second binding", artifactBindings)
	}

	oldSync := "2026-03-08T09:00:00Z"
	if _, err := s.db.Exec(`UPDATE external_bindings SET last_synced_at = ? WHERE id = ?`, oldSync, updated.ID); err != nil {
		t.Fatalf("seed old last_synced_at: %v", err)
	}
	stale, err := s.ListStaleBindings(ExternalProviderGmail, time.Date(2026, time.March, 8, 10, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("ListStaleBindings() error: %v", err)
	}
	if len(stale) != 1 || stale[0].ID != updated.ID {
		t.Fatalf("ListStaleBindings() = %+v, want updated binding only", stale)
	}

	if err := s.DeleteBinding(updated.ID); err != nil {
		t.Fatalf("DeleteBinding() error: %v", err)
	}
	if _, err := s.GetBindingByRemote(account.ID, ExternalProviderGmail, "email", "msg-1"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetBindingByRemote(deleted) error = %v, want sql.ErrNoRows", err)
	}
	latestRemoteAt, err = s.LatestBindingRemoteUpdatedAt(account.ID, ExternalProviderGmail, "email")
	if err != nil {
		t.Fatalf("LatestBindingRemoteUpdatedAt(deleted) error: %v", err)
	}
	if latestRemoteAt != nil {
		t.Fatalf("LatestBindingRemoteUpdatedAt(deleted) = %v, want nil", latestRemoteAt)
	}
}

func TestExternalBindingStoreListsMissingContainerRef(t *testing.T) {
	s := newTestStore(t)

	account, err := s.CreateExternalAccount(SphereWork, ExternalProviderExchangeEWS, "TU Graz", map[string]any{
		"endpoint": "https://exchange.tugraz.at/EWS/Exchange.asmx",
		"username": "ert",
	})
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}

	item, err := s.CreateItem("Follow up", ItemOptions{})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}

	missing, err := s.UpsertExternalBinding(ExternalBinding{
		AccountID:  account.ID,
		Provider:   ExternalProviderExchangeEWS,
		ObjectType: "email",
		RemoteID:   "msg-missing",
		ItemID:     &item.ID,
	})
	if err != nil {
		t.Fatalf("UpsertExternalBinding(missing) error: %v", err)
	}
	containerRef := "Posteingang"
	present, err := s.UpsertExternalBinding(ExternalBinding{
		AccountID:    account.ID,
		Provider:     ExternalProviderExchangeEWS,
		ObjectType:   "email",
		RemoteID:     "msg-present",
		ItemID:       &item.ID,
		ContainerRef: &containerRef,
	})
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

	account, err := s.CreateExternalAccount(SphereWork, ExternalProviderExchangeEWS, "TU Graz", map[string]any{
		"endpoint": "https://exchange.tugraz.at/EWS/Exchange.asmx",
		"username": "ert",
	})
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
	if _, err := s.UpsertExternalBinding(ExternalBinding{
		AccountID:    account.ID,
		Provider:     ExternalProviderExchangeEWS,
		ObjectType:   "email",
		RemoteID:     "msg-1",
		ItemID:       &item.ID,
		ArtifactID:   &artifact.ID,
		ContainerRef: &containerRef,
	}); err != nil {
		t.Fatalf("UpsertExternalBinding() error: %v", err)
	}

	trashRef := "Gelöschte Elemente"
	doneState := ItemStateDone
	if err := s.ApplyExternalBindingReconcileUpdates(account.ID, ExternalProviderExchangeEWS, []ExternalBindingReconcileUpdate{
		{
			ObjectType:        "email",
			OldRemoteID:       "msg-1",
			NewRemoteID:       "msg-1-trash",
			ContainerRef:      &trashRef,
			FollowUpItemState: &doneState,
		},
	}); err != nil {
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

	account, err := s.CreateExternalAccount(SphereWork, ExternalProviderIMAP, "Work IMAP", map[string]any{
		"host":     "imap.example.com",
		"port":     993,
		"username": "alice@example.com",
	})
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}

	if _, err := s.UpsertExternalBinding(ExternalBinding{}); err == nil {
		t.Fatal("expected missing account error")
	}
	if _, err := s.UpsertExternalBinding(ExternalBinding{
		AccountID:  account.ID,
		Provider:   ExternalProviderGmail,
		ObjectType: "email",
		RemoteID:   "msg-1",
	}); err == nil {
		t.Fatal("expected provider mismatch error")
	}
	if _, err := s.UpsertExternalBinding(ExternalBinding{
		AccountID: account.ID,
		Provider:  ExternalProviderIMAP,
		RemoteID:  "msg-1",
	}); err == nil {
		t.Fatal("expected missing object_type error")
	}
	if _, err := s.UpsertExternalBinding(ExternalBinding{
		AccountID:  account.ID,
		Provider:   ExternalProviderIMAP,
		ObjectType: "email",
	}); err == nil {
		t.Fatal("expected missing remote_id error")
	}
	badRemoteUpdatedAt := "tomorrow morning"
	if _, err := s.UpsertExternalBinding(ExternalBinding{
		AccountID:       account.ID,
		Provider:        ExternalProviderIMAP,
		ObjectType:      "email",
		RemoteID:        "msg-1",
		RemoteUpdatedAt: &badRemoteUpdatedAt,
	}); err == nil {
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
