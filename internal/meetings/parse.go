// Package meetings parses MEETING_NOTES-style brain notes into structured
// per-person tasks with stable comment-anchored IDs. The parser walks the
// `## Action Checklist` section, treats each `### <Person>` subsection as
// the task owner, and recognises optional inline metadata such as `@due:`,
// `@follow:`, and `^[[projects/X]]`. IDs are assigned by AssignIDs and
// written back into the source as HTML comments so that re-parsing is
// idempotent regardless of LLM-side formatting variance.
package meetings

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
)

// ActionChecklistSection is the H2 heading whose H3 subsections carry tasks.
const ActionChecklistSection = "Action Checklist"

// IDLength is the hex-character count of the stamped task ID.
const IDLength = 10

// Task is a single per-person checkbox extracted from a meeting note.
type Task struct {
	Person   string `json:"person,omitempty"`
	Text     string `json:"text"`
	Line     int    `json:"line"`
	ID       string `json:"id,omitempty"`
	Done     bool   `json:"done,omitempty"`
	Due      string `json:"due,omitempty"`
	FollowUp string `json:"follow_up,omitempty"`
	Project  string `json:"project,omitempty"`
}

// Note is the parsed view of a meeting source file.
type Note struct {
	Slug  string `json:"slug"`
	Tasks []Task `json:"tasks"`
}

var (
	idPattern      = regexp.MustCompile(`<!--\s*gtd:([0-9a-f]{` + fmt.Sprintf("%d", IDLength) + `})\s*-->`)
	checkboxPrefix = regexp.MustCompile(`^([ \t]*)([-*])\s*\[([ xX])\]\s*(.*)$`)
	duePattern     = regexp.MustCompile(`@due:([0-9]{4}-[0-9]{2}-[0-9]{2})`)
	followPattern  = regexp.MustCompile(`@follow:([0-9]{4}-[0-9]{2}-[0-9]{2})`)
	projectPattern = regexp.MustCompile(`\^\[\[projects/([^\]]+)\]\]`)
)

// Parse extracts the per-person task list from a meeting note source.
// The slug is recorded on the returned Note so callers can use it for
// stable ID generation and binding refs without re-deriving from paths.
func Parse(slug, src string) Note {
	note := Note{Slug: slug}
	scope := scanScope{}
	for index, raw := range strings.Split(src, "\n") {
		line := strings.TrimRight(raw, "\r")
		if heading, level := parseHeading(line); level > 0 {
			scope.update(level, heading)
			continue
		}
		if !scope.inActionChecklist {
			continue
		}
		task, ok := parseChecklistLine(scope.person, line, index+1)
		if !ok {
			continue
		}
		note.Tasks = append(note.Tasks, task)
	}
	return note
}

// ComputeID returns the deterministic 10-hex stable ID for a task, given
// the meeting slug, owning person, and the cleaned task text (stripped
// of inline metadata and the existing ID comment).
func ComputeID(slug, person, text string) string {
	sum := sha1.Sum([]byte(strings.Join([]string{
		strings.TrimSpace(slug),
		strings.TrimSpace(strings.ToLower(person)),
		cleanForID(text),
	}, "\x00")))
	return hex.EncodeToString(sum[:])[:IDLength]
}

// FormatComment renders the HTML comment that anchors a task ID on the source line.
func FormatComment(id string) string {
	return "<!-- gtd:" + id + " -->"
}

type scanScope struct {
	inActionChecklist bool
	person            string
}

func (s *scanScope) update(level int, heading string) {
	switch level {
	case 1:
		s.inActionChecklist = false
		s.person = ""
	case 2:
		s.inActionChecklist = strings.EqualFold(strings.TrimSpace(heading), ActionChecklistSection)
		s.person = ""
	case 3:
		if s.inActionChecklist {
			s.person = strings.TrimSpace(heading)
		}
	default:
		// Higher-level headings (####+) leave person/section scope intact.
	}
}

func parseHeading(line string) (string, int) {
	trimmed := strings.TrimLeft(line, " \t")
	if !strings.HasPrefix(trimmed, "#") {
		return "", 0
	}
	level := 0
	for level < len(trimmed) && trimmed[level] == '#' {
		level++
	}
	if level == 0 || level > 6 {
		return "", 0
	}
	if level < len(trimmed) && trimmed[level] != ' ' && trimmed[level] != '\t' {
		return "", 0
	}
	rest := strings.TrimSpace(strings.TrimRight(trimmed[level:], "#"))
	return rest, level
}

func parseChecklistLine(person, line string, lineNumber int) (Task, bool) {
	match := checkboxPrefix.FindStringSubmatch(line)
	if match == nil {
		return Task{}, false
	}
	rest := match[4]
	id, restWithoutID := extractID(rest)
	due, restAfterDue := extractTagValue(restWithoutID, duePattern)
	follow, restAfterFollow := extractTagValue(restAfterDue, followPattern)
	project, restAfterProject := extractTagValue(restAfterFollow, projectPattern)
	text := strings.TrimSpace(restAfterProject)
	if text == "" {
		return Task{}, false
	}
	done := strings.ContainsAny(match[3], "xX")
	return Task{
		Person:   person,
		Text:     text,
		Line:     lineNumber,
		ID:       id,
		Done:     done,
		Due:      due,
		FollowUp: follow,
		Project:  project,
	}, true
}

func extractID(text string) (string, string) {
	match := idPattern.FindStringSubmatch(text)
	if match == nil {
		return "", text
	}
	return match[1], strings.Replace(text, match[0], "", 1)
}

func extractTagValue(text string, pattern *regexp.Regexp) (string, string) {
	match := pattern.FindStringSubmatch(text)
	if match == nil {
		return "", text
	}
	return match[1], strings.Replace(text, match[0], "", 1)
}

func cleanForID(text string) string {
	rest := text
	rest = idPattern.ReplaceAllString(rest, "")
	rest = duePattern.ReplaceAllString(rest, "")
	rest = followPattern.ReplaceAllString(rest, "")
	rest = projectPattern.ReplaceAllString(rest, "")
	return strings.ToLower(strings.Join(strings.Fields(rest), " "))
}
