package gtd

import (
	"crypto/sha1"
	"encoding/hex"
	"sort"
	"strings"
	"time"
)

const (
	DefaultDeterministicThreshold = 0.82
	DefaultLLMThreshold           = 0.35
	DefaultCandidateThreshold     = 0.80
)

type CommitmentEntry struct {
	Path       string     `json:"path"`
	Commitment Commitment `json:"commitment"`
}

type ScanOptions struct {
	DeterministicThreshold float64
	LLMThreshold           float64
	CandidateThreshold     float64
	LLM                    SimilarityReviewer
}

type SimilarityReviewer interface {
	ReviewSimilarity(a, b CommitmentEntry, score float64) (LLMReview, error)
}

type LLMReview struct {
	Same       bool    `json:"same"`
	Confidence float64 `json:"confidence"`
	Reasoning  string  `json:"reasoning"`
}

type ScanResult struct {
	Aggregates []Aggregate `json:"aggregates"`
	Candidates []Candidate `json:"candidates"`
	Changed    bool        `json:"changed"`
}

type Aggregate struct {
	ID          string          `json:"id"`
	Paths       []string        `json:"paths"`
	Title       string          `json:"title,omitempty"`
	Outcome     string          `json:"outcome,omitempty"`
	Bindings    []SourceBinding `json:"bindings,omitempty"`
	BindingIDs  []string        `json:"binding_ids,omitempty"`
	ReviewState string          `json:"review_state,omitempty"`
}

type Candidate struct {
	ID          string   `json:"id"`
	Paths       []string `json:"paths"`
	Score       float64  `json:"score"`
	Confidence  float64  `json:"confidence"`
	Reasoning   string   `json:"reasoning"`
	Detector    string   `json:"detector"`
	ReviewState string   `json:"review_state"`
}

func Scan(entries []CommitmentEntry, opts ScanOptions) ScanResult {
	opts = normalizeScanOptions(opts)
	open := openCommitments(entries)
	aggregates := buildAggregates(open)
	candidates := buildCandidates(open, aggregates, opts)
	return ScanResult{Aggregates: aggregates, Candidates: candidates, Changed: false}
}

func CandidateID(pathA, pathB string) string {
	paths := []string{strings.TrimSpace(pathA), strings.TrimSpace(pathB)}
	sort.Strings(paths)
	sum := sha1.Sum([]byte(paths[0] + "\n" + paths[1]))
	return "gtd-dedup-" + hex.EncodeToString(sum[:])[:16]
}

func ApplyMerge(winner, loser *CommitmentEntry, id, outcome, decidedAt string) {
	if decidedAt == "" {
		decidedAt = time.Now().UTC().Format(time.RFC3339)
	}
	winner.Commitment.SourceBindings = mergeBindings(winner.Commitment.SourceBindings, loser.Commitment.SourceBindings)
	if strings.TrimSpace(outcome) != "" {
		winner.Commitment.Outcome = strings.TrimSpace(outcome)
	}
	winner.Commitment.Dedup.MergeHistory = append(winner.Commitment.Dedup.MergeHistory, DedupHistoryEntry{
		ID: id, MergedFrom: []string{loser.Path}, DecidedAt: decidedAt,
	})
	loser.Commitment.LocalOverlay.Status = "dropped"
	loser.Commitment.LocalOverlay.ClosedVia = "dedup_merge"
	loser.Commitment.Dedup.EquivalentTo = winner.Path
}

func MergeSourceBindings(base, extra []SourceBinding) []SourceBinding {
	return mergeBindings(base, extra)
}

func MarkNotDuplicate(a, b *CommitmentEntry, id string) {
	a.Commitment.Dedup.NotDuplicates = appendUnique(a.Commitment.Dedup.NotDuplicates, id)
	b.Commitment.Dedup.NotDuplicates = appendUnique(b.Commitment.Dedup.NotDuplicates, id)
}

func MarkDeferred(a, b *CommitmentEntry, id string) {
	a.Commitment.Dedup.Deferred = appendUnique(a.Commitment.Dedup.Deferred, id)
	b.Commitment.Dedup.Deferred = appendUnique(b.Commitment.Dedup.Deferred, id)
}

func normalizeScanOptions(opts ScanOptions) ScanOptions {
	if opts.DeterministicThreshold <= 0 {
		opts.DeterministicThreshold = DefaultDeterministicThreshold
	}
	if opts.LLMThreshold <= 0 {
		opts.LLMThreshold = DefaultLLMThreshold
	}
	if opts.CandidateThreshold <= 0 {
		opts.CandidateThreshold = DefaultCandidateThreshold
	}
	return opts
}

func openCommitments(entries []CommitmentEntry) []CommitmentEntry {
	var out []CommitmentEntry
	for _, entry := range entries {
		if isOpen(entry.Commitment) {
			out = append(out, entry)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func isOpen(commitment Commitment) bool {
	status := strings.ToLower(strings.TrimSpace(commitment.Status))
	overlay := strings.ToLower(strings.TrimSpace(commitment.LocalOverlay.Status))
	return commitment.Dedup.EquivalentTo == "" && status != "closed" &&
		status != "done" && overlay != "closed" && overlay != "dropped"
}

func buildAggregates(entries []CommitmentEntry) []Aggregate {
	groups := groupedBySource(entries)
	out := make([]Aggregate, 0, len(groups))
	for _, group := range groups {
		out = append(out, aggregateGroup(group))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func aggregateGroup(group []CommitmentEntry) Aggregate {
	agg := Aggregate{ID: aggregateID(group), ReviewState: "open"}
	for _, entry := range group {
		agg.Paths = append(agg.Paths, entry.Path)
		if agg.Title == "" {
			agg.Title = entry.Commitment.Title
		}
		if agg.Outcome == "" {
			agg.Outcome = entry.Commitment.Outcome
		}
		agg.Bindings = mergeBindings(agg.Bindings, entry.Commitment.SourceBindings)
	}
	for _, binding := range agg.Bindings {
		agg.BindingIDs = append(agg.BindingIDs, binding.StableID())
	}
	return agg
}

func aggregateID(group []CommitmentEntry) string {
	parts := make([]string, 0, len(group))
	for _, entry := range group {
		parts = append(parts, entry.Path)
	}
	sort.Strings(parts)
	sum := sha1.Sum([]byte(strings.Join(parts, "\n")))
	return "gtd-aggregate-" + hex.EncodeToString(sum[:])[:16]
}

func groupedBySource(entries []CommitmentEntry) [][]CommitmentEntry {
	parent := make([]int, len(entries))
	for i := range parent {
		parent[i] = i
	}
	seen := map[string]int{}
	for i, entry := range entries {
		for _, binding := range entry.Commitment.SourceBindings {
			id := binding.StableID()
			if id == "" {
				continue
			}
			if j, ok := seen[id]; ok {
				union(parent, i, j)
			} else {
				seen[id] = i
			}
		}
	}
	return collectGroups(entries, parent)
}

func collectGroups(entries []CommitmentEntry, parent []int) [][]CommitmentEntry {
	byRoot := map[int][]CommitmentEntry{}
	for i, entry := range entries {
		root := find(parent, i)
		byRoot[root] = append(byRoot[root], entry)
	}
	groups := make([][]CommitmentEntry, 0, len(byRoot))
	for _, group := range byRoot {
		sort.Slice(group, func(i, j int) bool { return group[i].Path < group[j].Path })
		groups = append(groups, group)
	}
	return groups
}

func find(parent []int, i int) int {
	for parent[i] != i {
		parent[i] = parent[parent[i]]
		i = parent[i]
	}
	return i
}

func union(parent []int, a, b int) {
	ra, rb := find(parent, a), find(parent, b)
	if ra != rb {
		parent[rb] = ra
	}
}

func mergeBindings(base, extra []SourceBinding) []SourceBinding {
	seen := map[string]bool{}
	out := make([]SourceBinding, 0, len(base)+len(extra))
	for _, binding := range append(append([]SourceBinding(nil), base...), extra...) {
		id := binding.StableID()
		if id == "" || seen[id] {
			continue
		}
		binding.Provider = normalizeBindingPart(binding.Provider)
		binding.Ref = strings.TrimSpace(binding.Ref)
		seen[id] = true
		out = append(out, binding)
	}
	return out
}

func appendUnique(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
