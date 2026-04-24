package mailtriage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"
)

func combineFactors(message Message, evidence localEvidence) Decision {
	factors := evidence.Factors
	protected := protectedTopic(message, protectedTopicKeywords)
	signals := append([]string(nil), evidence.Signals...)
	if protected {
		signals = append(signals, "topic:protected")
	}
	if message.IsFlagged {
		signals = append(signals, "message:flagged")
		return Decision{Action: ActionInbox, Confidence: max(0.94, factors.ActionRequired), Reason: "flagged mail should stay visible", Signals: dedupeStrings(signals), Model: "local_factors", Factors: factors}
	}
	if factors.ActionRequired >= 0.72 {
		return Decision{Action: ActionInbox, Confidence: clamp01(0.60 + 0.40*factors.ActionRequired), Reason: "action or attention likely needed", Signals: dedupeStrings(signals), Model: "local_factors", Factors: factors}
	}
	if !protected && factors.Spam >= 0.97 {
		return Decision{Action: ActionTrash, Confidence: clamp01(0.75 + 0.25*factors.Spam), Reason: "strong personalized spam signal", Signals: dedupeStrings(signals), Model: "local_factors", Factors: factors}
	}
	if factors.Skim >= 0.62 {
		return Decision{Action: ActionCC, Confidence: clamp01(0.55 + 0.35*factors.Skim + 0.10*(1-factors.ActionRequired)), Reason: "skim-worthy but no clear action needed", Signals: dedupeStrings(signals), Model: "local_factors", Factors: factors}
	}
	if factors.Reference >= 0.58 || protected {
		return Decision{Action: ActionArchive, Confidence: clamp01(0.52 + 0.33*max(factors.Reference, 0.4) + 0.10*(1-factors.ActionRequired)), Reason: archiveReason(protected), Signals: dedupeStrings(signals), Model: "local_factors", Factors: factors}
	}
	if factors.Staleness >= 0.72 && factors.ActionRequired < 0.45 {
		return Decision{Action: ActionTrash, Confidence: clamp01(0.55 + 0.35*factors.Staleness), Reason: "old notification-like mail is likely obsolete", Signals: dedupeStrings(signals), Model: "local_factors", Factors: factors}
	}
	if !protected && factors.Spam >= 0.80 && factors.ActionRequired < 0.35 {
		return Decision{Action: ActionTrash, Confidence: clamp01(0.50 + 0.30*factors.Spam), Reason: "discardable low-value mail", Signals: dedupeStrings(signals), Model: "local_factors", Factors: factors}
	}
	return Decision{Action: ActionInbox, Confidence: clamp01(0.55 + 0.25*factors.ActionRequired), Reason: "default to inbox when uncertain", Signals: dedupeStrings(signals), Model: "local_factors", Factors: factors}
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
		return normalizeDecision(Decision{Action: ActionArchive, Confidence: max(local.Confidence, semantic.Confidence) * 0.85, Reason: "protected-topic disagreement falls back to archive", Signals: dedupeStrings(append(local.Signals, semantic.Signals...)), Model: "hybrid", Factors: local.Factors})
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
	report := TrainingReport{ReviewCount: len(clean), ActionCounts: map[string]int{}, DeterministicRules: buildDeterministicRules(clean), InconsistentPatterns: buildInconsistentPatterns(clean), ProtectedTopics: append([]string(nil), protectedTopicKeywords...)}
	for _, review := range clean {
		report.ActionCounts[review.Action]++
	}
	return report
}

const DefaultSystemPrompt = `You classify emails for a personal triage system.

Return strict JSON with shape:
{"action":"inbox|cc|archive|trash","archive_label":"optional short label","confidence":0.0,"reason":"short reason","signals":["short signal"]}

Semantics:
- inbox: the user should look at this or act on it.
- cc: not inbox-worthy; worth a skimmed read for information if the user has time, and no action is needed.
- archive: not inbox-worthy; keep only for later reference, with no skimmed read expected.
- trash: clearly useless, spam, or safe to discard.

Rules:
- Decide inbox vs cc by answering this first: does the email likely require action, follow-up, or deliberate attention from the user? If yes, prefer inbox.
- If no action is required from the user, prefer cc over inbox when the message is still worth a skim.
- When unsure between inbox and anything else, choose inbox.
- When unsure between archive and trash, choose archive.
- Use archive_label only for clear project/reference buckets.
- Do not use "already read" by itself as a reason to archive or trash.
- If the message is flagged, treat that as a strong inbox signal.
- Treat folder-aware manual-review policy as authoritative when it is provided.
- In particular: trash reviewed from junk means confirmed spam/junk; trash reviewed from inbox means discardable but not necessarily spam.
- Prefer inbox for direct human mail from collaborators, admins, or teaching/research contacts when action or attention may still be needed.
- For inbox vs cc, action-needed matters more than sender prestige or generic importance.
- Prefer cc instead of archive for newsletters, webinars, and FYI list traffic that is worth a skimmed read.
- Prefer archive instead of cc when the mail should be kept only as reference and does not merit a skimmed read.
- If a message is already in junk/spam but is still research-adjacent (for example journals, conferences, plasma physics, acoustics, machine learning, physics), prefer archive over trash unless it is obviously scammy.
- Compare the message against the distilled manual policy before choosing an action.
- Treat any provided local factor hints as probabilistic evidence, not as absolute truth.
- Confidence is 0.0 to 1.0.
- Keep reason and signals short.`

const defaultRequestTimeout = 20 * time.Second

type OpenAIClassifier struct {
	BaseURL      string
	Model        string
	SystemPrompt string
	HTTPClient   *http.Client
	Timeout      time.Duration
}

type chatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func (c OpenAIClassifier) Classify(ctx context.Context, message Message) (Decision, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if baseURL == "" {
		return Decision{}, fmt.Errorf("mail triage classifier base URL is required")
	}
	model := strings.TrimSpace(c.Model)
	if model == "" {
		model = "local"
	}
	systemPrompt := strings.TrimSpace(c.SystemPrompt)
	if systemPrompt == "" {
		systemPrompt = DefaultSystemPrompt
	}
	body, _ := json.Marshal(map[string]any{"model": model, "temperature": 0, "max_tokens": 256, "response_format": map[string]any{"type": "json_object"}, "chat_template_kwargs": map[string]any{"enable_thinking": false}, "messages": []map[string]string{{"role": "system", "content": systemPrompt}, {"role": "user", "content": buildUserPrompt(message)}}})
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = defaultRequestTimeout
	}
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Decision{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return Decision{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return Decision{}, fmt.Errorf("mail triage classifier HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	var payload chatCompletionResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return Decision{}, err
	}
	if len(payload.Choices) == 0 {
		return Decision{}, nil
	}
	content := strings.TrimSpace(stripThinkingPreamble(stripCodeFence(payload.Choices[0].Message.Content)))
	if content == "" {
		return Decision{}, nil
	}
	var decision Decision
	if err := json.Unmarshal([]byte(content), &decision); err != nil {
		return Decision{}, err
	}
	decision.Model = model
	return normalizeDecision(decision), nil
}

func buildUserPrompt(message Message) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Message ID: %s\n", strings.TrimSpace(message.ID))
	if value := strings.TrimSpace(message.Provider); value != "" {
		fmt.Fprintf(&b, "Provider: %s\n", value)
	}
	if value := strings.TrimSpace(message.AccountLabel); value != "" {
		fmt.Fprintf(&b, "Account: %s\n", value)
	}
	if value := strings.TrimSpace(message.AccountAddress); value != "" {
		fmt.Fprintf(&b, "Account address: %s\n", value)
	}
	if value := strings.TrimSpace(message.Sender); value != "" {
		fmt.Fprintf(&b, "From: %s\n", value)
	}
	if len(message.Recipients) > 0 {
		fmt.Fprintf(&b, "Recipients: %s\n", strings.Join(message.Recipients, ", "))
	}
	if value := strings.TrimSpace(message.Subject); value != "" {
		fmt.Fprintf(&b, "Subject: %s\n", value)
	}
	if value := strings.TrimSpace(message.Snippet); value != "" {
		fmt.Fprintf(&b, "Snippet: %s\n", value)
	}
	if value := strings.TrimSpace(message.Body); value != "" {
		body := value
		if len(body) > 6000 {
			body = body[:6000]
		}
		fmt.Fprintf(&b, "Body:\n%s\n", body)
	}
	if len(message.Labels) > 0 {
		fmt.Fprintf(&b, "Labels: %s\n", strings.Join(message.Labels, ", "))
	}
	fmt.Fprintf(&b, "Has attachments: %t\n", message.HasAttachments)
	fmt.Fprintf(&b, "Is read: %t\n", message.IsRead)
	fmt.Fprintf(&b, "Is flagged: %t\n", message.IsFlagged)
	if message.ReviewCount > 0 {
		fmt.Fprintf(&b, "Manual review corpus size: %d\n", message.ReviewCount)
	}
	if len(message.PolicySummary) > 0 {
		fmt.Fprintf(&b, "Treat the following distilled manual-review policy as authoritative mailbox-specific guidance:\n")
		fmt.Fprintf(&b, "Distilled mailbox policy from manual reviews:\n")
		for _, line := range message.PolicySummary {
			fmt.Fprintf(&b, "- %s\n", strings.TrimSpace(line))
		}
	}
	if len(message.LocalHints) > 0 {
		fmt.Fprintf(&b, "Local factor hints:\n")
		for _, line := range message.LocalHints {
			fmt.Fprintf(&b, "- %s\n", strings.TrimSpace(line))
		}
	}
	if message.ProtectedTopic {
		fmt.Fprintf(&b, "Protected topic: true\n")
	}
	if message.AgeDays > 0 {
		fmt.Fprintf(&b, "Age days: %d\n", message.AgeDays)
	}
	if len(message.Examples) > 0 {
		fmt.Fprintf(&b, "Representative reviewed examples:\n")
		for i, example := range message.Examples {
			if i >= maxTrainingExamples {
				break
			}
			fmt.Fprintf(&b, "- action=%s; folder=%s; from=%s; subject=%s\n", strings.TrimSpace(example.Action), strings.TrimSpace(example.Folder), strings.TrimSpace(example.Sender), strings.TrimSpace(example.Subject))
		}
	}
	return strings.TrimSpace(b.String())
}

func stripCodeFence(raw string) string {
	clean := strings.TrimSpace(raw)
	if !strings.HasPrefix(clean, "```") {
		return clean
	}
	lines := strings.Split(clean, "\n")
	if len(lines) == 0 {
		return clean
	}
	lines = lines[1:]
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "```" {
		lines = lines[:len(lines)-1]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func stripThinkingPreamble(raw string) string {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return clean
	}
	if strings.HasPrefix(clean, "<think>") {
		if idx := strings.Index(clean, "</think>"); idx >= 0 {
			clean = clean[idx+len("</think>"):]
		}
	}
	if strings.HasPrefix(clean, "</think>") {
		clean = clean[len("</think>"):]
	}
	return strings.TrimSpace(clean)
}

var protectedTopicKeywords = []string{"acoustic", "acoustics", "bayesian", "fortran", "fusion", "gcc", "gfortran", "itpcp", "machine learning", "ml", "physics", "plasma", "scientific computing", "tu graz"}

type trainingDatum struct {
	Review ReviewedExample
	Tokens []string
}

type binaryBayesModel struct {
	posDocs    int
	negDocs    int
	posTokens  map[string]int
	negTokens  map[string]int
	posTotal   int
	negTotal   int
	vocabulary map[string]struct{}
}

type actionStatModel struct {
	senderCounts map[string]map[Action]int
	domainCounts map[string]map[Action]int
}

type TrainingModel struct {
	spam           binaryBayesModel
	actionRequired binaryBayesModel
	skim           binaryBayesModel
	reference      binaryBayesModel
	actionStats    actionStatModel
	rules          []DeterministicRule
	protected      []string
}

type localEvidence struct {
	Factors FactorScores
	Rule    *DeterministicRule
	Signals []string
}

func newBinaryBayesModel() binaryBayesModel {
	return binaryBayesModel{posTokens: map[string]int{}, negTokens: map[string]int{}, vocabulary: map[string]struct{}{}}
}

func (m *binaryBayesModel) add(tokens []string, positive bool) {
	if len(tokens) == 0 {
		return
	}
	seen := map[string]struct{}{}
	if positive {
		m.posDocs++
	} else {
		m.negDocs++
	}
	for _, token := range tokens {
		clean := strings.TrimSpace(strings.ToLower(token))
		if clean == "" {
			continue
		}
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		m.vocabulary[clean] = struct{}{}
		if positive {
			m.posTokens[clean]++
			m.posTotal++
		} else {
			m.negTokens[clean]++
			m.negTotal++
		}
	}
}

func (m binaryBayesModel) score(tokens []string) float64 {
	if m.posDocs == 0 || m.negDocs == 0 {
		return 0.5
	}
	vocabSize := float64(max(1, len(m.vocabulary)))
	totalDocs := float64(m.posDocs + m.negDocs)
	logPos := math.Log(float64(m.posDocs) / totalDocs)
	logNeg := math.Log(float64(m.negDocs) / totalDocs)
	seen := map[string]struct{}{}
	for _, token := range tokens {
		clean := strings.TrimSpace(strings.ToLower(token))
		if clean == "" {
			continue
		}
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		posCount := float64(m.posTokens[clean] + 1)
		negCount := float64(m.negTokens[clean] + 1)
		logPos += math.Log(posCount / (float64(m.posTotal) + vocabSize))
		logNeg += math.Log(negCount / (float64(m.negTotal) + vocabSize))
	}
	return logistic(logPos - logNeg)
}

func logistic(value float64) float64 {
	if value > 50 {
		return 1
	}
	if value < -50 {
		return 0
	}
	return 1 / (1 + math.Exp(-value))
}

func trainModel(reviews []ReviewedExample, rules []DeterministicRule) *TrainingModel {
	data := normalizeTrainingData(reviews)
	model := &TrainingModel{spam: newBinaryBayesModel(), actionRequired: newBinaryBayesModel(), skim: newBinaryBayesModel(), reference: newBinaryBayesModel(), actionStats: actionStatModel{senderCounts: map[string]map[Action]int{}, domainCounts: map[string]map[Action]int{}}, rules: append([]DeterministicRule(nil), rules...), protected: append([]string(nil), protectedTopicKeywords...)}
	for _, datum := range data {
		action := toAction(datum.Review.Action)
		if action == "" {
			continue
		}
		sender := normalizeSender(datum.Review.Sender)
		domain := senderDomain(sender)
		if sender != "" {
			incrementActionCount(model.actionStats.senderCounts, sender, action)
		}
		if domain != "" {
			incrementActionCount(model.actionStats.domainCounts, domain, action)
		}
		if positive, ok := spamTrainingLabel(datum.Review); ok {
			model.spam.add(datum.Tokens, positive)
		}
		if positive, ok := actionTrainingLabel(datum.Review); ok {
			model.actionRequired.add(datum.Tokens, positive)
		}
		if positive, ok := skimTrainingLabel(datum.Review); ok {
			model.skim.add(datum.Tokens, positive)
		}
		if positive, ok := referenceTrainingLabel(datum.Review); ok {
			model.reference.add(datum.Tokens, positive)
		}
	}
	return model
}

func normalizeTrainingData(reviews []ReviewedExample) []trainingDatum {
	clean := normalizeReviewedExamples(reviews)
	out := make([]trainingDatum, 0, len(clean))
	for _, review := range clean {
		out = append(out, trainingDatum{Review: review, Tokens: reviewTokens(review)})
	}
	return out
}

func reviewTokens(review ReviewedExample) []string {
	return messageTokens(Message{Sender: review.Sender, Subject: review.Subject, Labels: compactTokens([]string{review.Folder}), AgeDays: 0, IsRead: false, IsFlagged: false})
}
