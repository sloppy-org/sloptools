package scout

import "testing"

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

func TestClassify_ConflictBullet_Escalates(t *testing.T) {
	body := `# Scout report — X

## Conflicting / outdated
- email v2c2.at is outdated; current is tugraz.at (source: TUGRAZonline vCard)

## Open questions
- (none)
`
	d := classifyForEscalation(body)
	if !d.Escalate {
		t.Fatalf("conflict bullet must escalate: %+v", d)
	}
}

func TestClassify_MultipleOpenQuestions_Escalates(t *testing.T) {
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
		t.Fatalf("multiple substantive open questions must escalate: %+v", d)
	}
}

func TestClassify_OneOpenQuestion_NoEscalation(t *testing.T) {
	body := `# Scout report — X

## Conflicting / outdated
- (none)

## Open questions
- one minor follow-up?
`
	d := classifyForEscalation(body)
	if d.Escalate {
		t.Fatalf("single open question is below threshold: %+v", d)
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
