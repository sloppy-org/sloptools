package edit

import (
	"bufio"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/brain/evidence"
)

// DetectReverts reads git log since lastCommit and marks evidence entries
// as reverted when their entity paths appear in revert commits. Returns
// the set of reverted entity paths.
func DetectReverts(brainRoot, runID string, since time.Time) ([]string, error) {
	// git log --name-only --since=<time> to find reverted files.
	sinceStr := since.UTC().Format(time.RFC3339)
	cmd := exec.Command("git", "-C", brainRoot, "log",
		"--name-only",
		"--pretty=format:%H %s",
		"--since="+sinceStr,
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("feedback: git log: %w", err)
	}

	// Identify revert commits by subject prefix.
	var revertedPaths []string
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	inRevert := false
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			inRevert = false
			continue
		}
		if strings.Contains(line, " ") && (strings.HasPrefix(line, "Revert") ||
			strings.Contains(strings.ToLower(line), "revert")) {
			inRevert = true
			continue
		}
		if inRevert && strings.HasSuffix(line, ".md") {
			revertedPaths = append(revertedPaths, line)
		}
	}

	// Mark reverted entries in the evidence log.
	for _, path := range revertedPaths {
		// Convert file path to entity (strip leading brain/ prefix if present).
		entity := strings.TrimPrefix(path, "brain/")
		_ = evidence.MarkReverted(brainRoot, entity)
	}

	return revertedPaths, nil
}
