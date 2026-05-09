package audit

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
	body := "# Report — X\n\n## Verified\n- a\n"
	rawPath, cleanedPath, err := WriteStageArtifact(rpath, "bulk", body, body)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if rawPath != "" {
		t.Fatalf("rawPath should be empty when raw==cleaned, got %q", rawPath)
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
	raw := "I'll now produce the report.\n\n# Report — X\n- a\n"
	cleaned := "# Report — X\n- a\n"
	rawPath, cleanedPath, err := WriteStageArtifact(rpath, "bulk", raw, cleaned)
	if err != nil {
		t.Fatalf("write: %v", err)
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
	for _, suffix := range []string{"bulk", "resolve.1", "escalate.codex", "escalate.claude"} {
		_, p, err := WriteStageArtifact(rpath, suffix, "body", "body")
		if err != nil {
			t.Fatalf("suffix %q: %v", suffix, err)
		}
		want := "x." + suffix + ".md"
		if filepath.Base(p) != want {
			t.Fatalf("suffix %q produced %q want %q", suffix, filepath.Base(p), want)
		}
	}
}

func TestWriteFile_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	rpath := filepath.Join(dir, "x.md")
	now := time.Date(2026, 5, 8, 22, 0, 0, 0, time.UTC)
	in := File{
		Path:       "brain/folders/Test.md",
		Title:      "Test",
		ReportPath: rpath,
		RunID:      "20260508-220000",
		Sphere:     "private",
		StartedAt:  now,
		EndedAt:    now.Add(2 * time.Minute),
		FinalStage: "sleep-judge-escalate",
		Escalated:  true,
		Stages: []StageRecord{
			{
				Stage:        "sleep-judge-bulk",
				Backend:      "opencode",
				Provider:     "local",
				Model:        "llamacpp/qwen27b",
				Tier:         "bulk",
				StartedAt:    now,
				WallMS:       300_000,
				TokensIn:     800,
				TokensOut:    1500,
				RawBytes:     2400,
				CleanedBytes: 2200,
				ReasonAfter:  "trigram repetition 4500 exceeds 30",
			},
			{
				Stage:         "sleep-judge-escalate",
				Backend:       "codex",
				Provider:      "openai",
				Model:         "gpt-5.4-mini",
				Tier:          "medium",
				StartedAt:     now.Add(time.Minute),
				WallMS:        90_000,
				TokensIn:      80,
				TokensOut:     4500,
				CostHint:      0.12,
				TriggerReason: "trigram repetition 4500 exceeds 30",
			},
		},
	}
	if err := WriteFile(rpath, in); err != nil {
		t.Fatalf("write audit: %v", err)
	}
	auditPath := strings.TrimSuffix(rpath, ".md") + ".audit.json"
	buf, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	var got File
	if err := json.Unmarshal(buf, &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, buf)
	}
	if got.RunID != in.RunID || got.FinalStage != in.FinalStage {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}
	if !got.Escalated || len(got.Stages) != 2 {
		t.Fatalf("expected 2 stages and Escalated=true, got %+v", got)
	}
	if got.Stages[0].ReasonAfter != "trigram repetition 4500 exceeds 30" {
		t.Fatalf("ReasonAfter not preserved: %q", got.Stages[0].ReasonAfter)
	}
	if got.Stages[1].TriggerReason != "trigram repetition 4500 exceeds 30" {
		t.Fatalf("TriggerReason not preserved: %q", got.Stages[1].TriggerReason)
	}
}

func TestWriteStageArtifact_ErrorsOnEmptyArgs(t *testing.T) {
	dir := t.TempDir()
	rpath := filepath.Join(dir, "x.md")
	if _, _, err := WriteStageArtifact("", "bulk", "a", "a"); err == nil {
		t.Fatalf("expected error on empty reportPath")
	}
	if _, _, err := WriteStageArtifact(rpath, "", "a", "a"); err == nil {
		t.Fatalf("expected error on empty suffix")
	}
}
