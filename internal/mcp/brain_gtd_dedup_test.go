package mcp

import (
	"path/filepath"
	"testing"

	braingtd "github.com/sloppy-org/sloptools/internal/brain/gtd"
)

func TestBrainGTDDedupScanReconcilesAndQueuesCandidates(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	writeDedupCommitment(t, tmp, "work", "brain/gtd/a.md", "Send alpha budget", "mail", "m1")
	writeDedupCommitment(t, tmp, "work", "brain/gtd/b.md", "Send alpha budget", "todoist", "t1")
	writeDedupCommitment(t, tmp, "work", "brain/gtd/c.md", "Review W7-X plots", "github", "org/repo#1")
	writeDedupCommitment(t, tmp, "work", "brain/gtd/d.md", "Review W7-X plots", "GitHub", "org/repo#1")

	s := NewServer(t.TempDir())
	got, err := s.callTool("brain.gtd.dedup_scan", map[string]interface{}{"config_path": configPath, "sphere": "work"})
	if err != nil {
		t.Fatalf("brain.gtd.dedup_scan: %v", err)
	}
	result := got["dedup"].(braingtd.ScanResult)
	if len(result.Candidates) != 1 {
		t.Fatalf("candidate count = %d, want 1: %#v", len(result.Candidates), result.Candidates)
	}
	if aggregateWithBinding(result.Aggregates, "github:org/repo#1") == nil {
		t.Fatalf("missing reconciled GitHub aggregate: %#v", result.Aggregates)
	}
	if agg := aggregateWithBinding(result.Aggregates, "github:org/repo#1"); len(agg.Bindings) != 1 {
		t.Fatalf("duplicate binding survived: %#v", agg)
	}
}

func TestBrainGTDDedupKeepSeparateDoesNotResurface(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	writeDedupCommitment(t, tmp, "work", "brain/gtd/a.md", "Send alpha budget", "mail", "m1")
	writeDedupCommitment(t, tmp, "work", "brain/gtd/b.md", "send alpha budget", "todoist", "t1")

	s := NewServer(t.TempDir())
	first, err := s.callTool("brain.gtd.dedup_scan", map[string]interface{}{"config_path": configPath, "sphere": "work"})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	id := first["dedup"].(braingtd.ScanResult).Candidates[0].ID
	if _, err := s.callTool("brain.gtd.dedup_review_apply", map[string]interface{}{
		"config_path": configPath, "sphere": "work", "id": id, "decision": "keep_separate",
	}); err != nil {
		t.Fatalf("dedup_review_apply: %v", err)
	}
	second, err := s.callTool("brain.gtd.dedup_scan", map[string]interface{}{"config_path": configPath, "sphere": "work"})
	if err != nil {
		t.Fatalf("rescan: %v", err)
	}
	if got := len(second["dedup"].(braingtd.ScanResult).Candidates); got != 0 {
		t.Fatalf("kept-separate candidate resurfaced, count=%d", got)
	}
	history, err := s.callTool("brain.gtd.dedup_history", map[string]interface{}{"config_path": configPath, "sphere": "work"})
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if history["count"] != 2 {
		t.Fatalf("history count = %v, want 2: %#v", history["count"], history)
	}
}

func TestBrainGTDDedupMergePreservesBindings(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	writeDedupCommitment(t, tmp, "work", "brain/gtd/a.md", "Send alpha budget", "mail", "m1")
	writeDedupCommitment(t, tmp, "work", "brain/gtd/b.md", "send alpha budget", "todoist", "t1")

	s := NewServer(t.TempDir())
	first, err := s.callTool("brain.gtd.dedup_scan", map[string]interface{}{"config_path": configPath, "sphere": "work"})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	id := first["dedup"].(braingtd.ScanResult).Candidates[0].ID
	if _, err := s.callTool("brain.gtd.dedup_review_apply", map[string]interface{}{
		"config_path": configPath, "sphere": "work", "id": id, "decision": "merge",
		"winner_path": "brain/gtd/a.md", "outcome": "Send Alpha budget",
	}); err != nil {
		t.Fatalf("dedup_review_apply merge: %v", err)
	}
	parsed, err := s.callTool("brain.note.parse", map[string]interface{}{"config_path": configPath, "sphere": "work", "path": "brain/gtd/a.md"})
	if err != nil {
		t.Fatalf("parse winner: %v", err)
	}
	winner := parsed["commitment"].(*braingtd.Commitment)
	if len(winner.SourceBindings) != 2 || winner.Outcome != "Send Alpha budget" {
		t.Fatalf("winner = %#v", winner)
	}
}

func TestBrainGTDDedupScanUsesLocalLLMCommand(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	writeDedupCommitment(t, tmp, "work", "brain/gtd/a.md", "Prepare W7-X campaign slide deck", "mail", "m1")
	writeDedupCommitment(t, tmp, "work", "brain/gtd/b.md", "Draft presentation slides for W7-X campaign", "github", "org/repo#2")

	s := NewServer(t.TempDir())
	got, err := s.callTool("brain.gtd.dedup_scan", map[string]interface{}{
		"config_path":   configPath,
		"sphere":        "work",
		"llm_threshold": 0.01,
		"llm_command":   "printf '%s\n' '{\"same\":true,\"confidence\":0.92,\"reasoning\":\"same slide-deck task\"}'",
	})
	if err != nil {
		t.Fatalf("brain.gtd.dedup_scan: %v", err)
	}
	candidates := got["dedup"].(braingtd.ScanResult).Candidates
	if len(candidates) != 1 || candidates[0].Detector != "llm" || candidates[0].Reasoning == "" {
		t.Fatalf("candidates = %#v", candidates)
	}
}

func writeDedupCommitment(t *testing.T, root, sphere, rel, outcome, provider, ref string) {
	t.Helper()
	body := `---
kind: commitment
title: ` + outcome + `
status: next
outcome: ` + outcome + `
source_bindings:
  - provider: ` + provider + `
    ref: "` + ref + `"
---
Body.
`
	writeMCPBrainFile(t, filepath.Join(root, sphere, filepath.FromSlash(rel)), body)
}

func aggregateWithBinding(aggregates []braingtd.Aggregate, id string) *braingtd.Aggregate {
	for i := range aggregates {
		for _, bindingID := range aggregates[i].BindingIDs {
			if bindingID == id {
				return &aggregates[i]
			}
		}
	}
	return nil
}
