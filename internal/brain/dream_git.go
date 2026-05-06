package brain

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// gitTimeoutPerInvocation caps each shell-out so a stuck git process can't
// hang the sleep cycle.
const gitTimeoutPerInvocation = 30 * time.Second

// gitRepoAge returns the duration between the oldest commit reachable from
// HEAD and now. ok is false when the directory is not a git work tree or
// the git binary is unavailable. The intent is a safety floor for
// last-touch heuristics: a repo younger than the cold threshold cannot
// reliably tell which targets are stale.
func gitRepoAge(workTree string) (time.Duration, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeoutPerInvocation)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", workTree,
		"log", "--reverse", "--format=%ct", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return 0, false
	}
	line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	if line == "" {
		return 0, false
	}
	ts, err := strconv.ParseInt(line, 10, 64)
	if err != nil {
		return 0, false
	}
	oldest := time.Unix(ts, 0)
	age := time.Since(oldest)
	if age < 0 {
		age = 0
	}
	return age, true
}

// gitFileTouchMap returns a map of repo-relative slash path -> last-touch
// time, computed from HEAD's commit history in one shell-out. ok is false
// when the directory is not a git work tree or the git binary is
// unavailable; callers should fall back to filesystem mtime in that case.
func gitFileTouchMap(workTree string) (map[string]time.Time, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeoutPerInvocation)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", workTree,
		"log", "--name-only", "--format=__C__%ct", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return nil, false
	}
	touch := map[string]time.Time{}
	var current time.Time
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "__C__") {
			tsStr := strings.TrimPrefix(line, "__C__")
			ts, err := strconv.ParseInt(tsStr, 10, 64)
			if err != nil {
				continue
			}
			current = time.Unix(ts, 0)
			continue
		}
		path := strings.TrimSpace(line)
		if path == "" {
			continue
		}
		if existing, ok := touch[path]; ok && !existing.Before(current) {
			continue
		}
		touch[path] = current
	}
	return touch, true
}
