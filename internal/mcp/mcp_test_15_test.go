package mcp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sloppy-org/sloptools/internal/brain"
)

func TestBrainNoteParseToolReturnsStructuredSourcePaths(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	notePath := filepath.Join("brain", "folders", "project.md")
	writeMCPBrainFile(t, filepath.Join(tmp, "work", notePath), `---
kind: folder
vault: nextcloud
sphere: work
source_folder: project
status: stale
projects: []
people: []
institutions: []
topics: []
---
# project

## Summary
Summary.

## Key Facts
- Source folder: project

## Important Files
- None.

## Related Folders
- None.

## Related Notes
- None.

## Notes
Free prose.

## Open Questions
- None.
`)

	s := NewServer(t.TempDir())
	got, err := s.callTool("brain.note.parse", map[string]interface{}{
		"config_path": configPath,
		"sphere":      "work",
		"path":        notePath,
	})
	if err != nil {
		t.Fatalf("brain.note.parse: %v", err)
	}
	if got["kind"] != "folder" {
		t.Fatalf("kind = %v, want folder: %#v", got["kind"], got)
	}
	source := got["source"].(brain.ResolvedPath)
	if source.Rel != notePath {
		t.Fatalf("source rel = %q, want %q", source.Rel, notePath)
	}
	folder := got["folder"].(brain.FolderNote)
	if folder.SourceFolder != "project" {
		t.Fatalf("folder = %#v", folder)
	}
}

func TestBrainNoteValidateToolReportsDiagnostics(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	notePath := filepath.Join("brain", "glossary", "ntv.md")
	writeMCPBrainFile(t, filepath.Join(tmp, "work", notePath), `---
kind: glossary
display_name: NTV
aliases: []
sphere: work
canonical_topic: "[[people/Ada]]"
---
# NTV

## Definition
Neoclassical toroidal viscosity.
`)

	s := NewServer(t.TempDir())
	got, err := s.callTool("brain.note.validate", map[string]interface{}{
		"config_path": configPath,
		"sphere":      "work",
		"path":        notePath,
	})
	if err != nil {
		t.Fatalf("brain.note.validate: %v", err)
	}
	if got["valid"] != false {
		t.Fatalf("valid = %v, want false: %#v", got["valid"], got)
	}
	if got["count"] == 0 {
		t.Fatalf("expected diagnostics: %#v", got)
	}
	source := got["source"].(brain.ResolvedPath)
	if source.Rel != notePath {
		t.Fatalf("source rel = %q, want %q", source.Rel, notePath)
	}
}

func TestBrainLinksResolveToolRejectsGuardrail(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	secretPath := filepath.Join("personal", "secret.md")
	writeMCPBrainFile(t, filepath.Join(tmp, "work", secretPath), "secret\n")

	s := NewServer(t.TempDir())
	_, err := s.callTool("brain.links.resolve", map[string]interface{}{
		"config_path": configPath,
		"sphere":      "work",
		"path":        secretPath,
		"link":        "../people/alice.md",
	})
	if err == nil {
		t.Fatal("expected guardrail failure")
	}
	if got := brain.KindOf(err); got != brain.ErrorExcludedPath {
		t.Fatalf("KindOf(err) = %q, want excluded_path; err=%v", got, err)
	}
}

func TestBrainVaultValidateToolSummarizesNotes(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	writeMCPBrainFile(t, filepath.Join(tmp, "work", "brain", "folders", "project.md"), `---
kind: folder
vault: nextcloud
sphere: work
source_folder: project
status: stale
projects: []
people: []
institutions: []
topics: []
---
# project

## Summary
Summary.

## Key Facts
- Source folder: project

## Important Files
- None.

## Related Folders
- None.

## Related Notes
- None.

## Notes
Free prose.

## Open Questions
- None.
`)
	writeMCPBrainFile(t, filepath.Join(tmp, "work", "brain", "glossary", "ntv.md"), `---
kind: glossary
display_name: NTV
aliases: []
sphere: work
canonical_topic: "[[people/Ada]]"
---
# NTV

## Definition
Neoclassical toroidal viscosity.
`)

	s := NewServer(t.TempDir())
	got, err := s.callTool("brain.vault.validate", map[string]interface{}{
		"config_path": configPath,
		"sphere":      "work",
	})
	if err != nil {
		t.Fatalf("brain.vault.validate: %v", err)
	}
	if got["count"] != 2 {
		t.Fatalf("count = %v, want 2: %#v", got["count"], got)
	}
	if got["issues"] == 0 {
		t.Fatalf("expected vault issues: %#v", got)
	}
	notes, _ := got["notes"].([]map[string]interface{})
	if len(notes) != 2 {
		t.Fatalf("notes = %#v", got["notes"])
	}
}

func TestBrainSearchToolReturnsStructuredResults(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	writeMCPBrainFile(t, filepath.Join(tmp, "work", "brain", "projects", "alpha.md"), "needle\n")
	writeMCPBrainFile(t, filepath.Join(tmp, "work", "personal", "secret.md"), "needle\n")

	s := NewServer(t.TempDir())
	got, err := s.callTool("brain_search", map[string]interface{}{
		"config_path": configPath,
		"sphere":      "work",
		"query":       "needle",
	})
	if err != nil {
		t.Fatalf("brain_search: %v", err)
	}
	if got["count"] != 1 {
		t.Fatalf("count = %v, want 1: %#v", got["count"], got)
	}
	if got["results"] == nil {
		t.Fatalf("missing results")
	}
}

func TestBrainBacklinksToolFindsLinks(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	writeMCPBrainFile(t, filepath.Join(tmp, "work", "brain", "people", "alice.md"), "Alice\n")
	writeMCPBrainFile(t, filepath.Join(tmp, "work", "brain", "projects", "project.md"), "[Alice](../people/alice.md)\n")

	s := NewServer(t.TempDir())
	got, err := s.callTool("brain_backlinks", map[string]interface{}{
		"config_path": configPath,
		"sphere":      "work",
		"target":      "people/alice.md",
	})
	if err != nil {
		t.Fatalf("brain_backlinks: %v", err)
	}
	if got["count"] != 1 {
		t.Fatalf("count = %v, want 1: %#v", got["count"], got)
	}
}

func writeMCPBrainConfig(t *testing.T, root string) string {
	t.Helper()
	path := filepath.Join(root, "vaults.toml")
	body := `[[vault]]
sphere = "work"
root = "` + filepath.ToSlash(filepath.Join(root, "work")) + `"
brain = "brain"

[[vault]]
sphere = "private"
root = "` + filepath.ToSlash(filepath.Join(root, "private")) + `"
brain = "brain"
`
	writeMCPBrainFile(t, path, body)
	return path
}

func writeMCPBrainFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}
