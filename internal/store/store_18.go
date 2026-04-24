package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

func (s *Store) GetWorkspaceByRootPath(rootPath string) (Workspace, error) {
	cleanPath := normalizeWorkspacePath(rootPath)
	workspaces, err := s.ListWorkspaces()
	if err != nil {
		return Workspace{}, err
	}
	for i := range workspaces {
		if workspaces[i].IsDaily {
			continue
		}
		if strings.TrimSpace(s.workspaceRootPath(workspaces[i].ID, workspaces[i].DirPath)) == cleanPath {
			s.enrichWorkspace(&workspaces[i])
			return workspaces[i], nil
		}
	}
	return s.GetWorkspaceByStoredPath(cleanPath)
}

func (s *Store) GetWorkspaceByCanvasSession(canvasSessionID string) (Workspace, error) {
	workspaces, err := s.ListWorkspaces()
	if err != nil {
		return Workspace{}, err
	}
	clean := strings.TrimSpace(canvasSessionID)
	for i := range workspaces {
		if strings.TrimSpace(workspaces[i].CanvasSessionID) == clean {
			s.enrichWorkspace(&workspaces[i])
			return workspaces[i], nil
		}
	}
	return Workspace{}, sql.ErrNoRows
}

func (s *Store) CreateEnrichedWorkspace(name, workspacePath, rootPath, kind, mcpURL, canvasSessionID string, isDefault bool) (Workspace, error) {
	sphere := SpherePrivate
	cleanRootPath := normalizeWorkspacePath(rootPath)
	cleanWorkspacePath := strings.TrimSpace(workspacePath)
	if cleanWorkspacePath == "" {
		cleanWorkspacePath = cleanRootPath
	}
	targetPath := cleanRootPath
	if targetPath == "" {
		targetPath = normalizeWorkspacePath(cleanWorkspacePath)
	}
	if targetPath != "" {
		if workspace, err := s.GetWorkspaceByPath(targetPath); err == nil && strings.TrimSpace(workspace.Sphere) != "" {
			sphere = workspace.Sphere
		} else if !errors.Is(err, sql.ErrNoRows) && err != nil {
			return Workspace{}, err
		} else if workspaceID, findErr := s.FindWorkspaceContainingPath(targetPath); findErr == nil && workspaceID != nil {
			workspace, getErr := s.GetWorkspace(*workspaceID)
			if getErr != nil {
				return Workspace{}, getErr
			}
			if strings.TrimSpace(workspace.Sphere) != "" {
				sphere = workspace.Sphere
			}
		} else if findErr != nil {
			return Workspace{}, findErr
		}
	}
	if sphere == SpherePrivate {
		if activeSphere, err := s.ActiveSphere(); err == nil && strings.TrimSpace(activeSphere) != "" {
			sphere = activeSphere
		}
	}
	workspace, err := s.CreateWorkspace(name, cleanRootPath, sphere)
	if err != nil {
		return Workspace{}, err
	}
	if err := s.SetAppState(workspaceNameStateKey(workspace.ID), strings.TrimSpace(name)); err != nil {
		return Workspace{}, err
	}
	if err := s.SetAppState(workspacePathStateKey(workspace.ID), cleanWorkspacePath); err != nil {
		return Workspace{}, err
	}
	if err := s.SetAppState(workspaceRootPathStateKey(workspace.ID), cleanRootPath); err != nil {
		return Workspace{}, err
	}
	if cleanKind := strings.ToLower(strings.TrimSpace(kind)); cleanKind != "" {
		if err := s.SetAppState(workspaceKindStateKey(workspace.ID), cleanKind); err != nil {
			return Workspace{}, err
		}
	}
	if strings.TrimSpace(mcpURL) != "" {
		if updated, updateErr := s.UpdateWorkspaceMCPURL(workspace.ID, mcpURL); updateErr == nil {
			workspace = updated
		} else {
			return Workspace{}, updateErr
		}
	}
	if strings.TrimSpace(canvasSessionID) != "" {
		if updated, updateErr := s.UpdateWorkspaceCanvasSession(workspace.ID, canvasSessionID); updateErr == nil {
			workspace = updated
		} else {
			return Workspace{}, updateErr
		}
	}
	if isDefault {
		if err := s.SetAppState(appStateActiveWorkspaceIDKey, workspaceIDString(workspace.ID)); err != nil {
			return Workspace{}, err
		}
		if err := s.SetActiveWorkspace(workspace.ID); err != nil {
			return Workspace{}, err
		}
		workspace, err = s.GetWorkspace(workspace.ID)
		if err != nil {
			return Workspace{}, err
		}
	}
	s.enrichWorkspace(&workspace)
	return workspace, nil
}

func (s *Store) UpdateWorkspaceMCPURL(id int64, mcpURL string) (Workspace, error) {
	_, err := s.db.Exec(`UPDATE workspaces SET mcp_url = ?, updated_at = datetime('now') WHERE id = ?`, strings.TrimSpace(mcpURL), id)
	if err != nil {
		return Workspace{}, err
	}
	return s.GetWorkspace(id)
}

func (s *Store) UpdateWorkspaceCanvasSession(id int64, canvasSessionID string) (Workspace, error) {
	_, err := s.db.Exec(`UPDATE workspaces SET canvas_session_id = ?, updated_at = datetime('now') WHERE id = ?`, strings.TrimSpace(canvasSessionID), id)
	if err != nil {
		return Workspace{}, err
	}
	return s.GetWorkspace(id)
}

func (s *Store) SetActiveWorkspaceID(workspaceID string) error {
	workspaceNumericID, err := parseWorkspaceIDString(workspaceID)
	if err != nil {
		return errors.New("workspace id is required")
	}
	if _, err := s.GetWorkspace(workspaceNumericID); err != nil {
		return err
	}
	return s.SetAppState(appStateActiveWorkspaceIDKey, workspaceIDString(workspaceNumericID))
}

func (s *Store) ActiveWorkspaceID() (string, error) {
	if activeProjectID, err := s.activeWorkspaceIDFromState(); err == nil && strings.TrimSpace(activeProjectID) != "" {
		return strings.TrimSpace(activeProjectID), nil
	} else if err != nil {
		return "", err
	}
	workspace, err := s.ActiveWorkspace()
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	if workspace.IsDaily {
		return "", nil
	}
	return workspaceIDString(workspace.ID), nil
}

func (s *Store) TouchWorkspace(id string) error {
	workspaceID, err := parseWorkspaceIDString(id)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE workspaces SET updated_at = datetime('now') WHERE id = ?`, workspaceID)
	return err
}

func (s *Store) UpdateWorkspaceTransport(id, mcpURL, canvasSessionID string) error {
	workspaceID, err := parseWorkspaceIDString(id)
	if err != nil {
		return err
	}
	if _, err := s.UpdateWorkspaceMCPURL(workspaceID, mcpURL); err != nil {
		return err
	}
	_, err = s.UpdateWorkspaceCanvasSession(workspaceID, canvasSessionID)
	return err
}

func (s *Store) UpdateWorkspaceRuntime(id, mcpURL, canvasSessionID string) error {
	return s.UpdateWorkspaceTransport(id, mcpURL, canvasSessionID)
}

func (s *Store) UpdateEnrichedWorkspaceChatModel(id, model string) error {
	workspaceID, err := parseWorkspaceIDString(id)
	if err != nil {
		return err
	}
	return s.UpdateWorkspaceChatModel(workspaceID, model)
}

func (s *Store) UpdateEnrichedWorkspaceChatModelReasoningEffort(id, effort string) error {
	workspaceID, err := parseWorkspaceIDString(id)
	if err != nil {
		return err
	}
	return s.UpdateWorkspaceChatModelReasoningEffort(workspaceID, effort)
}

func (s *Store) UpdateWorkspaceKind(id, kind string) error {
	workspaceID, err := parseWorkspaceIDString(id)
	if err != nil {
		return err
	}
	return s.SetAppState(workspaceKindStateKey(workspaceID), strings.ToLower(strings.TrimSpace(kind)))
}

func (s *Store) RenameWorkspace(id, name, workspacePath, rootPath, kind string) error {
	workspaceID, err := parseWorkspaceIDString(id)
	if err != nil {
		return err
	}
	if cleanName := strings.TrimSpace(name); cleanName != "" {
		if err := s.SetAppState(workspaceNameStateKey(workspaceID), cleanName); err != nil {
			return err
		}
	}
	if cleanWorkspacePath := strings.TrimSpace(workspacePath); cleanWorkspacePath != "" {
		if err := s.SetAppState(workspacePathStateKey(workspaceID), cleanWorkspacePath); err != nil {
			return err
		}
	}
	if cleanRootPath := normalizeWorkspacePath(rootPath); cleanRootPath != "" {
		if err := s.SetAppState(workspaceRootPathStateKey(workspaceID), cleanRootPath); err != nil {
			return err
		}
	}
	if cleanKind := strings.ToLower(strings.TrimSpace(kind)); cleanKind != "" {
		if err := s.SetAppState(workspaceKindStateKey(workspaceID), cleanKind); err != nil {
			return err
		}
	}
	if cleanRootPath := normalizeWorkspacePath(rootPath); cleanRootPath != "" {
		_, err = s.UpdateWorkspaceLocation(workspaceID, name, cleanRootPath)
		return err
	}
	_, err = s.UpdateWorkspaceName(workspaceID, name)
	return err
}

func (s *Store) UpdateWorkspaceLocation2(id, name, workspacePath, rootPath, kind string) error {
	return s.RenameWorkspace(id, name, workspacePath, rootPath, kind)
}

func (s *Store) DeleteEnrichedWorkspace(workspaceID string) error {
	workspaceNumericID, err := parseWorkspaceIDString(workspaceID)
	if err != nil {
		return err
	}
	if activeProjectID, err := s.activeWorkspaceIDFromState(); err == nil && strings.TrimSpace(activeProjectID) == workspaceIDString(workspaceNumericID) {
		if err := s.SetAppState(appStateActiveWorkspaceIDKey, ""); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if err := s.SetAppState(workspaceKindStateKey(workspaceNumericID), ""); err != nil {
		return err
	}
	if err := s.SetAppState(workspaceNameStateKey(workspaceNumericID), ""); err != nil {
		return err
	}
	if err := s.SetAppState(workspacePathStateKey(workspaceNumericID), ""); err != nil {
		return err
	}
	if err := s.SetAppState(workspaceRootPathStateKey(workspaceNumericID), ""); err != nil {
		return err
	}
	return s.DeleteWorkspace(workspaceNumericID)
}

func (s *Store) UpdateEnrichedWorkspaceCompanionConfig(id, configJSON string) error {
	workspaceID, err := parseWorkspaceIDString(id)
	if err != nil {
		return err
	}
	return s.UpdateWorkspaceCompanionConfig(workspaceID, configJSON)
}

func (s *Store) UpdateEnrichedWorkspaceCanvasSession(id, canvasSessionID string) error {
	workspaceID, err := parseWorkspaceIDString(id)
	if err != nil {
		return err
	}
	_, err = s.UpdateWorkspaceCanvasSession(workspaceID, canvasSessionID)
	return err
}

func (s *Store) UpdateEnrichedWorkspaceMCPURL(id, mcpURL string) error {
	workspaceID, err := parseWorkspaceIDString(id)
	if err != nil {
		return err
	}
	_, err = s.UpdateWorkspaceMCPURL(workspaceID, mcpURL)
	return err
}

func (s *Store) ListWorkspacesForID(workspaceID string) ([]Workspace, error) {
	numericID, err := parseWorkspaceIDString(workspaceID)
	if err != nil {
		return nil, err
	}
	workspace, err := s.GetWorkspace(numericID)
	if err != nil {
		return nil, err
	}
	return []Workspace{workspace}, nil
}

func (s *Store) SetWorkspaceNoOp(id int64, _ *string) (Workspace, error) {
	return s.GetWorkspace(id)
}

func (s *Store) FindWorkspaceByPath(path string) (*int64, error) {
	return s.FindWorkspaceContainingPath(path)
}

func (s *Store) activeEnrichedWorkspace() (Workspace, error) {
	id, err := s.ActiveWorkspaceID()
	if err != nil {
		return Workspace{}, err
	}
	return s.GetEnrichedWorkspace(id)
}

func (s *Store) appServerModelProfileForWorkspacePath(workspacePath string) string {
	workspace, err := s.GetWorkspaceByStoredPath(workspacePath)
	if err != nil {
		return ""
	}
	return normalizeWorkspaceChatModel(workspace.ChatModel)
}

func normalizeWorkspaceCompatName(name string) string {
	return normalizeWorkspaceName(name)
}

func normalizeWorkspaceCompatChatModel(raw string) string {
	return normalizeWorkspaceChatModel(raw)
}

func normalizeWorkspaceCompatChatModelReasoningEffort(raw string) string {
	return normalizeWorkspaceChatModelReasoningEffort(raw)
}

func invalidWorkspaceIDError(id string) error {
	return fmt.Errorf("invalid workspace id: %s", strings.TrimSpace(id))
}

const appStateFocusedWorkspaceIDKey = "focused_workspace_id"

func (s *Store) SetFocusedWorkspaceID(id int64) error {
	if id < 0 {
		return errors.New("focused workspace id must be zero or a positive integer")
	}
	if id == 0 {
		return s.SetAppState(appStateFocusedWorkspaceIDKey, "")
	}
	if _, err := s.GetWorkspace(id); err != nil {
		return err
	}
	return s.SetAppState(appStateFocusedWorkspaceIDKey, strconv.FormatInt(id, 10))
}

func (s *Store) FocusedWorkspaceID() (int64, error) {
	raw, err := s.AppState(appStateFocusedWorkspaceIDKey)
	if err != nil {
		return 0, err
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, errors.New("focused workspace id is invalid")
	}
	if id <= 0 {
		return 0, nil
	}
	if _, err := s.GetWorkspace(id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, err
	}
	return id, nil
}

func (s *Store) migrateWorkspaceSphereSupport() error {
	return nil
}

func (s *Store) migrateWorkspaceConfigSupport() error {
	tableColumns, err := s.tableColumnSet("workspaces")
	if err != nil {
		return err
	}
	type columnDef struct {
		name string
		sql  string
	}
	defs := []columnDef{{name: "mcp_url", sql: `ALTER TABLE workspaces ADD COLUMN mcp_url TEXT NOT NULL DEFAULT ''`}, {name: "canvas_session_id", sql: `ALTER TABLE workspaces ADD COLUMN canvas_session_id TEXT NOT NULL DEFAULT ''`}, {name: "chat_model", sql: `ALTER TABLE workspaces ADD COLUMN chat_model TEXT NOT NULL DEFAULT ''`}, {name: "chat_model_reasoning_effort", sql: `ALTER TABLE workspaces ADD COLUMN chat_model_reasoning_effort TEXT NOT NULL DEFAULT ''`}, {name: "companion_config_json", sql: `ALTER TABLE workspaces ADD COLUMN companion_config_json TEXT NOT NULL DEFAULT '{}'`}}
	for _, def := range defs {
		if tableColumns["workspaces"][def.name] {
			continue
		}
		if _, err := s.db.Exec(def.sql); err != nil {
			return err
		}
	}
	_, err = s.db.Exec(`UPDATE workspaces SET companion_config_json = '{}' WHERE trim(companion_config_json) = ''`)
	return err
}

func (s *Store) migrateWorkspaceDailySupport() error {
	tableColumns, err := s.tableColumnSet("workspaces")
	if err != nil {
		return err
	}
	type columnDef struct {
		name string
		sql  string
	}
	defs := []columnDef{{name: "is_daily", sql: `ALTER TABLE workspaces ADD COLUMN is_daily INTEGER NOT NULL DEFAULT 0`}, {name: "daily_date", sql: `ALTER TABLE workspaces ADD COLUMN daily_date TEXT`}}
	for _, def := range defs {
		if tableColumns["workspaces"][def.name] {
			continue
		}
		if _, err := s.db.Exec(def.sql); err != nil {
			return err
		}
	}
	_, err = s.db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_workspaces_daily_date ON workspaces(daily_date) WHERE daily_date IS NOT NULL AND is_daily <> 0`)
	return err
}

func (s *Store) UpdateWorkspaceChatModel(id int64, chatModel string) error {
	if id <= 0 {
		return errors.New("workspace id is required")
	}
	res, err := s.db.Exec(`UPDATE workspaces SET chat_model = ?, updated_at = datetime('now') WHERE id = ?`, normalizeWorkspaceChatModel(chatModel), id)
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

func (s *Store) UpdateWorkspaceChatModelReasoningEffort(id int64, effort string) error {
	if id <= 0 {
		return errors.New("workspace id is required")
	}
	res, err := s.db.Exec(`UPDATE workspaces SET chat_model_reasoning_effort = ?, updated_at = datetime('now') WHERE id = ?`, normalizeWorkspaceChatModelReasoningEffort(effort), id)
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

func (s *Store) GetWorkspaceWatch(workspaceID int64) (WorkspaceWatch, error) {
	if workspaceID <= 0 {
		return WorkspaceWatch{}, errors.New("workspace_id must be positive")
	}
	return scanWorkspaceWatch(s.db.QueryRow(`SELECT workspace_id, config_json, poll_interval_seconds, enabled, current_batch_id, created_at, updated_at
		 FROM workspace_watches
		 WHERE workspace_id = ?`, workspaceID))
}
