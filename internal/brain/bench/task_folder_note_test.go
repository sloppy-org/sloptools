package bench

import "testing"

func TestFolderNoteScorePassesOnCompleteOutput(t *testing.T) {
	task := FolderNoteTask{}
	f := Fixture{
		ID:       "fx-test",
		Expected: map[string]string{"expected_anchor": "projects/NEO-RT"},
	}
	good := `---
folder: plasma/CODES/NEO-RT
sphere: work
---

# NEO-RT

## Summary
Local research code [[projects/NEO-RT]] for transport calculations.

## Key Facts
- Code under projects/NEO-RT

## Important Files
- README.md

## Related Folders
- examples

## Related Notes
- brain/projects/NEO-RT.md

## Notes
Notes here.

## Open Questions
- None.
`
	score, pass, _ := task.Score(f, good)
	if !pass {
		t.Fatalf("expected pass, got fail")
	}
	if score < 1.0-1e-6 {
		t.Fatalf("expected score 1.0, got %.3f", score)
	}
}

func TestFolderNoteScoreFailsWhenAnchorMissing(t *testing.T) {
	task := FolderNoteTask{}
	f := Fixture{
		ID:       "fx-test",
		Expected: map[string]string{"expected_anchor": "projects/NEO-RT"},
	}
	body := `---
folder: x
sphere: work
---

# x

## Summary
Content.

## Key Facts
- a

## Important Files
- a

## Related Folders
- a

## Related Notes
- a

## Notes
text

## Open Questions
- a
`
	score, pass, rationale := task.Score(f, body)
	if pass {
		t.Fatalf("expected fail when anchor missing")
	}
	if score >= 1.0 {
		t.Fatalf("expected reduced score, got %.3f rationale=%s", score, rationale)
	}
}

func TestFolderNoteScoreFailsWhenSectionMissing(t *testing.T) {
	task := FolderNoteTask{}
	f := Fixture{
		ID:       "fx-test",
		Expected: map[string]string{"expected_anchor": "anchor"},
	}
	body := "anchor"
	score, pass, _ := task.Score(f, body)
	if pass {
		t.Fatalf("empty body must not pass")
	}
	if score > 0 {
		t.Fatalf("empty body must score 0, got %.3f", score)
	}
}
