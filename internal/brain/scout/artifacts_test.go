package scout

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteStageArtifact_CleanedOnlyWhenIdentical(t *testing.T) {
	dir := t.TempDir()
	rpath := filepath.Join(dir, "x.md")
	body := "# Scout report — X\n\n## Verified\n- a\n"
	rawPath, cleanedPath, err := writeStageArtifact(rpath, "bulk", body, body)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if rawPath != "" {
		t.Fatalf("rawPath should be empty when raw==cleaned, got %q", rawPath)
	}
	if cleanedPath == "" {
		t.Fatalf("cleanedPath empty")
	}
	if filepath.Base(cleanedPath) != "x.bulk.md" {
		t.Fatalf("unexpected cleaned filename %q", filepath.Base(cleanedPath))
	}
	if _, err := os.Stat(cleanedPath); err != nil {
		t.Fatalf("cleaned not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "x.bulk.raw.md")); !os.IsNotExist(err) {
		t.Fatalf("raw sidecar should not exist when raw==cleaned")
	}
}

func TestWriteStageArtifact_RawWhenTrimmed(t *testing.T) {
	dir := t.TempDir()
	rpath := filepath.Join(dir, "x.md")
	raw := "I'll now produce the report.\n\n# Scout report — X\n\n## Verified\n- a\n"
	cleaned := "# Scout report — X\n\n## Verified\n- a\n"
	rawPath, cleanedPath, err := writeStageArtifact(rpath, "bulk", raw, cleaned)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if rawPath == "" {
		t.Fatalf("rawPath empty when narration was trimmed")
	}
	if filepath.Base(rawPath) != "x.bulk.raw.md" {
		t.Fatalf("unexpected raw filename %q", filepath.Base(rawPath))
	}
	if filepath.Base(cleanedPath) != "x.bulk.md" {
		t.Fatalf("unexpected cleaned filename %q", filepath.Base(cleanedPath))
	}
	got, err := os.ReadFile(rawPath)
	if err != nil {
		t.Fatalf("read raw: %v", err)
	}
	if string(got) != raw {
		t.Fatalf("raw content mismatch")
	}
}

func TestWriteStageArtifact_SuffixSlugsAreSafe(t *testing.T) {
	dir := t.TempDir()
	rpath := filepath.Join(dir, "x.md")
	for _, suffix := range []string{"bulk", "resolve.1", "resolve.2", "escalate.codex", "escalate.claude"} {
		_, p, err := writeStageArtifact(rpath, suffix, "body", "body")
		if err != nil {
			t.Fatalf("suffix %q: %v", suffix, err)
		}
		want := "x." + suffix + ".md"
		if filepath.Base(p) != want {
			t.Fatalf("suffix %q produced %q want %q", suffix, filepath.Base(p), want)
		}
	}
}

func TestWriteAuditFile_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	rpath := filepath.Join(dir, "x.md")
	now := time.Date(2026, 5, 7, 22, 0, 0, 0, time.UTC)
	audit := auditFile{
		Path:       "brain/folders/Test.md",
		Title:      "Test",
		ReportPath: rpath,
		RunID:      "20260507-220000",
		Sphere:     "work",
		StartedAt:  now,
		EndedAt:    now.Add(2 * time.Minute),
		FinalStage: "scout-escalate-test",
		Escalated:  true,
		Stages: []stageRecord{
			{
				Stage:        "scout-test",
				Backend:      "opencode",
				Provider:     "local",
				Model:        "llamacpp/qwen",
				Tier:         "bulk",
				StartedAt:    now,
				WallMS:       300_000,
				TokensIn:     800,
				TokensOut:    1500,
				RawBytes:     2400,
				CleanedBytes: 2200,
				ReasonAfter:  "explicit needs-paid-review marker",
			},
			{
				Stage:         "scout-escalate-test",
				Backend:       "codex",
				Provider:      "openai",
				Model:         "gpt-5.4-mini",
				Tier:          "medium",
				StartedAt:     now.Add(time.Minute),
				WallMS:        90_000,
				TokensIn:      80,
				TokensOut:     4500,
				CostHint:      0.12,
				TriggerReason: "explicit needs-paid-review marker",
			},
		},
	}
	if err := writeAuditFile(rpath, audit); err != nil {
		t.Fatalf("write audit: %v", err)
	}
	auditPath := strings.TrimSuffix(rpath, ".md") + ".audit.json"
	buf, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	var got auditFile
	if err := json.Unmarshal(buf, &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, buf)
	}
	if got.RunID != audit.RunID || got.FinalStage != audit.FinalStage {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}
	if !got.Escalated || len(got.Stages) != 2 {
		t.Fatalf("expected 2 stages and Escalated=true, got %+v", got)
	}
	if got.Stages[0].ReasonAfter != "explicit needs-paid-review marker" {
		t.Fatalf("ReasonAfter not preserved: %q", got.Stages[0].ReasonAfter)
	}
	if got.Stages[1].TriggerReason != "explicit needs-paid-review marker" {
		t.Fatalf("TriggerReason not preserved: %q", got.Stages[1].TriggerReason)
	}
}

func TestWriteStageArtifact_ErrorsOnEmptyArgs(t *testing.T) {
	dir := t.TempDir()
	rpath := filepath.Join(dir, "x.md")
	if _, _, err := writeStageArtifact("", "bulk", "a", "a"); err == nil {
		t.Fatalf("expected error on empty reportPath")
	}
	if _, _, err := writeStageArtifact(rpath, "", "a", "a"); err == nil {
		t.Fatalf("expected error on empty suffix")
	}
}
