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
	"github.com/sloppy-org/sloptools/internal/brain/evidence"
	"gopkg.in/yaml.v3"
)

// Pick is one entity selected for verification.
type Pick struct {
	Path      string    `json:"path"` // vault-relative
	Title     string    `json:"title"`
	Cadence   string    `json:"cadence"`
	LastSeen  time.Time `json:"last_seen,omitempty"`
	Score     float64   `json:"score"`
	Reason    string    `json:"reason"`
	NeedsRevw bool      `json:"needs_review,omitempty"` // set when an explicit `needs_review` flag is present
	// UncertaintyMarkers carries body-level claims that the agent should
	// verify specifically — populated when the note body contains lines
	// like `- needs review: ...`, or inline `(unverified)` / `(unconfirmed)`
	// markers. Empty for canonical entity picks scored only on cadence.
	UncertaintyMarkers []string `json:"uncertainty_markers,omitempty"`
}

// PickerOpts configures the picker.
type PickerOpts struct {
	BrainRoot string
	Roots     []string  // entity directories under brain root; default people/, projects/, institutions/
	Now       time.Time // default time.Now()
	TopN      int       // default 10 per root
	// CooldownDays excludes notes that already have a scout report under
	// <brain>/reports/scout/*/<slug>.md younger than this many days.
	// Zero (the default sentinel) is replaced with 7 so a weekly nightly
	// rotates through fresh picks. A negative value disables the filter
	// entirely (every eligible note is in scope, even if scouted today).
	CooldownDays int
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
	if opts.CooldownDays == 0 {
		opts.CooldownDays = 7
	}
	cooldown := loadCooldown(opts.BrainRoot, opts.Now, opts.CooldownDays)
	roots := opts.Roots
	if len(roots) == 0 {
		roots = []string{"people", "projects", "institutions", "folders"}
	}
	out := []Pick{}
	for _, root := range roots {
		picks, err := scoreRoot(opts.BrainRoot, root, opts.Now)
		if err != nil {
			return nil, err
		}
		// Drop notes that were already scouted within the cooldown
		// window. cooldown is keyed by sanitized vault-relative slug.
		if len(cooldown) > 0 {
			kept := picks[:0]
			for _, p := range picks {
				if cooldown[sanitizePath(p.Path)] {
					continue
				}
				kept = append(kept, p)
			}
			picks = kept
		}
		// Drop zero-score picks before TopN crop. folders/ is the new
		// motivator: most folder notes have neither cadence nor explicit
		// uncertainty markers and should never be scouted.
		nonzero := picks[:0]
		for _, p := range picks {
			if p.Score > 0 {
				nonzero = append(nonzero, p)
			}
		}
		picks = nonzero
		sort.SliceStable(picks, func(i, j int) bool {
			return picks[i].Score > picks[j].Score
		})
		if len(picks) > opts.TopN {
			picks = picks[:opts.TopN]
		}
		out = append(out, picks...)
	}
	// Global sort by score so a high-uncertainty folder note outranks a
	// merely stale canonical entity in a different root. Stable so picks
	// from the same root keep their per-root order on ties.
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Score > out[j].Score
	})
	return out, nil
}

// loadCooldown walks <brain>/reports/scout/* and returns a set of
// sanitized slugs that have a per-pick report file younger than
// cooldownDays. Empty result on missing reports dir, missing slugs, or
// zero cooldown — callers must tolerate the absence.
//
// The slug key matches sanitizePath(pick.Path), which is the same
// transform used by runner.sanitize when writing the report.
func loadCooldown(brainRoot string, now time.Time, cooldownDays int) map[string]bool {
	if cooldownDays <= 0 {
		return nil
	}
	scoutDir := filepath.Join(brainRoot, "reports", "scout")
	cutoff := now.Add(-time.Duration(cooldownDays) * 24 * time.Hour)
	out := map[string]bool{}
	runs, err := os.ReadDir(scoutDir)
	if err != nil {
		return out
	}
	for _, runEntry := range runs {
		if !runEntry.IsDir() {
			continue
		}
		runDir := filepath.Join(scoutDir, runEntry.Name())
		entries, err := os.ReadDir(runDir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			info, err := e.Info()
			if err != nil || info.ModTime().Before(cutoff) {
				continue
			}
			slug := strings.TrimSuffix(e.Name(), ".md")
			out[slug] = true
		}
	}
	return out
}

// sanitizePath mirrors runner.sanitize: lowercase letters, digits, '-'
// and '_' kept; everything else collapsed to '-'; trim leading/trailing
// '-'. Used to match a Pick.Path against the scout-report slug key.
func sanitizePath(p string) string {
	out := make([]rune, 0, len(p))
	for _, r := range p {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			out = append(out, r)
		case r == '-' || r == '_':
			out = append(out, r)
		default:
			out = append(out, '-')
		}
	}
	return strings.Trim(string(out), "-")
}

// scanUncertainty walks one note body looking for explicit
// uncertainty markers and returns the list of claim lines plus a
// boost score. Markers:
//
//   - lines under any `## Open Questions` heading starting with
//     `- needs review:` (case-insensitive)
//   - inline `(unverified)`, `(unconfirmed)`, `(tbd)` or `?` at end of bullet
//
// Result is bounded so a malformed note cannot blow up the score.
func scanUncertainty(body string) ([]string, float64) {
	var markers []string
	var score float64
	inOpenQuestions := false
	lines := strings.Split(body, "\n")
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		lower := strings.ToLower(line)
		if strings.HasPrefix(line, "##") {
			inOpenQuestions = strings.Contains(lower, "open questions") || strings.Contains(lower, "open question")
			continue
		}
		if inOpenQuestions && strings.HasPrefix(lower, "- needs review:") {
			markers = append(markers, strings.TrimSpace(line[len("- needs review:"):]))
			score += 1000
			continue
		}
		if strings.HasPrefix(line, "- ") {
			if strings.Contains(lower, "(unverified)") || strings.Contains(lower, "(unconfirmed)") || strings.Contains(lower, "(tbd)") {
				markers = append(markers, strings.TrimPrefix(line, "- "))
				score += 50
			}
		}
	}
	if score > 200 && len(markers) > 0 && score < 1000 {
		// Cap inline-marker contribution so a noisy note can't crowd out
		// a single explicit `needs review:` request elsewhere.
		score = 200
	}
	if len(markers) > 8 {
		markers = markers[:8]
	}
	return markers, score
}

func scoreRoot(brainRoot, root string, now time.Time) ([]Pick, error) {
	abs := filepath.Join(brainRoot, root)
	out := []Pick{}
	err := filepath.WalkDir(abs, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return filepath.SkipDir
			}
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		note, _ := brain.ParseMarkdownNote(string(body), brain.MarkdownParseOptions{})
		if note == nil {
			return nil
		}
		rel, err := filepath.Rel(brainRoot, path)
		if err != nil {
			rel = filepath.Join(root, d.Name())
		}
		rel = filepath.ToSlash(rel)
		pick := scoreNote(brainRoot, note, rel, now)
		markers, boost := scanUncertainty(string(body))
		if len(markers) > 0 {
			pick.UncertaintyMarkers = markers
			pick.Score += boost
			if boost >= 1000 {
				pick.Reason = strings.TrimSpace(pick.Reason + "; explicit needs-review")
			} else {
				pick.Reason = strings.TrimSpace(pick.Reason + fmt.Sprintf("; %d inline markers", len(markers)))
			}
		}
		out = append(out, pick)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scout picker: walk %s: %w", abs, err)
	}
	return out, nil
}

func scoreNote(brainRoot string, note *brain.MarkdownNote, vaultRel string, now time.Time) Pick {
	cadence := scalarField(note, "cadence")
	focus := scalarField(note, "focus")
	lastSeen := parseDateField(note, "last_seen")
	strategic := boolField(note, "strategic")
	needsReview := boolField(note, "needs_review")
	title := scalarField(note, "title")
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(vaultRel), ".md")
	}
	score, reason := computeScoreWithFocus(lastSeen, focus, strategic, needsReview, now)

	// Apply yield_ratio modifier: entities whose past evidence reliably led
	// to applied edits score higher; entities where evidence was always
	// stranded (never applied) score lower. Default 0.5 = neutral.
	yr := evidence.YieldRatio(brainRoot, vaultRel, 90)
	if yr < 0.1 {
		score *= 0.5
		reason += " [low-yield×0.5]"
	} else if yr > 0.7 {
		score *= 1.3
		reason += " [high-yield×1.3]"
	}

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
// Selection signals (since 2026-05-08):
//   - explicit needs_review:        +1000 base
//   - last_seen older than 90 days: +days_since_last_seen
//   - focus weight (when set):      core=200, active=120, watch=60, parked=0
//   - strategic with positive base: ×1.25 multiplier
//
// `cadence` is intentionally NOT used. It is the user's contact /
// review rhythm with an entity (per `brain/conventions/attention.md`
// "how often the node should resurface for review, contact, or
// attention"), not a directive to scout to rescan automatically. The
// previous score formula used cadence as a rescan-frequency trigger,
// which over-picked entities the user merely meets weekly even when
// nothing about them changed. Drive scout selection from explicit
// review flags, fresh staleness, and focus alone.
//
// Notes with no needs_review, no last_seen, and no focus get score 0
// (skipped by PickEntities's TopN crop). Tag notes with
// `needs_review: true` to force a rescan, or set `focus: core/active`
// when the entity belongs in the regular scout backlog.
func computeScore(_ string, lastSeen time.Time, strategic, needsReview bool, now time.Time) (float64, string) {
	return computeScoreWithFocus(lastSeen, "", strategic, needsReview, now)
}

func computeScoreWithFocus(lastSeen time.Time, focus string, strategic, needsReview bool, now time.Time) (float64, string) {
	score := 0.0
	reason := []string{}
	if needsReview {
		score += 1000
		reason = append(reason, "needs_review flag")
	}
	if !lastSeen.IsZero() {
		days := now.Sub(lastSeen).Hours() / 24.0
		if days >= 90 {
			score += days
			reason = append(reason, fmt.Sprintf("last_seen %.0fd ago", days))
		}
	}
	if w := focusWeight(focus); w > 0 {
		score += w
		reason = append(reason, fmt.Sprintf("focus=%s", focus))
	}
	if strategic && score > 0 {
		score *= 1.25
		reason = append(reason, "strategic")
	}
	return score, strings.Join(reason, "; ")
}

// focusWeight maps the canonical focus tiers to baseline scout
// priority. core/active notes deserve regular re-verification even
// without an explicit needs_review flag; watch is a low signal;
// parked is intentionally excluded.
func focusWeight(f string) float64 {
	switch strings.ToLower(strings.TrimSpace(f)) {
	case "core":
		return 200
	case "active":
		return 120
	case "watch":
		return 60
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
