package brain

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

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
