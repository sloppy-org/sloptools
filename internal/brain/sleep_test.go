package brain

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newSleepVault(t *testing.T) (*Config, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "topics"), 0o755); err != nil {
		t.Fatalf("mkdir topics: %v", err)
	}
	for i := 0; i < 4; i++ {
		writeDreamTopic(t, root, fmt.Sprintf("topics/strategic-%d.md", i),
			fmt.Sprintf("Strategic %d", i), true, "weekly")
	}
	for i := 0; i < 4; i++ {
		writeDreamTopic(t, root, fmt.Sprintf("topics/regular-%d.md", i),
			fmt.Sprintf("Regular %d", i), false, "")
	}
	cfg, err := NewConfig([]Vault{{Sphere: SphereWork, Root: root}})
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	return cfg, root
}

func TestRunSleepDryRunWritesNothing(t *testing.T) {
	cfg, root := newSleepVault(t)
	now := time.Date(2026, 5, 6, 23, 30, 0, 0, time.UTC)
	res, err := RunSleep(cfg, SleepOpts{
		Sphere:  SphereWork,
		Budget:  4,
		Backend: SleepBackendNone,
		DryRun:  true,
		Now:     now,
	})
	if err != nil {
		t.Fatalf("RunSleep: %v", err)
	}
	if !res.DryRun {
		t.Fatalf("DryRun=false, want true")
	}
	if res.ReportPath != "" {
		t.Fatalf("ReportPath=%q in dry-run, want empty", res.ReportPath)
	}
	if res.PruneApplied {
		t.Fatalf("PruneApplied=true in dry-run, want false")
	}
	if res.CodexUsed {
		t.Fatalf("CodexUsed=true with backend=none, want false")
	}
	if _, err := os.Stat(filepath.Join(root, "brain", SleepReportSubdir, "2026-05-06.md")); err == nil {
		t.Fatalf("report file written in dry-run")
	}
	if res.Date != "2026-05-06" {
		t.Fatalf("Date=%q, want 2026-05-06", res.Date)
	}
	if len(res.Report.Topics) != 4 {
		t.Fatalf("topics=%d, want 4", len(res.Report.Topics))
	}
}

func TestRunSleepWritesReportWithoutCodex(t *testing.T) {
	cfg, root := newSleepVault(t)
	now := time.Date(2026, 5, 6, 23, 30, 0, 0, time.UTC)
	res, err := RunSleep(cfg, SleepOpts{
		Sphere:  SphereWork,
		Budget:  4,
		Backend: SleepBackendNone,
		Now:     now,
	})
	if err != nil {
		t.Fatalf("RunSleep: %v", err)
	}
	if res.CodexUsed {
		t.Fatalf("CodexUsed=true with backend=none, want false")
	}
	want := filepath.Join(root, "brain", SleepReportSubdir, "2026-05-06.md")
	if res.ReportPath != want {
		t.Fatalf("ReportPath=%q, want %q", res.ReportPath, want)
	}
	body, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	bodyStr := string(body)
	if !strings.Contains(bodyStr, "# Brain sleep report — work — 2026-05-06") {
		t.Fatalf("report header missing; got: %q", bodyStr[:min(200, len(bodyStr))])
	}
	if !strings.Contains(bodyStr, "## Picked topics (4)") {
		t.Fatalf("Picked topics section missing; got: %q", bodyStr)
	}
	if !strings.Contains(bodyStr, "## Cold-link prune scan") {
		t.Fatalf("prune scan section missing")
	}
}

func TestRunSleepCodexBackendInvokesRunner(t *testing.T) {
	cfg, root := newSleepVault(t)
	now := time.Date(2026, 5, 6, 23, 30, 0, 0, time.UTC)
	const stamp = "<!-- judged-by-codex -->"
	var capturedModel, capturedVault string
	var capturedPacket string
	exec := func(ctx context.Context, model, vaultRoot, packet string) ([]byte, error) {
		capturedModel = model
		capturedVault = vaultRoot
		capturedPacket = packet
		return []byte(stamp + "\n" + packet), nil
	}
	res, err := RunSleep(cfg, SleepOpts{
		Sphere:    SphereWork,
		Budget:    4,
		Backend:   SleepBackendCodex,
		Model:     "gpt-5.5",
		Now:       now,
		CodexExec: exec,
	})
	if err != nil {
		t.Fatalf("RunSleep: %v", err)
	}
	if !res.CodexUsed {
		t.Fatalf("CodexUsed=false, want true")
	}
	if res.Model != "gpt-5.5" {
		t.Fatalf("Model=%q, want gpt-5.5", res.Model)
	}
	if capturedModel != "gpt-5.5" {
		t.Fatalf("captured model=%q, want gpt-5.5", capturedModel)
	}
	if capturedVault != root {
		t.Fatalf("captured vault=%q, want %q", capturedVault, root)
	}
	if !strings.Contains(capturedPacket, "## Picked topics") {
		t.Fatalf("captured packet missing Picked topics; got: %q", capturedPacket[:min(200, len(capturedPacket))])
	}
	body, err := os.ReadFile(res.ReportPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	if !strings.HasPrefix(string(body), stamp) {
		t.Fatalf("report does not start with codex stamp; got: %q", string(body)[:min(200, len(body))])
	}
}

func TestRunSleepCodexErrorPropagates(t *testing.T) {
	cfg, _ := newSleepVault(t)
	exec := func(ctx context.Context, model, vaultRoot, packet string) ([]byte, error) {
		return nil, errors.New("codex unreachable")
	}
	_, err := RunSleep(cfg, SleepOpts{
		Sphere:    SphereWork,
		Budget:    4,
		Backend:   SleepBackendCodex,
		Model:     "gpt-5.5",
		CodexExec: exec,
	})
	if err == nil {
		t.Fatalf("expected error from codex exec")
	}
	if !strings.Contains(err.Error(), "codex unreachable") {
		t.Fatalf("error does not wrap codex failure; got: %v", err)
	}
}

func TestRunSleepRejectsUnknownBackend(t *testing.T) {
	cfg, _ := newSleepVault(t)
	_, err := RunSleep(cfg, SleepOpts{
		Sphere:  SphereWork,
		Backend: "ouija",
	})
	if err == nil || !strings.Contains(err.Error(), "unknown sleep backend") {
		t.Fatalf("expected unknown-backend error, got: %v", err)
	}
}

func TestRunSleepDefaultsBudgetAndModel(t *testing.T) {
	cfg, _ := newSleepVault(t)
	exec := func(ctx context.Context, model, vaultRoot, packet string) ([]byte, error) {
		if model != SleepDefaultModel {
			t.Fatalf("default model=%q, want %q", model, SleepDefaultModel)
		}
		return []byte("ok\n"), nil
	}
	res, err := RunSleep(cfg, SleepOpts{
		Sphere:    SphereWork,
		Backend:   SleepBackendCodex,
		CodexExec: exec,
	})
	if err != nil {
		t.Fatalf("RunSleep: %v", err)
	}
	if res.Model != SleepDefaultModel {
		t.Fatalf("result Model=%q, want %q", res.Model, SleepDefaultModel)
	}
}

