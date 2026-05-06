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

func TestRunSleepIncludesGitContextFromPreviousSleepCommit(t *testing.T) {
	cfg, _ := newSleepVault(t)
	vault, ok := cfg.Vault(SphereWork)
	if !ok {
		t.Fatalf("work vault missing")
	}
	brainRoot := vault.BrainRoot()
	gitInit(t, brainRoot, time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC))
	gitAddCommit(t, brainRoot, "seed topics", time.Date(2026, 5, 1, 10, 5, 0, 0, time.UTC))
	reportDir := filepath.Join(brainRoot, SleepReportSubdir)
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		t.Fatalf("mkdir report dir: %v", err)
	}
	previousReport := filepath.Join(reportDir, "2026-05-05.md")
	if err := os.WriteFile(previousReport, []byte("# Previous sleep\n\nRemember KINEQ.\n"), 0o644); err != nil {
		t.Fatalf("write previous report: %v", err)
	}
	gitAddCommit(t, brainRoot, "brain sleep: work 2026-05-05", time.Date(2026, 5, 5, 23, 0, 0, 0, time.UTC))
	topic := filepath.Join(brainRoot, "topics", "regular-0.md")
	f, err := os.OpenFile(topic, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open topic: %v", err)
	}
	if _, err := f.WriteString("\nFresh note.\n"); err != nil {
		t.Fatalf("append topic: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close topic: %v", err)
	}
	gitAddCommit(t, brainRoot, "brain gtd update: topics/regular-0.md", time.Date(2026, 5, 6, 9, 0, 0, 0, time.UTC))

	res, err := RunSleep(cfg, SleepOpts{
		Sphere:  SphereWork,
		Budget:  4,
		Backend: SleepBackendNone,
		Now:     time.Date(2026, 5, 6, 23, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("RunSleep: %v", err)
	}
	if !res.GitContextUsed {
		t.Fatalf("GitContextUsed=false, want true")
	}
	data, err := os.ReadFile(res.ReportPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	body := string(data)
	for _, want := range []string{
		"## Recent git context",
		"Subject: brain sleep: work 2026-05-05",
		"Remember KINEQ.",
		"Subject: brain gtd update: topics/regular-0.md",
		"Fresh note.",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("sleep report missing %q:\n%s", want, body)
		}
	}
}

func TestRunSleepUsesDistinctReportPathForSecondSleepOnSameDay(t *testing.T) {
	cfg, root := newSleepVault(t)
	now := time.Date(2026, 5, 6, 23, 30, 0, 0, time.UTC)
	first, err := RunSleep(cfg, SleepOpts{
		Sphere:  SphereWork,
		Budget:  4,
		Backend: SleepBackendNone,
		Now:     now,
	})
	if err != nil {
		t.Fatalf("first RunSleep: %v", err)
	}
	second, err := RunSleep(cfg, SleepOpts{
		Sphere:  SphereWork,
		Budget:  4,
		Backend: SleepBackendNone,
		Now:     now,
	})
	if err != nil {
		t.Fatalf("second RunSleep: %v", err)
	}
	if first.ReportPath == second.ReportPath {
		t.Fatalf("ReportPath reused: %s", first.ReportPath)
	}
	wantSecond := filepath.Join(root, "brain", SleepReportSubdir, "2026-05-06-233000.md")
	if second.ReportPath != wantSecond {
		t.Fatalf("second ReportPath=%q, want %q", second.ReportPath, wantSecond)
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
