package mailtriage

import (
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
	"time"
)

var protectedTopicKeywords = []string{
	"acoustic",
	"acoustics",
	"bayesian",
	"fortran",
	"fusion",
	"gcc",
	"gfortran",
	"itpcp",
	"machine learning",
	"ml",
	"physics",
	"plasma",
	"scientific computing",
	"tu graz",
}

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
	return binaryBayesModel{
		posTokens:  map[string]int{},
		negTokens:  map[string]int{},
		vocabulary: map[string]struct{}{},
	}
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
	model := &TrainingModel{
		spam:           newBinaryBayesModel(),
		actionRequired: newBinaryBayesModel(),
		skim:           newBinaryBayesModel(),
		reference:      newBinaryBayesModel(),
		actionStats: actionStatModel{
			senderCounts: map[string]map[Action]int{},
			domainCounts: map[string]map[Action]int{},
		},
		rules:     append([]DeterministicRule(nil), rules...),
		protected: append([]string(nil), protectedTopicKeywords...),
	}
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
		out = append(out, trainingDatum{
			Review: review,
			Tokens: reviewTokens(review),
		})
	}
	return out
}

func reviewTokens(review ReviewedExample) []string {
	return messageTokens(Message{
		Sender:    review.Sender,
		Subject:   review.Subject,
		Labels:    compactTokens([]string{review.Folder}),
		AgeDays:   0,
		IsRead:    false,
		IsFlagged: false,
	})
}

func messageTokens(message Message) []string {
	tokens := []string{}
	addTokenSet := func(prefix string, value string) {
		for _, token := range splitTokenTerms(value) {
			tokens = append(tokens, prefix+token)
		}
	}
	sender := normalizeSender(message.Sender)
	if sender != "" {
		tokens = append(tokens, "sender:"+sender)
		if domain := senderDomain(sender); domain != "" {
			tokens = append(tokens, "domain:"+domain)
		}
		if machineLikeSender(sender) {
			tokens = append(tokens, "sender_kind:machine")
		}
	}
	for _, label := range message.Labels {
		addTokenSet("label:", label)
	}
	addTokenSet("subject:", message.Subject)
	addTokenSet("snippet:", message.Snippet)
	addTokenSet("body:", message.Body)
	for _, recipient := range message.Recipients {
		clean := normalizeSender(recipient)
		if clean != "" {
			tokens = append(tokens, "recipient:"+clean)
			if domain := senderDomain(clean); domain != "" {
				tokens = append(tokens, "recipient_domain:"+domain)
			}
		}
	}
	if strings.TrimSpace(message.AccountAddress) != "" && recipientContains(message.Recipients, message.AccountAddress) {
		tokens = append(tokens, "recipient:self")
	}
	if message.HasAttachments {
		tokens = append(tokens, "has:attachment")
	}
	if message.IsFlagged {
		tokens = append(tokens, "has:flagged")
	}
	if message.IsRead {
		tokens = append(tokens, "has:read")
	}
	if machineLikeSender(sender) {
		tokens = append(tokens, "sender:machine")
	}
	if notificationLikeText(message.Subject, message.Snippet) {
		tokens = append(tokens, "kind:notification")
	}
	if protectedTopic(message, protectedTopicKeywords) {
		tokens = append(tokens, "kind:protected")
	}
	return compactTokens(tokens)
}

func splitTokenTerms(values ...string) []string {
	var out []string
	for _, value := range values {
		clean := strings.ToLower(strings.TrimSpace(value))
		if clean == "" {
			continue
		}
		replacer := strings.NewReplacer(
			"\n", " ",
			"\t", " ",
			",", " ",
			".", " ",
			":", " ",
			";", " ",
			"(", " ",
			")", " ",
			"[", " ",
			"]", " ",
			"<", " ",
			">", " ",
			"/", " ",
			"\\", " ",
			"\"", " ",
			"'", " ",
			"!", " ",
			"?", " ",
			"#", " ",
			"*", " ",
			"=", " ",
			"|", " ",
			"_", " ",
		)
		for _, term := range strings.Fields(replacer.Replace(clean)) {
			if len(term) < 2 {
				continue
			}
			out = append(out, term)
		}
	}
	return out
}

func compactTokens(tokens []string) []string {
	out := make([]string, 0, len(tokens))
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
		out = append(out, clean)
	}
	return out
}

func incrementActionCount(dst map[string]map[Action]int, key string, action Action) {
	if key == "" || action == "" {
		return
	}
	bucket := dst[key]
	if bucket == nil {
		bucket = map[Action]int{}
		dst[key] = bucket
	}
	bucket[action]++
}

func toAction(raw string) Action {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(ActionInbox):
		return ActionInbox
	case string(ActionCC):
		return ActionCC
	case string(ActionArchive):
		return ActionArchive
	case string(ActionTrash):
		return ActionTrash
	default:
		return ""
	}
}

func spamTrainingLabel(review ReviewedExample) (bool, bool) {
	action := strings.ToLower(strings.TrimSpace(review.Action))
	switch classifyFolderKind(review.Folder) {
	case "junk":
		if action == "trash" {
			return true, true
		}
		if action == "inbox" || action == "archive" || action == "cc" {
			return false, true
		}
	case "inbox":
		if action == "inbox" || action == "cc" || action == "archive" {
			return false, true
		}
	}
	return false, false
}

func actionTrainingLabel(review ReviewedExample) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(review.Action)) {
	case "inbox":
		return true, true
	case "cc", "archive", "trash":
		return false, true
	default:
		return false, false
	}
}

func skimTrainingLabel(review ReviewedExample) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(review.Action)) {
	case "cc":
		return true, true
	case "archive", "trash":
		return false, true
	default:
		return false, false
	}
}

func referenceTrainingLabel(review ReviewedExample) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(review.Action)) {
	case "archive":
		return true, true
	case "trash", "cc":
		return false, true
	default:
		return false, false
	}
}

func (m *TrainingModel) Score(message Message) localEvidence {
	if m == nil {
		return localEvidence{Factors: FactorScores{Spam: 0.5, ActionRequired: 0.5, Skim: 0.5, Reference: 0.5, Staleness: heuristicStaleness(message)}}
	}
	tokens := messageTokens(message)
	factors := FactorScores{
		Spam:           m.spam.score(tokens),
		ActionRequired: m.actionRequired.score(tokens),
		Skim:           m.skim.score(tokens),
		Reference:      m.reference.score(tokens),
		Staleness:      heuristicStaleness(message),
	}
	signals := []string{
		fmt.Sprintf("spam=%.2f", factors.Spam),
		fmt.Sprintf("action=%.2f", factors.ActionRequired),
		fmt.Sprintf("skim=%.2f", factors.Skim),
		fmt.Sprintf("reference=%.2f", factors.Reference),
		fmt.Sprintf("staleness=%.2f", factors.Staleness),
	}
	if rule := m.matchDeterministicRule(message); rule != nil {
		signals = append(signals, fmt.Sprintf("rule:%s %s -> %s", rule.Scope, rule.Key, rule.Action))
		return localEvidence{Factors: factors, Rule: rule, Signals: dedupeStrings(signals)}
	}
	if sender := normalizeSender(message.Sender); sender != "" {
		if stats := m.actionStats.senderCounts[sender]; len(stats) > 0 {
			signals = append(signals, dominantActionSignal("sender", sender, stats))
		}
		if domain := senderDomain(sender); domain != "" {
			if stats := m.actionStats.domainCounts[domain]; len(stats) > 0 {
				signals = append(signals, dominantActionSignal("domain", domain, stats))
			}
		}
	}
	if message.IsFlagged {
		signals = append(signals, "flagged")
	}
	if protectedTopic(message, m.protected) {
		signals = append(signals, "protected_topic")
	}
	if likelyDirectMessage(message) {
		signals = append(signals, "direct_recipient")
	}
	return localEvidence{Factors: factors, Signals: dedupeStrings(signals)}
}

func dominantActionSignal(scope, key string, counts map[Action]int) string {
	actions := []Action{ActionInbox, ActionCC, ActionArchive, ActionTrash}
	bestAction := ActionInbox
	bestCount := 0
	total := 0
	for _, action := range actions {
		count := counts[action]
		total += count
		if count > bestCount {
			bestAction = action
			bestCount = count
		}
	}
	if total == 0 || bestCount == 0 {
		return ""
	}
	return fmt.Sprintf("%s:%s usually %s (%d/%d)", scope, key, bestAction, bestCount, total)
}

func (m *TrainingModel) matchDeterministicRule(message Message) *DeterministicRule {
	if m == nil {
		return nil
	}
	sender := normalizeSender(message.Sender)
	domain := senderDomain(sender)
	for _, rule := range m.rules {
		switch rule.Scope {
		case "sender":
			if sender != "" && strings.EqualFold(sender, rule.Key) {
				copy := rule
				return &copy
			}
		case "domain":
			if domain != "" && strings.EqualFold(domain, rule.Key) {
				copy := rule
				return &copy
			}
		case "folder":
			if folderListContains(message.Labels, rule.Key) {
				copy := rule
				return &copy
			}
		}
	}
	return nil
}

func folderListContains(labels []string, want string) bool {
	for _, label := range labels {
		if strings.EqualFold(strings.TrimSpace(label), strings.TrimSpace(want)) {
			return true
		}
	}
	return false
}

func heuristicStaleness(message Message) float64 {
	if message.ReceivedAt.IsZero() {
		return 0.25
	}
	ageDays := int(max(0, int(time.Since(message.ReceivedAt).Hours()/24)))
	score := 0.1
	switch {
	case ageDays >= 90:
		score += 0.55
	case ageDays >= 30:
		score += 0.40
	case ageDays >= 14:
		score += 0.25
	case ageDays >= 7:
		score += 0.15
	}
	if notificationLikeText(message.Subject, message.Snippet) {
		score += 0.15
	}
	if machineLikeSender(message.Sender) {
		score += 0.10
	}
	if message.IsFlagged || likelyDirectMessage(message) {
		score -= 0.25
	}
	if protectedTopic(message, protectedTopicKeywords) {
		score -= 0.10
	}
	return clamp01(score)
}

func likelyDirectMessage(message Message) bool {
	if strings.TrimSpace(message.AccountAddress) == "" {
		return false
	}
	if !recipientContains(message.Recipients, message.AccountAddress) {
		return false
	}
	return len(message.Recipients) <= 3
}

func recipientContains(recipients []string, account string) bool {
	needle := normalizeSender(account)
	if needle == "" {
		return false
	}
	for _, recipient := range recipients {
		if normalizeSender(recipient) == needle {
			return true
		}
	}
	return false
}

func machineLikeSender(raw string) bool {
	sender := normalizeSender(raw)
	for _, snippet := range []string{"noreply", "no-reply", "notification", "notifications", "bot@", "mailer-daemon", "daemon", "news@", "newsletter", "alerts@", "alert@"} {
		if strings.Contains(sender, snippet) {
			return true
		}
	}
	return false
}

func notificationLikeText(values ...string) bool {
	joined := strings.ToLower(strings.Join(values, " "))
	for _, snippet := range []string{"notification", "newsletter", "digest", "alert", "security alert", "job_id", "project was moved", "codespaces", "expire", "expir", "what's new", "what’s new"} {
		if strings.Contains(joined, snippet) {
			return true
		}
	}
	return false
}

func protectedTopic(message Message, topics []string) bool {
	joined := strings.ToLower(strings.Join([]string{message.Subject, message.Snippet, message.Body}, "\n"))
	for _, topic := range topics {
		if strings.Contains(joined, strings.ToLower(strings.TrimSpace(topic))) {
			return true
		}
	}
	return false
}

func clamp01(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func buildDeterministicRules(reviews []ReviewedExample) []DeterministicRule {
	clean := normalizeReviewedExamples(reviews)
	if len(clean) == 0 {
		return nil
	}
	senderCounts := map[string]map[string]int{}
	domainCounts := map[string]map[string]int{}
	for _, review := range clean {
		sender := normalizeSender(review.Sender)
		if sender != "" {
			incrementNestedCount(senderCounts, sender, review.Action)
			if domain := senderDomain(sender); domain != "" {
				incrementNestedCount(domainCounts, domain, review.Action)
			}
		}
	}
	rules := []DeterministicRule{}
	for _, stat := range collectDominantRules(senderCounts, 4, 1.0) {
		if !senderRuleAllowed(stat.key) {
			continue
		}
		rules = append(rules, DeterministicRule{
			Scope:   "sender",
			Key:     stat.key,
			Action:  toAction(stat.action),
			Support: stat.total,
			Purity:  float64(stat.dominant) / float64(stat.total),
			Reason:  "exact sender pattern from manual reviews",
		})
	}
	for _, stat := range collectDominantRules(domainCounts, 8, 1.0) {
		if !domainRuleAllowed(stat.key, stat.action) {
			continue
		}
		rules = append(rules, DeterministicRule{
			Scope:   "domain",
			Key:     stat.key,
			Action:  toAction(stat.action),
			Support: stat.total,
			Purity:  float64(stat.dominant) / float64(stat.total),
			Reason:  "exact domain pattern from manual reviews",
		})
	}
	sort.Slice(rules, func(i, j int) bool {
		if rules[i].Scope != rules[j].Scope {
			return rules[i].Scope < rules[j].Scope
		}
		if rules[i].Support != rules[j].Support {
			return rules[i].Support > rules[j].Support
		}
		return rules[i].Key < rules[j].Key
	})
	return rules
}

func senderRuleAllowed(sender string) bool {
	if sender == "" || protectedTopic(Message{Sender: sender, Subject: sender, Snippet: sender}, protectedTopicKeywords) {
		return false
	}
	for _, snippet := range []string{"noreply", "no-reply", "notification", "news@", "newsletter", "mail@", "alerts@", "bot@", "online.tugraz.at", "qodo.ai", "academia-mail.com", "iter.org"} {
		if strings.Contains(sender, snippet) {
			return true
		}
	}
	return false
}

func domainRuleAllowed(domain, action string) bool {
	if domain == "" {
		return false
	}
	if strings.HasSuffix(domain, "tugraz.at") && action == "trash" {
		return false
	}
	for _, snippet := range []string{"academia-mail.com", "qodo.ai", "iter.org"} {
		if strings.Contains(domain, snippet) {
			return true
		}
	}
	return false
}

func buildInconsistentPatterns(reviews []ReviewedExample) []InconsistentPattern {
	clean := normalizeReviewedExamples(reviews)
	senderCounts := map[string]map[string]int{}
	for _, review := range clean {
		if sender := normalizeSender(review.Sender); sender != "" {
			incrementNestedCount(senderCounts, sender, review.Action)
		}
	}
	out := make([]InconsistentPattern, 0, len(senderCounts))
	for sender, counts := range senderCounts {
		if len(counts) < 2 {
			continue
		}
		total := 0
		actions := make([]string, 0, len(counts))
		for action, count := range counts {
			total += count
			actions = append(actions, action)
		}
		if total < 4 {
			continue
		}
		slices.Sort(actions)
		out = append(out, InconsistentPattern{
			Scope:   "sender",
			Key:     sender,
			Count:   total,
			Actions: actions,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Key < out[j].Key
	})
	if len(out) > 8 {
		out = out[:8]
	}
	return out
}
