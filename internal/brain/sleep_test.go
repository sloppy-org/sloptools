package brain

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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
	if !strings.Contains(bodyStr, "# Brain sleep run — work — 2026-05-06") {
		t.Fatalf("report header missing; got: %q", bodyStr[:min(200, len(bodyStr))])
	}
	if !strings.Contains(bodyStr, "## REM Picked Topics (4)") {
		t.Fatalf("REM picked topics section missing; got: %q", bodyStr)
	}
	if !strings.Contains(bodyStr, "## Synaptic Maintenance") {
		t.Fatalf("synaptic maintenance section missing")
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
		"### Git Context",
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
	var capturedReq SleepCodexRequest
	var capturedPacket string
	exec := func(ctx context.Context, req SleepCodexRequest) ([]byte, error) {
		capturedReq = req
		capturedPacket = req.Packet
		return []byte(stamp + "\n" + req.Packet), nil
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
	if capturedReq.Model != "gpt-5.5" {
		t.Fatalf("captured model=%q, want gpt-5.5", capturedReq.Model)
	}
	if capturedReq.VaultRoot != filepath.Join(root, "brain") {
		t.Fatalf("captured vault=%q, want %q", capturedReq.VaultRoot, filepath.Join(root, "brain"))
	}
	if capturedReq.Autonomy != SleepAutonomyFull {
		t.Fatalf("captured autonomy=%q, want %q", capturedReq.Autonomy, SleepAutonomyFull)
	}
	for _, want := range []string{
		"Autonomy: full",
		"## Folder Coverage Prepass",
		"## NREM Recent-Prioritized Consolidation Candidates",
		"## REM Picked Topics",
		"Run an autonomous brain sleep cycle",
	} {
		if !strings.Contains(capturedPacket, want) {
			t.Fatalf("captured packet missing %q; got: %q", want, capturedPacket[:min(400, len(capturedPacket))])
		}
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
	exec := func(ctx context.Context, req SleepCodexRequest) ([]byte, error) {
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

func TestCodexExecArgsUseAutonomousSandbox(t *testing.T) {
	args := codexExecArgs(SleepCodexRequest{
		Model:     "gpt-5.5",
		VaultRoot: "/tmp/brain",
		Autonomy:  SleepAutonomyFull,
	}, "/tmp/out.md")
	for _, want := range []string{
		"--ask-for-approval",
		"never",
		"workspace-write",
		"/tmp/brain",
		"/tmp/out.md",
	} {
		if !slices.Contains(args, want) {
			t.Fatalf("codex args missing %q: %#v", want, args)
		}
	}

	planArgs := codexExecArgs(SleepCodexRequest{Autonomy: SleepAutonomyPlanOnly}, "/tmp/out.md")
	if !slices.Contains(planArgs, "read-only") {
		t.Fatalf("plan-only args missing read-only sandbox: %#v", planArgs)
	}
}

func TestPrioritizeSleepNREMKeepsRecentMemoryInsideBudget(t *testing.T) {
	rows := []ConsolidateRow{
		{Outcome: OutcomeRetire, Path: "brain/topics/old.md", Score: 500},
		{Outcome: OutcomeConsolidate, Path: "brain/topics/recent.md", Score: 1},
		{Outcome: OutcomeArchive, Path: "brain/topics/archive.md", Score: 300},
	}
	got := prioritizeSleepNREM(rows, []string{"topics/recent.md"}, 1)
	if len(got) != 1 {
		t.Fatalf("got %d rows, want 1", len(got))
	}
	if got[0].Path != "brain/topics/recent.md" {
		t.Fatalf("picked %q, want recent path", got[0].Path)
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

func TestRunSleepRejectsUnknownAutonomy(t *testing.T) {
	cfg, _ := newSleepVault(t)
	_, err := RunSleep(cfg, SleepOpts{
		Sphere:   SphereWork,
		Backend:  SleepBackendNone,
		Autonomy: "committee",
	})
	if err == nil || !strings.Contains(err.Error(), "unknown sleep autonomy") {
		t.Fatalf("expected unknown-autonomy error, got: %v", err)
	}
}

func TestRunSleepPacketIncludesNREMAndRecentMemory(t *testing.T) {
	cfg, root := newSleepVault(t)
	vault, ok := cfg.Vault(SphereWork)
	if !ok {
		t.Fatalf("work vault missing")
	}
	brainRoot := vault.BrainRoot()
	now := time.Date(2026, 5, 6, 23, 30, 0, 0, time.UTC)

	writeDreamRaw(t, root, "brain/people/alex.md", `---
kind: human
display_name: Alex Doe
aliases:
  - Alex Doe
  - A. Doe
status: archived
---
# Alex
`)
	writeDreamRaw(t, root, "brain/people/alex-copy.md", `---
kind: human
display_name: Alex Doe
aliases:
  - Alex Doe
  - A. Doe
status: archived
---
# Alex Copy
`)
	gitInit(t, brainRoot, time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC))
	gitAddCommit(t, brainRoot, "seed duplicate people", time.Date(2026, 5, 1, 10, 5, 0, 0, time.UTC))
	reportDir := filepath.Join(brainRoot, SleepReportSubdir)
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		t.Fatalf("mkdir report dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(reportDir, "2026-05-05.md"), []byte("previous sleep\n"), 0o644); err != nil {
		t.Fatalf("write previous sleep: %v", err)
	}
	gitAddCommit(t, brainRoot, "brain sleep: work 2026-05-05", time.Date(2026, 5, 5, 23, 0, 0, 0, time.UTC))
	topicPath := filepath.Join(brainRoot, "topics", "regular-1.md")
	f, err := os.OpenFile(topicPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open topic: %v", err)
	}
	if _, err := f.WriteString("\nRecent memory payload.\n"); err != nil {
		t.Fatalf("append topic: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close topic: %v", err)
	}
	gitAddCommit(t, brainRoot, "record recent topic memory", time.Date(2026, 5, 6, 9, 0, 0, 0, time.UTC))

	var captured string
	exec := func(ctx context.Context, req SleepCodexRequest) ([]byte, error) {
		captured = req.Packet
		return []byte("sleep done\n"), nil
	}
	res, err := RunSleep(cfg, SleepOpts{
		Sphere:     SphereWork,
		Budget:     4,
		NREMBudget: 10,
		Backend:    SleepBackendCodex,
		Now:        now,
		CodexExec:  exec,
	})
	if err != nil {
		t.Fatalf("RunSleep: %v", err)
	}
	if res.NREMCount == 0 {
		t.Fatalf("NREMCount=0, want duplicate consolidation rows")
	}
	if res.RecentCount == 0 {
		t.Fatalf("RecentCount=0, want recent changed paths")
	}
	for _, want := range []string{
		"brain/people/alex-copy.md",
		"consolidate",
		"topics/regular-1.md",
		"Recent memory payload.",
	} {
		if !strings.Contains(captured, want) {
			t.Fatalf("sleep packet missing %q:\n%s", want, captured)
		}
	}
}

func TestRunSleepCodexCanMutateBrainRoot(t *testing.T) {
	cfg, root := newSleepVault(t)
	brainRoot := filepath.Join(root, "brain")
	target := filepath.Join(brainRoot, "topics", "regular-0.md")
	exec := func(ctx context.Context, req SleepCodexRequest) ([]byte, error) {
		if req.VaultRoot != brainRoot {
			t.Fatalf("VaultRoot=%q, want %q", req.VaultRoot, brainRoot)
		}
		f, err := os.OpenFile(target, os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, err
		}
		if _, err := f.WriteString("\nAutonomous sleep rewrite.\n"); err != nil {
			_ = f.Close()
			return nil, err
		}
		if err := f.Close(); err != nil {
			return nil, err
		}
		return []byte("rewired regular-0\n"), nil
	}
	_, err := RunSleep(cfg, SleepOpts{
		Sphere:    SphereWork,
		Budget:    4,
		Backend:   SleepBackendCodex,
		CodexExec: exec,
	})
	if err != nil {
		t.Fatalf("RunSleep: %v", err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if !strings.Contains(string(data), "Autonomous sleep rewrite.") {
		t.Fatalf("codex mutation missing from target:\n%s", string(data))
	}
}

func TestRunSleepDefaultsBudgetAndModel(t *testing.T) {
	cfg, _ := newSleepVault(t)
	exec := func(ctx context.Context, req SleepCodexRequest) ([]byte, error) {
		if req.Model != SleepDefaultModel {
			t.Fatalf("default model=%q, want %q", req.Model, SleepDefaultModel)
		}
		if req.Autonomy != SleepDefaultAutonomy {
			t.Fatalf("default autonomy=%q, want %q", req.Autonomy, SleepDefaultAutonomy)
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
	if res.Autonomy != SleepDefaultAutonomy {
		t.Fatalf("result Autonomy=%q, want %q", res.Autonomy, SleepDefaultAutonomy)
	}
}
