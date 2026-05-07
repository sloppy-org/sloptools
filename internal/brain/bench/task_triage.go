package bench

import (
	"encoding/json"
	"strings"
)

// TriageTask grades the model's promote / maybe / reject decision for a
// candidate-entity packet. The model must return a JSON object on the
// last line of its output; the rubric ignores prose that comes before
// it.
//
// Output shape:
//
//	{"verdict": "promote|maybe|reject",
//	 "reason": "...",
//	 "rejection_class": "textbook|duplicate|out-of-scope|"}
//
// Score is 1.0 when verdict matches the fixture's ground truth and the
// rejection_class matches when verdict=reject. Passes iff score >= 1.0.
type TriageTask struct {
	FixtureSet []Fixture
}

func (TriageTask) ID() string                     { return "triage" }
func (TriageTask) PromptFile() string             { return "triage.md" }
func (t TriageTask) Fixtures() ([]Fixture, error) { return t.FixtureSet, nil }

type triageVerdict struct {
	Verdict        string `json:"verdict"`
	Reason         string `json:"reason"`
	RejectionClass string `json:"rejection_class"`
}

func (TriageTask) Score(f Fixture, output string) (float64, bool, string) {
	body := strings.TrimSpace(output)
	if body == "" {
		return 0, false, "empty output"
	}
	v, err := extractTriageVerdict(body)
	if err != nil {
		return 0.1, false, "no JSON verdict found: " + err.Error()
	}
	expectedVerdict := strings.ToLower(strings.TrimSpace(f.Expected["verdict"]))
	expectedClass := strings.ToLower(strings.TrimSpace(f.Expected["rejection_class"]))
	got := strings.ToLower(v.Verdict)
	if got != expectedVerdict {
		return 0.3, false, "verdict mismatch (want " + expectedVerdict + ", got " + got + ")"
	}
	if expectedVerdict == "reject" {
		gotClass := strings.ToLower(v.RejectionClass)
		if gotClass != expectedClass {
			return 0.7, false, "rejection_class mismatch (want " + expectedClass + ", got " + gotClass + ")"
		}
	}
	return 1.0, true, "ok"
}

// extractTriageVerdict scans the output for the last JSON object and
// decodes it as a triage verdict. We accept either a fenced ```json
// block, a plain JSON object on its own line, or a JSON object on the
// last line of the response.
func extractTriageVerdict(body string) (triageVerdict, error) {
	candidates := jsonObjectCandidates(body)
	for i := len(candidates) - 1; i >= 0; i-- {
		var v triageVerdict
		if err := json.Unmarshal([]byte(candidates[i]), &v); err == nil && v.Verdict != "" {
			return v, nil
		}
	}
	return triageVerdict{}, errEmpty("no JSON object")
}

type errStr string

func (e errStr) Error() string { return string(e) }

func errEmpty(s string) error { return errStr(s) }

// jsonObjectCandidates returns every {…} substring that looks like a
// balanced JSON object. Best-effort: brace-balanced scan.
func jsonObjectCandidates(s string) []string {
	out := []string{}
	depth := 0
	start := -1
	for i, r := range s {
		switch r {
		case '{':
			if depth == 0 {
				start = i
			}
			depth++
		case '}':
			depth--
			if depth == 0 && start >= 0 {
				out = append(out, s[start:i+1])
				start = -1
			}
		}
	}
	return out
}
