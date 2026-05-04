package meetingkickoff

import (
	"sort"
)

// MaxBreakouts caps the breakout count per the meetings convention
// (`brain/conventions/meetings.md` §5: 2–4 huddles).
const MaxBreakouts = 4

// MinBreakoutPosters is the minimum number of *posters* required for a
// cluster to count as a breakout — single-poster topics become
// pair-off-cycle candidates instead.
const MinBreakoutPosters = 2

// Breakout is a synchronous huddle suggestion. Participants is the
// union of the posters and the people they want to sync with, sorted
// alphabetically for determinism.
type Breakout struct {
	Participants []string
	Posters      []string
	Posts        []Post
}

// PairOffCycle is a single-poster topic that becomes an off-cycle pair
// rather than occupying a breakout slot.
type PairOffCycle struct {
	Poster string
	With   []string
	Body   string
}

// ClusterResult is the output of clustering: the breakouts that fit
// within MaxBreakouts plus everyone routed to off-cycle pairing
// (single-poster topics and overflow beyond the cap).
type ClusterResult struct {
	Breakouts    []Breakout
	PairOffCycle []PairOffCycle
}

// Cluster groups posts into breakouts via union-find on overlapping
// person mentions. A cluster with at least MinBreakoutPosters distinct
// posters becomes a breakout; everything else (single-poster topics
// and any breakouts past MaxBreakouts) becomes pair-off-cycle.
func Cluster(posts []Post) ClusterResult {
	uf := newUnionFind()
	for _, post := range posts {
		uf.add(canonicalName(post.Sender))
		for _, mention := range post.Mentions {
			uf.add(canonicalName(mention))
			uf.union(canonicalName(post.Sender), canonicalName(mention))
		}
	}
	groups := map[string][]Post{}
	for _, post := range posts {
		root := uf.find(canonicalName(post.Sender))
		groups[root] = append(groups[root], post)
	}
	candidates := make([]Breakout, 0, len(groups))
	for _, members := range groups {
		candidates = append(candidates, breakoutFromCluster(members))
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if len(candidates[i].Posters) != len(candidates[j].Posters) {
			return len(candidates[i].Posters) > len(candidates[j].Posters)
		}
		return joinedKey(candidates[i].Participants) < joinedKey(candidates[j].Participants)
	})
	result := ClusterResult{}
	for _, candidate := range candidates {
		if len(candidate.Posters) < MinBreakoutPosters || len(result.Breakouts) >= MaxBreakouts {
			result.PairOffCycle = append(result.PairOffCycle, pairsFromCluster(candidate)...)
			continue
		}
		result.Breakouts = append(result.Breakouts, candidate)
	}
	sort.SliceStable(result.PairOffCycle, func(i, j int) bool {
		if canonicalName(result.PairOffCycle[i].Poster) != canonicalName(result.PairOffCycle[j].Poster) {
			return canonicalName(result.PairOffCycle[i].Poster) < canonicalName(result.PairOffCycle[j].Poster)
		}
		return result.PairOffCycle[i].Body < result.PairOffCycle[j].Body
	})
	return result
}

func breakoutFromCluster(posts []Post) Breakout {
	posters := uniqueOrdered(func(yield func(string)) {
		for _, post := range posts {
			yield(post.Sender)
		}
	})
	participantSet := map[string]string{}
	for _, post := range posts {
		participantSet[canonicalName(post.Sender)] = post.Sender
		for _, mention := range post.Mentions {
			participantSet[canonicalName(mention)] = mention
		}
	}
	participants := make([]string, 0, len(participantSet))
	for _, name := range participantSet {
		participants = append(participants, name)
	}
	sort.Slice(participants, func(i, j int) bool {
		return canonicalName(participants[i]) < canonicalName(participants[j])
	})
	sortedPosts := append([]Post(nil), posts...)
	sort.SliceStable(sortedPosts, func(i, j int) bool {
		return canonicalName(sortedPosts[i].Sender) < canonicalName(sortedPosts[j].Sender)
	})
	return Breakout{Participants: participants, Posters: posters, Posts: sortedPosts}
}

func pairsFromCluster(b Breakout) []PairOffCycle {
	out := make([]PairOffCycle, 0, len(b.Posts))
	for _, post := range b.Posts {
		out = append(out, PairOffCycle{
			Poster: post.Sender,
			With:   append([]string(nil), post.Mentions...),
			Body:   post.Body,
		})
	}
	return out
}

func joinedKey(values []string) string {
	canon := make([]string, len(values))
	for i, value := range values {
		canon[i] = canonicalName(value)
	}
	sort.Strings(canon)
	return string(byteJoin(canon, '|'))
}

func byteJoin(values []string, sep byte) []byte {
	if len(values) == 0 {
		return nil
	}
	total := 0
	for _, v := range values {
		total += len(v) + 1
	}
	out := make([]byte, 0, total)
	for i, v := range values {
		if i > 0 {
			out = append(out, sep)
		}
		out = append(out, v...)
	}
	return out
}

func uniqueOrdered(walk func(yield func(string))) []string {
	seen := map[string]struct{}{}
	out := []string{}
	walk(func(name string) {
		key := canonicalName(name)
		clean := name
		if key == "" || clean == "" {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, clean)
	})
	return out
}

type unionFind struct {
	parent map[string]string
	rank   map[string]int
}

func newUnionFind() *unionFind {
	return &unionFind{parent: map[string]string{}, rank: map[string]int{}}
}

func (uf *unionFind) add(key string) {
	if key == "" {
		return
	}
	if _, ok := uf.parent[key]; ok {
		return
	}
	uf.parent[key] = key
	uf.rank[key] = 0
}

func (uf *unionFind) find(key string) string {
	if _, ok := uf.parent[key]; !ok {
		return key
	}
	for uf.parent[key] != key {
		uf.parent[key] = uf.parent[uf.parent[key]]
		key = uf.parent[key]
	}
	return key
}

func (uf *unionFind) union(a, b string) {
	if a == "" || b == "" {
		return
	}
	ra, rb := uf.find(a), uf.find(b)
	if ra == rb {
		return
	}
	if uf.rank[ra] < uf.rank[rb] {
		ra, rb = rb, ra
	}
	uf.parent[rb] = ra
	if uf.rank[ra] == uf.rank[rb] {
		uf.rank[ra]++
	}
}
