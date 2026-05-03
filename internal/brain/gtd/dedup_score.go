package gtd

import (
	"math"
	"regexp"
	"strings"
	"time"
)

var dedupTokenRe = regexp.MustCompile(`[a-z0-9]+`)

func buildCandidates(entries []CommitmentEntry, aggregates []Aggregate, opts ScanOptions) []Candidate {
	group := aggregateMembership(aggregates)
	var out []Candidate
	for i := 0; i < len(entries); i++ {
		for j := i + 1; j < len(entries); j++ {
			if group[entries[i].Path] == group[entries[j].Path] {
				continue
			}
			candidate, ok := scoreCandidate(entries[i], entries[j], opts)
			if ok {
				out = append(out, candidate)
			}
		}
	}
	return out
}

func aggregateMembership(aggregates []Aggregate) map[string]string {
	out := map[string]string{}
	for _, aggregate := range aggregates {
		for _, path := range aggregate.Paths {
			out[path] = aggregate.ID
		}
	}
	return out
}

func scoreCandidate(a, b CommitmentEntry, opts ScanOptions) (Candidate, bool) {
	id := CandidateID(a.Path, b.Path)
	if decisionState(a.Commitment, b.Commitment, id) == "not_duplicate" {
		return Candidate{}, false
	}
	score, reason := deterministicScore(a.Commitment, b.Commitment)
	state := decisionState(a.Commitment, b.Commitment, id)
	candidate := Candidate{ID: id, Paths: []string{a.Path, b.Path}, Score: score, Confidence: score, Reasoning: reason, Detector: "deterministic", ReviewState: state}
	if opts.LLM != nil && score >= opts.LLMThreshold {
		if reviewed, err := opts.LLM.ReviewSimilarity(a, b, score); err == nil && reviewed.Same {
			candidate.Confidence = reviewed.Confidence
			candidate.Reasoning = strings.TrimSpace(reviewed.Reasoning)
			candidate.Detector = "llm"
		}
	}
	if candidate.Detector == "llm" {
		return candidate, candidate.Confidence >= opts.CandidateThreshold
	}
	return candidate, candidate.Score >= opts.DeterministicThreshold
}

func decisionState(a, b Commitment, id string) string {
	if containsString(a.Dedup.NotDuplicates, id) || containsString(b.Dedup.NotDuplicates, id) {
		return "not_duplicate"
	}
	if containsString(a.Dedup.Deferred, id) || containsString(b.Dedup.Deferred, id) {
		return "deferred"
	}
	return "open"
}

func deterministicScore(a, b Commitment) (float64, string) {
	if normalizedOutcome(a) != "" && normalizedOutcome(a) == normalizedOutcome(b) {
		return 0.98, "exact normalized outcome match"
	}
	score := 0.55 * jaccard(tokens(normalizedOutcome(a)), tokens(normalizedOutcome(b)))
	score += 0.15 * overlapScore(a.People, b.People)
	score += 0.08 * sameStringScore(a.Project, b.Project)
	score += 0.04 * sameStringScore(a.EffectiveTrack(), b.EffectiveTrack())
	score += 0.10 * dateProximityScore(a, b)
	score += 0.08 * sourceContextScore(a.SourceBindings, b.SourceBindings)
	return math.Round(score*100) / 100, deterministicReason(score)
}

func deterministicReason(score float64) string {
	switch {
	case score >= 0.82:
		return "high deterministic overlap across outcome, people, project, timing, and source context"
	case score >= 0.50:
		return "moderate deterministic overlap; local LLM review can decide"
	default:
		return "low deterministic overlap"
	}
}

func normalizedOutcome(c Commitment) string {
	value := strings.TrimSpace(c.Outcome)
	if value == "" {
		value = c.Title
	}
	return strings.Join(tokens(value), " ")
}

func tokens(value string) []string {
	raw := dedupTokenRe.FindAllString(strings.ToLower(value), -1)
	var out []string
	for _, token := range raw {
		if len(token) > 2 && !dedupStopWords[token] {
			out = append(out, token)
		}
	}
	return compactStrings(out)
}

var dedupStopWords = map[string]bool{
	"and": true, "for": true, "the": true, "with": true, "from": true,
	"into": true, "this": true, "that": true, "you": true, "your": true,
}

func jaccard(a, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	set := map[string]bool{}
	for _, token := range a {
		set[token] = true
	}
	intersection := 0
	for _, token := range b {
		if set[token] {
			intersection++
		}
		set[token] = true
	}
	return float64(intersection) / float64(len(set))
}

func overlapScore(a, b []string) float64 {
	return jaccard(tokens(strings.Join(a, " ")), tokens(strings.Join(b, " ")))
}

func sameStringScore(a, b string) float64 {
	if strings.TrimSpace(a) != "" && strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b)) {
		return 1
	}
	return 0
}

func dateProximityScore(a, b Commitment) float64 {
	ad, aok := commitmentDate(a)
	bd, bok := commitmentDate(b)
	if !aok || !bok {
		return 0
	}
	days := math.Abs(ad.Sub(bd).Hours() / 24)
	if days <= 3 {
		return 1
	}
	if days <= 14 {
		return 0.5
	}
	return 0
}

func commitmentDate(c Commitment) (time.Time, bool) {
	for _, value := range []string{c.Due, c.FollowUp, c.LocalOverlay.Due, c.LocalOverlay.FollowUp} {
		if parsed, ok := parseDedupDate(value); ok {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func parseDedupDate(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func sourceContextScore(a, b []SourceBinding) float64 {
	var left, right []string
	for _, binding := range a {
		left = append(left, binding.Provider, binding.Summary)
	}
	for _, binding := range b {
		right = append(right, binding.Provider, binding.Summary)
	}
	return jaccard(tokens(strings.Join(left, " ")), tokens(strings.Join(right, " ")))
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
