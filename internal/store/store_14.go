package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
)

func (s *Store) migrateItemArtifactLinkSupport() error {
	if _, err := s.db.Exec(itemArtifactsTableSchema); err != nil {
		return err
	}
	_, err := s.db.Exec(`
INSERT INTO item_artifacts (item_id, artifact_id, role)
SELECT items.id, items.artifact_id, 'source'
FROM items
WHERE items.artifact_id IS NOT NULL
  AND NOT EXISTS (
    SELECT 1
    FROM item_artifacts
    WHERE item_artifacts.item_id = items.id
      AND item_artifacts.artifact_id = items.artifact_id
  )
`)
	return err
}

func (s *Store) syncPrimaryItemArtifact(id int64, artifactID *int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := s.syncPrimaryItemArtifactTx(tx, id, artifactID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) syncPrimaryItemArtifactTx(tx *sql.Tx, id int64, artifactID *int64) error {
	if _, err := scanItem(tx.QueryRow(`SELECT id, title, state, workspace_id, `+scopedContextSelect("context_items", "item_id", "items.id")+` AS sphere, artifact_id, actor_id, visible_after, follow_up_at, source, source_ref, review_target, reviewer, reviewed_at, created_at, updated_at
		 FROM items
		 WHERE id = ?`, id)); err != nil {
		return err
	}
	if artifactID != nil {
		if _, err := scanArtifact(tx.QueryRow(`SELECT id, kind, ref_path, ref_url, title, meta_json, created_at, updated_at
			 FROM artifacts
			 WHERE id = ?`, *artifactID)); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE item_artifacts
		 SET role = 'related'
		 WHERE item_id = ?
		   AND role = 'source'`, id); err != nil {
		return err
	}
	if artifactID != nil {
		if _, err := tx.Exec(`INSERT INTO item_artifacts (item_id, artifact_id, role)
			 VALUES (?, ?, 'source')
			 ON CONFLICT(item_id, artifact_id) DO UPDATE SET role = excluded.role`, id, *artifactID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE items
		 SET artifact_id = ?, updated_at = datetime('now')
		 WHERE id = ?`, artifactID, id); err != nil {
		return err
	}
	return nil
}

func (s *Store) touchItemTx(tx *sql.Tx, id int64) error {
	res, err := tx.Exec(`UPDATE items SET updated_at = datetime('now') WHERE id = ?`, id)
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

func (s *Store) choosePrimaryItemArtifactTx(tx *sql.Tx, itemID int64) (*int64, error) {
	var next sql.NullInt64
	err := tx.QueryRow(`SELECT artifact_id
		 FROM item_artifacts
		 WHERE item_id = ?
		 ORDER BY CASE role WHEN 'source' THEN 0 WHEN 'related' THEN 1 ELSE 2 END,
		          datetime(created_at) ASC,
		          artifact_id ASC
		 LIMIT 1`, itemID).Scan(&next)
	if errors.Is(err, sql.ErrNoRows) || !next.Valid {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	value := next.Int64
	return &value, nil
}

func (s *Store) LinkItemArtifact(itemID, artifactID int64, role string) error {
	cleanRole := normalizeItemArtifactRole(role)
	if cleanRole == "" {
		return errors.New("item artifact role must be source, related, or output")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	item, err := scanItem(tx.QueryRow(`SELECT id, title, state, workspace_id, `+scopedContextSelect("context_items", "item_id", "items.id")+` AS sphere, artifact_id, actor_id, visible_after, follow_up_at, source, source_ref, review_target, reviewer, reviewed_at, created_at, updated_at
		 FROM items
		 WHERE id = ?`, itemID))
	if err != nil {
		return err
	}
	if item.ArtifactID != nil && *item.ArtifactID == artifactID {
		cleanRole = "source"
	}
	if cleanRole == "source" {
		if err := s.syncPrimaryItemArtifactTx(tx, itemID, &artifactID); err != nil {
			return err
		}
		return tx.Commit()
	}
	if _, err := scanArtifact(tx.QueryRow(`SELECT id, kind, ref_path, ref_url, title, meta_json, created_at, updated_at
		 FROM artifacts
		 WHERE id = ?`, artifactID)); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO item_artifacts (item_id, artifact_id, role)
		 VALUES (?, ?, ?)
		 ON CONFLICT(item_id, artifact_id) DO UPDATE SET role = excluded.role`, itemID, artifactID, cleanRole); err != nil {
		return err
	}
	if err := s.touchItemTx(tx, itemID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) UnlinkItemArtifact(itemID, artifactID int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	item, err := scanItem(tx.QueryRow(`SELECT id, title, state, workspace_id, `+scopedContextSelect("context_items", "item_id", "items.id")+` AS sphere, artifact_id, actor_id, visible_after, follow_up_at, source, source_ref, review_target, reviewer, reviewed_at, created_at, updated_at
		 FROM items
		 WHERE id = ?`, itemID))
	if err != nil {
		return err
	}
	res, err := tx.Exec(`DELETE FROM item_artifacts
		 WHERE item_id = ? AND artifact_id = ?`, itemID, artifactID)
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
	if item.ArtifactID != nil && *item.ArtifactID == artifactID {
		nextArtifactID, err := s.choosePrimaryItemArtifactTx(tx, itemID)
		if err != nil {
			return err
		}
		if err := s.syncPrimaryItemArtifactTx(tx, itemID, nextArtifactID); err != nil {
			return err
		}
	} else if err := s.touchItemTx(tx, itemID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ListItemArtifactLinks(itemID int64) ([]ItemArtifactLink, error) {
	if _, err := s.GetItem(itemID); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT item_id, artifact_id, role, created_at
		 FROM item_artifacts
		 WHERE item_id = ?
		 ORDER BY CASE role WHEN 'source' THEN 0 WHEN 'related' THEN 1 ELSE 2 END,
		          datetime(created_at) ASC,
		          artifact_id ASC`, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ItemArtifactLink{}
	for rows.Next() {
		var link ItemArtifactLink
		if err := rows.Scan(&link.ItemID, &link.ArtifactID, &link.Role, &link.CreatedAt); err != nil {
			return nil, err
		}
		link.Role = normalizeItemArtifactRole(link.Role)
		out = append(out, link)
	}
	return out, rows.Err()
}

func (s *Store) ListItemArtifacts(itemID int64) ([]ItemArtifact, error) {
	if _, err := s.GetItem(itemID); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT
		   ia.item_id,
		   ia.artifact_id,
		   ia.role,
		   ia.created_at,
		   a.id,
		   a.kind,
		   a.ref_path,
		   a.ref_url,
		   a.title,
		   a.meta_json,
		   a.created_at,
		   a.updated_at
		 FROM item_artifacts ia
		 INNER JOIN artifacts a ON a.id = ia.artifact_id
		 WHERE ia.item_id = ?
		 ORDER BY CASE ia.role WHEN 'source' THEN 0 WHEN 'related' THEN 1 ELSE 2 END,
		          datetime(ia.created_at) ASC,
		          ia.artifact_id ASC`, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ItemArtifact{}
	for rows.Next() {
		var (
			entry                            ItemArtifact
			refPath, refURL, title, metaJSON sql.NullString
		)
		if err := rows.Scan(&entry.ItemID, &entry.ArtifactID, &entry.Role, &entry.LinkCreatedAt, &entry.Artifact.ID, &entry.Artifact.Kind, &refPath, &refURL, &title, &metaJSON, &entry.Artifact.CreatedAt, &entry.Artifact.UpdatedAt); err != nil {
			return nil, err
		}
		entry.Role = normalizeItemArtifactRole(entry.Role)
		entry.Artifact.Kind = normalizeArtifactKind(entry.Artifact.Kind)
		entry.Artifact.RefPath = nullStringPointer(refPath)
		entry.Artifact.RefURL = nullStringPointer(refURL)
		entry.Artifact.Title = nullStringPointer(title)
		entry.Artifact.MetaJSON = nullStringPointer(metaJSON)
		out = append(out, entry)
	}
	return out, rows.Err()
}

func (s *Store) ListArtifactItems(artifactID int64) ([]Item, error) {
	if _, err := s.GetArtifact(artifactID); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT
		   i.id,
		   i.title,
		   i.state,
		   i.workspace_id,
		   `+scopedContextSelect("context_items", "item_id", "i.id")+`,
		   i.artifact_id,
		   i.actor_id,
		   i.visible_after,
		   i.follow_up_at,
		   i.source,
		   i.source_ref,
		   i.review_target,
		   i.reviewer,
		   i.reviewed_at,
		   i.created_at,
		   i.updated_at
		 FROM item_artifacts ia
		 INNER JOIN items i ON i.id = ia.item_id
		 WHERE ia.artifact_id = ?
		 ORDER BY datetime(i.updated_at) DESC, i.id ASC`, artifactID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Item{}
	for rows.Next() {
		item, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func normalizeItemListFilter(filter ItemListFilter) (ItemListFilter, error) {
	normalized := ItemListFilter{Source: normalizeOptionalSourceFilter(filter.Source), WorkspaceUnassigned: filter.WorkspaceUnassigned}
	sphere, err := normalizeOptionalSphereFilter(filter.Sphere)
	if err != nil {
		return ItemListFilter{}, err
	}
	normalized.Sphere = sphere
	if filter.WorkspaceID != nil {
		if *filter.WorkspaceID <= 0 {
			return ItemListFilter{}, errors.New("workspace_id must be a positive integer")
		}
		value := *filter.WorkspaceID
		normalized.WorkspaceID = &value
	}
	if normalized.WorkspaceID != nil && normalized.WorkspaceUnassigned {
		return ItemListFilter{}, errors.New("workspace_id cannot be combined with workspace_id=null")
	}
	normalized.Label = normalizeOptionalContextQuery(filter.Label)
	if filter.LabelID != nil {
		if *filter.LabelID <= 0 {
			return ItemListFilter{}, errors.New("label_id must be a positive integer")
		}
		value := *filter.LabelID
		normalized.LabelID = &value
	}
	if normalized.Label != "" && normalized.LabelID != nil {
		return ItemListFilter{}, errors.New("label cannot be combined with label_id")
	}
	return normalized, nil
}

func (s *Store) prepareItemListFilter(filter ItemListFilter) (ItemListFilter, error) {
	normalized, err := normalizeItemListFilter(filter)
	if err != nil {
		return ItemListFilter{}, err
	}
	if normalized.Label == "" {
		return normalized, nil
	}
	for _, term := range splitContextQueryTerms(normalized.Label) {
		labelIDs, err := s.resolveContextQueryIDs(term)
		if err != nil {
			return ItemListFilter{}, err
		}
		normalized.resolvedLabelGroups = append(normalized.resolvedLabelGroups, labelIDs)
	}
	normalized.labelResolved = true
	return normalized, nil
}

func appendItemFilterClauses(parts []string, args []any, filter ItemListFilter, alias string) ([]string, []any) {
	column := func(name string) string {
		return alias + name
	}
	outerColumn := func(name string) string {
		if alias == "" {
			return "items." + name
		}
		return alias + name
	}
	if filter.Sphere != "" {
		parts = append(parts, scopedContextFilter("context_items", "item_id", outerColumn("id")))
		args = append(args, filter.Sphere)
	}
	if filter.Source != "" {
		parts = append(parts, "lower(trim("+column("source")+")) = ?")
		args = append(args, filter.Source)
	}
	if filter.WorkspaceID != nil {
		parts = append(parts, column("workspace_id")+" = ?")
		args = append(args, *filter.WorkspaceID)
	}
	if filter.WorkspaceUnassigned {
		parts = append(parts, column("workspace_id")+" IS NULL")
	}
	if filter.labelResolved {
		if len(filter.resolvedLabelGroups) == 0 {
			parts = append(parts, "0=1")
			return parts, args
		}
		for _, labelIDs := range filter.resolvedLabelGroups {
			if len(labelIDs) == 0 {
				parts = append(parts, "0=1")
				return parts, args
			}
			labelItemMatch, labelItemArgs := contextLinkExistsClause("context_items", "item_id", outerColumn("id"), labelIDs)
			labelWorkspaceMatch, labelWorkspaceArgs := contextLinkExistsClause("context_workspaces", "workspace_id", outerColumn("workspace_id"), labelIDs)
			parts = append(parts, `(`+labelItemMatch+` OR `+labelWorkspaceMatch+`)`)
			args = append(args, labelItemArgs...)
			args = append(args, labelWorkspaceArgs...)
		}
		return parts, args
	}
	if filter.LabelID != nil {
		contextItemMatch := `EXISTS (
WITH RECURSIVE context_tree(id) AS (
  SELECT id FROM contexts WHERE id = ?
  UNION ALL
  SELECT c.id
  FROM contexts c
  JOIN context_tree tree ON c.parent_id = tree.id
)
SELECT 1
FROM context_items ci
JOIN context_tree tree ON tree.id = ci.context_id
WHERE ci.item_id = ` + outerColumn("id") + `
)`
		contextWorkspaceMatch := `EXISTS (
WITH RECURSIVE context_tree(id) AS (
  SELECT id FROM contexts WHERE id = ?
  UNION ALL
  SELECT c.id
  FROM contexts c
  JOIN context_tree tree ON c.parent_id = tree.id
)
SELECT 1
FROM context_workspaces cw
JOIN context_tree tree ON tree.id = cw.context_id
WHERE cw.workspace_id = ` + outerColumn("workspace_id") + `
)`
		parts = append(parts, `(`+contextItemMatch+` OR `+contextWorkspaceMatch+`)`)
		args = append(args, *filter.LabelID, *filter.LabelID)
	}
	return parts, args
}

func (s *Store) SetItemWorkspace(id int64, workspaceID *int64) error {
	args := []any{nullablePositiveID(valueOrZeroInt64(workspaceID)), id}
	query := `UPDATE items
		 SET workspace_id = ?, updated_at = datetime('now')
		 WHERE id = ?`
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
	if err := s.syncItemDateContext(id, workspaceID); err != nil {
		return err
	}
	return nil
}

func valueOrZeroInt64(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func normalizeWorkspaceName(name string) string {
	return strings.TrimSpace(name)
}

func normalizeWorkspacePath(path string) string {
	p := strings.TrimSpace(path)
	if p == "" {
		return ""
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return filepath.Clean(p)
	}
	return filepath.Clean(abs)
}

func normalizeActorName(name string) string {
	return strings.TrimSpace(name)
}

func normalizeActorKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case ActorKindHuman:
		return ActorKindHuman
	case ActorKindAgent:
		return ActorKindAgent
	default:
		return ""
	}
}

func normalizeActorEmail(email string) string {
	clean := strings.ToLower(strings.TrimSpace(email))
	if clean == "" {
		return ""
	}
	return clean
}
