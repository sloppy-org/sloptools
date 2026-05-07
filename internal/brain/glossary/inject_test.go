package glossary

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupGlossaryFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gloss := filepath.Join(dir, "brain", "glossary")
	if err := os.MkdirAll(gloss, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	files := map[string]string{
		"1-nu-transport.md": `---
kind: glossary
display_name: 1/ν transport
aliases:
  - 1/ν transport
  - 1/nu transport
  - neoclassical 1/ν
sphere: work
canonical_topic: "[[topics/1-nu-transport]]"
do_not_confuse_with:
  - neutrino transport
  - neutron transport
---

# 1/ν transport

## Definition

Neoclassical-transport regime in stellarator plasmas where the radial particle and energy fluxes scale as 1/ν with the collision frequency ν. Distinct from neutrino or neutron transport (different physics entirely).
`,
		"cxrs.md": `---
kind: glossary
display_name: CXRS
aliases:
  - CXRS
  - charge exchange recombination spectroscopy
sphere: work
canonical_topic: "[[topics/plasma-diagnostics]]"
do_not_confuse_with: []
---

# CXRS

## Definition

CXRS means charge exchange recombination spectroscopy in the AUG diagnostic and plasma-profile context of this vault.
`,
		"index.md": `# index — should NOT be matched`,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(gloss, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}

func TestRelevantTerms_NeoRTNote(t *testing.T) {
	root := setupGlossaryFixture(t)
	body := "In our work, the [[projects/NEO-RT]] code computes neoclassical 1/ν transport in the banana regime."
	terms := RelevantTerms(root, body)
	if len(terms) != 1 {
		t.Fatalf("want 1 term, got %d: %+v", len(terms), terms)
	}
	if terms[0].DisplayName != "1/ν transport" {
		t.Fatalf("display = %q, want '1/ν transport'", terms[0].DisplayName)
	}
	if !strings.Contains(terms[0].Definition, "Neoclassical-transport regime") {
		t.Fatalf("definition missing expected prose: %q", terms[0].Definition)
	}
	if len(terms[0].DoNotConfuseWith) != 2 {
		t.Fatalf("DoNotConfuseWith count = %d, want 2", len(terms[0].DoNotConfuseWith))
	}
}

func TestRelevantTerms_RanksLongestSurfaceFirst(t *testing.T) {
	root := setupGlossaryFixture(t)
	body := "We use CXRS via charge exchange recombination spectroscopy. Also some 1/nu transport content."
	terms := RelevantTerms(root, body)
	if len(terms) != 2 {
		t.Fatalf("want 2 terms, got %d: %+v", len(terms), terms)
	}
	// Longest matched alias wins.
	if terms[0].MatchedSurface == "CXRS" {
		t.Fatalf("ranking failed: longer 'charge exchange recombination spectroscopy' should beat 'CXRS'; got first=%q", terms[0].MatchedSurface)
	}
}

func TestRelevantTerms_NoMatchOnUnknownText(t *testing.T) {
	root := setupGlossaryFixture(t)
	terms := RelevantTerms(root, "Plain English with no domain terms.")
	if len(terms) != 0 {
		t.Fatalf("expected zero matches, got %d: %+v", len(terms), terms)
	}
}

func TestRelevantTerms_WordBoundaryAvoidsFalseMatch(t *testing.T) {
	root := setupGlossaryFixture(t)
	// "CXRSAvoidance" is not CXRS; the boundary check should reject it.
	terms := RelevantTerms(root, "CXRSAvoidance is not the same as CXRS proper.")
	if len(terms) != 1 {
		t.Fatalf("want 1 term, got %d: %+v", len(terms), terms)
	}
	if terms[0].MatchedSurface != "CXRS" {
		t.Fatalf("matched surface = %q, want CXRS", terms[0].MatchedSurface)
	}
}

func TestRelevantTerms_IndexMdSkipped(t *testing.T) {
	root := setupGlossaryFixture(t)
	terms := RelevantTerms(root, "Looking for an index entry here.")
	for _, term := range terms {
		if filepath.Base(term.File) == "index.md" {
			t.Fatalf("index.md should not be matched: %+v", term)
		}
	}
}

func TestFormatPacketSection_RendersCleanly(t *testing.T) {
	root := setupGlossaryFixture(t)
	body := "neoclassical 1/ν transport"
	section := FormatPacketSection(RelevantTerms(root, body))
	if !strings.HasPrefix(section, "## Glossary context\n") {
		t.Fatalf("section missing header:\n%s", section)
	}
	if !strings.Contains(section, "**1/ν transport**") {
		t.Fatalf("section missing bold display name:\n%s", section)
	}
	if !strings.Contains(section, "do not confuse with: neutrino transport") {
		t.Fatalf("section missing do_not_confuse_with caveat:\n%s", section)
	}
	if !strings.Contains(section, "brain/glossary/1-nu-transport.md") {
		t.Fatalf("section missing vault-relative path:\n%s", section)
	}
}

func TestFormatPacketSection_EmptyReturnsEmpty(t *testing.T) {
	if got := FormatPacketSection(nil); got != "" {
		t.Fatalf("empty input should produce empty output, got %q", got)
	}
}

func TestRelevantTerms_RespectsMaxTermBytes(t *testing.T) {
	root := setupGlossaryFixture(t)
	long := strings.Repeat("A ", 800)
	if err := os.WriteFile(filepath.Join(root, "brain", "glossary", "long.md"), []byte(`---
kind: glossary
display_name: Long Term
aliases:
  - LongTermXYZ
sphere: work
---

# Long Term

## Definition

`+long+`
`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Force cache invalidation by bumping the directory mtime.
	bump := filepath.Join(root, "brain", "glossary", "trigger.tmp")
	_ = os.WriteFile(bump, []byte("x"), 0o644)
	_ = os.Remove(bump)
	terms := RelevantTerms(root, "We mention LongTermXYZ here.")
	if len(terms) != 1 {
		t.Fatalf("want 1 term, got %d", len(terms))
	}
	if len(terms[0].Definition) > MaxTermBytes {
		t.Fatalf("definition not capped: %d bytes (limit %d)", len(terms[0].Definition), MaxTermBytes)
	}
}
