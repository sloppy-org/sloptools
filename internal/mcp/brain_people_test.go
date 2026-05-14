package mcp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sloppy-org/sloptools/internal/brain"
	"github.com/sloppy-org/sloptools/internal/surface"
)

func TestBrainSurfaceExportsLockedMutatingTools(t *testing.T) {
	names := make(map[string]bool, len(surface.MCPTools))
	for _, tool := range surface.MCPTools {
		names[tool.Name] = true
	}
	if !names["sloppy_brain"] {
		t.Fatal("surface missing sloppy_brain tool")
	}
	// Verify sloppy_brain description covers key brain actions.
	var brainTool surface.Tool
	for _, tool := range surface.MCPTools {
		if tool.Name == "sloppy_brain" {
			brainTool = tool
			break
		}
	}
	for _, action := range []string{"note_parse", "gtd_write", "people_brief", "gtd_focus"} {
		if !strings.Contains(brainTool.Description, action) {
			t.Errorf("sloppy_brain description missing action %q", action)
		}
	}
}

func TestBrainPeopleDashboardAggregatesOpenLoops(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	writePersonNote(t, tmp, "Ada Example", "# Ada Example\n")
	recent := time.Now().UTC().AddDate(0, 0, -3).Format(time.RFC3339)
	old := time.Now().UTC().AddDate(0, 0, -30).Format(time.RFC3339)
	writePeopleCommitment(t, tmp, "wait.md", "waiting", "Waiting for draft", "Ada Example", []string{"Ada Example"}, "", recent)
	writePeopleCommitment(t, tmp, "defer.md", "deferred", "Waiting for review", "Ada Example", []string{"Ada Example"}, "", recent)
	writePeopleCommitment(t, tmp, "owe.md", "next", "Send recommendation", "", []string{"Ada Example"}, "", recent)
	writePeopleCommitment(t, tmp, "inbox.md", "inbox", "Clarify scope", "", []string{"Ada Example"}, "", recent)
	writePeopleCommitment(t, tmp, "closed.md", "closed", "Submitted form", "", []string{"Ada Example"}, recent, recent)
	writePeopleCommitment(t, tmp, "old.md", "closed", "Old context", "", []string{"Ada Example"}, old, old)
	writePeopleCommitment(t, tmp, "other.md", "next", "Other person task", "", []string{"Other Person"}, "", recent)

	s := NewServer(t.TempDir())
	got, err := s.callTool("sloppy_brain", map[string]interface{}{"action": "people_dashboard",
		"config_path": configPath,
		"sphere":      "work",
		"name":        "Ada",
	})
	if err != nil {
		t.Fatalf("brain.people.dashboard: %v", err)
	}
	if got["person"].(string) != "Ada Example" {
		t.Fatalf("person = %v, want Ada Example", got["person"])
	}
	assertPeopleLoopCount(t, got, "waiting_on_them", 2)
	assertPeopleLoopCount(t, got, "i_owe_them", 2)
	assertPeopleLoopCount(t, got, "recently_closed", 1)
}

func TestBrainPeopleDashboardResolvesFoldedParentheticalPerson(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	writePersonNote(t, tmp, "Zoë Example (Lab)", "# Zoe Example\n")
	writePeopleCommitment(t, tmp, "owe.md", "next", "Send outline", "", []string{"Zoe Example"}, "", "")

	s := NewServer(t.TempDir())
	got, err := s.callTool("sloppy_brain", map[string]interface{}{"action": "people_dashboard",
		"config_path": configPath,
		"sphere":      "work",
		"name":        "Zoe",
	})
	if err != nil {
		t.Fatalf("brain.people.dashboard folded name: %v", err)
	}
	if got["person"].(string) != "Zoë Example (Lab)" {
		t.Fatalf("person = %v, want folded parenthetical match", got["person"])
	}
	assertPeopleLoopCount(t, got, "i_owe_them", 1)
}

func TestBrainPeopleRenderReplacesOnlyCurrentOpenLoops(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	personPath := writePersonNote(t, tmp, "Ada Example", "# Ada Example\n\nIntro.\n\n## Current open loops\nold\n\n## Notes\nKeep me.\n")
	writePeopleCommitment(t, tmp, "wait.md", "waiting", "Waiting for draft", "Ada Example", []string{"Ada Example"}, "", "")
	writePeopleCommitment(t, tmp, "owe.md", "next", "Send recommendation", "", []string{"Ada Example"}, "", "")

	s := NewServer(t.TempDir())
	got, err := s.callTool("sloppy_brain", map[string]interface{}{"action": "people_render",
		"config_path": configPath,
		"sphere":      "work",
		"name":        "Ada Example",
	})
	if err != nil {
		t.Fatalf("brain.people.render: %v", err)
	}
	if got["changed"] != true {
		t.Fatalf("changed = %v, want true", got["changed"])
	}
	rendered := readPeopleFile(t, personPath)
	if !strings.Contains(rendered, "## Current open loops\n\n### Waiting on them\n- [Waiting for draft](../gtd/wait.md)") {
		t.Fatalf("rendered missing waiting section:\n%s", rendered)
	}
	if !strings.Contains(rendered, "### I owe them\n- [Send recommendation](../gtd/owe.md)") {
		t.Fatalf("rendered missing owed section:\n%s", rendered)
	}
	if !strings.Contains(rendered, "Intro.\n\n## Current open loops") || !strings.Contains(rendered, "## Notes\nKeep me.\n") {
		t.Fatalf("render changed unrelated note content:\n%s", rendered)
	}
	if diags := brain.ValidateMarkdownNote(rendered, brain.MarkdownParseOptions{}); len(diags) != 0 {
		t.Fatalf("rendered person note invalid: %#v\n%s", diags, rendered)
	}
	firstInfo := statPeopleFile(t, personPath)
	second, err := s.callTool("sloppy_brain", map[string]interface{}{"action": "people_render",
		"config_path": configPath,
		"sphere":      "work",
		"name":        "Ada Example",
	})
	if err != nil {
		t.Fatalf("second brain.people.render: %v", err)
	}
	if second["changed"] != false {
		t.Fatalf("second changed = %v, want false", second["changed"])
	}
	if statPeopleFile(t, personPath).ModTime() != firstInfo.ModTime() {
		t.Fatal("idempotent render changed mtime")
	}
}

func TestBrainPeopleRenderEmptyAndMissingPerson(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	personPath := writePersonNote(t, tmp, "Ada Example", "# Ada Example\n")

	s := NewServer(t.TempDir())
	got, err := s.callTool("sloppy_brain", map[string]interface{}{"action": "people_render",
		"config_path": configPath,
		"sphere":      "work",
		"name":        "Ada Example",
	})
	if err != nil {
		t.Fatalf("brain.people.render empty: %v", err)
	}
	if got["changed"] != true {
		t.Fatalf("changed = %v, want true", got["changed"])
	}
	if !strings.Contains(readPeopleFile(t, personPath), "## Current open loops\n\n_None at present._\n") {
		t.Fatalf("missing empty state:\n%s", readPeopleFile(t, personPath))
	}
	missing, err := s.callTool("sloppy_brain", map[string]interface{}{"action": "people_render",
		"config_path": configPath,
		"sphere":      "work",
		"name":        "Missing Person",
	})
	if err != nil {
		t.Fatalf("missing person render returned error: %v", err)
	}
	diags, _ := missing["diagnostics"].([]string)
	if len(diags) != 1 || diags[0] != "needs_person_note: Missing Person" {
		t.Fatalf("missing person diagnostics = %#v", missing["diagnostics"])
	}
	if missing["changed"] != false {
		t.Fatalf("missing person changed = %v, want false", missing["changed"])
	}
}

func TestBrainCatalogTools(t *testing.T) {
	t.Run("vault list", func(t *testing.T) {
		tmp := t.TempDir()
		configPath := writeMCPBrainConfig(t, tmp)
		s := NewServer(t.TempDir())

		got, err := s.callTool("sloppy_brain", map[string]interface{}{"action": "vault_list",
			"config_path": configPath,
		})
		if err != nil {
			t.Fatalf("brain.vault.list: %v", err)
		}
		if got["count"].(int) != 2 {
			t.Fatalf("count = %#v", got["count"])
		}
	})

	t.Run("folder audit", func(t *testing.T) {
		tmp := t.TempDir()
		configPath := writeMCPBrainConfig(t, tmp)
		writeMCPBrainFile(t, filepath.Join(tmp, "work", "brain", "folders", "project.md"), `---
kind: folder
vault: nextcloud
sphere: work
source_folder: project
status: broken
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
`)

		s := NewServer(t.TempDir())
		got, err := s.callTool("sloppy_brain", map[string]interface{}{"action": "folder_audit",
			"config_path": configPath,
			"sphere":      "work",
		})
		if err != nil {
			t.Fatalf("brain.folder.audit: %v", err)
		}
		if got["count"].(int) != 1 {
			t.Fatalf("audit count = %#v", got["count"])
		}
		if got["valid"].(bool) {
			t.Fatalf("expected invalid folder audit: %#v", got)
		}
	})

	t.Run("entities candidates", func(t *testing.T) {
		tmp := t.TempDir()
		configPath := writeMCPBrainConfig(t, tmp)
		writeMCPBrainFile(t, filepath.Join(tmp, "work", "brain", "folders", "project.md"), `---
kind: folder
vault: nextcloud
sphere: work
source_folder: project
status: stale
projects:
  - Fusion
people:
  - Ada Lovelace
institutions: []
topics:
  - Plasma
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
aliases:
  - NTV
  - neoclassical toroidal viscosity
sphere: work
canonical_topic: "[[topics/plasma]]"
---
# NTV

## Definition
Neoclassical toroidal viscosity.
`)

		s := NewServer(t.TempDir())
		got, err := s.callTool("sloppy_brain", map[string]interface{}{"action": "entities_candidates",
			"config_path": configPath,
			"sphere":      "work",
		})
		if err != nil {
			t.Fatalf("brain.entities.candidates: %v", err)
		}
		if got["count"].(int) < 4 {
			t.Fatalf("candidate count = %#v", got["count"])
		}
	})

	t.Run("gtd parse and list", func(t *testing.T) {
		tmp := t.TempDir()
		configPath := writeMCPBrainConfig(t, tmp)
		writeMCPBrainFile(t, filepath.Join(tmp, "work", "brain", "gtd", "task.md"), `---
kind: commitment
sphere: work
title: Reply to Ada
status: next
next_action: Send the reply
context: email
source_refs:
  - mail:work:abc
---
# Reply to Ada

## Summary
Send the reply.

## Next Action
- [ ] Send the reply.

## Evidence
- mail:work:abc

## Linked Items
- None.

## Review Notes
- None.
`)

		s := NewServer(t.TempDir())
		parsed, err := s.callTool("sloppy_brain", map[string]interface{}{"action": "gtd_parse",
			"config_path": configPath,
			"sphere":      "work",
		})
		if err != nil {
			t.Fatalf("brain.gtd.parse: %v", err)
		}
		if parsed["count"].(int) != 1 {
			t.Fatalf("parse count = %#v", parsed["count"])
		}
		listed, err := s.callTool("sloppy_brain", map[string]interface{}{"action": "gtd_list",
			"config_path": configPath,
			"sphere":      "work",
			"status":      "next",
		})
		if err != nil {
			t.Fatalf("brain.gtd.list: %v", err)
		}
		if listed["count"].(int) != 1 {
			t.Fatalf("list count = %#v", listed["count"])
		}
	})
}

func TestBrainProjectToolsDispatch(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	writeMCPBrainFile(t, filepath.Join(tmp, "work", "brain", "projects", "Alpha.md"), "# Alpha\n")
	writeMCPBrainFile(t, filepath.Join(tmp, "work", "brain", "commitments", "task.md"), `---
kind: commitment
sphere: work
status: next
title: Alpha task
outcome: Alpha task
context: test
people:
  - Ada Example
---
# Alpha task
`)
	rulesPath := filepath.Join(tmp, "projects.toml")
	writeMCPBrainFile(t, rulesPath, `[project.alpha]
hub = "brain/projects/Alpha.md"
match.people = ["Ada Example"]
`)

	s := NewServer(t.TempDir())
	linked, err := s.callTool("sloppy_brain", map[string]interface{}{"action": "gtd_bulk_link", "config_path": configPath, "sphere": "work", "rules": rulesPath})
	if err != nil {
		t.Fatalf("brain.gtd.bulk_link: %v", err)
	}
	if linked["linked"] != 1 {
		t.Fatalf("linked = %#v, want 1", linked["linked"])
	}
	rendered, err := s.callTool("sloppy_brain", map[string]interface{}{"action": "projects_render", "config_path": configPath, "sphere": "work", "hub": "brain/projects/Alpha.md"})
	if err != nil {
		t.Fatalf("brain.projects.render: %v", err)
	}
	if rendered["changed"] != true {
		t.Fatalf("changed = %#v, want true", rendered["changed"])
	}
	listed, err := s.callTool("sloppy_brain", map[string]interface{}{"action": "projects_list", "config_path": configPath, "sphere": "work"})
	if err != nil {
		t.Fatalf("brain.projects.list: %v", err)
	}
	if listed["count"] != 1 {
		t.Fatalf("project count = %#v, want 1", listed["count"])
	}
}

func TestBrainPeopleMonthlyIndexDispatch(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	writeMCPBrainFile(t, filepath.Join(tmp, "work", "brain", "people", "Ada.md"), "# Ada\n## Log\n- 2026-04-12 — coffee\n")
	args := map[string]interface{}{"action": "people_monthly_index", "config_path": configPath, "sphere": "work"}
	s := NewServer(t.TempDir())
	got, err := s.callTool("sloppy_brain", args)
	want := "# 2026-04\n\n- [[Ada]] — coffee\n"
	if err != nil || got["months"] != 1 || got["writes"] != 1 || readPeopleFile(t, filepath.Join(tmp, "work", "brain", "journal", "2026-04.md")) != want {
		t.Fatalf("write: err=%v result=%#v", err, got)
	}
	if again, _ := s.callTool("sloppy_brain", args); again["writes"] != 0 {
		t.Fatalf("idempotency: %#v", again)
	}
}
func assertPeopleLoopCount(t *testing.T, got map[string]interface{}, key string, want int) {
	t.Helper()
	items, ok := got[key].([]personOpenLoop)
	if !ok {
		t.Fatalf("%s type = %T", key, got[key])
	}
	if len(items) != want {
		t.Fatalf("%s count = %d, want %d: %#v", key, len(items), want, items)
	}
}

func writePersonNote(t *testing.T, root, name, content string) string {
	t.Helper()
	path := filepath.Join(root, "work", "brain", "people", name+".md")
	writeMCPBrainFile(t, path, content)
	return path
}

func writePeopleCommitment(t *testing.T, root, name, status, title, waitingFor string, people []string, closedAt, evidenceAt string) {
	t.Helper()
	body := fmt.Sprintf("---\nkind: commitment\nsphere: work\nstatus: %s\ntitle: %s\noutcome: %s\ncontext: test\n", status, title, title)
	if status == "next" {
		body += "next_action: " + title + "\n"
	} else if status == "deferred" {
		body += "follow_up: 2026-05-01\n"
	}
	if waitingFor != "" {
		body += "waiting_for: " + waitingFor + "\n"
	}
	if len(people) > 0 {
		body += "people:\n  - " + strings.Join(people, "\n  - ") + "\n"
	}
	if evidenceAt != "" {
		body += "last_evidence_at: " + evidenceAt + "\n"
	}
	if closedAt != "" {
		body += "local_overlay:\n  closed_at: " + closedAt + "\n"
	}
	body += "source_bindings:\n  - provider: manual\n    ref: " + name + "\n---\nBody.\n"
	writeMCPBrainFile(t, filepath.Join(root, "work", "brain", "gtd", name), body)
}

func readPeopleFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func statPeopleFile(t *testing.T, path string) os.FileInfo {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info
}
