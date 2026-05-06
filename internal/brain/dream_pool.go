package brain

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// loadDreamPool loads every topics/* and projects/* note in the vault
// (excluding personal/), populating both an ordered slice (sorted by rel)
// and a rel -> *dreamNote map.
func loadDreamPool(vault Vault) ([]*dreamNote, map[string]*dreamNote, error) {
	notes, byRel, err := walkBrainNotes(vault, func(rel string) bool {
		for _, prefix := range dreamPoolPrefixes {
			if strings.HasPrefix(rel, prefix) {
				return true
			}
		}
		return false
	})
	if err != nil {
		return nil, nil, err
	}
	return notes, byRel, nil
}

// loadAllBrainNotes loads every .md note in the vault, excluding personal/
// and the configured vault excludes.
func loadAllBrainNotes(vault Vault) ([]*dreamNote, map[string]*dreamNote, error) {
	return walkBrainNotes(vault, func(string) bool { return true })
}

func walkBrainNotes(vault Vault, accept func(rel string) bool) ([]*dreamNote, map[string]*dreamNote, error) {
	brainRoot := vault.BrainRoot()
	excludes := append([]string{"personal"}, vault.Exclude...)
	excludeSet := map[string]bool{}
	for _, exclude := range excludes {
		excludeSet[filepath.Clean(exclude)] = true
	}

	gitTouch, _ := gitFileTouchMap(brainRoot)

	var ordered []*dreamNote
	byRel := map[string]*dreamNote{}
	err := filepath.WalkDir(brainRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		rel, err := filepath.Rel(vault.Root, path)
		if err != nil {
			return err
		}
		relSlash := filepath.ToSlash(rel)
		if d.IsDir() {
			if excludeSet[filepath.Clean(rel)] {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".md" {
			return nil
		}
		// Exclude personal/ at any depth and any vault-excluded prefix.
		for excluded := range excludeSet {
			if relSlash == excluded || strings.HasPrefix(relSlash, excluded+"/") {
				return nil
			}
		}
		brainRel, err := filepath.Rel(brainRoot, path)
		if err != nil {
			return err
		}
		brainRelSlash := filepath.ToSlash(brainRel)
		if !accept(brainRelSlash) {
			return nil
		}
		// Protected: skip commitments/, gtd/, glossary/ and TODO-bearing notes.
		if IsProtectedPath("brain/" + brainRelSlash) {
			return nil
		}
		note, err := loadDreamNote(path, brainRelSlash)
		if err != nil {
			return err
		}
		if HasTODOMarkers(note.body) {
			return nil
		}
		if t, ok := gitTouch[brainRelSlash]; ok {
			note.gitTouch = t
		}
		ordered = append(ordered, note)
		byRel[brainRelSlash] = note
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].rel < ordered[j].rel })
	return ordered, byRel, nil
}

func loadDreamNote(abs, brainRel string) (*dreamNote, error) {
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	stat, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	body := string(data)
	note, _ := ParseMarkdownNote(body, MarkdownParseOptions{})
	dn := &dreamNote{
		rel:         brainRel,
		abs:         abs,
		displayName: scalarField(note, "display_name"),
		stem:        strings.TrimSuffix(filepath.Base(brainRel), ".md"),
		strategic:   boolField(note, "strategic"),
		focus:       scalarField(note, "focus"),
		cadence:     scalarField(note, "cadence"),
		lastSeen:    scalarField(note, "last_seen"),
		mtime:       stat.ModTime(),
		body:        body,
	}
	for _, raw := range extractWikilinks(body) {
		dn.wikilinks = append(dn.wikilinks, parseDreamWikilink(raw))
	}
	return dn, nil
}

// parseDreamWikilink turns the raw text inside [[...]] into a structured
// record. Targets are normalised to slash form with a ".md" suffix; an
// empty target is preserved as "".
func parseDreamWikilink(raw string) dreamWikilink {
	link := dreamWikilink{raw: raw}
	rest, alias := raw, ""
	if idx := strings.Index(raw, "|"); idx >= 0 {
		rest = raw[:idx]
		alias = strings.TrimSpace(raw[idx+1:])
	}
	if idx := strings.Index(rest, "#"); idx >= 0 {
		rest = rest[:idx]
	}
	target := strings.TrimSpace(rest)
	link.alias = alias
	if target == "" {
		return link
	}
	target = filepath.ToSlash(target)
	if !strings.HasSuffix(target, ".md") {
		target += ".md"
	}
	link.target = target
	return link
}

// stripFrontMatter returns the body with the leading YAML frontmatter
// fence removed. Mentions inside frontmatter (display_name, etc.) must
// not generate cross-link suggestions back to the same note.
func stripFrontMatter(body string) string {
	trimmed := strings.TrimLeft(body, " \t\r\n")
	if !strings.HasPrefix(trimmed, "---") {
		return body
	}
	rest := trimmed[3:]
	if idx := strings.Index(rest, "\n---"); idx >= 0 {
		tail := rest[idx+4:]
		return strings.TrimLeft(tail, "\r\n")
	}
	return body
}

// vaultBrainRel returns the slash-form relative path from the vault root to
// the brain root. For the default `brain` brain dir this is just "brain".
func vaultBrainRel(vault Vault) string {
	rel, err := filepath.Rel(vault.Root, vault.BrainRoot())
	if err != nil {
		return strings.TrimSuffix(vault.Brain, "/")
	}
	return filepath.ToSlash(rel)
}
