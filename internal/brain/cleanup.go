package brain

import (
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// standardCleanupIgnores lists directories the cleanup walker never descends
// into and never emits. They are repository or sync infrastructure that we
// must leave alone.
var standardCleanupIgnores = map[string]bool{
	".git":           true,
	".obsidian":      true,
	".dropbox.cache": true,
	"personal":       true,
}

// emitOnlyIgnores lists directories that are emitted as candidates and then
// not descended into. minDepth is the smallest distance from the vault root
// at which the rule fires (0 = direct child of vault root). node_modules is
// only flagged below the vault root so a vault that is itself a checked-out
// npm project does not get its own dependency tree wiped.
var emitOnlyIgnores = map[string]struct {
	reason   string
	minDepth int
}{
	".svn":         {reason: "svn", minDepth: 0},
	"__pycache__":  {reason: "pycache", minDepth: 0},
	"node_modules": {reason: "node-modules", minDepth: 1},
}

// CleanupDeadDirsScan walks the named vault and emits dead-directory
// candidates for the cleanup CLI to act on. Inbound link counts are computed
// across both configured vaults so a wikilink from the private side can still
// protect a work-side candidate.
func CleanupDeadDirsScan(cfg *Config, sphere Sphere) ([]DeadDirCandidate, error) {
	vault, err := cfgVault(cfg, sphere)
	if err != nil {
		return nil, err
	}
	candidates, err := scanDeadDirs(vault)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return candidates, nil
	}
	links, err := collectInboundLinks(cfg)
	if err != nil {
		return nil, err
	}
	for i := range candidates {
		count := countInbound(candidates[i], links)
		candidates[i].Inbound = count
		if count > 0 {
			candidates[i].Confidence = "medium"
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Sphere != candidates[j].Sphere {
			return candidates[i].Sphere < candidates[j].Sphere
		}
		return candidates[i].Path < candidates[j].Path
	})
	return candidates, nil
}

func scanDeadDirs(vault Vault) ([]DeadDirCandidate, error) {
	excludes := make(map[string]bool, len(vault.Exclude))
	for _, exclude := range vault.Exclude {
		excludes[filepath.Clean(exclude)] = true
	}
	out, claimed, err := scanNamedDeadDirs(vault, excludes)
	if err != nil {
		return nil, err
	}
	emptyCandidates, err := scanEmptyDeadDirs(vault, excludes, claimed)
	if err != nil {
		return nil, err
	}
	out = append(out, emptyCandidates...)
	return out, nil
}

// scanNamedDeadDirs covers all detection rules that key on the directory's
// own name: .svn, __pycache__, node_modules, *.old/*.bak/backup variants.
// The returned claimed set lists every candidate's vault-relative path so
// the empty-pass can ignore ancestors of claimed dirs.
func scanNamedDeadDirs(vault Vault, excludes map[string]bool) ([]DeadDirCandidate, map[string]bool, error) {
	var out []DeadDirCandidate
	claimed := map[string]bool{}
	walkErr := filepath.WalkDir(vault.Root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if path == vault.Root || !entry.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(vault.Root, path)
		if relErr != nil {
			return relErr
		}
		relSlash := filepath.ToSlash(rel)
		depth := strings.Count(relSlash, "/")
		base := entry.Name()
		if standardCleanupIgnores[base] || excludes[filepath.Clean(rel)] {
			return fs.SkipDir
		}
		if rule, ok := emitOnlyIgnores[base]; ok {
			if depth >= rule.minDepth {
				out = append(out, DeadDirCandidate{
					Sphere:     vault.Sphere,
					Path:       relSlash,
					Reason:     rule.reason,
					Confidence: "high",
				})
				claimed[relSlash] = true
			}
			return fs.SkipDir
		}
		if reason, ok := siblingReason(filepath.Dir(path), base); ok {
			out = append(out, DeadDirCandidate{
				Sphere:     vault.Sphere,
				Path:       relSlash,
				Reason:     reason,
				Confidence: "high",
			})
			claimed[relSlash] = true
			return fs.SkipDir
		}
		return nil
	})
	if walkErr != nil {
		return nil, nil, walkErr
	}
	return out, claimed, nil
}

// scanEmptyDeadDirs flags directories whose subtree contains no files. It
// stops at depth 1 since top-level folders are user-meaningful, and skips
// ancestors of named candidates so we do not mask a more specific reason.
func scanEmptyDeadDirs(vault Vault, excludes map[string]bool, claimed map[string]bool) ([]DeadDirCandidate, error) {
	var out []DeadDirCandidate
	walkErr := filepath.WalkDir(vault.Root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if path == vault.Root || !entry.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(vault.Root, path)
		if relErr != nil {
			return relErr
		}
		relSlash := filepath.ToSlash(rel)
		depth := strings.Count(relSlash, "/")
		base := entry.Name()
		if standardCleanupIgnores[base] || excludes[filepath.Clean(rel)] {
			return fs.SkipDir
		}
		if _, ok := emitOnlyIgnores[base]; ok {
			return fs.SkipDir
		}
		if claimed[relSlash] {
			return fs.SkipDir
		}
		// Top-level vault folders are user-meaningful even when momentarily
		// empty; only emit `empty` candidates from depth >= 1 (i.e. at least
		// one level inside a top-level folder).
		if depth < 1 {
			return nil
		}
		empty, emptyErr := subtreeHasNoFiles(path)
		if emptyErr != nil {
			return emptyErr
		}
		if !empty {
			return nil
		}
		if hasClaimedDescendant(claimed, relSlash) {
			return nil
		}
		out = append(out, DeadDirCandidate{
			Sphere:     vault.Sphere,
			Path:       relSlash,
			Reason:     "empty",
			Confidence: "high",
		})
		return fs.SkipDir
	})
	if walkErr != nil {
		return nil, walkErr
	}
	return out, nil
}

func hasClaimedDescendant(claimed map[string]bool, ancestor string) bool {
	prefix := ancestor + "/"
	for path := range claimed {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// siblingReason classifies a directory name as a backup/old variant when a
// live sibling without the suffix exists in the same parent. Returns the
// reason code and ok=true when the directory should be flagged.
func siblingReason(parent, name string) (string, bool) {
	stem, kind := classifyBackupName(name)
	if kind == "" || stem == "" {
		return "", false
	}
	siblingPath := filepath.Join(parent, stem)
	info, err := os.Lstat(siblingPath)
	if err != nil || !info.IsDir() {
		return "", false
	}
	if kind == "old" {
		return "old-with-live-sibling", true
	}
	return "bak-with-live-sibling", true
}

func classifyBackupName(name string) (string, string) {
	lower := strings.ToLower(name)
	for _, suffix := range []string{".old2", ".old", ".bak", ".backup"} {
		if strings.HasSuffix(lower, suffix) && len(lower) > len(suffix) {
			stem := name[:len(name)-len(suffix)]
			if suffix == ".bak" || suffix == ".backup" {
				return stem, "bak"
			}
			return stem, "old"
		}
	}
	if strings.HasPrefix(lower, "backup") && len(lower) > len("backup") {
		stem := name[len("backup"):]
		stem = strings.TrimLeft(stem, "._-")
		if stem != "" {
			return stem, "bak"
		}
	}
	if strings.HasSuffix(lower, "backup") && len(lower) > len("backup") {
		stem := name[:len(name)-len("backup")]
		stem = strings.TrimRight(stem, "._-")
		if stem != "" {
			return stem, "bak"
		}
	}
	return "", ""
}

// subtreeHasNoFiles reports whether the subtree rooted at root contains zero
// non-directory entries. Empty subdirectories do not count as "files".
func subtreeHasNoFiles(root string) (bool, error) {
	empty := true
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		if !entry.IsDir() {
			empty = false
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	return empty, nil
}

// collectInboundLinks walks every configured vault and returns the set of
// brain-relative paths referenced by wikilinks and resolved Markdown links.
// Entries are stored as forward-slash paths without a `.md` suffix.
type inboundIndex struct {
	bySphere map[Sphere][]string
}

func collectInboundLinks(cfg *Config) (*inboundIndex, error) {
	index := &inboundIndex{bySphere: map[Sphere][]string{}}
	if cfg == nil {
		return index, nil
	}
	for _, vault := range cfg.Vaults {
		err := WalkVaultNotes(cfg, vault.Sphere, func(snap NoteSnapshot) error {
			noteDir := filepath.Dir(snap.Source.Path)
			for _, link := range extractWikilinks(snap.Body) {
				target := normalizeWikilinkTarget(link)
				if target == "" {
					continue
				}
				index.bySphere[vault.Sphere] = append(index.bySphere[vault.Sphere], target)
			}
			for _, link := range extractMarkdownLinks(snap.Body) {
				if rel, ok := resolveRelativeLink(vault, noteDir, link); ok {
					index.bySphere[vault.Sphere] = append(index.bySphere[vault.Sphere], rel)
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return index, nil
}

// normalizeWikilinkTarget strips alias and section parts from a wikilink
// target and returns a forward-slash brain-relative path without `.md`.
func normalizeWikilinkTarget(raw string) string {
	target := strings.TrimSpace(raw)
	if pipe := strings.Index(target, "|"); pipe >= 0 {
		target = target[:pipe]
	}
	if hash := strings.Index(target, "#"); hash >= 0 {
		target = target[:hash]
	}
	target = strings.TrimSpace(target)
	target = strings.TrimSuffix(target, ".md")
	target = strings.Trim(target, "/")
	return target
}

// resolveRelativeLink resolves a relative Markdown link against the note's
// directory and returns its vault-relative forward-slash path. Returns ok=false
// when the link is external or escapes the vault.
func resolveRelativeLink(vault Vault, noteDir, raw string) (string, bool) {
	target := strings.TrimSpace(raw)
	if target == "" || hasURLScheme(target) {
		return "", false
	}
	if hash := strings.Index(target, "#"); hash >= 0 {
		target = target[:hash]
	}
	if target == "" {
		return "", false
	}
	if unescaped, err := url.PathUnescape(target); err == nil {
		target = unescaped
	}
	target = filepath.FromSlash(target)
	var absolute string
	if filepath.IsAbs(target) {
		absolute = target
	} else {
		absolute = filepath.Join(noteDir, target)
	}
	clean := filepath.Clean(absolute)
	if !isWithin(vault.Root, clean) {
		return "", false
	}
	rel, err := filepath.Rel(vault.Root, clean)
	if err != nil {
		return "", false
	}
	rel = filepath.ToSlash(rel)
	rel = strings.TrimSuffix(rel, ".md")
	return rel, true
}

func countInbound(candidate DeadDirCandidate, index *inboundIndex) int {
	if index == nil {
		return 0
	}
	prefix := strings.TrimSuffix(candidate.Path, "/")
	brainPrefix := stripBrainPrefix(prefix)
	count := 0
	for _, links := range index.bySphere {
		for _, link := range links {
			if linkMatchesPath(link, prefix, brainPrefix) {
				count++
			}
		}
	}
	return count
}

// stripBrainPrefix returns the directory path with the leading `brain/`
// segment removed. Wikilinks are typically written relative to the brain
// root, while DeadDirCandidate.Path is relative to the vault root.
func stripBrainPrefix(path string) string {
	if path == "brain" {
		return ""
	}
	if strings.HasPrefix(path, "brain/") {
		return strings.TrimPrefix(path, "brain/")
	}
	return path
}

func linkMatchesPath(link, vaultRel, brainRel string) bool {
	if link == "" {
		return false
	}
	if vaultRel != "" {
		if link == vaultRel || strings.HasPrefix(link, vaultRel+"/") {
			return true
		}
	}
	if brainRel != "" {
		if link == brainRel || strings.HasPrefix(link, brainRel+"/") {
			return true
		}
	}
	return false
}
