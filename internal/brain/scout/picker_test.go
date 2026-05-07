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
