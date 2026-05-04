package people

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writeNote(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestCollectMonthlyIndexesGroupsLogLinesAcrossPeopleProjectsTopics(t *testing.T) {
	brainRoot := t.TempDir()
	writeNote(t, filepath.Join(brainRoot, "people", "Ada Example.md"), `# Ada
## Log
- 2026-04-12 — coffee about Tokamak status
- 2026-03-02 — emailed about reviewer report
`)
	writeNote(t, filepath.Join(brainRoot, "projects", "Plasma Edge.md"), `# Plasma Edge
## Log
- 2026-04 — kicked off new sprint
- 2026-04-30 - merged the gradient solver
`)
	writeNote(t, filepath.Join(brainRoot, "topics", "Magnetic Reconnection.md"), `# Reconnection
## Log
- 2026-03-15 — read Yamada review
`)
	writeNote(t, filepath.Join(brainRoot, "people", "no-log.md"), "# no log here\n")

	got, err := CollectMonthlyIndexes(brainRoot)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	want := []MonthlyIndex{
		{
			Month: "2026-03",
			Lines: []string{
				"- [[Ada Example]] — emailed about reviewer report",
				"- [[Magnetic Reconnection]] — read Yamada review",
			},
		},
		{
			Month: "2026-04",
			Lines: []string{
				"- [[Ada Example]] — coffee about Tokamak status",
				"- [[Plasma Edge]] — kicked off new sprint",
				"- [[Plasma Edge]] — merged the gradient solver",
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("collect mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestCollectMonthlyIndexesIgnoresNotesWithoutLogHeading(t *testing.T) {
	brainRoot := t.TempDir()
	writeNote(t, filepath.Join(brainRoot, "people", "ada.md"), `# Ada
## Activity
- 2026-04-12 — looks like a log entry but wrong heading
`)
	got, err := CollectMonthlyIndexes(brainRoot)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no months, got %#v", got)
	}
}

func TestCollectMonthlyIndexesIgnoresNonLogLines(t *testing.T) {
	brainRoot := t.TempDir()
	writeNote(t, filepath.Join(brainRoot, "people", "ada.md"), `# Ada
## Log
plain prose without a leading bullet
- not a date — just text
- 2026-04-12 — kept
`)
	got, err := CollectMonthlyIndexes(brainRoot)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	want := []MonthlyIndex{{Month: "2026-04", Lines: []string{"- [[ada]] — kept"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("collect mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestRenderMonthlyIndexProducesCanonicalText(t *testing.T) {
	got := RenderMonthlyIndex("2026-04", []string{
		"- [[Ada Example]] — coffee",
		"- [[Plasma Edge]] — sprint kickoff",
	})
	want := "# 2026-04\n\n- [[Ada Example]] — coffee\n- [[Plasma Edge]] — sprint kickoff\n"
	if got != want {
		t.Fatalf("render mismatch\n got: %q\nwant: %q", got, want)
	}
}

func TestRenderMonthlyIndexSortsLines(t *testing.T) {
	got := RenderMonthlyIndex("2026-04", []string{
		"- [[Plasma Edge]] — sprint kickoff",
		"- [[Ada Example]] — coffee",
	})
	want := "# 2026-04\n\n- [[Ada Example]] — coffee\n- [[Plasma Edge]] — sprint kickoff\n"
	if got != want {
		t.Fatalf("render mismatch\n got: %q\nwant: %q", got, want)
	}
}

func TestWriteMonthlyIndexesCreatesJournalFiles(t *testing.T) {
	brainRoot := t.TempDir()
	writeNote(t, filepath.Join(brainRoot, "people", "Ada.md"), `# Ada
## Log
- 2026-04-12 — coffee
`)
	writeNote(t, filepath.Join(brainRoot, "topics", "Reconnection.md"), `# R
## Log
- 2026-04-15 — read paper
`)
	res, err := WriteMonthlyIndexes(brainRoot, false)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if res.Months != 1 || res.Writes != 1 || res.DryRun {
		t.Fatalf("result = %#v", res)
	}
	got, err := os.ReadFile(filepath.Join(brainRoot, "journal", "2026-04.md"))
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	want := "# 2026-04\n\n- [[Ada]] — coffee\n- [[Reconnection]] — read paper\n"
	if string(got) != want {
		t.Fatalf("journal mismatch\n got: %q\nwant: %q", got, want)
	}
}

func TestWriteMonthlyIndexesIsIdempotent(t *testing.T) {
	brainRoot := t.TempDir()
	writeNote(t, filepath.Join(brainRoot, "people", "Ada.md"), `# Ada
## Log
- 2026-04-12 — coffee
`)
	if _, err := WriteMonthlyIndexes(brainRoot, false); err != nil {
		t.Fatalf("first write: %v", err)
	}
	res, err := WriteMonthlyIndexes(brainRoot, false)
	if err != nil {
		t.Fatalf("second write: %v", err)
	}
	if res.Writes != 0 {
		t.Fatalf("expected zero rewrites, got %#v", res)
	}
	if res.Months != 1 {
		t.Fatalf("expected 1 month bucket, got %#v", res)
	}
}

func TestWriteMonthlyIndexesDryRunDoesNotWrite(t *testing.T) {
	brainRoot := t.TempDir()
	writeNote(t, filepath.Join(brainRoot, "people", "Ada.md"), `# Ada
## Log
- 2026-04-12 — coffee
`)
	res, err := WriteMonthlyIndexes(brainRoot, true)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if !res.DryRun || res.Writes != 1 || res.Months != 1 {
		t.Fatalf("result = %#v", res)
	}
	if _, err := os.Stat(filepath.Join(brainRoot, "journal", "2026-04.md")); !os.IsNotExist(err) {
		t.Fatalf("journal file should not exist on dry run, got err=%v", err)
	}
}

func TestWriteMonthlyIndexesEmptyVaultReturnsZero(t *testing.T) {
	brainRoot := t.TempDir()
	res, err := WriteMonthlyIndexes(brainRoot, false)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if res.Months != 0 || res.Writes != 0 {
		t.Fatalf("expected zero counts, got %#v", res)
	}
}

func TestCollectMonthlyIndexesAcceptsHyphenSeparator(t *testing.T) {
	brainRoot := t.TempDir()
	writeNote(t, filepath.Join(brainRoot, "people", "ada.md"), `# Ada
## Log
- 2026-04-12 - hyphen separator
`)
	got, err := CollectMonthlyIndexes(brainRoot)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	want := []MonthlyIndex{{Month: "2026-04", Lines: []string{"- [[ada]] — hyphen separator"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("collect mismatch\n got: %#v\nwant: %#v", got, want)
	}
}
