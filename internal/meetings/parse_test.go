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

const meetingNote = `---
title: "Standup 2026-04-29"
date: 2026-04-29
owner: "Christopher Albert"
---
# Standup 2026-04-29

## Attendees
- Christopher Albert
- Ada Lovelace
- Charles Babbage

## Decisions
- Ship the analytical engine paper before the conference.
- Hold a calibration retro on Friday.

## Action Checklist

### Ada Lovelace
- [ ] Draft benchmark write-up @due:2026-05-02
- [ ] Schedule retro slot @follow:2026-05-04

### Charles Babbage
- [ ] Send the analytical engine paper @due:2026-05-09 ^[[projects/Engine]]

### Christopher Albert
- [ ] File the conference travel claim
`

func TestParseSummaryExtractsHeaderAttendeesDecisionsAndTasks(t *testing.T) {
	note := ParseSummary("2026-04-29-standup", meetingNote)
	if note.Title != "Standup 2026-04-29" {
		t.Fatalf("title = %q", note.Title)
	}
	if note.Date != "2026-04-29" {
		t.Fatalf("date = %q", note.Date)
	}
	if note.Owner != "Christopher Albert" {
		t.Fatalf("owner = %q", note.Owner)
	}
	if len(note.Attendees) != 3 || note.Attendees[1] != "Ada Lovelace" {
		t.Fatalf("attendees = %#v", note.Attendees)
	}
	if len(note.Decisions) != 2 || !strings.Contains(note.Decisions[0], "analytical engine paper") {
		t.Fatalf("decisions = %#v", note.Decisions)
	}
	if len(note.Tasks) != 4 {
		t.Fatalf("tasks = %d, want 4: %#v", len(note.Tasks), note.Tasks)
	}
}

func TestSummaryRecipientsExcludesOwner(t *testing.T) {
	note := ParseSummary("standup", meetingNote)
	got := note.SummaryRecipients()
	if len(got) != 2 {
		t.Fatalf("recipients = %#v, want 2", got)
	}
	for _, name := range got {
		if strings.EqualFold(name, "Christopher Albert") {
			t.Fatalf("owner leaked into recipients: %#v", got)
		}
	}
}

func TestSummaryRecipientsFallsBackToActionChecklistPersons(t *testing.T) {
	src := `---
owner: "Chris"
---
# Sync

## Action Checklist

### Ada Lovelace
- [ ] Reply to Chris

### Babbage
- [ ] Send paper
`
	note := ParseSummary("sync", src)
	if len(note.Attendees) != 0 {
		t.Fatalf("attendees should be empty: %#v", note.Attendees)
	}
	got := note.SummaryRecipients()
	if len(got) != 2 || got[0] != "Ada Lovelace" || got[1] != "Babbage" {
		t.Fatalf("recipients = %#v", got)
	}
}

func TestRenderDraftIncludesDecisionsAttendeesAndOnlyRecipientTasks(t *testing.T) {
	note := ParseSummary("2026-04-29-standup", meetingNote)
	draft := note.RenderDraft("Ada Lovelace", "ada@example.com", DraftRequest{ShareURL: "https://cloud.example/s/AAA"})
	if draft.Recipient != "Ada Lovelace" || draft.Email != "ada@example.com" {
		t.Fatalf("recipient/email wrong: %#v", draft)
	}
	if !strings.Contains(draft.Subject, "Standup 2026-04-29") || !strings.Contains(draft.Subject, "Ada") {
		t.Fatalf("subject = %q", draft.Subject)
	}
	body := draft.Body
	for _, want := range []string{
		"Hi Ada,",
		"Standup 2026-04-29",
		"2026-04-29",
		"Decisions:",
		"analytical engine paper",
		"Your tasks:",
		"Draft benchmark write-up",
		"due 2026-05-02",
		"https://cloud.example/s/AAA",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q:\n%s", want, body)
		}
	}
	for _, leak := range []string{
		"Send the analytical engine paper",
		"File the conference travel claim",
	} {
		if strings.Contains(body, leak) {
			t.Fatalf("body leaked another attendee's task %q:\n%s", leak, body)
		}
	}
}

func TestRenderDraftHandlesEmptyTasksGracefully(t *testing.T) {
	src := `---
title: "Quick chat"
owner: "Chris"
---
# Quick chat

## Attendees
- Chris
- Dana

## Decisions
- Park the topic.
`
	note := ParseSummary("quick", src)
	draft := note.RenderDraft("Dana", "", DraftRequest{})
	if !strings.Contains(draft.Body, "no action items captured for you") {
		t.Fatalf("expected empty-tasks fallback, got:\n%s", draft.Body)
	}
	if draft.HasTasks {
		t.Fatalf("HasTasks must be false when no tasks: %#v", draft)
	}
	if !draft.HasDecision {
		t.Fatalf("HasDecision must be true: %#v", draft)
	}
}

func TestSortDraftsByRecipientSortsCaseInsensitively(t *testing.T) {
	drafts := []Draft{{Recipient: "babbage"}, {Recipient: "Ada"}}
	SortDraftsByRecipient(drafts)
	if drafts[0].Recipient != "Ada" || drafts[1].Recipient != "babbage" {
		t.Fatalf("sort order: %#v", drafts)
	}
}
