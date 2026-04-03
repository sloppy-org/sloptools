package store

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const timeEntriesTableSchema = `CREATE TABLE IF NOT EXISTS time_entries (
  id INTEGER PRIMARY KEY,
  workspace_id INTEGER REFERENCES workspaces(id) ON DELETE SET NULL,
  started_at TEXT NOT NULL,
  ended_at TEXT,
  activity TEXT NOT NULL DEFAULT '',
  notes TEXT
);`

func scanTimeEntry(scanner interface {
	Scan(dest ...any) error
}) (TimeEntry, error) {
	var (
		out       TimeEntry
		workspace sql.NullInt64
		endedAt   sql.NullString
		notes     sql.NullString
	)
	if err := scanner.Scan(
		&out.ID,
		&workspace,
		&out.Sphere,
		&out.StartedAt,
		&endedAt,
		&out.Activity,
		&notes,
	); err != nil {
		return TimeEntry{}, err
	}
	out.WorkspaceID = nullInt64Pointer(workspace)
	out.EndedAt = nullStringPointer(endedAt)
	out.Notes = nullStringPointer(notes)
	out.Sphere = normalizeSphere(out.Sphere)
	out.Activity = strings.TrimSpace(out.Activity)
	return out, nil
}

func formatTimeEntryTimestamp(ts time.Time) string {
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	return ts.UTC().Format(time.RFC3339)
}

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
	return scanWorkspace(s.db.QueryRow(
		`SELECT id, name, dir_path, ` + scopedContextSelect("context_workspaces", "workspace_id", "workspaces.id") + ` AS sphere, is_active, is_daily, daily_date, mcp_url, canvas_session_id, chat_model, chat_model_reasoning_effort, companion_config_json, created_at, updated_at
		 FROM workspaces
		 WHERE is_active <> 0
		 ORDER BY updated_at DESC, id DESC
		 LIMIT 1`,
	))
}

func (s *Store) ActiveTimeEntry() (*TimeEntry, error) {
	entry, err := scanTimeEntry(s.db.QueryRow(
		`SELECT id, workspace_id, ` + scopedContextSelect("context_time_entries", "time_entry_id", "time_entries.id") + ` AS sphere, started_at, ended_at, activity, notes
		 FROM time_entries
		 WHERE ended_at IS NULL
		 ORDER BY started_at DESC, id DESC
		 LIMIT 1`,
	))
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
	res, err := s.db.Exec(
		`INSERT INTO time_entries (workspace_id, started_at, activity, notes)
		 VALUES (?, ?, ?, ?)`,
		nullablePositiveID(derefInt64(workspaceID)),
		startedAt,
		cleanActivity,
		normalizeOptionalString(notes),
	)
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
	res, err := s.db.Exec(
		`UPDATE time_entries
		 SET ended_at = ?
		 WHERE ended_at IS NULL`,
		formatTimeEntryTimestamp(at),
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *Store) GetTimeEntry(id int64) (TimeEntry, error) {
	return scanTimeEntry(s.db.QueryRow(
		`SELECT id, workspace_id, `+scopedContextSelect("context_time_entries", "time_entry_id", "time_entries.id")+` AS sphere, started_at, ended_at, activity, notes
		 FROM time_entries
		 WHERE id = ?`,
		id,
	))
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
	type key struct {
		value string
	}
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
			return "workspace:none", TimeEntrySummary{
				Key:    "workspace:none",
				Label:  "No workspace",
				Sphere: entry.Sphere,
			}
		}
		return fmt.Sprintf("workspace:%d", *entry.WorkspaceID), TimeEntrySummary{
			Key:         fmt.Sprintf("workspace:%d", *entry.WorkspaceID),
			Label:       fmt.Sprintf("Workspace %d", *entry.WorkspaceID),
			WorkspaceID: entry.WorkspaceID,
			Sphere:      entry.Sphere,
		}
	default:
		return "sphere:" + entry.Sphere, TimeEntrySummary{
			Key:    "sphere:" + entry.Sphere,
			Label:  entry.Sphere,
			Sphere: entry.Sphere,
		}
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
