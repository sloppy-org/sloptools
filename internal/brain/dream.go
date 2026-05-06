package brain

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// dreamColdThreshold is the age above which a wikilink target counts as
// "cold" for dreaming and prune-links.
const dreamColdThreshold = 365 * 24 * time.Hour

// dreamRepoAgeFloor guards prune-link scans against false positives in a
// freshly-initialised brain repo. Filesystem mtimes survive the initial
// import, so notes with old original mtimes look "cold" even though the
// repo just started tracking them; we refuse to declare anything cold
// until the repo itself is at least this old. Mirrors dreamColdThreshold.
const dreamRepoAgeFloor = dreamColdThreshold

// dreamDefaultBudget is the default note count for DreamReportRun when
// callers pass budget <= 0.
const dreamDefaultBudget = 10

// dreamMaxSuggestionsPerSource caps prose-mention suggestions per picked
// note so a single rich note cannot drown the report.
const dreamMaxSuggestionsPerSource = 5

// dreamCadenceWeights maps strategic cadences to picker weights. Anything
// not listed (including empty) gets weight 1 so a strategic note never
// drops out of the strategic half.
var dreamCadenceWeights = map[string]int{
	"daily":     5,
	"weekly":    4,
	"monthly":   3,
	"quarterly": 2,
	"annual":    1,
}

// dreamPoolPrefixes are the slash-prefixes (relative to brain root) that
// dreaming considers part of the topics/projects pool.
var dreamPoolPrefixes = []string{"topics/", "projects/"}

// dreamWordBoundary matches a word character on either side of a mention
// candidate to enforce one-sided word-boundary checks.
var dreamWordBoundary = regexp.MustCompile(`\w`)

// dreamNote is the per-note state the dreaming pass needs.
type dreamNote struct {
	rel         string // brain-relative slash path, e.g. "topics/foo.md"
	abs         string
	displayName string
	stem        string // filename without ".md"
	strategic   bool
	focus       string
	cadence     string
	lastSeen    string
	mtime       time.Time
	gitTouch    time.Time // most recent commit on HEAD touching this file (zero if untracked)
	body        string
	wikilinks   []dreamWikilink
}

// dreamWikilink captures a wikilink occurrence in source order with both
// the raw content and the canonicalised target rel path.
type dreamWikilink struct {
	raw    string // text inside [[...]]
	target string // brain-relative slash path with .md
	alias  string // alias text after | or ""
}

// DreamReportRun produces the Phase 7 free-association evidence packet for
// the named sphere. See package docs (and the spec) for the full contract.
func DreamReportRun(cfg *Config, sphere Sphere, budget int) (*DreamReport, error) {
	if budget <= 0 {
		budget = dreamDefaultBudget
	}
	vault, err := cfgVault(cfg, sphere)
	if err != nil {
		return nil, err
	}
	pool, byRel, err := loadDreamPool(vault)
	if err != nil {
		return nil, err
	}
	picked := pickDreamNotes(pool, budget, dreamDailySeed(time.Now()))

	report := &DreamReport{
		Sphere:    vault.Sphere,
		Topics:    make([]string, 0, len(picked)),
		Generated: time.Now().UTC().Format(time.RFC3339),
	}
	for _, note := range picked {
		report.Topics = append(report.Topics, note.rel)
	}

	report.CrossLinks = collectCrossLinkSuggestions(picked, pool)
	report.Cold = collectColdLinks(picked, byRel, time.Now())
	if report.CrossLinks == nil {
		report.CrossLinks = []LinkSuggestion{}
	}
	if report.Cold == nil {
		report.Cold = []ColdLink{}
	}
	return report, nil
}

// DreamPruneLinksScan walks every brain note in the sphere and emits a
// ColdLink for each wikilink whose target is cold and is not protected by
// strategic=true or focus=core.
//
// Safety floor: when the brain git repo's oldest reachable commit is
// younger than dreamRepoAgeFloor, the scan returns an empty cold list.
// In a freshly-initialised repo, filesystem mtimes survive from before
// the import, so without this floor every note with an old original
// mtime would falsely be called cold and its inbound wikilinks would be
// pruned in bulk.
func DreamPruneLinksScan(cfg *Config, sphere Sphere) ([]ColdLink, error) {
	vault, err := cfgVault(cfg, sphere)
	if err != nil {
		return nil, err
	}
	if age, ok := gitRepoAge(vault.BrainRoot()); ok && age < dreamRepoAgeFloor {
		return []ColdLink{}, nil
	}
	allNotes, byRel, err := loadAllBrainNotes(vault)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	var out []ColdLink
	for _, note := range allNotes {
		for _, link := range note.wikilinks {
			cold, days, ok := classifyColdTarget(link, byRel, now)
			if !ok || !cold {
				continue
			}
			out = append(out, ColdLink{
				Source:        note.rel,
				Target:        link.target,
				LastTouchDays: days,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Source != out[j].Source {
			return out[i].Source < out[j].Source
		}
		return out[i].Target < out[j].Target
	})
	return out, nil
}

// classifyColdTarget returns whether the wikilink target is cold, the age
// in days, and whether the target was found at all. Targets protected by
// strategic=true or focus=core are not cold.
func classifyColdTarget(link dreamWikilink, byRel map[string]*dreamNote, now time.Time) (bool, int, bool) {
	if link.target == "" {
		return false, 0, false
	}
	target, ok := byRel[link.target]
	if !ok {
		return false, 0, false
	}
	if target.strategic || target.focus == "core" {
		return false, 0, true
	}
	touch := lastTouchTime(target)
	age := now.Sub(touch)
	if age <= dreamColdThreshold {
		return false, int(age / (24 * time.Hour)), true
	}
	return true, int(age / (24 * time.Hour)), true
}

// collectColdLinks scans wikilinks in the picked notes and emits ColdLink
// records when target last-touch is older than the cold threshold and the
// target is not protected by strategic or focus=core.
func collectColdLinks(picked []*dreamNote, byRel map[string]*dreamNote, now time.Time) []ColdLink {
	var out []ColdLink
	for _, source := range picked {
		for _, link := range source.wikilinks {
			cold, days, ok := classifyColdTarget(link, byRel, now)
			if !ok || !cold {
				continue
			}
			out = append(out, ColdLink{
				Source:        source.rel,
				Target:        link.target,
				LastTouchDays: days,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Source != out[j].Source {
			return out[i].Source < out[j].Source
		}
		return out[i].Target < out[j].Target
	})
	return out
}

// lastTouchTime is the most recent of: frontmatter last_seen, filesystem
// mtime, and git's last-touch commit time. Taking the maximum prevents a
// freshly committed note (whose mtime predates the import) from looking
// cold just because its on-disk timestamp is old.
func lastTouchTime(note *dreamNote) time.Time {
	best := time.Time{}
	if note.lastSeen != "" {
		if t, err := parseLastSeen(note.lastSeen); err == nil && t.After(best) {
			best = t
		}
	}
	if note.mtime.After(best) {
		best = note.mtime
	}
	if note.gitTouch.After(best) {
		best = note.gitTouch
	}
	return best
}

func parseLastSeen(raw string) (time.Time, error) {
	value := strings.TrimSpace(raw)
	for _, layout := range []string{time.RFC3339, "2006-01-02", "2006/01/02"} {
		if t, err := time.Parse(layout, value); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognised last_seen %q", raw)
}

// equalFold compares two strings case-insensitively after trimming.
func equalFold(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}
