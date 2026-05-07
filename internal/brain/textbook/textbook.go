// Package textbook classifies brain notes against a deny-list of
// textbook-only topics (Wikipedia-derivable concepts with no local
// anchor to Christopher Albert's group or projects).
//
// The classifier has three outputs:
//
//   - Reject (pure textbook):       deny-list match + zero local anchor
//   - Compress (mixed):             deny-list match + at least one
//     local anchor; textbook prose can
//     be replaced with a one-line pointer
//   - Keep (canonical or anchored): not on deny-list, OR canonical
//     entity note (people/projects/
//     institutions), never archived for
//     being on Wikipedia
package textbook

import (
	"bufio"
	_ "embed"
	"path/filepath"
	"regexp"
	"strings"
)

//go:embed deny-list.txt
var embeddedDenyList string

// Verdict is the classifier's decision for one note.
type Verdict string

const (
	// VerdictReject: pure textbook, zero local anchor; safe to archive.
	VerdictReject Verdict = "reject"
	// VerdictCompress: mixed; deny-list match plus at least one local
	// anchor. Textbook prose can be compressed; every locally-specific
	// fact stays.
	VerdictCompress Verdict = "compress"
	// VerdictKeep: not on deny-list, or canonical entity. Never
	// archived.
	VerdictKeep Verdict = "keep"
)

// Note is the classifier input.
type Note struct {
	// Path is the vault-relative slug, e.g. "topics/boltzmann-equation".
	Path string
	// Title is the H1 title (or filename stem if missing).
	Title string
	// Body is the Markdown body, used for anchor scanning.
	Body string
	// Frontmatter signals: strategic / focus / cadence / archived.
	Strategic bool
	Focus     string
	Cadence   string
}

// Anchor classes a note can link to. Pre-defined here so Classifier can
// match wikilinks without re-implementing the parser.
var anchorPrefixes = []string{
	"people/", "projects/", "institutions/", "commitments/",
	"folders/plasma/", "folders/itp/", "folders/teaching/",
}

// Canonical entity directories — notes here are never archived,
// regardless of deny-list match.
var canonicalRoots = []string{
	"people/", "projects/", "institutions/",
}

// Classifier holds the deny-list patterns.
type Classifier struct {
	patterns []string // lowercase substrings
}

// New builds a Classifier from the embedded deny-list.
func New() *Classifier {
	return &Classifier{patterns: parseDenyList(embeddedDenyList)}
}

// FromFile reads a deny-list from disk (overrides the embedded copy,
// useful when the user updates data/brain/textbook-deny.txt without a
// re-build).
func FromFile(path string) (*Classifier, error) {
	body, err := readFile(path)
	if err != nil {
		return nil, err
	}
	return &Classifier{patterns: parseDenyList(body)}, nil
}

// Patterns returns the loaded deny-list patterns (for tests).
func (c *Classifier) Patterns() []string {
	out := make([]string, len(c.patterns))
	copy(out, c.patterns)
	return out
}

// Classify returns the verdict and the matched pattern (empty string
// when no deny-list match).
func (c *Classifier) Classify(n Note) (Verdict, string) {
	if isCanonical(n.Path) {
		return VerdictKeep, ""
	}
	if n.Strategic || n.Focus == "core" || n.Cadence == "daily" {
		return VerdictKeep, ""
	}
	matched := c.matchPattern(n)
	if matched == "" {
		return VerdictKeep, ""
	}
	if hasLocalAnchor(n.Body) {
		return VerdictCompress, matched
	}
	return VerdictReject, matched
}

// matchPattern checks the slug and title against deny-list substrings.
func (c *Classifier) matchPattern(n Note) string {
	stem := strings.ToLower(filepath.Base(n.Path))
	stem = strings.TrimSuffix(stem, filepath.Ext(stem))
	title := strings.ToLower(n.Title)
	for _, pat := range c.patterns {
		if pat == "" {
			continue
		}
		if strings.Contains(stem, pat) || strings.Contains(title, pat) {
			return pat
		}
	}
	return ""
}

func isCanonical(path string) bool {
	clean := strings.TrimPrefix(strings.TrimPrefix(filepath.ToSlash(path), "/"), "brain/")
	for _, root := range canonicalRoots {
		if strings.HasPrefix(clean, root) {
			return true
		}
	}
	return false
}

var wikilinkRE = regexp.MustCompile(`\[\[([^\]|]+)(?:\|[^\]]+)?\]\]`)

func hasLocalAnchor(body string) bool {
	for _, m := range wikilinkRE.FindAllStringSubmatch(body, -1) {
		target := strings.TrimSpace(m[1])
		target = strings.TrimPrefix(target, "/")
		for _, prefix := range anchorPrefixes {
			if strings.HasPrefix(target, prefix) {
				return true
			}
		}
	}
	return false
}

func parseDenyList(raw string) []string {
	out := []string{}
	scanner := bufio.NewScanner(strings.NewReader(raw))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, strings.ToLower(line))
	}
	return out
}
