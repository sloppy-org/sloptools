package bench

import (
	"strings"
)

// FolderNoteTask grades the model's output against the strict folder-note
// shape: required H2 sections, frontmatter, no Wikipedia-style filler.
//
// Scoring is deterministic, no LLM judge needed: the rubric checks for
// the presence of fixed headings and the absence of obviously textbook
// fillers like "is a numerical method" without any local anchor.
type FolderNoteTask struct {
	FixtureSet []Fixture
}

// ID returns the task name.
func (FolderNoteTask) ID() string { return "folder-note" }

// PromptFile returns the role prompt filename under PromptDir.
func (FolderNoteTask) PromptFile() string { return "folder-note.md" }

// Fixtures returns the configured fixture set.
func (t FolderNoteTask) Fixtures() ([]Fixture, error) { return t.FixtureSet, nil }

// requiredFolderSections is the set of H2 headings every folder note must carry.
var requiredFolderSections = []string{
	"## Summary",
	"## Key Facts",
	"## Important Files",
	"## Related Folders",
	"## Related Notes",
	"## Notes",
	"## Open Questions",
}

// Score grades a folder-note output. Score is required-section coverage
// times an anchor-presence factor. Passes iff every required section is
// present and at least one expected anchor appears in the body.
func (FolderNoteTask) Score(f Fixture, output string) (float64, bool, string) {
	body := strings.TrimSpace(output)
	if body == "" {
		return 0, false, "empty output"
	}
	covered := 0
	missing := []string{}
	for _, sec := range requiredFolderSections {
		if strings.Contains(body, sec) {
			covered++
		} else {
			missing = append(missing, sec)
		}
	}
	covRatio := float64(covered) / float64(len(requiredFolderSections))

	anchorOK := true
	missingAnchors := []string{}
	if expected := f.Expected["expected_anchor"]; expected != "" {
		if !strings.Contains(body, expected) {
			anchorOK = false
			missingAnchors = append(missingAnchors, expected)
		}
	}

	frontMatterOK := strings.HasPrefix(body, "---") &&
		strings.Contains(body, "folder:") &&
		strings.Contains(body, "sphere:")

	score := covRatio
	if !anchorOK {
		score *= 0.5
	}
	if !frontMatterOK {
		score *= 0.5
	}
	pass := covRatio == 1.0 && anchorOK && frontMatterOK
	rationale := strings.TrimSpace(strings.Join([]string{
		formatField("missing_sections", missing),
		formatField("missing_anchors", missingAnchors),
		formatBoolField("frontmatter_ok", frontMatterOK),
	}, " | "))
	return score, pass, rationale
}

func formatField(name string, items []string) string {
	if len(items) == 0 {
		return ""
	}
	return name + "=" + strings.Join(items, ",")
}

func formatBoolField(name string, ok bool) string {
	if ok {
		return ""
	}
	return name + "=false"
}
