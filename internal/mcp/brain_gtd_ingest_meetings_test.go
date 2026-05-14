package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBrainGTDIngestMeetingsBackfillsConfiguredRootWhenNoPaths(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	if err := os.MkdirAll(filepath.Join(tmp, "work", "brain"), 0o755); err != nil {
		t.Fatalf("mkdir brain: %v", err)
	}
	meetingsRoot := filepath.Join(tmp, "work", "MEETINGS")
	standup := filepath.Join(meetingsRoot, "2026-04-29-standup", "MEETING_NOTES.md")
	loose := filepath.Join(meetingsRoot, "2026-04-30-1on1.md")
	writeMCPBrainFile(t, standup, meetingsBackfillNote("Standup", "Send weekly summary"))
	writeMCPBrainFile(t, loose, meetingsBackfillNote("1on1", "Review Q3 plan"))

	sourcesPath := writeMeetingsBackfillSourcesConfig(t, tmp, meetingsRoot, nil)
	server := NewServer(t.TempDir())
	got, err := server.callTool("sloppy_brain", map[string]interface{}{"action": "gtd_ingest",
		"config_path":    configPath,
		"sphere":         "work",
		"source":         "meetings",
		"sources_config": sourcesPath,
	})
	if err != nil {
		t.Fatalf("brain.gtd.ingest backfill: %v", err)
	}
	walked, _ := got["walked"].([]string)
	if len(walked) != 2 {
		t.Fatalf("walked = %#v, want 2 entries", walked)
	}
	created, _ := got["created"].([]string)
	if len(created) != 2 {
		t.Fatalf("created = %#v, want 2 commitments", created)
	}
	if got["count"].(int) != 2 {
		t.Fatalf("count = %#v, want 2", got["count"])
	}
}

func TestBrainGTDIngestMeetingsRequiresPathsOrConfiguredRoot(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	if err := os.MkdirAll(filepath.Join(tmp, "work", "brain"), 0o755); err != nil {
		t.Fatalf("mkdir brain: %v", err)
	}
	emptySources := filepath.Join(tmp, "sources.toml")
	writeMCPBrainFile(t, emptySources, "")
	server := NewServer(t.TempDir())
	_, err := server.callTool("sloppy_brain", map[string]interface{}{"action": "gtd_ingest",
		"config_path":    configPath,
		"sphere":         "work",
		"source":         "meetings",
		"sources_config": emptySources,
	})
	if err == nil || !strings.Contains(err.Error(), "paths are required") {
		t.Fatalf("expected paths-required-or-configured-root error, got %v", err)
	}
	if !strings.Contains(err.Error(), "meetings_root") {
		t.Fatalf("error must mention meetings_root fallback: %v", err)
	}
}

func TestBrainGTDIngestMeetingsAppliesOwnerAlias(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	rel := "brain/meetings/standup-aliased.md"
	body := `---
title: Standup
---
# Standup

## Action Checklist

### chris
- [ ] Document the new pipeline
`
	writeMCPBrainFile(t, filepath.Join(tmp, "work", filepath.FromSlash(rel)), body)
	writeMCPBrainFile(t, filepath.Join(tmp, "work", "brain", "people", "Christopher Albert.md"), "# Christopher Albert\n")

	sourcesPath := writeMeetingsBackfillSourcesConfig(t, tmp, "", map[string]string{"chris": "Christopher Albert"})

	server := NewServer(t.TempDir())
	got, err := server.callTool("sloppy_brain", map[string]interface{}{"action": "gtd_ingest",
		"config_path":    configPath,
		"sphere":         "work",
		"source":         "meetings",
		"paths":          []interface{}{rel},
		"sources_config": sourcesPath,
	})
	if err != nil {
		t.Fatalf("brain.gtd.ingest: %v", err)
	}
	created, _ := got["created"].([]string)
	if len(created) != 1 {
		t.Fatalf("created = %#v", got["created"])
	}
	commitment := readFile(t, filepath.Join(tmp, "work", created[0]))
	if !strings.Contains(commitment, `- "Christopher Albert"`) {
		t.Fatalf("expected canonical name in commitment, got:\n%s", commitment)
	}
}

func TestBrainGTDIngestMeetingsResolvesPersonByBrainPeopleSingleToken(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	rel := "brain/meetings/standup-token.md"
	body := `---
title: Standup
---
# Standup

## Action Checklist

### Ada
- [ ] Send the analytical engine paper
`
	writeMCPBrainFile(t, filepath.Join(tmp, "work", filepath.FromSlash(rel)), body)
	writeMCPBrainFile(t, filepath.Join(tmp, "work", "brain", "people", "Ada Lovelace.md"), "# Ada Lovelace\n")
	writeMCPBrainFile(t, filepath.Join(tmp, "work", "brain", "people", "Charles Babbage.md"), "# Charles Babbage\n")

	server := NewServer(t.TempDir())
	got, err := server.callTool("sloppy_brain", map[string]interface{}{"action": "gtd_ingest",
		"config_path": configPath,
		"sphere":      "work",
		"source":      "meetings",
		"paths":       []interface{}{rel},
	})
	if err != nil {
		t.Fatalf("brain.gtd.ingest: %v", err)
	}
	created, _ := got["created"].([]string)
	if len(created) != 1 {
		t.Fatalf("created = %#v", got["created"])
	}
	commitment := readFile(t, filepath.Join(tmp, "work", created[0]))
	if !strings.Contains(commitment, `- "Ada Lovelace"`) {
		t.Fatalf("expected single-token resolution, got:\n%s", commitment)
	}
}

func TestBrainGTDIngestMeetingsAcceptsLegacyImporterRefAsEquivalent(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	rel := "brain/meetings/legacy-standup.md"
	body := `---
title: Standup
---
# Standup

## Action Checklist

### Ada Lovelace
- [ ] Reply to Ada about benchmarks
`
	writeMCPBrainFile(t, filepath.Join(tmp, "work", filepath.FromSlash(rel)), body)

	legacyCommitment := `---
kind: commitment
sphere: work
title: "Reply to Ada about benchmarks"
status: next
context: meetings
people:
  - "Ada Lovelace"
source_bindings:
  - provider: meetings
    ref: "work:legacy-standup:Ada Lovelace:abcd1234"
    writeable: true
---
# Reply to Ada about benchmarks

## Summary
Legacy importer commitment.

## Next Action
- [ ] Reply to Ada about benchmarks

## Evidence
- imported via legacy script

## Linked Items
- None.

## Review Notes
- Legacy import.
`
	writeMCPBrainFile(t, filepath.Join(tmp, "work", "brain", "gtd", "legacy", "ada.md"), legacyCommitment)

	server := NewServer(t.TempDir())
	got, err := server.callTool("sloppy_brain", map[string]interface{}{"action": "gtd_ingest",
		"config_path": configPath,
		"sphere":      "work",
		"source":      "meetings",
		"paths":       []interface{}{rel},
	})
	if err != nil {
		t.Fatalf("brain.gtd.ingest: %v", err)
	}
	if got["count"].(int) != 1 {
		t.Fatalf("count = %#v: %#v", got["count"], got)
	}
	created, _ := got["created"].([]string)
	if len(created) != 0 {
		t.Fatalf("legacy match must not create new commitment, got %#v", created)
	}
	legacyHits, _ := got["legacy_hit"].([]string)
	if len(legacyHits) != 1 {
		t.Fatalf("expected 1 legacy_hit, got %#v", legacyHits)
	}
	stamped, _ := got["stamped"].([]string)
	if len(stamped) != 1 {
		t.Fatalf("expected source to be stamped on first ingest, got %#v", stamped)
	}
}

func TestWriteQuickMeetingCommitmentRendersInboxCommitmentWithTranscript(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	if err := os.MkdirAll(filepath.Join(tmp, "work", "brain"), 0o755); err != nil {
		t.Fatalf("mkdir brain: %v", err)
	}
	audio := "/tmp/inbox/2026-05-01-103015-memo.m4a"
	rel, err := WriteQuickMeetingCommitment(configPath, "work", "send Ada the budget by Friday", "Hey, just send Ada the budget by Friday please.", audio)
	if err != nil {
		t.Fatalf("WriteQuickMeetingCommitment: %v", err)
	}
	body := readFile(t, filepath.Join(tmp, "work", rel))
	for _, want := range []string{
		"provider: meetings",
		"status: inbox",
		"context: meetings",
		"send Ada the budget by Friday",
		"transcript: Hey, just send Ada the budget by Friday please.",
		"audio: 2026-05-01-103015-memo.m4a",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(rel, "_quick") {
		t.Fatalf("quick commitment must not write under meetings_root/_quick: rel=%s", rel)
	}
}

func TestWriteQuickMeetingCommitmentIsIdempotentForSameAudio(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	if err := os.MkdirAll(filepath.Join(tmp, "work", "brain"), 0o755); err != nil {
		t.Fatalf("mkdir brain: %v", err)
	}
	first, err := WriteQuickMeetingCommitment(configPath, "work", "ping payroll", "Remember to ping payroll about the new hire.", "/tmp/inbox/2026-05-01-payroll.m4a")
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := WriteQuickMeetingCommitment(configPath, "work", "ping payroll", "Remember to ping payroll about the new hire.", "/tmp/inbox/2026-05-01-payroll.m4a")
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if first != second {
		t.Fatalf("expected same path for identical audio+transcript, got %q vs %q", first, second)
	}
}

func TestIngestMeetingsExportedHelperWalksConfiguredRoot(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	if err := os.MkdirAll(filepath.Join(tmp, "work", "brain"), 0o755); err != nil {
		t.Fatalf("mkdir brain: %v", err)
	}
	meetingsRoot := filepath.Join(tmp, "work", "MEETINGS")
	writeMCPBrainFile(t, filepath.Join(meetingsRoot, "2026-05-01-board", "MEETING_NOTES.md"), meetingsBackfillNote("Board", "Review Q3 plan"))
	sourcesPath := writeMeetingsBackfillSourcesConfig(t, tmp, meetingsRoot, nil)

	got, err := IngestMeetings(configPath, "work", nil, sourcesPath)
	if err != nil {
		t.Fatalf("IngestMeetings: %v", err)
	}
	if got["count"].(int) != 1 {
		t.Fatalf("count = %#v: %#v", got["count"], got)
	}
	walked, _ := got["walked"].([]string)
	if len(walked) != 1 {
		t.Fatalf("walked = %#v", walked)
	}
}

func meetingsBackfillNote(title, action string) string {
	return `---
title: ` + title + `
---
# ` + title + `

## Action Checklist

### Ada Lovelace
- [ ] ` + action + `
`
}

func writeMeetingsBackfillSourcesConfig(t *testing.T, root, meetingsRoot string, aliases map[string]string) string {
	t.Helper()
	path := filepath.Join(root, "sources.toml")
	var b strings.Builder
	b.WriteString("[meetings.work]\n")
	if meetingsRoot != "" {
		b.WriteString("meetings_root = \"" + filepath.ToSlash(meetingsRoot) + "\"\n")
	}
	if len(aliases) > 0 {
		b.WriteString("[meetings.work.owner_aliases]\n")
		for alias, canonical := range aliases {
			b.WriteString(alias + " = \"" + canonical + "\"\n")
		}
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write sources.toml: %v", err)
	}
	return path
}
