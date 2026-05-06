package brain

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// outcomeOrder is the canonical sort order for consolidate rows.
var outcomeOrder = map[ConsolidateOutcome]int{
	OutcomeDelete:      0,
	OutcomeArchive:     1,
	OutcomeRetire:      2,
	OutcomeConsolidate: 3,
	OutcomeDemote:      4,
	OutcomeKeep:        5,
}

// scannedNote captures the per-note state needed by the consolidation passes.
type scannedNote struct {
	source      ResolvedPath
	body        string
	note        *MarkdownNote
	kind        string
	focus       string
	status      string
	displayName string
	canonical   string
	aliases     []string
	wikilinks   []string
	inbound     int
	mtime       time.Time
	opened      string
	commitments bool
	bodyChars   int
}

// brainScan collects every note under brain/ for the consolidation passes.
type brainScan struct {
	notes       []*scannedNote
	byKey       map[string]*scannedNote
	commitments map[string]bool
}

// consolidationRoots enumerates the folders the planner cares about.
var consolidationRoots = []string{
	"brain/folders",
	"brain/people",
	"brain/projects",
	"brain/topics",
	"brain/institutions",
	"brain/glossary",
}

// ConsolidatePlan scans the configured vault and emits the Phase 6 queue.
func ConsolidatePlan(cfg *Config, sphere Sphere) ([]ConsolidateRow, error) {
	scan, err := scanBrain(cfg, sphere)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	var rows []ConsolidateRow
	rows = append(rows, orphanRows(scan, sphere, now)...)
	rows = append(rows, duplicateRows(scan, sphere)...)
	rows = append(rows, mocPromotionRows(scan, sphere)...)
	rows = append(rows, demoteRows(scan, sphere)...)
	rows = append(rows, archiveRows(cfg, sphere)...)
	sortConsolidateRows(rows)
	return rows, nil
}

// PrepareMerge produces a merge dry-run payload for two notes.
func PrepareMerge(cfg *Config, sphere Sphere, loser, survivor string) (*MergePlan, error) {
	loserResolved, loserData, err := ReadNoteFile(cfg, sphere, loser)
	if err != nil {
		return nil, fmt.Errorf("load loser note: %w", err)
	}
	survivorResolved, survivorData, err := ReadNoteFile(cfg, sphere, survivor)
	if err != nil {
		return nil, fmt.Errorf("load survivor note: %w", err)
	}
	if IsProtectedPath(filepath.ToSlash(loserResolved.Rel)) {
		return nil, fmt.Errorf("brain consolidate: loser %q is in a protected area (commitments/gtd/glossary)", loserResolved.Rel)
	}
	if IsProtectedPath(filepath.ToSlash(survivorResolved.Rel)) {
		return nil, fmt.Errorf("brain consolidate: survivor %q is in a protected area (commitments/gtd/glossary)", survivorResolved.Rel)
	}
	loserNote, _ := ParseMarkdownNote(string(loserData), MarkdownParseOptions{})
	survivorNote, _ := ParseMarkdownNote(string(survivorData), MarkdownParseOptions{})
	if HasTODOMarkers(string(loserData)) || HasTODOMarkers(string(survivorData)) {
		return nil, fmt.Errorf("brain consolidate: refusing merge involving notes with TODO/checkbox markers")
	}
	if IsProtectedStatus(strings.ToLower(scalarField(loserNote, "status"))) || IsProtectedStatus(strings.ToLower(scalarField(survivorNote, "status"))) {
		return nil, fmt.Errorf("brain consolidate: refusing merge involving notes with open/active/deferred status")
	}
	mergedYAML, err := mergeFrontMatter(loserNote, survivorNote, loserResolved.Rel, survivorResolved.Rel)
	if err != nil {
		return nil, err
	}
	mergedBody := mergeBodies(loserNote, survivorNote, loserResolved.Rel)
	plan := &MergePlan{
		Loser:    filepath.ToSlash(loserResolved.Rel),
		Survivor: filepath.ToSlash(survivorResolved.Rel),
		YAML:     mergedYAML,
		Body:     mergedBody,
	}
	linkPlan, err := PlanMerge(cfg, sphere, loserResolved.Rel, survivorResolved.Rel)
	if err != nil {
		return nil, fmt.Errorf("plan merge link rewrite: %w", err)
	}
	plan.LinkPlan = linkPlan
	return plan, nil
}

// scanBrain walks the vault once and builds the in-memory link graph.
func scanBrain(cfg *Config, sphere Sphere) (*brainScan, error) {
	scan := &brainScan{byKey: map[string]*scannedNote{}, commitments: map[string]bool{}}
	err := WalkVaultNotes(cfg, sphere, func(snap NoteSnapshot) error {
		rel := filepath.ToSlash(snap.Source.Rel)
		if !rooted(rel, "brain/") {
			return nil
		}
		if rooted(rel, "brain/generated/retired/") {
			return nil
		}
		if rooted(rel, "brain/commitments/") {
			collectCommitmentTargets(snap, scan.commitments)
			return nil
		}
		if !belongsToConsolidationRoot(rel) {
			return nil
		}
		info := &scannedNote{
			source:    snap.Source,
			body:      snap.Body,
			note:      snap.Note,
			kind:      snap.Kind,
			wikilinks: extractWikilinks(snap.Body),
			bodyChars: bodyCharCount(snap.Body),
		}
		stat, err := os.Stat(snap.Source.Path)
		if err == nil {
			info.mtime = stat.ModTime()
		}
		populateNoteFields(info)
		scan.notes = append(scan.notes, info)
		scan.byKey[rel] = info
		return nil
	})
	if err != nil {
		return nil, err
	}
	computeInbound(scan, cfg, sphere)
	return scan, nil
}

// populateNoteFields lifts the frontmatter fields the planner consults.
func populateNoteFields(info *scannedNote) {
	if info.note == nil {
		return
	}
	info.focus = strings.ToLower(scalarField(info.note, "focus"))
	info.status = strings.ToLower(scalarField(info.note, "status"))
	info.displayName = scalarField(info.note, "display_name")
	if info.displayName == "" {
		info.displayName = scalarField(info.note, "name")
	}
	info.canonical = scalarField(info.note, "canonical_topic")
	info.aliases = listField(info.note, "aliases")
	info.opened = scalarField(info.note, "opened")
}

// computeInbound walks every note's wikilinks and counts inbound resolutions.
func computeInbound(scan *brainScan, cfg *Config, sphere Sphere) {
	for _, info := range scan.notes {
		for _, raw := range info.wikilinks {
			target, ok := resolveWikilinkRel(cfg, sphere, raw)
			if !ok {
				continue
			}
			if dest, exists := scan.byKey[target]; exists && dest != info {
				dest.inbound++
			}
		}
	}
}

func resolveWikilinkRel(cfg *Config, sphere Sphere, raw string) (string, bool) {
	resolved, err := ResolveWikilink(cfg, sphere, raw)
	if err != nil {
		return "", false
	}
	return filepath.ToSlash(resolved.Rel), true
}

// collectCommitmentTargets records every wikilink target referenced from a
// commitments/ note so the orphan detector can spare them.
func collectCommitmentTargets(snap NoteSnapshot, sink map[string]bool) {
	for _, raw := range extractWikilinks(snap.Body) {
		clean := strings.TrimSpace(strings.SplitN(strings.SplitN(raw, "|", 2)[0], "#", 2)[0])
		if clean == "" {
			continue
		}
		clean = strings.TrimSuffix(filepath.ToSlash(clean), ".md")
		sink[clean] = true
	}
}

func bodyCharCount(src string) int {
	body := src
	if idx := strings.Index(src, "\n---"); strings.HasPrefix(src, "---\n") && idx > 0 {
		end := strings.Index(src[4:], "\n---")
		if end >= 0 {
			body = src[4+end+4:]
		}
	}
	return len(strings.TrimSpace(body))
}

func belongsToConsolidationRoot(rel string) bool {
	for _, root := range consolidationRoots {
		if rooted(rel, root+"/") || rel == root+".md" {
			return true
		}
	}
	return false
}

func rooted(path, prefix string) bool {
	return strings.HasPrefix(path, prefix)
}

// orphanRows detects unreferenced notes per the note-retirement convention.
// Brain history is preserved by the brain repo's git log; the proposed action
// is a hard delete (Apply via `brain move --to /dev/null`), not a redirect
// stub.
func orphanRows(scan *brainScan, sphere Sphere, now time.Time) []ConsolidateRow {
	cutoff := now.AddDate(-1, 0, 0)
	var rows []ConsolidateRow
	for _, info := range scan.notes {
		if !isOrphan(info, scan, cutoff) {
			continue
		}
		ageDays := int(now.Sub(info.mtime).Hours() / 24)
		if ageDays < 0 {
			ageDays = 0
		}
		rows = append(rows, ConsolidateRow{
			Sphere:    sphere,
			Outcome:   OutcomeRetire,
			Path:      filepath.ToSlash(info.source.Rel),
			Score:     ageDays,
			Rationale: "orphan, focus=" + emptyDefault(info.focus, "(unset)"),
			Proposed:  nullDestination,
		})
	}
	return rows
}

func isOrphan(info *scannedNote, scan *brainScan, cutoff time.Time) bool {
	if isProtectedScannedNote(info) {
		return false
	}
	if info.kind == "glossary" && len(info.aliases) > 0 {
		return false
	}
	if info.focus != "" && info.focus != "parked" {
		return false
	}
	if info.mtime.IsZero() || info.mtime.After(cutoff) {
		return false
	}
	if info.inbound > 0 {
		return false
	}
	if outboundReachesNonOrphan(info, scan, cutoff) {
		return false
	}
	if commitmentReferences(info, scan.commitments) {
		return false
	}
	return true
}

func outboundReachesNonOrphan(info *scannedNote, scan *brainScan, cutoff time.Time) bool {
	for _, raw := range info.wikilinks {
		clean := strings.TrimSpace(strings.SplitN(strings.SplitN(raw, "|", 2)[0], "#", 2)[0])
		if clean == "" {
			continue
		}
		clean = strings.TrimSuffix(filepath.ToSlash(clean), ".md")
		target, ok := scan.byKey[clean+".md"]
		if !ok {
			return true
		}
		if target.focus != "" && target.focus != "parked" {
			return true
		}
		if !target.mtime.IsZero() && target.mtime.After(cutoff) {
			return true
		}
	}
	return false
}

func commitmentReferences(info *scannedNote, commitments map[string]bool) bool {
	rel := strings.TrimSuffix(filepath.ToSlash(info.source.Rel), ".md")
	if commitments[rel] {
		return true
	}
	brainRel := strings.TrimPrefix(rel, "brain/")
	return commitments[brainRel]
}

// isProtectedScannedNote excludes commitments/gtd/glossary notes and notes
// with status open/active/deferred/waiting/in_progress/started or TODO
// markers in the body from retire/consolidate/demote outcomes.
func isProtectedScannedNote(info *scannedNote) bool {
	if info == nil {
		return false
	}
	if IsProtectedPath(filepath.ToSlash(info.source.Rel)) {
		return true
	}
	if IsProtectedStatus(info.status) {
		return true
	}
	if HasTODOMarkers(info.body) {
		return true
	}
	return false
}

