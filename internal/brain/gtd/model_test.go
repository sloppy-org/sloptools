package gtd

import (
	"strings"
	"testing"
)

func TestParseCommitmentSourceBindingsAndOverlay(t *testing.T) {
	src := `---
title: Review W7-X plots
status: next
follow_up: 2026-05-01
due: 2026-05-10
actor: me
labels: [paper, plasma]
source_bindings:
  - provider: GitHub
    ref: sloppy-org/slopshell#740
    url: https://github.com/sloppy-org/slopshell/issues/740
    writeable: true
    authoritative_for: [title, state]
local_overlay:
  status: closed
  closed_at: 2026-04-29T11:23:00Z
  closed_via: sls
---
# Context

Free prose.
`

	commitment, _, diags := ParseCommitmentMarkdown(src)
	if len(diags) != 0 {
		t.Fatalf("ParseCommitmentMarkdown() diagnostics: %v", diags)
	}
	if commitment.Title != "Review W7-X plots" || commitment.Status != "next" {
		t.Fatalf("unexpected commitment: %#v", commitment)
	}
	if len(commitment.SourceBindings) != 1 {
		t.Fatalf("source bindings = %#v", commitment.SourceBindings)
	}
	binding := commitment.SourceBindings[0]
	if binding.Provider != "github" || binding.StableID() != "github:sloppy-org/slopshell#740" {
		t.Fatalf("unexpected binding: %#v", binding)
	}
	if !binding.Writeable || len(binding.AuthoritativeFor) != 2 {
		t.Fatalf("binding fields lost: %#v", binding)
	}
	if commitment.LocalOverlay.Status != "closed" || commitment.LocalOverlay.ClosedVia != "sls" {
		t.Fatalf("overlay = %#v", commitment.LocalOverlay)
	}
}

func TestParseCommitmentLegacySourceRefs(t *testing.T) {
	src := `---
title: Legacy task
source_refs:
  - meetings:work:alpha:2026-04-29
  - plain-local-ref
---
Body.
`

	commitment, _, diags := ParseCommitmentMarkdown(src)
	if len(diags) != 0 {
		t.Fatalf("ParseCommitmentMarkdown() diagnostics: %v", diags)
	}
	if len(commitment.LegacySources) != 2 || len(commitment.SourceBindings) != 2 {
		t.Fatalf("legacy conversion failed: %#v", commitment)
	}
	if got := commitment.SourceBindings[0].StableID(); got != "meetings:work:alpha:2026-04-29" {
		t.Fatalf("first binding id = %q", got)
	}
	if got := commitment.SourceBindings[1].StableID(); got != "manual:plain-local-ref" {
		t.Fatalf("second binding id = %q", got)
	}
}

func TestApplyCommitmentPreservesProseAndAddsNewSchema(t *testing.T) {
	src := `---
title: Preserve me
source_refs:
  - todoist:123
---
Intro prose.

# Checklist

- [ ] one
`

	commitment, note, diags := ParseCommitmentMarkdown(src)
	if len(diags) != 0 {
		t.Fatalf("ParseCommitmentMarkdown() diagnostics: %v", diags)
	}
	commitment.LocalOverlay = LocalOverlay{Status: "closed", ClosedVia: "cli"}
	if err := ApplyCommitment(note, *commitment); err != nil {
		t.Fatalf("ApplyCommitment() error: %v", err)
	}
	rendered, err := note.Render()
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}
	for _, want := range []string{
		"source_bindings:",
		"provider: todoist",
		"ref: \"123\"",
		"local_overlay:",
		"closed_via: cli",
		"Intro prose.",
		"- [ ] one",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered note missing %q:\n%s", want, rendered)
		}
	}
}

func TestCommitmentDedupHints(t *testing.T) {
	commitment := Commitment{
		Title: "  Review   W7-X plots ",
		SourceBindings: []SourceBinding{
			{Provider: "GitHub", Ref: "org/repo#1"},
			{Provider: "github", Ref: "org/repo#1"},
			{Provider: "todoist", Ref: "abc"},
		},
	}
	hints := commitment.DedupHints()
	want := []string{"github:org/repo#1", "todoist:abc", "review w7-x plots"}
	if strings.Join(hints, "|") != strings.Join(want, "|") {
		t.Fatalf("hints = %#v, want %#v", hints, want)
	}
}
