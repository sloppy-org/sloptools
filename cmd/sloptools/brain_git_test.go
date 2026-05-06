package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func initBrainCLIGit(t *testing.T, workTree, remote string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	if err := os.MkdirAll(workTree, 0o755); err != nil {
		t.Fatalf("mkdir git worktree: %v", err)
	}
	runBrainCLIGit(t, "", "init", "--bare", remote)
	runBrainCLIGit(t, "", "init", "-q", "-b", "main", workTree)
	runBrainCLIGit(t, workTree, "config", "user.email", "test@example.invalid")
	runBrainCLIGit(t, workTree, "config", "user.name", "sloptools test")
	runBrainCLIGit(t, workTree, "commit", "-q", "--allow-empty", "-m", "init")
	runBrainCLIGit(t, workTree, "remote", "add", "origin", remote)
	runBrainCLIGit(t, workTree, "push", "-q", "-u", "origin", "main")
}

func commitBrainCLIFileIfTracked(t *testing.T, path string) {
	t.Helper()
	dir := filepath.Dir(path)
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return
	}
	runBrainCLIGit(t, root, "add", path)
	status := gitOutputForBrainCLITest(t, root, "diff", "--cached", "--name-only")
	if strings.TrimSpace(status) == "" {
		return
	}
	runBrainCLIGit(t, root, "commit", "-q", "-m", "test fixture")
	runBrainCLIGit(t, root, "push", "-q")
}

func runBrainCLIGit(t *testing.T, workTree string, args ...string) {
	t.Helper()
	cmdArgs := args
	if workTree != "" {
		cmdArgs = append([]string{"-C", workTree}, args...)
	}
	cmd := exec.Command("git", cmdArgs...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", cmdArgs, err, strings.TrimSpace(string(out)))
	}
}

func gitOutputForBrainCLITest(t *testing.T, workTree string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", workTree}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, strings.TrimSpace(string(out)))
	}
	return string(out)
}
