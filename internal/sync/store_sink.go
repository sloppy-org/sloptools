package sync

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/krystophny/sloppy/internal/store"
)

type StoreSink struct {
	store *store.Store
}

func NewStoreSink(s *store.Store) *StoreSink {
	return &StoreSink{store: s}
}

func (s *StoreSink) UpsertItem(_ context.Context, item store.Item, binding store.ExternalBinding) (store.Item, error) {
	account, existingBinding, existingItem, assignment, err := s.resolveItemTarget(binding)
	if err != nil {
		return store.Item{}, err
	}

	if existingItem != nil {
		update, err := s.itemUpdate(account, item, assignment)
		if err != nil {
			return store.Item{}, err
		}
		if err := s.store.UpdateItem(existingItem.ID, update); err != nil {
			return store.Item{}, err
		}
		updated, err := s.store.GetItem(existingItem.ID)
		if err != nil {
			return store.Item{}, err
		}
		if _, err := s.store.UpsertExternalBinding(store.ExternalBinding{
			AccountID:       account.ID,
			Provider:        account.Provider,
			ObjectType:      binding.ObjectType,
			RemoteID:        binding.RemoteID,
			ItemID:          &updated.ID,
			ArtifactID:      existingBinding.ArtifactID,
			ContainerRef:    normalizeContainerRef(binding.ContainerRef),
			RemoteUpdatedAt: binding.RemoteUpdatedAt,
		}); err != nil {
			return store.Item{}, err
		}
		return updated, nil
	}

	options, err := s.itemCreateOptions(account, item, assignment)
	if err != nil {
		return store.Item{}, err
	}
	created, err := s.store.CreateItem(item.Title, options)
	if err != nil {
		return store.Item{}, err
	}
	artifactID := existingBinding.ArtifactID
	if item.ArtifactID != nil {
		artifactID = item.ArtifactID
	}
	if _, err := s.store.UpsertExternalBinding(store.ExternalBinding{
		AccountID:       account.ID,
		Provider:        account.Provider,
		ObjectType:      binding.ObjectType,
		RemoteID:        binding.RemoteID,
		ItemID:          &created.ID,
		ArtifactID:      artifactID,
		ContainerRef:    normalizeContainerRef(binding.ContainerRef),
		RemoteUpdatedAt: binding.RemoteUpdatedAt,
	}); err != nil {
		return store.Item{}, err
	}
	return created, nil
}

func (s *StoreSink) UpsertArtifact(_ context.Context, artifact store.Artifact, binding store.ExternalBinding) (store.Artifact, error) {
	account, existingBinding, existingArtifact, assignment, err := s.resolveArtifactTarget(binding)
	if err != nil {
		return store.Artifact{}, err
	}

	if existingArtifact != nil {
		update, err := artifactUpdate(artifact)
		if err != nil {
			return store.Artifact{}, err
		}
		if err := s.store.UpdateArtifact(existingArtifact.ID, update); err != nil {
			return store.Artifact{}, err
		}
		updated, err := s.store.GetArtifact(existingArtifact.ID)
		if err != nil {
			return store.Artifact{}, err
		}
		if err := s.linkArtifactWorkspace(updated, assignment.WorkspaceID); err != nil {
			return store.Artifact{}, err
		}
		if _, err := s.store.UpsertExternalBinding(store.ExternalBinding{
			AccountID:       account.ID,
			Provider:        account.Provider,
			ObjectType:      binding.ObjectType,
			RemoteID:        binding.RemoteID,
			ItemID:          existingBinding.ItemID,
			ArtifactID:      &updated.ID,
			ContainerRef:    normalizeContainerRef(binding.ContainerRef),
			RemoteUpdatedAt: binding.RemoteUpdatedAt,
		}); err != nil {
			return store.Artifact{}, err
		}
		return updated, nil
	}

	if strings.TrimSpace(string(artifact.Kind)) == "" {
		return store.Artifact{}, errors.New("artifact kind is required")
	}
	created, err := s.store.CreateArtifact(artifact.Kind, artifact.RefPath, artifact.RefURL, artifact.Title, artifact.MetaJSON)
	if err != nil {
		return store.Artifact{}, err
	}
	if err := s.linkArtifactWorkspace(created, assignment.WorkspaceID); err != nil {
		return store.Artifact{}, err
	}
	if _, err := s.store.UpsertExternalBinding(store.ExternalBinding{
		AccountID:       account.ID,
		Provider:        account.Provider,
		ObjectType:      binding.ObjectType,
		RemoteID:        binding.RemoteID,
		ItemID:          existingBinding.ItemID,
		ArtifactID:      &created.ID,
		ContainerRef:    normalizeContainerRef(binding.ContainerRef),
		RemoteUpdatedAt: binding.RemoteUpdatedAt,
	}); err != nil {
		return store.Artifact{}, err
	}
	return created, nil
}

type assignment struct {
	WorkspaceID *int64
	Sphere      *string
}

func (s *StoreSink) resolveItemTarget(binding store.ExternalBinding) (store.ExternalAccount, store.ExternalBinding, *store.Item, assignment, error) {
	account, existingBinding, err := s.resolveBinding(binding)
	if err != nil {
		return store.ExternalAccount{}, store.ExternalBinding{}, nil, assignment{}, err
	}
	target, err := s.lookupAssignment(account.Provider, normalizeContainerRef(binding.ContainerRef))
	if err != nil {
		return store.ExternalAccount{}, store.ExternalBinding{}, nil, assignment{}, err
	}
	var item *store.Item
	switch {
	case existingBinding.ItemID != nil:
		resolved, err := s.store.GetItem(*existingBinding.ItemID)
		if err != nil {
			return store.ExternalAccount{}, store.ExternalBinding{}, nil, assignment{}, err
		}
		item = &resolved
	case binding.ItemID != nil:
		resolved, err := s.store.GetItem(*binding.ItemID)
		if err != nil {
			return store.ExternalAccount{}, store.ExternalBinding{}, nil, assignment{}, err
		}
		item = &resolved
	}
	return account, existingBinding, item, target, nil
}

func (s *StoreSink) resolveArtifactTarget(binding store.ExternalBinding) (store.ExternalAccount, store.ExternalBinding, *store.Artifact, assignment, error) {
	account, existingBinding, err := s.resolveBinding(binding)
	if err != nil {
		return store.ExternalAccount{}, store.ExternalBinding{}, nil, assignment{}, err
	}
	target, err := s.lookupAssignment(account.Provider, normalizeContainerRef(binding.ContainerRef))
	if err != nil {
		return store.ExternalAccount{}, store.ExternalBinding{}, nil, assignment{}, err
	}
	var artifact *store.Artifact
	switch {
	case existingBinding.ArtifactID != nil:
		resolved, err := s.store.GetArtifact(*existingBinding.ArtifactID)
		if err != nil {
			return store.ExternalAccount{}, store.ExternalBinding{}, nil, assignment{}, err
		}
		artifact = &resolved
	case binding.ArtifactID != nil:
		resolved, err := s.store.GetArtifact(*binding.ArtifactID)
		if err != nil {
			return store.ExternalAccount{}, store.ExternalBinding{}, nil, assignment{}, err
		}
		artifact = &resolved
	}
	return account, existingBinding, artifact, target, nil
}

func (s *StoreSink) resolveBinding(binding store.ExternalBinding) (store.ExternalAccount, store.ExternalBinding, error) {
	if binding.AccountID <= 0 {
		return store.ExternalAccount{}, store.ExternalBinding{}, errors.New("external binding account_id is required")
	}
	account, err := s.store.GetExternalAccount(binding.AccountID)
	if err != nil {
		return store.ExternalAccount{}, store.ExternalBinding{}, err
	}
	if strings.TrimSpace(binding.Provider) == "" {
		binding.Provider = account.Provider
	}
	if binding.Provider != account.Provider {
		return store.ExternalAccount{}, store.ExternalBinding{}, errors.New("external binding provider must match account provider")
	}
	if strings.TrimSpace(binding.ObjectType) == "" {
		return store.ExternalAccount{}, store.ExternalBinding{}, errors.New("external binding object_type is required")
	}
	if strings.TrimSpace(binding.RemoteID) == "" {
		return store.ExternalAccount{}, store.ExternalBinding{}, errors.New("external binding remote_id is required")
	}
	existingBinding, err := s.store.GetBindingByRemote(binding.AccountID, binding.Provider, binding.ObjectType, binding.RemoteID)
	if errors.Is(err, sql.ErrNoRows) {
		return account, store.ExternalBinding{}, nil
	}
	if err != nil {
		return store.ExternalAccount{}, store.ExternalBinding{}, err
	}
	return account, existingBinding, nil
}

func (s *StoreSink) lookupAssignment(provider string, containerRef *string) (assignment, error) {
	ref := strings.TrimSpace(stringFromPointer(containerRef))
	if ref == "" {
		return assignment{}, nil
	}
	for _, containerType := range []string{"project", "collection", "notebook", "tag", "label", "calendar", "folder"} {
		mapping, err := s.store.GetContainerMapping(provider, containerType, ref)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return assignment{}, err
		}
		return assignment{
			WorkspaceID: mapping.WorkspaceID,
			Sphere:      mapping.Sphere,
		}, nil
	}
	return assignment{}, nil
}

func (s *StoreSink) itemCreateOptions(account store.ExternalAccount, item store.Item, assignment assignment) (store.ItemOptions, error) {
	if strings.TrimSpace(item.Title) == "" {
		return store.ItemOptions{}, errors.New("item title is required")
	}
	opts := store.ItemOptions{
		State:        item.State,
		ArtifactID:   item.ArtifactID,
		ActorID:      item.ActorID,
		VisibleAfter: item.VisibleAfter,
		FollowUpAt:   item.FollowUpAt,
		Source:       item.Source,
		SourceRef:    item.SourceRef,
	}
	opts.WorkspaceID = firstInt64(item.WorkspaceID, assignment.WorkspaceID)
	if opts.WorkspaceID == nil {
		sphere := strings.TrimSpace(item.Sphere)
		if sphere == "" {
			sphere = strings.TrimSpace(stringFromPointer(assignment.Sphere))
		}
		if sphere == "" {
			sphere = account.Sphere
		}
		opts.Sphere = &sphere
	}
	return opts, nil
}

func (s *StoreSink) itemUpdate(account store.ExternalAccount, item store.Item, assignment assignment) (store.ItemUpdate, error) {
	if strings.TrimSpace(item.Title) == "" {
		return store.ItemUpdate{}, errors.New("item title is required")
	}
	update := store.ItemUpdate{
		Title:        stringPointer(item.Title),
		VisibleAfter: item.VisibleAfter,
		FollowUpAt:   item.FollowUpAt,
		Source:       item.Source,
		SourceRef:    item.SourceRef,
	}
	if state := strings.TrimSpace(item.State); state != "" {
		update.State = &state
	}
	if workspaceID := firstInt64(item.WorkspaceID, assignment.WorkspaceID); workspaceID != nil {
		update.WorkspaceID = int64Pointer(*workspaceID)
	}
	if update.WorkspaceID == nil {
		sphere := strings.TrimSpace(item.Sphere)
		if sphere == "" {
			sphere = strings.TrimSpace(stringFromPointer(assignment.Sphere))
		}
		if sphere == "" {
			sphere = account.Sphere
		}
		update.Sphere = &sphere
	}
	if item.ArtifactID != nil {
		update.ArtifactID = int64Pointer(*item.ArtifactID)
	}
	if item.ActorID != nil {
		update.ActorID = int64Pointer(*item.ActorID)
	}
	return update, nil
}

func artifactUpdate(artifact store.Artifact) (store.ArtifactUpdate, error) {
	if strings.TrimSpace(string(artifact.Kind)) == "" && artifact.RefPath == nil && artifact.RefURL == nil && artifact.Title == nil && artifact.MetaJSON == nil {
		return store.ArtifactUpdate{}, nil
	}
	update := store.ArtifactUpdate{
		RefPath:  artifact.RefPath,
		RefURL:   artifact.RefURL,
		Title:    artifact.Title,
		MetaJSON: artifact.MetaJSON,
	}
	if strings.TrimSpace(string(artifact.Kind)) != "" {
		kind := artifact.Kind
		update.Kind = &kind
	}
	return update, nil
}

func (s *StoreSink) linkArtifactWorkspace(artifact store.Artifact, workspaceID *int64) error {
	if workspaceID == nil || *workspaceID <= 0 {
		return nil
	}
	if err := s.store.LinkArtifactToWorkspace(*workspaceID, artifact.ID); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "already belongs to workspace") {
			return nil
		}
		return err
	}
	return nil
}

func normalizeContainerRef(value *string) *string {
	if value == nil {
		return nil
	}
	clean := strings.TrimSpace(*value)
	if clean == "" {
		return nil
	}
	return &clean
}

func stringFromPointer(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func stringPointer(value string) *string {
	return &value
}

func int64Pointer(value int64) *int64 {
	return &value
}

func firstInt64(values ...*int64) *int64 {
	for _, value := range values {
		if value != nil {
			next := *value
			return &next
		}
	}
	return nil
}

func firstString(values ...*string) *string {
	for _, value := range values {
		if value != nil && strings.TrimSpace(*value) != "" {
			next := strings.TrimSpace(*value)
			return &next
		}
	}
	return nil
}
