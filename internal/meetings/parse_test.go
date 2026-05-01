package meetings

import (
	"strings"
	"testing"
)

const sampleNote = `---
title: Standup 2026-04-29
---
# Standup 2026-04-29

## Notes
Some prose. - [ ] this checkbox is outside Action Checklist and must be ignored.

## Action Checklist

### Ada Lovelace
- [ ] Reply to Ada about benchmarks @due:2026-05-02
- [x] Already done item ^[[projects/Alpha]]

### Babbage
- [ ] Send the analytical engine paper @follow:2026-05-10 <!-- gtd:abc1234567 -->

## Other
- [ ] Outside the action checklist again
`

func TestParseExtractsPerPersonTasksOnlyFromActionChecklist(t *testing.T) {
	note := Parse("standup", sampleNote)
	if note.Slug != "standup" {
		t.Fatalf("slug = %q, want standup", note.Slug)
	}
	if len(note.Tasks) != 3 {
		t.Fatalf("got %d tasks, want 3: %#v", len(note.Tasks), note.Tasks)
	}
	got := note.Tasks[0]
	if got.Person != "Ada Lovelace" || got.Text != "Reply to Ada about benchmarks" || got.Due != "2026-05-02" || got.Done {
		t.Fatalf("first task wrong: %#v", got)
	}
	if note.Tasks[1].Done == false || note.Tasks[1].Project != "Alpha" {
		t.Fatalf("second task wrong: %#v", note.Tasks[1])
	}
	if note.Tasks[2].Person != "Babbage" || note.Tasks[2].FollowUp != "2026-05-10" || note.Tasks[2].ID != "abc1234567" {
		t.Fatalf("third task wrong: %#v", note.Tasks[2])
	}
}

func TestParseIgnoresChecklistOutsideActionSection(t *testing.T) {
	src := "## Other\n- [ ] not a meeting task\n"
	if got := Parse("foo", src); len(got.Tasks) != 0 {
		t.Fatalf("expected zero tasks, got %d: %#v", len(got.Tasks), got.Tasks)
	}
}

func TestComputeIDIsStableAndIgnoresMetadata(t *testing.T) {
	a := ComputeID("standup", "Ada Lovelace", "Reply to Ada about benchmarks @due:2026-05-02")
	b := ComputeID("standup", "Ada Lovelace", "Reply to Ada about benchmarks @due:2026-06-02")
	if a == "" || a != b {
		t.Fatalf("ComputeID not stable across metadata change: %q vs %q", a, b)
	}
	if len(a) != IDLength {
		t.Fatalf("id length = %d, want %d", len(a), IDLength)
	}
	other := ComputeID("standup", "Babbage", "Reply to Ada about benchmarks")
	if other == a {
		t.Fatal("ComputeID should differ across persons")
	}
}

func TestComputeIDPersonInsensitiveCase(t *testing.T) {
	if ComputeID("s", "Ada Lovelace", "x") != ComputeID("s", "ada lovelace", "x") {
		t.Fatal("ComputeID should be case-insensitive on person")
	}
}

func TestAssignIDsStampsMissingIDsAndPreservesExisting(t *testing.T) {
	updated, tasks, changed := AssignIDs("standup", sampleNote)
	if !changed {
		t.Fatal("expected changed=true since two tasks lacked IDs")
	}
	if len(tasks) != 3 {
		t.Fatalf("got %d tasks, want 3", len(tasks))
	}
	for _, task := range tasks {
		if task.ID == "" {
			t.Fatalf("task missing ID after stamp: %#v", task)
		}
	}
	if !strings.Contains(updated, "<!-- gtd:abc1234567 -->") {
		t.Fatal("existing ID was lost during stamping")
	}
	for _, task := range tasks {
		if !strings.Contains(updated, FormatComment(task.ID)) {
			t.Fatalf("stamped ID %q missing from updated source", task.ID)
		}
	}
}

func TestAssignIDsIdempotent(t *testing.T) {
	first, _, _ := AssignIDs("standup", sampleNote)
	second, _, changed := AssignIDs("standup", first)
	if changed {
		t.Fatal("second AssignIDs should not change a fully-stamped note")
	}
	if first != second {
		t.Fatal("idempotent AssignIDs must return identical source on second pass")
	}
}

func TestAssignIDsPreservesNonTaskLinesByteForByte(t *testing.T) {
	updated, _, _ := AssignIDs("standup", sampleNote)
	// Lines that are not Action Checklist tasks must be untouched.
	for _, expected := range []string{
		"---",
		"title: Standup 2026-04-29",
		"# Standup 2026-04-29",
		"## Notes",
		"Some prose. - [ ] this checkbox is outside Action Checklist and must be ignored.",
		"## Other",
		"- [ ] Outside the action checklist again",
	} {
		if !strings.Contains(updated, expected) {
			t.Fatalf("expected unchanged line missing: %q", expected)
		}
	}
}
