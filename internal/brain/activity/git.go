package activity

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// compactGitHistory returns a compact summary of brain git commits in
// [since, until). For multi-day gaps it groups by date.
func compactGitHistory(brainRoot string, since, until time.Time) string {
	// Exclude report/data/episodic paths that add noise.
	cmd := exec.Command("git", "-C", brainRoot, "log",
		"--pretty=format:%ai %s",
		"--after="+since.UTC().Format(time.RFC3339),
		"--before="+until.UTC().Format(time.RFC3339),
		"--",
		".", ":!reports/", ":!data/", ":!episodic/", ":!evidence/",
	)
	out, err := cmd.Output()
	if err != nil || len(strings.TrimSpace(string(out))) == 0 {
		return ""
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	gapDays := until.Sub(since).Hours() / 24.0

	if gapDays < 1.5 {
		// Single day: list all commits up to 15.
		var b strings.Builder
		count := 0
		for _, l := range lines {
			if l == "" || count >= 15 {
				break
			}
			// Strip timestamp prefix (29 chars).
			msg := l
			if len(l) > 26 {
				msg = strings.TrimSpace(l[26:])
			}
			fmt.Fprintf(&b, "- %s\n", msg)
			count++
		}
		if len(lines) > 15 {
			fmt.Fprintf(&b, "- ...and %d more commits\n", len(lines)-15)
		}
		return b.String()
	}

	// Multi-day: group by date.
	byDate := map[string][]string{}
	var dates []string
	for _, l := range lines {
		if len(l) < 10 {
			continue
		}
		date := l[:10]
		msg := strings.TrimSpace(l[26:])
		if msg == "" {
			continue
		}
		if _, ok := byDate[date]; !ok {
			dates = append(dates, date)
		}
		byDate[date] = append(byDate[date], msg)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dates)))

	var b strings.Builder
	for _, date := range dates {
		msgs := byDate[date]
		if len(msgs) == 1 {
			fmt.Fprintf(&b, "- %s: %s\n", date, msgs[0])
		} else {
			fmt.Fprintf(&b, "- %s: %d commits (%s, ...)\n", date, len(msgs), truncMsg(msgs[0], 60))
		}
	}
	return b.String()
}

func truncMsg(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}
