package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

func (s *Store) UpdateItem(id int64, updates ItemUpdate) error {
	item, err := s.GetItem(id)
	if err != nil {
		return err
	}
	parts := []string{}
	args := []any{}
	artifactUpdated := false
	scopeUpdated := false
	var artifactID *int64
	targetWorkspaceID := item.WorkspaceID
	reopenToInbox := false
	if updates.Title != nil {
		title := strings.TrimSpace(*updates.Title)
		if title == "" {
			return errors.New("item title is required")
		}
		parts = append(parts, "title = ?")
		args = append(args, title)
	}
	if updates.State != nil {
		next := normalizeItemState(*updates.State)
		if err := validateItemTransition(item.State, next); err != nil {
			return err
		}
		reopenToInbox = next == ItemStateInbox && item.State != ItemStateInbox
		parts = append(parts, "state = ?")
		args = append(args, next)
	}
	if updates.WorkspaceID != nil {
		parts = append(parts, "workspace_id = ?")
		args = append(args, nullablePositiveID(*updates.WorkspaceID))
		if *updates.WorkspaceID > 0 {
			value := *updates.WorkspaceID
			targetWorkspaceID = &value
		} else {
			targetWorkspaceID = nil
		}
	}
	if updates.Sphere != nil {
		if targetWorkspaceID != nil {
			return errors.New("item sphere is derived from workspace")
		}
		nextSphere := normalizeRequiredSphere(*updates.Sphere)
		if nextSphere == "" {
			return errors.New("item sphere must be work or private")
		}
		item.Sphere = nextSphere
		scopeUpdated = true
	}
	if updates.ArtifactID != nil {
		artifactUpdated = true
		if *updates.ArtifactID > 0 {
			value := *updates.ArtifactID
			artifactID = &value
		}
	}
	if updates.ActorID != nil {
		parts = append(parts, "actor_id = ?")
		args = append(args, nullablePositiveID(*updates.ActorID))
	}
	if updates.VisibleAfter != nil {
		value, err := normalizeOptionalRFC3339String(updates.VisibleAfter)
		if err != nil {
			return err
		}
		parts = append(parts, "visible_after = ?")
		args = append(args, value)
	}
	if updates.FollowUpAt != nil {
		value, err := normalizeOptionalRFC3339String(updates.FollowUpAt)
		if err != nil {
			return err
		}
		parts = append(parts, "follow_up_at = ?")
		args = append(args, value)
	}
	if updates.Source != nil {
		sourceValue := strings.TrimSpace(*updates.Source)
		sourceRefValue := strings.TrimSpace(nullStringValue(updates.SourceRef))
		switch {
		case sourceValue == "" && sourceRefValue != "":
			return errors.New("item source and source_ref are required")
		case sourceValue != "" && sourceRefValue == "":
			return errors.New("item source and source_ref are required")
		case sourceValue != "" && sourceRefValue != "":
			if err := s.UpdateItemSource(id, sourceValue, sourceRefValue); err != nil {
				return err
			}
		case sourceValue == "" && sourceRefValue == "":
			parts = append(parts, "source = ?", "source_ref = ?")
			args = append(args, nil, nil)
		}
	}
	if updates.ReviewTarget != nil || updates.Reviewer != nil {
		if err := validateReviewTargetPointer(updates.ReviewTarget); err != nil {
			return err
		}
		cleanTarget := normalizedReviewTargetPointer(updates.ReviewTarget)
		cleanReviewer := normalizedReviewerPointer(updates.Reviewer)
		if cleanTarget == nil && cleanReviewer != nil {
			return errors.New("review target is required when reviewer is set")
		}
		parts = append(parts, "review_target = ?", "reviewer = ?", "reviewed_at = ?")
		args = append(args, normalizeOptionalString(cleanTarget), normalizeOptionalString(cleanReviewer), normalizeOptionalString(reviewTimestampPointer(updates.ReviewTarget, updates.Reviewer)))
	}
	if targetWorkspaceID != nil {
		workspaceSphere, err := s.workspaceSphere(*targetWorkspaceID)
		if err != nil {
			return err
		}
		item.Sphere = workspaceSphere
		scopeUpdated = true
	}
	if reopenToInbox {
		if updates.VisibleAfter == nil {
			parts = append(parts, "visible_after = NULL")
		}
		if updates.FollowUpAt == nil {
			parts = append(parts, "follow_up_at = NULL")
		}
	}
	if len(parts) == 0 && !artifactUpdated && !scopeUpdated {
		return nil
	}
	if len(parts) > 0 {
		parts = append(parts, "updated_at = datetime('now')")
		args = append(args, id)
		res, err := s.db.Exec(`UPDATE items SET `+stringsJoin(parts, ", ")+` WHERE id = ?`, args...)
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
	}
	if artifactUpdated {
		if err := s.syncPrimaryItemArtifact(id, artifactID); err != nil {
			return err
		}
	}
	if item.Sphere != "" {
		if err := s.syncScopedContextLink("context_items", "item_id", id, item.Sphere); err != nil {
			return err
		}
	}
	if updates.WorkspaceID != nil || item.WorkspaceID != nil {
		if err := s.syncItemDateContext(id, targetWorkspaceID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) SetItemSphere(id int64, sphere string) error {
	item, err := s.GetItem(id)
	if err != nil {
		return err
	}
	if item.WorkspaceID != nil {
		return errors.New("item sphere is derived from workspace")
	}
	cleanSphere := normalizeRequiredSphere(sphere)
	if cleanSphere == "" {
		return errors.New("item sphere must be work or private")
	}
	res, err := s.db.Exec(`UPDATE items SET updated_at = datetime('now') WHERE id = ?`, id)
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
	return s.syncScopedContextLink("context_items", "item_id", id, cleanSphere)
}

func (s *Store) ListSphereAccounts(sphere string) ([]ExternalAccount, error) {
	return s.ListExternalAccounts(sphere)
}

func (s *Store) AddSphereAccount(sphere, kind, label string, config map[string]any) (ExternalAccount, error) {
	return s.CreateExternalAccount(sphere, kind, label, config)
}

func (s *Store) RemoveSphereAccount(id int64) error {
	return s.DeleteExternalAccount(id)
}

func (s *Store) SyncItemStateBySource(source, sourceRef, state string) error {
	cleanSource := strings.TrimSpace(source)
	cleanSourceRef := strings.TrimSpace(sourceRef)
	cleanState := normalizeItemState(state)
	if cleanSource == "" || cleanSourceRef == "" {
		return errors.New("item source and source_ref are required")
	}
	if cleanState == "" {
		return errors.New("invalid item state")
	}
	query := `UPDATE items
		 SET state = ?, updated_at = datetime('now')`
	args := []any{cleanState}
	if cleanState == ItemStateInbox {
		query += `, visible_after = NULL, follow_up_at = NULL`
	}
	query += `
		 WHERE source = ? AND source_ref = ?`
	args = append(args, cleanSource, cleanSourceRef)
	res, err := s.db.Exec(query, args...)
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

func (s *Store) UpdateItemState(id int64, state string) error {
	item, err := s.GetItem(id)
	if err != nil {
		return err
	}
	next := normalizeItemState(state)
	if err := validateItemTransition(item.State, next); err != nil {
		return err
	}
	query := `UPDATE items SET state = ?, updated_at = datetime('now')`
	args := []any{next}
	if next == ItemStateInbox {
		query += `, visible_after = NULL, follow_up_at = NULL`
	}
	query += ` WHERE id = ?`
	args = append(args, id)
	res, err := s.db.Exec(query, args...)
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

func (s *Store) triageableItem(id int64) (Item, error) {
	item, err := s.GetItem(id)
	if err != nil {
		return Item{}, err
	}
	if item.State == ItemStateDone {
		return Item{}, fmt.Errorf("cannot triage item in %s state", item.State)
	}
	return item, nil
}

func normalizeRFC3339String(value string) (string, error) {
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return "", err
	}
	return parsed.UTC().Format(time.RFC3339Nano), nil
}

func (s *Store) TriageItemDone(id int64) error {
	if _, err := s.triageableItem(id); err != nil {
		return err
	}
	res, err := s.db.Exec(`UPDATE items
		 SET state = ?, updated_at = datetime('now')
		 WHERE id = ?`, ItemStateDone, id)
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

func (s *Store) TriageItemLater(id int64, visibleAfter string) error {
	if _, err := s.triageableItem(id); err != nil {
		return err
	}
	normalized, err := normalizeRFC3339String(visibleAfter)
	if err != nil {
		return errors.New("visible_after must be a valid RFC3339 timestamp")
	}
	res, err := s.db.Exec(`UPDATE items
		 SET state = ?, visible_after = ?, updated_at = datetime('now')
		 WHERE id = ?`, ItemStateWaiting, normalized, id)
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

func (s *Store) TriageItemDelegate(id, actorID int64) error {
	if _, err := s.triageableItem(id); err != nil {
		return err
	}
	if _, err := s.GetActor(actorID); err != nil {
		return err
	}
	res, err := s.db.Exec(`UPDATE items
		 SET actor_id = ?, state = ?, visible_after = NULL, updated_at = datetime('now')
		 WHERE id = ?`, actorID, ItemStateWaiting, id)
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

func (s *Store) TriageItemDelete(id int64) error {
	if _, err := s.triageableItem(id); err != nil {
		return err
	}
	return s.DeleteItem(id)
}

func (s *Store) TriageItemSomeday(id int64) error {
	if _, err := s.triageableItem(id); err != nil {
		return err
	}
	res, err := s.db.Exec(`UPDATE items
		 SET state = ?, visible_after = NULL, follow_up_at = NULL, updated_at = datetime('now')
		 WHERE id = ?`, ItemStateSomeday, id)
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

func (s *Store) AssignItem(id, actorID int64) error {
	if _, err := s.GetActor(actorID); err != nil {
		return err
	}
	item, err := s.GetItem(id)
	if err != nil {
		return err
	}
	if item.State == ItemStateDone {
		return fmt.Errorf("cannot assign item in %s state", item.State)
	}
	res, err := s.db.Exec(`UPDATE items
		 SET actor_id = ?, state = ?, updated_at = datetime('now')
		 WHERE id = ?`, actorID, ItemStateWaiting, id)
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

func (s *Store) UnassignItem(id int64) error {
	item, err := s.GetItem(id)
	if err != nil {
		return err
	}
	if item.State == ItemStateDone {
		return fmt.Errorf("cannot unassign item in %s state", item.State)
	}
	if item.ActorID == nil {
		return errors.New("item is not assigned")
	}
	res, err := s.db.Exec(`UPDATE items
		 SET actor_id = NULL, state = ?, visible_after = NULL, follow_up_at = NULL, updated_at = datetime('now')
		 WHERE id = ?`, ItemStateInbox, id)
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

func (s *Store) CompleteItemByActor(id, actorID int64) error {
	item, err := s.GetItem(id)
	if err != nil {
		return err
	}
	if item.State == ItemStateDone {
		return fmt.Errorf("cannot complete item in %s state", item.State)
	}
	if item.ActorID == nil {
		return errors.New("item is not assigned")
	}
	if *item.ActorID != actorID {
		return errors.New("item actor does not match")
	}
	res, err := s.db.Exec(`UPDATE items
		 SET state = ?, updated_at = datetime('now')
		 WHERE id = ?`, ItemStateDone, id)
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

func (s *Store) ReturnItemToInbox(id int64) error {
	item, err := s.GetItem(id)
	if err != nil {
		return err
	}
	if item.State == ItemStateDone {
		return fmt.Errorf("cannot return item from %s state", item.State)
	}
	res, err := s.db.Exec(`UPDATE items
		 SET state = ?, visible_after = NULL, follow_up_at = NULL, updated_at = datetime('now')
		 WHERE id = ?`, ItemStateInbox, id)
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
