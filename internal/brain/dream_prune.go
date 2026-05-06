package brain

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DreamPruneApplySummary is the JSON-shaped result of a successful apply.
type DreamPruneApplySummary struct {
	Sphere      Sphere     `json:"sphere"`
	Applied     int        `json:"applied"`
	FilesEdited int        `json:"files_edited"`
	EditedPaths []string   `json:"edited_paths"`
	Digest      string     `json:"digest"`
	Cold        []ColdLink `json:"cold"`
}

// BuildDreamPrunePlan turns a cold-link list into a deterministic MovePlan
// whose Edits degrade each [[Target]] / [[Target|alias]] occurrence to
// plain text. Files is empty; From and To stay empty because no file moves.
func BuildDreamPrunePlan(cfg *Config, sphere Sphere, cold []ColdLink) (*MovePlan, error) {
	vault, err := cfgVault(cfg, sphere)
	if err != nil {
		return nil, err
	}
	if len(cold) == 0 {
		plan := &MovePlan{Sphere: vault.Sphere}
		plan.Digest = canonicalDigest(plan)
		return plan, nil
	}

	// Group by brain-relative source so we read each source note exactly
	// once. The LinkEdit path must be vault-relative for applyEdits.
	bySource := map[string][]ColdLink{}
	for _, item := range cold {
		bySource[item.Source] = append(bySource[item.Source], item)
	}
	brainPrefix := vaultBrainRel(vault)

	var edits []LinkEdit
	for source, items := range bySource {
		abs := filepath.Join(vault.BrainRoot(), filepath.FromSlash(source))
		data, err := os.ReadFile(abs)
		if err != nil {
			return nil, fmt.Errorf("brain dream: read %q: %w", source, err)
		}
		vaultRel := filepath.ToSlash(filepath.Join(brainPrefix, source))
		fileEdits, err := buildPruneEditsForFile(vault.Sphere, vaultRel, string(data), items)
		if err != nil {
			return nil, err
		}
		edits = append(edits, fileEdits...)
	}

	sortEdits(edits)
	plan := &MovePlan{
		Sphere: vault.Sphere,
		Edits:  edits,
	}
	plan.Digest = canonicalDigest(plan)
	return plan, nil
}

// DreamPruneLinksApply re-scans cold links, re-derives the synthetic
// prune plan, refuses to run when confirm does not match the fresh digest,
// and applies the edits in place. Returns a summary suitable for JSON
// emission.
func DreamPruneLinksApply(cfg *Config, sphere Sphere, confirm string) (*DreamPruneApplySummary, error) {
	cold, err := DreamPruneLinksScan(cfg, sphere)
	if err != nil {
		return nil, fmt.Errorf("brain dream: scan: %w", err)
	}
	plan, err := BuildDreamPrunePlan(cfg, sphere, cold)
	if err != nil {
		return nil, fmt.Errorf("brain dream: build plan: %w", err)
	}
	if confirm != plan.Digest {
		return nil, fmt.Errorf("brain dream: confirm digest %q does not match fresh plan digest %q", confirm, plan.Digest)
	}
	if err := applyEdits(cfg, plan.Edits); err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	editedPaths := []string{}
	for _, edit := range plan.Edits {
		key := string(edit.Sphere) + "\x00" + edit.Path
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		editedPaths = append(editedPaths, edit.Path)
	}
	sort.Strings(editedPaths)
	return &DreamPruneApplySummary{
		Sphere:      plan.Sphere,
		Applied:     len(plan.Edits),
		FilesEdited: len(editedPaths),
		EditedPaths: editedPaths,
		Digest:      plan.Digest,
		Cold:        cold,
	}, nil
}

// buildPruneEditsForFile constructs LinkEdit records for each wikilink
// occurrence in the source body. Multiple matches on the same line are
// degraded together so the resulting plan keeps one LinkEdit per line.
func buildPruneEditsForFile(sphere Sphere, source, body string, items []ColdLink) ([]LinkEdit, error) {
	wantTargets := map[string][]ColdLink{}
	for _, item := range items {
		wantTargets[item.Target] = append(wantTargets[item.Target], item)
	}
	type pendingEdit struct {
		line int
		old  string
		new  string
	}
	var pending []pendingEdit
	for i, line := range strings.Split(body, "\n") {
		newLine, changed := degradeWikilinksOnLine(line, wantTargets)
		if changed {
			pending = append(pending, pendingEdit{line: i + 1, old: line, new: newLine})
		}
	}
	if len(pending) == 0 {
		return nil, fmt.Errorf("brain dream: no matching wikilinks found in %q", source)
	}
	out := make([]LinkEdit, 0, len(pending))
	for _, p := range pending {
		out = append(out, LinkEdit{
			Path:    source,
			Sphere:  sphere,
			Line:    p.line,
			OldText: p.old,
			NewText: p.new,
			Kind:    "wikilink",
		})
	}
	return out, nil
}

// degradeWikilinksOnLine rewrites every [[...]] occurrence on a line whose
// target is in wantTargets, replacing it with the alias or basename.
// Returns the rewritten line and a changed flag.
func degradeWikilinksOnLine(line string, wantTargets map[string][]ColdLink) (string, bool) {
	matches := noteWikilinkPattern.FindAllStringSubmatchIndex(line, -1)
	if len(matches) == 0 {
		return line, false
	}
	newLine := line
	changed := false
	// Walk matches in reverse so byte offsets stay valid as we splice.
	for m := len(matches) - 1; m >= 0; m-- {
		start, end := matches[m][0], matches[m][1]
		contentStart, contentEnd := matches[m][2], matches[m][3]
		raw := line[contentStart:contentEnd]
		link := parseDreamWikilink(raw)
		if _, ok := wantTargets[link.target]; !ok {
			continue
		}
		newLine = newLine[:start] + pruneReplacement(link) + newLine[end:]
		changed = true
	}
	return newLine, changed
}

// pruneReplacement returns the plain-text replacement for a degraded
// wikilink: the alias when present, otherwise the basename of the target.
func pruneReplacement(link dreamWikilink) string {
	if link.alias != "" {
		return link.alias
	}
	target := link.target
	if target == "" {
		return link.raw
	}
	base := filepath.Base(target)
	return strings.TrimSuffix(base, ".md")
}
