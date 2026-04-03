package todoist

import (
	"fmt"
	"strings"
	"unicode"
)

const (
	defaultBaseURL     = "https://api.todoist.com/rest/v2"
	defaultMoveBaseURL = "https://api.todoist.com/api/v1"
)

type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	if e == nil {
		return ""
	}
	if strings.TrimSpace(e.Body) == "" {
		return fmt.Sprintf("todoist API returned HTTP %d", e.StatusCode)
	}
	return fmt.Sprintf("todoist API returned HTTP %d: %s", e.StatusCode, e.Body)
}

type Project struct {
	ID             string  `json:"id"`
	ParentID       *string `json:"parent_id,omitempty"`
	Order          int     `json:"order"`
	Color          string  `json:"color"`
	Name           string  `json:"name"`
	CommentCount   int     `json:"comment_count"`
	IsShared       bool    `json:"is_shared"`
	IsFavorite     bool    `json:"is_favorite"`
	IsInboxProject bool    `json:"is_inbox_project"`
	IsTeamInbox    bool    `json:"is_team_inbox"`
	ViewStyle      string  `json:"view_style"`
	URL            string  `json:"url"`
}

type Due struct {
	String      string  `json:"string"`
	Date        string  `json:"date"`
	IsRecurring bool    `json:"is_recurring"`
	DateTime    *string `json:"datetime,omitempty"`
	Timezone    *string `json:"timezone,omitempty"`
	Lang        *string `json:"lang,omitempty"`
}

type Deadline struct {
	Date string `json:"date"`
}

type Duration struct {
	Amount int    `json:"amount"`
	Unit   string `json:"unit"`
}

type Task struct {
	ID           string    `json:"id"`
	CreatorID    *string   `json:"creator_id,omitempty"`
	CreatedAt    *string   `json:"created_at,omitempty"`
	AssigneeID   *string   `json:"assignee_id,omitempty"`
	AssignerID   *string   `json:"assigner_id,omitempty"`
	CommentCount int       `json:"comment_count"`
	IsCompleted  bool      `json:"is_completed"`
	Content      string    `json:"content"`
	Description  string    `json:"description"`
	Due          *Due      `json:"due,omitempty"`
	Deadline     *Deadline `json:"deadline,omitempty"`
	Duration     *Duration `json:"duration,omitempty"`
	Labels       []string  `json:"labels,omitempty"`
	Order        int       `json:"order"`
	Priority     int       `json:"priority"`
	ProjectID    *string   `json:"project_id,omitempty"`
	SectionID    *string   `json:"section_id,omitempty"`
	ParentID     *string   `json:"parent_id,omitempty"`
	URL          string    `json:"url"`
}

type CommentAttachment struct {
	FileName     string `json:"file_name"`
	FileType     string `json:"file_type"`
	FileURL      string `json:"file_url"`
	ResourceType string `json:"resource_type"`
}

type Comment struct {
	ID         string             `json:"id"`
	TaskID     *string            `json:"task_id,omitempty"`
	ProjectID  *string            `json:"project_id,omitempty"`
	PostedAt   string             `json:"posted_at"`
	Content    string             `json:"content"`
	Attachment *CommentAttachment `json:"attachment,omitempty"`
}

type TaskDetail struct {
	Task     Task      `json:"task"`
	Comments []Comment `json:"comments"`
}

type ListTasksOptions struct {
	ProjectID string
	SectionID string
	Label     string
	Filter    string
	Lang      string
	IDs       []string
	// DueFilter passes Todoist filter syntax for due-date queries, for example
	// "today" or "due before: 2026-03-10".
	DueFilter string
}

type CreateTaskRequest struct {
	Content      string
	Description  string
	ProjectID    string
	SectionID    string
	ParentID     string
	Order        int
	Labels       []string
	Priority     int
	DueString    string
	DueDate      string
	DueDateTime  string
	DueLang      string
	AssigneeID   string
	Duration     *Duration
	DeadlineDate string
}

type UpdateTaskRequest struct {
	Content      *string
	Description  *string
	Labels       *[]string
	Priority     *int
	DueString    *string
	DueDate      *string
	DueDateTime  *string
	DueLang      *string
	AssigneeID   *string
	Duration     *Duration
	DeadlineDate *string
	ProjectID    *string
	SectionID    *string
	ParentID     *string
}

func TokenEnvVar(label string) string {
	return "SLOPSHELL_TODOIST_TOKEN_" + sanitizeEnvSegment(label)
}

func sanitizeEnvSegment(raw string) string {
	var b strings.Builder
	lastUnderscore := true
	for _, r := range strings.ToUpper(strings.TrimSpace(raw)) {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			b.WriteRune(r)
			lastUnderscore = false
		case !lastUnderscore:
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	clean := strings.Trim(b.String(), "_")
	if clean == "" {
		return "DEFAULT"
	}
	return clean
}
