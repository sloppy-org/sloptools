package store

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

func normalizeTimeEntryFilter(filter TimeEntryListFilter) (TimeEntryListFilter, error) {
	normalized := TimeEntryListFilter{ActiveOnly: filter.ActiveOnly}
	sphere, err := normalizeOptionalSphereFilter(filter.Sphere)
	if err != nil {
		return TimeEntryListFilter{}, err
	}
	normalized.Sphere = sphere
	if filter.From != nil {
		from := filter.From.UTC()
		normalized.From = &from
	}
	if filter.To != nil {
		to := filter.To.UTC()
		normalized.To = &to
	}
	if normalized.From != nil && normalized.To != nil && !normalized.To.After(*normalized.From) {
		return TimeEntryListFilter{}, errors.New("time range end must be after start")
	}
	return normalized, nil
}

func timeEntryContextMatches(entry *TimeEntry, workspaceID *int64, sphere string) bool {
	if entry == nil {
		return false
	}
	if normalizeSphere(entry.Sphere) != normalizeSphere(sphere) {
		return false
	}
	switch {
	case entry.WorkspaceID == nil && workspaceID != nil:
		return false
	case entry.WorkspaceID != nil && workspaceID == nil:
		return false
	case entry.WorkspaceID != nil && workspaceID != nil && *entry.WorkspaceID != *workspaceID:
		return false
	}
	return true
}

func (s *Store) validateTimeEntryContext(workspaceID *int64, sphere string) error {
	if normalizeRequiredSphere(sphere) == "" {
		return errors.New("sphere must be work or private")
	}
	if workspaceID != nil {
		if *workspaceID <= 0 {
			return errors.New("workspace_id must be a positive integer")
		}
		if _, err := s.GetWorkspace(*workspaceID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ActiveWorkspace() (Workspace, error) {
	return scanWorkspace(s.db.QueryRow(`SELECT id, name, dir_path, ` + scopedContextSelect("context_workspaces", "workspace_id", "workspaces.id") + ` AS sphere, is_active, is_daily, daily_date, mcp_url, canvas_session_id, chat_model, chat_model_reasoning_effort, companion_config_json, created_at, updated_at
		 FROM workspaces
		 WHERE is_active <> 0
		 ORDER BY updated_at DESC, id DESC
		 LIMIT 1`))
}

func (s *Store) ActiveTimeEntry() (*TimeEntry, error) {
	entry, err := scanTimeEntry(s.db.QueryRow(`SELECT id, workspace_id, ` + scopedContextSelect("context_time_entries", "time_entry_id", "time_entries.id") + ` AS sphere, started_at, ended_at, activity, notes
		 FROM time_entries
		 WHERE ended_at IS NULL
		 ORDER BY started_at DESC, id DESC
		 LIMIT 1`))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &entry, nil
}

func (s *Store) StartTimeEntry(at time.Time, workspaceID *int64, sphere, activity string, notes *string) (TimeEntry, error) {
	if err := s.validateTimeEntryContext(workspaceID, sphere); err != nil {
		return TimeEntry{}, err
	}
	startedAt := formatTimeEntryTimestamp(at)
	cleanSphere := normalizeRequiredSphere(sphere)
	cleanActivity := strings.TrimSpace(activity)
	if cleanActivity == "" {
		cleanActivity = "context_switch"
	}
	res, err := s.db.Exec(`INSERT INTO time_entries (workspace_id, started_at, activity, notes)
		 VALUES (?, ?, ?, ?)`, nullablePositiveID(derefInt64(workspaceID)), startedAt, cleanActivity, normalizeOptionalString(notes))
	if err != nil {
		return TimeEntry{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return TimeEntry{}, err
	}
	if err := s.syncScopedContextLink("context_time_entries", "time_entry_id", id, cleanSphere); err != nil {
		return TimeEntry{}, err
	}
	return s.GetTimeEntry(id)
}

func (s *Store) SwitchActiveTimeEntry(at time.Time, workspaceID *int64, sphere, activity string, notes *string) (TimeEntry, bool, error) {
	if err := s.validateTimeEntryContext(workspaceID, sphere); err != nil {
		return TimeEntry{}, false, err
	}
	active, err := s.ActiveTimeEntry()
	if err != nil {
		return TimeEntry{}, false, err
	}
	if timeEntryContextMatches(active, workspaceID, sphere) {
		return *active, false, nil
	}
	if _, err := s.StopActiveTimeEntries(at); err != nil {
		return TimeEntry{}, false, err
	}
	entry, err := s.StartTimeEntry(at, workspaceID, sphere, activity, notes)
	if err != nil {
		return TimeEntry{}, false, err
	}
	return entry, true, nil
}

func (s *Store) StopActiveTimeEntries(at time.Time) (int64, error) {
	res, err := s.db.Exec(`UPDATE time_entries
		 SET ended_at = ?
		 WHERE ended_at IS NULL`, formatTimeEntryTimestamp(at))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *Store) GetTimeEntry(id int64) (TimeEntry, error) {
	return scanTimeEntry(s.db.QueryRow(`SELECT id, workspace_id, `+scopedContextSelect("context_time_entries", "time_entry_id", "time_entries.id")+` AS sphere, started_at, ended_at, activity, notes
		 FROM time_entries
		 WHERE id = ?`, id))
}

func (s *Store) ListTimeEntries(filter TimeEntryListFilter) ([]TimeEntry, error) {
	normalized, err := normalizeTimeEntryFilter(filter)
	if err != nil {
		return nil, err
	}
	query := `SELECT id, workspace_id, ` + scopedContextSelect("context_time_entries", "time_entry_id", "time_entries.id") + ` AS sphere, started_at, ended_at, activity, notes
		FROM time_entries`
	parts := make([]string, 0, 4)
	args := make([]any, 0, 4)
	if normalized.Sphere != "" {
		parts = append(parts, scopedContextFilter("context_time_entries", "time_entry_id", "time_entries.id"))
		args = append(args, normalized.Sphere)
	}
	if normalized.ActiveOnly {
		parts = append(parts, "ended_at IS NULL")
	}
	if normalized.From != nil {
		parts = append(parts, "(ended_at IS NULL OR ended_at >= ?)")
		args = append(args, formatTimeEntryTimestamp(*normalized.From))
	}
	if normalized.To != nil {
		parts = append(parts, "started_at < ?")
		args = append(args, formatTimeEntryTimestamp(*normalized.To))
	}
	if len(parts) > 0 {
		query += " WHERE " + strings.Join(parts, " AND ")
	}
	query += " ORDER BY started_at ASC, id ASC"
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := []TimeEntry{}
	for rows.Next() {
		entry, err := scanTimeEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

func (s *Store) SummarizeTimeEntries(filter TimeEntryListFilter, groupBy string, now time.Time) ([]TimeEntrySummary, error) {
	normalized, err := normalizeTimeEntryFilter(filter)
	if err != nil {
		return nil, err
	}
	cleanGroupBy := strings.ToLower(strings.TrimSpace(groupBy))
	if cleanGroupBy == "project" {
		cleanGroupBy = "workspace"
	}
	switch cleanGroupBy {
	case "workspace", "sphere":
	default:
		return nil, errors.New("group_by must be workspace, project or sphere")
	}
	entries, err := s.ListTimeEntries(normalized)
	if err != nil {
		return nil, err
	}
	now = now.UTC()
	type key struct{ value string }
	summaries := map[key]*TimeEntrySummary{}
	workspaceLabels := map[int64]string{}
	for _, entry := range entries {
		startedAt, err := time.Parse(time.RFC3339, entry.StartedAt)
		if err != nil {
			return nil, fmt.Errorf("parse started_at for time entry %d: %w", entry.ID, err)
		}
		endedAt := now
		if entry.EndedAt != nil {
			endedAt, err = time.Parse(time.RFC3339, *entry.EndedAt)
			if err != nil {
				return nil, fmt.Errorf("parse ended_at for time entry %d: %w", entry.ID, err)
			}
		}
		if normalized.From != nil && startedAt.Before(*normalized.From) {
			startedAt = *normalized.From
		}
		if normalized.To != nil && endedAt.After(*normalized.To) {
			endedAt = *normalized.To
		}
		if !endedAt.After(startedAt) {
			continue
		}
		seconds := int64(endedAt.Sub(startedAt).Seconds())
		if seconds <= 0 {
			continue
		}
		summaryKey, summary := summarizeTimeEntry(entry, cleanGroupBy)
		if cleanGroupBy == "workspace" && entry.WorkspaceID != nil {
			if _, ok := workspaceLabels[*entry.WorkspaceID]; !ok {
				workspace, err := s.GetWorkspace(*entry.WorkspaceID)
				if err != nil {
					return nil, err
				}
				workspaceLabels[*entry.WorkspaceID] = workspace.Name
			}
			summary.Label = workspaceLabels[*entry.WorkspaceID]
		}
		current := summaries[key{value: summaryKey}]
		if current == nil {
			copySummary := summary
			summaries[key{value: summaryKey}] = &copySummary
			current = &copySummary
		}
		current.Seconds += seconds
		current.EntryCount++
		current.Duration = formatDurationSeconds(current.Seconds)
	}
	rows := make([]TimeEntrySummary, 0, len(summaries))
	for _, summary := range summaries {
		rows = append(rows, *summary)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Seconds != rows[j].Seconds {
			return rows[i].Seconds > rows[j].Seconds
		}
		return rows[i].Label < rows[j].Label
	})
	return rows, nil
}

func summarizeTimeEntry(entry TimeEntry, groupBy string) (string, TimeEntrySummary) {
	switch groupBy {
	case "workspace":
		if entry.WorkspaceID == nil {
			return "workspace:none", TimeEntrySummary{Key: "workspace:none", Label: "No workspace", Sphere: entry.Sphere}
		}
		return fmt.Sprintf("workspace:%d", *entry.WorkspaceID), TimeEntrySummary{Key: fmt.Sprintf("workspace:%d", *entry.WorkspaceID), Label: fmt.Sprintf("Workspace %d", *entry.WorkspaceID), WorkspaceID: entry.WorkspaceID, Sphere: entry.Sphere}
	default:
		return "sphere:" + entry.Sphere, TimeEntrySummary{Key: "sphere:" + entry.Sphere, Label: entry.Sphere, Sphere: entry.Sphere}
	}
}

func formatDurationSeconds(total int64) string {
	if total < 0 {
		total = 0
	}
	hours := total / 3600
	minutes := (total % 3600) / 60
	if hours == 0 {
		return fmt.Sprintf("%dm", minutes)
	}
	if minutes == 0 {
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dh %dm", hours, minutes)
}

func derefInt64(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func normalizeWorkspaceCompanionConfigJSON(configJSON string) string {
	clean := strings.TrimSpace(configJSON)
	if clean == "" {
		return "{}"
	}
	return clean
}

func (s *Store) UpdateWorkspaceCompanionConfig(id int64, configJSON string) error {
	if id <= 0 {
		return errors.New("workspace id is required")
	}
	_, err := s.db.Exec(`UPDATE workspaces SET companion_config_json = ?, updated_at = datetime('now') WHERE id = ?`, normalizeWorkspaceCompanionConfigJSON(configJSON), id)
	return err
}

const (
	appStateActiveWorkspaceIDKey = "active_workspace_id"
	workspaceNameStatePrefix     = "project_name:"
	workspacePathStatePrefix     = "project_workspace_path:"
	workspaceRootPathStatePrefix = "project_root_path:"
	workspaceKindStatePrefix     = "project_kind:"
) // DB-stored key prefixes are intentionally kept unchanged for backward
// compatibility with existing databases.

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

func (s *Store) enrichWorkspace(workspace *Workspace) {
	workspace.Kind = s.workspaceKind(workspace.ID)
	workspace.RootPath = s.workspaceRootPath(workspace.ID, workspace.DirPath)
	workspace.WorkspacePath = s.workspaceStoredPath(workspace.ID, workspace.DirPath)
	workspace.Name = s.workspaceOverrideName(workspace.ID, workspace.Name)
	activeID, err := s.activeWorkspaceIDFromState()
	if err == nil {
		workspace.IsDefault = strings.TrimSpace(activeID) == workspaceIDString(workspace.ID) || strings.EqualFold(strings.TrimSpace(workspace.CanvasSessionID), "local")
	}
} // enrichWorkspace populates the Kind, RootPath, IsDefault fields from
// app_state keys. It is called after every workspace scan/fetch.

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

func (s *Store) ListEnrichedWorkspaces() ([]Workspace, // ListEnrichedWorkspaces returns all workspaces enriched with app_state metadata.
	error) {
	workspaces, err := s.ListWorkspaces()
	if err != nil {
		return nil, err
	}
	for i := range workspaces {
		s.enrichWorkspace(&workspaces[i])
	}
	return workspaces, nil
}

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
} // GetProject returns a workspace by string ID, enriched with app_state
// metadata. This is a compatibility shim; callers should migrate to
// GetWorkspace with int64 IDs.

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
