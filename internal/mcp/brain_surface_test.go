package mcp

import (
	"testing"

	"github.com/sloppy-org/sloptools/internal/surface"
)

func TestBrainSurfaceExportsLockedMutatingTools(t *testing.T) {
	want := []string{
		"brain.note.parse",
		"brain.note.validate",
		"brain.note.write",
		"brain.vault.validate",
		"brain.links.resolve",
		"brain.search",
		"brain.backlinks",
		"brain.gtd.parse",
		"brain.gtd.list",
		"brain.projects.render",
		"brain.projects.list",
		"brain.gtd.write",
		"brain.gtd.bulk_link",
		"brain.gtd.organize",
		"brain.gtd.resurface",
		"brain.gtd.dashboard",
		"brain.gtd.review_batch",
		"brain.gtd.ingest",
	}
	names := make(map[string]bool, len(surface.MCPTools))
	for _, tool := range surface.MCPTools {
		names[tool.Name] = true
	}
	for _, name := range want {
		if !names[name] {
			t.Fatalf("surface missing required brain tool %q", name)
		}
	}
}
