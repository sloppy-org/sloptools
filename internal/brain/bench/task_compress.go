package bench

import (
	"strings"
)

// CompressTask grades a mixed-note compression. The model is given a
// note that has at least one local anchor and is asked to keep every
// locally-specific fact while collapsing the publicly-derivable
// background to a one-line pointer.
//
// Scoring:
//  - retains every expected_local_anchor (60%)
//  - shrinks total length by at least 30% (20%)
//  - introduces a "Background" pointer line (Wikipedia/textbook ref) (20%)
type CompressTask struct {
	FixtureSet []Fixture
}

func (CompressTask) ID() string                     { return "compress" }
func (CompressTask) PromptFile() string             { return "compress.md" }
func (t CompressTask) Fixtures() ([]Fixture, error) { return t.FixtureSet, nil }

func (CompressTask) Score(f Fixture, output string) (float64, bool, string) {
	body := strings.TrimSpace(output)
	if body == "" {
		return 0, false, "empty output"
	}
	score := 0.0
	rationale := []string{}

	anchors := splitFieldList(f.Expected["expected_local_anchors"])
	dropped := []string{}
	for _, a := range anchors {
		if !strings.Contains(body, a) {
			dropped = append(dropped, a)
		}
	}
	if len(anchors) > 0 {
		score += 0.60 * (1.0 - float64(len(dropped))/float64(len(anchors)))
	} else {
		score += 0.60
	}
	if len(dropped) > 0 {
		rationale = append(rationale, "dropped_anchors="+strings.Join(dropped, ","))
	}

	if len(f.Packet) > 0 {
		ratio := float64(len(body)) / float64(len(f.Packet))
		if ratio <= 0.70 {
			score += 0.20
		} else {
			rationale = append(rationale, "no_compression_ratio_too_high")
		}
	} else {
		score += 0.20
	}

	lower := strings.ToLower(body)
	if strings.Contains(lower, "wikipedia") || strings.Contains(lower, "textbook") || strings.Contains(lower, "background:") {
		score += 0.20
	} else {
		rationale = append(rationale, "no_background_pointer")
	}

	pass := score >= 0.95
	return score, pass, strings.Join(rationale, " | ")
}
