package store_test

import (
	"database/sql"
	"errors"
	. "github.com/sloppy-org/sloptools/internal/store"
	_ "modernc.org/sqlite"
	"path/filepath"
	"reflect"
	"testing"
)

var _ *Store

func TestItemSchemaEnforcesForeignKeys(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.DB().Exec(`INSERT INTO items (title, workspace_id) VALUES ('invalid', 999)`); err == nil {
		t.Fatal("expected foreign key violation for missing workspace")
	}
	if _, err := s.DB().Exec(`INSERT INTO items (title, artifact_id) VALUES ('invalid', 999)`); err == nil {
		t.Fatal("expected foreign key violation for missing artifact")
	}
	if _, err := s.DB().Exec(`INSERT INTO items (title, actor_id) VALUES ('invalid', 999)`); err == nil {
		t.Fatal("expected foreign key violation for missing actor")
	}
}

func TestDomainTypesExposeJSONTags(t *testing.T) {
	for _, tc := range []struct {
		name string
		typ  reflect.Type
	}{{name: "Workspace", typ: reflect.TypeOf(Workspace{})}, {name: "Actor", typ: reflect.TypeOf(Actor{})}, {name: "Artifact", typ: reflect.TypeOf(Artifact{})}, {name: "ArtifactWorkspaceLink", typ: reflect.TypeOf(ArtifactWorkspaceLink{})}, {name: "ItemArtifactLink", typ: reflect.TypeOf(ItemArtifactLink{})}, {name: "ItemArtifact", typ: reflect.TypeOf(ItemArtifact{})}, {name: "Item", typ: reflect.TypeOf(Item{})}} {
		for i := 0; i < tc.typ.NumField(); i++ {
			field := tc.typ.Field(i)
			if field.PkgPath != "" {
				continue
			}
			if tag := field.Tag.Get("json"); tag == "" || tag == "-" {
				t.Fatalf("%s.%s missing json tag", tc.name, field.Name)
			}
		}
	}
}

func TestDomainCRUDRoundTrip(t *testing.T) {
	s := newTestStore(t)
	workspaceAPath := filepath.Join(t.TempDir(), "workspace-a")
	workspaceBPath := filepath.Join(t.TempDir(), "workspace-b")
	workspaceA, err := s.CreateWorkspace("Workspace A", workspaceAPath)
	if err != nil {
		t.Fatalf("CreateWorkspace(workspace-a) error: %v", err)
	}
	workspaceB, err := s.CreateWorkspace(" Workspace B ", workspaceBPath)
	if err != nil {
		t.Fatalf("CreateWorkspace(workspace-b) error: %v", err)
	}
	if workspaceA.Sphere != SpherePrivate || workspaceB.Sphere != SpherePrivate {
		t.Fatalf("default workspace spheres = %q/%q, want private/private", workspaceA.Sphere, workspaceB.Sphere)
	}
	workWorkspace, err := s.CreateWorkspace("Workspace Work", filepath.Join(t.TempDir(), "workspace-work"), SphereWork)
	if err != nil {
		t.Fatalf("CreateWorkspace(workspace-work) error: %v", err)
	}
	if workWorkspace.Sphere != SphereWork {
		t.Fatalf("CreateWorkspace(workspace-work).Sphere = %q, want %q", workWorkspace.Sphere, SphereWork)
	}
	gotByPath, err := s.GetWorkspaceByPath(workspaceBPath)
	if err != nil {
		t.Fatalf("GetWorkspaceByPath() error: %v", err)
	}
	if gotByPath.ID != workspaceB.ID {
		t.Fatalf("GetWorkspaceByPath() ID = %d, want %d", gotByPath.ID, workspaceB.ID)
	}
	duplicate, err := s.CreateWorkspace("Duplicate", workspaceAPath)
	if err != nil {
		t.Fatalf("CreateWorkspace(duplicate) error: %v", err)
	}
	if duplicate.ID != workspaceA.ID {
		t.Fatalf("duplicate workspace id = %d, want %d", duplicate.ID, workspaceA.ID)
	}
	if err := s.SetActiveWorkspace(workspaceB.ID); err != nil {
		t.Fatalf("SetActiveWorkspace() error: %v", err)
	}
	workspaces, err := s.ListWorkspaces()
	if err != nil {
		t.Fatalf("ListWorkspaces() error: %v", err)
	}
	if len(workspaces) != 3 {
		t.Fatalf("ListWorkspaces() len = %d, want 3", len(workspaces))
	}
	if !workspaces[0].IsActive || workspaces[0].ID != workspaceB.ID {
		t.Fatalf("ListWorkspaces() active workspace mismatch: %+v", workspaces)
	}
	workspaceA, err = s.GetWorkspace(workspaceA.ID)
	if err != nil {
		t.Fatalf("GetWorkspace(workspace-a) error: %v", err)
	}
	if workspaceA.IsActive {
		t.Fatal("expected inactive workspace after SetActiveWorkspace")
	}
	workspaceA, err = s.UpdateWorkspaceName(workspaceA.ID, " Workspace Alpha ")
	if err != nil {
		t.Fatalf("UpdateWorkspaceName() error: %v", err)
	}
	if workspaceA.Name != "Workspace Alpha" {
		t.Fatalf("UpdateWorkspaceName().Name = %q, want %q", workspaceA.Name, "Workspace Alpha")
	}
	if _, err := s.UpdateWorkspaceName(999999, "Missing"); err == nil {
		t.Fatal("expected missing workspace rename error")
	}
	humanEmail := "alice@example.com"
	humanProvider := "manual"
	humanMetaJSON := `{"organization":"Acme"}`
	human, err := s.CreateActorWithOptions("Alice", ActorKindHuman, ActorOptions{Email: &humanEmail, Provider: &humanProvider, MetaJSON: &humanMetaJSON})
	if err != nil {
		t.Fatalf("CreateActorWithOptions(Alice) error: %v", err)
	}
	agent, err := s.CreateActor("Codex", ActorKindAgent)
	if err != nil {
		t.Fatalf("CreateActor(Codex) error: %v", err)
	}
	if _, err := s.CreateActor("Nobody", "robot"); err == nil {
		t.Fatal("expected invalid actor kind error")
	}
	actors, err := s.ListActors()
	if err != nil {
		t.Fatalf("ListActors() error: %v", err)
	}
	if len(actors) != 2 {
		t.Fatalf("ListActors() len = %d, want 2", len(actors))
	}
	if actors[0].Email == nil || *actors[0].Email != "alice@example.com" {
		t.Fatalf("ListActors()[0].Email = %v, want alice@example.com", actors[0].Email)
	}
	if actors[0].Provider == nil || *actors[0].Provider != "manual" {
		t.Fatalf("ListActors()[0].Provider = %v, want manual", actors[0].Provider)
	}
	if actors[0].MetaJSON == nil || *actors[0].MetaJSON != humanMetaJSON {
		t.Fatalf("ListActors()[0].MetaJSON = %v, want %q", actors[0].MetaJSON, humanMetaJSON)
	}
	if actors[0].Name != "Alice" || actors[1].Name != "Codex" {
		t.Fatalf("ListActors() names = %#v, want Alice/Codex", []string{actors[0].Name, actors[1].Name})
	}
	gotActor, err := s.GetActor(agent.ID)
	if err != nil {
		t.Fatalf("GetActor() error: %v", err)
	}
	if gotActor.Kind != ActorKindAgent {
		t.Fatalf("GetActor().Kind = %q, want %q", gotActor.Kind, ActorKindAgent)
	}
	contactMetaJSON := `{"organization":"Example Corp","phones":["+1 555 0100"]}`
	contact, err := s.UpsertActorContact("Alice Example", "alice@example.com", ExternalProviderGmail, "people/c123", &contactMetaJSON)
	if err != nil {
		t.Fatalf("UpsertActorContact(create) error: %v", err)
	}
	if contact.Email == nil || *contact.Email != "alice@example.com" {
		t.Fatalf("UpsertActorContact(create).Email = %v, want alice@example.com", contact.Email)
	}
	if contact.ID != human.ID {
		t.Fatalf("UpsertActorContact(create).ID = %d, want %d", contact.ID, human.ID)
	}
	if contact.Provider == nil || *contact.Provider != ExternalProviderGmail {
		t.Fatalf("UpsertActorContact(create).Provider = %v, want %q", contact.Provider, ExternalProviderGmail)
	}
	updatedContact, err := s.UpsertActorContact("Alice Updated", "Alice@Example.com", ExternalProviderExchange, "exchange-7", nil)
	if err != nil {
		t.Fatalf("UpsertActorContact(update) error: %v", err)
	}
	if updatedContact.ID != contact.ID {
		t.Fatalf("UpsertActorContact(update).ID = %d, want %d", updatedContact.ID, contact.ID)
	}
	if updatedContact.ProviderRef == nil || *updatedContact.ProviderRef != "exchange-7" {
		t.Fatalf("UpsertActorContact(update).ProviderRef = %v, want exchange-7", updatedContact.ProviderRef)
	}
	if _, err := s.GetActorByEmail("ALICE@example.com"); err != nil {
		t.Fatalf("GetActorByEmail() error: %v", err)
	}
	if _, err := s.GetActorByProviderRef(ExternalProviderExchange, "exchange-7"); err != nil {
		t.Fatalf("GetActorByProviderRef() error: %v", err)
	}
	refPath := filepath.Join(t.TempDir(), "artifact.md")
	refURL := "https://example.invalid/item/1"
	title := "Plan draft"
	metaJSON := `{"source":"unit"}`
	artifact, err := s.CreateArtifact(ArtifactKindMarkdown, &refPath, &refURL, &title, &metaJSON)
	if err != nil {
		t.Fatalf("CreateArtifact() error: %v", err)
	}
	gotArtifact, err := s.GetArtifact(artifact.ID)
	if err != nil {
		t.Fatalf("GetArtifact() error: %v", err)
	}
	if gotArtifact.Kind != ArtifactKindMarkdown || gotArtifact.Title == nil || *gotArtifact.Title != title {
		t.Fatalf("GetArtifact() = %+v", gotArtifact)
	}
	updatedTitle := "Plan draft v2"
	clearRefURL := ""
	updatedKind := ArtifactKindDocument
	if err := s.UpdateArtifact(artifact.ID, ArtifactUpdate{Kind: &updatedKind, Title: &updatedTitle, RefURL: &clearRefURL}); err != nil {
		t.Fatalf("UpdateArtifact() error: %v", err)
	}
	gotArtifact, err = s.GetArtifact(artifact.ID)
	if err != nil {
		t.Fatalf("GetArtifact(updated) error: %v", err)
	}
	if gotArtifact.Kind != ArtifactKindDocument {
		t.Fatalf("GetArtifact(updated).Kind = %q, want %q", gotArtifact.Kind, ArtifactKindDocument)
	}
	if gotArtifact.RefURL != nil {
		t.Fatalf("GetArtifact(updated).RefURL = %v, want nil", *gotArtifact.RefURL)
	}
	if gotArtifact.Title == nil || *gotArtifact.Title != updatedTitle {
		t.Fatalf("GetArtifact(updated).Title = %v, want %q", gotArtifact.Title, updatedTitle)
	}
	artifacts, err := s.ListArtifactsByKind(ArtifactKindDocument)
	if err != nil {
		t.Fatalf("ListArtifactsByKind() error: %v", err)
	}
	if len(artifacts) != 1 || artifacts[0].ID != artifact.ID {
		t.Fatalf("ListArtifactsByKind() = %+v, want artifact %d", artifacts, artifact.ID)
	}
	source := "github"
	sourceRef := "issue-174"
	visibleAfter := "2026-03-09T10:00:00Z"
	followUpAt := "2026-03-10T11:00:00Z"
	inboxItem, err := s.CreateItem("Inbox item", ItemOptions{})
	if err != nil {
		t.Fatalf("CreateItem(inbox) error: %v", err)
	}
	artifactItem, err := s.CreateItem("Artifact item", ItemOptions{ArtifactID: &artifact.ID, Source: &source, SourceRef: &sourceRef})
	if err != nil {
		t.Fatalf("CreateItem(artifact) error: %v", err)
	}
	workspaceItem, err := s.CreateItem("Workspace item", ItemOptions{WorkspaceID: &workWorkspace.ID})
	if err != nil {
		t.Fatalf("CreateItem(workspace) error: %v", err)
	}
	if workspaceItem.Sphere != SphereWork {
		t.Fatalf("CreateItem(workspace).Sphere = %q, want %q", workspaceItem.Sphere, SphereWork)
	}
	assignedItem, err := s.CreateItem("Assigned item", ItemOptions{State: ItemStateWaiting, WorkspaceID: &workspaceB.ID, ArtifactID: &artifact.ID, ActorID: &human.ID, VisibleAfter: &visibleAfter, FollowUpAt: &followUpAt})
	if err != nil {
		t.Fatalf("CreateItem(assigned) error: %v", err)
	}
	if assignedItem.WorkspaceID == nil || *assignedItem.WorkspaceID != workspaceB.ID {
		t.Fatalf("CreateItem(assigned).WorkspaceID = %v, want %d", assignedItem.WorkspaceID, workspaceB.ID)
	}
	if assignedItem.ArtifactID == nil || *assignedItem.ArtifactID != artifact.ID {
		t.Fatalf("CreateItem(assigned).ArtifactID = %v, want %d", assignedItem.ArtifactID, artifact.ID)
	}
	if assignedItem.ActorID == nil || *assignedItem.ActorID != human.ID {
		t.Fatalf("CreateItem(assigned).ActorID = %v, want %d", assignedItem.ActorID, human.ID)
	}
	if assignedItem.Sphere != SpherePrivate {
		t.Fatalf("CreateItem(assigned).Sphere = %q, want %q", assignedItem.Sphere, SpherePrivate)
	}
	sourceCompleteRef := "issue-183"
	sourceItem, err := s.CreateItem("Source completion item", ItemOptions{State: ItemStateWaiting, Source: &source, SourceRef: &sourceCompleteRef})
	if err != nil {
		t.Fatalf("CreateItem(source completion) error: %v", err)
	}
	if err := s.AssignItem(artifactItem.ID, agent.ID); err != nil {
		t.Fatalf("AssignItem() error: %v", err)
	}
	gotItem, err := s.GetItem(artifactItem.ID)
	if err != nil {
		t.Fatalf("GetItem(artifact assigned) error: %v", err)
	}
	if gotItem.ActorID == nil || *gotItem.ActorID != agent.ID {
		t.Fatalf("GetItem(artifact assigned).ActorID = %v, want %d", gotItem.ActorID, agent.ID)
	}
	if err := s.AssignItem(artifactItem.ID, 9999); err == nil {
		t.Fatal("expected assign to nonexistent actor error")
	}
	if err := s.AssignItem(artifactItem.ID, human.ID); err != nil {
		t.Fatalf("AssignItem(reassign) error: %v", err)
	}
	gotItem, err = s.GetItem(artifactItem.ID)
	if err != nil {
		t.Fatalf("GetItem(artifact reassigned) error: %v", err)
	}
	if gotItem.ActorID == nil || *gotItem.ActorID != human.ID {
		t.Fatalf("GetItem(artifact reassigned).ActorID = %v, want %d", gotItem.ActorID, human.ID)
	}
	if gotItem.State != ItemStateWaiting {
		t.Fatalf("GetItem(artifact reassigned).State = %q, want %q", gotItem.State, ItemStateWaiting)
	}
	if err := s.UnassignItem(artifactItem.ID); err != nil {
		t.Fatalf("UnassignItem() error: %v", err)
	}
	gotItem, err = s.GetItem(artifactItem.ID)
	if err != nil {
		t.Fatalf("GetItem(artifact unassigned) error: %v", err)
	}
	if gotItem.ActorID != nil {
		t.Fatalf("GetItem(artifact unassigned).ActorID = %v, want nil", gotItem.ActorID)
	}
	if gotItem.State != ItemStateInbox {
		t.Fatalf("GetItem(artifact unassigned).State = %q, want %q", gotItem.State, ItemStateInbox)
	}
	if err := s.UnassignItem(artifactItem.ID); err == nil {
		t.Fatal("expected unassign on unassigned item error")
	}
	if err := s.AssignItem(artifactItem.ID, agent.ID); err != nil {
		t.Fatalf("AssignItem(reassign to agent) error: %v", err)
	}
	if err := s.CompleteItemByActor(artifactItem.ID, human.ID); err == nil {
		t.Fatal("expected complete with wrong actor error")
	}
	if err := s.CompleteItemByActor(artifactItem.ID, agent.ID); err != nil {
		t.Fatalf("CompleteItemByActor() error: %v", err)
	}
	gotItem, err = s.GetItem(artifactItem.ID)
	if err != nil {
		t.Fatalf("GetItem(artifact completed) error: %v", err)
	}
	if gotItem.State != ItemStateDone {
		t.Fatalf("GetItem(artifact completed).State = %q, want %q", gotItem.State, ItemStateDone)
	}
	if err := s.CompleteItemByActor(artifactItem.ID, agent.ID); err == nil {
		t.Fatal("expected double complete error")
	}
	if err := s.AssignItem(artifactItem.ID, human.ID); err == nil {
		t.Fatal("expected assign on done item error")
	}
	if err := s.ReturnItemToInbox(assignedItem.ID); err != nil {
		t.Fatalf("ReturnItemToInbox() error: %v", err)
	}
	gotItem, err = s.GetItem(assignedItem.ID)
	if err != nil {
		t.Fatalf("GetItem(returned to inbox) error: %v", err)
	}
	if gotItem.State != ItemStateInbox {
		t.Fatalf("GetItem(returned to inbox).State = %q, want %q", gotItem.State, ItemStateInbox)
	}
	if gotItem.ActorID == nil || *gotItem.ActorID != human.ID {
		t.Fatalf("GetItem(returned to inbox).ActorID = %v, want %d", gotItem.ActorID, human.ID)
	}
	if err := s.ReturnItemToInbox(artifactItem.ID); err == nil {
		t.Fatal("expected return on done item error")
	}
	if err := s.CompleteItemBySource(source, sourceCompleteRef); err != nil {
		t.Fatalf("CompleteItemBySource() error: %v", err)
	}
	gotItem, err = s.GetItem(sourceItem.ID)
	if err != nil {
		t.Fatalf("GetItem(source completed) error: %v", err)
	}
	if gotItem.State != ItemStateDone {
		t.Fatalf("GetItem(source completed).State = %q, want %q", gotItem.State, ItemStateDone)
	}
	if err := s.CompleteItemBySource(source, sourceCompleteRef); err == nil {
		t.Fatal("expected double source complete error")
	}
	if err := s.CompleteItemBySource("github", "missing"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("CompleteItemBySource(missing) error = %v, want sql.ErrNoRows", err)
	}
	if err := s.UpdateItemTimes(inboxItem.ID, &visibleAfter, &followUpAt); err != nil {
		t.Fatalf("UpdateItemTimes() error: %v", err)
	}
	gotItem, err = s.GetItem(inboxItem.ID)
	if err != nil {
		t.Fatalf("GetItem(updated times) error: %v", err)
	}
	if gotItem.VisibleAfter == nil || *gotItem.VisibleAfter != visibleAfter {
		t.Fatalf("VisibleAfter = %v, want %q", gotItem.VisibleAfter, visibleAfter)
	}
	if gotItem.FollowUpAt == nil || *gotItem.FollowUpAt != followUpAt {
		t.Fatalf("FollowUpAt = %v, want %q", gotItem.FollowUpAt, followUpAt)
	}
	if err := s.UpdateItemState(inboxItem.ID, ItemStateWaiting); err != nil {
		t.Fatalf("UpdateItemState(waiting) error: %v", err)
	}
	if err := s.UpdateItemState(workspaceItem.ID, ItemStateSomeday); err != nil {
		t.Fatalf("UpdateItemState(someday) error: %v", err)
	}
	if err := s.UpdateItemState(workspaceItem.ID, ItemStateInbox); err != nil {
		t.Fatalf("UpdateItemState(inbox from someday) error: %v", err)
	}
	if err := s.UpdateItemState(inboxItem.ID, ItemStateDone); err != nil {
		t.Fatalf("UpdateItemState(done) error: %v", err)
	}
	if err := s.UpdateItemState(inboxItem.ID, ItemStateInbox); err != nil {
		t.Fatalf("UpdateItemState(inbox from done) error: %v", err)
	}
	if err := s.UpdateItemState(inboxItem.ID, "paused"); err == nil {
		t.Fatal("expected invalid item state error")
	}
	waitingItems, err := s.ListItemsByState(ItemStateWaiting)
	if err != nil {
		t.Fatalf("ListItemsByState(waiting) error: %v", err)
	}
	if len(waitingItems) != 0 {
		t.Fatalf("ListItemsByState(waiting) len = %d, want 0", len(waitingItems))
	}
	doneItems, err := s.ListItemsByState(ItemStateDone)
	if err != nil {
		t.Fatalf("ListItemsByState(done) error: %v", err)
	}
	if len(doneItems) != 2 {
		t.Fatalf("ListItemsByState(done) len = %d, want 2", len(doneItems))
	}
	doneIDs := map[int64]bool{}
	for _, item := range doneItems {
		doneIDs[item.ID] = true
	}
	for _, id := range []int64{artifactItem.ID, sourceItem.ID} {
		if !doneIDs[id] {
			t.Fatalf("ListItemsByState(done) missing item %d: %+v", id, doneItems)
		}
	}
	if _, err := s.ListItemsByState("paused"); err == nil {
		t.Fatal("expected invalid ListItemsByState error")
	}
	if err := s.DeleteWorkspace(workWorkspace.ID); err != nil {
		t.Fatalf("DeleteWorkspace() error: %v", err)
	}
	workspaceItem, err = s.GetItem(workspaceItem.ID)
	if err != nil {
		t.Fatalf("GetItem(workspace item after workspace delete) error: %v", err)
	}
	if workspaceItem.WorkspaceID != nil {
		t.Fatalf("workspace item WorkspaceID = %v, want nil", *workspaceItem.WorkspaceID)
	}
	if workspaceItem.Sphere != SphereWork {
		t.Fatalf("workspace item sphere after workspace delete = %q, want %q", workspaceItem.Sphere, SphereWork)
	}
	if err := s.DeleteArtifact(artifact.ID); err != nil {
		t.Fatalf("DeleteArtifact() error: %v", err)
	}
	artifactItem, err = s.GetItem(artifactItem.ID)
	if err != nil {
		t.Fatalf("GetItem(artifact item after artifact delete) error: %v", err)
	}
	if artifactItem.ArtifactID != nil {
		t.Fatalf("artifact item ArtifactID = %v, want nil", *artifactItem.ArtifactID)
	}
	if err := s.DeleteActor(agent.ID); err != nil {
		t.Fatalf("DeleteActor() error: %v", err)
	}
	artifactItem, err = s.GetItem(artifactItem.ID)
	if err != nil {
		t.Fatalf("GetItem(artifact item after actor delete) error: %v", err)
	}
	if artifactItem.ActorID != nil {
		t.Fatalf("artifact item ActorID = %v, want nil", *artifactItem.ActorID)
	}
	if err := s.DeleteItem(assignedItem.ID); err != nil {
		t.Fatalf("DeleteItem() error: %v", err)
	}
	if _, err := s.GetItem(assignedItem.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetItem(deleted) error = %v, want sql.ErrNoRows", err)
	}
}
