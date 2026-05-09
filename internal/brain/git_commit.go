package brain

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// WithGitCommit runs fn only when the brain repo is clean, then commits and
// pushes the changes fn made. Existing local commits are pushed first so a
// later no-op command still heals a previous push failure.
func WithGitCommit(cfg *Config, sphere Sphere, message string, fn func() error) error {
	vault, err := gitVault(cfg, sphere)
	if err != nil {
		return err
	}
	root := vault.BrainRoot()
	if err := ensureGitReady(root); err != nil {
		return err
	}
	if err := fn(); err != nil {
		_ = rollbackBrainGit(root)
		return err
	}
	return CommitBrainGit(root, message)
}

// PrepareBrainGit refuses to start a mutation when the brain repo is dirty,
// lacks an upstream, or has unpushed commits that cannot be pushed.
func PrepareBrainGit(cfg *Config, sphere Sphere) error {
	vault, err := gitVault(cfg, sphere)
	if err != nil {
		return err
	}
	return ensureGitReady(vault.BrainRoot())
}

// CommitBrainGit stages, commits, and pushes changes under brainRoot. If no
// files changed, it still pushes existing ahead commits.
func CommitBrainGit(brainRoot, message string) error {
	if err := ensureGitRepo(brainRoot); err != nil {
		return err
	}
	if err := ensureGitUpstream(brainRoot); err != nil {
		return err
	}
	if err := runBrainGit(brainRoot, "add", "-A"); err != nil {
		_ = rollbackBrainGit(brainRoot)
		return fmt.Errorf("brain auto-commit: git add failed: %w", err)
	}
	if brainRepoHasStagedChanges(brainRoot) {
		if strings.TrimSpace(message) == "" {
			_ = rollbackBrainGit(brainRoot)
			return fmt.Errorf("brain auto-commit: commit message is required")
		}
		if err := runBrainGit(brainRoot, "commit", "-m", message); err != nil {
			_ = rollbackBrainGit(brainRoot)
			return fmt.Errorf("brain auto-commit: git commit failed: %w", err)
		}
	}
	if err := runBrainGit(brainRoot, "push"); err != nil {
		return fmt.Errorf("brain auto-commit: git push failed: %w", err)
	}
	return nil
}

func gitVault(cfg *Config, sphere Sphere) (Vault, error) {
	if cfg == nil {
		return Vault{}, fmt.Errorf("brain auto-commit: nil config")
	}
	vault, ok := cfg.Vault(sphere)
	if !ok {
		return Vault{}, fmt.Errorf("brain auto-commit: unknown vault %q", sphere)
	}
	return vault, nil
}

func ensureGitReady(brainRoot string) error {
	if err := ensureGitRepo(brainRoot); err != nil {
		return err
	}
	if err := ensureGitUpstream(brainRoot); err != nil {
		return err
	}
	if err := commitPreexistingDirtyGit(brainRoot); err != nil {
		return err
	}
	if err := runBrainGit(brainRoot, "push"); err != nil {
		return fmt.Errorf("brain auto-commit: git push failed: %w", err)
	}
	return nil
}

func ensureGitRepo(brainRoot string) error {
	out, err := brainGitOutput(brainRoot, "rev-parse", "--show-toplevel")
	if err != nil {
		return fmt.Errorf("brain auto-commit: %s is not a git repository", brainRoot)
	}
	root := filepath.Clean(strings.TrimSpace(out))
	want := filepath.Clean(brainRoot)
	if !samePath(root, want) {
		return fmt.Errorf("brain auto-commit: git root %s does not match brain root %s", root, want)
	}
	return nil
}

func ensureGitUpstream(brainRoot string) error {
	out, err := brainGitOutput(brainRoot, "rev-parse", "--abbrev-ref", "@{upstream}")
	if err != nil || strings.TrimSpace(out) == "" {
		return fmt.Errorf("brain auto-commit: git upstream is not configured")
	}
	return nil
}

func commitPreexistingDirtyGit(brainRoot string) error {
	out, err := brainGitOutput(brainRoot, "status", "--porcelain")
	if err != nil {
		return fmt.Errorf("brain auto-commit: git status failed: %w", err)
	}
	if strings.TrimSpace(out) == "" {
		return nil
	}
	if err := CommitBrainGit(brainRoot, "brain manual edits: preflight"); err != nil {
		return fmt.Errorf("brain auto-commit: preflight commit failed: %w", err)
	}
	return nil
}

func brainRepoHasStagedChanges(brainRoot string) bool {
	err := runBrainGit(brainRoot, "diff", "--cached", "--quiet")
	if err == nil {
		return false
	}
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr) && exitErr.ExitCode() == 1
}

func brainGitOutput(brainRoot string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeoutPerInvocation)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", brainRoot}, args...)...)
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
	return string(out), nil
}

func runBrainGit(brainRoot string, args ...string) error {
	_, err := brainGitOutput(brainRoot, args...)
	return err
}

func rollbackBrainGit(brainRoot string) error {
	if err := runBrainGit(brainRoot, "reset", "--hard", "HEAD"); err != nil {
		return err
	}
	return runBrainGit(brainRoot, "clean", "-fd")
}

// RollbackBrainGit discards uncommitted changes in a brain repo. Callers use
// it only after PrepareBrainGit proved the repo was clean before a mutation.
func RollbackBrainGit(brainRoot string) error {
	return rollbackBrainGit(brainRoot)
}
