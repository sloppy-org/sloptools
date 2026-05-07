package brain

import (
	"strings"
	"testing"
)

func TestMarkdownNoteRoundTripPreservesFrontMatterAndSections(t *testing.T) {
	src := `---
title: Alpha
tags:
  - one
  - two
---
Intro prose stays here.

# Context

- bullet one
- bullet two

## Details

Free prose under details.

# Unknown

Keep **this** exactly.
`

	note, diags := ParseMarkdownNote(src, MarkdownParseOptions{RequiredSections: []string{"Context", "Unknown"}})
	if len(diags) != 0 {
		t.Fatalf("ParseMarkdownNote() diagnostics: %v", diags)
	}
	rendered, err := note.Render()
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}
	if rendered != src {
		t.Fatalf("Render() changed source:\n%s", rendered)
	}
	title, ok := note.FrontMatterField("title")
	if !ok || title.Value != "Alpha" {
		t.Fatalf("title field = %#v, ok %v", title, ok)
	}
	context, ok := note.Section("Context")
	if !ok || !strings.Contains(context.Body, "- bullet one") {
		t.Fatalf("Context section = %#v, ok %v", context, ok)
	}
	if _, ok := note.Section("Unknown"); !ok {
		t.Fatal("Unknown section missing")
	}
}

func TestMarkdownNoteUpdatesSelectedFieldsAndPreservesUnknownProse(t *testing.T) {
	src := `---
title: Alpha
status: draft
---
Lead paragraph.

# Context

Old body.

# Unknown

Do not rewrite this prose.
`
	note, diags := ParseMarkdownNote(src, MarkdownParseOptions{RequiredSections: []string{"Context"}})
	if len(diags) != 0 {
		t.Fatalf("ParseMarkdownNote() diagnostics: %v", diags)
	}
	if err := note.SetFrontMatterField("status", "active"); err != nil {
		t.Fatalf("SetFrontMatterField() error: %v", err)
	}
	if err := note.SetSectionBody("Context", "\nNew body.\n\n"); err != nil {
		t.Fatalf("SetSectionBody() error: %v", err)
	}
	rendered, err := note.Render()
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}
	if !strings.Contains(rendered, "status: active") {
		t.Fatalf("updated frontmatter missing:\n%s", rendered)
	}
	if !strings.Contains(rendered, "# Context\n\nNew body.\n\n# Unknown") {
		t.Fatalf("updated section not rendered:\n%s", rendered)
	}
	if !strings.Contains(rendered, "Lead paragraph.") || !strings.Contains(rendered, "Do not rewrite this prose.") {
		t.Fatalf("unknown prose not preserved:\n%s", rendered)
	}
}

func TestMarkdownNoteReportsFrontMatterDiagnosticsWithLines(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
		line int
	}{
		{
			name: "invalid yaml",
			src:  "---\ntitle: [unterminated\n---\n# Context\n",
			want: "invalid frontmatter",
			line: 2,
		},
		{
			name: "duplicate key",
			src:  "---\ntitle: Alpha\ntitle: Beta\n---\n# Context\n",
			want: "duplicate frontmatter key",
			line: 3,
		},
		{
			name: "missing close",
			src:  "---\ntitle: Alpha\n",
			want: "closing delimiter is missing",
			line: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := ParseMarkdownNote(tt.src, MarkdownParseOptions{})
			assertDiagnostic(t, diags, tt.line, tt.want)
		})
	}
}

func TestMarkdownNoteReportsDuplicateRequiredSection(t *testing.T) {
	src := `---
title: Alpha
---
# Context

First.

## Other

Nested.

# Context

Second.
`
	_, diags := ParseMarkdownNote(src, MarkdownParseOptions{RequiredSections: []string{"Context"}})
	assertDiagnostic(t, diags, 12, "duplicate required section")
	if !strings.Contains(diags[0].Message, "line 4") {
		t.Fatalf("duplicate diagnostic should mention first definition: %v", diags[0])
	}
}

func TestMarkdownNoteAppendSectionAddsNewH2AtEnd(t *testing.T) {
	src := `---
title: Alpha
---
# Context

First.
`
	note, diags := ParseMarkdownNote(src, MarkdownParseOptions{})
	if len(diags) != 0 {
		t.Fatalf("ParseMarkdownNote() diagnostics: %v", diags)
	}
	if err := note.AppendSection(2, "Backlog", "- entry one\n- entry two"); err != nil {
		t.Fatalf("AppendSection() error: %v", err)
	}
	rendered, err := note.Render()
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}
	want := "# Context\n\nFirst.\n\n## Backlog\n\n- entry one\n- entry two\n"
	if !strings.HasSuffix(rendered, want) {
		t.Fatalf("appended section render mismatch:\n%s", rendered)
	}
	reparsed, diags := ParseMarkdownNote(rendered, MarkdownParseOptions{})
	if len(diags) != 0 {
		t.Fatalf("re-parse diagnostics: %v", diags)
	}
	if section, ok := reparsed.Section("Backlog"); !ok {
		t.Fatal("reparsed note missing appended Backlog section")
	} else if section.Level != 2 {
		t.Fatalf("appended section level = %d, want 2", section.Level)
	}
}

func TestMarkdownNoteUpsertSectionUpdatesExistingAndAppendsMissing(t *testing.T) {
	src := `---
title: Alpha
---
# Context

Old body.
`
	note, _ := ParseMarkdownNote(src, MarkdownParseOptions{})
	if err := note.UpsertSection(0, "Context", "\nUpdated body.\n"); err != nil {
		t.Fatalf("UpsertSection() update error: %v", err)
	}
	if err := note.UpsertSection(0, "Backlog", "Added backlog entry."); err != nil {
		t.Fatalf("UpsertSection() append error: %v", err)
	}
	rendered, err := note.Render()
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}
	if !strings.Contains(rendered, "# Context\n\nUpdated body.\n") {
		t.Fatalf("existing section was not updated:\n%s", rendered)
	}
	if !strings.Contains(rendered, "## Backlog\n\nAdded backlog entry.\n") {
		t.Fatalf("missing section was not appended:\n%s", rendered)
	}
}

func TestMarkdownNoteAppendSectionRejectsEmptyName(t *testing.T) {
	note, _ := ParseMarkdownNote("# Context\n\nBody.\n", MarkdownParseOptions{})
	if err := note.AppendSection(2, "  ", "body"); err == nil {
		t.Fatal("AppendSection accepted empty name")
	}
}

func assertDiagnostic(t *testing.T, diags []MarkdownDiagnostic, line int, contains string) {
	t.Helper()
	for _, diag := range diags {
		if diag.Line == line && strings.Contains(diag.Message, contains) {
			return
		}
	}
	t.Fatalf("diagnostic %q on line %d missing from %v", contains, line, diags)
}
