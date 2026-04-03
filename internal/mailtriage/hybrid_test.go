package mailtriage

import (
	"context"
	"testing"
	"time"
)

func TestBuildTrainingReportFindsDeterministicMachineRulesAndInconsistency(t *testing.T) {
	report := BuildTrainingReport([]ReviewedExample{
		{Sender: "Qodo <community@qodo.ai>", Subject: "News 1", Folder: "Posteingang", Action: "trash"},
		{Sender: "Qodo <community@qodo.ai>", Subject: "News 2", Folder: "Posteingang", Action: "trash"},
		{Sender: "Qodo <community@qodo.ai>", Subject: "News 3", Folder: "Posteingang", Action: "trash"},
		{Sender: "Qodo <community@qodo.ai>", Subject: "News 4", Folder: "Posteingang", Action: "trash"},
		{Sender: "Alice <alice@example.com>", Subject: "Action 1", Folder: "Posteingang", Action: "inbox"},
		{Sender: "Alice <alice@example.com>", Subject: "FYI 1", Folder: "Posteingang", Action: "cc"},
		{Sender: "Alice <alice@example.com>", Subject: "FYI 2", Folder: "Posteingang", Action: "cc"},
		{Sender: "Alice <alice@example.com>", Subject: "Action 2", Folder: "Posteingang", Action: "inbox"},
	})
	if len(report.DeterministicRules) == 0 {
		t.Fatal("DeterministicRules is empty")
	}
	if report.DeterministicRules[0].Key != "community@qodo.ai" || report.DeterministicRules[0].Action != ActionTrash {
		t.Fatalf("first deterministic rule = %+v", report.DeterministicRules[0])
	}
	if len(report.InconsistentPatterns) == 0 || report.InconsistentPatterns[0].Key != "alice@example.com" {
		t.Fatalf("InconsistentPatterns = %+v", report.InconsistentPatterns)
	}
}

func TestHybridClassifierUsesDeterministicRuleWithoutSemantic(t *testing.T) {
	training := DistillReviewedExamples([]ReviewedExample{
		{Sender: "Qodo <community@qodo.ai>", Subject: "News 1", Folder: "Posteingang", Action: "trash"},
		{Sender: "Qodo <community@qodo.ai>", Subject: "News 2", Folder: "Posteingang", Action: "trash"},
		{Sender: "Qodo <community@qodo.ai>", Subject: "News 3", Folder: "Posteingang", Action: "trash"},
		{Sender: "Qodo <community@qodo.ai>", Subject: "News 4", Folder: "Posteingang", Action: "trash"},
	})
	classifier := HybridClassifier{Training: training.Model}
	decision, err := classifier.Classify(context.Background(), Message{
		ID:         "m1",
		Sender:     "community@qodo.ai",
		Subject:    "Another update",
		Snippet:    "New benchmark",
		ReceivedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("Classify() error: %v", err)
	}
	if decision.Action != ActionTrash {
		t.Fatalf("Action = %q, want trash", decision.Action)
	}
	if decision.Model != "deterministic" {
		t.Fatalf("Model = %q, want deterministic", decision.Model)
	}
	if decision.Factors.Spam <= 0 {
		t.Fatalf("Factors.Spam = %v, want > 0", decision.Factors.Spam)
	}
}

func TestHybridClassifierFallsBackToArchiveForProtectedTopicDisagreement(t *testing.T) {
	training := DistillReviewedExamples([]ReviewedExample{
		{Sender: "spam@example.com", Subject: "Conference spam", Folder: "Junk-E-Mail", Action: "trash"},
		{Sender: "ham@example.com", Subject: "Need action", Folder: "Posteingang", Action: "inbox"},
	})
	classifier := HybridClassifier{
		Training: training.Model,
		Semantic: stubClassifier{decision: Decision{Action: ActionTrash, Confidence: 0.95}},
	}
	decision, err := classifier.Classify(context.Background(), Message{
		ID:         "m1",
		Sender:     "editor@example.com",
		Subject:    "Plasma physics special issue invitation",
		Snippet:    "Submit your work in plasma physics",
		ReceivedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("Classify() error: %v", err)
	}
	if decision.Action != ActionArchive {
		t.Fatalf("Action = %q, want archive", decision.Action)
	}
	if decision.Model != "hybrid" {
		t.Fatalf("Model = %q, want hybrid", decision.Model)
	}
}
