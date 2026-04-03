package mailtriage

import (
	"context"
	"strings"
	"time"
)

type HybridClassifier struct {
	Training *TrainingModel
	Semantic Classifier
}

func (c HybridClassifier) Classify(ctx context.Context, message Message) (Decision, error) {
	local := c.localDecision(message)
	if c.Semantic == nil {
		return local, nil
	}
	if skipSemantic(local, message) {
		return local, nil
	}
	augmented := message
	augmented.LocalHints = append(append([]string(nil), message.LocalHints...), local.Signals...)
	augmented.ProtectedTopic = protectedTopic(message, protectedTopicKeywords)
	if !message.ReceivedAt.IsZero() {
		augmented.AgeDays = max(0, int(timeSince(message.ReceivedAt).Hours()/24))
	}
	semantic, err := c.Semantic.Classify(ctx, augmented)
	if err != nil {
		return Decision{}, err
	}
	semantic = normalizeDecision(semantic)
	semantic.Factors = local.Factors
	semantic.Signals = dedupeStrings(append(local.Signals, semantic.Signals...))
	return combineHybridDecisions(message, local, semantic), nil
}

func timeSince(value time.Time) time.Duration {
	return time.Since(value)
}

func skipSemantic(local Decision, message Message) bool {
	if local.Model == "deterministic" {
		return true
	}
	if local.Action == ActionTrash && local.Confidence >= 0.995 && !protectedTopic(message, protectedTopicKeywords) {
		return true
	}
	return local.Confidence >= 0.97 && !protectedTopic(message, protectedTopicKeywords)
}

func (c HybridClassifier) localDecision(message Message) Decision {
	evidence := c.Training.Score(message)
	if evidence.Rule != nil {
		return normalizeDecision(Decision{
			Action:     evidence.Rule.Action,
			Confidence: 0.995,
			Reason:     evidence.Rule.Reason,
			Signals:    evidence.Signals,
			Model:      "deterministic",
			Factors:    evidence.Factors,
		})
	}
	decision := combineFactors(message, evidence)
	return normalizeDecision(decision)
}

func combineFactors(message Message, evidence localEvidence) Decision {
	factors := evidence.Factors
	protected := protectedTopic(message, protectedTopicKeywords)
	signals := append([]string(nil), evidence.Signals...)
	if protected {
		signals = append(signals, "topic:protected")
	}
	if message.IsFlagged {
		signals = append(signals, "message:flagged")
		return Decision{
			Action:     ActionInbox,
			Confidence: max(0.94, factors.ActionRequired),
			Reason:     "flagged mail should stay visible",
			Signals:    dedupeStrings(signals),
			Model:      "local_factors",
			Factors:    factors,
		}
	}
	if factors.ActionRequired >= 0.72 {
		return Decision{
			Action:     ActionInbox,
			Confidence: clamp01(0.60 + 0.40*factors.ActionRequired),
			Reason:     "action or attention likely needed",
			Signals:    dedupeStrings(signals),
			Model:      "local_factors",
			Factors:    factors,
		}
	}
	if !protected && factors.Spam >= 0.97 {
		return Decision{
			Action:     ActionTrash,
			Confidence: clamp01(0.75 + 0.25*factors.Spam),
			Reason:     "strong personalized spam signal",
			Signals:    dedupeStrings(signals),
			Model:      "local_factors",
			Factors:    factors,
		}
	}
	if factors.Skim >= 0.62 {
		return Decision{
			Action:     ActionCC,
			Confidence: clamp01(0.55 + 0.35*factors.Skim + 0.10*(1-factors.ActionRequired)),
			Reason:     "skim-worthy but no clear action needed",
			Signals:    dedupeStrings(signals),
			Model:      "local_factors",
			Factors:    factors,
		}
	}
	if factors.Reference >= 0.58 || protected {
		return Decision{
			Action:     ActionArchive,
			Confidence: clamp01(0.52 + 0.33*max(factors.Reference, 0.4) + 0.10*(1-factors.ActionRequired)),
			Reason:     archiveReason(protected),
			Signals:    dedupeStrings(signals),
			Model:      "local_factors",
			Factors:    factors,
		}
	}
	if factors.Staleness >= 0.72 && factors.ActionRequired < 0.45 {
		return Decision{
			Action:     ActionTrash,
			Confidence: clamp01(0.55 + 0.35*factors.Staleness),
			Reason:     "old notification-like mail is likely obsolete",
			Signals:    dedupeStrings(signals),
			Model:      "local_factors",
			Factors:    factors,
		}
	}
	if !protected && factors.Spam >= 0.80 && factors.ActionRequired < 0.35 {
		return Decision{
			Action:     ActionTrash,
			Confidence: clamp01(0.50 + 0.30*factors.Spam),
			Reason:     "discardable low-value mail",
			Signals:    dedupeStrings(signals),
			Model:      "local_factors",
			Factors:    factors,
		}
	}
	return Decision{
		Action:     ActionInbox,
		Confidence: clamp01(0.55 + 0.25*factors.ActionRequired),
		Reason:     "default to inbox when uncertain",
		Signals:    dedupeStrings(signals),
		Model:      "local_factors",
		Factors:    factors,
	}
}

func combineHybridDecisions(message Message, local, semantic Decision) Decision {
	protected := protectedTopic(message, protectedTopicKeywords)
	if local.Action == semantic.Action {
		local.Confidence = max(local.Confidence, semantic.Confidence)
		local.Model = "hybrid"
		if strings.TrimSpace(semantic.Reason) != "" {
			local.Reason = semantic.Reason
		}
		local.Signals = dedupeStrings(append(local.Signals, "semantic_agreement"))
		return normalizeDecision(local)
	}
	if protected && (local.Action == ActionTrash || semantic.Action == ActionTrash) {
		return normalizeDecision(Decision{
			Action:     ActionArchive,
			Confidence: max(local.Confidence, semantic.Confidence) * 0.85,
			Reason:     "protected-topic disagreement falls back to archive",
			Signals:    dedupeStrings(append(local.Signals, semantic.Signals...)),
			Model:      "hybrid",
			Factors:    local.Factors,
		})
	}
	if local.Action == ActionInbox && local.Factors.ActionRequired >= 0.75 {
		local.Confidence = max(local.Confidence, 0.92)
		local.Model = "hybrid"
		local.Signals = dedupeStrings(append(local.Signals, "semantic_disagreement"))
		return normalizeDecision(local)
	}
	if semantic.Confidence >= local.Confidence+0.15 {
		semantic.Model = "hybrid"
		semantic.Signals = dedupeStrings(append(local.Signals, semantic.Signals...))
		semantic.Factors = local.Factors
		return normalizeDecision(semantic)
	}
	local.Model = "hybrid"
	local.Signals = dedupeStrings(append(local.Signals, "semantic_disagreement"))
	return normalizeDecision(local)
}

func archiveReason(protected bool) string {
	if protected {
		return "protected-topic mail should be kept, but no action is evident"
	}
	return "reference-only mail with low action pressure"
}

func BuildTrainingReport(reviews []ReviewedExample) TrainingReport {
	clean := normalizeReviewedExamples(reviews)
	report := TrainingReport{
		ReviewCount:          len(clean),
		ActionCounts:         map[string]int{},
		DeterministicRules:   buildDeterministicRules(clean),
		InconsistentPatterns: buildInconsistentPatterns(clean),
		ProtectedTopics:      append([]string(nil), protectedTopicKeywords...),
	}
	for _, review := range clean {
		report.ActionCounts[review.Action]++
	}
	return report
}
