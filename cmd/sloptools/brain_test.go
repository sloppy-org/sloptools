package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBrainFolderParseCLIUsesBrainRelativePath(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeBrainCLIConfig(t, tmp)
	notePath := filepath.Join("brain", "folders", "project.md")
	writeBrainCLIFile(t, filepath.Join(tmp, "work", notePath), `---
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

	stdout, stderr, code := captureRun(t, []string{
		"brain", "folder", "parse",
		"--config", configPath,
		"--sphere", "work",
		"--path", notePath,
	})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr=%q", code, stderr)
	}
	var got map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("decode stdout: %v\n%s", err, stdout)
	}
	if got["source"].(map[string]interface{})["rel"] != notePath {
		t.Fatalf("source.rel = %v, stdout=%s", got["source"], stdout)
	}
	if got["folder"].(map[string]interface{})["source_folder"] != "project" {
		t.Fatalf("folder = %v, stdout=%s", got["folder"], stdout)
	}
}

func TestBrainGTDValidateCLIEmitsStructuredCommitment(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeBrainCLIConfig(t, tmp)
	notePath := filepath.Join("brain", "gtd", "task.md")
	writeBrainCLIFile(t, filepath.Join(tmp, "work", notePath), `---
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
		"brain", "gtd", "validate",
		"--config", configPath,
		"--sphere", "work",
		"--path", notePath,
	})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr=%q", code, stderr)
	}
	var got map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("decode stdout: %v\n%s", err, stdout)
	}
	if !got["valid"].(bool) {
		t.Fatalf("valid = %v, stdout=%s", got["valid"], stdout)
	}
	if got["source"].(map[string]interface{})["rel"] != notePath {
		t.Fatalf("source.rel = %v, stdout=%s", got["source"], stdout)
	}
	if got["commitment"].(map[string]interface{})["next_action"] != "Send the reply" {
		t.Fatalf("commitment = %v, stdout=%s", got["commitment"], stdout)
	}
}

func TestBrainGTDValidateCLISupportsPrivateSphere(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeBrainCLIConfig(t, tmp)
	notePath := filepath.Join("brain", "gtd", "private-task.md")
	writeBrainCLIFile(t, filepath.Join(tmp, "private", notePath), `---
kind: commitment
sphere: private
title: Repair shelf
status: next
next_action: Call carpenter
context: home
source_refs:
  - manual:home
---
# Repair shelf

## Summary
Call carpenter.

## Next Action
- [ ] Call carpenter.

## Evidence
- manual:home

## Linked Items
- None.

## Review Notes
- None.
`)

	stdout, stderr, code := captureRun(t, []string{
		"brain", "gtd", "validate",
		"--config", configPath,
		"--sphere", "private",
		"--path", notePath,
	})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr=%q", code, stderr)
	}
	var got map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("decode stdout: %v\n%s", err, stdout)
	}
	if got["source"].(map[string]interface{})["sphere"] != "private" {
		t.Fatalf("source = %v, stdout=%s", got["source"], stdout)
	}
	if got["commitment"].(map[string]interface{})["sphere"] != "private" {
		t.Fatalf("commitment = %v, stdout=%s", got["commitment"], stdout)
	}
}

func TestBrainAttentionValidateCLIAcceptsAttentionKind(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeBrainCLIConfig(t, tmp)
	notePath := filepath.Join("brain", "people", "ada.md")
	writeBrainCLIFile(t, filepath.Join(tmp, "work", notePath), `---
kind: attention
status: active
focus: active
cadence: weekly
strategic: true
enjoyment: 3
---
# Ada
`)

	stdout, stderr, code := captureRun(t, []string{
		"brain", "attention", "validate",
		"--config", configPath,
		"--sphere", "work",
		"--path", notePath,
	})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr=%q", code, stderr)
	}
	var got map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("decode stdout: %v\n%s", err, stdout)
	}
	if !got["valid"].(bool) {
		t.Fatalf("valid = %v, stdout=%s", got["valid"], stdout)
	}
	if got["attention"].(map[string]interface{})["cadence"] != "weekly" {
		t.Fatalf("attention = %v, stdout=%s", got["attention"], stdout)
	}
}

func TestBrainGlossaryValidateCLIReportsDiagnostics(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeBrainCLIConfig(t, tmp)
	notePath := filepath.Join("brain", "glossary", "ntv.md")
	writeBrainCLIFile(t, filepath.Join(tmp, "work", notePath), `---
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

	stdout, stderr, code := captureRun(t, []string{
		"brain", "glossary", "validate",
		"--config", configPath,
		"--sphere", "work",
		"--path", notePath,
	})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr=%q", code, stderr)
	}
	var got map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("decode stdout: %v\n%s", err, stdout)
	}
	if got["valid"].(bool) {
		t.Fatalf("expected invalid glossary note, stdout=%s", stdout)
	}
	if int(got["count"].(float64)) == 0 {
		t.Fatalf("expected diagnostics, stdout=%s", stdout)
	}
	if got["source"].(map[string]interface{})["rel"] != notePath {
		t.Fatalf("source.rel = %v, stdout=%s", got["source"], stdout)
	}
}

func TestBrainLinksResolveCLIRejectsPersonalGuardrail(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeBrainCLIConfig(t, tmp)
	secret := filepath.Join("personal", "secret.md")
	writeBrainCLIFile(t, filepath.Join(tmp, "work", secret), "secret\n")

	_, stderr, code := captureRun(t, []string{
		"brain", "links", "resolve",
		"--config", configPath,
		"--sphere", "work",
		"--path", secret,
		"--link", "../people/ada.md",
	})
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "excluded_path") {
		t.Fatalf("stderr missing guardrail rejection: %q", stderr)
	}
}

func TestBrainLinksResolveCLIResolvesValidRelativeLink(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeBrainCLIConfig(t, tmp)
	source := filepath.Join("brain", "projects", "alpha.md")
	target := filepath.Join("brain", "people", "ada.md")
	writeBrainCLIFile(t, filepath.Join(tmp, "work", source), "[Ada](../people/ada.md)\n")
	writeBrainCLIFile(t, filepath.Join(tmp, "work", target), "# Ada\n")

	stdout, stderr, code := captureRun(t, []string{
		"brain", "links", "resolve",
		"--config", configPath,
		"--sphere", "work",
		"--path", source,
		"--link", "../people/ada.md",
	})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr=%q", code, stderr)
	}
	var got map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("decode stdout: %v\n%s", err, stdout)
	}
	if got["resolved"].(map[string]interface{})["rel"] != target {
		t.Fatalf("resolved = %v, stdout=%s", got["resolved"], stdout)
	}
}

func TestBrainVaultValidateCLISummarizesNotes(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeBrainCLIConfig(t, tmp)
	writeBrainCLIFile(t, filepath.Join(tmp, "work", "brain", "folders", "project.md"), `---
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
	writeBrainCLIFile(t, filepath.Join(tmp, "work", "brain", "glossary", "ntv.md"), `---
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
	writeBrainCLIFile(t, filepath.Join(tmp, "work", "brain", "projects", "ada.md"), `---
kind: project
focus: active
cadence: weekly
strategic: true
enjoyment: 3
---
# Ada
`)

	stdout, stderr, code := captureRun(t, []string{
		"brain", "vault", "validate",
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
	if got["valid"].(bool) {
		t.Fatalf("expected invalid vault, stdout=%s", stdout)
	}
	if int(got["count"].(float64)) != 3 {
		t.Fatalf("count = %v, stdout=%s", got["count"], stdout)
	}
	if int(got["issues"].(float64)) == 0 {
		t.Fatalf("expected issues, stdout=%s", stdout)
	}
}

func TestBrainSearchCLIEmitsStructuredResults(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeBrainCLIConfig(t, tmp)
	writeBrainCLIFile(t, filepath.Join(tmp, "work", "brain", "projects", "alpha.md"), "needle\n")

	stdout, stderr, code := captureRun(t, []string{
		"brain", "search",
		"--config", configPath,
		"--sphere", "work",
		"--query", "needle",
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
}

func TestBrainBacklinksCLIRejectsPersonalTarget(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeBrainCLIConfig(t, tmp)
	secret := filepath.Join(tmp, "work", "personal", "secret.md")
	writeBrainCLIFile(t, secret, "secret\n")

	_, stderr, code := captureRun(t, []string{
		"brain", "backlinks",
		"--config", configPath,
		"--sphere", "work",
		"--target", secret,
	})
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if stderr == "" {
		t.Fatalf("expected rejection on stderr")
	}
}

func writeBrainCLIConfig(t *testing.T, root string) string {
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
	writeBrainCLIFile(t, path, body)
	return path
}

func writeBrainCLIFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}
