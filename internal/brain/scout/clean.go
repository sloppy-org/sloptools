package scout

import "github.com/sloppy-org/sloptools/internal/brain/cleanup"

// cleanReport delegates to internal/brain/cleanup so the same
// preamble/footer trim is shared with sleep, triage, and any future
// brain-night stage. See cleanup.CleanReport for the rule set.
func cleanReport(body string) string { return cleanup.CleanReport(body) }
