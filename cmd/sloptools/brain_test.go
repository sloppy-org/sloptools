package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestBrainSearchCLIEmitsStructuredResults(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeBrainCLIConfig(t, tmp)
	writeCLIFile(t, filepath.Join(tmp, "work", "brain", "projects", "alpha.md"), "needle\n")

	stdout, stderr, code := captureRun(t, []string{
		"brain", "search",
		"--config", configPath,
		"--sphere", "work",
		"--query", "needle",
	})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr=%q", code, stderr)
	}
	var got map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("decode stdout: %v\n%s", err, stdout)
	}
	if int(got["count"].(float64)) != 1 {
		t.Fatalf("count = %v, stdout=%s", got["count"], stdout)
	}
}

func TestBrainBacklinksCLIRejectsPersonalTarget(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeBrainCLIConfig(t, tmp)
	secret := filepath.Join(tmp, "work", "personal", "secret.md")
	writeCLIFile(t, secret, "secret\n")

	_, stderr, code := captureRun(t, []string{
		"brain", "backlinks",
		"--config", configPath,
		"--sphere", "work",
		"--target", secret,
	})
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if stderr == "" {
		t.Fatalf("expected rejection on stderr")
	}
}

func writeBrainCLIConfig(t *testing.T, root string) string {
	t.Helper()
	path := filepath.Join(root, "vaults.toml")
	body := `[[vault]]
sphere = "work"
root = "` + filepath.ToSlash(filepath.Join(root, "work")) + `"
brain = "brain"

[[vault]]
sphere = "private"
root = "` + filepath.ToSlash(filepath.Join(root, "private")) + `"
brain = "brain"
`
	writeCLIFile(t, path, body)
	return path
}

func writeCLIFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}
