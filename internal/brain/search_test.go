package brain

import (
	"context"
	"path/filepath"
	"testing"
)

func TestSearchSelectsVaultAndAppliesExcludes(t *testing.T) {
	cfg := searchTestConfig(t, []string{"brain/archive"})
	writeSearchVaultFile(t, cfg, SphereWork, "brain/projects/alpha.md", "work needle\n")
	writeSearchVaultFile(t, cfg, SphereWork, "brain/archive/old.md", "archived needle\n")
	writeSearchVaultFile(t, cfg, SphereWork, "personal/secret.md", "personal needle\n")
	writeSearchVaultFile(t, cfg, SpherePrivate, "brain/projects/private.md", "private needle\n")

	got, err := Search(context.Background(), cfg, SearchOptions{Sphere: SphereWork, Query: "needle"})
	if err != nil {
		t.Fatalf("Search(work): %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("work result count = %d, want 1: %#v", len(got), got)
	}
	if got[0].Rel != "brain/projects/alpha.md" {
		t.Fatalf("work rel = %q", got[0].Rel)
	}
	if got[0].NoteType != "projects" {
		t.Fatalf("note type = %q, want projects", got[0].NoteType)
	}

	private, err := Search(context.Background(), cfg, SearchOptions{Sphere: SpherePrivate, Query: "needle"})
	if err != nil {
		t.Fatalf("Search(private): %v", err)
	}
	if len(private) != 1 || private[0].Rel != "brain/projects/private.md" {
		t.Fatalf("private results = %#v", private)
	}
}

func TestSearchRegexWikilinksMarkdownLinksAndAliases(t *testing.T) {
	cfg := searchTestConfig(t, nil)
	writeSearchVaultFile(t, cfg, SphereWork, "brain/people/alice.md", `---
aliases:
  - Alice A. Smith
---
needle-42
`)
	writeSearchVaultFile(t, cfg, SphereWork, "brain/projects/project.md", "Meet [[people/alice|Alice]] and [Alice](../people/alice.md).\n")

	regex, err := Search(context.Background(), cfg, SearchOptions{Sphere: SphereWork, Query: `needle-[0-9]+`, Mode: SearchRegex})
	if err != nil {
		t.Fatalf("regex search: %v", err)
	}
	if len(regex) != 1 || regex[0].Rel != "brain/people/alice.md" {
		t.Fatalf("regex results = %#v", regex)
	}

	wiki, err := Search(context.Background(), cfg, SearchOptions{Sphere: SphereWork, Query: "people/alice", Mode: SearchWikilink})
	if err != nil {
		t.Fatalf("wikilink search: %v", err)
	}
	if len(wiki) != 1 || wiki[0].Rel != "brain/projects/project.md" {
		t.Fatalf("wikilink results = %#v", wiki)
	}

	md, err := Search(context.Background(), cfg, SearchOptions{Sphere: SphereWork, Query: "alice.md", Mode: SearchMarkdownLink})
	if err != nil {
		t.Fatalf("markdown link search: %v", err)
	}
	if len(md) != 1 || md[0].Rel != "brain/projects/project.md" {
		t.Fatalf("markdown link results = %#v", md)
	}

	aliases, err := Search(context.Background(), cfg, SearchOptions{Sphere: SphereWork, Query: "A. Smith", Mode: SearchAlias})
	if err != nil {
		t.Fatalf("alias search: %v", err)
	}
	if len(aliases) != 1 || aliases[0].Rel != "brain/people/alice.md" || aliases[0].Line != 3 {
		t.Fatalf("alias results = %#v", aliases)
	}
}

func TestBacklinksFindWikilinksAndMarkdownLinks(t *testing.T) {
	cfg := searchTestConfig(t, []string{"brain/archive"})
	writeSearchVaultFile(t, cfg, SphereWork, "brain/people/alice.md", "Alice\n")
	writeSearchVaultFile(t, cfg, SphereWork, "brain/projects/project.md", "Meet [[people/alice|Alice]] and [Alice](../people/alice.md).\n")
	writeSearchVaultFile(t, cfg, SphereWork, "brain/archive/old.md", "[Alice](../people/alice.md)\n")

	got, err := Backlinks(context.Background(), cfg, BacklinkOptions{Sphere: SphereWork, Target: "people/alice.md"})
	if err != nil {
		t.Fatalf("Backlinks: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("backlink count = %d, want 2: %#v", len(got), got)
	}
	for _, result := range got {
		if result.Rel != "brain/projects/project.md" {
			t.Fatalf("backlink rel = %q", result.Rel)
		}
	}
	if got[0].Why != "markdown_link:../people/alice.md" || got[1].Why != "wikilink:people/alice|Alice" {
		t.Fatalf("backlink why = %q, %q", got[0].Why, got[1].Why)
	}

	roundTrip, err := Backlinks(context.Background(), cfg, BacklinkOptions{Sphere: SphereWork, Target: "brain/people/alice.md"})
	if err != nil {
		t.Fatalf("Backlinks with vault-relative target: %v", err)
	}
	if len(roundTrip) != len(got) {
		t.Fatalf("round-trip count = %d, want %d", len(roundTrip), len(got))
	}
}

func TestBacklinksRejectWorkPersonalTarget(t *testing.T) {
	cfg := searchTestConfig(t, nil)
	secret := writeSearchVaultFile(t, cfg, SphereWork, "personal/secret.md", "secret\n")

	_, err := Backlinks(context.Background(), cfg, BacklinkOptions{Sphere: SphereWork, Target: secret})
	if KindOf(err) != ErrorExcludedPath {
		t.Fatalf("Backlinks kind = %q, err %v", KindOf(err), err)
	}
	_, err = Backlinks(context.Background(), cfg, BacklinkOptions{Sphere: SphereWork, Target: "personal/secret.md"})
	if KindOf(err) != ErrorExcludedPath {
		t.Fatalf("relative Backlinks kind = %q, err %v", KindOf(err), err)
	}
}

func writeSearchVaultFile(t *testing.T, cfg *Config, sphere Sphere, rel, content string) string {
	t.Helper()
	vault := cfg.mustVault(t, sphere)
	path := filepath.Join(vault.Root, filepath.FromSlash(rel))
	writeFile(t, path, content)
	return path
}

func searchTestConfig(t *testing.T, workExcludes []string) *Config {
	t.Helper()
	root := t.TempDir()
	cfg, err := NewConfig([]Vault{
		{Sphere: SphereWork, Root: filepath.Join(root, "work"), Brain: "brain", Exclude: workExcludes},
		{Sphere: SpherePrivate, Root: filepath.Join(root, "private"), Brain: "brain"},
	})
	if err != nil {
		t.Fatalf("NewConfig() error: %v", err)
	}
	return cfg
}
