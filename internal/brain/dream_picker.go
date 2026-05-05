package brain

import (
	"crypto/sha256"
	"encoding/binary"
	"math/rand"
	"sort"
	"strings"
	"time"
)

// dreamDailySeed derives a deterministic int64 seed from the UTC date so
// two report runs on the same day pick the same notes.
func dreamDailySeed(now time.Time) int64 {
	stamp := now.UTC().Format("2006-01-02")
	sum := sha256.Sum256([]byte("brain.dream/" + stamp))
	return int64(binary.BigEndian.Uint64(sum[:8]))
}

// pickDreamNotes selects up to budget notes: half from strategic notes
// weighted by inverse cadence, half random from the rest. The result is
// stable for a fixed seed and pool.
func pickDreamNotes(pool []*dreamNote, budget int, seed int64) []*dreamNote {
	if budget <= 0 || len(pool) == 0 {
		return nil
	}
	if budget > len(pool) {
		budget = len(pool)
	}
	rng := rand.New(rand.NewSource(seed))

	var strategic, rest []*dreamNote
	for _, note := range pool {
		if note.strategic {
			strategic = append(strategic, note)
		} else {
			rest = append(rest, note)
		}
	}
	strategicHalf, restHalf := splitDreamBudget(budget, len(strategic), len(rest))

	picks := make([]*dreamNote, 0, strategicHalf+restHalf)
	picks = append(picks, weightedSample(rng, strategic, strategicHalf, dreamCadenceWeight)...)
	picks = append(picks, uniformSample(rng, rest, restHalf)...)
	sort.Slice(picks, func(i, j int) bool { return picks[i].rel < picks[j].rel })
	return picks
}

// splitDreamBudget aims for a strategic/rest half-and-half split, then tops
// up from whichever side has slack so the two halves sum to budget.
func splitDreamBudget(budget, strategicAvailable, restAvailable int) (int, int) {
	strategicHalf := budget / 2
	if strategicHalf > strategicAvailable {
		strategicHalf = strategicAvailable
	}
	restHalf := budget - strategicHalf
	if restHalf > restAvailable {
		restHalf = restAvailable
	}
	pad := budget - strategicHalf - restHalf
	if pad <= 0 {
		return strategicHalf, restHalf
	}
	if extra := strategicAvailable - strategicHalf; extra > 0 {
		add := extra
		if add > pad {
			add = pad
		}
		strategicHalf += add
		pad -= add
	}
	if pad > 0 {
		if extra := restAvailable - restHalf; extra > 0 {
			add := extra
			if add > pad {
				add = pad
			}
			restHalf += add
		}
	}
	return strategicHalf, restHalf
}

func dreamCadenceWeight(note *dreamNote) int {
	if w, ok := dreamCadenceWeights[note.cadence]; ok {
		return w
	}
	return 1
}

// weightedSample draws count items without replacement using the supplied
// weight function. Items with weight <= 0 are treated as weight 1.
func weightedSample(rng *rand.Rand, items []*dreamNote, count int, weight func(*dreamNote) int) []*dreamNote {
	if count <= 0 || len(items) == 0 {
		return nil
	}
	if count > len(items) {
		count = len(items)
	}
	pool := append([]*dreamNote(nil), items...)
	weights := make([]int, len(pool))
	total := 0
	for i, item := range pool {
		w := weight(item)
		if w <= 0 {
			w = 1
		}
		weights[i] = w
		total += w
	}
	out := make([]*dreamNote, 0, count)
	for len(out) < count && len(pool) > 0 {
		pick := rng.Intn(total)
		idx := 0
		for cum := 0; idx < len(pool); idx++ {
			cum += weights[idx]
			if pick < cum {
				break
			}
		}
		if idx >= len(pool) {
			idx = len(pool) - 1
		}
		out = append(out, pool[idx])
		total -= weights[idx]
		pool = append(pool[:idx], pool[idx+1:]...)
		weights = append(weights[:idx], weights[idx+1:]...)
	}
	return out
}

// uniformSample draws count distinct items using a Fisher-Yates partial
// shuffle. The input order does not matter for stability when the rng is
// seeded.
func uniformSample(rng *rand.Rand, items []*dreamNote, count int) []*dreamNote {
	if count <= 0 || len(items) == 0 {
		return nil
	}
	if count > len(items) {
		count = len(items)
	}
	pool := append([]*dreamNote(nil), items...)
	rng.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
	return pool[:count]
}

// collectCrossLinkSuggestions emits LinkSuggestion records for prose
// mentions in picked notes that point at other pool notes' display_name or
// stem and lack an existing wikilink. Capped per source.
func collectCrossLinkSuggestions(picked []*dreamNote, pool []*dreamNote) []LinkSuggestion {
	var out []LinkSuggestion
	for _, source := range picked {
		linked := wikilinkTargetSet(source)
		body := stripFrontMatter(source.body)
		seenForSource := map[string]bool{}
		var perSource []LinkSuggestion
		for _, candidate := range pool {
			if candidate.rel == source.rel {
				continue
			}
			if linked[candidate.rel] {
				continue
			}
			if !anyMentionMatches(body, candidateNeedles(candidate)) {
				continue
			}
			if seenForSource[candidate.rel] {
				continue
			}
			seenForSource[candidate.rel] = true
			perSource = append(perSource, LinkSuggestion{
				From:   source.rel,
				To:     candidate.rel,
				Reason: "prose mention without wikilink",
			})
			if len(perSource) >= dreamMaxSuggestionsPerSource {
				break
			}
		}
		out = append(out, perSource...)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].From != out[j].From {
			return out[i].From < out[j].From
		}
		return out[i].To < out[j].To
	})
	return out
}

func anyMentionMatches(body string, needles []string) bool {
	for _, needle := range needles {
		if mentionMatches(body, needle) {
			return true
		}
	}
	return false
}

func candidateNeedles(note *dreamNote) []string {
	var needles []string
	if name := strings.TrimSpace(note.displayName); name != "" {
		needles = append(needles, name)
	}
	if stem := strings.TrimSpace(note.stem); stem != "" && !equalFold(stem, note.displayName) {
		needles = append(needles, stem)
	}
	return needles
}

// mentionMatches checks whether body contains needle (case-insensitive)
// with at least one side being a word boundary, i.e. not flanked by word
// characters on both sides. An exact substring match alone is too noisy.
func mentionMatches(body, needle string) bool {
	if needle == "" {
		return false
	}
	loweredBody := strings.ToLower(body)
	loweredNeedle := strings.ToLower(needle)
	start := 0
	for {
		idx := strings.Index(loweredBody[start:], loweredNeedle)
		if idx < 0 {
			return false
		}
		abs := start + idx
		end := abs + len(loweredNeedle)
		leftWord := abs > 0 && dreamWordBoundary.MatchString(string(loweredBody[abs-1]))
		rightWord := end < len(loweredBody) && dreamWordBoundary.MatchString(string(loweredBody[end]))
		if !leftWord || !rightWord {
			return true
		}
		start = abs + 1
		if start >= len(loweredBody) {
			return false
		}
	}
}

func wikilinkTargetSet(note *dreamNote) map[string]bool {
	out := map[string]bool{}
	for _, link := range note.wikilinks {
		if link.target == "" {
			continue
		}
		out[link.target] = true
	}
	return out
}
