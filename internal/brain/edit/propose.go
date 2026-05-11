package edit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/brain/evidence"
)

// ProposeConflicts writes proposed commitment files for high-confidence
// conflicting evidence entries that were not applied tonight. These land
// in brain/commitments/_proposed/<date>-<slug>.md for morning GTD review.
func ProposeConflicts(brainRoot string, entries []evidence.Entry, now time.Time) error {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	date := now.Format("2006-01-02")
	proposedDir := filepath.Join(brainRoot, "commitments", "_proposed")
	if err := os.MkdirAll(proposedDir, 0o755); err != nil {
		return fmt.Errorf("propose: mkdir: %w", err)
	}

	var written int
	for _, e := range entries {
		if e.Verdict != evidence.VerdictConflicting {
			continue
		}
		if e.Confidence < 0.8 {
			continue
		}
		if e.Applied || e.Reverted {
			continue
		}
		slug := sanitize(e.Entity)
		path := filepath.Join(proposedDir, date+"-"+slug+".md")
		// Don't overwrite if already proposed today.
		if _, err := os.Stat(path); err == nil {
			continue
		}
		body := buildProposal(e, date)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			return fmt.Errorf("propose: write %s: %w", path, err)
		}
		written++
	}

	// Also surface sparse notes as a batch proposal if any.
	var sparse []evidence.Entry
	for _, e := range entries {
		if e.Verdict == evidence.VerdictSkipped {
			sparse = append(sparse, e)
		}
	}
	if len(sparse) > 0 {
		if err := writeSparseProposal(proposedDir, date, sparse); err != nil {
			return err
		}
	}

	_ = written // logged by caller
	return nil
}

func buildProposal(e evidence.Entry, date string) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("kind: commitment\n")
	b.WriteString("status: proposed\n")
	fmt.Fprintf(&b, "created: %s\n", date)
	fmt.Fprintf(&b, "entity: %s\n", e.Entity)
	b.WriteString("source: brain-night-evidence\n")
	b.WriteString("---\n\n")
	fmt.Fprintf(&b, "# Proposed: review conflicting evidence for %s\n\n", e.Entity)
	fmt.Fprintf(&b, "Scout found conflicting claim (confidence %.0f%%):\n\n", e.Confidence*100)
	fmt.Fprintf(&b, "- %s\n", e.Claim)
	if e.Source != "" {
		fmt.Fprintf(&b, "\nSource: %s\n", e.Source)
	}
	if e.SuggestedEdit != "" {
		fmt.Fprintf(&b, "\nSuggested edit: %s\n", e.SuggestedEdit)
	}
	b.WriteString("\nReview and apply or reject.\n")
	return b.String()
}

func writeSparseProposal(dir, date string, entries []evidence.Entry) error {
	path := filepath.Join(dir, date+"-sparse-notes.md")
	if _, err := os.Stat(path); err == nil {
		return nil // already written today
	}
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("kind: commitment\n")
	b.WriteString("status: proposed\n")
	fmt.Fprintf(&b, "created: %s\n", date)
	b.WriteString("source: brain-night-evidence\n")
	b.WriteString("---\n\n")
	b.WriteString("# Proposed: improve sparse folder notes\n\n")
	b.WriteString("These notes were skipped by scout because their body is < 200 chars.\n")
	b.WriteString("Add a sentence or two of content so future runs can verify them.\n\n")
	for _, e := range entries {
		fmt.Fprintf(&b, "- `%s`\n", e.Entity)
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}
