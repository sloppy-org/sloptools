package cleanup

import (
	"strings"
	"testing"
)

func TestCleanReport_TrimsPreambleBeforeFirstH1(t *testing.T) {
	in := strings.Join([]string{
		"Now I have all the evidence I need. Let me compile the resolved report.",
		"",
		"Let me refine the report.",
		"",
		"---",
		"",
		"# Scout report — Foo",
		"",
		"## Verified",
		"- a (source: x)",
	}, "\n")
	out := CleanReport(in)
	if !strings.HasPrefix(out, "# Scout report — Foo") {
		t.Fatalf("preamble not trimmed:\n%s", out)
	}
	if strings.Contains(out, "Now I have") || strings.Contains(out, "Let me refine") {
		t.Fatalf("narration leaked into output:\n%s", out)
	}
}

func TestCleanReport_TrimsTrailingMethodologyFooter(t *testing.T) {
	in := strings.Join([]string{
		"# Scout report — Bar",
		"",
		"## Verified",
		"- a (source: x)",
		"",
		"## Open questions",
		"- q1",
		"",
		"---",
		"",
		"**Note on methodology**: This report is based on the bulk-tier evidence...",
	}, "\n")
	out := CleanReport(in)
	if strings.Contains(out, "Note on methodology") {
		t.Fatalf("trailing footer not trimmed:\n%s", out)
	}
	if !strings.HasSuffix(out, "- q1") {
		t.Fatalf("expected output to end with the last bullet, got:\n%s", out)
	}
}

func TestCleanReport_TrimsBothEnds(t *testing.T) {
	in := strings.Join([]string{
		"I'm encountering permission restrictions on the MCP tools.",
		"Let me work from the bulk-tier evidence:",
		"",
		"---",
		"",
		"# Scout report — Baz",
		"",
		"## Verified",
		"- ok (source: y)",
		"",
		"---",
		"",
		"**Note**: tools were unavailable.",
	}, "\n")
	out := CleanReport(in)
	if !strings.HasPrefix(out, "# Scout report — Baz") {
		t.Fatalf("preamble not trimmed:\n%s", out)
	}
	if strings.Contains(out, "tools were unavailable") || strings.Contains(out, "permission restrictions") {
		t.Fatalf("narration leaked:\n%s", out)
	}
	if !strings.HasSuffix(out, "(source: y)") {
		t.Fatalf("expected output to end with the last bullet, got:\n%s", out)
	}
}

func TestCleanReport_KeepsHorizontalRuleIfFollowedByContent(t *testing.T) {
	// A `---` rule followed by another `## Section` (not a bold footer)
	// is a legitimate intra-document separator and must stay.
	in := strings.Join([]string{
		"# Scout report — Qux",
		"",
		"## Verified",
		"- a",
		"",
		"---",
		"",
		"## Open questions",
		"- q1",
	}, "\n")
	out := CleanReport(in)
	if !strings.Contains(out, "## Open questions") {
		t.Fatalf("legitimate `---` separator caused over-trim:\n%s", out)
	}
	if !strings.Contains(out, "- q1") {
		t.Fatalf("trailing section dropped:\n%s", out)
	}
}

func TestCleanReport_KeepsBoldInsideBullet(t *testing.T) {
	// Bullets that begin with bold (e.g., `- **SAB member identities ...**: ...`)
	// must not be mistaken for footers because they are not preceded by
	// a `---` rule.
	in := strings.Join([]string{
		"# Scout report — Boldy",
		"",
		"## Open questions",
		"- **SAB member identities remain unpublished**: no public source.",
	}, "\n")
	out := CleanReport(in)
	if !strings.Contains(out, "SAB member identities") {
		t.Fatalf("bold inside bullet was wrongly trimmed:\n%s", out)
	}
}

func TestCleanReport_NoH1ReturnsUnchanged(t *testing.T) {
	// If the body never produced an h1, leave it alone so the caller
	// (e.g., classifier) can see the raw narration and trigger
	// escalation.
	in := "I cannot find any evidence and could not be resolved."
	out := CleanReport(in)
	if out != in {
		t.Fatalf("non-report body should round-trip; got:\n%s", out)
	}
}

func TestCleanReport_NoFooterReturnsUnchanged(t *testing.T) {
	in := strings.Join([]string{
		"# Scout report — Clean",
		"",
		"## Verified",
		"- a",
		"",
		"## Open questions",
		"- q",
	}, "\n")
	out := CleanReport(in)
	if out != strings.TrimSpace(in) {
		t.Fatalf("clean body should round-trip; got:\n%s", out)
	}
}

func TestIsFooterLabel(t *testing.T) {
	for _, tc := range []struct {
		line string
		want bool
	}{
		{"**Note**: foo", true},
		{"**Note on methodology**: foo", true},
		{"**Note on tools**: foo", true},
		{"**Note on permissions**: foo", true},
		{"**Note on internal access**: foo", true}, // catch-all "note on ..."
		{"**Methodology**: foo", true},
		{"**Disclaimer**: foo", true},
		{"**SAB member identities remain unpublished**: foo", false},
		{"**Important Files**", false},
		{"## Open questions", false},
		{"- bullet", false},
	} {
		got := isFooterLabel(tc.line)
		if got != tc.want {
			t.Errorf("isFooterLabel(%q) = %v, want %v", tc.line, got, tc.want)
		}
	}
}
