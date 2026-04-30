package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	braingtd "github.com/sloppy-org/sloptools/internal/brain/gtd"
)

func TestBrainVaultListCLIListsConfiguredVaults(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeBrainCLIConfig(t, tmp)

	stdout, stderr, code := captureRun(t, []string{
		"brain", "vault", "list",
		"--config", configPath,
	})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr=%q", code, stderr)
	}
	var got map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("decode stdout: %v\n%s", err, stdout)
	}
	if int(got["count"].(float64)) != 2 {
		t.Fatalf("count = %v, stdout=%s", got["count"], stdout)
	}
}

func TestBrainFolderAuditCLIScansFolderNotes(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeBrainCLIConfig(t, tmp)
	writeBrainCLIFile(t, filepath.Join(tmp, "work", "brain", "folders", "project.md"), `---
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

	stdout, stderr, code := captureRun(t, []string{
		"brain", "folder", "audit",
		"--config", configPath,
		"--sphere", "work",
	})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr=%q", code, stderr)
	}
	var got map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("decode stdout: %v\n%s", err, stdout)
	}
	if int(got["count"].(float64)) != 1 {
		t.Fatalf("count = %v, stdout=%s", got["count"], stdout)
	}
	if int(got["issues"].(float64)) == 0 {
		t.Fatalf("expected folder audit issues, stdout=%s", stdout)
	}
}

func TestBrainEntitiesCandidatesCLIExtractsCandidates(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeBrainCLIConfig(t, tmp)
	writeBrainCLIFile(t, filepath.Join(tmp, "work", "brain", "folders", "project.md"), `---
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
	writeBrainCLIFile(t, filepath.Join(tmp, "work", "brain", "glossary", "ntv.md"), `---
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

	stdout, stderr, code := captureRun(t, []string{
		"brain", "entities", "candidates",
		"--config", configPath,
		"--sphere", "work",
	})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr=%q", code, stderr)
	}
	var got map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("decode stdout: %v\n%s", err, stdout)
	}
	if int(got["count"].(float64)) < 4 {
		t.Fatalf("count = %v, stdout=%s", got["count"], stdout)
	}
}

func TestBrainGTDListAndUpdateCLI(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeBrainCLIConfig(t, tmp)
	notePath := filepath.Join("brain", "gtd", "task.md")
	fullPath := filepath.Join(tmp, "work", notePath)
	writeBrainCLIFile(t, fullPath, `---
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

	stdout, stderr, code := captureRun(t, []string{
		"brain", "gtd", "list",
		"--config", configPath,
		"--sphere", "work",
		"--status", "next",
	})
	if code != 0 {
		t.Fatalf("list exit code = %d, stderr=%q", code, stderr)
	}
	var listed map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &listed); err != nil {
		t.Fatalf("decode list stdout: %v\n%s", err, stdout)
	}
	if int(listed["count"].(float64)) != 1 {
		t.Fatalf("list count = %v, stdout=%s", listed["count"], stdout)
	}

	updateStdout, updateStderr, updateCode := captureRun(t, []string{
		"brain", "gtd", "update",
		"--config", configPath,
		"--sphere", "work",
		"--path", notePath,
		"--status", "closed",
		"--closed-at", "2026-04-29T14:00:00Z",
		"--closed-via", "brain.gtd.update",
	})
	if updateCode != 0 {
		t.Fatalf("update exit code = %d, stderr=%q", updateCode, updateStderr)
	}
	if !strings.Contains(updateStdout, `"status": "closed"`) {
		t.Fatalf("update output missing closed status: %s", updateStdout)
	}
	updated, err := os.ReadFile(fullPath)
	if err != nil {
		t.Fatalf("read updated note: %v", err)
	}
	if !strings.Contains(string(updated), "2026-04-29T14:00:00Z") {
		t.Fatalf("updated note missing closed_at:\n%s", string(updated))
	}
	if result := braingtd.ParseAndValidate(string(updated)); len(result.Diagnostics) != 0 {
		t.Fatalf("updated note invalid: %#v\n%s", result.Diagnostics, string(updated))
	}
}
