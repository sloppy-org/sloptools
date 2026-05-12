package backend

// toolUsage tracks per-tool call counts against the per-stage quota map.
// A tool absent from caps has no quota; the counter is incremented for
// every attempt (success or failure) so a model that hammers a broken
// tool cannot bypass the cap.
type toolUsage struct {
	caps   map[string]int
	counts map[string]int
}

func newToolUsage(caps map[string]int) *toolUsage {
	return &toolUsage{caps: caps, counts: make(map[string]int)}
}

func (u *toolUsage) exceeded(name string) bool {
	if u == nil || len(u.caps) == 0 {
		return false
	}
	max, ok := u.caps[name]
	if !ok || max <= 0 {
		return false
	}
	return u.counts[name] >= max
}

func (u *toolUsage) record(name string) {
	if u == nil || len(u.caps) == 0 {
		return
	}
	if _, ok := u.caps[name]; !ok {
		return
	}
	u.counts[name]++
}

func (u *toolUsage) cap(name string) int {
	if u == nil {
		return 0
	}
	return u.caps[name]
}
