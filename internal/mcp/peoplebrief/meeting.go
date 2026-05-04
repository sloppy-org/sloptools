package peoplebrief

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/brain"
)

var wikilinkPattern = regexp.MustCompile(`\[\[([^\]\n]+)\]\]`)

// LatestMeetingNote walks `<brainRoot>/meetings/**/*.md` and returns the
// most recent meeting note that wikilinks the person, picking the front
// matter `date` first and falling back to a YYYY-MM-DD filename prefix.
// Returns (nil, nil) when no meetings folder exists or no meeting links the
// person.
func LatestMeetingNote(vaultRoot, brainRoot, personName string) (*Meeting, error) {
	meetingsRoot := filepath.Join(brainRoot, "meetings")
	info, err := os.Stat(meetingsRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, nil
	}
	type candidate struct {
		path  string
		title string
		date  string
	}
	var matches []candidate
	walkErr := filepath.WalkDir(meetingsRoot, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() || filepath.Ext(path) != ".md" {
			return walkErr
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !meetingMentionsPerson(string(data), personName) {
			return nil
		}
		note, _ := brain.ParseMarkdownNote(string(data), brain.MarkdownParseOptions{})
		title := frontmatterStringField(note, "title")
		date := frontmatterStringField(note, "date")
		if date == "" {
			date = dateFromFilename(filepath.Base(path))
		}
		rel, err := filepath.Rel(vaultRoot, path)
		if err != nil {
			return err
		}
		matches = append(matches, candidate{path: filepath.ToSlash(rel), title: title, date: date})
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	if len(matches) == 0 {
		return nil, nil
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].date == matches[j].date {
			return matches[i].path > matches[j].path
		}
		return matches[i].date > matches[j].date
	})
	winner := matches[0]
	return &Meeting{Path: winner.path, Title: winner.title, Date: winner.date}, nil
}

func meetingMentionsPerson(src, person string) bool {
	target := normalizePersonName(person)
	if target == "" {
		return false
	}
	for _, match := range wikilinkPattern.FindAllStringSubmatch(src, -1) {
		if wikilinkResolvesToPerson(match[1], target) {
			return true
		}
	}
	return false
}

func wikilinkResolvesToPerson(raw, target string) bool {
	clean := strings.TrimSpace(strings.SplitN(strings.SplitN(raw, "|", 2)[0], "#", 2)[0])
	clean = strings.TrimSuffix(filepath.ToSlash(clean), ".md")
	if clean == "" {
		return false
	}
	parts := strings.Split(clean, "/")
	tail := parts[len(parts)-1]
	return normalizePersonName(tail) == target
}

func dateFromFilename(name string) string {
	clean := strings.TrimSuffix(name, filepath.Ext(name))
	if len(clean) < 10 {
		return ""
	}
	prefix := clean[:10]
	if _, err := time.Parse("2006-01-02", prefix); err != nil {
		return ""
	}
	return prefix
}
