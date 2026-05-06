package brain

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	FolderCoverageCreate      = "create_folder_note"
	FolderCoverageMarkMissing = "mark_source_missing"
)

var folderCoverageStaticExcludes = map[string]bool{
	".ipynb_checkpoints": true,
	".sloptools":         true,
	".vs":                true,
	"archive":            true,
	"bin":                true,
	"bilder":             true,
	"books":              true,
	"build":              true,
	"camera uploads":     true,
	"debug":              true,
	"dist":               true,
	"fotos":              true,
	"node_modules":       true,
	"obj":                true,
	"photos":             true,
	"pictures":           true,
	"release":            true,
	"roms":               true,
	"sound":              true,
	"target":             true,
	"zotero":             true,
}

type FolderCoverageOpts struct {
	Limit  int
	DryRun bool
	Now    time.Time
	Since  time.Time
}

type FolderCoverageSummary struct {
	Sphere        Sphere               `json:"sphere"`
	DryRun        bool                 `json:"dry_run"`
	Planned       int                  `json:"planned"`
	Created       int                  `json:"created"`
	MarkedMissing int                  `json:"marked_missing"`
	Items         []FolderCoverageItem `json:"items,omitempty"`
}

type FolderCoverageItem struct {
	Sphere       Sphere `json:"sphere"`
	Action       string `json:"action"`
	SourceFolder string `json:"source_folder"`
	NotePath     string `json:"note_path"`
	Reason       string `json:"reason"`
}

type folderNoteIndex struct {
	bySource map[string]folderNoteRecord
	byPath   map[string]folderNoteRecord
}

type folderNoteRecord struct {
	source string
	status string
	path   string
}

func SyncFolderCoverage(cfg *Config, sphere Sphere, opts FolderCoverageOpts) (FolderCoverageSummary, error) {
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	vault, err := cfgVault(cfg, sphere)
	if err != nil {
		return FolderCoverageSummary{}, err
	}
	if opts.Since.IsZero() {
		opts.Since = folderCoverageSince(vault, opts.Now)
	}
	items, err := planFolderCoverage(cfg, vault, opts.Limit, opts.Since)
	if err != nil {
		return FolderCoverageSummary{}, err
	}
	summary := FolderCoverageSummary{Sphere: sphere, DryRun: opts.DryRun, Planned: len(items), Items: items}
	if opts.DryRun {
		return summary, nil
	}
	for _, item := range items {
		switch item.Action {
		case FolderCoverageCreate:
			if err := writeGeneratedFolderNote(vault, item); err != nil {
				return FolderCoverageSummary{}, err
			}
			summary.Created++
		case FolderCoverageMarkMissing:
			changed, err := markFolderNoteSourceMissing(vault, item, opts.Now)
			if err != nil {
				return FolderCoverageSummary{}, err
			}
			if changed {
				summary.MarkedMissing++
			}
		}
	}
	return summary, nil
}

func planFolderCoverage(cfg *Config, vault Vault, limit int, since time.Time) ([]FolderCoverageItem, error) {
	sources, err := scanSourceFolders(vault)
	if err != nil {
		return nil, err
	}
	notes, err := scanFolderNotes(cfg, vault.Sphere)
	if err != nil {
		return nil, err
	}
	items := missingFolderNoteItems(vault, sources, notes, since)
	items = append(items, missingSourceItems(vault, sources, notes)...)
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Action != items[j].Action {
			return items[i].Action < items[j].Action
		}
		return items[i].NotePath < items[j].NotePath
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func scanSourceFolders(vault Vault) (map[string]time.Time, error) {
	excludes := folderCoverageExcludes(vault)
	out := map[string]time.Time{}
	err := filepath.WalkDir(vault.Root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		if path == vault.Root || !entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(vault.Root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if folderCoverageExcluded(rel, entry.Name(), excludes) {
			return fs.SkipDir
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		out[rel] = info.ModTime()
		return nil
	})
	return out, err
}

func scanFolderNotes(cfg *Config, sphere Sphere) (folderNoteIndex, error) {
	index := folderNoteIndex{bySource: map[string]folderNoteRecord{}, byPath: map[string]folderNoteRecord{}}
	err := WalkVaultNotes(cfg, sphere, func(snap NoteSnapshot) error {
		if snap.Kind != "folder" {
			return nil
		}
		folder, _ := ParseFolderNote(snap.Body)
		source := canonicalSourceFolder(folder.SourceFolder)
		rel := filepath.ToSlash(snap.Source.Rel)
		record := folderNoteRecord{source: source, status: strings.ToLower(folder.Status), path: rel}
		if source != "" {
			index.bySource[source] = record
		}
		index.byPath[rel] = record
		return nil
	})
	return index, err
}

func missingFolderNoteItems(vault Vault, sources map[string]time.Time, notes folderNoteIndex, since time.Time) []FolderCoverageItem {
	var items []FolderCoverageItem
	for source, mtime := range sources {
		if !since.IsZero() && mtime.Before(since) {
			continue
		}
		notePath := folderNotePath(source)
		if _, ok := notes.bySource[source]; ok {
			continue
		}
		if _, ok := notes.byPath[notePath]; ok {
			continue
		}
		items = append(items, FolderCoverageItem{
			Sphere:       vault.Sphere,
			Action:       FolderCoverageCreate,
			SourceFolder: source,
			NotePath:     notePath,
			Reason:       "source folder has no folder note",
		})
	}
	return items
}

func missingSourceItems(vault Vault, sources map[string]time.Time, notes folderNoteIndex) []FolderCoverageItem {
	var items []FolderCoverageItem
	for source, note := range notes.bySource {
		_, exists := sources[source]
		if source == "" || exists || note.status == "stale" || note.status == "frozen" {
			continue
		}
		if sourceExcludedByCoverage(vault, source) {
			continue
		}
		items = append(items, FolderCoverageItem{
			Sphere:       vault.Sphere,
			Action:       FolderCoverageMarkMissing,
			SourceFolder: source,
			NotePath:     note.path,
			Reason:       "source folder is missing",
		})
	}
	return items
}

func folderCoverageExcludes(vault Vault) map[string]bool {
	excludes := map[string]bool{}
	brainRel, err := filepath.Rel(vault.Root, vault.BrainRoot())
	if err == nil {
		excludes[filepath.Clean(brainRel)] = true
	}
	for _, exclude := range vault.Exclude {
		excludes[filepath.Clean(exclude)] = true
	}
	return excludes
}

func folderCoverageExcluded(rel, base string, excludes map[string]bool) bool {
	lowerBase := strings.ToLower(base)
	if strings.HasPrefix(base, ".") {
		return true
	}
	if standardCleanupIgnores[base] || folderCoverageStaticExcludes[lowerBase] {
		return true
	}
	if _, kind := classifyBackupName(base); kind != "" {
		return true
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	for exclude := range excludes {
		if clean == exclude || strings.HasPrefix(clean, exclude+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func sourceExcludedByCoverage(vault Vault, source string) bool {
	clean := filepath.Clean(filepath.FromSlash(source))
	rel := filepath.ToSlash(clean)
	if folderCoverageExcluded(rel, filepath.Base(clean), folderCoverageExcludes(vault)) {
		return true
	}
	for _, part := range strings.Split(rel, "/") {
		if folderCoverageExcluded(part, part, nil) {
			return true
		}
	}
	return false
}

func folderCoverageSince(vault Vault, now time.Time) time.Time {
	if hash, ok := latestSleepCommit(vault.BrainRoot()); ok {
		out, err := gitOutput(vault.BrainRoot(), "show", "-s", "--format=%cI", hash)
		if err == nil {
			if stamp, parseErr := time.Parse(time.RFC3339, strings.TrimSpace(out)); parseErr == nil {
				return stamp
			}
		}
	}
	return now.Add(-24 * time.Hour)
}

func writeGeneratedFolderNote(vault Vault, item FolderCoverageItem) error {
	target := filepath.Join(vault.Root, filepath.FromSlash(item.NotePath))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	body, err := renderGeneratedFolderNote(vault, item.SourceFolder)
	if err != nil {
		return err
	}
	return os.WriteFile(target, []byte(body), 0o644)
}

func renderGeneratedFolderNote(vault Vault, source string) (string, error) {
	stats, err := folderSourceStats(vault, source)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "---\nkind: folder\nvault: %s\nsphere: %s\nsource_folder: %s\nstatus: active\nprojects: []\npeople: []\ninstitutions: []\ntopics: []\n---\n",
		yamlQuoted(vaultLabel(vault)), yamlQuoted(string(vault.Sphere)), yamlQuoted(source))
	fmt.Fprintf(&b, "# %s\n\n", filepath.Base(source))
	fmt.Fprintln(&b, "## Summary")
	fmt.Fprintf(&b, "Source folder `%s` currently contains %d direct folder(s) and %d direct file(s).\n\n", source, stats.dirs, stats.files)
	fmt.Fprintln(&b, "## Key Facts")
	fmt.Fprintf(&b, "- Source folder: %s\n", source)
	fmt.Fprintf(&b, "- Direct folders: %d\n", stats.dirs)
	fmt.Fprintf(&b, "- Direct files: %d\n", stats.files)
	fmt.Fprintln(&b, "- Evidence basis: generated folder coverage scan")
	fmt.Fprintln(&b)
	writeFolderListSection(&b, "Important Files", stats.fileNames)
	writeFolderListSection(&b, "Related Folders", stats.dirNames)
	fmt.Fprintln(&b, "## Related Notes")
	fmt.Fprintln(&b, "- None.")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "## Notes")
	fmt.Fprintln(&b, "This note records folder-level evidence for later semantic consolidation.")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "## Open Questions\n- None.")
	return b.String(), nil
}

func markFolderNoteSourceMissing(vault Vault, item FolderCoverageItem, now time.Time) (bool, error) {
	path := filepath.Join(vault.Root, filepath.FromSlash(item.NotePath))
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	note, diags := ParseMarkdownNote(string(data), MarkdownParseOptions{})
	if len(diags) != 0 {
		return false, fmt.Errorf("parse folder note %s: %s", item.NotePath, diagnosticsString(diags))
	}
	if scalarField(note, "status") == "stale" {
		return false, nil
	}
	if err := note.SetFrontMatterField("status", "stale"); err != nil {
		return false, err
	}
	open := fmt.Sprintf("- Source folder missing as of %s; resolve as moved, archived, or historical evidence.\n", now.Format("2006-01-02"))
	if err := note.SetSectionBody("Open Questions", open); err != nil {
		return false, err
	}
	rendered, err := note.Render()
	if err != nil {
		return false, err
	}
	if rendered == string(data) {
		return false, nil
	}
	return true, os.WriteFile(path, []byte(rendered), 0o644)
}

type folderStats struct {
	dirs      int
	files     int
	dirNames  []string
	fileNames []string
}

func folderSourceStats(vault Vault, source string) (folderStats, error) {
	dir := filepath.Join(vault.Root, filepath.FromSlash(source))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return folderStats{}, err
	}
	var stats folderStats
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			stats.dirs++
			if len(stats.dirNames) < 8 {
				stats.dirNames = append(stats.dirNames, name)
			}
			continue
		}
		stats.files++
		if len(stats.fileNames) < 8 {
			stats.fileNames = append(stats.fileNames, name)
		}
	}
	sort.Strings(stats.dirNames)
	sort.Strings(stats.fileNames)
	return stats, nil
}

func writeFolderListSection(b *strings.Builder, name string, items []string) {
	fmt.Fprintf(b, "## %s\n", name)
	if len(items) == 0 {
		fmt.Fprintln(b, "- None.")
		fmt.Fprintln(b)
		return
	}
	for _, item := range items {
		fmt.Fprintf(b, "- %s\n", item)
	}
	fmt.Fprintln(b)
}

func folderNotePath(source string) string {
	return filepath.ToSlash(filepath.Join("brain", "folders", filepath.FromSlash(source)+".md"))
}

func canonicalSourceFolder(source string) string {
	source = filepath.ToSlash(filepath.Clean(strings.TrimSpace(source)))
	if source == "." {
		return ""
	}
	return strings.Trim(source, "/")
}

func vaultLabel(vault Vault) string {
	if strings.TrimSpace(vault.Label) != "" {
		return vault.Label
	}
	if vault.Sphere == SpherePrivate {
		return "dropbox"
	}
	return "nextcloud"
}

func yamlQuoted(value string) string {
	return strconv.Quote(value)
}
