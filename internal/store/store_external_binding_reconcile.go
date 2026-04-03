package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

type ExternalBindingReconcileUpdate struct {
	ObjectType        string
	OldRemoteID       string
	NewRemoteID       string
	ContainerRef      *string
	FollowUpItemState *string
}

func (s *Store) ApplyExternalBindingReconcileUpdates(accountID int64, provider string, updates []ExternalBindingReconcileUpdate) error {
	if _, err := s.validateExternalBindingAccount(accountID, provider); err != nil {
		return err
	}
	cleanProvider := normalizeExternalAccountProvider(provider)
	if len(updates) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, update := range updates {
		objectType := normalizeExternalBindingObjectType(update.ObjectType)
		if objectType == "" {
			return errors.New("external binding reconcile update object_type is required")
		}
		oldRemoteID := normalizeExternalBindingRemoteID(update.OldRemoteID)
		newRemoteID := normalizeExternalBindingRemoteID(update.NewRemoteID)
		if oldRemoteID == "" && newRemoteID == "" {
			continue
		}
		lookupRemoteID := oldRemoteID
		if lookupRemoteID == "" {
			lookupRemoteID = newRemoteID
		}

		var (
			bindingID       int64
			itemID          sql.NullInt64
			artifactID      sql.NullInt64
			currentID       string
			container       sql.NullString
			ignoredRemoteAt sql.NullString
			ignoredSyncedAt string
		)
		err := tx.QueryRow(
			`SELECT id, item_id, artifact_id, remote_id, container_ref, remote_updated_at, last_synced_at
			 FROM external_bindings
			 WHERE account_id = ? AND provider = ? AND object_type = ? AND remote_id = ?`,
			accountID,
			cleanProvider,
			objectType,
			lookupRemoteID,
		).Scan(&bindingID, &itemID, &artifactID, &currentID, &container, &ignoredRemoteAt, &ignoredSyncedAt)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}

		targetRemoteID := currentID
		if newRemoteID != "" {
			targetRemoteID = newRemoteID
		}
		targetContainer := normalizeOptionalString(update.ContainerRef)
		if targetContainer == nil {
			targetContainer = nullStringPointer(container)
		}

		if targetRemoteID != currentID {
			var existingID sql.NullInt64
			err = tx.QueryRow(
				`SELECT id
				 FROM external_bindings
				 WHERE account_id = ? AND provider = ? AND object_type = ? AND remote_id = ?`,
				accountID,
				cleanProvider,
				objectType,
				targetRemoteID,
			).Scan(&existingID)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if existingID.Valid && existingID.Int64 != bindingID {
				if _, err := tx.Exec(
					`UPDATE external_bindings
					 SET item_id = COALESCE(item_id, ?),
					     artifact_id = COALESCE(artifact_id, ?),
					     container_ref = ?,
					     last_synced_at = ?
					 WHERE id = ?`,
					nullablePositiveID(itemID.Int64),
					nullablePositiveID(artifactID.Int64),
					targetContainer,
					now,
					existingID.Int64,
				); err != nil {
					return err
				}
				if _, err := tx.Exec(`DELETE FROM external_bindings WHERE id = ?`, bindingID); err != nil {
					return err
				}
				bindingID = existingID.Int64
			} else {
				if _, err := tx.Exec(
					`UPDATE external_bindings
					 SET remote_id = ?, container_ref = ?, last_synced_at = ?
					 WHERE id = ?`,
					targetRemoteID,
					targetContainer,
					now,
					bindingID,
				); err != nil {
					return err
				}
			}
		} else {
			if _, err := tx.Exec(
				`UPDATE external_bindings
				 SET container_ref = ?, last_synced_at = ?
				 WHERE id = ?`,
				targetContainer,
				now,
				bindingID,
			); err != nil {
				return err
			}
		}

		if itemID.Valid && update.FollowUpItemState != nil {
			state := strings.TrimSpace(*update.FollowUpItemState)
			if state != "" {
				if _, err := tx.Exec(`UPDATE items SET state = ? WHERE id = ?`, state, itemID.Int64); err != nil {
					return err
				}
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	tx = nil
	return nil
}
