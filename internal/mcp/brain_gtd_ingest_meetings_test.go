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
	got, err := server.callTool("brain.gtd.ingest", map[string]interface{}{
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
	server := NewServer(t.TempDir())
	_, err := server.callTool("brain.gtd.ingest", map[string]interface{}{
		"config_path": configPath,
		"sphere":      "work",
		"source":      "meetings",
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
	got, err := server.callTool("brain.gtd.ingest", map[string]interface{}{
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
	got, err := server.callTool("brain.gtd.ingest", map[string]interface{}{
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
	got, err := server.callTool("brain.gtd.ingest", map[string]interface{}{
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
