package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBrainSleepCLICommitsAndPushesReport(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeBrainCLIConfig(t, tmp)
	stdout, stderr, code := captureRun(t, []string{
		"brain", "sleep",
		"--config", configPath,
		"--sphere", "work",
		"--backend", "none",
	})
	if code != 0 {
		t.Fatalf("sleep exit code = %d, stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, `"report_path"`) {
		t.Fatalf("sleep output missing report_path: %s", stdout)
	}
	workTree := filepath.Join(tmp, "work", "brain")
	subject := gitOutputForTest(t, workTree, "log", "-1", "--format=%s")
	if !strings.HasPrefix(subject, "brain sleep: work ") {
		t.Fatalf("sleep commit subject = %q", subject)
	}
	localHead := gitOutputForTest(t, workTree, "rev-parse", "HEAD")
	remoteHead := gitOutputForTest(t, filepath.Join(tmp, "work-brain.git"), "rev-parse", "main")
	if localHead != remoteHead {
		t.Fatalf("remote head %s != local head %s", remoteHead, localHead)
	}
}

func gitOutputForTest(t *testing.T, workTree string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", workTree}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out))
}
