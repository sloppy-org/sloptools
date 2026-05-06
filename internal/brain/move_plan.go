package brain

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// standardMoveExcludes is applied in addition to vault-configured Exclude.
var standardMoveExcludes = []string{".git", ".obsidian", "node_modules"}

// normalizeMoveSide resolves a vault-relative path within the given vault and
// rejects paths inside personal/. Returns vault-relative slash path and the
// absolute path. The path need not exist (destination usually does not).
func normalizeMoveSide(vault Vault, raw, side string) (string, string, error) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return "", "", fmt.Errorf("brain move: %s path is required", side)
	}
	if filepath.IsAbs(clean) {
		abs := filepath.Clean(clean)
		if !isWithin(vault.Root, abs) {
			return "", "", fmt.Errorf("brain move: %s path %q is outside vault %s", side, clean, vault.Sphere)
		}
		rel, err := filepath.Rel(vault.Root, abs)
		if err != nil {
			return "", "", fmt.Errorf("brain move: %s path %q: %w", side, clean, err)
		}
		clean = rel
	}
	clean = filepath.Clean(clean)
	if clean == "." || clean == "" || strings.HasPrefix(clean, "..") {
		return "", "", fmt.Errorf("brain move: %s path %q resolves outside vault", side, raw)
	}
	relSlash := filepath.ToSlash(clean)
	if relSlash == "personal" || strings.HasPrefix(relSlash, "personal/") {
		return "", "", fmt.Errorf("brain move: %s path %q is inside personal/ (off limits)", side, raw)
	}
	if side == "from" && IsProtectedPath(relSlash) {
		return "", "", fmt.Errorf("brain move: %s path %q is inside a protected brain area (commitments/gtd/glossary)", side, raw)
	}
	abs := filepath.Join(vault.Root, clean)
	if !isWithin(vault.Root, abs) {
		return "", "", fmt.Errorf("brain move: %s path %q is outside vault %s", side, raw, vault.Sphere)
	}
	return relSlash, abs, nil
}

// collectFileMoves walks the source and emits a stable list of file/dir moves.
func collectFileMoves(vault Vault, fromRel, fromAbs, toRel string, deleting bool) ([]FileMove, error) {
	info, err := os.Lstat(fromAbs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("brain move: stat %q: %w", fromRel, err)
	}
	var moves []FileMove
	if !info.IsDir() {
		moves = append(moves, FileMove{From: fromRel, To: destinationFor(fromRel, fromRel, toRel, deleting), IsDir: false})
		return moves, nil
	}
	if err := filepath.WalkDir(fromAbs, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(vault.Root, path)
		if err != nil {
			return err
		}
		relSlash := filepath.ToSlash(rel)
		if d.IsDir() && path != fromAbs && shouldSkipMoveDir(d.Name()) {
			return filepath.SkipDir
		}
		moves = append(moves, FileMove{
			From:  relSlash,
			To:    destinationFor(relSlash, fromRel, toRel, deleting),
			IsDir: d.IsDir(),
		})
		return nil
	}); err != nil {
		return nil, err
	}
	return moves, nil
}

func shouldSkipMoveDir(name string) bool {
	for _, skip := range standardMoveExcludes {
		if name == skip {
			return true
		}
	}
	return false
}

// destinationFor maps an item under fromRel to its destination under toRel.
func destinationFor(itemRel, fromRel, toRel string, deleting bool) string {
	if deleting {
		return ""
	}
	if itemRel == fromRel {
		return toRel
	}
	suffix := strings.TrimPrefix(itemRel, fromRel+"/")
	return filepath.ToSlash(filepath.Join(toRel, suffix))
}

// makeMovedSet returns the set of vault-relative file paths in the moved tree.
func makeMovedSet(files []FileMove) map[string]string {
	out := make(map[string]string, len(files))
	for _, f := range files {
		out[f.From] = f.To
	}
	return out
}

// collectInboundEdits walks every configured vault and gathers wikilink and
// markdown link rewrites in files that are NOT being moved.
func collectInboundEdits(cfg *Config, srcVault Vault, fromRel, toRel string, deleting bool, movedSet map[string]string) ([]LinkEdit, error) {
	var edits []LinkEdit
	for _, vault := range cfg.Vaults {
		vaultEdits, err := collectVaultInboundEdits(vault, srcVault, fromRel, toRel, deleting, movedSet)
		if err != nil {
			return nil, err
		}
		edits = append(edits, vaultEdits...)
	}
	sortEdits(edits)
	return edits, nil
}

func collectVaultInboundEdits(vault, srcVault Vault, fromRel, toRel string, deleting bool, movedSet map[string]string) ([]LinkEdit, error) {
	var edits []LinkEdit
	err := filepath.WalkDir(vault.Root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, fs.ErrNotExist) {
				return nil
			}
			return walkErr
		}
		if d.IsDir() {
			if shouldSkipWalkDir(vault, path, d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".md" {
			return nil
		}
		rel, err := filepath.Rel(vault.Root, path)
		if err != nil {
			return err
		}
		relSlash := filepath.ToSlash(rel)
		if vault.Sphere == srcVault.Sphere {
			if _, moved := movedSet[relSlash]; moved {
				return nil
			}
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		fileEdits := scanInboundFile(vault, srcVault, relSlash, data, fromRel, toRel, deleting)
		edits = append(edits, fileEdits...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return edits, nil
}

func shouldSkipWalkDir(vault Vault, absPath, name string) bool {
	for _, skip := range standardMoveExcludes {
		if name == skip {
			return true
		}
	}
	rel, err := filepath.Rel(vault.Root, absPath)
	if err != nil {
		return false
	}
	relSlash := filepath.ToSlash(filepath.Clean(rel))
	if vault.Sphere == SphereWork && (relSlash == "personal" || strings.HasPrefix(relSlash, "personal/")) {
		return true
	}
	for _, exclude := range vault.Exclude {
		excludeSlash := filepath.ToSlash(filepath.Clean(exclude))
		if relSlash == excludeSlash || strings.HasPrefix(relSlash, excludeSlash+"/") {
			return true
		}
	}
	return false
}

// scanInboundFile finds wikilinks and relative markdown links pointing into
// the moved subtree and emits LinkEdit rewrites. Cross-vault wikilinks count;
// markdown links only resolve within the same vault as the source.
func scanInboundFile(vault, srcVault Vault, relSlash string, data []byte, fromRel, toRel string, deleting bool) []LinkEdit {
	var out []LinkEdit
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	sameSphere := vault.Sphere == srcVault.Sphere
	for lineNo := 1; scanner.Scan(); lineNo++ {
		original := scanner.Text()
		newLine, kind, changed := rewriteLineInbound(relSlash, original, fromRel, toRel, deleting, sameSphere)
		if !changed {
			continue
		}
		out = append(out, LinkEdit{
			Path:    relSlash,
			Sphere:  vault.Sphere,
			Line:    lineNo,
			OldText: original,
			NewText: newLine,
			Kind:    kind,
		})
	}
	return out
}

func rewriteLineInbound(relSlash, line, fromRel, toRel string, deleting, sameSphere bool) (string, string, bool) {
	current := line
	wikiChanged := false
	mdChanged := false

	current = wikilinkPattern.ReplaceAllStringFunc(current, func(match string) string {
		inner := match[2 : len(match)-2]
		rewritten, ok := rewriteWikilink(inner, fromRel, toRel, deleting)
		if !ok {
			return match
		}
		wikiChanged = true
		return "[[" + rewritten + "]]"
	})

	if sameSphere {
		current = markdownLinkPattern.ReplaceAllStringFunc(current, func(match string) string {
			rewritten, ok := rewriteMarkdownLink(relSlash, match, fromRel, toRel, deleting)
			if !ok {
				return match
			}
			mdChanged = true
			return rewritten
		})
	}

	if !wikiChanged && !mdChanged {
		return line, "", false
	}
	switch {
	case wikiChanged && mdChanged:
		return current, "wikilink+markdown", true
	case mdChanged:
		return current, "markdown", true
	default:
		return current, "wikilink", true
	}
}

// rewriteWikilink rewrites a wikilink body when its target falls under
// fromRel. Wikilinks may be expressed vault-relative ("brain/projects/old"),
// brain-relative ("projects/old"), or with a trailing ".md".
func rewriteWikilink(body, fromRel, toRel string, deleting bool) (string, bool) {
	target, alias, anchor := splitWikilink(body)
	cleanTarget := strings.TrimSpace(target)
	if cleanTarget == "" || hasURLScheme(cleanTarget) {
		return "", false
	}
	normalized := strings.TrimSuffix(filepath.ToSlash(cleanTarget), ".md")
	suffix, matched := matchWikilink(normalized, wikilinkForms(fromRel))
	if !matched {
		return "", false
	}
	if deleting {
		display := alias
		if display == "" {
			display = filepath.Base(strings.TrimSuffix(fromRel, ".md"))
		}
		return display, true
	}
	toForms := wikilinkForms(toRel)
	newBase := toForms[0]
	if !strings.HasPrefix(normalized, "brain/") && len(toForms) > 1 {
		newBase = toForms[1]
	}
	newTarget := newBase
	if suffix != "" {
		newTarget = newBase + "/" + suffix
	}
	if strings.HasSuffix(cleanTarget, ".md") {
		newTarget += ".md"
	}
	out := newTarget
	if anchor != "" {
		out += "#" + anchor
	}
	if alias != "" {
		out += "|" + alias
	}
	return out, true
}

// wikilinkForms returns vault-relative and brain-relative no-extension forms
// for a vault-relative path. The first element is the canonical form.
func wikilinkForms(rel string) []string {
	rel = strings.TrimSuffix(filepath.ToSlash(rel), ".md")
	out := []string{rel}
	if strings.HasPrefix(rel, "brain/") {
		out = append(out, strings.TrimPrefix(rel, "brain/"))
	}
	return out
}

func matchWikilink(normalized string, forms []string) (string, bool) {
	for _, form := range forms {
		if normalized == form {
			return "", true
		}
		if strings.HasPrefix(normalized, form+"/") {
			return strings.TrimPrefix(normalized, form+"/"), true
		}
	}
	return "", false
}

func splitWikilink(body string) (target, alias, anchor string) {
	rest := body
	if i := strings.Index(rest, "|"); i >= 0 {
		alias = strings.TrimSpace(rest[i+1:])
		rest = rest[:i]
	}
	if i := strings.Index(rest, "#"); i >= 0 {
		anchor = strings.TrimSpace(rest[i+1:])
		rest = rest[:i]
	}
	return strings.TrimSpace(rest), alias, anchor
}

// rewriteMarkdownLink rewrites a `[text](path)` link when the resolved target
// is at-or-under fromRel. Same-vault only.
func rewriteMarkdownLink(sourceRel, match, fromRel, toRel string, deleting bool) (string, bool) {
	open := strings.Index(match, "](")
	if open < 0 {
		return "", false
	}
	prefix := match[:open+2]
	closeRel := strings.LastIndex(match, ")")
	if closeRel <= open+2 {
		return "", false
	}
	inside := match[open+2 : closeRel]
	rawTarget, title := splitMarkdownTarget(inside)
	cleanTarget, err := cleanLinkTarget(rawTarget)
	if err != nil || cleanTarget == "" {
		return "", false
	}
	if filepath.IsAbs(cleanTarget) {
		return "", false
	}
	sourceDir := filepath.Dir(filepath.FromSlash(sourceRel))
	resolvedRel := filepath.ToSlash(filepath.Clean(filepath.Join(sourceDir, cleanTarget)))
	if !pathInsideMoved(resolvedRel, fromRel) {
		return "", false
	}
	if deleting {
		return "", false
	}
	suffix := strings.TrimPrefix(resolvedRel, fromRel)
	suffix = strings.TrimPrefix(suffix, "/")
	newTargetRel := toRel
	if suffix != "" {
		newTargetRel = filepath.ToSlash(filepath.Join(toRel, suffix))
	}
	newRelLink, err := filepath.Rel(filepath.FromSlash(sourceDir), filepath.FromSlash(newTargetRel))
	if err != nil {
		return "", false
	}
	encoded := encodeMarkdownTarget(filepath.ToSlash(newRelLink), rawTarget)
	rewritten := prefix + encoded
	if title != "" {
		rewritten += " " + title
	}
	rewritten += match[closeRel:]
	return rewritten, true
}

func pathInsideMoved(target, fromRel string) bool {
	if target == fromRel {
		return true
	}
	return strings.HasPrefix(target, fromRel+"/")
}

func splitMarkdownTarget(inside string) (target, title string) {
	idx := -1
	for i, r := range inside {
		if r == ' ' || r == '\t' {
			idx = i
			break
		}
	}
	if idx < 0 {
		return inside, ""
	}
	return inside[:idx], strings.TrimSpace(inside[idx:])
}

// encodeMarkdownTarget mirrors URL-escaping behaviour seen in the original
// link, falling back to a plain path when the original used no escaping.
func encodeMarkdownTarget(rel, original string) string {
	if !strings.Contains(original, "%") {
		return rel
	}
	segments := strings.Split(rel, "/")
	for i, seg := range segments {
		segments[i] = url.PathEscape(seg)
	}
	return strings.Join(segments, "/")
}
