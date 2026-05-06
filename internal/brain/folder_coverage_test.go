package brain

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSyncFolderCoverageCreatesMissingFolderNote(t *testing.T) {
	cfg, root := newSleepVault(t)
	if err := os.MkdirAll(filepath.Join(root, "projects", "new"), 0o755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "projects", "new", "README.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	dry, err := SyncFolderCoverage(cfg, SphereWork, FolderCoverageOpts{DryRun: true, Limit: 10})
	if err != nil {
		t.Fatalf("dry SyncFolderCoverage: %v", err)
	}
	notePath := filepath.Join(root, "brain", "folders", "projects", "new.md")
	item, ok := findCoverageItem(dry.Items, "projects/new")
	if !ok || item.Action != FolderCoverageCreate {
		t.Fatalf("dry coverage = %+v", dry)
	}
	if _, err := os.Stat(notePath); !os.IsNotExist(err) {
		t.Fatalf("dry run wrote note: %v", err)
	}

	applied, err := SyncFolderCoverage(cfg, SphereWork, FolderCoverageOpts{Limit: 10})
	if err != nil {
		t.Fatalf("apply SyncFolderCoverage: %v", err)
	}
	if applied.Created == 0 || applied.MarkedMissing != 0 {
		t.Fatalf("applied coverage = %+v", applied)
	}
	data, err := os.ReadFile(notePath)
	if err != nil {
		t.Fatalf("read generated note: %v", err)
	}
	folder, diags := ValidateFolderNote(string(data), LinkValidationContext{Config: cfg, Sphere: SphereWork, Path: notePath})
	if len(diags) != 0 {
		t.Fatalf("generated folder note invalid: %+v\n%s", diags, string(data))
	}
	if folder.SourceFolder != "projects/new" || folder.Status != "active" {
		t.Fatalf("folder = %+v", folder)
	}
}

func findCoverageItem(items []FolderCoverageItem, source string) (FolderCoverageItem, bool) {
	for _, item := range items {
		if item.SourceFolder == source {
			return item, true
		}
	}
	return FolderCoverageItem{}, false
}

func TestSyncFolderCoverageMarksMissingSourceFolder(t *testing.T) {
	cfg, root := newSleepVault(t)
	writeDreamRaw(t, root, "brain/folders/gone.md", folderNoteFixture("gone", "active"))

	summary, err := SyncFolderCoverage(cfg, SphereWork, FolderCoverageOpts{
		Limit: 10,
		Now:   time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("SyncFolderCoverage: %v", err)
	}
	if summary.MarkedMissing != 1 {
		t.Fatalf("MarkedMissing=%d, summary=%+v", summary.MarkedMissing, summary)
	}
	path := filepath.Join(root, "brain", "folders", "gone.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read note: %v", err)
	}
	folder, diags := ValidateFolderNote(string(data), LinkValidationContext{Config: cfg, Sphere: SphereWork, Path: path})
	if len(diags) != 0 {
		t.Fatalf("updated folder note invalid: %+v\n%s", diags, string(data))
	}
	if folder.Status != "stale" {
		t.Fatalf("status=%q, want stale", folder.Status)
	}
	if !strings.Contains(string(data), "Source folder missing as of 2026-05-06") {
		t.Fatalf("missing open question:\n%s", string(data))
	}
}

func TestSyncFolderCoverageSkipsBulkyExcludedRoots(t *testing.T) {
	cfg, root := newSleepVault(t)
	for _, rel := range []string{"Photos", "Camera Uploads", "ROMS", ".sloptools"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(rel), "child"), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
	}
	summary, err := SyncFolderCoverage(cfg, SphereWork, FolderCoverageOpts{DryRun: true, Limit: 100})
	if err != nil {
		t.Fatalf("SyncFolderCoverage: %v", err)
	}
	for _, item := range summary.Items {
		for _, excluded := range []string{"Photos", "Camera Uploads", "ROMS", ".sloptools"} {
			if item.SourceFolder == excluded || strings.HasPrefix(item.SourceFolder, excluded+"/") {
				t.Fatalf("coverage included excluded source %q in %+v", excluded, summary)
			}
		}
	}
}

func TestRunSleepAppliesFolderCoverageBeforeCodex(t *testing.T) {
	cfg, root := newSleepVault(t)
	if err := os.MkdirAll(filepath.Join(root, "projects", "new"), 0o755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	var captured string
	exec := func(ctx context.Context, req SleepCodexRequest) ([]byte, error) {
		captured = req.Packet
		return []byte("coverage done\n"), nil
	}
	res, err := RunSleep(cfg, SleepOpts{
		Sphere:         SphereWork,
		Budget:         4,
		CoverageBudget: 10,
		Backend:        SleepBackendCodex,
		CodexExec:      exec,
	})
	if err != nil {
		t.Fatalf("RunSleep: %v", err)
	}
	if res.Coverage.Created == 0 {
		t.Fatalf("Coverage.Created=0, summary=%+v", res.Coverage)
	}
	noteRel := "brain/folders/projects/new.md"
	if _, err := os.Stat(filepath.Join(root, noteRel)); err != nil {
		t.Fatalf("coverage note missing: %v", err)
	}
	if !strings.Contains(captured, noteRel) {
		t.Fatalf("sleep packet missing coverage note %q:\n%s", noteRel, captured)
	}
}

func folderNoteFixture(source, status string) string {
	return `---
kind: folder
vault: nextcloud
sphere: work
source_folder: ` + source + `
status: ` + status + `
projects: []
people: []
institutions: []
topics: []
---
# ` + source + `

## Summary
Folder note for ` + source + `.

## Key Facts
- Source folder: ` + source + `

## Important Files
- None.

## Related Folders
- None.

## Related Notes
- None.

## Notes
Evidence note.

## Open Questions
- None.
`
}
