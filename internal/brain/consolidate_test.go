package brain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeBrainNote writes a note under the work vault and stamps mtime when set.
func writeBrainNote(t *testing.T, cfg *Config, sphere Sphere, rel, body string, mtime time.Time) string {
	t.Helper()
	vault := cfg.mustVault(t, sphere)
	path := filepath.Join(vault.Root, filepath.FromSlash(rel))
	writeFile(t, path, body)
	if !mtime.IsZero() {
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatalf("chtimes %s: %v", path, err)
		}
	}
	return path
}

func TestConsolidatePlanDetectsOrphan(t *testing.T) {
	cfg := testConfig(t)
	old := time.Now().AddDate(-2, 0, 0)
	writeBrainNote(t, cfg, SphereWork, "brain/topics/lonely.md", `---
kind: topic
focus: parked
---
# Lonely
`, old)

	rows, err := ConsolidatePlan(cfg, SphereWork)
	if err != nil {
		t.Fatalf("ConsolidatePlan: %v", err)
	}
	row, ok := findRow(rows, "brain/topics/lonely.md")
	if !ok {
		t.Fatalf("expected orphan row; got %+v", rows)
	}
	if row.Outcome != OutcomeRetire {
		t.Fatalf("outcome = %s, want retire", row.Outcome)
	}
	if row.Score < 365 {
		t.Fatalf("score = %d, want >=365 days", row.Score)
	}
	if !strings.HasPrefix(row.Proposed, "brain/generated/retired/") {
		t.Fatalf("proposed = %q, want retired path", row.Proposed)
	}
}

func TestConsolidatePlanSparesActiveFocus(t *testing.T) {
	cfg := testConfig(t)
	old := time.Now().AddDate(-2, 0, 0)
	writeBrainNote(t, cfg, SphereWork, "brain/topics/active.md", `---
kind: topic
focus: active
---
# Active
`, old)

	rows, err := ConsolidatePlan(cfg, SphereWork)
	if err != nil {
		t.Fatalf("ConsolidatePlan: %v", err)
	}
	if _, ok := findRow(rows, "brain/topics/active.md"); ok {
		t.Fatalf("active note should not be retired: %+v", rows)
	}
}

func TestConsolidatePlanGlossaryAliasException(t *testing.T) {
	cfg := testConfig(t)
	old := time.Now().AddDate(-2, 0, 0)
	writeBrainNote(t, cfg, SphereWork, "brain/glossary/term.md", `---
kind: glossary
display_name: NTV
aliases:
  - NTV
  - neoclassical toroidal viscosity
---
# NTV
`, old)

	rows, err := ConsolidatePlan(cfg, SphereWork)
	if err != nil {
		t.Fatalf("ConsolidatePlan: %v", err)
	}
	for _, row := range rows {
		if row.Path == "brain/glossary/term.md" && row.Outcome == OutcomeRetire {
			t.Fatalf("glossary with aliases should not be retired: %+v", row)
		}
	}
}

func TestConsolidatePlanDuplicateAliases(t *testing.T) {
	cfg := testConfig(t)
	now := time.Now()
	// Survivor: many inbound links, multiple aliases.
	writeBrainNote(t, cfg, SphereWork, "brain/people/Alex_Doe.md", `---
kind: human
display_name: Alex Doe
aliases:
  - Alex Doe
  - A. Doe
opened: 2020-01-01
---
# Alex Doe
`, now)
	writeBrainNote(t, cfg, SphereWork, "brain/people/Alex_Duplicate.md", `---
kind: human
display_name: Alex Doe
aliases:
  - Alex Doe
  - A. Doe
opened: 2024-01-01
---
# Alex Duplicate
`, now)
	// Inbound for the survivor.
	writeBrainNote(t, cfg, SphereWork, "brain/projects/p.md", `---
kind: project
focus: active
---
# Project
[[people/Alex_Doe]]
`, now)

	rows, err := ConsolidatePlan(cfg, SphereWork)
	if err != nil {
		t.Fatalf("ConsolidatePlan: %v", err)
	}
	row, ok := findRow(rows, "brain/people/Alex_Duplicate.md")
	if !ok {
		t.Fatalf("expected loser row; got %+v", rows)
	}
	if row.Outcome != OutcomeConsolidate {
		t.Fatalf("outcome = %s, want consolidate", row.Outcome)
	}
	if row.Proposed != "brain/people/Alex_Doe.md" {
		t.Fatalf("proposed = %q, want survivor brain/people/Alex_Doe.md", row.Proposed)
	}
	if row.Score < 100 {
		t.Fatalf("score = %d, want >= 100", row.Score)
	}
}

func TestConsolidatePlanMOCPromotionEmits(t *testing.T) {
	cfg := testConfig(t)
	now := time.Now()
	parent := "## Summary\n" + strings.Repeat("Body content. ", 60) + "\n"
	parentNote := "---\nkind: folder\nfocus: active\n---\n# Hub\n" + parent
	writeBrainNote(t, cfg, SphereWork, "brain/folders/hub/index.md", parentNote, now)
	for i := 0; i < 12; i++ {
		writeBrainNote(t, cfg, SphereWork, filepath.ToSlash(filepath.Join("brain/folders/hub", "child_"+strings.Repeat("a", i+1)+".md")), "---\nkind: folder\nfocus: active\n---\n# child\n", now)
	}

	rows, err := ConsolidatePlan(cfg, SphereWork)
	if err != nil {
		t.Fatalf("ConsolidatePlan: %v", err)
	}
	row, ok := findRow(rows, "brain/folders/hub/index.md")
	if !ok {
		t.Fatalf("expected MOC candidate row; got %+v", rows)
	}
	if row.Outcome != OutcomeKeep {
		t.Fatalf("outcome = %s, want keep", row.Outcome)
	}
	if !strings.Contains(row.Rationale, "MOC candidate") {
		t.Fatalf("rationale = %q, want MOC candidate", row.Rationale)
	}
	if row.Proposed != "brain/topics/index.md" {
		t.Fatalf("proposed = %q, want brain/topics/index.md", row.Proposed)
	}
}

func TestConsolidatePlanSortOrder(t *testing.T) {
	rows := []ConsolidateRow{
		{Outcome: OutcomeKeep, Path: "brain/topics/k.md", Score: 5},
		{Outcome: OutcomeRetire, Path: "brain/topics/r1.md", Score: 100},
		{Outcome: OutcomeArchive, Path: "brain/topics/a.md", Score: 80},
		{Outcome: OutcomeRetire, Path: "brain/topics/r2.md", Score: 200},
		{Outcome: OutcomeConsolidate, Path: "brain/topics/c.md", Score: 110},
	}
	sortConsolidateRows(rows)
	wantOrder := []string{
		"brain/topics/a.md",
		"brain/topics/r2.md",
		"brain/topics/r1.md",
		"brain/topics/c.md",
		"brain/topics/k.md",
	}
	for i, want := range wantOrder {
		if rows[i].Path != want {
			t.Fatalf("rows[%d] = %s, want %s; full=%v", i, rows[i].Path, want, rows)
		}
	}
}

func TestConsolidatePlanArchiveRowsHonorSphere(t *testing.T) {
	cfg := testConfig(t)
	work := cfg.mustVault(t, SphereWork)
	priv := cfg.mustVault(t, SpherePrivate)
	// brain/ must exist in both vaults for ConsolidatePlan to walk them.
	writeFile(t, filepath.Join(work.Root, "brain", "topics", "anchor.md"), "---\nkind: topic\nfocus: active\n---\n# anchor\n")
	writeFile(t, filepath.Join(priv.Root, "brain", "topics", "anchor.md"), "---\nkind: topic\nfocus: active\n---\n# anchor\n")
	// The brain-ingest archive profile lives under each vault root. Stage one
	// per vault with a row that scores high enough to surface (>= 50 files,
	// vendored-style hint via .dll extensions).
	header := "vault\tsphere\tpath\tdepth\tdirect_dirs\tdirect_files\tdescendant_dirs\tdescendant_files\tprocessable_dirs\tprocessable_files\textensions\n"
	workRow := "nextcloud\twork\tproj/vendor/zlib\t3\t0\t120\t0\t0\t14\t120\t.dll:7\n"
	privRow := "dropbox\tprivate\tapp/vendor/lib\t3\t0\t120\t0\t0\t14\t120\t.dll:7\n"
	writeFile(t, filepath.Join(work.Root, "tools", "brain-ingest", "data", "folder", "tree_profile_fast.tsv"), header+workRow+privRow)
	writeFile(t, filepath.Join(priv.Root, "tools", "brain-ingest", "data", "folder", "tree_profile_fast.tsv"), header+workRow+privRow)

	rows, err := ConsolidatePlan(cfg, SphereWork)
	if err != nil {
		t.Fatalf("ConsolidatePlan(work): %v", err)
	}
	for _, row := range rows {
		if row.Outcome != OutcomeArchive {
			continue
		}
		if strings.HasPrefix(row.Path, "app/") {
			t.Fatalf("private archive row leaked into work plan: %+v", row)
		}
	}
	rows, err = ConsolidatePlan(cfg, SpherePrivate)
	if err != nil {
		t.Fatalf("ConsolidatePlan(private): %v", err)
	}
	for _, row := range rows {
		if row.Outcome != OutcomeArchive {
			continue
		}
		if strings.HasPrefix(row.Path, "proj/") {
			t.Fatalf("work archive row leaked into private plan: %+v", row)
		}
	}
}

func TestConsolidatePlanExcludesPersonal(t *testing.T) {
	cfg := testConfig(t)
	old := time.Now().AddDate(-2, 0, 0)
	// personal/ is excluded for the work vault. Create brain/ first so the
	// scan walker has a real directory to enter.
	writeBrainNote(t, cfg, SphereWork, "brain/topics/keep.md", `---
kind: topic
focus: active
---
# Keep
`, old)
	writeBrainNote(t, cfg, SphereWork, "personal/secret.md", "secret", old)
	rows, err := ConsolidatePlan(cfg, SphereWork)
	if err != nil {
		t.Fatalf("ConsolidatePlan: %v", err)
	}
	for _, row := range rows {
		if strings.HasPrefix(row.Path, "personal/") {
			t.Fatalf("personal row leaked: %+v", row)
		}
	}
}

func TestPrepareMergeProducesYAMLConflictMarkers(t *testing.T) {
	cfg := testConfig(t)
	now := time.Now()
	writeBrainNote(t, cfg, SphereWork, "brain/people/loser.md", `---
kind: human
display_name: Loser Display
aliases:
  - Loser
status: stale
---
# Loser

## Summary
Loser summary.
`, now)
	writeBrainNote(t, cfg, SphereWork, "brain/people/survivor.md", `---
kind: human
display_name: Survivor Display
aliases:
  - Survivor
status: archived
---
# Survivor

## Summary
Survivor summary.
`, now)

	plan, err := PrepareMerge(cfg, SphereWork, "brain/people/loser.md", "brain/people/survivor.md")
	if err != nil {
		t.Fatalf("PrepareMerge: %v", err)
	}
	if !strings.Contains(plan.YAML, ">>>>>>>") || !strings.Contains(plan.YAML, "<<<<<<<") {
		t.Fatalf("YAML missing conflict markers: %q", plan.YAML)
	}
	if !strings.Contains(plan.YAML, "Loser Display") || !strings.Contains(plan.YAML, "Survivor Display") {
		t.Fatalf("YAML missing both display values: %q", plan.YAML)
	}
	// Aliases must be unioned uniquely without conflict markers.
	if !strings.Contains(plan.YAML, "Loser") || !strings.Contains(plan.YAML, "Survivor") {
		t.Fatalf("aliases not unioned: %q", plan.YAML)
	}
}

func TestPrepareMergeAlignsBodiesByH2(t *testing.T) {
	cfg := testConfig(t)
	now := time.Now()
	writeBrainNote(t, cfg, SphereWork, "brain/topics/loser.md", `---
kind: topic
---
# Loser

## Summary
Loser summary text.

## Loser Only
Loser-specific content.
`, now)
	writeBrainNote(t, cfg, SphereWork, "brain/topics/survivor.md", `---
kind: topic
---
# Survivor

## Summary
Survivor summary text.

## Survivor Only
Survivor-specific content.
`, now)

	plan, err := PrepareMerge(cfg, SphereWork, "brain/topics/loser.md", "brain/topics/survivor.md")
	if err != nil {
		t.Fatalf("PrepareMerge: %v", err)
	}
	if !strings.Contains(plan.Body, "<<< loser") || !strings.Contains(plan.Body, "=== survivor") || !strings.Contains(plan.Body, ">>>") {
		t.Fatalf("body missing diverging-section markers: %q", plan.Body)
	}
	if !strings.Contains(plan.Body, "## Survivor Only") {
		t.Fatalf("body missing survivor-only section: %q", plan.Body)
	}
	if !strings.Contains(plan.Body, "(from brain/topics/loser.md) Loser Only") {
		t.Fatalf("body missing loser-only appendix heading: %q", plan.Body)
	}
}

func TestFindRetiredStubsRespectsAge(t *testing.T) {
	cfg := testConfig(t)
	now := time.Now()
	old := now.AddDate(0, 0, -45)
	young := now.AddDate(0, 0, -10)
	writeBrainNote(t, cfg, SphereWork, "brain/generated/retired/2026-03/topics/old.md", `---
retired: true
retired_at: `+old.Format("2006-01-02")+`
redirect: "[[topics/new]]"
---
`, time.Time{})
	writeBrainNote(t, cfg, SphereWork, "brain/generated/retired/2026-04/topics/young.md", `---
retired: true
retired_at: `+young.Format("2006-01-02")+`
redirect: "[[topics/fresh]]"
---
`, time.Time{})

	stubs, err := FindRetiredStubs(cfg, SphereWork, 30*24*time.Hour, now)
	if err != nil {
		t.Fatalf("FindRetiredStubs: %v", err)
	}
	if len(stubs) != 1 || !strings.HasSuffix(stubs[0].Path, "old.md") {
		t.Fatalf("stubs = %+v, want only old", stubs)
	}
}

func TestMergeBodyHasUnresolvedConflicts(t *testing.T) {
	if !MergeBodyHasUnresolvedConflicts("text\n<<<<<<< loser\nstuff") {
		t.Fatalf("expected conflict detected")
	}
	if MergeBodyHasUnresolvedConflicts("text without markers") {
		t.Fatalf("unexpected conflict")
	}
}

func findRow(rows []ConsolidateRow, path string) (ConsolidateRow, bool) {
	for _, row := range rows {
		if row.Path == path {
			return row, true
		}
	}
	return ConsolidateRow{}, false
}
