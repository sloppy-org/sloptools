package mailtriage

import (
	"context"
	"testing"
)

type stubClassifier struct {
	decision Decision
}

func (s stubClassifier) Classify(context.Context, Message) (Decision, error) {
	return s.decision, nil
}

func TestEngineShadowKeepsClassificationWithoutReview(t *testing.T) {
	engine := Engine{
		Primary: stubClassifier{decision: Decision{Action: ActionArchive, Confidence: 0.4}},
		Policy:  DefaultPolicy(PhaseShadow),
	}
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
	engine := Engine{
		Primary: stubClassifier{decision: Decision{Action: ActionArchive, Confidence: 0.6}},
		Policy:  DefaultPolicy(PhaseAutoApply),
	}
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
	engine := Engine{
		Primary: stubClassifier{decision: Decision{Action: ActionCC, Confidence: 0.99}},
		Audit:   stubClassifier{decision: Decision{Action: ActionInbox, Confidence: 0.99}},
		Policy:  DefaultPolicy(PhaseAutoApply),
	}
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
	engine := Engine{
		Primary: stubClassifier{decision: Decision{Action: ActionTrash, Confidence: 0.99}},
		Policy:  DefaultPolicy(PhaseAutoApply),
	}
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
