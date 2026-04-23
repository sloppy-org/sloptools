package store

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Project is a temporary type alias kept so callers outside this package
// continue to compile while they migrate to Workspace.

const (
	appStateActiveWorkspaceIDKey = "active_workspace_id"
	// DB-stored key prefixes are intentionally kept unchanged for backward
	// compatibility with existing databases.
	workspaceNameStatePrefix     = "project_name:"
	workspacePathStatePrefix     = "project_workspace_path:"
	workspaceRootPathStatePrefix = "project_root_path:"
	workspaceKindStatePrefix     = "project_kind:"
)

func workspaceIDString(id int64) string {
	return strconv.FormatInt(id, 10)
}

func parseWorkspaceIDString(id string) (int64, error) {
	workspaceID, err := strconv.ParseInt(strings.TrimSpace(id), 10, 64)
	if err != nil || workspaceID <= 0 {
		return 0, sql.ErrNoRows
	}
	return workspaceID, nil
}

func parseWorkspaceTimestamp(value string) int64 {
	if strings.TrimSpace(value) == "" {
		return 0
	}
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed.Unix()
	}
	if parsed, err := time.Parse("2006-01-02 15:04:05", value); err == nil {
		return parsed.Unix()
	}
	return 0
}

func workspaceKindStateKey(id int64) string {
	return workspaceKindStatePrefix + strconv.FormatInt(id, 10)
}

func workspaceNameStateKey(id int64) string {
	return workspaceNameStatePrefix + strconv.FormatInt(id, 10)
}

func workspacePathStateKey(id int64) string {
	return workspacePathStatePrefix + strconv.FormatInt(id, 10)
}

func workspaceRootPathStateKey(id int64) string {
	return workspaceRootPathStatePrefix + strconv.FormatInt(id, 10)
}

func (s *Store) activeWorkspaceIDFromState() (string, error) {
	return s.AppState(appStateActiveWorkspaceIDKey)
}

func (s *Store) compatibilityWorkspacePath(workspaceID int64, defaultPath string) string {
	if workspaceID <= 0 {
		return normalizeWorkspacePath(defaultPath)
	}
	if path, err := s.AppState(workspacePathStateKey(workspaceID)); err == nil && strings.TrimSpace(path) != "" {
		return strings.TrimSpace(path)
	}
	return normalizeWorkspacePath(defaultPath)
}

// enrichWorkspace populates the Kind, RootPath, IsDefault fields from
// app_state keys. It is called after every workspace scan/fetch.
func (s *Store) enrichWorkspace(workspace *Workspace) {
	workspace.Kind = s.workspaceKind(workspace.ID)
	workspace.RootPath = s.workspaceRootPath(workspace.ID, workspace.DirPath)
	workspace.WorkspacePath = s.workspaceStoredPath(workspace.ID, workspace.DirPath)
	workspace.Name = s.workspaceOverrideName(workspace.ID, workspace.Name)
	activeID, err := s.activeWorkspaceIDFromState()
	if err == nil {
		workspace.IsDefault = strings.TrimSpace(activeID) == workspaceIDString(workspace.ID) ||
			strings.EqualFold(strings.TrimSpace(workspace.CanvasSessionID), "local")
	}
}

func (s *Store) workspaceKind(id int64) string {
	if kind, err := s.AppState(workspaceKindStateKey(id)); err == nil {
		switch clean := strings.ToLower(strings.TrimSpace(kind)); clean {
		case "managed", "linked", "meeting", "task":
			return clean
		}
	}
	return "workspace"
}

func (s *Store) workspaceOverrideName(id int64, fallback string) string {
	if name, err := s.AppState(workspaceNameStateKey(id)); err == nil && strings.TrimSpace(name) != "" {
		return strings.TrimSpace(name)
	}
	return fallback
}

func (s *Store) workspaceStoredPath(id int64, fallback string) string {
	if path, err := s.AppState(workspacePathStateKey(id)); err == nil && strings.TrimSpace(path) != "" {
		return strings.TrimSpace(path)
	}
	return fallback
}

func (s *Store) workspaceRootPath(id int64, fallback string) string {
	if path, err := s.AppState(workspaceRootPathStateKey(id)); err == nil && strings.TrimSpace(path) != "" {
		return normalizeWorkspacePath(path)
	}
	return fallback
}

// ListEnrichedWorkspaces returns all workspaces enriched with app_state metadata.
func (s *Store) ListEnrichedWorkspaces() ([]Workspace, error) {
	workspaces, err := s.ListWorkspaces()
	if err != nil {
		return nil, err
	}
	for i := range workspaces {
		s.enrichWorkspace(&workspaces[i])
	}
	return workspaces, nil
}

// GetProject returns a workspace by string ID, enriched with app_state
// metadata. This is a compatibility shim; callers should migrate to
// GetWorkspace with int64 IDs.
func (s *Store) GetEnrichedWorkspace(id string) (Workspace, error) {
	workspaceID, err := parseWorkspaceIDString(id)
	if err != nil {
		return Workspace{}, err
	}
	workspace, err := s.GetWorkspace(workspaceID)
	if err != nil {
		return Workspace{}, err
	}
	s.enrichWorkspace(&workspace)
	return workspace, nil
}

func (s *Store) GetWorkspaceByStoredPath(workspacePath string) (Workspace, error) {
	rawPath := strings.TrimSpace(workspacePath)
	cleanPath := normalizeWorkspacePath(workspacePath)
	workspace, err := s.GetWorkspaceByPath(cleanPath)
	if err == nil {
		s.enrichWorkspace(&workspace)
		return workspace, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Workspace{}, err
	}
	workspaces, err := s.ListWorkspaces()
	if err != nil {
		return Workspace{}, err
	}
	for i := range workspaces {
		if workspaces[i].IsDaily {
			continue
		}
		storedPath := strings.TrimSpace(s.workspaceStoredPath(workspaces[i].ID, workspaces[i].DirPath))
		switch {
		case storedPath != "" && storedPath == rawPath:
			s.enrichWorkspace(&workspaces[i])
			return workspaces[i], nil
		case filepath.IsAbs(storedPath) && storedPath == cleanPath:
			s.enrichWorkspace(&workspaces[i])
			return workspaces[i], nil
		}
	}
	return Workspace{}, sql.ErrNoRows
}

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
