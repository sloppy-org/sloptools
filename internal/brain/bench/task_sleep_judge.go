package bench

import (
	"strings"
)

// SleepJudgeTask grades the model's editorial pass over a rendered
// sleep packet. The packet typically contains required H2 sections
// (e.g. ## Prune, ## NREM, ## REM, ## Folder coverage). The judge must
// preserve those sections and not introduce sections the packet does
// not authorise.
//
// Score components:
//   - retains all expected_sections from fixture (40%)
//   - does not introduce a forbidden section (30%)
//   - does not echo the packet verbatim (must edit something) (15%)
//   - does not output a fenced ``` wrapper around the whole document (15%)
type SleepJudgeTask struct {
	FixtureSet []Fixture
}

func (SleepJudgeTask) ID() string                     { return "sleep-judge" }
func (SleepJudgeTask) PromptFile() string             { return "sleep-judge.md" }
func (t SleepJudgeTask) Fixtures() ([]Fixture, error) { return t.FixtureSet, nil }

func (SleepJudgeTask) Score(f Fixture, output string) (float64, bool, string) {
	body := strings.TrimSpace(output)
	if body == "" {
		return 0, false, "empty output"
	}
	score := 0.0
	rationale := []string{}

	expected := splitFieldList(f.Expected["expected_sections"])
	missing := []string{}
	for _, sec := range expected {
		if !strings.Contains(body, sec) {
			missing = append(missing, sec)
		}
	}
	if len(expected) > 0 {
		score += 0.40 * (1.0 - float64(len(missing))/float64(len(expected)))
	} else {
		score += 0.40
	}
	if len(missing) > 0 {
		rationale = append(rationale, "missing_sections="+strings.Join(missing, ","))
	}

	forbidden := splitFieldList(f.Expected["forbidden_sections"])
	introduced := []string{}
	for _, sec := range forbidden {
		if strings.Contains(body, sec) {
			introduced = append(introduced, sec)
		}
	}
	if len(introduced) == 0 {
		score += 0.30
	} else {
		rationale = append(rationale, "introduced_forbidden="+strings.Join(introduced, ","))
	}

	if body != strings.TrimSpace(f.Packet) {
		score += 0.15
	} else {
		rationale = append(rationale, "echoed_packet")
	}

	if !strings.HasPrefix(body, "```") {
		score += 0.15
	} else {
		rationale = append(rationale, "wrapped_in_code_fence")
	}

	pass := score >= 0.95
	return score, pass, strings.Join(rationale, " | ")
}

func splitFieldList(s string) []string {
	out := []string{}
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
