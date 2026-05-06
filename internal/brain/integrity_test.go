package brain

import (
	"strings"
	"testing"
	"time"
)

// TestIntegrityScanCountsBrokenLinks builds a single-note vault with a known
// broken wikilink and verifies the scanner counts it and surfaces an example.
func TestIntegrityScanCountsBrokenLinks(t *testing.T) {
	cfg := testConfig(t)
	now := time.Now()
	writeBrainNote(t, cfg, SphereWork, "brain/folders/x.md", `---
kind: folder
source_folder: x
status: active
projects: []
people: []
institutions: []
topics: []
---

# x

## Summary
Body that links to [[topics/missing-target]] which does not exist.

## Key Facts
- one
- two

## Important Files
- None.

## Related Folders
- None.

## Related Notes
- None.

## Notes
- None.

## Open Questions
- None.
`, now)

	report, err := IntegrityScan(cfg, SphereWork)
	if err != nil {
		t.Fatalf("IntegrityScan: %v", err)
	}
	if report.Notes != 1 {
		t.Fatalf("notes = %d, want 1", report.Notes)
	}
	if report.BrokenLinks < 1 {
		t.Fatalf("broken_links = %d, want >= 1; report=%+v", report.BrokenLinks, report)
	}
	if len(report.LinkExamples) == 0 || !strings.Contains(report.LinkExamples[0], "missing-target") {
		t.Fatalf("link_examples missing concrete target: %+v", report.LinkExamples)
	}
}

// TestCompareIntegrityFlagsOnlyRegressions checks that a clean apply run that
// does not introduce new broken links is not flagged as a regression even when
// before/after both contained pre-existing broken links.
func TestCompareIntegrityFlagsOnlyRegressions(t *testing.T) {
	before := IntegrityReport{BrokenLinks: 3, TotalIssues: 5}
	after := IntegrityReport{BrokenLinks: 3, TotalIssues: 5}
	if reg := CompareIntegrity(before, after); reg.IsRegression() {
		t.Fatalf("steady-state baseline must not be a regression: %+v", reg)
	}
	worse := IntegrityReport{BrokenLinks: 4, TotalIssues: 6}
	reg := CompareIntegrity(before, worse)
	if !reg.IsRegression() {
		t.Fatalf("expected regression; got %+v", reg)
	}
	if reg.NewBrokenLinks != 1 || reg.NewIssues != 1 {
		t.Fatalf("delta wrong: %+v", reg)
	}
	better := IntegrityReport{BrokenLinks: 1, TotalIssues: 2}
	if reg := CompareIntegrity(before, better); reg.IsRegression() {
		t.Fatalf("improvement must not be a regression: %+v", reg)
	}
}
