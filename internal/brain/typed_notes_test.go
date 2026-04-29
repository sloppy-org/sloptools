package brain

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateFolderNoteParsesLinksAndGuardsPersonal(t *testing.T) {
	cfg := testConfig(t)
	work := cfg.mustVault(t, SphereWork)
	notePath := writeVaultFile(t, cfg, SphereWork, "brain/folders/project.md")
	writeFile(t, filepath.Join(work.BrainRoot(), "people", "Ada.md"), "Ada")
	src := `---
kind: folder
vault: nextcloud
sphere: work
source_folder: project
status: stale
projects: [Project]
people: [Ada]
institutions: []
topics: [testing]
---
# project

## Summary
Summary.

## Key Facts
- Source folder: project

## Important Files
- [safe](../../project/file.pdf)
- [secret](../../personal/secret.pdf)

## Related Folders
- None.

## Related Notes
- [[people/Ada]]

## Notes
Free prose remains free.

## Open Questions
- None.
`

	parsed, diags := ValidateFolderNote(src, LinkValidationContext{Config: cfg, Sphere: SphereWork, Path: notePath})
	if parsed.SourceFolder != "project" || len(parsed.MarkdownLinks) != 2 || len(parsed.Wikilinks) != 1 {
		t.Fatalf("parsed folder = %#v", parsed)
	}
	assertBrainDiag(t, diags, "excluded_path")
}

func TestValidateFolderNoteAcceptsStrictShape(t *testing.T) {
	cfg := testConfig(t)
	notePath := writeVaultFile(t, cfg, SphereWork, "brain/folders/project.md")
	src := strings.ReplaceAll(validFolderNote(), "[[people/Ada]]", "")

	_, diags := ValidateFolderNote(src, LinkValidationContext{Config: cfg, Sphere: SphereWork, Path: notePath})
	if len(diags) != 0 {
		t.Fatalf("diagnostics: %v", diags)
	}
}

func TestValidateGlossaryNoteChecksCanonicalTopic(t *testing.T) {
	cfg := testConfig(t)
	writeVaultFile(t, cfg, SphereWork, "brain/topics/transport.md")
	src := `---
kind: glossary
display_name: NTV
aliases:
  - NTV
  - neoclassical toroidal viscosity
sphere: work
canonical_topic: "[[topics/transport]]"
do_not_confuse_with:
  - neoclassical tearing mode
---
# NTV

## Definition
Neoclassical toroidal viscosity.
`

	parsed, diags := ValidateGlossaryNote(src, LinkValidationContext{Config: cfg, Sphere: SphereWork})
	if len(diags) != 0 {
		t.Fatalf("diagnostics: %v", diags)
	}
	if parsed.DisplayName != "NTV" || !containsFold(parsed.Aliases, "neoclassical toroidal viscosity") {
		t.Fatalf("parsed glossary = %#v", parsed)
	}

	_, bad := ValidateGlossaryNote(strings.Replace(src, "[[topics/transport]]", "[[people/Ada]]", 1), LinkValidationContext{Config: cfg, Sphere: SphereWork})
	assertBrainDiag(t, bad, "canonical_topic must be exactly one")
}

func TestValidateAttentionFields(t *testing.T) {
	valid := `---
kind: project
focus: active
cadence: weekly
strategic: true
enjoyment: 3
---
# Project
`
	parsed, diags := ValidateAttentionFields(valid)
	if len(diags) != 0 {
		t.Fatalf("diagnostics: %v", diags)
	}
	if parsed.Strategic == nil || !*parsed.Strategic || parsed.Enjoyment != "3" {
		t.Fatalf("attention fields = %#v", parsed)
	}

	_, bad := ValidateAttentionFields(strings.Replace(valid, "weekly", "hourly", 1))
	assertBrainDiag(t, bad, "cadence must be")
}

func TestValidateAttentionFieldsAllowsDeceasedHumanWithoutCadence(t *testing.T) {
	src := `---
kind: human
status: deceased
memorial: 01.01
---
# Person
`
	_, diags := ValidateAttentionFields(src)
	if len(diags) != 0 {
		t.Fatalf("diagnostics: %v", diags)
	}
}

func TestValidateAttentionFieldsAcceptsAttentionKindAlias(t *testing.T) {
	src := `---
kind: attention
focus: watch
cadence: monthly
---
# Project
`
	parsed, diags := ValidateAttentionFields(src)
	if len(diags) != 0 {
		t.Fatalf("diagnostics: %v", diags)
	}
	if parsed.Kind != "attention" || parsed.Focus != "watch" {
		t.Fatalf("attention alias = %#v", parsed)
	}
}

func validFolderNote() string {
	return `---
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
- [[people/Ada]]

## Notes
Free prose.

## Open Questions
- None.
`
}

func assertBrainDiag(t *testing.T, diags []MarkdownDiagnostic, want string) {
	t.Helper()
	for _, diag := range diags {
		if strings.Contains(diag.Error(), want) {
			return
		}
	}
	t.Fatalf("diagnostic containing %q missing from %v", want, diags)
}
