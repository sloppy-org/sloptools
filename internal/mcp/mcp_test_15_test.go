package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sloppy-org/sloptools/internal/brain"
	braingtd "github.com/sloppy-org/sloptools/internal/brain/gtd"
)

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

func TestBrainLinksResolveToolResolvesValidRelativeLink(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	sourcePath := filepath.Join("brain", "projects", "alpha.md")
	targetPath := filepath.Join("brain", "people", "ada.md")
	writeMCPBrainFile(t, filepath.Join(tmp, "work", sourcePath), "[Ada](../people/ada.md)\n")
	writeMCPBrainFile(t, filepath.Join(tmp, "work", targetPath), "# Ada\n")

	s := NewServer(t.TempDir())
	got, err := s.callTool("brain.links.resolve", map[string]interface{}{
		"config_path": configPath,
		"sphere":      "work",
		"path":        sourcePath,
		"link":        "../people/ada.md",
	})
	if err != nil {
		t.Fatalf("brain.links.resolve: %v", err)
	}
	resolved := got["resolved"].(brain.ResolvedPath)
	if resolved.Rel != targetPath {
		t.Fatalf("resolved rel = %q, want %q", resolved.Rel, targetPath)
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
	writeMCPBrainFile(t, filepath.Join(tmp, "work", "brain", "projects", "ada.md"), `---
kind: project
focus: active
cadence: weekly
strategic: true
enjoyment: 3
---
# Ada
`)

	s := NewServer(t.TempDir())
	got, err := s.callTool("brain.vault.validate", map[string]interface{}{
		"config_path": configPath,
		"sphere":      "work",
	})
	if err != nil {
		t.Fatalf("brain.vault.validate: %v", err)
	}
	if got["count"] != 3 {
		t.Fatalf("count = %v, want 3: %#v", got["count"], got)
	}
	if got["issues"] == 0 {
		t.Fatalf("expected vault issues: %#v", got)
	}
	notes, _ := got["notes"].([]map[string]interface{})
	if len(notes) != 3 {
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

func TestBrainNoteWriteToolPreservesProseAndSections(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	notePath := filepath.Join("brain", "notes", "alpha.md")
	writeMCPBrainFile(t, filepath.Join(tmp, "work", notePath), `---
title: Alpha
review_state: needs_evidence
---
Intro prose.

## Details
- one
`)

	s := NewServer(t.TempDir())
	got, err := s.callTool("brain.note.write", map[string]interface{}{
		"config_path": configPath,
		"sphere":      "work",
		"path":        notePath,
		"fields": map[string]interface{}{
			"frontmatter": map[string]interface{}{
				"title":        "Beta",
				"review_state": "ok",
			},
			"sections": map[string]interface{}{
				"Details": "- two",
			},
		},
	})
	if err != nil {
		t.Fatalf("brain.note.write: %v", err)
	}
	if got["valid"] != true {
		t.Fatalf("valid = %v, want true: %#v", got["valid"], got)
	}
	updated, err := os.ReadFile(filepath.Join(tmp, "work", notePath))
	if err != nil {
		t.Fatalf("read updated note: %v", err)
	}
	for _, want := range []string{"Intro prose.", "title: Beta", "review_state: ok", "- two"} {
		if !strings.Contains(string(updated), want) {
			t.Fatalf("updated note missing %q:\n%s", want, string(updated))
		}
	}
}

func TestBrainGTDWriteAndResurfaceToolsMutateCommitmentNotes(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	notePath := filepath.Join("brain", "gtd", "task.md")
	writeMCPBrainFile(t, filepath.Join(tmp, "work", notePath), `---
kind: commitment
sphere: work
title: Reply to Ada
status: next
context: email
source_bindings:
  - provider: mail
    ref: m1
local_overlay:
  status: deferred
  closed_via: cli
follow_up: 2026-04-29
---
Intro prose.

## Summary
Send the reply.

## Next Action
- [ ] Send the reply.

## Evidence
- mail:m1

## Linked Items
- None.

## Review Notes
- None.
`)

	s := NewServer(t.TempDir())
	got, err := s.callTool("brain.gtd.write", map[string]interface{}{
		"config_path": configPath,
		"sphere":      "work",
		"path":        notePath,
		"commitment": map[string]interface{}{
			"status":  "closed",
			"outcome": "Reply sent",
		},
	})
	if err != nil {
		t.Fatalf("brain.gtd.write: %v", err)
	}
	if got["valid"] != true {
		t.Fatalf("valid = %v, want true: %#v", got["valid"], got)
	}
	updated, err := os.ReadFile(filepath.Join(tmp, "work", notePath))
	if err != nil {
		t.Fatalf("read updated GTD note: %v", err)
	}
	if !strings.Contains(string(updated), "Intro prose.") || !strings.Contains(string(updated), "outcome: Reply sent") {
		t.Fatalf("updated GTD note lost prose or outcome:\n%s", string(updated))
	}
	resurface, err := s.callTool("brain.gtd.resurface", map[string]interface{}{
		"config_path": configPath,
		"sphere":      "work",
		"path":        notePath,
	})
	if err != nil {
		t.Fatalf("brain.gtd.resurface: %v", err)
	}
	if resurface["count"] != 1 {
		t.Fatalf("resurface count = %v, want 1: %#v", resurface["count"], resurface)
	}
	refreshed, err := os.ReadFile(filepath.Join(tmp, "work", notePath))
	if err != nil {
		t.Fatalf("read resurfaced note: %v", err)
	}
	if !strings.Contains(string(refreshed), "status: next") {
		t.Fatalf("resurfaced note missing next status:\n%s", string(refreshed))
	}
}

func TestBrainGTDOrganizeDashboardReviewBatchAndIngestToolsWriteMarkdown(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	writeMCPBrainFile(t, filepath.Join(tmp, "work", "brain", "gtd", "alpha.md"), `---
kind: commitment
sphere: work
title: Reply to Ada
status: next
context: email
actor: Ada Lovelace
source_bindings:
  - provider: mail
    ref: m1
---
# Reply to Ada

## Summary
Send the reply.

## Next Action
- [ ] Send the reply.

## Evidence
- mail:m1

## Linked Items
- None.

## Review Notes
- None.
`)
	writeMCPBrainFile(t, filepath.Join(tmp, "work", "brain", "gtd", "done.md"), `---
kind: commitment
sphere: work
title: Ignore Ada
status: done
context: email
source_bindings:
  - provider: mail
    ref: m2
---
# Ignore Ada

## Summary
Ignore this.

## Next Action
- [ ] Ignore this.

## Evidence
- mail:m2

## Linked Items
- None.

## Review Notes
- None.
`)
	writeMCPBrainFile(t, filepath.Join(tmp, "work", "brain", "meetings", "standup.md"), "- [ ] Follow up with Ada\n")

	s := NewServer(t.TempDir())
	organized, err := s.callTool("brain.gtd.organize", map[string]interface{}{
		"config_path": configPath,
		"sphere":      "work",
	})
	if err != nil {
		t.Fatalf("brain.gtd.organize: %v", err)
	}
	orgPath := filepath.Join(tmp, "work", organized["path"].(string))
	orgData, err := os.ReadFile(orgPath)
	if err != nil {
		t.Fatalf("read organize output: %v", err)
	}
	if !strings.Contains(string(orgData), "# GTD Organize") {
		t.Fatalf("organize output missing heading:\n%s", string(orgData))
	}
	if diags := brain.ValidateMarkdownNote(string(orgData), brain.MarkdownParseOptions{}); len(diags) != 0 {
		t.Fatalf("organize output invalid: %#v\n%s", diags, string(orgData))
	}
	dashboard, err := s.callTool("brain.gtd.dashboard", map[string]interface{}{
		"config_path": configPath,
		"sphere":      "work",
		"name":        "Ada",
	})
	if err != nil {
		t.Fatalf("brain.gtd.dashboard: %v", err)
	}
	dashPath := filepath.Join(tmp, "work", dashboard["path"].(string))
	dashData, err := os.ReadFile(dashPath)
	if err != nil {
		t.Fatalf("read dashboard output: %v", err)
	}
	if !strings.Contains(string(dashData), "Reply to Ada") {
		t.Fatalf("dashboard output missing commitment:\n%s", string(dashData))
	}
	if diags := brain.ValidateMarkdownNote(string(dashData), brain.MarkdownParseOptions{}); len(diags) != 0 {
		t.Fatalf("dashboard output invalid: %#v\n%s", diags, string(dashData))
	}
	reviewBatch, err := s.callTool("brain.gtd.review_batch", map[string]interface{}{
		"config_path": configPath,
		"sphere":      "work",
		"q":           "Ada",
	})
	if err != nil {
		t.Fatalf("brain.gtd.review_batch: %v", err)
	}
	reviewPath := filepath.Join(tmp, "work", reviewBatch["path"].(string))
	reviewData, err := os.ReadFile(reviewPath)
	if err != nil {
		t.Fatalf("read review batch output: %v", err)
	}
	if !strings.Contains(string(reviewData), "GTD Review Batch") {
		t.Fatalf("review batch output missing heading:\n%s", string(reviewData))
	}
	if strings.Contains(string(reviewData), "Ignore Ada") {
		t.Fatalf("review batch included done item:\n%s", string(reviewData))
	}
	if diags := brain.ValidateMarkdownNote(string(reviewData), brain.MarkdownParseOptions{}); len(diags) != 0 {
		t.Fatalf("review batch output invalid: %#v\n%s", diags, string(reviewData))
	}
	for _, source := range []string{"meetings", "mail", "todoist", "github", "gitlab", "evernote"} {
		ingested, err := s.callTool("brain.gtd.ingest", map[string]interface{}{
			"config_path": configPath,
			"sphere":      "work",
			"source":      source,
			"paths":       []interface{}{filepath.Join("brain", "meetings", "standup.md")},
		})
		if err != nil {
			t.Fatalf("brain.gtd.ingest %s: %v", source, err)
		}
		if ingested["count"] != 1 {
			t.Fatalf("ingest count for %s = %v, want 1: %#v", source, ingested["count"], ingested)
		}
		var ingestRel string
		switch paths := ingested["paths"].(type) {
		case []string:
			ingestRel = paths[0]
		case []interface{}:
			ingestRel = paths[0].(string)
		default:
			t.Fatalf("unexpected ingest paths type for %s: %T", source, ingested["paths"])
		}
		ingestPath := filepath.Join(tmp, "work", ingestRel)
		ingestData, err := os.ReadFile(ingestPath)
		if err != nil {
			t.Fatalf("read ingest output for %s: %v", source, err)
		}
		if !strings.Contains(string(ingestData), "source_bindings:") || !strings.Contains(string(ingestData), "provider: "+source) {
			t.Fatalf("ingest output missing expected source data for %s:\n%s", source, string(ingestData))
		}
		if result := braingtd.ParseAndValidate(string(ingestData)); len(result.Diagnostics) != 0 {
			t.Fatalf("ingest output invalid for %s: %#v\n%s", source, result.Diagnostics, string(ingestData))
		}
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
