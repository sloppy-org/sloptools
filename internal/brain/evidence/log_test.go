package evidence

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAppendReadRecent(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC()
	old := now.AddDate(0, 0, -100)

	entries := []Entry{
		{TS: now, RunID: "run1", Entity: "people/Alice.md", Claim: "works at TUGraz", Verdict: VerdictVerified, Confidence: 0.9},
		{TS: old, RunID: "run0", Entity: "people/Bob.md", Claim: "old claim", Verdict: VerdictVerified, Confidence: 0.9},
		{TS: now, RunID: "run1", Entity: "people/Alice.md", Claim: "email outdated", Verdict: VerdictConflicting, Confidence: 0.85},
	}
	if err := Append(root, entries); err != nil {
		t.Fatalf("Append: %v", err)
	}

	recent, err := ReadRecent(root, 90)
	if err != nil {
		t.Fatalf("ReadRecent: %v", err)
	}
	if len(recent) != 2 {
		t.Fatalf("expected 2 recent entries (old one pruned), got %d", len(recent))
	}

	// File should now only have 2 entries after pruning.
	all, err := readAll(filepath.Join(root, "evidence", "log.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 entries after rotation, got %d", len(all))
	}
}

func TestMarkApplied(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC()
	if err := Append(root, []Entry{
		{TS: now, RunID: "run1", Entity: "people/X.md", Claim: "claim1", Verdict: VerdictVerified, Confidence: 0.9},
	}); err != nil {
		t.Fatal(err)
	}
	if err := MarkApplied(root, "people/X.md", "run1"); err != nil {
		t.Fatal(err)
	}
	all, _ := readAll(filepath.Join(root, "evidence", "log.jsonl"))
	if !all[0].Applied {
		t.Fatal("expected entry to be marked applied")
	}
	if all[0].AppliedAt == nil {
		t.Fatal("expected AppliedAt to be set")
	}
}

func TestMarkReverted(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC()
	at := now
	if err := Append(root, []Entry{
		{TS: now, RunID: "run1", Entity: "people/X.md", Claim: "c", Verdict: VerdictVerified, Confidence: 0.9, Applied: true, AppliedAt: &at},
	}); err != nil {
		t.Fatal(err)
	}
	if err := MarkReverted(root, "people/X.md"); err != nil {
		t.Fatal(err)
	}
	all, _ := readAll(filepath.Join(root, "evidence", "log.jsonl"))
	if !all[0].Reverted {
		t.Fatal("expected entry to be marked reverted")
	}
}

func TestYieldRatio(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC()
	at := now
	if err := Append(root, []Entry{
		{TS: now, RunID: "r1", Entity: "people/A.md", Claim: "c1", Verdict: VerdictVerified, Confidence: 0.9, Applied: true, AppliedAt: &at},
		{TS: now, RunID: "r1", Entity: "people/A.md", Claim: "c2", Verdict: VerdictConflicting, Confidence: 0.85},
		{TS: now, RunID: "r1", Entity: "people/B.md", Claim: "c3", Verdict: VerdictVerified, Confidence: 0.9},
	}); err != nil {
		t.Fatal(err)
	}
	ratio := YieldRatio(root, "people/A.md", 90)
	if ratio != 0.5 { // 1 applied out of 2
		t.Fatalf("expected 0.5, got %f", ratio)
	}
	ratioB := YieldRatio(root, "people/B.md", 90)
	if ratioB != 0.0 { // 0 applied out of 1
		t.Fatalf("expected 0.0, got %f", ratioB)
	}
	ratioC := YieldRatio(root, "people/Unknown.md", 90)
	if ratioC != 0.5 { // no history → default
		t.Fatalf("expected 0.5 default, got %f", ratioC)
	}
}

func TestParseBullets(t *testing.T) {
	body := `# Scout report — Alice

## Verified
- Works at TU Graz as professor (source: https://tugraz.at/alice)
- Email is alice@tugraz.at (source: https://tugraz.at/contact)

## Conflicting / outdated
- Title was "Dr." but now "Univ.-Prof. Dr." (current: Dr.; observed: Univ.-Prof. Dr.; source: https://tugraz.at/alice)

## Suggestions
- Update title field to "Univ.-Prof. Dr."

## Open questions
- Whether she has a second affiliation
`
	entries := ParseBullets("run1", "people/Alice.md", body, time.Now().UTC())
	if len(entries) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(entries))
	}
	if entries[0].Verdict != VerdictVerified {
		t.Errorf("entry 0 should be verified, got %q", entries[0].Verdict)
	}
	if entries[0].Source != "https://tugraz.at/alice" {
		t.Errorf("entry 0 source wrong: %q", entries[0].Source)
	}
	if entries[2].Verdict != VerdictConflicting {
		t.Errorf("entry 2 should be conflicting, got %q", entries[2].Verdict)
	}
	if entries[3].SuggestedEdit == "" {
		t.Error("entry 3 should have suggested_edit")
	}
}

func TestAppendCreatesDir(t *testing.T) {
	root := filepath.Join(t.TempDir(), "nonexistent")
	if err := Append(root, []Entry{{TS: time.Now().UTC(), RunID: "r", Entity: "x", Claim: "c", Verdict: VerdictVerified, Confidence: 0.9}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "evidence", "log.jsonl")); err != nil {
		t.Fatal("log.jsonl not created:", err)
	}
}
