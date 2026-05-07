package scout

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeNote(t *testing.T, root, vaultRel, body string) {
	t.Helper()
	full := filepath.Join(root, vaultRel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPickerScoresStaleEntitiesHigher(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC)
	writeNote(t, root, "people/fresh.md", `---
cadence: weekly
last_seen: 2026-05-05
---

# Fresh
`)
	writeNote(t, root, "people/stale.md", `---
cadence: weekly
last_seen: 2026-04-01
---

# Stale
`)
	picks, err := PickEntities(PickerOpts{
		BrainRoot: root,
		Roots:     []string{"people"},
		Now:       now,
		TopN:      10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(picks) < 2 {
		t.Fatalf("expected 2 picks, got %d", len(picks))
	}
	if picks[0].Path != "people/stale.md" {
		t.Fatalf("stale should rank first, got %s (score=%v) vs %s (score=%v)",
			picks[0].Path, picks[0].Score, picks[1].Path, picks[1].Score)
	}
}

func TestPickerNeedsReviewBoostsScore(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC)
	writeNote(t, root, "people/normal.md", `---
cadence: weekly
last_seen: 2026-04-01
---

# Normal
`)
	writeNote(t, root, "people/flagged.md", `---
cadence: monthly
last_seen: 2026-05-01
needs_review: true
---

# Flagged
`)
	picks, err := PickEntities(PickerOpts{BrainRoot: root, Roots: []string{"people"}, Now: now, TopN: 10})
	if err != nil {
		t.Fatal(err)
	}
	if picks[0].Path != "people/flagged.md" {
		t.Fatalf("needs_review should rank first, got %s", picks[0].Path)
	}
}

func TestPickerSkipsEntitiesWithoutCadence(t *testing.T) {
	root := t.TempDir()
	writeNote(t, root, "people/blank.md", `---
---

# Blank
`)
	picks, err := PickEntities(PickerOpts{
		BrainRoot: root,
		Roots:     []string{"people"},
		Now:       time.Now(),
		TopN:      10,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range picks {
		if p.Path == "people/blank.md" && p.Score > 0 {
			t.Fatalf("blank note should score 0, got %.2f", p.Score)
		}
	}
}

func TestScanUncertainty_NeedsReviewLine(t *testing.T) {
	body := "# Folder\n\n## Open Questions\n- needs review: license year unclear\n- something else\n"
	markers, score := scanUncertainty(body)
	if len(markers) != 1 || markers[0] != "license year unclear" {
		t.Fatalf("markers=%v", markers)
	}
	if score != 1000 {
		t.Fatalf("score=%v want 1000", score)
	}
}

func TestScanUncertainty_InlineMarkers(t *testing.T) {
	body := "# X\n\n- claim one (unverified)\n- claim two (unconfirmed)\n- normal bullet\n"
	markers, score := scanUncertainty(body)
	if len(markers) != 2 {
		t.Fatalf("markers=%v", markers)
	}
	if score != 100 {
		t.Fatalf("score=%v want 100 (2 inline @ 50)", score)
	}
}

func TestScanUncertainty_NoMarkers(t *testing.T) {
	body := "# X\n\n## Notes\n- regular bullet\n- another\n"
	markers, score := scanUncertainty(body)
	if len(markers) != 0 || score != 0 {
		t.Fatalf("markers=%v score=%v", markers, score)
	}
}

func TestScanUncertainty_OpenQuestionsCaseInsensitive(t *testing.T) {
	body := "# X\n\n## OPEN QUESTIONS\n- needs review: who hosts the data?\n"
	markers, score := scanUncertainty(body)
	if len(markers) != 1 || markers[0] != "who hosts the data?" {
		t.Fatalf("markers=%v", markers)
	}
	if score != 1000 {
		t.Fatalf("score=%v", score)
	}
}

func TestScanUncertainty_InlineCapAt200(t *testing.T) {
	body := "# X\n\n- a (unverified)\n- b (unverified)\n- c (unverified)\n- d (unverified)\n- e (unverified)\n"
	_, score := scanUncertainty(body)
	if score != 200 {
		t.Fatalf("score=%v want 200 (capped)", score)
	}
}

func TestPickerFolderNoteWithNeedsReview_OutranksStaleperson(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC)
	writeNote(t, root, "people/Stale Person.md", `---
cadence: monthly
---

# Stale Person
`)
	writeNote(t, root, "folders/plasma/CODES/NEO-RT.md", `---
title: NEO-RT
---

# NEO-RT

## Open Questions
- needs review: latest release tag
`)
	picks, err := PickEntities(PickerOpts{BrainRoot: root, Now: now, TopN: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(picks) < 2 {
		t.Fatalf("expected at least 2 picks, got %d (%v)", len(picks), picks)
	}
	if picks[0].Path != "folders/plasma/CODES/NEO-RT.md" {
		t.Fatalf("expected folder note to win, got %s (score %.2f)", picks[0].Path, picks[0].Score)
	}
	if len(picks[0].UncertaintyMarkers) != 1 {
		t.Fatalf("expected 1 marker, got %v", picks[0].UncertaintyMarkers)
	}
}

func TestPickerFolderNoteWithoutMarkersNoCadence_NotPicked(t *testing.T) {
	root := t.TempDir()
	writeNote(t, root, "folders/plasma/CODES/idle.md", "# idle\n\nLorem ipsum.\n")
	writeNote(t, root, "people/Active Person.md", `---
cadence: weekly
---

# Active Person
`)
	picks, err := PickEntities(PickerOpts{BrainRoot: root, Now: time.Now(), TopN: 5})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range picks {
		if p.Path == "folders/plasma/CODES/idle.md" {
			t.Fatalf("zero-score folder note should not be picked: %+v", p)
		}
	}
}

func TestPickerFoldersWalkedRecursively(t *testing.T) {
	root := t.TempDir()
	writeNote(t, root, "folders/lv/progphys/PiP_2026/syllabus.md", `# Syllabus

## Open Questions
- needs review: room booking
`)
	picks, err := PickEntities(PickerOpts{BrainRoot: root, Now: time.Now(), TopN: 5})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range picks {
		if p.Path == "folders/lv/progphys/PiP_2026/syllabus.md" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("nested folder note not picked: %+v", picks)
	}
}

func TestPickerCooldown_RecentlyScoutedSkipped(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC)
	writeNote(t, root, "people/Anton Fuchs.md", `---
cadence: monthly
---

# Anton Fuchs
`)
	writeNote(t, root, "people/Eve Stenson.md", `---
cadence: monthly
---

# Eve Stenson
`)
	// Simulate a scout report from 2 days ago for Anton Fuchs only.
	reportDir := filepath.Join(root, "reports", "scout", "20260505-100000")
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		t.Fatal(err)
	}
	reportPath := filepath.Join(reportDir, sanitizePath("people/Anton Fuchs.md")+".md")
	if err := os.WriteFile(reportPath, []byte("# old report\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	twoDaysAgo := now.Add(-48 * time.Hour)
	if err := os.Chtimes(reportPath, twoDaysAgo, twoDaysAgo); err != nil {
		t.Fatal(err)
	}
	picks, err := PickEntities(PickerOpts{BrainRoot: root, Now: now, TopN: 10, CooldownDays: 7})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range picks {
		if p.Path == "people/Anton Fuchs.md" {
			t.Fatalf("recently-scouted note should be skipped: %+v", p)
		}
	}
	found := false
	for _, p := range picks {
		if p.Path == "people/Eve Stenson.md" {
			found = true
		}
	}
	if !found {
		t.Fatalf("non-cooldown note should still be picked: %+v", picks)
	}
}

func TestPickerCooldown_OldReportEligibleAgain(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC)
	writeNote(t, root, "people/A.md", `---
cadence: monthly
---

# A
`)
	reportDir := filepath.Join(root, "reports", "scout", "20260401-100000")
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		t.Fatal(err)
	}
	reportPath := filepath.Join(reportDir, "people-A-md.md")
	if err := os.WriteFile(reportPath, []byte("# old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	month := now.Add(-30 * 24 * time.Hour)
	if err := os.Chtimes(reportPath, month, month); err != nil {
		t.Fatal(err)
	}
	picks, err := PickEntities(PickerOpts{BrainRoot: root, Now: now, TopN: 10, CooldownDays: 7})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range picks {
		if p.Path == "people/A.md" {
			found = true
		}
	}
	if !found {
		t.Fatalf("note scouted >7 days ago should be eligible again: %+v", picks)
	}
}

func TestPickerCooldown_ZeroDisablesFilter(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC)
	writeNote(t, root, "people/A.md", `---
cadence: monthly
---

# A
`)
	reportDir := filepath.Join(root, "reports", "scout", "20260507-100000")
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(reportDir, "people-A-md.md"), []byte("# fresh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	picks, err := PickEntities(PickerOpts{BrainRoot: root, Now: now, TopN: 10, CooldownDays: -1})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range picks {
		if p.Path == "people/A.md" {
			found = true
		}
	}
	if !found {
		t.Fatalf("CooldownDays=-1 should disable filter: %+v", picks)
	}
}

func TestPickerStrategicMultiplier(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC)
	writeNote(t, root, "people/normal.md", `---
cadence: weekly
last_seen: 2026-04-01
---

# Normal
`)
	writeNote(t, root, "people/strategic.md", `---
cadence: weekly
last_seen: 2026-04-01
strategic: true
---

# Strategic
`)
	picks, err := PickEntities(PickerOpts{BrainRoot: root, Roots: []string{"people"}, Now: now, TopN: 10})
	if err != nil {
		t.Fatal(err)
	}
	if picks[0].Path != "people/strategic.md" {
		t.Fatalf("strategic note should rank first, got %s", picks[0].Path)
	}
}
