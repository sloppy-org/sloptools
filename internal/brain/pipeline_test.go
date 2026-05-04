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

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}
