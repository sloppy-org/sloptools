// Package scout selects stale-or-uncertain canonical entities and runs a
// bounded autonomous research pass over each. The scout never edits
// canonical Markdown directly: it writes evidence reports under
// <brain>/reports/scout/<date>/<entity-slug>.md and adds suggestions to
// a queue the judge stage applies.
//
// Picker is deterministic, zero-LLM. It walks the canonical-entity
// directories (people/, projects/, institutions/), parses frontmatter,
// and scores each entity by cadence × time-since-last-seen.
package scout

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/brain"
	"gopkg.in/yaml.v3"
)

// Pick is one entity selected for verification.
type Pick struct {
	Path      string    `json:"path"`       // vault-relative
	Title     string    `json:"title"`
	Cadence   string    `json:"cadence"`
	LastSeen  time.Time `json:"last_seen,omitempty"`
	Score     float64   `json:"score"`
	Reason    string    `json:"reason"`
	NeedsRevw bool      `json:"needs_review,omitempty"` // set when an explicit `needs_review` flag is present
}

// PickerOpts configures the picker.
type PickerOpts struct {
	BrainRoot string
	Roots     []string  // entity directories under brain root; default people/, projects/, institutions/
	Now       time.Time // default time.Now()
	TopN      int       // default 10 per root
}

// Pick walks the canonical-entity directories, parses frontmatter,
// scores each entity, and returns the top N per root sorted by score
// descending.
func PickEntities(opts PickerOpts) ([]Pick, error) {
	if opts.BrainRoot == "" {
		return nil, fmt.Errorf("scout picker: BrainRoot required")
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	if opts.TopN <= 0 {
		opts.TopN = 10
	}
	roots := opts.Roots
	if len(roots) == 0 {
		roots = []string{"people", "projects", "institutions"}
	}
	out := []Pick{}
	for _, root := range roots {
		picks, err := scoreRoot(opts.BrainRoot, root, opts.Now)
		if err != nil {
			return nil, err
		}
		sort.SliceStable(picks, func(i, j int) bool {
			return picks[i].Score > picks[j].Score
		})
		if len(picks) > opts.TopN {
			picks = picks[:opts.TopN]
		}
		out = append(out, picks...)
	}
	return out, nil
}

func scoreRoot(brainRoot, root string, now time.Time) ([]Pick, error) {
	abs := filepath.Join(brainRoot, root)
	entries, err := os.ReadDir(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("scout picker: read %s: %w", abs, err)
	}
	out := []Pick{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		path := filepath.Join(abs, e.Name())
		body, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		note, _ := brain.ParseMarkdownNote(string(body), brain.MarkdownParseOptions{})
		if note == nil {
			continue
		}
		pick := scoreNote(note, filepath.Join(root, e.Name()), now)
		out = append(out, pick)
	}
	return out, nil
}

func scoreNote(note *brain.MarkdownNote, vaultRel string, now time.Time) Pick {
	cadence := scalarField(note, "cadence")
	lastSeen := parseDateField(note, "last_seen")
	strategic := boolField(note, "strategic")
	needsReview := boolField(note, "needs_review")
	title := scalarField(note, "title")
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(vaultRel), ".md")
	}
	score, reason := computeScore(cadence, lastSeen, strategic, needsReview, now)
	return Pick{
		Path:      vaultRel,
		Title:     title,
		Cadence:   cadence,
		LastSeen:  lastSeen,
		Score:     score,
		Reason:    reason,
		NeedsRevw: needsReview,
	}
}

// computeScore: higher = more in need of verification.
//
// Heuristic:
//  - explicit needs_review:    +1000 base
//  - strategic + not seen:     +50
//  - cadence weight base       (daily=1, weekly=7, monthly=30, quarterly=90)
//  - days since last_seen / cadence_days = staleness ratio
//  - no last_seen + has cadence = treat as 2× cadence (double overdue)
//
// Notes with no cadence and no needs_review get score 0 (skipped by
// PickEntities's TopN crop).
func computeScore(cadence string, lastSeen time.Time, strategic, needsReview bool, now time.Time) (float64, string) {
	score := 0.0
	reason := []string{}
	cadDays := cadenceDays(cadence)
	if needsReview {
		score += 1000
		reason = append(reason, "needs_review flag")
	}
	if cadDays > 0 {
		var ratio float64
		if lastSeen.IsZero() {
			ratio = 2.0
			reason = append(reason, "no last_seen")
		} else {
			days := now.Sub(lastSeen).Hours() / 24.0
			ratio = days / float64(cadDays)
		}
		if ratio > 0 {
			score += ratio * float64(cadDays)
			reason = append(reason, fmt.Sprintf("cadence=%s ratio=%.2f", cadence, ratio))
		}
	}
	if strategic && score > 0 {
		score *= 1.25
		reason = append(reason, "strategic")
	}
	return score, strings.Join(reason, "; ")
}

func cadenceDays(c string) int {
	switch strings.ToLower(strings.TrimSpace(c)) {
	case "daily":
		return 1
	case "weekly":
		return 7
	case "biweekly", "fortnightly":
		return 14
	case "monthly":
		return 30
	case "quarterly":
		return 90
	case "annual", "yearly":
		return 365
	}
	return 0
}

func scalarField(note *brain.MarkdownNote, name string) string {
	n, ok := note.FrontMatterField(name)
	if !ok || n == nil {
		return ""
	}
	if n.Kind == yaml.ScalarNode {
		return n.Value
	}
	return ""
}

func boolField(note *brain.MarkdownNote, name string) bool {
	v := strings.ToLower(scalarField(note, name))
	return v == "true" || v == "yes" || v == "1"
}

func parseDateField(note *brain.MarkdownNote, name string) time.Time {
	v := scalarField(note, name)
	if v == "" {
		return time.Time{}
	}
	for _, layout := range []string{"2006-01-02", time.RFC3339, "2006-01-02T15:04:05Z07:00"} {
		if t, err := time.Parse(layout, v); err == nil {
			return t
		}
	}
	return time.Time{}
}
