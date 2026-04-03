package store

import "errors"

func normalizeItemListFilter(filter ItemListFilter) (ItemListFilter, error) {
	normalized := ItemListFilter{
		Source:              normalizeOptionalSourceFilter(filter.Source),
		WorkspaceUnassigned: filter.WorkspaceUnassigned,
	}
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
