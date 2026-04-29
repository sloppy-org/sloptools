package gtd

import "testing"

type staticReviewer struct {
	review LLMReview
}

func (s staticReviewer) ReviewSimilarity(CommitmentEntry, CommitmentEntry, float64) (LLMReview, error) {
	return s.review, nil
}

func TestScanReconcilesIdenticalSourceBindingOnce(t *testing.T) {
	entries := []CommitmentEntry{
		entry("brain/gtd/a.md", "Review W7-X plots", SourceBinding{Provider: "GitHub", Ref: "org/repo#1"}),
		entry("brain/gtd/b.md", "Review W7-X plots", SourceBinding{Provider: "github", Ref: "org/repo#1"}),
	}
	got := Scan(entries, ScanOptions{})
	if len(got.Aggregates) != 1 {
		t.Fatalf("aggregate count = %d, want 1: %#v", len(got.Aggregates), got.Aggregates)
	}
	if len(got.Aggregates[0].Bindings) != 1 || got.Aggregates[0].BindingIDs[0] != "github:org/repo#1" {
		t.Fatalf("bindings = %#v, ids=%#v", got.Aggregates[0].Bindings, got.Aggregates[0].BindingIDs)
	}
	if len(got.Candidates) != 0 {
		t.Fatalf("same source binding should not produce review candidate: %#v", got.Candidates)
	}
}

func TestScanFlagsSameOutcomeCrossSourceCandidate(t *testing.T) {
	entries := []CommitmentEntry{
		entry("brain/gtd/mail.md", "Send alpha budget", SourceBinding{Provider: "mail", Ref: "m1"}),
		entry("brain/gtd/todo.md", "send alpha budget", SourceBinding{Provider: "todoist", Ref: "t1"}),
	}
	got := Scan(entries, ScanOptions{})
	if len(got.Candidates) != 1 {
		t.Fatalf("candidate count = %d, want 1: %#v", len(got.Candidates), got.Candidates)
	}
	candidate := got.Candidates[0]
	if candidate.Confidence < 0.95 || candidate.Detector != "deterministic" {
		t.Fatalf("candidate = %#v", candidate)
	}
}

func TestScanUsesLLMForParaphrasedCandidate(t *testing.T) {
	entries := []CommitmentEntry{
		entry("brain/gtd/a.md", "Prepare W7-X campaign slide deck", SourceBinding{Provider: "mail", Ref: "m1", Summary: "campaign slides"}),
		entry("brain/gtd/b.md", "Draft presentation slides for W7-X campaign", SourceBinding{Provider: "github", Ref: "org/repo#2", Summary: "campaign presentation"}),
	}
	got := Scan(entries, ScanOptions{
		LLMThreshold: 0.01,
		LLM: staticReviewer{review: LLMReview{
			Same: true, Confidence: 0.91, Reasoning: "same campaign-slide outcome",
		}},
	})
	if len(got.Candidates) != 1 {
		t.Fatalf("candidate count = %d, want 1: %#v", len(got.Candidates), got.Candidates)
	}
	if got.Candidates[0].Detector != "llm" || got.Candidates[0].Reasoning == "" {
		t.Fatalf("candidate = %#v", got.Candidates[0])
	}
}

func TestKeepSeparateSuppressesFutureScan(t *testing.T) {
	a := entry("brain/gtd/mail.md", "Send alpha budget", SourceBinding{Provider: "mail", Ref: "m1"})
	b := entry("brain/gtd/todo.md", "send alpha budget", SourceBinding{Provider: "todoist", Ref: "t1"})
	id := CandidateID(a.Path, b.Path)
	MarkNotDuplicate(&a, &b, id)
	got := Scan([]CommitmentEntry{a, b}, ScanOptions{})
	if len(got.Candidates) != 0 {
		t.Fatalf("kept-separate pair resurfaced: %#v", got.Candidates)
	}
}

func TestDeferKeepsCandidateInReviewLaterState(t *testing.T) {
	a := entry("brain/gtd/mail.md", "Send alpha budget", SourceBinding{Provider: "mail", Ref: "m1"})
	b := entry("brain/gtd/todo.md", "send alpha budget", SourceBinding{Provider: "todoist", Ref: "t1"})
	id := CandidateID(a.Path, b.Path)
	MarkDeferred(&a, &b, id)
	got := Scan([]CommitmentEntry{a, b}, ScanOptions{})
	if len(got.Candidates) != 1 || got.Candidates[0].ReviewState != "deferred" {
		t.Fatalf("deferred candidate = %#v", got.Candidates)
	}
}

func TestScanIsIdempotentForUnchangedInputs(t *testing.T) {
	entries := []CommitmentEntry{
		entry("brain/gtd/mail.md", "Send alpha budget", SourceBinding{Provider: "mail", Ref: "m1"}),
		entry("brain/gtd/todo.md", "send alpha budget", SourceBinding{Provider: "todoist", Ref: "t1"}),
	}
	first := Scan(entries, ScanOptions{})
	second := Scan(entries, ScanOptions{})
	if first.Changed || second.Changed {
		t.Fatalf("scan should not report changes: first=%#v second=%#v", first, second)
	}
	if len(first.Candidates) != len(second.Candidates) || first.Candidates[0].ID != second.Candidates[0].ID {
		t.Fatalf("scan changed: first=%#v second=%#v", first, second)
	}
}

func TestApplyMergePreservesBindingsAndMarksLoser(t *testing.T) {
	winner := entry("brain/gtd/mail.md", "Send alpha budget", SourceBinding{Provider: "mail", Ref: "m1"})
	loser := entry("brain/gtd/todo.md", "send alpha budget", SourceBinding{Provider: "todoist", Ref: "t1"})
	ApplyMerge(&winner, &loser, CandidateID(winner.Path, loser.Path), "Send Alpha budget", "2026-04-29T12:00:00Z")
	if winner.Commitment.Outcome != "Send Alpha budget" || len(winner.Commitment.SourceBindings) != 2 {
		t.Fatalf("winner = %#v", winner.Commitment)
	}
	if loser.Commitment.LocalOverlay.Status != "dropped" || loser.Commitment.Dedup.EquivalentTo != winner.Path {
		t.Fatalf("loser = %#v", loser.Commitment)
	}
}

func entry(path, outcome string, binding SourceBinding) CommitmentEntry {
	return CommitmentEntry{Path: path, Commitment: Commitment{
		Kind: "commitment", Status: "next", Outcome: outcome, Title: outcome,
		SourceBindings: []SourceBinding{binding},
	}}
}
