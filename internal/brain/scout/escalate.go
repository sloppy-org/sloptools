package scout

import "strings"

// escalateDecision is the deterministic classifier output for one
// bulk-tier scout report. Reason is empty when no escalation is needed.
type escalateDecision struct {
	Escalate bool
	Reason   string
}

// classifyForEscalation reads a scout report body and decides whether to
// re-run it at a paid medium tier. Triggers:
//
//   - any substantive bullet under `## Conflicting / outdated` (i.e. a
//     bullet that is not "(none)" and contains specific words)
//   - more than one substantive bullet under `## Open questions`
//
// "Substantive" means: not empty, not "(none)", not "(unverified)" alone,
// not "(unconfirmed)" alone. The point is to skip placeholder bullets the
// model writes when there is genuinely nothing to say.
func classifyForEscalation(body string) escalateDecision {
	conflicts := countSubstantiveBullets(body, "## Conflicting", "## Conflicting / outdated", "## Conflicting/outdated")
	questions := countSubstantiveBullets(body, "## Open Questions", "## Open questions")
	if conflicts > 0 {
		return escalateDecision{Escalate: true, Reason: "conflict bullets present"}
	}
	if questions > 1 {
		return escalateDecision{Escalate: true, Reason: "multiple open questions"}
	}
	return escalateDecision{}
}

// countSubstantiveBullets returns the number of bullet lines under the
// FIRST heading whose name matches any of the provided headingPrefixes.
// Bullets that are "(none)", "(unverified)", "(unconfirmed)", "(tbd)", or
// empty after trimming the leading dash do not count.
func countSubstantiveBullets(body string, headingPrefixes ...string) int {
	lines := strings.Split(body, "\n")
	inSection := false
	count := 0
	for _, raw := range lines {
		line := strings.TrimRight(raw, " \t")
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "##") {
			inSection = false
			for _, pref := range headingPrefixes {
				if strings.EqualFold(strings.TrimSpace(trim), pref) || strings.HasPrefix(strings.ToLower(trim), strings.ToLower(pref)) {
					inSection = true
					break
				}
			}
			continue
		}
		if !inSection {
			continue
		}
		if !strings.HasPrefix(trim, "- ") {
			continue
		}
		body := strings.TrimSpace(trim[2:])
		lower := strings.ToLower(body)
		switch lower {
		case "", "(none)", "none", "(unverified)", "(unconfirmed)", "(tbd)", "tbd":
			continue
		}
		count++
	}
	return count
}
