package meetings

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DiscoveryResult is the relative-path list of meeting notes found under
// a configured meetings root, sorted lexicographically. Paths are
// returned absolute so callers can stat or read them directly.
type DiscoveryResult struct {
	Root  string
	Paths []string
}

// Discover walks root and returns every Markdown file that looks like a
// meeting note: any `MEETING_NOTES.md` (case-insensitive) plus any
// loose `.md` directly under root or under one of root's first-level
// subdirectories. Hidden directories (`.git`, `.obsidian`, etc.) and
// files ending in `.failed.md` are skipped.
func Discover(root string) (DiscoveryResult, error) {
	clean := strings.TrimSpace(root)
	if clean == "" {
		return DiscoveryResult{}, errors.New("meetings root is empty")
	}
	abs, err := filepath.Abs(clean)
	if err != nil {
		return DiscoveryResult{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return DiscoveryResult{Root: abs}, nil
		}
		return DiscoveryResult{}, err
	}
	if !info.IsDir() {
		return DiscoveryResult{}, errors.New("meetings root is not a directory: " + abs)
	}
	var hits []string
	walkErr := filepath.WalkDir(abs, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if isHiddenDir(entry.Name()) && path != abs {
				return filepath.SkipDir
			}
			return nil
		}
		if !looksLikeMeetingNote(entry.Name()) {
			return nil
		}
		hits = append(hits, path)
		return nil
	})
	if walkErr != nil {
		return DiscoveryResult{}, walkErr
	}
	sort.Strings(hits)
	return DiscoveryResult{Root: abs, Paths: hits}, nil
}

func looksLikeMeetingNote(name string) bool {
	if !strings.HasSuffix(strings.ToLower(name), ".md") {
		return false
	}
	if strings.HasSuffix(strings.ToLower(name), ".failed.md") {
		return false
	}
	if strings.EqualFold(name, "MEETING_NOTES.md") {
		return true
	}
	return true
}

func isHiddenDir(name string) bool {
	return strings.HasPrefix(name, ".") && name != "." && name != ".."
}
