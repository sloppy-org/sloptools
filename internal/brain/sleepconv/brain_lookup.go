package sleepconv

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// BrainIndex is the in-memory map of canonical brain notes resolvable
// for a sleep run. Built once per Activity render to amortize the
// directory walk; rebuilt when the brain repo changes between runs.
type BrainIndex struct {
	root string
	// byKind[kind][lower-name] = brain-relative path
	byKind map[string]map[string]string
	// folderNotes maps a vault-relative source-folder path to its
	// brain/folders/<...>.md note path, when one exists.
	folderNotes map[string]string
}

// NewBrainIndex walks brain/people/, projects/, institutions/, topics/,
// glossary/, commitments/, and folders/ once.
func NewBrainIndex(brainRoot string) *BrainIndex {
	idx := &BrainIndex{
		root:        brainRoot,
		byKind:      map[string]map[string]string{},
		folderNotes: map[string]string{},
	}
	if brainRoot == "" {
		return idx
	}
	for _, kind := range []string{"people", "projects", "institutions", "topics", "glossary", "commitments"} {
		idx.byKind[kind] = map[string]string{}
		dir := filepath.Join(brainRoot, kind)
		_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".md") {
				return nil
			}
			name := strings.TrimSuffix(filepath.Base(path), ".md")
			rel, _ := filepath.Rel(brainRoot, path)
			idx.byKind[kind][strings.ToLower(name)] = filepath.ToSlash(rel)
			return nil
		})
	}
	folders := filepath.Join(brainRoot, "folders")
	_ = filepath.WalkDir(folders, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".md") {
			return nil
		}
		rel, _ := filepath.Rel(brainRoot, path)
		// brain/folders/<vault-relative>.md → vault-relative key.
		// e.g. brain/folders/code/sloppy/sloptools.md → code/sloppy/sloptools
		key := strings.TrimSuffix(filepath.ToSlash(rel), ".md")
		key = strings.TrimPrefix(key, "folders/")
		idx.folderNotes[key] = filepath.ToSlash(rel)
		return nil
	})
	return idx
}

// LookupName returns the canonical brain-relative path for a name (and
// its kind) when one exists. Tries an exact lower-case filename match
// across the entity dirs in deterministic order: people, projects,
// institutions, topics, glossary, commitments.
func (idx *BrainIndex) LookupName(name string) (path, kind string) {
	low := strings.ToLower(strings.TrimSpace(name))
	if low == "" {
		return "", ""
	}
	for _, k := range []string{"people", "projects", "institutions", "topics", "glossary", "commitments"} {
		if p, ok := idx.byKind[k][low]; ok {
			return p, k
		}
	}
	return "", ""
}

// LookupFolder maps an absolute filesystem path to the brain folder
// note that governs it, climbing the directory tree until a matching
// note is found or the vault root is exited. Returns "" when no
// folder note corresponds to any ancestor of the path.
//
// vaultRoots is the set of vault roots the path may live under
// (e.g. "/home/ert/Nextcloud", "/home/ert/code", "/home/ert/data").
// The brain's folder-note keys are vault-relative, so we strip the
// matching root before walking parents.
func (idx *BrainIndex) LookupFolder(absPath string, vaultRoots []string) string {
	if absPath == "" || len(idx.folderNotes) == 0 {
		return ""
	}
	for _, root := range vaultRoots {
		root = strings.TrimRight(root, "/")
		prefix := root + "/"
		if !strings.HasPrefix(absPath, prefix) && absPath != root {
			continue
		}
		rel := strings.TrimPrefix(absPath, prefix)
		rel = strings.TrimSuffix(rel, "/")
		// Walk up the path components looking for a folder note.
		for {
			if note, ok := idx.folderNotes[rel]; ok {
				return note
			}
			i := strings.LastIndex(rel, "/")
			if i < 0 {
				break
			}
			rel = rel[:i]
		}
	}
	return ""
}

// LookupRepoProject maps a path under ~/code/<org>/<repo>/... to the
// candidate brain/projects/<repo>.md note when one exists. Returns
// the brain-relative path or "" if the path doesn't fit the repo
// pattern or there's no project note.
var codePathRE = regexp.MustCompile(`^/[^/]+/[^/]+/code/([^/]+)/([^/]+)(?:/|$)`)

func (idx *BrainIndex) LookupRepoProject(absPath string) string {
	m := codePathRE.FindStringSubmatch(absPath)
	if m == nil {
		return ""
	}
	repo := m[2]
	if path, kind := idx.LookupName(repo); kind == "projects" {
		return path
	}
	return ""
}

// EnrichFiles attaches a BrainHit to every FileTouch by trying first
// the project-name match (via repo dir) then the folder-note ancestry
// walk. vaultRoots is supplied by the caller so this package doesn't
// need to know the user's $HOME layout.
func (idx *BrainIndex) EnrichFiles(files []FileTouch, vaultRoots []string) []FileTouch {
	for i := range files {
		if hit := idx.LookupRepoProject(files[i].Path); hit != "" {
			files[i].BrainHit = hit
			continue
		}
		if hit := idx.LookupFolder(files[i].Path, vaultRoots); hit != "" {
			files[i].BrainHit = hit
		}
	}
	return files
}

// VaultRootsForHome returns the vault roots a brain folder index
// expects when looking up file paths. Mirrors the routing layer in
// CLAUDE.md: Nextcloud is the work vault, Dropbox the private vault.
func VaultRootsForHome(home string) []string {
	if home == "" {
		return nil
	}
	return []string{
		filepath.Join(home, "Nextcloud"),
		filepath.Join(home, "Dropbox"),
		filepath.Join(home, "code"),
		filepath.Join(home, "data"),
	}
}

// PreferredEntityNames extracts a list of {name, kind, path} hits the
// brain already knows about, used for cross-referencing the day's
// activity. Cheap precomputation for the renderer.
func (idx *BrainIndex) PreferredEntityNames(kind string) []string {
	if idx == nil {
		return nil
	}
	m, ok := idx.byKind[kind]
	if !ok {
		return nil
	}
	out := make([]string, 0, len(m))
	for name := range m {
		out = append(out, name)
	}
	return out
}

// LooksLikeBrainPath reports whether an absolute path is inside one of
// the canonical brain trees. Used to mark brain-self-reads as origin
// "brain" so they don't get extracted as new candidate facts (the
// anti-feedback rule from the mem0 audit).
func LooksLikeBrainPath(absPath, home string) bool {
	if absPath == "" || home == "" {
		return false
	}
	for _, root := range []string{
		filepath.Join(home, "Nextcloud", "brain"),
		filepath.Join(home, "Dropbox", "brain"),
	} {
		if absPath == root || strings.HasPrefix(absPath, root+"/") {
			return true
		}
	}
	return false
}

// EnsureBrainExists is a tiny convenience for tests that build a
// fixture brain dir.
func EnsureBrainExists(brainRoot string) error {
	return os.MkdirAll(brainRoot, 0o755)
}
