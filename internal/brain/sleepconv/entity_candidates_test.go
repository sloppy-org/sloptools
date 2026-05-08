package sleepconv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScanWikilinkTargets(t *testing.T) {
	in := "see [[people/Anna Niggas]] and the [[projects/EUROfusion-WPTE|WPTE project]] for [[Adametz]] context"
	got := scanWikilinkTargets(in)
	want := []string{"Anna Niggas", "EUROfusion-WPTE", "Adametz"}
	if len(got) != len(want) {
		t.Fatalf("want %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("position %d: want %q, got %q", i, want[i], got[i])
		}
	}
}

func TestScanProperNouns(t *testing.T) {
	in := "Today I met Sebastian Riepl about the TU Graz cost sheet for Sascha Ranftl. Please update."
	got := scanProperNouns(in)
	if !contains(got, "Sebastian Riepl") {
		t.Errorf("missed 'Sebastian Riepl': %v", got)
	}
	if !contains(got, "TU Graz") {
		t.Errorf("missed 'TU Graz': %v", got)
	}
	if !contains(got, "Sascha Ranftl") {
		t.Errorf("missed 'Sascha Ranftl': %v", got)
	}
	for _, m := range got {
		if strings.HasPrefix(strings.ToLower(m), "today ") || strings.HasPrefix(strings.ToLower(m), "please ") {
			t.Errorf("stopword leaked: %q", m)
		}
	}
}

func TestScanProperNouns_StripsCodeBlocks(t *testing.T) {
	in := "Real prose mentions Sebastian Riepl. ```\nSome Code With Caps\n``` and `inline Code Here` end."
	got := scanProperNouns(in)
	for _, m := range got {
		if strings.Contains(m, "Code") {
			t.Errorf("code-block content leaked into proper nouns: %q (all=%v)", m, got)
		}
	}
}

func TestScanAcronyms(t *testing.T) {
	in := "Need to fix the JSON parser, then the API. Discussed at EUROFUSION and TUG meetings; OK with PDF export."
	got := scanAcronyms(in)
	for _, banned := range []string{"JSON", "API", "PDF", "OK"} {
		if contains(got, banned) {
			t.Errorf("stopword acronym %q leaked: %v", banned, got)
		}
	}
	if !contains(got, "EUROFUSION") {
		t.Errorf("real acronym 'EUROFUSION' missing: %v", got)
	}
	if !contains(got, "TUG") {
		t.Errorf("real acronym 'TUG' missing: %v", got)
	}
}

func TestExtractEntityCandidates_RankingAndFloor(t *testing.T) {
	brainRoot := t.TempDir()
	mustWrite(t, filepath.Join(brainRoot, "people", "Sebastian Riepl.md"), "# Sebastian Riepl\n")
	mustWrite(t, filepath.Join(brainRoot, "institutions", "TU Graz.md"), "# TU Graz\n")
	prompts := []Prompt{
		{Prose: "Met Sebastian Riepl at TU Graz today. Sebastian Riepl asked about Sascha Ranftl's slides."},
		{Prose: "Need to follow up with Sebastian Riepl re TU Graz cost sheet. Also check [[Adametz]]."},
		{Prose: "Single mention of Random Person should be filtered."},
	}
	got := ExtractCandidates(prompts, brainRoot)
	if len(got) == 0 {
		t.Fatal("expected at least some candidates")
	}
	// Existing-note candidates should rank first.
	if got[0].NotePath == "" {
		t.Fatalf("first candidate should have an existing note, got %+v", got[0])
	}
	// Single-mention "Random Person" without existing note must be filtered.
	for _, c := range got {
		if c.Name == "Random Person" {
			t.Errorf("single-mention non-existing entity should be filtered: %+v", c)
		}
	}
	// Wikilink-only mention "Adametz" should pass the floor.
	foundAdametz := false
	for _, c := range got {
		if c.Name == "Adametz" {
			foundAdametz = true
			if !c.FromLink {
				t.Errorf("Adametz should be marked FromLink, got %+v", c)
			}
		}
	}
	if !foundAdametz {
		t.Errorf("wikilink mention Adametz should pass the floor: %v", got)
	}
	// Sebastian Riepl mentioned 3× should be present with high count.
	foundRiepl := false
	for _, c := range got {
		if c.Name == "Sebastian Riepl" {
			foundRiepl = true
			if c.Mentions < 3 {
				t.Errorf("Sebastian Riepl should have 3 mentions, got %d", c.Mentions)
			}
			if c.NotePath == "" {
				t.Errorf("Sebastian Riepl should map to existing note: %+v", c)
			}
		}
	}
	if !foundRiepl {
		t.Errorf("Sebastian Riepl should be in candidates: %v", got)
	}
}

func TestRenderEntityCandidatesSection_EmptyOnNoCandidates(t *testing.T) {
	if RenderCandidatesSection(nil) != "" {
		t.Error("empty input should produce empty section")
	}
	if RenderCandidatesSection([]Candidate{}) != "" {
		t.Error("empty slice should produce empty section")
	}
}

func TestRenderEntityCandidatesSection_TableFormat(t *testing.T) {
	candidates := []Candidate{
		{Name: "Sebastian Riepl", Mentions: 3, NotePath: "people/Sebastian Riepl.md", NoteKind: "people", FromLink: false},
		{Name: "Adametz", Mentions: 1, FromLink: true},
		{Name: "TU Graz", Mentions: 5, NotePath: "institutions/TU Graz.md", NoteKind: "institutions"},
	}
	out := RenderCandidatesSection(candidates)
	for _, want := range []string{
		"## Entity candidates from conversation",
		"mandatory checklist",
		"Sebastian Riepl",
		"Adametz",
		"TU Graz",
		"people/Sebastian Riepl.md",
		"_(none — consider creating)_",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("section missing %q:\n%s", want, out)
		}
	}
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
