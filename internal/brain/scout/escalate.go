package scout

import "strings"

// escalateDecision is the deterministic classifier output for one
// bulk-tier scout report. Reason is empty when no escalation is needed.
type escalateDecision struct {
	Escalate bool
	Reason   string
}

// classifyForEscalation reads a scout report body and decides whether
// to re-run it at a paid medium tier. The 2026-05-07 first-with-
// escalation run showed 100% trigger rate on the original
// "any-conflict-bullet-or-multiple-open-questions" heuristic — most
// honest scout reports surface at least one drift item (status, email,
// affiliation) that the bulk pass already resolved with a citation.
// Tighter triggers, from observation:
//
//   - explicit "needs paid review:" bullet anywhere — caller signal
//   - cry-for-help phrases ("unable to verify", "could not confirm",
//     "not externally accessible", "no source available") in any
//     Verified / Conflicting / Open question bullet — bulk gave up
//   - ≥3 substantive `## Conflicting / outdated` bullets — severe drift
//   - ≥3 substantive `## Open questions` bullets — bulk hit a wall
//
// Substantive means: not "(none)", "(unverified)" / "(unconfirmed)" /
// "(tbd)" alone, and not empty after trimming the leading dash.
func classifyForEscalation(body string) escalateDecision {
	if cryReason := scanCryForHelp(body); cryReason != "" {
		return escalateDecision{Escalate: true, Reason: cryReason}
	}
	conflicts := countSubstantiveBullets(body, "## Conflicting", "## Conflicting / outdated", "## Conflicting/outdated")
	questions := countSubstantiveBullets(body, "## Open Questions", "## Open questions")
	if conflicts >= 3 {
		return escalateDecision{Escalate: true, Reason: "≥3 conflict bullets"}
	}
	if questions >= 3 {
		return escalateDecision{Escalate: true, Reason: "≥3 open questions"}
	}
	return escalateDecision{}
}

// scanCryForHelp returns a non-empty reason when the body contains an
// explicit "needs paid review" line or a phrase the bulk model uses
// when it could not finish the job. Case-insensitive.
func scanCryForHelp(body string) string {
	lower := strings.ToLower(body)
	if strings.Contains(lower, "- needs paid review:") || strings.Contains(lower, "- needs paid review ") {
		return "explicit needs-paid-review marker"
	}
	for _, phrase := range []string{
		"unable to verify",
		"could not verify",
		"could not confirm",
		"unable to confirm",
		"unable to access",
		"not externally accessible",
		"no source available",
		"no external source",
	} {
		if strings.Contains(lower, phrase) {
			return "cry-for-help phrase: " + phrase
		}
	}
	return ""
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
