package main

import (
	"fmt"
	"os/exec"
)

// postReportComment shells out to `gh issue comment` to post the
// rendered report.md to a sloptools issue. We use the gh CLI rather
// than the GitHub API directly so the user's authenticated tokens
// flow through naturally.
func postReportComment(issue int, reportPath string) error {
	cmd := exec.Command("gh", "issue", "comment",
		fmt.Sprintf("%d", issue),
		"--repo", "sloppy-org/sloptools",
		"--body-file", reportPath,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("gh issue comment: %w; output: %s", err, string(out))
	}
	return nil
}
