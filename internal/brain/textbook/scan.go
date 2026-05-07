package textbook

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ScanResult is one note's classification.
type ScanResult struct {
	Path    string  `json:"path"`
	Verdict Verdict `json:"verdict"`
	Match   string  `json:"match,omitempty"`
	Title   string  `json:"title,omitempty"`
}

// Summary aggregates a scan over a vault.
type Summary struct {
	Total     int          `json:"total"`
	Keep      int          `json:"keep"`
	Compress  int          `json:"compress"`
	Reject    int          `json:"reject"`
	Rejects   []ScanResult `json:"rejects,omitempty"`
	Compress_ []ScanResult `json:"compress_candidates,omitempty"`
}

// scanRoots are the relative directories under brainRoot that are eligible
// for textbook reject. Canonical entity directories (people/, projects/,
// institutions/) are walked to surface compress candidates only — never
// reject — by Classify itself.
var scanRoots = []string{
	"topics",
	"glossary",
	"people",
	"projects",
	"institutions",
}

// Scan walks the brain root and classifies every Markdown note under the
// scanRoots. brainRoot is the absolute filesystem path to <vault>/brain.
func (c *Classifier) Scan(brainRoot string) (Summary, error) {
	var s Summary
	for _, root := range scanRoots {
		dir := filepath.Join(brainRoot, root)
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				if os.IsNotExist(walkErr) {
					return filepath.SkipDir
				}
				return walkErr
			}
			if d.IsDir() || filepath.Ext(path) != ".md" {
				return nil
			}
			rel, err := filepath.Rel(brainRoot, path)
			if err != nil {
				return err
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			note := buildNote(filepath.ToSlash(rel), string(body))
			verdict, match := c.Classify(note)
			s.Total++
			r := ScanResult{Path: filepath.ToSlash(rel), Verdict: verdict, Match: match, Title: note.Title}
			switch verdict {
			case VerdictKeep:
				s.Keep++
			case VerdictCompress:
				s.Compress++
				s.Compress_ = append(s.Compress_, r)
			case VerdictReject:
				s.Reject++
				s.Rejects = append(s.Rejects, r)
			}
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			return s, err
		}
	}
	sort.Slice(s.Rejects, func(i, j int) bool { return s.Rejects[i].Path < s.Rejects[j].Path })
	sort.Slice(s.Compress_, func(i, j int) bool { return s.Compress_[i].Path < s.Compress_[j].Path })
	return s, nil
}

// buildNote turns a vault-relative path and raw Markdown into the input
// the Classifier needs. Frontmatter parsing tolerates malformed YAML by
// falling back to empty defaults.
func buildNote(relPath, raw string) Note {
	fm, body := splitFrontMatter(raw)
	n := Note{
		Path:  strings.TrimSuffix(relPath, ".md"),
		Body:  body,
		Title: firstH1(body),
	}
	if n.Title == "" {
		n.Title = strings.TrimSuffix(filepath.Base(relPath), ".md")
	}
	if fm != "" {
		var meta struct {
			Strategic bool   `yaml:"strategic"`
			Focus     string `yaml:"focus"`
			Cadence   string `yaml:"cadence"`
		}
		_ = yaml.Unmarshal([]byte(fm), &meta)
		n.Strategic = meta.Strategic
		n.Focus = meta.Focus
		n.Cadence = meta.Cadence
	}
	return n
}

func splitFrontMatter(raw string) (string, string) {
	if !strings.HasPrefix(raw, "---") {
		return "", raw
	}
	rest := raw[3:]
	rest = strings.TrimLeft(rest, "\r")
	if !strings.HasPrefix(rest, "\n") {
		return "", raw
	}
	rest = rest[1:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", raw
	}
	fm := rest[:end]
	body := rest[end+4:]
	body = strings.TrimLeft(body, "\r\n")
	return fm, body
}

func firstH1(body string) string {
	for _, line := range strings.Split(body, "\n") {
		s := strings.TrimSpace(line)
		if strings.HasPrefix(s, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(s, "# "))
		}
	}
	return ""
}
