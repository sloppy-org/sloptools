package mailtriage

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

type Classifier interface {
	Classify(context.Context, Message) (Decision, error)
}

type Engine struct {
	Primary Classifier
	Audit   Classifier
	Policy  Policy
}

func (e Engine) Evaluate(ctx context.Context, messages []Message) ([]Evaluation, error) {
	if e.Primary == nil {
		return nil, fmt.Errorf("mail triage primary classifier is required")
	}
	policy := DefaultPolicy(e.Policy.Phase)
	if e.Policy.Phase != "" {
		policy.Phase = e.Policy.Phase
	}
	if len(e.Policy.AutoApplyMinConfidence) > 0 {
		policy.AutoApplyMinConfidence = e.Policy.AutoApplyMinConfidence
	}
	if e.Policy.ManualActions != nil {
		policy.ManualActions = e.Policy.ManualActions
	}
	policy.ReviewOnAuditDisagreement = e.Policy.ReviewOnAuditDisagreement || (!e.Policy.ReviewOnAuditDisagreement && e.Policy.Phase == "")
	results := make([]Evaluation, 0, len(messages))
	for _, message := range messages {
		primary, err := e.Primary.Classify(ctx, message)
		if err != nil {
			return nil, fmt.Errorf("classify %s: %w", strings.TrimSpace(message.ID), err)
		}
		primary = normalizeDecision(primary)
		eval := Evaluation{
			Message: message,
			Primary: primary,
		}
		if e.Audit != nil {
			audit, err := e.Audit.Classify(ctx, message)
			if err != nil {
				return nil, fmt.Errorf("audit classify %s: %w", strings.TrimSpace(message.ID), err)
			}
			clean := normalizeDecision(audit)
			eval.Audit = &clean
		}
		eval.ReviewReasons = reviewReasons(policy, eval)
		eval.ReviewRequired = len(eval.ReviewReasons) > 0
		eval.Disposition = disposition(policy, eval)
		results = append(results, eval)
	}
	return results, nil
}

func disposition(policy Policy, eval Evaluation) Disposition {
	switch policy.Phase {
	case PhaseShadow:
		return DispositionShadow
	case PhaseManualReview:
		return DispositionReview
	default:
		if eval.ReviewRequired {
			return DispositionReview
		}
		if eval.Primary.Action == ActionInbox {
			return DispositionNoop
		}
		return DispositionAutoApply
	}
}

func reviewReasons(policy Policy, eval Evaluation) []string {
	if policy.Phase == PhaseShadow {
		return nil
	}
	reasons := make([]string, 0, 4)
	if eval.Primary.Action == "" {
		reasons = append(reasons, "invalid_action")
	}
	if eval.Primary.Action != ActionInbox {
		if threshold, ok := policy.AutoApplyMinConfidence[eval.Primary.Action]; ok && eval.Primary.Confidence < threshold {
			reasons = append(reasons, "low_confidence")
		}
	}
	if policy.Phase == PhaseManualReview {
		reasons = append(reasons, "manual_review_phase")
	}
	if slices.Contains(policy.ManualActions, eval.Primary.Action) {
		reasons = append(reasons, "manual_action")
	}
	if policy.ReviewOnAuditDisagreement && auditDisagrees(eval) {
		reasons = append(reasons, "audit_disagreement")
	}
	return dedupeStrings(reasons)
}

func auditDisagrees(eval Evaluation) bool {
	if eval.Audit == nil {
		return false
	}
	if eval.Primary.Action != eval.Audit.Action {
		return true
	}
	return !strings.EqualFold(strings.TrimSpace(eval.Primary.ArchiveLabel), strings.TrimSpace(eval.Audit.ArchiveLabel))
}

func normalizeDecision(decision Decision) Decision {
	switch decision.Action {
	case ActionInbox, ActionCC, ActionArchive, ActionTrash:
	default:
		decision.Action = ActionInbox
	}
	if decision.Confidence < 0 {
		decision.Confidence = 0
	}
	if decision.Confidence > 1 {
		decision.Confidence = 1
	}
	decision.Reason = strings.TrimSpace(decision.Reason)
	decision.ArchiveLabel = strings.TrimSpace(decision.ArchiveLabel)
	decision.Model = strings.TrimSpace(decision.Model)
	decision.Signals = dedupeStrings(trimmedStrings(decision.Signals))
	decision.Factors = FactorScores{
		Spam:           clamp01(decision.Factors.Spam),
		ActionRequired: clamp01(decision.Factors.ActionRequired),
		Skim:           clamp01(decision.Factors.Skim),
		Reference:      clamp01(decision.Factors.Reference),
		Staleness:      clamp01(decision.Factors.Staleness),
	}
	return decision
}

func dedupeStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		clean := strings.TrimSpace(value)
		if clean == "" {
			continue
		}
		key := strings.ToLower(clean)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, clean)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func trimmedStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if clean := strings.TrimSpace(value); clean != "" {
			out = append(out, clean)
		}
	}
	return out
}
