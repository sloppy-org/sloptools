package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/sloppy-org/sloptools/internal/brain"
)

// applyIntegrityGate snapshots brain integrity before running fn, then again
// after, and refuses success when the after-state has new broken links or
// new validation issues. On success it auto-commits and pushes the brain
// repo with the supplied commitMessage, which is how every apply leaves a
// reviewable audit trail in git history. Apply paths run with the gate
// enabled by default; the caller may pass disable=true (e.g. when already
// wrapped at a higher level) to skip the post-pass and the auto-commit.
func applyIntegrityGate(cfg *brain.Config, sphere brain.Sphere, disable bool, commitMessage string, fn func() error) error {
	if disable {
		return fn()
	}
	before, err := brain.IntegrityScan(cfg, sphere)
	if err != nil {
		return fmt.Errorf("integrity scan (before): %w", err)
	}
	if err := fn(); err != nil {
		return err
	}
	after, err := brain.IntegrityScan(cfg, sphere)
	if err != nil {
		return fmt.Errorf("integrity scan (after): %w", err)
	}
	reg := brain.CompareIntegrity(before, after)
	if reg.IsRegression() {
		emitIntegrityRegression(reg)
		return fmt.Errorf("integrity gate: apply introduced %d new broken link(s), %d new issue(s)",
			reg.NewBrokenLinks, reg.NewIssues)
	}
	if commitMessage != "" {
		brainAutoCommit(cfg, sphere, commitMessage)
	}
	return nil
}

func emitIntegrityRegression(reg brain.IntegrityRegression) {
	enc := json.NewEncoder(os.Stderr)
	enc.SetIndent("", "  ")
	if err := enc.Encode(map[string]interface{}{
		"integrity_regression": map[string]interface{}{
			"before_total_issues": reg.Before.TotalIssues,
			"after_total_issues":  reg.After.TotalIssues,
			"before_broken_links": reg.Before.BrokenLinks,
			"after_broken_links":  reg.After.BrokenLinks,
			"new_broken_links":    reg.NewBrokenLinks,
			"new_issues":          reg.NewIssues,
			"link_examples":       reg.After.LinkExamples,
		},
	}); err != nil {
		fmt.Fprintln(os.Stderr, "(failed to emit integrity regression details)")
	}
}
