package gtd

import (
	"strings"
	"testing"

	"github.com/sloppy-org/sloptools/internal/brain"
)

func TestValidateCommitmentAcceptsCoreStatuses(t *testing.T) {
	statuses := []struct {
		status string
		extra  string
	}{
		{"inbox", ""},
		{"next", "next_action: Email the signed form\n"},
		{"waiting", "waiting_for: Ada Lovelace\n"},
		{"deferred", "follow_up: 2026-05-01\n"},
		{"someday", ""},
		{"maybe_stale", ""},
		{"closed", ""},
		{"done", ""},
		{"dropped", ""},
	}
	for _, tc := range statuses {
		result := ParseAndValidate(commitmentFixture("status: " + tc.status + "\n" + tc.extra))
		if len(result.Diagnostics) != 0 {
			t.Fatalf("%s diagnostics: %v", tc.status, result.Diagnostics)
		}
	}
}

func TestValidateCommitmentRejectsDueAsDeferredStart(t *testing.T) {
	result := ParseAndValidate(commitmentFixture("status: deferred\ndue: 2026-05-01\n"))
	assertDiagnosticContains(t, result.Diagnostics, "deferred commitments require follow_up")
	assertDiagnosticContains(t, result.Diagnostics, "due is only a hard deadline")
}

func TestValidateCommitmentRejectsBadDatesAndMissingSource(t *testing.T) {
	src := strings.ReplaceAll(commitmentFixture("status: next\nnext_action: Reply\nfollow_up: soon\n"), "source_refs:\n  - mail:work:abc\n", "")
	result := ParseAndValidate(src)
	assertDiagnosticContains(t, result.Diagnostics, "follow_up must be YYYY-MM-DD")
	assertDiagnosticContains(t, result.Diagnostics, "source_bindings or legacy source_refs is required")
}

func TestValidateCommitmentRejectsMissingSections(t *testing.T) {
	src := `---
kind: commitment
sphere: work
status: inbox
outcome: Triage new item
context: review
source_refs:
  - manual:abc
---
## Summary
Only one section.
`
	result := ParseAndValidate(src)
	assertDiagnosticContains(t, result.Diagnostics, `missing required section "Next Action"`)
	assertDiagnosticContains(t, result.Diagnostics, `missing required section "Evidence"`)
}

func TestParseAndValidateReturnsStructuredCommitment(t *testing.T) {
	result := ParseAndValidate(commitmentFixture("status: next\nnext_action: Reply to Ada\nfollow_up: 2026-05-02\n"))
	if len(result.Diagnostics) != 0 {
		t.Fatalf("diagnostics: %v", result.Diagnostics)
	}
	if result.Commitment.Status != "next" || result.Commitment.NextAction != "Reply to Ada" {
		t.Fatalf("commitment = %#v", result.Commitment)
	}
	if len(result.Commitment.SourceBindings) != 1 || result.Commitment.SourceBindings[0].Provider != "mail" {
		t.Fatalf("source binding = %#v", result.Commitment.SourceBindings)
	}
}

func commitmentFixture(extra string) string {
	return `---
kind: commitment
sphere: work
outcome: Send report
context: email
review_state: ok
source_refs:
  - mail:work:abc
` + extra + `---
# Send report

## Summary
Short summary.

## Next Action
- [ ] Decide next move.

## Evidence
- mail:work:abc

## Linked Items
- None.

## Review Notes
- None.
`
}

func assertDiagnosticContains(t *testing.T, diags []brain.MarkdownDiagnostic, want string) {
	t.Helper()
	for _, diag := range diags {
		if strings.Contains(diag.Error(), want) {
			return
		}
	}
	t.Fatalf("diagnostic containing %q missing from %v", want, diags)
}
