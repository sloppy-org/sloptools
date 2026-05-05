package brain

import (
	"path/filepath"
	"regexp"
	"strings"
)

// Protected paths and frontmatter values. Consolidate/dream/move apply
// paths refuse to modify these. See brain/conventions/archival.md and
// note-retirement.md.

var protectedDirs = []string{
	"brain/commitments/",
	"brain/gtd/",
	"brain/glossary/",
}

var protectedStatus = map[string]bool{
	"open":        true,
	"active":      true,
	"deferred":    true,
	"waiting":     true,
	"in_progress": true,
	"in-progress": true,
	"started":     true,
}

// matches TODO markers, checkboxes, FIXME/XXX, and "deferred:" frontmatter.
var todoMarkerRe = regexp.MustCompile(`(?m)\bTODO\b|\bFIXME\b|\bXXX\b|^\s*-\s*\[[ xX]\]|^\s*deferred:`)

// IsProtectedPath returns true when the vault-relative path falls under
// brain/commitments/, brain/gtd/, or brain/glossary/.
func IsProtectedPath(vaultRel string) bool {
	clean := filepath.ToSlash(filepath.Clean(vaultRel))
	for _, dir := range protectedDirs {
		if clean == strings.TrimSuffix(dir, "/") || strings.HasPrefix(clean, dir) {
			return true
		}
	}
	return false
}

// IsProtectedStatus returns true when the frontmatter status value indicates
// open/active/deferred/waiting/in_progress/started work.
func IsProtectedStatus(status string) bool {
	return protectedStatus[strings.ToLower(strings.TrimSpace(status))]
}

// HasTODOMarkers returns true when the body contains any TODO/FIXME/XXX
// marker, a Markdown checkbox, or a "deferred:" line.
func HasTODOMarkers(body string) bool {
	return todoMarkerRe.MatchString(body)
}

// LineHasTODOMarker returns true when a single line contains a TODO marker
// or a Markdown checkbox. Used by ApplyMove --protect-todos to gate
// individual line edits.
func LineHasTODOMarker(line string) bool {
	return todoMarkerRe.MatchString(line)
}

// LineRewriteTouchesNonLinkText returns true when oldLine and newLine differ
// outside the wikilink target itself. Used by --protect-todos: if the line
// has a TODO marker, only a pure wikilink-target swap is allowed.
//
// The check is conservative: it strips every wikilink ([[...]]) from both
// lines and demands the residue is identical. If both residues match, only
// the wikilink content changed, and the rewrite is allowed.
func LineRewriteTouchesNonLinkText(oldLine, newLine string) bool {
	stripWikilinks := func(s string) string {
		return regexp.MustCompile(`\[\[[^\]]*\]\]`).ReplaceAllString(s, "")
	}
	return stripWikilinks(oldLine) != stripWikilinks(newLine)
}
