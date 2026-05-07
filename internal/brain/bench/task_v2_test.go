package bench

import "testing"

func TestTriageScoreCorrectVerdict(t *testing.T) {
	task := TriageTask{}
	out := `Reasoning: this is a textbook concept.

{"verdict": "reject", "reason": "wikipedia-derivable", "rejection_class": "textbook"}`
	score, pass, _ := task.Score(Fixture{Expected: map[string]string{"verdict": "reject", "rejection_class": "textbook"}}, out)
	if !pass || score < 1.0 {
		t.Fatalf("expected pass + 1.0, got pass=%v score=%v", pass, score)
	}
}

func TestTriageScoreVerdictMismatch(t *testing.T) {
	task := TriageTask{}
	out := `{"verdict": "promote", "reason": "ok", "rejection_class": ""}`
	score, pass, _ := task.Score(Fixture{Expected: map[string]string{"verdict": "reject", "rejection_class": "textbook"}}, out)
	if pass {
		t.Fatalf("verdict mismatch should not pass: score=%v", score)
	}
}

func TestTriageScoreClassMismatch(t *testing.T) {
	task := TriageTask{}
	out := `{"verdict": "reject", "reason": "x", "rejection_class": "duplicate"}`
	score, pass, _ := task.Score(Fixture{Expected: map[string]string{"verdict": "reject", "rejection_class": "textbook"}}, out)
	if pass {
		t.Fatalf("class mismatch should not pass: score=%v", score)
	}
	if score >= 1.0 {
		t.Fatalf("class mismatch should partial-credit, got %v", score)
	}
}

func TestSleepJudgePass(t *testing.T) {
	task := SleepJudgeTask{}
	out := `# Sleep packet 2026-05-07

## Prune candidates
- (one fewer than the packet)

## NREM consolidation
- brain/people/jane-doe.md

## REM dream candidates
- brain/projects/NEO-RT.md

## Folder coverage
- 1 new folder
`
	f := Fixture{
		Packet:   "different content",
		Expected: map[string]string{"expected_sections": "## Prune candidates,## NREM consolidation,## REM dream candidates,## Folder coverage"},
	}
	score, pass, rationale := task.Score(f, out)
	if !pass {
		t.Fatalf("expected pass, got pass=%v score=%v rationale=%s", pass, score, rationale)
	}
}

func TestSleepJudgeFailsOnFenceWrap(t *testing.T) {
	task := SleepJudgeTask{}
	out := "```markdown\n# Sleep packet\n## Prune candidates\n## NREM consolidation\n## REM dream candidates\n## Folder coverage\n```"
	f := Fixture{
		Packet:   "different content",
		Expected: map[string]string{"expected_sections": "## Prune candidates,## NREM consolidation,## REM dream candidates,## Folder coverage"},
	}
	score, pass, _ := task.Score(f, out)
	if pass || score >= 0.95 {
		t.Fatalf("fenced output should fail: score=%v", score)
	}
}

func TestScoutPass(t *testing.T) {
	task := ScoutTask{}
	out := `# Scout report — Test Entity

## Verified
- Affiliation matches https://example.org

## Conflicting / outdated
- (none)

## Suggestions
- update last_seen

## Open questions
- ?
`
	score, pass, rationale := task.Score(Fixture{}, out)
	if !pass {
		t.Fatalf("expected pass, got pass=%v score=%v rationale=%s", pass, score, rationale)
	}
}

func TestScoutFailsWithoutSource(t *testing.T) {
	task := ScoutTask{}
	out := `# Scout report — X
## Verified
- thing
## Conflicting / outdated
- nothing
## Suggestions
- nothing
## Open questions
- nothing
`
	_, pass, rationale := task.Score(Fixture{}, out)
	if pass {
		t.Fatalf("source-less output should fail; rationale=%s", rationale)
	}
}

func TestCompressPassWithAllAnchorsAndShorter(t *testing.T) {
	task := CompressTask{}
	long := "filler filler filler filler filler filler filler filler filler filler filler filler filler filler filler filler filler filler filler filler filler"
	f := Fixture{
		Packet:   long,
		Expected: map[string]string{"expected_local_anchors": "[[projects/X]]"},
	}
	out := "# X\n\nBackground: see Wikipedia.\n\nLocal: [[projects/X]]\n"
	score, pass, rationale := task.Score(f, out)
	if !pass {
		t.Fatalf("expected pass, got pass=%v score=%v rationale=%s", pass, score, rationale)
	}
}

func TestCompressFailsWhenAnchorDropped(t *testing.T) {
	task := CompressTask{}
	long := "filler filler filler filler filler filler filler filler filler"
	f := Fixture{
		Packet:   long,
		Expected: map[string]string{"expected_local_anchors": "[[projects/X]]"},
	}
	out := "# X\n\nBackground: see Wikipedia.\n"
	_, pass, _ := task.Score(f, out)
	if pass {
		t.Fatalf("dropped anchor should fail")
	}
}
