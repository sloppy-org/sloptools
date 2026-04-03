package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

// EnsureDateContextHierarchy creates or repairs the YYYY -> YYYY/MM -> YYYY/MM/DD
// context chain for a given UTC calendar date and returns the day-level context ID.
func (s *Store) EnsureDateContextHierarchy(date time.Time) (int64, error) {
	names := dateContextNames(date)
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var (
		parentID *int64
		lastID   int64
	)
	for _, name := range names {
		contextID, err := ensureContextWithParentTx(tx, name, parentID)
		if err != nil {
			return 0, err
		}
		lastID = contextID
		parentID = &lastID
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return lastID, nil
}

func dateContextNames(date time.Time) []string {
	utc := date.UTC()
	return []string{
		utc.Format("2006"),
		utc.Format("2006/01"),
		utc.Format("2006/01/02"),
	}
}

func ensureContextWithParentTx(tx *sql.Tx, name string, parentID *int64) (int64, error) {
	cleanName := strings.TrimSpace(name)
	if cleanName == "" {
		return 0, errors.New("context name is required")
	}
	var (
		contextID        int64
		existingParentID sql.NullInt64
	)
	err := tx.QueryRow(
		`SELECT id, parent_id
		 FROM contexts
		 WHERE lower(name) = lower(?)`,
		cleanName,
	).Scan(&contextID, &existingParentID)
	switch {
	case err == nil:
		if !sameContextParent(existingParentID, parentID) {
			if _, err := tx.Exec(`UPDATE contexts SET parent_id = ? WHERE id = ?`, parentID, contextID); err != nil {
				return 0, err
			}
		}
		return contextID, nil
	case err != nil && !errors.Is(err, sql.ErrNoRows):
		return 0, err
	}

	res, err := tx.Exec(`INSERT INTO contexts (name, parent_id) VALUES (?, ?)`, cleanName, parentID)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func sameContextParent(existing sql.NullInt64, parentID *int64) bool {
	switch {
	case !existing.Valid && parentID == nil:
		return true
	case !existing.Valid || parentID == nil:
		return false
	default:
		return existing.Int64 == *parentID
	}
}

func parseDailyWorkspaceDate(value string) (time.Time, error) {
	clean := normalizeDailyWorkspaceDate(value)
	if clean == "" {
		return time.Time{}, errors.New("daily workspace date must be YYYY-MM-DD")
	}
	return time.Parse("2006-01-02", clean)
}

func (s *Store) syncWorkspaceDateContext(workspaceID int64, dailyDate *string) error {
	if workspaceID <= 0 || dailyDate == nil || strings.TrimSpace(*dailyDate) == "" {
		return nil
	}
	date, err := parseDailyWorkspaceDate(*dailyDate)
	if err != nil {
		return err
	}
	contextID, err := s.EnsureDateContextHierarchy(date)
	if err != nil {
		return err
	}
	return s.LinkLabelToWorkspace(contextID, workspaceID)
}

func (s *Store) syncItemDateContext(itemID int64, workspaceID *int64) error {
	if itemID <= 0 {
		return nil
	}
	if _, err := s.db.Exec(
		`DELETE FROM context_items
		 WHERE item_id = ?
		   AND context_id IN (
		     SELECT id
		     FROM contexts
		     WHERE name GLOB '[0-9][0-9][0-9][0-9]/[0-9][0-9]/[0-9][0-9]'
		   )`,
		itemID,
	); err != nil {
		return err
	}
	if workspaceID == nil || *workspaceID <= 0 {
		return nil
	}
	workspace, err := s.GetWorkspace(*workspaceID)
	if err != nil {
		return err
	}
	if workspace.DailyDate == nil || strings.TrimSpace(*workspace.DailyDate) == "" {
		return nil
	}
	date, err := parseDailyWorkspaceDate(*workspace.DailyDate)
	if err != nil {
		return err
	}
	contextID, err := s.EnsureDateContextHierarchy(date)
	if err != nil {
		return err
	}
	return s.LinkLabelToItem(contextID, itemID)
}
