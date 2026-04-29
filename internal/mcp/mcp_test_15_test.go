package mcp

import (
	"os"
	"path/filepath"
	"testing"
)

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
