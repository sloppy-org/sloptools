package meetings

import "testing"

func TestResolvePersonAppliesAliasThenCandidateMatch(t *testing.T) {
	candidates := []string{"Christopher Albert", "Ada Lovelace", "Charles Babbage"}
	aliases := map[string]string{"chris": "Christopher Albert"}

	if got := ResolvePerson("Chris", aliases, candidates); got != "Christopher Albert" {
		t.Fatalf("alias resolution = %q", got)
	}
	if got := ResolvePerson("ada", aliases, candidates); got != "Ada Lovelace" {
		t.Fatalf("single-token resolution = %q", got)
	}
	if got := ResolvePerson("Charlës (PhD)", aliases, candidates); got != "Charles Babbage" {
		t.Fatalf("ascii-fold + parenthetical strip = %q", got)
	}
}

func TestResolvePersonAmbiguousTokenFallsBackToInput(t *testing.T) {
	candidates := []string{"Christopher Albert", "Christine Norman"}
	got := ResolvePerson("chris", nil, candidates)
	if got != "chris" {
		t.Fatalf("ambiguous single-token must return input, got %q", got)
	}
}

func TestResolvePersonEmptyInputReturnsEmpty(t *testing.T) {
	if got := ResolvePerson("  ", nil, nil); got != "" {
		t.Fatalf("empty input = %q", got)
	}
}

func TestParseLegacyRefAcceptsImporterFormat(t *testing.T) {
	key, ok := ParseLegacyRef("work:2026-04-29-standup:Ada Lovelace:abcd1234")
	if !ok {
		t.Fatal("expected parse ok")
	}
	if key.Slug != "2026-04-29-standup" || key.Person != "ada lovelace" {
		t.Fatalf("legacy key = %#v", key)
	}
}

func TestParseLegacyRefRejectsNewStableID(t *testing.T) {
	if _, ok := ParseLegacyRef("standup#abcd123456"); ok {
		t.Fatal("stable IDs must not parse as legacy refs")
	}
}
