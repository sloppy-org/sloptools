package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"sort"
	"strings"
)

func (s *Store) FindWorkspaceContainingPath(filePath string) (*int64, error) {
	targetPath := normalizeWorkspacePath(filePath)
	if targetPath == "" {
		return nil, nil
	}
	workspaces, err := s.ListWorkspaces()
	if err != nil {
		return nil, err
	}
	var best *Workspace
	for i := range workspaces {
		rel, err := filepath.Rel(workspaces[i].DirPath, targetPath)
		if err != nil {
			continue
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		if best == nil || len(workspaces[i].DirPath) > len(best.DirPath) {
			best = &workspaces[i]
		}
	}
	if best == nil {
		return nil, nil
	}
	return &best.ID, nil
}

func (s *Store) FindWorkspaceByGitRemote(ownerRepo string) (*int64, error) {
	target := normalizeGitHubOwnerRepo(ownerRepo)
	if target == "" {
		return nil, nil
	}
	workspaces, err := s.ListWorkspaces()
	if err != nil {
		return nil, err
	}
	var matches []int64
	for _, workspace := range workspaces {
		repo, err := workspaceGitRemoteOwnerRepo(workspace.DirPath)
		if err != nil {
			return nil, err
		}
		if repo == target {
			matches = append(matches, workspace.ID)
		}
	}
	if len(matches) != 1 {
		return nil, nil
	}
	return &matches[0], nil
}

func (s *Store) GitHubRepoForWorkspace(id int64) (string, error) {
	workspace, err := s.GetWorkspace(id)
	if err != nil {
		return "", err
	}
	return workspaceGitRemoteOwnerRepo(workspace.DirPath)
}

func (s *Store) InferWorkspaceForArtifact(artifact Artifact) (*int64, error) {
	switch artifact.Kind {
	case ArtifactKindDocument, ArtifactKindMarkdown, ArtifactKindPDF:
		if artifact.RefPath == nil {
			return nil, nil
		}
		return s.FindWorkspaceContainingPath(*artifact.RefPath)
	case ArtifactKindGitHubIssue, ArtifactKindGitHubPR:
		var ownerRepo string
		if artifact.RefURL != nil {
			ownerRepo = githubOwnerRepoFromURL(*artifact.RefURL)
		}
		if ownerRepo == "" && artifact.MetaJSON != nil {
			ownerRepo = githubOwnerRepoFromMeta(*artifact.MetaJSON)
		}
		if ownerRepo == "" {
			return nil, nil
		}
		return s.FindWorkspaceByGitRemote(ownerRepo)
	default:
		return nil, nil
	}
}

func (s *Store) SetActiveWorkspace(id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE workspaces SET is_active = 0, updated_at = datetime('now') WHERE is_active <> 0`); err != nil {
		return err
	}
	res, err := tx.Exec(`UPDATE workspaces
		SET is_active = 1,
		    canvas_session_id = CASE
		      WHEN trim(canvas_session_id) = '' THEN 'local'
		      ELSE canvas_session_id
		    END,
		    updated_at = datetime('now')
		WHERE id = ?`, id)
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
	return tx.Commit()
}

func (s *Store) UpdateWorkspaceName(id int64, name string) (Workspace, error) {
	cleanName := normalizeWorkspaceName(name)
	if cleanName == "" {
		return Workspace{}, errors.New("workspace name is required")
	}
	res, err := s.db.Exec(`UPDATE workspaces
		SET name = ?, is_daily = 0, updated_at = datetime('now')
		WHERE id = ?`, cleanName, id)
	if err != nil {
		return Workspace{}, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return Workspace{}, err
	}
	if affected == 0 {
		return Workspace{}, sql.ErrNoRows
	}
	return s.GetWorkspace(id)
}

func (s *Store) UpdateWorkspaceLocation(id int64, name, dirPath string) (Workspace, error) {
	cleanName := normalizeWorkspaceName(name)
	if cleanName == "" {
		return Workspace{}, errors.New("workspace name is required")
	}
	cleanPath := normalizeWorkspacePath(dirPath)
	if cleanPath == "" {
		return Workspace{}, errors.New("workspace path is required")
	}
	res, err := s.db.Exec(`UPDATE workspaces
		 SET name = ?, dir_path = ?, is_daily = 0, updated_at = datetime('now')
		 WHERE id = ?`, cleanName, cleanPath, id)
	if err != nil {
		return Workspace{}, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return Workspace{}, err
	}
	if affected == 0 {
		return Workspace{}, sql.ErrNoRows
	}
	return s.GetWorkspace(id)
}

func (s *Store) SetWorkspaceSphere(id int64, sphere string) (Workspace, error) {
	cleanSphere := normalizeRequiredSphere(sphere)
	if cleanSphere == "" {
		return Workspace{}, errors.New("workspace sphere must be work or private")
	}
	res, err := s.db.Exec(`UPDATE workspaces SET updated_at = datetime('now') WHERE id = ?`, id)
	if err != nil {
		return Workspace{}, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return Workspace{}, err
	}
	if affected == 0 {
		return Workspace{}, sql.ErrNoRows
	}
	if err := s.syncScopedContextLink("context_workspaces", "workspace_id", id, cleanSphere); err != nil {
		return Workspace{}, err
	}
	rows, err := s.db.Query(`SELECT id FROM items WHERE workspace_id = ?`, id)
	if err != nil {
		return Workspace{}, err
	}
	itemIDs := []int64{}
	for rows.Next() {
		var itemID int64
		if err := rows.Scan(&itemID); err != nil {
			_ = rows.Close()
			return Workspace{}, err
		}
		itemIDs = append(itemIDs, itemID)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return Workspace{}, err
	}
	if err := rows.Close(); err != nil {
		return Workspace{}, err
	}
	for _, itemID := range itemIDs {
		if err := s.syncScopedContextLink("context_items", "item_id", itemID, cleanSphere); err != nil {
			return Workspace{}, err
		}
	}
	return s.GetWorkspace(id)
}

func (s *Store) DeleteWorkspace(id int64) error {
	res, err := s.db.Exec(`DELETE FROM workspaces WHERE id = ?`, id)
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

func (s *Store) CreateArtifact(kind ArtifactKind, refPath, refURL, title, metaJSON *string) (Artifact, error) {
	cleanKind := normalizeArtifactKind(kind)
	if cleanKind == "" {
		return Artifact{}, errors.New("artifact kind is required")
	}
	res, err := s.db.Exec(`INSERT INTO artifacts (kind, ref_path, ref_url, title, meta_json)
		 VALUES (?, ?, ?, ?, ?)`, cleanKind, normalizeOptionalString(refPath), normalizeOptionalString(refURL), normalizeOptionalString(title), normalizeOptionalString(metaJSON))
	if err != nil {
		return Artifact{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Artifact{}, err
	}
	return s.GetArtifact(id)
}

func (s *Store) GetArtifact(id int64) (Artifact, error) {
	return scanArtifact(s.db.QueryRow(`SELECT id, kind, ref_path, ref_url, title, meta_json, created_at, updated_at
		 FROM artifacts
		 WHERE id = ?`, id))
}

func sortArtifactsNewestFirst(artifacts []Artifact) {
	sort.Slice(artifacts, func(i, j int) bool {
		if artifacts[i].UpdatedAt == artifacts[j].UpdatedAt {
			return artifacts[i].ID < artifacts[j].ID
		}
		return artifacts[i].UpdatedAt > artifacts[j].UpdatedAt
	})
}

func (s *Store) ListArtifactsByKind(kind ArtifactKind) ([]Artifact, error) {
	cleanKind := normalizeArtifactKind(kind)
	if cleanKind == "" {
		return nil, errors.New("artifact kind is required")
	}
	rows, err := s.db.Query(`SELECT id, kind, ref_path, ref_url, title, meta_json, created_at, updated_at
		 FROM artifacts
		 WHERE kind = ?`, cleanKind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Artifact
	for rows.Next() {
		artifact, err := scanArtifact(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, artifact)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sortArtifactsNewestFirst(out)
	return out, nil
}

func (s *Store) ListArtifacts() ([]Artifact, error) {
	rows, err := s.db.Query(`SELECT id, kind, ref_path, ref_url, title, meta_json, created_at, updated_at
		 FROM artifacts`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Artifact
	for rows.Next() {
		artifact, err := scanArtifact(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, artifact)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sortArtifactsNewestFirst(out)
	return out, nil
}

func (s *Store) LinkArtifactToWorkspace(workspaceID, artifactID int64) error {
	if _, err := s.GetWorkspace(workspaceID); err != nil {
		return err
	}
	artifact, err := s.GetArtifact(artifactID)
	if err != nil {
		return err
	}
	if homeWorkspaceID, err := s.InferWorkspaceForArtifact(artifact); err != nil {
		return err
	} else if homeWorkspaceID != nil && *homeWorkspaceID == workspaceID {
		return errors.New("artifact already belongs to workspace")
	}
	_, err = s.db.Exec(`INSERT INTO workspace_artifact_links (workspace_id, artifact_id)
		 VALUES (?, ?)
		 ON CONFLICT(workspace_id, artifact_id) DO NOTHING`, workspaceID, artifactID)
	return err
}

func (s *Store) ListArtifactWorkspaceLinks(workspaceID int64) ([]ArtifactWorkspaceLink, error) {
	if _, err := s.GetWorkspace(workspaceID); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT workspace_id, artifact_id, created_at
		 FROM workspace_artifact_links
		 WHERE workspace_id = ?
		 ORDER BY created_at DESC, artifact_id ASC`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ArtifactWorkspaceLink{}
	for rows.Next() {
		var link ArtifactWorkspaceLink
		if err := rows.Scan(&link.WorkspaceID, &link.ArtifactID, &link.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, link)
	}
	return out, rows.Err()
}

func (s *Store) ListArtifactLinkWorkspaces(artifactID int64) ([]Workspace, error) {
	if _, err := s.GetArtifact(artifactID); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT w.id, w.name, w.dir_path, `+scopedContextSelect("context_workspaces", "workspace_id", "w.id")+` AS sphere, w.is_active, w.is_daily, w.daily_date, w.mcp_url, w.canvas_session_id, w.chat_model, w.chat_model_reasoning_effort, w.companion_config_json, w.created_at, w.updated_at
		 FROM workspace_artifact_links wal
		 INNER JOIN workspaces w ON w.id = wal.workspace_id
		 WHERE wal.artifact_id = ?
		 ORDER BY datetime(wal.created_at) DESC, w.id ASC`, artifactID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Workspace{}
	for rows.Next() {
		workspace, err := scanWorkspace(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, workspace)
	}
	return out, rows.Err()
}

func (s *Store) ListLinkedArtifacts(workspaceID int64) ([]Artifact, error) {
	links, err := s.ListArtifactWorkspaceLinks(workspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]Artifact, 0, len(links))
	for _, link := range links {
		artifact, err := s.GetArtifact(link.ArtifactID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			return nil, err
		}
		out = append(out, artifact)
	}
	sortArtifactsNewestFirst(out)
	return out, nil
}

func (s *Store) ListArtifactsForWorkspace(workspaceID int64) ([]Artifact, error) {
	if _, err := s.GetWorkspace(workspaceID); err != nil {
		return nil, err
	}
	artifacts, err := s.ListArtifacts()
	if err != nil {
		return nil, err
	}
	out := make([]Artifact, 0, len(artifacts))
	seen := map[int64]struct{}{}
	for _, artifact := range artifacts {
		homeWorkspaceID, err := s.InferWorkspaceForArtifact(artifact)
		if err != nil {
			return nil, err
		}
		if homeWorkspaceID == nil || *homeWorkspaceID != workspaceID {
			continue
		}
		out = append(out, artifact)
		seen[artifact.ID] = struct{}{}
	}
	linked, err := s.ListLinkedArtifacts(workspaceID)
	if err != nil {
		return nil, err
	}
	for _, artifact := range linked {
		if _, ok := seen[artifact.ID]; ok {
			continue
		}
		out = append(out, artifact)
	}
	sortArtifactsNewestFirst(out)
	return out, nil
}

func (s *Store) UpdateArtifact(id int64, updates ArtifactUpdate) error {
	parts := []string{}
	args := []any{}
	if updates.Kind != nil {
		cleanKind := normalizeArtifactKind(*updates.Kind)
		if cleanKind == "" {
			return errors.New("artifact kind is required")
		}
		parts = append(parts, "kind = ?")
		args = append(args, cleanKind)
	}
	if updates.RefPath != nil {
		parts = append(parts, "ref_path = ?")
		args = append(args, normalizeOptionalString(updates.RefPath))
	}
	if updates.RefURL != nil {
		parts = append(parts, "ref_url = ?")
		args = append(args, normalizeOptionalString(updates.RefURL))
	}
	if updates.Title != nil {
		parts = append(parts, "title = ?")
		args = append(args, normalizeOptionalString(updates.Title))
	}
	if updates.MetaJSON != nil {
		parts = append(parts, "meta_json = ?")
		args = append(args, normalizeOptionalString(updates.MetaJSON))
	}
	if len(parts) == 0 {
		_, err := s.GetArtifact(id)
		return err
	}
	parts = append(parts, "updated_at = datetime('now')")
	args = append(args, id)
	res, err := s.db.Exec(`UPDATE artifacts SET `+stringsJoin(parts, ", ")+` WHERE id = ?`, args...)
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

func (s *Store) DeleteArtifact(id int64) error {
	res, err := s.db.Exec(`DELETE FROM artifacts WHERE id = ?`, id)
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
