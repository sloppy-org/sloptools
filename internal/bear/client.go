package bear

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const appleReferenceUnix = 978307200

var (
	ErrDatabaseNotFound = errors.New("bear database not found")

	bearTagPattern       = regexp.MustCompile(`(?:^|[\s(])#([A-Za-z0-9_/-]+)`)
	bearChecklistPattern = regexp.MustCompile(`(?m)^\s*(?:[-*+]|\d+[.)])\s+\[([ xX])\]\s+(.+?)\s*$`)
)

type Client struct {
	dbPath string
}

type Note struct {
	ID       string   `json:"id"`
	Title    string   `json:"title,omitempty"`
	Markdown string   `json:"markdown,omitempty"`
	Created  string   `json:"created,omitempty"`
	Modified string   `json:"modified,omitempty"`
	Tags     []string `json:"tags,omitempty"`
}

type ChecklistItem struct {
	Text    string `json:"text"`
	Checked bool   `json:"checked"`
}

func DefaultDatabasePath() string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(
		home,
		"Library",
		"Group Containers",
		"9K33E3U3T4.net.shinyfrog.bear",
		"Application Data",
		"database.sqlite",
	)
}

func NewClient(dbPath string) (*Client, error) {
	path := strings.TrimSpace(dbPath)
	if path == "" {
		path = DefaultDatabasePath()
	}
	if path == "" {
		return nil, ErrDatabaseNotFound
	}
	return &Client{dbPath: path}, nil
}

func (c *Client) DatabasePath() string {
	if c == nil {
		return ""
	}
	return c.dbPath
}

func (c *Client) ListNotes(ctx context.Context) ([]Note, error) {
	if c == nil || strings.TrimSpace(c.dbPath) == "" {
		return nil, ErrDatabaseNotFound
	}
	if _, err := os.Stat(c.dbPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrDatabaseNotFound
		}
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	db, err := sql.Open("sqlite", c.dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, `
SELECT
  trim(COALESCE(ZUNIQUEIDENTIFIER, '')),
  trim(COALESCE(ZTITLE, '')),
  COALESCE(ZTEXT, ''),
  ZCREATIONDATE,
  ZMODIFICATIONDATE
FROM ZSFNOTE
WHERE COALESCE(ZTRASHED, 0) = 0
  AND COALESCE(ZARCHIVED, 0) = 0
ORDER BY COALESCE(ZMODIFICATIONDATE, ZCREATIONDATE) DESC, Z_PK DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	notes := []Note{}
	for rows.Next() {
		var (
			note        Note
			createdRaw  any
			modifiedRaw any
		)
		if err := rows.Scan(&note.ID, &note.Title, &note.Markdown, &createdRaw, &modifiedRaw); err != nil {
			return nil, err
		}
		note.ID = strings.TrimSpace(note.ID)
		note.Title = strings.TrimSpace(note.Title)
		note.Markdown = strings.TrimSpace(note.Markdown)
		if note.ID == "" {
			continue
		}
		note.Created = formatBearTimestamp(createdRaw)
		note.Modified = formatBearTimestamp(modifiedRaw)
		note.Tags = ExtractTags(note.Markdown)
		notes = append(notes, note)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return notes, nil
}

func ExtractTags(markdown string) []string {
	matches := bearTagPattern.FindAllStringSubmatch(strings.TrimSpace(markdown), -1)
	if len(matches) == 0 {
		return nil
	}
	seen := map[string]bool{}
	tags := make([]string, 0, len(matches))
	for _, match := range matches {
		tag := strings.TrimSpace(match[1])
		if tag == "" {
			continue
		}
		key := strings.ToLower(tag)
		if seen[key] {
			continue
		}
		seen[key] = true
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	return tags
}

func ExtractChecklist(markdown string) []ChecklistItem {
	matches := bearChecklistPattern.FindAllStringSubmatch(strings.TrimSpace(markdown), -1)
	if len(matches) == 0 {
		return nil
	}
	items := make([]ChecklistItem, 0, len(matches))
	for _, match := range matches {
		text := strings.TrimSpace(match[2])
		if text == "" {
			continue
		}
		items = append(items, ChecklistItem{
			Text:    text,
			Checked: strings.EqualFold(strings.TrimSpace(match[1]), "x"),
		})
	}
	return items
}

func formatBearTimestamp(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case time.Time:
		return typed.UTC().Format(time.RFC3339)
	case string:
		return strings.TrimSpace(typed)
	case []byte:
		return strings.TrimSpace(string(typed))
	case int64:
		return appleReferenceTime(float64(typed))
	case float64:
		return appleReferenceTime(typed)
	case float32:
		return appleReferenceTime(float64(typed))
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func appleReferenceTime(seconds float64) string {
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) {
		return ""
	}
	base := time.Unix(appleReferenceUnix, 0).UTC()
	nanos := int64(seconds * float64(time.Second))
	return base.Add(time.Duration(nanos)).Format(time.RFC3339)
}
