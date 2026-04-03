package mailtriage

import (
	"strings"
	"testing"
)

func TestDistillReviewedExamplesBuildsBoundedSummary(t *testing.T) {
	training := DistillReviewedExamples([]ReviewedExample{
		{Sender: "Alice <alice@example.com>", Subject: "Timesheet", Folder: "Posteingang", Action: "inbox"},
		{Sender: "Alice <alice@example.com>", Subject: "FuEL follow-up", Folder: "Posteingang", Action: "inbox"},
		{Sender: "List <list@example.com>", Subject: "Weekly digest", Folder: "Posteingang", Action: "cc"},
		{Sender: "Billing <billing@example.com>", Subject: "Old notice", Folder: "Posteingang", Action: "trash"},
		{Sender: "Bob <bob@example.com>", Subject: "Scam 1", Folder: "Junk-E-Mail", Action: "trash"},
		{Sender: "Carol <carol@example.com>", Subject: "Scam 2", Folder: "Junk-E-Mail", Action: "trash"},
		{Sender: "Dave <dave@example.com>", Subject: "Scam 3", Folder: "Junk-E-Mail", Action: "trash"},
		{Sender: "Eve <eve@example.com>", Subject: "Scam 4", Folder: "Junk-E-Mail", Action: "trash"},
		{Sender: "Frank <frank@example.com>", Subject: "Scam 5", Folder: "Junk-E-Mail", Action: "trash"},
		{Sender: "Grace <grace@example.com>", Subject: "Scam 6", Folder: "Junk-E-Mail", Action: "trash"},
		{Sender: "Editor <editor@predatory.example>", Subject: "Call for papers", Folder: "Junk-E-Mail", Action: "archive"},
		{Sender: "Real Journal <journal@example.org>", Subject: "Issue alert", Folder: "Junk-E-Mail", Action: "inbox"},
	})
	if training.ReviewCount != 12 {
		t.Fatalf("ReviewCount = %d, want 12", training.ReviewCount)
	}
	if len(training.PolicySummary) == 0 {
		t.Fatal("PolicySummary is empty")
	}
	if len(training.PolicySummary) > maxPolicySummaryLines {
		t.Fatalf("PolicySummary len = %d, want <= %d", len(training.PolicySummary), maxPolicySummaryLines)
	}
	joined := strings.Join(training.PolicySummary, "\n")
	if !strings.Contains(joined, "Manual review distribution: inbox=3, cc=1, archive=1, trash=7") {
		t.Fatalf("summary missing action distribution: %q", joined)
	}
	if !strings.Contains(joined, "Primary decision boundary: inbox means action or deliberate attention is likely required from the user.") {
		t.Fatalf("summary missing inbox action boundary: %q", joined)
	}
	if !strings.Contains(joined, "Primary decision boundary: cc means no action is required from the user, but the message is still worth a skim.") {
		t.Fatalf("summary missing cc action boundary: %q", joined)
	}
	if !strings.Contains(joined, "Semantics: trash reviewed from junk means confirmed junk/spam.") {
		t.Fatalf("summary missing junk-trash semantics: %q", joined)
	}
	if !strings.Contains(joined, "Semantics: inbox reviewed from junk means a false-positive spam classification; rescue it.") {
		t.Fatalf("summary missing junk-inbox semantics: %q", joined)
	}
	if !strings.Contains(joined, "Semantics: trash reviewed from inbox means discardable, but not necessarily spam/junk.") {
		t.Fatalf("summary missing inbox-trash semantics: %q", joined)
	}
	if !strings.Contains(joined, "Folder rule: Junk-E-Mail usually -> trash") {
		t.Fatalf("summary missing folder rule: %q", joined)
	}
	if !strings.Contains(joined, "Sender rule: alice@example.com usually -> inbox") {
		t.Fatalf("summary missing sender rule: %q", joined)
	}
	if len(training.Examples) == 0 {
		t.Fatal("Examples is empty")
	}
	if len(training.Examples) > maxTrainingExamples {
		t.Fatalf("Examples len = %d, want <= %d", len(training.Examples), maxTrainingExamples)
	}
}
