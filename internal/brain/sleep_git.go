package brain

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/brain/sleepconv"
)

const sleepGitPacketLimit = 96 * 1024

type sleepGitPacket struct {
	Markdown string
	Scope    string
	Used     bool
}

func buildSleepGitPacket(vault Vault, now time.Time) sleepGitPacket {
	root := vault.BrainRoot()
	if _, ok := gitWorkTreeRoot(root); !ok {
		return sleepGitPacket{
			Markdown: "_Git history unavailable for this brain root._",
			Scope:    "unavailable",
		}
	}
	base, ok := latestSleepCommit(root)
	scope := ""
	// Include full diffs (--patch) so the model can see changed brain note
	// content, but exclude reports/, data/, and episodic/ from the commit
	// log: those directories accumulate large new files (sleep/scout reports,
	// ledger entries, episodic logs) whose full content is noise for NREM/REM.
	// The previous sleep commit is shown separately with full --patch so the
	// model still sees the previous sleep report body.
	args := []string{
		"log", "--date=iso-strict", "--patch", "--stat",
		"--summary", "--find-renames",
		"--format=commit %h%nDate: %cI%nSubject: %s%n",
	}
	previousSleep := ""
	if ok {
		scope = "since previous sleep commit " + shortHash(base) + " inclusive"
		args = append(args, inclusiveCommitRange(root, base),
			"--", ".", ":!reports/", ":!data/", ":!episodic/")
		var err error
		previousSleep, err = gitOutput(root,
			"show", "--date=iso-strict", "--patch", "--stat",
			"--summary", "--find-renames",
			"--format=commit %h%nDate: %cI%nSubject: %s%n", base, "--")
		if err != nil {
			previousSleep = "git show failed: " + err.Error()
		}
	} else {
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		scope = "since " + start.Format("2006-01-02 15:04:05 -0700")
		args = append(args, "--since="+start.Format(time.RFC3339), "HEAD",
			"--", ".", ":!reports/", ":!data/", ":!episodic/")
	}
	logText, err := gitOutput(root, args...)
	if err != nil {
		logText = "git log failed: " + err.Error()
	}
	status, err := gitOutput(root, "status", "--short")
	if err != nil {
		status = "git status failed: " + err.Error()
	}
	diff, err := gitOutput(root, "diff", "--patch", "--stat", "--find-renames", "HEAD", "--")
	if err != nil {
		diff = "git diff failed: " + err.Error()
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Scope: %s\n\n", scope)
	fmt.Fprintln(&b, "### Commits")
	fmt.Fprintln(&b)
	if strings.TrimSpace(logText) == "" {
		fmt.Fprintln(&b, "_No committed brain changes in scope._")
	} else {
		b.WriteString(strings.TrimRight(logText, "\n"))
	}
	if previousSleep != "" {
		fmt.Fprintln(&b)
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, "### Previous sleep commit")
		fmt.Fprintln(&b)
		b.WriteString(strings.TrimRight(previousSleep, "\n"))
	}
	fmt.Fprintln(&b)
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "### Working tree")
	fmt.Fprintln(&b)
	if strings.TrimSpace(status) == "" {
		fmt.Fprintln(&b, "_Clean._")
	} else {
		fmt.Fprintln(&b, "```")
		b.WriteString(strings.TrimRight(status, "\n"))
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, "```")
	}
	if strings.TrimSpace(diff) != "" {
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, "```diff")
		b.WriteString(strings.TrimRight(diff, "\n"))
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, "```")
	}
	return sleepGitPacket{
		Markdown: trimSleepGitPacket(b.String()),
		Scope:    scope,
		Used:     true,
	}
}

func latestSleepCommit(workTree string) (string, bool) {
	out, err := gitOutput(workTree, "log", "--grep=^brain sleep:", "--format=%H", "-n", "1", "HEAD")
	if err != nil {
		return "", false
	}
	hash := strings.TrimSpace(out)
	if hash == "" {
		return "", false
	}
	return hash, true
}

// latestSleepCommitTime returns the commit time of the most recent
// `brain sleep: …` commit in the brain repo. Used to bound the
// conversation-log scan in buildSleepConversations.
func latestSleepCommitTime(workTree string) (time.Time, bool) {
	hash, ok := latestSleepCommit(workTree)
	if !ok {
		return time.Time{}, false
	}
	out, err := gitOutput(workTree, "show", "-s", "--format=%cI", hash)
	if err != nil {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(out))
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

func inclusiveCommitRange(workTree, hash string) string {
	if _, err := gitOutput(workTree, "rev-parse", hash+"^"); err != nil {
		return hash
	}
	return hash + "^..HEAD"
}

func gitWorkTreeRoot(workTree string) (string, bool) {
	out, err := gitOutput(workTree, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", false
	}
	root := strings.TrimSpace(out)
	return root, root != ""
}

func gitOutput(workTree string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeoutPerInvocation)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", workTree}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return "", fmt.Errorf("%w: %s", err, msg)
		}
		return "", err
	}
	// Replace invalid UTF-8 sequences (binary file patches, etc.) so
	// the packet is always valid for JSON encoding and LLM submission.
	return strings.ToValidUTF8(string(out), "�"), nil
}

func trimSleepGitPacket(raw string) string {
	if len(raw) <= sleepGitPacketLimit {
		return raw
	}
	head := sleepGitPacketLimit * 2 / 3
	tail := sleepGitPacketLimit - head
	return raw[:head] + "\n\n[truncated middle]\n\n" + raw[len(raw)-tail:]
}

func shortHash(hash string) string {
	if len(hash) < 12 {
		return hash
	}
	return hash[:12]
}

// gatherSleepConversations resolves the previous-sleep anchor and
// reads interactive Claude Code + Codex logs from there forward.
func gatherSleepConversations(vault Vault, sphere Sphere, now time.Time) sleepconv.Result {
	home := homeOrEmpty()
	if home == "" {
		return sleepconv.Result{}
	}
	return sleepconv.Build(home, string(sphere), previousSleepWindow(vault, now), now)
}

// gatherSleepActivity walks the same session logs but extracts
// tool-call traces — files touched, URLs fetched, git/gh/sloptools
// commands run, sub-agents dispatched. The day's actual experience
// the sleep stage must consolidate.
func gatherSleepActivity(vault Vault, sphere Sphere, now time.Time) sleepconv.Activity {
	home := homeOrEmpty()
	if home == "" {
		return sleepconv.Activity{}
	}
	return sleepconv.BuildActivity(home, string(sphere), previousSleepWindow(vault, now), now)
}

func previousSleepWindow(vault Vault, now time.Time) time.Time {
	if t, ok := latestSleepCommitTime(vault.BrainRoot()); ok {
		return t
	}
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -1)
}

func homeOrEmpty() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return h
}
