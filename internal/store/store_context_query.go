package store

import (
	"database/sql"
	"errors"
	"sort"
	"strings"
)

func normalizeOptionalContextQuery(value string) string {
	return strings.TrimSpace(value)
}

func splitContextQueryTerms(query string) []string {
	cleanQuery := normalizeOptionalContextQuery(query)
	if cleanQuery == "" {
		return nil
	}
	rawTerms := strings.FieldsFunc(cleanQuery, func(r rune) bool {
		return r == '+' || r == ','
	})
	terms := make([]string, 0, len(rawTerms))
	for _, term := range rawTerms {
		clean := normalizeOptionalContextQuery(term)
		if clean == "" {
			continue
		}
		terms = append(terms, clean)
	}
	if len(terms) > 0 {
		return terms
	}
	return []string{cleanQuery}
}

func placeholders(count int) string {
	if count <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", count), ",")
}

func contextLinkExistsClause(linkTable, entityColumn, entityExpr string, contextIDs []int64) (string, []any) {
	if len(contextIDs) == 0 {
		return "0=1", nil
	}
	args := make([]any, 0, len(contextIDs))
	for _, contextID := range contextIDs {
		args = append(args, contextID)
	}
	return `EXISTS (
SELECT 1
FROM ` + linkTable + ` link
WHERE link.` + entityColumn + ` = ` + entityExpr + `
  AND link.context_id IN (` + placeholders(len(contextIDs)) + `)
)`, args
}

func (s *Store) resolveContextQueryIDs(query string) ([]int64, error) {
	cleanQuery := strings.ToLower(strings.TrimSpace(query))
	if cleanQuery == "" {
		return nil, nil
	}
	rows, err := s.db.Query(
		`WITH RECURSIVE context_paths(id, parent_id, name, path) AS (
		   SELECT id, parent_id, lower(name), lower(name)
		   FROM contexts
		   WHERE parent_id IS NULL
		   UNION ALL
		   SELECT c.id, c.parent_id, lower(c.name), cp.path || '/' || lower(c.name)
		   FROM contexts c
		   JOIN context_paths cp ON cp.id = c.parent_id
		 ),
		 matched_exact(id) AS (
		   SELECT id
		   FROM context_paths
		   WHERE name = ? OR path = ?
		 ),
		 matched_prefix(id) AS (
		   SELECT id
		   FROM context_paths
		   WHERE name = ?
		      OR name LIKE ? || '/%'
		      OR path = ?
		      OR path LIKE ? || '/%'
		 ),
		 descendants(id) AS (
		   SELECT id FROM matched_exact
		   UNION
		   SELECT c.id
		   FROM contexts c
		   JOIN descendants d ON c.parent_id = d.id
		 )
		 SELECT DISTINCT id
		 FROM (
		   SELECT id FROM matched_prefix
		   UNION
		   SELECT id FROM descendants
		 )
		 ORDER BY id ASC`,
		cleanQuery,
		cleanQuery,
		cleanQuery,
		cleanQuery,
		cleanQuery,
		cleanQuery,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var contextID int64
		if err := rows.Scan(&contextID); err != nil {
			return nil, err
		}
		out = append(out, contextID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) workspaceHasAnyContext(workspaceID int64, contextIDs []int64) (bool, error) {
	if workspaceID <= 0 {
		return false, nil
	}
	clause, args := contextLinkExistsClause("context_workspaces", "workspace_id", "?", contextIDs)
	var matched int
	rowArgs := append([]any{workspaceID}, args...)
	if err := s.db.QueryRow(`SELECT CASE WHEN `+clause+` THEN 1 ELSE 0 END`, rowArgs...).Scan(&matched); err != nil {
		return false, err
	}
	return matched != 0, nil
}

func (s *Store) entityHasAnyContext(linkTable, entityColumn string, entityID int64, contextIDs []int64) (bool, error) {
	if entityID <= 0 {
		return false, nil
	}
	clause, args := contextLinkExistsClause(linkTable, entityColumn, "?", contextIDs)
	var matched int
	rowArgs := append([]any{entityID}, args...)
	if err := s.db.QueryRow(`SELECT CASE WHEN `+clause+` THEN 1 ELSE 0 END`, rowArgs...).Scan(&matched); err != nil {
		return false, err
	}
	return matched != 0, nil
}

func (s *Store) ListWorkspacesByContextPrefix(prefix string) ([]Workspace, error) {
	cleanPrefix := normalizeOptionalContextQuery(prefix)
	if cleanPrefix == "" {
		return nil, errors.New("context is required")
	}
	terms := splitContextQueryTerms(cleanPrefix)
	clauses := make([]string, 0, len(terms))
	args := []any{}
	for _, term := range terms {
		contextIDs, err := s.resolveContextQueryIDs(term)
		if err != nil {
			return nil, err
		}
		if len(contextIDs) == 0 {
			return []Workspace{}, nil
		}
		clause, clauseArgs := contextLinkExistsClause("context_workspaces", "workspace_id", "workspaces.id", contextIDs)
		clauses = append(clauses, clause)
		args = append(args, clauseArgs...)
	}
	rows, err := s.db.Query(
		`SELECT id, name, dir_path, `+scopedContextSelect("context_workspaces", "workspace_id", "workspaces.id")+` AS sphere, is_active, is_daily, daily_date, mcp_url, canvas_session_id, chat_model, chat_model_reasoning_effort, companion_config_json, created_at, updated_at
		 FROM workspaces
		 WHERE `+strings.Join(clauses, ` AND `),
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Workspace
	for rows.Next() {
		workspace, err := scanWorkspace(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, workspace)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsActive != out[j].IsActive {
			return out[i].IsActive
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

func (s *Store) ListItemsByContextPrefix(prefix string) ([]Item, error) {
	cleanPrefix := normalizeOptionalContextQuery(prefix)
	if cleanPrefix == "" {
		return nil, errors.New("context is required")
	}
	return s.ListItemsFiltered(ItemListFilter{Label: cleanPrefix})
}

func (s *Store) filterArtifactsByContextIDs(artifacts []Artifact, contextIDs []int64) ([]Artifact, error) {
	if len(contextIDs) == 0 {
		return []Artifact{}, nil
	}
	out := make([]Artifact, 0, len(artifacts))
	for _, artifact := range artifacts {
		matched, err := s.entityHasAnyContext("context_artifacts", "artifact_id", artifact.ID, contextIDs)
		if err != nil {
			return nil, err
		}
		if matched {
			out = append(out, artifact)
			continue
		}
		homeWorkspaceID, err := s.InferWorkspaceForArtifact(artifact)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		if homeWorkspaceID != nil {
			matched, err = s.workspaceHasAnyContext(*homeWorkspaceID, contextIDs)
			if err != nil {
				return nil, err
			}
			if matched {
				out = append(out, artifact)
				continue
			}
		}
		workspaces, err := s.ListArtifactLinkWorkspaces(artifact.ID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		for _, workspace := range workspaces {
			matched, err = s.workspaceHasAnyContext(workspace.ID, contextIDs)
			if err != nil {
				return nil, err
			}
			if matched {
				out = append(out, artifact)
				break
			}
		}
	}
	sortArtifactsNewestFirst(out)
	return out, nil
}

func (s *Store) ListArtifactsByContextPrefix(prefix string) ([]Artifact, error) {
	cleanPrefix := normalizeOptionalContextQuery(prefix)
	if cleanPrefix == "" {
		return nil, errors.New("context is required")
	}
	artifacts, err := s.ListArtifacts()
	if err != nil {
		return nil, err
	}
	terms := splitContextQueryTerms(cleanPrefix)
	filtered := artifacts
	for _, term := range terms {
		contextIDs, err := s.resolveContextQueryIDs(term)
		if err != nil {
			return nil, err
		}
		filtered, err = s.filterArtifactsByContextIDs(filtered, contextIDs)
		if err != nil {
			return nil, err
		}
		if len(filtered) == 0 {
			return []Artifact{}, nil
		}
	}
	return filtered, nil
}
