package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestForbiddenTokensLintCatchesSyntheticHomePath(t *testing.T) {
	syntheticPath := filepath.Join("/", "home", "example", "Nextcloud", "secret.pdf")
	repo := initForbiddenTokensRepo(t, map[string]string{
		"cmd/example/main.go": "package main\n\n// " + syntheticPath + "\n",
	})

	script := findForbiddenTokensScript(t)
	out, err := runForbiddenTokensLint(t, repo, script, nil)
	if err == nil {
		t.Fatalf("expected lint to fail on synthetic forbidden token, but it passed\noutput: %s", out)
	}
	if !strings.Contains(out, "FORBIDDEN") {
		t.Fatalf("expected FORBIDDEN in output, got:\n%s", out)
	}
	if !strings.Contains(out, syntheticPath) {
		t.Fatalf("expected synthetic path in output, got:\n%s", out)
	}
}

func TestForbiddenTokensLintAcceptsEnvExtensions(t *testing.T) {
	repo := initForbiddenTokensRepo(t, map[string]string{
		"cmd/example/main.go": "package main\n\n// CUSTOM_FORBIDDEN_TOKEN_12345\n",
	})

	script := findForbiddenTokensScript(t)
	out, err := runForbiddenTokensLint(t, repo, script, []string{
		"SLOPTOOLS_FORBIDDEN_TOKENS=CUSTOM_FORBIDDEN_TOKEN_12345",
	})
	if err == nil {
		t.Fatalf("expected lint to fail on custom forbidden token from env, but it passed\noutput: %s", out)
	}
	if !strings.Contains(out, "CUSTOM_FORBIDDEN_TOKEN_12345") {
		t.Fatalf("expected custom token in output, got:\n%s", out)
	}
}

func findForbiddenTokensScript(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return filepath.Join(dir, "scripts", "check-forbidden-tokens.sh")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find git repo root from test")
		}
		dir = parent
	}
}

func initForbiddenTokensRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	runGitCommand(t, repo, "init", "--initial-branch=main")
	runGitCommand(t, repo, "config", "user.email", "test@test.com")
	runGitCommand(t, repo, "config", "user.name", "Test")
	for path, content := range files {
		full := filepath.Join(repo, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
		runGitCommand(t, repo, "add", path)
	}
	runGitCommand(t, repo, "commit", "-m", "fixture")
	return repo
}

func runForbiddenTokensLint(t *testing.T, repo, script string, extraEnv []string) (string, error) {
	t.Helper()
	cmd := exec.Command("bash", script)
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), extraEnv...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func runGitCommand(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
