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

func TestBrainGTDBindCollapsesCrossSourceCommitments(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	writeDedupCommitment(t, tmp, "work", "brain/gtd/meeting.md", "Send alpha budget", "meetings", "alpha-standup")
	writeDedupCommitment(t, tmp, "work", "brain/gtd/mail.md", "Send alpha budget", "mail", "m1")
	writeDedupCommitment(t, tmp, "work", "brain/gtd/todo.md", "Send alpha budget", "todoist", "t1")
	writeDedupCommitment(t, tmp, "work", "brain/gtd/github.md", "Send alpha budget", "github", "org/repo#7")

	s := NewServer(t.TempDir())
	got, err := s.callTool("brain.gtd.bind", map[string]interface{}{
		"config_path": configPath,
		"sphere":      "work",
		"winner_path": "brain/gtd/meeting.md",
		"paths":       []interface{}{"brain/gtd/meeting.md", "brain/gtd/mail.md", "brain/gtd/todo.md", "brain/gtd/github.md"},
		"outcome":     "Send alpha budget",
	})
	if err != nil {
		t.Fatalf("brain.gtd.bind: %v", err)
	}
	if got["binding_count"] != 4 {
		t.Fatalf("binding_count = %v, want 4: %#v", got["binding_count"], got)
	}
	parsed, err := s.callTool("brain.note.parse", map[string]interface{}{"config_path": configPath, "sphere": "work", "path": "brain/gtd/meeting.md"})
	if err != nil {
		t.Fatalf("parse winner: %v", err)
	}
	winner := parsed["commitment"].(*braingtd.Commitment)
	want := map[string]bool{"meetings:alpha-standup": false, "mail:m1": false, "todoist:t1": false, "github:org/repo#7": false}
	for _, binding := range winner.SourceBindings {
		if _, ok := want[binding.StableID()]; ok {
			want[binding.StableID()] = true
		}
	}
	for id, seen := range want {
		if !seen {
			t.Fatalf("winner missing binding %s: %#v", id, winner.SourceBindings)
		}
	}
	loser := parseDedupCommitment(t, s, configPath, "brain/gtd/github.md")
	if loser.LocalOverlay.Status != "dropped" || loser.Dedup.EquivalentTo != "brain/gtd/meeting.md" {
		t.Fatalf("loser state = %#v", loser)
	}
}

func TestBrainGTDBindAttachesNewSourceBinding(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	writeDedupCommitment(t, tmp, "work", "brain/gtd/meeting.md", "Send alpha budget", "meetings", "alpha-standup")

	s := NewServer(t.TempDir())
	got, err := s.callTool("brain.gtd.bind", map[string]interface{}{
		"config_path": configPath,
		"sphere":      "work",
		"winner_path": "brain/gtd/meeting.md",
		"source_bindings": []interface{}{
			map[string]interface{}{"provider": "mail", "ref": "m1"},
		},
	})
	if err != nil {
		t.Fatalf("brain.gtd.bind attach: %v", err)
	}
	if got["binding_count"] != 2 {
		t.Fatalf("binding_count = %v, want 2: %#v", got["binding_count"], got)
	}
	winner := parseDedupCommitment(t, s, configPath, "brain/gtd/meeting.md")
	if len(winner.SourceBindings) != 2 {
		t.Fatalf("winner bindings = %#v", winner.SourceBindings)
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

func parseDedupCommitment(t *testing.T, s *Server, configPath, path string) *braingtd.Commitment {
	t.Helper()
	parsed, err := s.callTool("brain.note.parse", map[string]interface{}{"config_path": configPath, "sphere": "work", "path": path})
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return parsed["commitment"].(*braingtd.Commitment)
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
