package sleepconv

import (
	"regexp"
	"strconv"
	"strings"
)

// parseGitOp pulls a coarse op-and-subject out of a `git ...` shell
// command. Supports the subjects sloptools brain night runs care
// about — commit (with -m message), push, log, diff, stash, checkout,
// rebase, merge — and falls through to the second-token verb for
// anything else.
var gitCommitMessageRE = regexp.MustCompile(`(?s)git\s+commit[^"']*-m\s*["'$\s]+([^"']+)["']`)

func parseGitOp(cmd string) GitOp {
	c := strings.TrimSpace(cmd)
	if !strings.HasPrefix(c, "git ") && c != "git" {
		return GitOp{}
	}
	tokens := strings.Fields(c)
	if len(tokens) < 2 {
		return GitOp{}
	}
	op := tokens[1]
	subject := ""
	if op == "commit" {
		// Heredoc form: git commit -m "$(cat <<'EOF' …) — extract the
		// first sensible quoted-or-bare title we can find. The simple
		// regex above handles the common -m "subject" form.
		if m := gitCommitMessageRE.FindStringSubmatch(c); len(m) > 1 {
			subject = strings.TrimSpace(strings.SplitN(m[1], "\n", 2)[0])
		}
	}
	return GitOp{Op: op, Subject: subject}
}

// parseGHRef pulls owner/repo (and optional issue/PR number) out of a
// `gh` invocation. Recognises:
//
//	gh issue view <number>
//	gh issue list --repo <owner>/<repo>
//	gh pr view <owner>/<repo>#<n>
//	gh repo view <owner>/<repo>
//	gh issue create --repo <owner>/<repo> ...
//
// Returns zero-value GitHubRef when nothing useful can be parsed.
var ghRepoRE = regexp.MustCompile(`([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+)(?:#(\d+))?`)

func parseGHRef(cmd string) GitHubRef {
	c := strings.TrimSpace(cmd)
	if !strings.HasPrefix(c, "gh ") {
		return GitHubRef{}
	}
	tokens := strings.Fields(c)
	if len(tokens) < 2 {
		return GitHubRef{}
	}
	kind := ""
	switch tokens[1] {
	case "issue":
		kind = "issue"
	case "pr", "pull":
		kind = "pull"
	case "repo":
		kind = "repo"
	default:
		return GitHubRef{}
	}
	for _, t := range tokens[2:] {
		t = strings.Trim(t, "\"'`")
		if m := ghRepoRE.FindStringSubmatch(t); m != nil {
			ref := GitHubRef{Owner: m[1], Repo: m[2], Kind: kind}
			if m[3] != "" {
				if n, err := strconv.Atoi(m[3]); err == nil {
					ref.Number = n
				}
			}
			return ref
		}
	}
	return GitHubRef{Kind: kind}
}
