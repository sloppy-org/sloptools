package brain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlanArchiveProducesEventNoteAndBacklinks(t *testing.T) {
	cfg := testConfig(t)
	work := cfg.mustVault(t, SphereWork)
	src := filepath.Join(work.Root, "archive", "PHD2017")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "thesis.pdf"), []byte("PDF"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	folderNote := `---
kind: folder
source_folder: archive/PHD2017
status: archived
projects:
  - "[[projects/PHD2017]]"
people:
  - "[[people/Wilfried Kernbichler]]"
institutions: []
topics:
  - "[[topics/Drift Kinetics]]"
---

# PHD2017

## Summary
PhD thesis material; defended 2018.

## Key Facts
- Supervisor: Kernbichler.
- Thesis title: Drift kinetics.

## Important Files
- thesis.pdf

## Related Folders
- None.

## Related Notes
- None.

## Notes
- Defence May 2018.

## Open Questions
- None.
`
	folderRel := filepath.Join("brain", "folders", "archive", "PHD2017.md")
	folderAbs := filepath.Join(work.Root, folderRel)
	if err := os.MkdirAll(filepath.Dir(folderAbs), 0o755); err != nil {
		t.Fatalf("mkdir folder dir: %v", err)
	}
	if err := os.WriteFile(folderAbs, []byte(folderNote), 0o644); err != nil {
		t.Fatalf("write folder note: %v", err)
	}

	plan, err := PlanArchive(cfg, SphereWork, "archive/PHD2017")
	if err != nil {
		t.Fatalf("PlanArchive: %v", err)
	}
	if plan.SourceKind != "dir" {
		t.Fatalf("SourceKind = %s, want dir", plan.SourceKind)
	}
	if plan.GigaDest != "Nextcloud/PHD2017.tar.xz" {
		t.Fatalf("GigaDest = %s, want Nextcloud/PHD2017.tar.xz", plan.GigaDest)
	}
	if !strings.HasPrefix(plan.EventNotePath, "brain/archive-events/") || !strings.HasSuffix(plan.EventNotePath, "-PHD2017.md") {
		t.Fatalf("EventNotePath = %s, want brain/archive-events/<date>-PHD2017.md", plan.EventNotePath)
	}
	if plan.FolderNotePath != filepath.ToSlash(folderRel) {
		t.Fatalf("FolderNotePath = %s, want %s", plan.FolderNotePath, folderRel)
	}
	body := plan.EventNoteBody
	for _, want := range []string{
		"kind: archive-event",
		"sphere: work",
		"source_folder: archive/PHD2017",
		"cold_archive: Nextcloud/PHD2017.tar.xz",
		"sha256: PENDING-FILLED-AT-APPLY",
		"size_bytes: 0",
		"## Distilled facts",
		"PhD thesis material",
		"Supervisor: Kernbichler",
		"## Recovery",
		"## Source folder note",
		"# PHD2017",
		`"[[projects/PHD2017]]"`,
		`"[[people/Wilfried Kernbichler]]"`,
		`"[[topics/Drift Kinetics]]"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("event body missing %q\n---body---\n%s", want, body)
		}
	}
	canonical := map[string]bool{}
	for _, e := range plan.BacklinkEdits {
		canonical[e.CanonicalNote] = true
		if !strings.Contains(e.Bullet, "[[archive-events/") {
			t.Errorf("bullet missing event link: %q", e.Bullet)
		}
		if !strings.Contains(e.Bullet, "PHD2017") {
			t.Errorf("bullet missing source ref: %q", e.Bullet)
		}
	}
	for _, want := range []string{
		"brain/projects/PHD2017.md",
		"brain/people/Wilfried Kernbichler.md",
		"brain/topics/Drift Kinetics.md",
	} {
		if !canonical[want] {
			t.Errorf("missing backlink edit for %s; got %v", want, canonical)
		}
	}
	if plan.Digest == "" {
		t.Fatalf("digest must be non-empty")
	}
}

func TestPlanArchiveRefusesNonArchivePath(t *testing.T) {
	cfg := testConfig(t)
	work := cfg.mustVault(t, SphereWork)
	if err := os.MkdirAll(filepath.Join(work.Root, "plasma", "DOCUMENTS"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if _, err := PlanArchive(cfg, SphereWork, "plasma/DOCUMENTS"); err == nil {
		t.Fatalf("expected error: source must be under archive/")
	}
}

func TestPlanArchiveRefusesPersonal(t *testing.T) {
	cfg := testConfig(t)
	work := cfg.mustVault(t, SphereWork)
	if err := os.MkdirAll(filepath.Join(work.Root, "personal", "secrets"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if _, err := PlanArchive(cfg, SphereWork, "personal/secrets"); err == nil {
		t.Fatalf("expected error for personal/")
	}
}

func TestPlanArchiveSingleFile(t *testing.T) {
	cfg := testConfig(t)
	work := cfg.mustVault(t, SphereWork)
	dst := filepath.Join(work.Root, "archive", "chat.zip")
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(dst, []byte("FAKEZIP"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	plan, err := PlanArchive(cfg, SphereWork, "archive/chat.zip")
	if err != nil {
		t.Fatalf("PlanArchive: %v", err)
	}
	if plan.SourceKind != "file" {
		t.Fatalf("SourceKind = %s, want file", plan.SourceKind)
	}
	if plan.GigaDest != "Nextcloud/chat.zip" {
		t.Fatalf("GigaDest = %s, want Nextcloud/chat.zip (no .tar.xz for single file)", plan.GigaDest)
	}
	if !strings.Contains(plan.EventNoteBody, "scp giga:/mnt/files/archive/chris/Nextcloud/chat.zip") {
		t.Errorf("expected scp recovery snippet in body:\n%s", plan.EventNoteBody)
	}
}

func TestAppendArchiveBacklinkCreatesAndReuses(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "person.md")
	if err := os.WriteFile(path, []byte("---\nkind: human\n---\n\n# Foo\n\n## Summary\nFoo bar.\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := appendArchiveBacklink(path, "- 2026-05-06: archived foo -> [[archive-events/2026-05-06-foo]]"); err != nil {
		t.Fatalf("appendArchiveBacklink first: %v", err)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "## Archive") || !strings.Contains(string(b), "2026-05-06-foo") {
		t.Fatalf("first append missing section/bullet:\n%s", b)
	}
	// Second different bullet appends to same section.
	if err := appendArchiveBacklink(path, "- 2026-05-07: archived bar -> [[archive-events/2026-05-07-bar]]"); err != nil {
		t.Fatalf("appendArchiveBacklink second: %v", err)
	}
	b, _ = os.ReadFile(path)
	if strings.Count(string(b), "## Archive") != 1 {
		t.Fatalf("expected exactly one ## Archive section:\n%s", b)
	}
	if !strings.Contains(string(b), "2026-05-06-foo") || !strings.Contains(string(b), "2026-05-07-bar") {
		t.Fatalf("missing one of the bullets:\n%s", b)
	}
	// Third identical bullet is a no-op.
	before, _ := os.ReadFile(path)
	if err := appendArchiveBacklink(path, "- 2026-05-07: archived bar -> [[archive-events/2026-05-07-bar]]"); err != nil {
		t.Fatalf("appendArchiveBacklink dedup: %v", err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatalf("duplicate bullet should have been deduped")
	}
}
