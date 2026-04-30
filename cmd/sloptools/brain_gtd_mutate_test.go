package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sloppy-org/sloptools/internal/brain"
	braingtd "github.com/sloppy-org/sloptools/internal/brain/gtd"
)

func TestBrainGTDCLIWriteResurfaceAndReportCommands(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeBrainCLIConfig(t, tmp)
	notePath := filepath.Join("brain", "gtd", "task.md")
	writeBrainCLIFile(t, filepath.Join(tmp, "work", notePath), `---
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
	writeBrainCLIFile(t, filepath.Join(tmp, "work", "brain", "gtd", "report.md"), `---
kind: commitment
sphere: work
title: Prepare slides for Ada
status: next
actor: Ada Lovelace
source_refs:
  - mail:m2
---
# Prepare slides

## Summary
Prepare slides.

## Next Action
- [ ] Prepare slides.

## Evidence
- mail:m2

## Linked Items
- None.

## Review Notes
- None.
`)
	writeBrainCLIFile(t, filepath.Join(tmp, "work", "brain", "meetings", "standup.md"), "- [ ] Follow up with Ada\n")

	stdout, stderr, code := captureRun(t, []string{
		"brain", "gtd", "write",
		"--config", configPath,
		"--sphere", "work",
		"--path", notePath,
		"--status", "closed",
		"--outcome", "Reply sent",
	})
	if code != 0 {
		t.Fatalf("write exit code = %d, stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, `"valid": true`) {
		t.Fatalf("write output missing valid=true: %s", stdout)
	}
	updated, err := os.ReadFile(filepath.Join(tmp, "work", notePath))
	if err != nil {
		t.Fatalf("read updated note: %v", err)
	}
	if !strings.Contains(string(updated), "Intro prose.") || !strings.Contains(string(updated), "outcome: Reply sent") {
		t.Fatalf("write lost prose or outcome:\n%s", string(updated))
	}

	stdout, stderr, code = captureRun(t, []string{
		"brain", "gtd", "write",
		"--config", configPath,
		"--sphere", "work",
		"--path", filepath.Join("brain", "gtd", "created.md"),
		"--title", "Reply to Bea",
		"--status", "next",
		"--next-action", "Send the reply",
		"--context", "email",
		"--binding-provider", "mail",
		"--binding-ref", "mail-42",
	})
	if code != 0 {
		t.Fatalf("create write exit code = %d, stderr=%q", code, stderr)
	}
	var created map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &created); err != nil {
		t.Fatalf("decode create write stdout: %v\n%s", err, stdout)
	}
	createdPath := filepath.Join(tmp, "work", "brain", "gtd", "created.md")
	createdData, err := os.ReadFile(createdPath)
	if err != nil {
		t.Fatalf("read created note: %v", err)
	}
	for _, want := range []string{"kind: commitment", "status: next", "source_bindings:", "provider: mail", "ref: mail-42", "## Summary", "## Next Action"} {
		if !strings.Contains(string(createdData), want) {
			t.Fatalf("created note missing %q:\n%s", want, string(createdData))
		}
	}
	if result := braingtd.ParseAndValidate(string(createdData)); len(result.Diagnostics) != 0 {
		t.Fatalf("created note invalid: %#v\n%s", result.Diagnostics, string(createdData))
	}

	stdout, stderr, code = captureRun(t, []string{
		"brain", "gtd", "resurface",
		"--config", configPath,
		"--sphere", "work",
		"--path", notePath,
	})
	if code != 0 {
		t.Fatalf("resurface exit code = %d, stderr=%q", code, stderr)
	}
	var resurfaced map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &resurfaced); err != nil {
		t.Fatalf("decode resurface stdout: %v\n%s", err, stdout)
	}
	if int(resurfaced["count"].(float64)) != 1 {
		t.Fatalf("resurface count = %v, stdout=%s", resurfaced["count"], stdout)
	}
	refreshed, err := os.ReadFile(filepath.Join(tmp, "work", notePath))
	if err != nil {
		t.Fatalf("read resurfaced note: %v", err)
	}
	if !strings.Contains(string(refreshed), "status: next") {
		t.Fatalf("resurface output missing next status:\n%s", string(refreshed))
	}

	stdout, stderr, code = captureRun(t, []string{
		"brain", "gtd", "organize",
		"--config", configPath,
		"--sphere", "work",
	})
	if code != 0 {
		t.Fatalf("organize exit code = %d, stderr=%q", code, stderr)
	}
	var organized map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &organized); err != nil {
		t.Fatalf("decode organize stdout: %v\n%s", err, stdout)
	}
	orgData, err := os.ReadFile(filepath.Join(tmp, "work", organized["path"].(string)))
	if err != nil {
		t.Fatalf("read organize output: %v", err)
	}
	if !strings.Contains(string(orgData), "# GTD Organize") {
		t.Fatalf("organize output missing heading:\n%s", string(orgData))
	}
	if diags := brain.ValidateMarkdownNote(string(orgData), brain.MarkdownParseOptions{}); len(diags) != 0 {
		t.Fatalf("organize output invalid: %#v\n%s", diags, string(orgData))
	}

	stdout, stderr, code = captureRun(t, []string{
		"brain", "gtd", "dashboard",
		"--config", configPath,
		"--sphere", "work",
		"--name", "Ada",
	})
	if code != 0 {
		t.Fatalf("dashboard exit code = %d, stderr=%q", code, stderr)
	}
	var dashboard map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &dashboard); err != nil {
		t.Fatalf("decode dashboard stdout: %v\n%s", err, stdout)
	}
	dashData, err := os.ReadFile(filepath.Join(tmp, "work", dashboard["path"].(string)))
	if err != nil {
		t.Fatalf("read dashboard output: %v", err)
	}
	if !strings.Contains(string(dashData), "Ada") {
		t.Fatalf("dashboard output missing subject:\n%s", string(dashData))
	}
	if diags := brain.ValidateMarkdownNote(string(dashData), brain.MarkdownParseOptions{}); len(diags) != 0 {
		t.Fatalf("dashboard output invalid: %#v\n%s", diags, string(dashData))
	}

	stdout, stderr, code = captureRun(t, []string{
		"brain", "gtd", "review-batch",
		"--config", configPath,
		"--sphere", "work",
		"--q", "Ada",
	})
	if code != 0 {
		t.Fatalf("review-batch exit code = %d, stderr=%q", code, stderr)
	}
	var review map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &review); err != nil {
		t.Fatalf("decode review-batch stdout: %v\n%s", err, stdout)
	}
	reviewData, err := os.ReadFile(filepath.Join(tmp, "work", review["path"].(string)))
	if err != nil {
		t.Fatalf("read review batch output: %v", err)
	}
	if !strings.Contains(string(reviewData), "GTD Review Batch") {
		t.Fatalf("review batch output missing heading:\n%s", string(reviewData))
	}
	if diags := brain.ValidateMarkdownNote(string(reviewData), brain.MarkdownParseOptions{}); len(diags) != 0 {
		t.Fatalf("review batch output invalid: %#v\n%s", diags, string(reviewData))
	}

	for _, source := range []string{"meetings", "mail", "todoist", "github", "gitlab", "evernote"} {
		stdout, stderr, code = captureRun(t, []string{
			"brain", "gtd", "ingest",
			"--config", configPath,
			"--sphere", "work",
			"--source", source,
			"--path", filepath.Join("brain", "meetings", "standup.md"),
		})
		if code != 0 {
			t.Fatalf("ingest %s exit code = %d, stderr=%q", source, code, stderr)
		}
		var ingest map[string]interface{}
		if err := json.Unmarshal([]byte(stdout), &ingest); err != nil {
			t.Fatalf("decode ingest stdout for %s: %v\n%s", source, err, stdout)
		}
		if int(ingest["count"].(float64)) != 1 {
			t.Fatalf("ingest count for %s = %v, stdout=%s", source, ingest["count"], stdout)
		}
		var ingestRel string
		switch paths := ingest["paths"].(type) {
		case []string:
			ingestRel = paths[0]
		case []interface{}:
			ingestRel = paths[0].(string)
		default:
			t.Fatalf("unexpected ingest paths type for %s: %T", source, ingest["paths"])
		}
		ingestData, err := os.ReadFile(filepath.Join(tmp, "work", ingestRel))
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
