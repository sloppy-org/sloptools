package scout

import (
	"strings"
	"testing"
)

func TestClassify_EmptyBulkReport_NoEscalation(t *testing.T) {
	body := `# Scout report — X

## Verified
- foo (source: ...)

## Conflicting / outdated
- (none)

## Suggestions
- bar

## Open questions
- (none)
`
	d := classifyForEscalation(body)
	if d.Escalate {
		t.Fatalf("(none) bullets must not trigger escalation: %+v", d)
	}
}

// One or two conflict bullets are NOT enough — the bulk pass already
// resolved them with citations. Only ≥3 trigger.
func TestClassify_OneConflictBullet_NoEscalation(t *testing.T) {
	body := `# Scout report — X

## Conflicting / outdated
- email v2c2.at is outdated; current is tugraz.at (source: TUGRAZonline vCard)
`
	d := classifyForEscalation(body)
	if d.Escalate {
		t.Fatalf("single resolved conflict should not escalate: %+v", d)
	}
}

func TestClassify_TwoConflictBullets_NoEscalation(t *testing.T) {
	body := `# Scout report — X

## Conflicting / outdated
- email v2c2.at outdated; current tugraz.at (source: TUGRAZonline)
- status dormant since 2020 but DFG profile shows 2024 activity (source: GEPRIS)
`
	d := classifyForEscalation(body)
	if d.Escalate {
		t.Fatalf("two resolved conflicts should not escalate: %+v", d)
	}
}

func TestClassify_ThreeConflictBullets_Escalates(t *testing.T) {
	body := `# Scout report — X

## Conflicting / outdated
- a (source ...)
- b (source ...)
- c (source ...)
`
	d := classifyForEscalation(body)
	if !d.Escalate {
		t.Fatalf("≥3 conflicts must escalate: %+v", d)
	}
}

func TestClassify_TwoOpenQuestions_NoEscalation(t *testing.T) {
	body := `# Scout report — X

## Conflicting / outdated
- (none)

## Open questions
- one minor?
- another minor?
`
	d := classifyForEscalation(body)
	if d.Escalate {
		t.Fatalf("two open questions below threshold: %+v", d)
	}
}

func TestClassify_ThreeOpenQuestions_Escalates(t *testing.T) {
	body := `# Scout report — X

## Conflicting / outdated
- (none)

## Open questions
- what is current title?
- is grant active?
- preferred contact channel?
`
	d := classifyForEscalation(body)
	if !d.Escalate {
		t.Fatalf("three open questions must escalate: %+v", d)
	}
}

func TestClassify_NeedsPaidReviewMarker_Escalates(t *testing.T) {
	body := `# Scout report — X

## Open questions
- needs paid review: which Tower of Letters article matches?
`
	d := classifyForEscalation(body)
	if !d.Escalate || d.Reason == "" {
		t.Fatalf("needs-paid-review marker must escalate with reason: %+v", d)
	}
}

func TestClassify_CryForHelpPhrase_Escalates(t *testing.T) {
	body := `# Scout report — X

## Verified
- thing (source: package)

## Conflicting / outdated
- could not verify the affiliation against the current TUGonline directory
`
	d := classifyForEscalation(body)
	if !d.Escalate {
		t.Fatalf("'could not verify' phrase must escalate: %+v", d)
	}
}

func TestClassify_NewCryForHelpPhrases_Escalate(t *testing.T) {
	for _, phrase := range []string{
		"could not be resolved",
		"tools were unavailable",
		"permission restrictions on the MCP tools",
		"no public source confirms",
	} {
		body := "# Scout report — X\n\n## Open questions\n- " + phrase + " (source: tools)\n"
		d := classifyForEscalation(body)
		if !d.Escalate {
			t.Fatalf("phrase %q must trip cry-for-help escalation: %+v", phrase, d)
		}
		if !strings.Contains(d.Reason, "cry-for-help") {
			t.Fatalf("phrase %q escalated but with wrong reason %q", phrase, d.Reason)
		}
	}
}

func TestClassify_PlaceholderUnverified_NoEscalation(t *testing.T) {
	body := `# Scout report — X

## Conflicting / outdated
- (unverified)

## Open questions
- (unverified)
`
	d := classifyForEscalation(body)
	if d.Escalate {
		t.Fatalf("(unverified) placeholders should not escalate: %+v", d)
	}
}

func TestCountSubstantiveBullets_ScopedToHeading(t *testing.T) {
	body := `## Verified
- a (source)
- b (source)

## Conflicting / outdated
- c
- (none)

## Open questions
- d
`
	if got := countSubstantiveBullets(body, "## Conflicting", "## Conflicting / outdated"); got != 1 {
		t.Fatalf("conflict count=%d want 1", got)
	}
	if got := countSubstantiveBullets(body, "## Open Questions", "## Open questions"); got != 1 {
		t.Fatalf("open-questions count=%d want 1", got)
	}
}
