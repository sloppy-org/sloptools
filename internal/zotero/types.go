package zotero

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

const defaultAPIBaseURL = "https://api.zotero.org"

var (
	ErrAPIKeyNotConfigured = errors.New("zotero API key is not configured")
	ErrDatabaseNotFound    = errors.New("zotero database not found")
	ErrUserIDRequired      = errors.New("zotero user id is required")
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
		return fmt.Sprintf("zotero API returned HTTP %d", e.StatusCode)
	}
	return fmt.Sprintf("zotero API returned HTTP %d: %s", e.StatusCode, e.Body)
}

type Creator struct {
	FirstName string `json:"first_name,omitempty"`
	LastName  string `json:"last_name,omitempty"`
	Name      string `json:"name,omitempty"`
	Type      string `json:"type,omitempty"`
	Order     int    `json:"order"`
}

type Item struct {
	ID           int64     `json:"id"`
	Key          string    `json:"key"`
	ItemType     string    `json:"item_type"`
	Title        string    `json:"title,omitempty"`
	Journal      string    `json:"journal,omitempty"`
	DOI          string    `json:"doi,omitempty"`
	ISBN         string    `json:"isbn,omitempty"`
	Abstract     string    `json:"abstract,omitempty"`
	Year         string    `json:"year,omitempty"`
	DateAdded    string    `json:"date_added,omitempty"`
	DateModified string    `json:"date_modified,omitempty"`
	CitationKey  string    `json:"citation_key,omitempty"`
	Creators     []Creator `json:"creators,omitempty"`
	Collections  []string  `json:"collections,omitempty"`
	Tags         []string  `json:"tags,omitempty"`
}

type Attachment struct {
	ID           int64  `json:"id"`
	Key          string `json:"key"`
	ParentKey    string `json:"parent_key,omitempty"`
	Title        string `json:"title,omitempty"`
	ContentType  string `json:"content_type,omitempty"`
	Path         string `json:"path,omitempty"`
	LinkMode     int64  `json:"link_mode"`
	DateModified string `json:"date_modified,omitempty"`
}

type Collection struct {
	ID        int64  `json:"id"`
	Key       string `json:"key"`
	Name      string `json:"name"`
	ParentKey string `json:"parent_key,omitempty"`
}

type Tag struct {
	Name      string `json:"name"`
	ItemCount int    `json:"item_count"`
}

type Annotation struct {
	ID             int64  `json:"id"`
	Key            string `json:"key"`
	ParentKey      string `json:"parent_key,omitempty"`
	AnnotationType string `json:"annotation_type,omitempty"`
	AuthorName     string `json:"author_name,omitempty"`
	Text           string `json:"text,omitempty"`
	Comment        string `json:"comment,omitempty"`
	Color          string `json:"color,omitempty"`
	PageLabel      string `json:"page_label,omitempty"`
	SortIndex      string `json:"sort_index,omitempty"`
	Position       string `json:"position,omitempty"`
	DateModified   string `json:"date_modified,omitempty"`
}

type RemoteCreator struct {
	CreatorType string `json:"creatorType,omitempty"`
	FirstName   string `json:"firstName,omitempty"`
	LastName    string `json:"lastName,omitempty"`
	Name        string `json:"name,omitempty"`
}

type RemoteTag struct {
	Tag  string `json:"tag"`
	Type int    `json:"type,omitempty"`
}

type RemoteItem struct {
	Key          string          `json:"key"`
	Version      int             `json:"version"`
	ItemType     string          `json:"item_type,omitempty"`
	Title        string          `json:"title,omitempty"`
	AbstractNote string          `json:"abstract_note,omitempty"`
	DOI          string          `json:"doi,omitempty"`
	ISBN         string          `json:"isbn,omitempty"`
	Date         string          `json:"date,omitempty"`
	PublicTitle  string          `json:"publication_title,omitempty"`
	ParentItem   string          `json:"parent_item,omitempty"`
	Creators     []RemoteCreator `json:"creators,omitempty"`
	Tags         []RemoteTag     `json:"tags,omitempty"`
	Raw          map[string]any  `json:"raw,omitempty"`
}

type RemoteCollection struct {
	Key       string         `json:"key"`
	Version   int            `json:"version"`
	Name      string         `json:"name"`
	ParentKey string         `json:"parent_key,omitempty"`
	Raw       map[string]any `json:"raw,omitempty"`
}

type ListRemoteItemsOptions struct {
	Limit         int
	CollectionKey string
	SinceVersion  int
}

type ListRemoteCollectionsOptions struct {
	Limit        int
	TopLevelOnly bool
	SinceVersion int
}

func APIKeyEnvVar(label string) string {
	return "SLOPSHELL_ZOTERO_API_KEY_" + sanitizeEnvSegment(label)
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
