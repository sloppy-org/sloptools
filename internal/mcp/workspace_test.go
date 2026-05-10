package mcp

import (
	"context"
	"github.com/sloppy-org/sloptools/internal/brain"
	tabcalendar "github.com/sloppy-org/sloptools/internal/calendar"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/store"
	"path/filepath"
	"testing"
	"time"
)

type stubFreeBusyProvider struct {
	*stubCalendarProvider
	slots []providerdata.FreeBusySlot
}

func (s *stubFreeBusyProvider) QueryFreeBusy(_ context.Context, participants []string, _ tabcalendar.TimeRange) ([]providerdata.FreeBusySlot, error) {
	slots := make([]providerdata.FreeBusySlot, 0, len(s.slots))
	for _, slot := range s.slots {
		found := false
		for _, p := range participants {
			if slot.Participant == p {
				found = true
				break
			}
		}
		if found {
			slots = append(slots, slot)
		}
	}
	return slots, nil
}

type stubNoFreeBusyProvider struct {
	*stubCalendarProvider
}

func TestCalendarFreeBusyReturnsPerParticipantSlots(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "sloppy.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
	})
	if _, err := st.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderGoogleCalendar, "Private", map[string]any{}); err != nil {
		t.Fatalf("CreateExternalAccount(calendar): %v", err)
	}
	start := time.Date(2026, time.April, 23, 9, 0, 0, 0, time.UTC)
	end := start.Add(30 * time.Minute)
	stub := &stubFreeBusyProvider{
		stubCalendarProvider: &stubCalendarProvider{calendars: []providerdata.Calendar{{ID: "primary", Name: "Primary"}}},
		slots: []providerdata.FreeBusySlot{
			{Participant: "alice@example.com", Start: start, End: end, Status: "busy"},
			{Participant: "bob@example.com", Start: start.Add(time.Hour), End: start.Add(2 * time.Hour), Status: "busy"},
		},
	}
	s := NewServerWithStore(t.TempDir(), st)
	s.newCalendarProvider = func(context.Context, store.ExternalAccount) (tabcalendar.Provider, error) {
		return stub, nil
	}
	got, err := s.callTool("sloppy_calendar", map[string]interface{}{"action": "freebusy", 
		"participants": []string{"alice@example.com", "bob@example.com"},
		"start":        start.Format(time.RFC3339),
		"end":          end.Add(2 * time.Hour).Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("calendar_freebusy failed: %v", err)
	}
	slots, ok := got["slots"].([]map[string]interface{})
	if !ok {
		t.Fatalf("slots type = %T, want []map[string]interface{}", got["slots"])
	}
	if len(slots) != 2 {
		t.Fatalf("slot count = %d, want 2", len(slots))
	}
	if strFromAny(slots[0]["participant"]) != "alice@example.com" {
		t.Fatalf("slot[0] participant = %q, want alice@example.com", strFromAny(slots[0]["participant"]))
	}
	if strFromAny(slots[0]["status"]) != "busy" {
		t.Fatalf("slot[0] status = %q, want busy", strFromAny(slots[0]["status"]))
	}
	if strFromAny(slots[1]["participant"]) != "bob@example.com" {
		t.Fatalf("slot[1] participant = %q, want bob@example.com", strFromAny(slots[1]["participant"]))
	}
}

func TestCalendarFreeBusyRejectsEmptyParticipants(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "sloppy.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
	})
	if _, err := st.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderGoogleCalendar, "Private", map[string]any{}); err != nil {
		t.Fatalf("CreateExternalAccount(calendar): %v", err)
	}
	s := NewServerWithStore(t.TempDir(), st)
	s.newCalendarProvider = func(context.Context, store.ExternalAccount) (tabcalendar.Provider, error) {
		return &stubFreeBusyProvider{stubCalendarProvider: &stubCalendarProvider{}}, nil
	}
	_, err = s.callTool("sloppy_calendar", map[string]interface{}{"action": "freebusy", 
		"participants": []string{},
		"start":        "2026-04-23T09:00:00Z",
		"end":          "2026-04-23T10:00:00Z",
	})
	if err == nil {
		t.Fatal("calendar_freebusy with empty participants should fail")
	}
}

func TestCalendarFreeBusyCapabilityUnsupported(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "sloppy.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
	})
	if _, err := st.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderGoogleCalendar, "Private", map[string]any{}); err != nil {
		t.Fatalf("CreateExternalAccount(calendar): %v", err)
	}
	s := NewServerWithStore(t.TempDir(), st)
	s.newCalendarProvider = func(context.Context, store.ExternalAccount) (tabcalendar.Provider, error) {
		return &stubNoFreeBusyProvider{&stubCalendarProvider{}}, nil
	}
	_, err = s.callTool("sloppy_calendar", map[string]interface{}{"action": "freebusy", 
		"participants": []string{"alice@example.com"},
		"start":        "2026-04-23T09:00:00Z",
		"end":          "2026-04-23T10:00:00Z",
	})
	if err == nil {
		t.Fatal("calendar_freebusy with unsupported provider should fail")
	}
}

func TestCalendarFreeBusyRejectsEndBeforeStart(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "sloppy.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
	})
	if _, err := st.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderGoogleCalendar, "Private", map[string]any{}); err != nil {
		t.Fatalf("CreateExternalAccount(calendar): %v", err)
	}
	s := NewServerWithStore(t.TempDir(), st)
	s.newCalendarProvider = func(context.Context, store.ExternalAccount) (tabcalendar.Provider, error) {
		return &stubFreeBusyProvider{stubCalendarProvider: &stubCalendarProvider{}}, nil
	}
	_, err = s.callTool("sloppy_calendar", map[string]interface{}{"action": "freebusy", 
		"participants": []string{"alice@example.com"},
		"start":        "2026-04-23T10:00:00Z",
		"end":          "2026-04-23T09:00:00Z",
	})
	if err == nil {
		t.Fatal("calendar_freebusy with end before start should fail")
	}
}

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
	got, err := s.callTool("sloppy_brain", map[string]interface{}{"action": "note_parse", 
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

func TestBrainNoteParseToolSupportsPrivateSphere(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	notePath := filepath.Join("brain", "folders", "private-project.md")
	writeMCPBrainFile(t, filepath.Join(tmp, "private", notePath), `---
kind: folder
vault: dropbox
sphere: private
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
	got, err := s.callTool("sloppy_brain", map[string]interface{}{"action": "note_parse", 
		"config_path": configPath,
		"sphere":      "private",
		"path":        notePath,
	})
	if err != nil {
		t.Fatalf("brain.note.parse: %v", err)
	}
	source := got["source"].(brain.ResolvedPath)
	if source.Sphere != "private" || source.Rel != notePath {
		t.Fatalf("source = %#v, want private %q", source, notePath)
	}
}

func TestBrainToolsUseServerDefaultVaultConfig(t *testing.T) {
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

	s := NewServerWithStoreAndBrainConfig(t.TempDir(), nil, configPath)
	got, err := s.callTool("sloppy_brain", map[string]interface{}{"action": "note_parse", 
		"sphere": "work",
		"path":   notePath,
	})
	if err != nil {
		t.Fatalf("brain.note.parse with server default config: %v", err)
	}
	source := got["source"].(brain.ResolvedPath)
	if source.Rel != notePath {
		t.Fatalf("source rel = %q, want %q", source.Rel, notePath)
	}
}

func TestBrainVaultValidateUsesServerDefaultVaultConfig(t *testing.T) {
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

	s := NewServerWithStoreAndBrainConfig(t.TempDir(), nil, configPath)
	got, err := s.callTool("sloppy_brain", map[string]interface{}{"action": "vault_validate", "sphere": "work"})
	if err != nil {
		t.Fatalf("brain.vault.validate with server default config: %v", err)
	}
	if got["valid"] != true || got["count"] != 1 {
		t.Fatalf("vault validation = %#v, want one valid note", got)
	}
}

func TestBrainToolsConfigPathOverridesServerDefaultVaultConfig(t *testing.T) {
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

	s := NewServerWithStoreAndBrainConfig(t.TempDir(), nil, filepath.Join(tmp, "missing.toml"))
	got, err := s.callTool("sloppy_brain", map[string]interface{}{"action": "note_parse", 
		"config_path": configPath,
		"sphere":      "work",
		"path":        notePath,
	})
	if err != nil {
		t.Fatalf("brain.note.parse with config_path override: %v", err)
	}
	source := got["source"].(brain.ResolvedPath)
	if source.Rel != notePath {
		t.Fatalf("source rel = %q, want %q", source.Rel, notePath)
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
	got, err := s.callTool("sloppy_brain", map[string]interface{}{"action": "note_validate", 
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
