package mailtriage

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDistillReviewedExamplesBuildsBoundedSummary(t *testing.T) {
	training := DistillReviewedExamples([]ReviewedExample{{Sender: "Alice <alice@example.com>", Subject: "Timesheet", Folder: "Posteingang", Action: "inbox"}, {Sender: "Alice <alice@example.com>", Subject: "FuEL follow-up", Folder: "Posteingang", Action: "inbox"}, {Sender: "List <list@example.com>", Subject: "Weekly digest", Folder: "Posteingang", Action: "cc"}, {Sender: "Billing <billing@example.com>", Subject: "Old notice", Folder: "Posteingang", Action: "trash"}, {Sender: "Bob <bob@example.com>", Subject: "Scam 1", Folder: "Junk-E-Mail", Action: "trash"}, {Sender: "Carol <carol@example.com>", Subject: "Scam 2", Folder: "Junk-E-Mail", Action: "trash"}, {Sender: "Dave <dave@example.com>", Subject: "Scam 3", Folder: "Junk-E-Mail", Action: "trash"}, {Sender: "Eve <eve@example.com>", Subject: "Scam 4", Folder: "Junk-E-Mail", Action: "trash"}, {Sender: "Frank <frank@example.com>", Subject: "Scam 5", Folder: "Junk-E-Mail", Action: "trash"}, {Sender: "Grace <grace@example.com>", Subject: "Scam 6", Folder: "Junk-E-Mail", Action: "trash"}, {Sender: "Editor <editor@predatory.example>", Subject: "Call for papers", Folder: "Junk-E-Mail", Action: "archive"}, {Sender: "Real Journal <journal@example.org>", Subject: "Issue alert", Folder: "Junk-E-Mail", Action: "inbox"}})
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

type stubClassifier struct{ decision Decision }

func (s stubClassifier) Classify(context.Context, Message) (Decision, error) {
	return s.decision, nil
}

func TestEngineShadowKeepsClassificationWithoutReview(t *testing.T) {
	engine := Engine{Primary: stubClassifier{decision: Decision{Action: ActionArchive, Confidence: 0.4}}, Policy: DefaultPolicy(PhaseShadow)}
	results, err := engine.Evaluate(context.Background(), []Message{{ID: "m1"}})
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].Disposition != DispositionShadow {
		t.Fatalf("Disposition = %q, want %q", results[0].Disposition, DispositionShadow)
	}
	if results[0].ReviewRequired {
		t.Fatal("ReviewRequired = true, want false")
	}
}

func TestEngineAutoApplyRequiresConfidence(t *testing.T) {
	engine := Engine{Primary: stubClassifier{decision: Decision{Action: ActionArchive, Confidence: 0.6}}, Policy: DefaultPolicy(PhaseAutoApply)}
	results, err := engine.Evaluate(context.Background(), []Message{{ID: "m1"}})
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	if !results[0].ReviewRequired {
		t.Fatal("ReviewRequired = false, want true")
	}
	if results[0].Disposition != DispositionReview {
		t.Fatalf("Disposition = %q, want %q", results[0].Disposition, DispositionReview)
	}
}

func TestEngineAuditDisagreementForcesReview(t *testing.T) {
	engine := Engine{Primary: stubClassifier{decision: Decision{Action: ActionCC, Confidence: 0.99}}, Audit: stubClassifier{decision: Decision{Action: ActionInbox, Confidence: 0.99}}, Policy: DefaultPolicy(PhaseAutoApply)}
	results, err := engine.Evaluate(context.Background(), []Message{{ID: "m1"}})
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	if !results[0].ReviewRequired {
		t.Fatal("ReviewRequired = false, want true")
	}
	if got := results[0].ReviewReasons[0]; got == "" {
		t.Fatal("expected review reasons")
	}
}

func TestEngineAutoApplyTrashWhenConfident(t *testing.T) {
	engine := Engine{Primary: stubClassifier{decision: Decision{Action: ActionTrash, Confidence: 0.99}}, Policy: DefaultPolicy(PhaseAutoApply)}
	results, err := engine.Evaluate(context.Background(), []Message{{ID: "m1"}})
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	if results[0].ReviewRequired {
		t.Fatal("ReviewRequired = true, want false")
	}
	if results[0].Disposition != DispositionAutoApply {
		t.Fatalf("Disposition = %q, want %q", results[0].Disposition, DispositionAutoApply)
	}
}

func TestBuildTrainingReportFindsDeterministicMachineRulesAndInconsistency(t *testing.T) {
	report := BuildTrainingReport([]ReviewedExample{{Sender: "Newsletter <newsletter@example.com>", Subject: "News 1", Folder: "Posteingang", Action: "trash"}, {Sender: "Newsletter <newsletter@example.com>", Subject: "News 2", Folder: "Posteingang", Action: "trash"}, {Sender: "Newsletter <newsletter@example.com>", Subject: "News 3", Folder: "Posteingang", Action: "trash"}, {Sender: "Newsletter <newsletter@example.com>", Subject: "News 4", Folder: "Posteingang", Action: "trash"}, {Sender: "Alice <alice@example.com>", Subject: "Action 1", Folder: "Posteingang", Action: "inbox"}, {Sender: "Alice <alice@example.com>", Subject: "FYI 1", Folder: "Posteingang", Action: "cc"}, {Sender: "Alice <alice@example.com>", Subject: "FYI 2", Folder: "Posteingang", Action: "cc"}, {Sender: "Alice <alice@example.com>", Subject: "Action 2", Folder: "Posteingang", Action: "inbox"}})
	if len(report.DeterministicRules) == 0 {
		t.Fatal("DeterministicRules is empty")
	}
	if report.DeterministicRules[0].Key != "newsletter@example.com" || report.DeterministicRules[0].Action != ActionTrash {
		t.Fatalf("first deterministic rule = %+v", report.DeterministicRules[0])
	}
	if len(report.InconsistentPatterns) == 0 || report.InconsistentPatterns[0].Key != "alice@example.com" {
		t.Fatalf("InconsistentPatterns = %+v", report.InconsistentPatterns)
	}
}

func TestHybridClassifierUsesDeterministicRuleWithoutSemantic(t *testing.T) {
	training := DistillReviewedExamples([]ReviewedExample{{Sender: "Newsletter <newsletter@example.com>", Subject: "News 1", Folder: "Posteingang", Action: "trash"}, {Sender: "Newsletter <newsletter@example.com>", Subject: "News 2", Folder: "Posteingang", Action: "trash"}, {Sender: "Newsletter <newsletter@example.com>", Subject: "News 3", Folder: "Posteingang", Action: "trash"}, {Sender: "Newsletter <newsletter@example.com>", Subject: "News 4", Folder: "Posteingang", Action: "trash"}})
	classifier := HybridClassifier{Training: training.Model}
	decision, err := classifier.Classify(context.Background(), Message{ID: "m1", Sender: "newsletter@example.com", Subject: "Another update", Snippet: "New benchmark", ReceivedAt: time.Now()})
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
	training := DistillReviewedExamples([]ReviewedExample{{Sender: "spam@example.com", Subject: "Conference spam", Folder: "Junk-E-Mail", Action: "trash"}, {Sender: "ham@example.com", Subject: "Need action", Folder: "Posteingang", Action: "inbox"}})
	classifier := HybridClassifier{Training: training.Model, Semantic: stubClassifier{decision: Decision{Action: ActionTrash, Confidence: 0.95}}}
	decision, err := classifier.Classify(context.Background(), Message{ID: "m1", Sender: "editor@example.com", Subject: "Plasma physics special issue invitation", Snippet: "Submit your work in plasma physics", ReceivedAt: time.Now()})
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

func TestOpenAIClassifierParsesStructuredJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("Decode() error: %v", err)
		}
		if got := strings.TrimSpace(r.URL.Path); got != "/v1/chat/completions" {
			t.Fatalf("path = %q, want /v1/chat/completions", got)
		}
		if payload["model"] != "qwen3.5-9b" {
			t.Fatalf("model = %#v, want qwen3.5-9b", payload["model"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"choices\":[{\"message\":{\"content\":\"```json\\n{\\\"action\\\":\\\"archive\\\",\\\"archive_label\\\":\\\"simons24\\\",\\\"confidence\\\":0.96,\\\"reason\\\":\\\"project update\\\",\\\"signals\\\":[\\\"direct update\\\"]}\\n```\"}}]}"))
	}))
	defer server.Close()
	classifier := OpenAIClassifier{BaseURL: server.URL, Model: "qwen3.5-9b"}
	decision, err := classifier.Classify(context.Background(), Message{ID: "m1", Subject: "Project update", Snippet: "FYI", Body: "Body", Provider: "exchange_ews"})
	if err != nil {
		t.Fatalf("Classify() error: %v", err)
	}
	if decision.Action != ActionArchive {
		t.Fatalf("Action = %q, want %q", decision.Action, ActionArchive)
	}
	if decision.ArchiveLabel != "simons24" {
		t.Fatalf("ArchiveLabel = %q, want simons24", decision.ArchiveLabel)
	}
	if decision.Model != "qwen3.5-9b" {
		t.Fatalf("Model = %q, want qwen3.5-9b", decision.Model)
	}
}

func TestOpenAIClassifierParsesThinkingPreamble(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"choices\":[{\"message\":{\"content\":\"</think>\\n\\n{\\\"action\\\":\\\"cc\\\",\\\"confidence\\\":0.81,\\\"reason\\\":\\\"newsletter\\\",\\\"signals\\\":[\\\"fyi\\\"]}\"}}]}"))
	}))
	defer server.Close()
	classifier := OpenAIClassifier{BaseURL: server.URL, Model: "qwen3.5-9b"}
	decision, err := classifier.Classify(context.Background(), Message{ID: "m2", Subject: "FYI"})
	if err != nil {
		t.Fatalf("Classify() error: %v", err)
	}
	if decision.Action != ActionCC {
		t.Fatalf("Action = %q, want %q", decision.Action, ActionCC)
	}
	if decision.Confidence != 0.81 {
		t.Fatalf("Confidence = %v, want 0.81", decision.Confidence)
	}
}

func TestOpenAIClassifierReturnsHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad gateway", http.StatusBadGateway)
	}))
	defer server.Close()
	classifier := OpenAIClassifier{BaseURL: server.URL}
	if _, err := classifier.Classify(context.Background(), Message{ID: "m1"}); err == nil {
		t.Fatal("Classify() error = nil, want non-nil")
	}
}

func TestBuildUserPromptIncludesFlagged(t *testing.T) {
	prompt := buildUserPrompt(Message{ID: "m3", Subject: "Important", IsRead: true, IsFlagged: true})
	if !strings.Contains(prompt, "Is flagged: true") {
		t.Fatalf("prompt missing flagged state: %q", prompt)
	}
}

func TestBuildUserPromptIncludesDistilledManualPolicy(t *testing.T) {
	prompt := buildUserPrompt(Message{ID: "m4", Subject: "Suspicious invite", ReviewCount: 37, PolicySummary: []string{"Semantics: trash reviewed from junk means confirmed junk/spam.", "Semantics: trash reviewed from inbox means discardable, but not necessarily spam/junk.", "Folder rule: Junk-E-Mail usually -> trash (21/24 reviews)"}, Examples: []Example{{Action: "trash", Folder: "Junk-E-Mail", Sender: "spam@example.com", Subject: "Win a prize"}}, LocalHints: []string{"spam=0.91", "staleness=0.88"}, ProtectedTopic: true, AgeDays: 42})
	if !strings.Contains(prompt, "Manual review corpus size: 37") {
		t.Fatalf("prompt missing review corpus size: %q", prompt)
	}
	if !strings.Contains(prompt, "Treat the following distilled manual-review policy as authoritative mailbox-specific guidance:") {
		t.Fatalf("prompt missing distilled policy guidance: %q", prompt)
	}
	if !strings.Contains(prompt, "Distilled mailbox policy from manual reviews:") {
		t.Fatalf("prompt missing policy header: %q", prompt)
	}
	if !strings.Contains(prompt, "Semantics: trash reviewed from junk means confirmed junk/spam.") {
		t.Fatalf("prompt missing junk-trash semantics: %q", prompt)
	}
	if !strings.Contains(prompt, "Semantics: trash reviewed from inbox means discardable, but not necessarily spam/junk.") {
		t.Fatalf("prompt missing inbox-trash semantics: %q", prompt)
	}
	if !strings.Contains(prompt, "Folder rule: Junk-E-Mail usually -> trash (21/24 reviews)") {
		t.Fatalf("prompt missing policy line: %q", prompt)
	}
	if !strings.Contains(prompt, "Representative reviewed examples:") {
		t.Fatalf("prompt missing examples header: %q", prompt)
	}
	if !strings.Contains(prompt, "action=trash; folder=Junk-E-Mail; from=spam@example.com; subject=Win a prize") {
		t.Fatalf("prompt missing example detail: %q", prompt)
	}
	if !strings.Contains(prompt, "Local factor hints:") {
		t.Fatalf("prompt missing local factor hints: %q", prompt)
	}
	if !strings.Contains(prompt, "Protected topic: true") {
		t.Fatalf("prompt missing protected topic: %q", prompt)
	}
	if !strings.Contains(prompt, "Age days: 42") {
		t.Fatalf("prompt missing age days: %q", prompt)
	}
}

func TestDefaultSystemPromptSeparatesCCAndArchiveSemantics(t *testing.T) {
	prompt := DefaultSystemPrompt
	for _, snippet := range []string{"cc: not inbox-worthy; worth a skimmed read for information if the user has time, and no action is needed.", "archive: not inbox-worthy; keep only for later reference, with no skimmed read expected.", "Decide inbox vs cc by answering this first: does the email likely require action, follow-up, or deliberate attention from the user? If yes, prefer inbox.", "If no action is required from the user, prefer cc over inbox when the message is still worth a skim.", "For inbox vs cc, action-needed matters more than sender prestige or generic importance.", "Prefer cc instead of archive for newsletters, webinars, and FYI list traffic that is worth a skimmed read.", "Prefer archive instead of cc when the mail should be kept only as reference and does not merit a skimmed read."} {
		if !strings.Contains(prompt, snippet) {
			t.Fatalf("DefaultSystemPrompt missing %q", snippet)
		}
	}
}
