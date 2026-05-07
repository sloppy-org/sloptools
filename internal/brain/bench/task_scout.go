package bench

import (
	"strings"
)

// ScoutTask grades a scout evidence report. Scoring is structural: the
// report must contain the canonical sections, must not introduce
// non-bullet prose under the bullet sections, and must include at
// least one source URL or DOI.
type ScoutTask struct {
	FixtureSet []Fixture
}

func (ScoutTask) ID() string                     { return "scout" }
func (ScoutTask) PromptFile() string             { return "scout.md" }
func (t ScoutTask) Fixtures() ([]Fixture, error) { return t.FixtureSet, nil }

var requiredScoutSections = []string{
	"## Verified",
	"## Conflicting / outdated",
	"## Suggestions",
	"## Open questions",
}

func (ScoutTask) Score(f Fixture, output string) (float64, bool, string) {
	body := strings.TrimSpace(output)
	if body == "" {
		return 0, false, "empty output"
	}
	covered := 0
	missing := []string{}
	for _, sec := range requiredScoutSections {
		if strings.Contains(body, sec) {
			covered++
		} else {
			missing = append(missing, sec)
		}
	}
	covRatio := float64(covered) / float64(len(requiredScoutSections))

	hasSource := strings.Contains(body, "http://") ||
		strings.Contains(body, "https://") ||
		strings.Contains(body, "doi:") ||
		strings.Contains(strings.ToLower(body), "doi.org")

	hasH1 := strings.Contains(body, "# Scout report")

	score := covRatio * 0.7
	if hasSource {
		score += 0.2
	}
	if hasH1 {
		score += 0.1
	}
	pass := covRatio == 1.0 && hasSource && hasH1
	rationale := []string{}
	if len(missing) > 0 {
		rationale = append(rationale, "missing_sections="+strings.Join(missing, ","))
	}
	if !hasSource {
		rationale = append(rationale, "no_source_cited")
	}
	if !hasH1 {
		rationale = append(rationale, "missing_h1")
	}
	return score, pass, strings.Join(rationale, " | ")
}
