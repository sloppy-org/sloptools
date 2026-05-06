package brain

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// collectInnerEdits scans files inside the moved subtree for relative MD
// links pointing OUTSIDE that subtree, and recomputes them against the new
// location.
func collectInnerEdits(vault Vault, files []FileMove, fromRel, toRel string) ([]LinkEdit, error) {
	var inner []LinkEdit
	for _, f := range files {
		if f.IsDir || filepath.Ext(f.From) != ".md" {
			continue
		}
		abs := filepath.Join(vault.Root, filepath.FromSlash(f.From))
		data, err := os.ReadFile(abs)
		if err != nil {
			return nil, fmt.Errorf("brain move: read %q: %w", f.From, err)
		}
		inner = append(inner, scanInnerFile(f, fromRel, toRel, data)...)
	}
	sortEdits(inner)
	return inner, nil
}

func scanInnerFile(file FileMove, fromRel, toRel string, data []byte) []LinkEdit {
	var out []LinkEdit
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for lineNo := 1; scanner.Scan(); lineNo++ {
		original := scanner.Text()
		newLine, changed := rewriteInnerLine(file, fromRel, toRel, original)
		if !changed {
			continue
		}
		out = append(out, LinkEdit{
			Path:    file.From,
			Line:    lineNo,
			OldText: original,
			NewText: newLine,
			Kind:    "markdown",
		})
	}
	return out
}

func rewriteInnerLine(file FileMove, fromRel, toRel, line string) (string, bool) {
	changed := false
	rewritten := markdownLinkPattern.ReplaceAllStringFunc(line, func(match string) string {
		newMatch, ok := rewriteInnerMarkdownLink(file, fromRel, toRel, match)
		if !ok {
			return match
		}
		changed = true
		return newMatch
	})
	return rewritten, changed
}

func rewriteInnerMarkdownLink(file FileMove, fromRel, toRel, match string) (string, bool) {
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
	sourceDir := filepath.Dir(filepath.FromSlash(file.From))
	resolvedRel := filepath.ToSlash(filepath.Clean(filepath.Join(sourceDir, cleanTarget)))
	if pathInsideMoved(resolvedRel, fromRel) {
		// Targets inside the moved tree shift with the source; relative path
		// stays the same after both ends move, so no edit is needed.
		return "", false
	}
	newSourceRel := destinationFor(file.From, fromRel, toRel, false)
	newSourceDir := filepath.Dir(filepath.FromSlash(newSourceRel))
	newRelLink, err := filepath.Rel(filepath.FromSlash(newSourceDir), filepath.FromSlash(resolvedRel))
	if err != nil {
		return "", false
	}
	newRelLinkSlash := filepath.ToSlash(newRelLink)
	if newRelLinkSlash == filepath.ToSlash(filepath.Clean(cleanTarget)) {
		return "", false
	}
	encoded := encodeMarkdownTarget(newRelLinkSlash, rawTarget)
	rewritten := prefix + encoded
	if title != "" {
		rewritten += " " + title
	}
	rewritten += match[closeRel:]
	return rewritten, true
}

func sortEdits(edits []LinkEdit) {
	sort.Slice(edits, func(i, j int) bool {
		if edits[i].Sphere != edits[j].Sphere {
			return edits[i].Sphere < edits[j].Sphere
		}
		if edits[i].Path != edits[j].Path {
			return edits[i].Path < edits[j].Path
		}
		if edits[i].Line != edits[j].Line {
			return edits[i].Line < edits[j].Line
		}
		return edits[i].OldText < edits[j].OldText
	})
}

// canonicalDigest returns sha256 hex of the canonical encoding of plan
// (Files, Edits, Inner). Files are sorted by From; Edits and Inner are
// sorted by their natural keys.
func canonicalDigest(plan *MovePlan) string {
	files := append([]FileMove(nil), plan.Files...)
	sort.Slice(files, func(i, j int) bool { return files[i].From < files[j].From })
	edits := append([]LinkEdit(nil), plan.Edits...)
	sortEdits(edits)
	inner := append([]LinkEdit(nil), plan.Inner...)
	sort.Slice(inner, func(i, j int) bool {
		if inner[i].Path != inner[j].Path {
			return inner[i].Path < inner[j].Path
		}
		if inner[i].Line != inner[j].Line {
			return inner[i].Line < inner[j].Line
		}
		return inner[i].OldText < inner[j].OldText
	})
	payload := struct {
		Sphere      Sphere     `json:"sphere"`
		From        string     `json:"from"`
		To          string     `json:"to"`
		MergeTarget string     `json:"merge_target"`
		Files       []FileMove `json:"files"`
		Edits       []LinkEdit `json:"edits"`
		Inner       []LinkEdit `json:"inner"`
	}{plan.Sphere, plan.From, plan.To, plan.MergeTarget, files, edits, inner}
	buf, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(buf)
	return hex.EncodeToString(sum[:])
}

// inboundWarnings turns inbound link edits into one warning string each, used
// when the move is a delete so the user can see what would break.
func inboundWarnings(edits []LinkEdit) []string {
	if len(edits) == 0 {
		return nil
	}
	out := make([]string, 0, len(edits))
	for _, edit := range edits {
		out = append(out, fmt.Sprintf("inbound link from %s:%s line %d", edit.Sphere, edit.Path, edit.Line))
	}
	sort.Strings(out)
	return out
}
