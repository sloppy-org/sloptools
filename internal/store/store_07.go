package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"
)

func githubOwnerRepoFromMeta(metaJSON string) string {
	var payload map[string]any
	if err := json.Unmarshal([]byte(metaJSON), &payload); err != nil {
		return ""
	}
	for _, key := range []string{"owner_repo", "repo", "source_ref", "url", "html_url"} {
		value, _ := payload[key].(string)
		if repo := normalizeGitHubOwnerRepo(value); repo != "" {
			return repo
		}
		if repo := githubOwnerRepoFromURL(value); repo != "" {
			return repo
		}
	}
	return ""
}

func workspaceGitRemoteOwnerRepo(dirPath string) (string, error) {
	cmd := exec.Command("git", "-C", dirPath, "remote", "get-url", "origin")
	output, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", nil
		}
		return "", err
	}
	return normalizeGitHubOwnerRepo(string(output)), nil
}

func (s *Store) CreateActor(name, kind string) (Actor, error) {
	return s.CreateActorWithOptions(name, kind, ActorOptions{})
}

func (s *Store) CreateActorWithOptions(name, kind string, opts ActorOptions) (Actor, error) {
	cleanName := normalizeActorName(name)
	cleanKind := normalizeActorKind(kind)
	cleanEmail := ""
	if opts.Email != nil {
		cleanEmail = normalizeActorEmail(*opts.Email)
	}
	cleanProvider := ""
	if opts.Provider != nil {
		cleanProvider = normalizeActorProvider(*opts.Provider)
	}
	cleanProviderRef := ""
	if opts.ProviderRef != nil {
		cleanProviderRef = strings.TrimSpace(*opts.ProviderRef)
	}
	metaJSON, err := normalizeOptionalJSON(opts.MetaJSON)
	if err != nil {
		return Actor{}, err
	}
	if cleanName == "" {
		return Actor{}, errors.New("actor name is required")
	}
	if cleanKind == "" {
		return Actor{}, errors.New("actor kind must be human or agent")
	}
	if cleanProvider == "" && cleanProviderRef != "" {
		return Actor{}, errors.New("actor provider is required when provider_ref is set")
	}
	res, err := s.db.Exec(`INSERT INTO actors (name, kind, email, provider, provider_ref, meta_json)
		 VALUES (?, ?, ?, ?, ?, ?)`, cleanName, cleanKind, nullableString(cleanEmail), nullableString(cleanProvider), nullableString(cleanProviderRef), metaJSON)
	if err != nil {
		return Actor{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Actor{}, err
	}
	return s.GetActor(id)
}

func (s *Store) GetActor(id int64) (Actor, error) {
	return scanActor(s.db.QueryRow(`SELECT id, name, kind, email, provider, provider_ref, meta_json, created_at
		 FROM actors
		 WHERE id = ?`, id))
}

func (s *Store) GetActorByEmail(email string) (Actor, error) {
	cleanEmail := normalizeActorEmail(email)
	if cleanEmail == "" {
		return Actor{}, errors.New("actor email is required")
	}
	return scanActor(s.db.QueryRow(`SELECT id, name, kind, email, provider, provider_ref, meta_json, created_at
		 FROM actors
		 WHERE lower(email) = ?`, cleanEmail))
}

func (s *Store) GetActorByProviderRef(provider, providerRef string) (Actor, error) {
	cleanProvider := normalizeActorProvider(provider)
	cleanProviderRef := strings.TrimSpace(providerRef)
	if cleanProvider == "" || cleanProviderRef == "" {
		return Actor{}, errors.New("actor provider and provider_ref are required")
	}
	return scanActor(s.db.QueryRow(`SELECT id, name, kind, email, provider, provider_ref, meta_json, created_at
		 FROM actors
		 WHERE lower(provider) = ? AND provider_ref = ?`, cleanProvider, cleanProviderRef))
}

func (s *Store) ListActors() ([]Actor, error) {
	rows, err := s.db.Query(`SELECT id, name, kind, email, provider, provider_ref, meta_json, created_at FROM actors`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Actor
	for rows.Next() {
		actor, err := scanActor(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, actor)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

func (s *Store) UpsertActorContact(name, email, provider, providerRef string, metaJSON *string) (Actor, error) {
	cleanName := normalizeActorName(name)
	cleanEmail := normalizeActorEmail(email)
	cleanProvider := normalizeActorProvider(provider)
	cleanProviderRef := strings.TrimSpace(providerRef)
	if cleanName == "" && cleanEmail == "" {
		return Actor{}, errors.New("actor contact name or email is required")
	}
	if cleanName == "" {
		cleanName = cleanEmail
	}
	if cleanProvider == "" && cleanProviderRef != "" {
		return Actor{}, errors.New("actor provider is required when provider_ref is set")
	}
	if cleanProvider != "" && cleanProviderRef != "" {
		actor, err := s.GetActorByProviderRef(cleanProvider, cleanProviderRef)
		switch {
		case err == nil:
			return s.updateActorContact(actor, cleanName, cleanEmail, cleanProvider, cleanProviderRef, metaJSON)
		case !errors.Is(err, sql.ErrNoRows):
			return Actor{}, err
		}
	}
	if cleanEmail != "" {
		actor, err := s.GetActorByEmail(cleanEmail)
		switch {
		case err == nil:
			return s.updateActorContact(actor, cleanName, cleanEmail, cleanProvider, cleanProviderRef, metaJSON)
		case !errors.Is(err, sql.ErrNoRows):
			return Actor{}, err
		}
	}
	return s.CreateActorWithOptions(cleanName, ActorKindHuman, ActorOptions{Email: stringPointer(cleanEmail), Provider: stringPointer(cleanProvider), ProviderRef: stringPointer(cleanProviderRef), MetaJSON: metaJSON})
}

func (s *Store) updateActorContact(existing Actor, name, email, provider, providerRef string, metaJSON *string) (Actor, error) {
	nextName := existing.Name
	if strings.TrimSpace(name) != "" {
		nextName = normalizeActorName(name)
	}
	nextEmail := existing.Email
	if email != "" {
		nextEmail = stringPointer(normalizeActorEmail(email))
	}
	nextProvider := existing.Provider
	if provider != "" {
		nextProvider = stringPointer(normalizeActorProvider(provider))
	}
	nextProviderRef := existing.ProviderRef
	if strings.TrimSpace(providerRef) != "" {
		nextProviderRef = stringPointer(strings.TrimSpace(providerRef))
	}
	nextMetaJSON := existing.MetaJSON
	if metaJSON != nil {
		cleanMetaJSON, err := normalizeOptionalJSON(metaJSON)
		if err != nil {
			return Actor{}, err
		}
		nextMetaJSON = nil
		if cleanMetaJSON != nil {
			nextMetaJSON = stringPointer(cleanMetaJSON.(string))
		}
	}
	if _, err := s.db.Exec(`UPDATE actors
		 SET name = ?, email = ?, provider = ?, provider_ref = ?, meta_json = ?
		 WHERE id = ?`, nextName, nullablePointerString(nextEmail), nullablePointerString(nextProvider), nullablePointerString(nextProviderRef), nullablePointerString(nextMetaJSON), existing.ID); err != nil {
		return Actor{}, err
	}
	return s.GetActor(existing.ID)
}

func (s *Store) DeleteActor(id int64) error {
	res, err := s.db.Exec(`DELETE FROM actors WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func nullablePointerString(value *string) any {
	if value == nil {
		return nil
	}
	return nullableString(*value)
}

func stringPointer(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	out := value
	return &out
}

func (s *Store) CreateItem(title string, opts ItemOptions) (Item, error) {
	cleanTitle := strings.TrimSpace(title)
	cleanState := normalizeItemState(opts.State)
	if cleanTitle == "" {
		return Item{}, errors.New("item title is required")
	}
	if cleanState == "" {
		return Item{}, errors.New("invalid item state")
	}
	if opts.WorkspaceID == nil && opts.ArtifactID != nil {
		artifact, err := s.GetArtifact(*opts.ArtifactID)
		if err != nil {
			return Item{}, err
		}
		inferredWorkspaceID, err := s.InferWorkspaceForArtifact(artifact)
		if err != nil {
			return Item{}, err
		}
		opts.WorkspaceID = inferredWorkspaceID
	}
	if opts.WorkspaceID != nil && *opts.WorkspaceID > 0 {
		if _, err := s.GetWorkspace(*opts.WorkspaceID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return Item{}, errors.New("foreign key constraint failed: workspace_id")
			}
			return Item{}, err
		}
	}
	itemSphere, err := s.resolveItemSphere(opts.WorkspaceID, opts.Sphere)
	if err != nil {
		return Item{}, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return Item{}, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`INSERT INTO items (
			title, state, workspace_id, artifact_id, actor_id, visible_after, follow_up_at, source, source_ref, review_target, reviewer, reviewed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, cleanTitle, cleanState, opts.WorkspaceID, opts.ArtifactID, opts.ActorID, normalizeOptionalString(opts.VisibleAfter), normalizeOptionalString(opts.FollowUpAt), normalizeOptionalString(opts.Source), normalizeOptionalString(opts.SourceRef), normalizeOptionalString(normalizedReviewTargetPointer(opts.ReviewTarget)), normalizeOptionalString(normalizedReviewerPointer(opts.Reviewer)), normalizeOptionalString(reviewTimestampPointer(opts.ReviewTarget, opts.Reviewer)))
	if err != nil {
		return Item{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Item{}, err
	}
	if err := s.syncPrimaryItemArtifactTx(tx, id, opts.ArtifactID); err != nil {
		return Item{}, err
	}
	if err := tx.Commit(); err != nil {
		return Item{}, err
	}
	if err := s.syncScopedContextLink("context_items", "item_id", id, itemSphere); err != nil {
		return Item{}, err
	}
	if opts.WorkspaceID != nil && *opts.WorkspaceID > 0 {
		if err := s.syncItemDateContext(id, opts.WorkspaceID); err != nil {
			return Item{}, err
		}
	}
	return s.GetItem(id)
}

func (s *Store) GetItem(id int64) (Item, error) {
	return scanItem(s.db.QueryRow(`SELECT id, title, state, workspace_id, `+scopedContextSelect("context_items", "item_id", "items.id")+` AS sphere, artifact_id, actor_id, visible_after, follow_up_at, source, source_ref, review_target, reviewer, reviewed_at, created_at, updated_at
		 FROM items
		 WHERE id = ?`, id))
}

func (s *Store) GetItemBySource(source, sourceRef string) (Item, error) {
	cleanSource := strings.TrimSpace(source)
	cleanSourceRef := strings.TrimSpace(sourceRef)
	if cleanSource == "" || cleanSourceRef == "" {
		return Item{}, errors.New("item source and source_ref are required")
	}
	return scanItem(s.db.QueryRow(`SELECT id, title, state, workspace_id, `+scopedContextSelect("context_items", "item_id", "items.id")+` AS sphere, artifact_id, actor_id, visible_after, follow_up_at, source, source_ref, review_target, reviewer, reviewed_at, created_at, updated_at
		 FROM items
		 WHERE source = ? AND source_ref = ?`, cleanSource, cleanSourceRef))
}

func (s *Store) UpsertItemFromSource(source, sourceRef, title string, workspaceID *int64) (Item, error) {
	cleanSource := strings.TrimSpace(source)
	cleanSourceRef := strings.TrimSpace(sourceRef)
	cleanTitle := strings.TrimSpace(title)
	if cleanSource == "" || cleanSourceRef == "" {
		return Item{}, errors.New("item source and source_ref are required")
	}
	if cleanTitle == "" {
		return Item{}, errors.New("item title is required")
	}
	existing, err := s.GetItemBySource(cleanSource, cleanSourceRef)
	switch {
	case err == nil:
		itemSphere, err := s.resolveItemSphere(workspaceID, &existing.Sphere)
		if err != nil {
			return Item{}, err
		}
		res, err := s.db.Exec(`UPDATE items
			 SET title = ?, workspace_id = ?, updated_at = datetime('now')
		 WHERE id = ?`, cleanTitle, workspaceID, existing.ID)
		if err != nil {
			return Item{}, err
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return Item{}, err
		}
		if affected == 0 {
			return Item{}, sql.ErrNoRows
		}
		if err := s.syncScopedContextLink("context_items", "item_id", existing.ID, itemSphere); err != nil {
			return Item{}, err
		}
		if workspaceID != nil || existing.WorkspaceID != nil {
			if err := s.syncItemDateContext(existing.ID, workspaceID); err != nil {
				return Item{}, err
			}
		}
		return s.GetItem(existing.ID)
	case !errors.Is(err, sql.ErrNoRows):
		return Item{}, err
	}
	return s.CreateItem(cleanTitle, ItemOptions{WorkspaceID: workspaceID, Source: &cleanSource, SourceRef: &cleanSourceRef})
}

func (s *Store) UpdateItemArtifact(id int64, artifactID *int64) error {
	return s.syncPrimaryItemArtifact(id, artifactID)
}

func (s *Store) UpdateItemSource(id int64, source, sourceRef string) error {
	cleanSource := strings.TrimSpace(source)
	cleanSourceRef := strings.TrimSpace(sourceRef)
	if cleanSource == "" || cleanSourceRef == "" {
		return errors.New("item source and source_ref are required")
	}
	existing, err := s.GetItemBySource(cleanSource, cleanSourceRef)
	switch {
	case err == nil && existing.ID != id:
		return fmt.Errorf("item source %s:%s is already linked to item %d", cleanSource, cleanSourceRef, existing.ID)
	case err != nil && !errors.Is(err, sql.ErrNoRows):
		return err
	}
	res, err := s.db.Exec(`UPDATE items
		 SET source = ?, source_ref = ?, updated_at = datetime('now')
		 WHERE id = ?`, cleanSource, cleanSourceRef, id)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func normalizedReviewTargetPointer(value *string) *string {
	if value == nil {
		return nil
	}
	clean := normalizeItemReviewTarget(*value)
	if clean == "" {
		return nil
	}
	return &clean
}

func validateReviewTargetPointer(value *string) error {
	if value == nil {
		return nil
	}
	if strings.TrimSpace(*value) == "" {
		return nil
	}
	if normalizedReviewTargetPointer(value) == nil {
		return errors.New("review target must be agent, github, or email")
	}
	return nil
}

func normalizedReviewerPointer(value *string) *string {
	if value == nil {
		return nil
	}
	clean := strings.TrimSpace(*value)
	if clean == "" {
		return nil
	}
	return &clean
}

func reviewTimestampPointer(target, reviewer *string) *string {
	if normalizedReviewTargetPointer(target) == nil && normalizedReviewerPointer(reviewer) == nil {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	return &now
}

func (s *Store) UpdateItemReviewDispatch(id int64, target, reviewer *string) error {
	if err := validateReviewTargetPointer(target); err != nil {
		return err
	}
	cleanTarget := normalizedReviewTargetPointer(target)
	cleanReviewer := normalizedReviewerPointer(reviewer)
	if cleanTarget == nil && cleanReviewer != nil {
		return errors.New("review target is required when reviewer is set")
	}
	res, err := s.db.Exec(`UPDATE items
		 SET review_target = ?, reviewer = ?, reviewed_at = ?, updated_at = datetime('now')
		 WHERE id = ?`, normalizeOptionalString(cleanTarget), normalizeOptionalString(cleanReviewer), normalizeOptionalString(reviewTimestampPointer(target, reviewer)), id)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}
