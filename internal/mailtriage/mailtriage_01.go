package mailtriage

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
)

const (
	maxPolicySummaryLines = 14
	maxTrainingExamples   = 4
)

type ruleStat struct {
	key          string
	action       string
	total        int
	dominant     int
	dominantName string
}

func DistillReviewedExamples(reviews []ReviewedExample) DistilledTraining {
	clean := normalizeReviewedExamples(reviews)
	if len(clean) == 0 {
		return DistilledTraining{}
	}
	report := BuildTrainingReport(clean)
	training := DistilledTraining{ReviewCount: len(clean), DeterministicRules: append([]DeterministicRule(nil), report.DeterministicRules...), Report: report}
	actionCounts := make(map[string]int, 4)
	folderCounts := make(map[string]map[string]int)
	folderKindCounts := make(map[string]map[string]int)
	senderCounts := make(map[string]map[string]int)
	domainCounts := make(map[string]map[string]int)
	for _, review := range clean {
		actionCounts[review.Action]++
		if review.Folder != "" {
			incrementNestedCount(folderCounts, review.Folder, review.Action)
			if kind := classifyFolderKind(review.Folder); kind != "" {
				incrementNestedCount(folderKindCounts, kind, review.Action)
			}
		}
		if sender := normalizeSender(review.Sender); sender != "" {
			incrementNestedCount(senderCounts, sender, review.Action)
			if domain := senderDomain(sender); domain != "" {
				incrementNestedCount(domainCounts, domain, review.Action)
			}
		}
	}
	training.PolicySummary = append(training.PolicySummary, overallActionSummary(actionCounts))
	training.PolicySummary = append(training.PolicySummary, summarizeActionVsCCSemantics(actionCounts)...)
	training.PolicySummary = append(training.PolicySummary, summarizeFolderActionSemantics(folderKindCounts)...)
	training.PolicySummary = append(training.PolicySummary, summarizeRules("Folder", collectDominantRules(folderCounts, 3, 0.75), 2)...)
	training.PolicySummary = append(training.PolicySummary, summarizeRules("Sender", collectDominantRules(senderCounts, 2, 0.85), 3)...)
	training.PolicySummary = append(training.PolicySummary, summarizeRules("Domain", collectDominantRules(domainCounts, 3, 0.90), 2)...)
	if len(report.InconsistentPatterns) > 0 {
		for _, pattern := range report.InconsistentPatterns[:min(2, len(report.InconsistentPatterns))] {
			training.Warnings = append(training.Warnings, fmt.Sprintf("Inconsistent sender: %s has mixed outcomes (%s)", pattern.Key, strings.Join(pattern.Actions, ", ")))
		}
	}
	training.PolicySummary = boundedNonEmptyLines(training.PolicySummary, maxPolicySummaryLines)
	training.Examples = representativeExamples(clean, maxTrainingExamples)
	training.Model = trainModel(clean, report.DeterministicRules)
	return training
}

func summarizeActionVsCCSemantics(actionCounts map[string]int) []string {
	lines := []string{}
	if actionCounts["inbox"] > 0 {
		lines = append(lines, "Primary decision boundary: inbox means action or deliberate attention is likely required from the user.")
	}
	if actionCounts["cc"] > 0 {
		lines = append(lines, "Primary decision boundary: cc means no action is required from the user, but the message is still worth a skim.")
	}
	if actionCounts["archive"] > 0 {
		lines = append(lines, "Primary decision boundary: archive means no action is required and the message is not worth a skim, but should remain searchable.")
	}
	return lines
}

func normalizeReviewedExamples(reviews []ReviewedExample) []ReviewedExample {
	out := make([]ReviewedExample, 0, len(reviews))
	for _, review := range reviews {
		action := strings.ToLower(strings.TrimSpace(review.Action))
		if action == "" {
			continue
		}
		out = append(out, ReviewedExample{Sender: strings.TrimSpace(review.Sender), Subject: strings.TrimSpace(review.Subject), Folder: strings.TrimSpace(review.Folder), Action: action})
	}
	return out
}

func incrementNestedCount(m map[string]map[string]int, key, action string) {
	bucket := m[key]
	if bucket == nil {
		bucket = map[string]int{}
		m[key] = bucket
	}
	bucket[action]++
}

func overallActionSummary(actionCounts map[string]int) string {
	order := []string{"inbox", "cc", "archive", "trash"}
	parts := make([]string, 0, len(order))
	for _, action := range order {
		if count := actionCounts[action]; count > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d", action, count))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "Manual review distribution: " + strings.Join(parts, ", ")
}

func summarizeFolderActionSemantics(folderKindCounts map[string]map[string]int) []string {
	lines := []string{}
	if count := folderKindCounts["junk"]["trash"]; count > 0 {
		lines = append(lines, "Semantics: trash reviewed from junk means confirmed junk/spam.")
	}
	if count := folderKindCounts["junk"]["inbox"]; count > 0 {
		lines = append(lines, "Semantics: inbox reviewed from junk means a false-positive spam classification; rescue it.")
	}
	if count := folderKindCounts["junk"]["archive"]; count > 0 {
		lines = append(lines, "Semantics: archive reviewed from junk means keep for reference, often research-adjacent solicitations or suspicious mail, but not inbox-worthy.")
	}
	if count := folderKindCounts["inbox"]["trash"]; count > 0 {
		lines = append(lines, "Semantics: trash reviewed from inbox means discardable, but not necessarily spam/junk.")
	}
	return lines
}

func collectDominantRules(source map[string]map[string]int, minSupport int, minPurity float64) []ruleStat {
	stats := make([]ruleStat, 0, len(source))
	for key, counts := range source {
		total := 0
		dominantName := ""
		dominant := 0
		for action, count := range counts {
			total += count
			if count > dominant || (count == dominant && action < dominantName) {
				dominantName = action
				dominant = count
			}
		}
		if total < minSupport || dominantName == "" {
			continue
		}
		purity := float64(dominant) / float64(total)
		if purity < minPurity {
			continue
		}
		stats = append(stats, ruleStat{key: key, action: dominantName, total: total, dominant: dominant, dominantName: dominantName})
	}
	sort.Slice(stats, func(i, j int) bool {
		if stats[i].dominant != stats[j].dominant {
			return stats[i].dominant > stats[j].dominant
		}
		if stats[i].total != stats[j].total {
			return stats[i].total > stats[j].total
		}
		return stats[i].key < stats[j].key
	})
	return stats
}

func summarizeRules(prefix string, stats []ruleStat, limit int) []string {
	if limit <= 0 || len(stats) == 0 {
		return nil
	}
	out := make([]string, 0, min(limit, len(stats)))
	for _, stat := range stats {
		out = append(out, fmt.Sprintf("%s rule: %s usually -> %s (%d/%d reviews)", prefix, stat.key, stat.action, stat.dominant, stat.total))
		if len(out) >= limit {
			break
		}
	}
	return out
}

func representativeExamples(reviews []ReviewedExample, limit int) []Example {
	if limit <= 0 || len(reviews) == 0 {
		return nil
	}
	byAction := map[string][]ReviewedExample{}
	for _, review := range reviews {
		byAction[review.Action] = append(byAction[review.Action], review)
	}
	order := []string{"inbox", "cc", "archive", "trash"}
	out := make([]Example, 0, min(limit, len(order)))
	for _, action := range order {
		candidates := byAction[action]
		if len(candidates) == 0 {
			continue
		}
		sort.Slice(candidates, func(i, j int) bool {
			if len(candidates[i].Subject) != len(candidates[j].Subject) {
				return len(candidates[i].Subject) < len(candidates[j].Subject)
			}
			if candidates[i].Folder != candidates[j].Folder {
				return candidates[i].Folder < candidates[j].Folder
			}
			if candidates[i].Sender != candidates[j].Sender {
				return candidates[i].Sender < candidates[j].Sender
			}
			return candidates[i].Subject < candidates[j].Subject
		})
		chosen := candidates[0]
		out = append(out, Example{Sender: chosen.Sender, Subject: chosen.Subject, Folder: chosen.Folder, Action: chosen.Action})
		if len(out) >= limit {
			break
		}
	}
	return out
}

func boundedNonEmptyLines(lines []string, limit int) []string {
	out := make([]string, 0, min(limit, len(lines)))
	seen := map[string]struct{}{}
	for _, line := range lines {
		clean := strings.TrimSpace(line)
		if clean == "" {
			continue
		}
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		out = append(out, clean)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func normalizeSender(raw string) string {
	clean := strings.TrimSpace(strings.ToLower(raw))
	if clean == "" {
		return ""
	}
	if start := strings.LastIndex(clean, "<"); start >= 0 && strings.HasSuffix(clean, ">") {
		inner := strings.TrimSpace(clean[start+1 : len(clean)-1])
		if strings.Contains(inner, "@") {
			return inner
		}
	}
	if fields := strings.Fields(clean); len(fields) == 1 && strings.Contains(fields[0], "@") {
		return fields[0]
	}
	return clean
}

func classifyFolderKind(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "inbox", "posteingang":
		return "inbox"
	case "junk", "junk email", "junk-e-mail", "spam":
		return "junk"
	default:
		return ""
	}
}

func senderDomain(sender string) string {
	if idx := strings.LastIndex(strings.TrimSpace(sender), "@"); idx >= 0 && idx < len(strings.TrimSpace(sender))-1 {
		return strings.TrimSpace(sender)[idx+1:]
	}
	return ""
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

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
		eval := Evaluation{Message: message, Primary: primary}
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
	decision.Factors = FactorScores{Spam: clamp01(decision.Factors.Spam), ActionRequired: clamp01(decision.Factors.ActionRequired), Skim: clamp01(decision.Factors.Skim), Reference: clamp01(decision.Factors.Reference), Staleness: clamp01(decision.Factors.Staleness)}
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
		return normalizeDecision(Decision{Action: evidence.Rule.Action, Confidence: 0.995, Reason: evidence.Rule.Reason, Signals: evidence.Signals, Model: "deterministic", Factors: evidence.Factors})
	}
	decision := combineFactors(message, evidence)
	return normalizeDecision(decision)
}
