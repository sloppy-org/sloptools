package brain

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	linkPlan, err := PlanMove(cfg, sphere, loserResolved.Rel, retiredDestination(loserResolved.Rel, time.Now()))
	if err != nil {
		return nil, fmt.Errorf("plan retired move: %w", err)
	}
	plan.LinkPlan = linkPlan
	return plan, nil
}

// retiredDestination computes the brain/generated/retired/<YYYY-MM>/<rel> path.
func retiredDestination(rel string, now time.Time) string {
	clean := filepath.ToSlash(rel)
	clean = strings.TrimPrefix(clean, "brain/")
	bucket := now.Format("2006-01")
	return filepath.ToSlash(filepath.Join("brain", "generated", "retired", bucket, clean))
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
			Proposed:  retiredDestination(info.source.Rel, now),
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

// duplicateRows surfaces near-duplicate notes by alias overlap or canonical name.
func duplicateRows(scan *brainScan, sphere Sphere) []ConsolidateRow {
	groups := groupDuplicateCandidates(scan)
	var rows []ConsolidateRow
	for _, group := range groups {
		if len(group.members) < 2 {
			continue
		}
		survivor, losers := pickSurvivor(group.members)
		if isProtectedScannedNote(survivor) {
			continue
		}
		for _, loser := range losers {
			if isProtectedScannedNote(loser) {
				continue
			}
			overlap := aliasOverlapCount(loser.aliases, survivor.aliases)
			extra := survivor.inbound - loser.inbound
			if extra < 0 {
				extra = 0
			}
			score := 100 + overlap*10 + extra
			rationale := group.reason
			if overlap >= 2 {
				rationale = fmt.Sprintf("%s; alias overlap %d", group.reason, overlap)
			}
			rows = append(rows, ConsolidateRow{
				Sphere:    sphere,
				Outcome:   OutcomeConsolidate,
				Path:      filepath.ToSlash(loser.source.Rel),
				Score:     score,
				Rationale: rationale,
				Proposed:  filepath.ToSlash(survivor.source.Rel),
			})
		}
	}
	return rows
}

type duplicateGroup struct {
	reason  string
	members []*scannedNote
}

func groupDuplicateCandidates(scan *brainScan) []duplicateGroup {
	byAlias := map[string][]*scannedNote{}
	byName := map[string][]*scannedNote{}
	for _, info := range scan.notes {
		for _, alias := range info.aliases {
			key := strings.ToLower(strings.TrimSpace(alias))
			if key == "" {
				continue
			}
			byAlias[key] = appendUnique(byAlias[key], info)
		}
		name := canonicalIdentity(info)
		if name != "" {
			byName[name] = appendUnique(byName[name], info)
		}
	}
	pairCounts := map[[2]*scannedNote]int{}
	for _, members := range byAlias {
		if len(members) < 2 {
			continue
		}
		for i := 0; i < len(members); i++ {
			for j := i + 1; j < len(members); j++ {
				key := orderedPair(members[i], members[j])
				pairCounts[key]++
			}
		}
	}
	seen := map[[2]*scannedNote]bool{}
	var groups []duplicateGroup
	for pair, count := range pairCounts {
		if count < 2 {
			continue
		}
		if seen[pair] {
			continue
		}
		seen[pair] = true
		groups = append(groups, duplicateGroup{
			reason:  fmt.Sprintf("alias overlap (%d shared)", count),
			members: []*scannedNote{pair[0], pair[1]},
		})
	}
	for name, members := range byName {
		if len(members) < 2 {
			continue
		}
		groups = append(groups, duplicateGroup{
			reason:  "shared canonical name " + name,
			members: members,
		})
	}
	return groups
}

func canonicalIdentity(info *scannedNote) string {
	for _, candidate := range []string{info.displayName, info.canonical} {
		clean := strings.ToLower(strings.TrimSpace(candidate))
		clean = strings.TrimPrefix(clean, "[[")
		clean = strings.TrimSuffix(clean, "]]")
		if clean != "" {
			return clean
		}
	}
	return ""
}

func aliasOverlapCount(left, right []string) int {
	rightSet := map[string]bool{}
	for _, alias := range right {
		rightSet[strings.ToLower(strings.TrimSpace(alias))] = true
	}
	count := 0
	for _, alias := range left {
		if rightSet[strings.ToLower(strings.TrimSpace(alias))] {
			count++
		}
	}
	return count
}

func appendUnique(list []*scannedNote, info *scannedNote) []*scannedNote {
	for _, existing := range list {
		if existing == info {
			return list
		}
	}
	return append(list, info)
}

func orderedPair(a, b *scannedNote) [2]*scannedNote {
	if a.source.Rel < b.source.Rel {
		return [2]*scannedNote{a, b}
	}
	return [2]*scannedNote{b, a}
}

func pickSurvivor(members []*scannedNote) (*scannedNote, []*scannedNote) {
	sorted := append([]*scannedNote(nil), members...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].inbound != sorted[j].inbound {
			return sorted[i].inbound > sorted[j].inbound
		}
		left := strings.TrimSpace(sorted[i].opened)
		right := strings.TrimSpace(sorted[j].opened)
		switch {
		case left != "" && right != "" && left != right:
			return left < right
		case left != "" && right == "":
			return true
		case right != "" && left == "":
			return false
		}
		return sorted[i].source.Rel < sorted[j].source.Rel
	})
	return sorted[0], sorted[1:]
}

// mocPromotionRows finds folder notes dense enough to be promoted to a topic.
// A parent note can sit beside its children (sibling index) or one level
// above (parent.md beside parent/ directory).
func mocPromotionRows(scan *brainScan, sphere Sphere) []ConsolidateRow {
	childCount := childFolderCounts(scan)
	var rows []ConsolidateRow
	for _, info := range scan.notes {
		if isProtectedScannedNote(info) {
			continue
		}
		rel := filepath.ToSlash(info.source.Rel)
		if !rooted(rel, "brain/folders/") || filepath.Ext(rel) != ".md" {
			continue
		}
		children := mocChildCount(rel, childCount)
		if children < 10 || info.bodyChars <= 500 {
			continue
		}
		rows = append(rows, ConsolidateRow{
			Sphere:    sphere,
			Outcome:   OutcomeKeep,
			Path:      rel,
			Score:     children + info.bodyChars/200,
			Rationale: fmt.Sprintf("MOC candidate (children=%d, body=%dch)", children, info.bodyChars),
			Proposed:  suggestedTopicPath(rel),
		})
	}
	return rows
}

func mocChildCount(rel string, counts map[string]int) int {
	siblings := counts[filepath.ToSlash(filepath.Dir(rel))] - 1
	if siblings < 0 {
		siblings = 0
	}
	nested := counts[strings.TrimSuffix(rel, ".md")]
	if siblings > nested {
		return siblings
	}
	return nested
}

func childFolderCounts(scan *brainScan) map[string]int {
	counts := map[string]int{}
	for _, info := range scan.notes {
		rel := filepath.ToSlash(info.source.Rel)
		if !rooted(rel, "brain/folders/") {
			continue
		}
		parent := filepath.ToSlash(filepath.Dir(rel))
		counts[parent]++
	}
	return counts
}

func suggestedTopicPath(rel string) string {
	base := strings.TrimSuffix(filepath.Base(rel), ".md")
	return filepath.ToSlash(filepath.Join("brain", "topics", base+".md"))
}

// demoteRows finds folder notes whose source is not yet under archive/.
func demoteRows(scan *brainScan, sphere Sphere) []ConsolidateRow {
	var rows []ConsolidateRow
	for _, info := range scan.notes {
		if info.kind != "folder" || info.status != "archived" {
			continue
		}
		folder, _ := ParseFolderNote(info.body)
		source := strings.TrimSpace(folder.SourceFolder)
		if source == "" || rooted(source, "archive/") {
			continue
		}
		rows = append(rows, ConsolidateRow{
			Sphere:    sphere,
			Outcome:   OutcomeDemote,
			Path:      filepath.ToSlash(info.source.Rel),
			Score:     50,
			Rationale: "status=archived but source not under archive/",
			Proposed:  filepath.ToSlash(filepath.Join("archive", filepath.Base(source))),
		})
	}
	return rows
}

// archiveRows lifts ArchiveCandidates findings into the consolidate queue.
func archiveRows(cfg *Config, sphere Sphere) []ConsolidateRow {
	candidates := loadArchiveCandidates(cfg, sphere)
	rows := make([]ConsolidateRow, 0, len(candidates))
	for _, candidate := range candidates {
		topic := archiveTopic(candidate.Path)
		rows = append(rows, ConsolidateRow{
			Sphere:    sphere,
			Outcome:   OutcomeArchive,
			Path:      filepath.ToSlash(candidate.Path),
			Score:     candidate.Score,
			Rationale: candidate.Action + ": " + candidate.Rationale,
			Proposed:  filepath.ToSlash(filepath.Join("archive", topic, filepath.Base(candidate.Path))),
		})
	}
	return rows
}

// archiveVaultFilter maps a brain sphere to the `vault` column value used in
// the brain-ingest profile TSV. Empty string means no filter, which would
// bleed cross-sphere rows; we only return that for unknown spheres.
func archiveVaultFilter(sphere Sphere) string {
	switch sphere {
	case SphereWork:
		return "nextcloud"
	case SpherePrivate:
		return "dropbox"
	}
	return ""
}

func loadArchiveCandidates(cfg *Config, sphere Sphere) []ArchiveCandidate {
	root := archiveProfileRoot(cfg, sphere)
	if root == "" {
		return nil
	}
	filter := archiveVaultFilter(sphere)
	if filter == "" {
		return nil
	}
	rows, err := ArchiveCandidates(root, filter, 0)
	if err != nil {
		return nil
	}
	out := rows[:0]
	for _, row := range rows {
		if row.Vault != filter {
			continue
		}
		out = append(out, row)
	}
	return out
}

func archiveProfileRoot(cfg *Config, sphere Sphere) string {
	if cfg == nil {
		return ""
	}
	vault, ok := cfg.Vault(sphere)
	if !ok {
		return ""
	}
	candidates := []string{
		filepath.Join(vault.Root, "tools", "brain-ingest"),
		filepath.Join(vault.BrainRoot(), "tools", "brain-ingest"),
	}
	for _, root := range candidates {
		if _, err := os.Stat(filepath.Join(root, "data", "folder")); err == nil {
			return root
		}
	}
	return ""
}

func archiveTopic(path string) string {
	parts := strings.Split(filepath.ToSlash(filepath.Clean(path)), "/")
	if len(parts) >= 2 {
		return strings.ToLower(parts[len(parts)-2])
	}
	return "misc"
}

// sortConsolidateRows applies the canonical ordering from the contract.
func sortConsolidateRows(rows []ConsolidateRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		left := outcomeOrder[rows[i].Outcome]
		right := outcomeOrder[rows[j].Outcome]
		if left != right {
			return left < right
		}
		if rows[i].Score != rows[j].Score {
			return rows[i].Score > rows[j].Score
		}
		return rows[i].Path < rows[j].Path
	})
}

func emptyDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

// PruneRetiredStub describes a redirect stub the prune-stubs CLI has flagged.
type PruneRetiredStub struct {
	Sphere    Sphere `json:"sphere"`
	Path      string `json:"path"`
	RetiredAt string `json:"retired_at"`
	AgeDays   int    `json:"age_days"`
	Redirect  string `json:"redirect,omitempty"`
}

// FindRetiredStubs returns redirect stubs older than maxAge under brain/generated/retired/.
func FindRetiredStubs(cfg *Config, sphere Sphere, maxAge time.Duration, now time.Time) ([]PruneRetiredStub, error) {
	vault, err := cfgVault(cfg, sphere)
	if err != nil {
		return nil, err
	}
	root := filepath.Join(vault.BrainRoot(), "generated", "retired")
	stat, err := os.Stat(root)
	if err != nil || !stat.IsDir() {
		return nil, nil
	}
	var stubs []PruneRetiredStub
	walkErr := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		note, _ := ParseMarkdownNote(string(data), MarkdownParseOptions{})
		if scalarField(note, "redirect") == "" {
			return nil
		}
		retiredAt := scalarField(note, "retired_at")
		stamp, ok := parseRetiredAt(retiredAt)
		if !ok || now.Sub(stamp) < maxAge {
			return nil
		}
		rel, _ := filepath.Rel(vault.Root, path)
		stubs = append(stubs, PruneRetiredStub{
			Sphere:    sphere,
			Path:      filepath.ToSlash(rel),
			RetiredAt: retiredAt,
			AgeDays:   int(now.Sub(stamp).Hours() / 24),
			Redirect:  scalarField(note, "redirect"),
		})
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	sort.Slice(stubs, func(i, j int) bool { return stubs[i].Path < stubs[j].Path })
	return stubs, nil
}

func parseRetiredAt(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{"2006-01-02", time.RFC3339, "2006-01-02T15:04:05"} {
		if stamp, err := time.Parse(layout, value); err == nil {
			return stamp, true
		}
	}
	return time.Time{}, false
}
