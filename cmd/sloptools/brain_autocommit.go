package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sloppy-org/sloptools/internal/brain"
)

// brainAutoCommit stages every change under the brain root, creates a commit
// with the supplied message, and pushes to origin. Push failures emit a
// stderr warning but never abort the apply: the file system change has
// already succeeded and the commit is the authoritative audit trail; push
// is best-effort sync to giga. If the brain root has no .git directory the
// helper is a silent no-op (the vault may legitimately not be tracked yet).
func brainAutoCommit(cfg *brain.Config, sphere brain.Sphere, message string) {
	if cfg == nil {
		return
	}
	vault, ok := cfg.Vault(sphere)
	if !ok {
		return
	}
	root := vault.BrainRoot()
	if !brainRepoExists(root) {
		return
	}
	if err := runGit(root, "add", "-A"); err != nil {
		fmt.Fprintf(os.Stderr, "brain auto-commit: git add failed: %v\n", err)
		return
	}
	if !brainRepoHasStagedChanges(root) {
		return
	}
	if err := runGit(root, "commit", "-m", message); err != nil {
		fmt.Fprintf(os.Stderr, "brain auto-commit: git commit failed: %v\n", err)
		return
	}
	if !brainRepoHasUpstream(root) {
		return
	}
	if err := runGit(root, "push"); err != nil {
		fmt.Fprintf(os.Stderr, "brain auto-commit: git push failed (commit landed locally): %v\n", err)
	}
}

func brainRepoExists(brainRoot string) bool {
	info, err := os.Stat(filepath.Join(brainRoot, ".git"))
	return err == nil && info.IsDir()
}

func brainRepoHasStagedChanges(brainRoot string) bool {
	cmd := exec.Command("git", "-C", brainRoot, "diff", "--cached", "--quiet")
	err := cmd.Run()
	if err == nil {
		return false
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return true
	}
	return false
}

func brainRepoHasUpstream(brainRoot string) bool {
	cmd := exec.Command("git", "-C", brainRoot, "rev-parse", "--abbrev-ref", "@{upstream}")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) != ""
}

func runGit(brainRoot string, args ...string) error {
	cmd := exec.Command("git", append([]string{"-C", brainRoot}, args...)...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return fmt.Errorf("%w: %s", err, msg)
		}
		return err
	}
	return nil
}
