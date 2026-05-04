package brain

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRelationCandidatesExtractTypedRelation(t *testing.T) {
	root := t.TempDir()
	cfg, err := NewConfig([]Vault{{Sphere: SphereWork, Root: root}})
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	path := filepath.Join(root, "brain", "folders", "project.md")
	writeTestFile(t, path, `---
kind: folder
source_folder: project
status: active
people:
  - Ada Lovelace
institutions:
  - TU Graz
projects: []
topics: []
---

# project

## Summary
Ada Lovelace collaborated with TU Graz.

## Key Facts
- Source folder: project

## Notes
- None.

## Open Questions
- None.
`)
	rows, err := RelationCandidates(cfg, SphereWork)
	if err != nil {
		t.Fatalf("RelationCandidates: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows)=%d, want 1", len(rows))
	}
	if rows[0].Type != "collaborated_with" || rows[0].Source != "Ada Lovelace" || rows[0].Target != "TU Graz" {
		t.Fatalf("row=%+v", rows[0])
	}
}

func TestStreamOpencodeReportWritesTextAndProgress(t *testing.T) {
	input := bytes.NewBufferString(
		`{"type":"text","part":{"text":"hello"}}` + "\n" +
			`{"type":"tool_use","part":{"tool":"helpy_web_search","state":{"status":"completed","title":"Search NTV"}}}` + "\n",
	)
	var report bytes.Buffer
	var events bytes.Buffer
	if err := StreamOpencodeReport(input, &report, &events); err != nil {
		t.Fatalf("StreamOpencodeReport: %v", err)
	}
	if got := report.String(); got != "hello\n- progress: `helpy_web_search` Search NTV\n" {
		t.Fatalf("report=%q", got)
	}
	if events.Len() == 0 {
		t.Fatalf("events were not written")
	}
}

func TestFolderQualityFlagsOpenQuestions(t *testing.T) {
	root := t.TempDir()
	cfg, err := NewConfig([]Vault{{Sphere: SphereWork, Root: root}})
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	writeFolderNote(t, root, "brain/folders/old.md", "project/2019", "active", "- needs review: check source")
	rows, err := FolderQuality(cfg, SphereWork)
	if err != nil {
		t.Fatalf("FolderQuality: %v", err)
	}
	if len(rows) == 0 {
		t.Fatalf("expected quality candidate")
	}
	codes := map[string]bool{}
	for _, finding := range rows[0].Findings {
		codes[finding.Code] = true
	}
	if !codes["open-questions"] || !codes["old-active-path"] {
		t.Fatalf("codes=%v", codes)
	}
}

func TestApplyFolderReviewPreservesFrontmatter(t *testing.T) {
	root := t.TempDir()
	cfg, err := NewConfig([]Vault{{Sphere: SphereWork, Root: root}})
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	writeFolderNote(t, root, "brain/folders/project.md", "project", "active", "- None.")
	review := "## Summary\nUpdated summary with enough detail to pass the strict parser.\n\n## Key Facts\n- Source folder: project\n- Status: active\n\n## Important Files\n- None.\n\n## Related Folders\n- None.\n\n## Related Notes\n- None.\n\n## Notes\n- Reviewed.\n\n## Open Questions\n- None.\n"
	_, changed, err := ApplyFolderReview(cfg, SphereWork, "brain/folders/project.md", review)
	if err != nil {
		t.Fatalf("ApplyFolderReview: %v", err)
	}
	if !changed {
		t.Fatalf("changed=false")
	}
}

func TestValidateWorkUnitsFindsOverlap(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "data", "folder", "work_units.tsv"), "vault\tunit_root\tstatus\nnextcloud\tproject\tpending\nnextcloud\tproject/child\tpending\n")
	issues, err := ValidateWorkUnits(root)
	if err != nil {
		t.Fatalf("ValidateWorkUnits: %v", err)
	}
	if len(issues) == 0 || issues[0].Issue != "overlap" {
		t.Fatalf("issues=%+v", issues)
	}
}

func TestArchiveCandidatesFlagsVendoredTree(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "data", "folder", "profiles.tsv"), "vault\tpath\textensions\tprocessable_files\tprocessable_dirs\nnextcloud\tproj/vendor/zlib\t.dll:7\t120\t14\n")
	rows, err := ArchiveCandidates(root, 10)
	if err != nil {
		t.Fatalf("ArchiveCandidates: %v", err)
	}
	if len(rows) != 1 || rows[0].Action != "archive_sure" {
		t.Fatalf("rows=%+v", rows)
	}
}

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func writeFolderNote(t *testing.T, root, rel, source, status, openQuestions string) {
	body := `---
kind: folder
source_folder: ` + source + `
status: ` + status + `
projects: []
people: []
institutions: []
topics: []
---

# ` + source + `

## Summary
Short.

## Key Facts
- Source folder: ` + source + `
- Status: ` + status + `

## Important Files
- None.

## Related Folders
- None.

## Related Notes
- None.

## Notes
- None.

## Open Questions
` + openQuestions + `
`
	writeTestFile(t, filepath.Join(root, rel), body)
}
