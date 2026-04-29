package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSourceListTableIncludesGithubIssuesAndPRs(t *testing.T) {
	tmp := t.TempDir()
	projectDir := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}
	if err := initGitRepoDir(projectDir, "https://github.com/sloppy-org/slopshell.git"); err != nil {
		t.Fatalf("init git repo: %v", err)
	}
	scriptDir := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(scriptDir, 0o755); err != nil {
		t.Fatalf("mkdir script dir: %v", err)
	}
	writeScript(t, scriptDir, "gh", `#!/bin/sh
case "$*" in
  *"issue list"*)
    printf '%s' '[{"number":12,"title":"Fix bug","url":"https://github.com/sloppy-org/slopshell/issues/12","state":"OPEN","labels":[{"name":"gtd"}],"assignees":[{"login":"ada"}],"author":{"login":"ada"},"updatedAt":"2026-04-29T12:00:00Z"}]'
    ;;
  *"pr list"*)
    printf '%s' '[{"number":51,"title":"Add source adapters","url":"https://github.com/sloppy-org/slopshell/pull/51","state":"OPEN","labels":[{"name":"review"}],"assignees":[{"login":"ada"}],"author":{"login":"ada"},"updatedAt":"2026-04-29T12:01:00Z","reviewDecision":"REVIEW_REQUIRED","reviewRequests":[{"login":"octocat"}]}]'
    ;;
  *)
    echo unexpected gh call >&2
    exit 1
    ;;
esac
`)
	t.Setenv("PATH", scriptDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	stdout, stderr, code := captureRun(t, []string{
		"source", "list",
		"--project-dir", projectDir,
		"--provider", "github",
	})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr=%q", code, stderr)
	}
	if stderr != "" {
		t.Fatalf("unexpected stderr: %q", stderr)
	}
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2: %q", len(lines), stdout)
	}
	if !strings.Contains(stdout, "github:sloppy-org/slopshell#51") || !strings.Contains(stdout, "github:sloppy-org/slopshell#12") {
		t.Fatalf("stdout missing source refs: %q", stdout)
	}
	if !strings.Contains(stdout, "review_required") || !strings.Contains(stdout, "octocat") {
		t.Fatalf("stdout missing review metadata: %q", stdout)
	}
}

func TestSourceCommentAndCloseCallUpstreamMutations(t *testing.T) {
	tmp := t.TempDir()
	projectDir := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}
	if err := initGitRepoDir(projectDir, "https://gitlab.com/sloppy-org/slopshell.git"); err != nil {
		t.Fatalf("init git repo: %v", err)
	}
	scriptDir := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(scriptDir, 0o755); err != nil {
		t.Fatalf("mkdir script dir: %v", err)
	}
	callsFile := filepath.Join(tmp, "calls.log")
	t.Setenv("SLOPPY_CALLS_FILE", callsFile)
	writeScript(t, scriptDir, "glab", `#!/bin/sh
if [ -n "$SLOPPY_CALLS_FILE" ]; then
  printf '%s\n' "$*" >> "$SLOPPY_CALLS_FILE"
fi
case "$*" in
  *"issue list"*)
    printf '%s' '[{"iid":12,"title":"Fix bug","web_url":"https://gitlab.com/sloppy-org/slopshell/-/issues/12","state":"opened","labels":["gtd"],"assignees":[{"username":"ada"}],"author":{"username":"ada"},"updated_at":"2026-04-29T12:00:00Z"}]'
    ;;
  *"mr list"*)
    printf '%s' '[{"iid":51,"title":"Add source adapters","web_url":"https://gitlab.com/sloppy-org/slopshell/-/merge_requests/51","state":"opened","labels":["review"],"assignees":[{"username":"ada"}],"author":{"username":"ada"},"updated_at":"2026-04-29T12:01:00Z","reviewers":[{"username":"octocat"}]}]'
    ;;
  *)
    :
    ;;
esac
`)
	t.Setenv("PATH", scriptDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	stdout, stderr, code := captureRun(t, []string{
		"source", "comment",
		"--project-dir", projectDir,
		"--provider", "gitlab",
		"--kind", "merge_request",
		"--number", "51",
		"--body", "Please review the diff",
	})
	if code != 0 {
		t.Fatalf("comment exit code = %d, stderr=%q stdout=%q", code, stderr, stdout)
	}
	stdout, stderr, code = captureRun(t, []string{
		"source", "close",
		"--project-dir", projectDir,
		"--provider", "gitlab",
		"--kind", "issue",
		"--number", "12",
		"--comment", "Done upstream",
	})
	if code != 0 {
		t.Fatalf("close exit code = %d, stderr=%q stdout=%q", code, stderr, stdout)
	}
	data, err := os.ReadFile(callsFile)
	if err != nil {
		t.Fatalf("read calls file: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "mr note create 51 -R sloppy-org/slopshell -m Please review the diff") {
		t.Fatalf("calls missing mr note: %q", got)
	}
	if !strings.Contains(got, "issue close 12 -R sloppy-org/slopshell") {
		t.Fatalf("calls missing issue close: %q", got)
	}
}

func TestSourceListAutoDetectsGitLabRemote(t *testing.T) {
	tmp := t.TempDir()
	projectDir := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}
	if err := initGitRepoDir(projectDir, "https://gitlab.com/sloppy-org/slopshell.git"); err != nil {
		t.Fatalf("init git repo: %v", err)
	}
	scriptDir := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(scriptDir, 0o755); err != nil {
		t.Fatalf("mkdir script dir: %v", err)
	}
	writeScript(t, scriptDir, "glab", `#!/bin/sh
case "$*" in
  *"issue list"*)
    printf '%s' '[{"iid":12,"title":"Fix bug","web_url":"https://gitlab.com/sloppy-org/slopshell/-/issues/12","state":"opened","labels":["gtd"],"assignees":[{"username":"ada"}],"author":{"username":"ada"},"updated_at":"2026-04-29T12:00:00Z"}]'
    ;;
  *"mr list"*)
    printf '%s' '[]'
    ;;
  *)
    echo unexpected glab call >&2
    exit 1
    ;;
esac
`)
	writeScript(t, scriptDir, "gh", `#!/bin/sh
echo gh should not be used for GitLab remotes >&2
exit 1
`)
	t.Setenv("PATH", scriptDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	stdout, stderr, code := captureRun(t, []string{
		"source", "list",
		"--project-dir", projectDir,
	})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr=%q", code, stderr)
	}
	if strings.Contains(stderr, "gh should not be used") {
		t.Fatalf("auto-detect used gh for GitLab remote: stderr=%q", stderr)
	}
	if !strings.Contains(stdout, "gitlab:sloppy-org/slopshell#12") {
		t.Fatalf("stdout missing GitLab source ref: %q", stdout)
	}
}

func initGitRepoDir(dir, remote string) error {
	run := func(args ...string) error {
		c := exec.Command("git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			return fmt.Errorf("git %v: %v\n%s", args, err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	if err := run("init"); err != nil {
		return err
	}
	return run("remote", "add", "origin", remote)
}

func writeScript(t *testing.T, dir, name, script string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write script %s: %v", name, err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatalf("chmod script %s: %v", name, err)
	}
}
