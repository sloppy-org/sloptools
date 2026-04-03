package evernote

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"
)

const defaultAPIBaseURL = "https://api.evernote.com"

var (
	ErrTokenNotConfigured = errors.New("evernote token is not configured")
	ErrNoteIDRequired     = errors.New("evernote note id is required")
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
		return fmt.Sprintf("evernote API returned HTTP %d", e.StatusCode)
	}
	return fmt.Sprintf("evernote API returned HTTP %d: %s", e.StatusCode, e.Body)
}

type Notebook struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Stack     string         `json:"stack,omitempty"`
	UpdatedAt string         `json:"updated_at,omitempty"`
	Raw       map[string]any `json:"raw,omitempty"`
}

type NoteSummary struct {
	ID          string         `json:"id"`
	NotebookID  string         `json:"notebook_id,omitempty"`
	Title       string         `json:"title,omitempty"`
	UpdatedAt   string         `json:"updated_at,omitempty"`
	CreatedAt   string         `json:"created_at,omitempty"`
	TagNames    []string       `json:"tag_names,omitempty"`
	ContentText string         `json:"content_text,omitempty"`
	Raw         map[string]any `json:"raw,omitempty"`
}

type Task struct {
	Text    string `json:"text"`
	Checked bool   `json:"checked"`
}

type Note struct {
	ID              string         `json:"id"`
	NotebookID      string         `json:"notebook_id,omitempty"`
	Title           string         `json:"title,omitempty"`
	CreatedAt       string         `json:"created_at,omitempty"`
	UpdatedAt       string         `json:"updated_at,omitempty"`
	TagNames        []string       `json:"tag_names,omitempty"`
	ContentENML     string         `json:"content_enml,omitempty"`
	ContentText     string         `json:"content_text,omitempty"`
	ContentMarkdown string         `json:"content_markdown,omitempty"`
	Tasks           []Task         `json:"tasks,omitempty"`
	Raw             map[string]any `json:"raw,omitempty"`
}

type Tag struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	ParentID string         `json:"parent_id,omitempty"`
	Raw      map[string]any `json:"raw,omitempty"`
}

type ListNotesOptions struct {
	Query        string
	Tag          string
	UpdatedAfter string
	Limit        int
	Offset       int
}

type notebookPayload struct {
	ID        string         `json:"id"`
	GUID      string         `json:"guid"`
	Name      string         `json:"name"`
	Stack     string         `json:"stack"`
	UpdatedAt string         `json:"updated_at"`
	UpdatedTS string         `json:"updated"`
	Raw       map[string]any `json:"-"`
}

type notePayload struct {
	ID           string         `json:"id"`
	GUID         string         `json:"guid"`
	NotebookID   string         `json:"notebook_id"`
	NotebookGUID string         `json:"notebookGuid"`
	Title        string         `json:"title"`
	UpdatedAt    string         `json:"updated_at"`
	UpdatedTS    string         `json:"updated"`
	CreatedAt    string         `json:"created_at"`
	CreatedTS    string         `json:"created"`
	TagNames     []string       `json:"tag_names"`
	Content      string         `json:"content"`
	ContentENML  string         `json:"content_enml"`
	ENML         string         `json:"enml"`
	Raw          map[string]any `json:"-"`
}

type tagPayload struct {
	ID         string         `json:"id"`
	GUID       string         `json:"guid"`
	Name       string         `json:"name"`
	ParentID   string         `json:"parent_id"`
	ParentGUID string         `json:"parentGuid"`
	Raw        map[string]any `json:"-"`
}

func TokenEnvVar(label string) string {
	return "SLOPSHELL_EVERNOTE_TOKEN_" + sanitizeEnvSegment(label)
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if clean := strings.TrimSpace(value); clean != "" {
			return clean
		}
	}
	return ""
}

func mustObject(data []byte) (map[string]any, error) {
	if len(data) == 0 {
		return map[string]any{}, nil
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return map[string]any{}, err
	}
	return out, nil
}
