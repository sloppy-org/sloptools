package gtd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

type DedupConfig struct {
	DeterministicThreshold float64 `toml:"deterministic_threshold"`
	LLMThreshold           float64 `toml:"llm_threshold"`
	CandidateThreshold     float64 `toml:"candidate_threshold"`
	LLMCommand             string  `toml:"llm_command"`
}

type dedupConfigFile struct {
	Dedup DedupConfig `toml:"dedup"`
}

type CommandReviewer struct {
	Command string
	Timeout time.Duration
}

func LoadDedupConfig(path string) (DedupConfig, error) {
	if strings.TrimSpace(path) == "" {
		return DedupConfig{}, nil
	}
	var file dedupConfigFile
	if _, err := toml.DecodeFile(path, &file); err != nil {
		return DedupConfig{}, err
	}
	return file.Dedup, nil
}

func (c DedupConfig) ScanOptions() ScanOptions {
	opts := ScanOptions{
		DeterministicThreshold: c.DeterministicThreshold,
		LLMThreshold:           c.LLMThreshold,
		CandidateThreshold:     c.CandidateThreshold,
	}
	if strings.TrimSpace(c.LLMCommand) != "" {
		opts.LLM = CommandReviewer{Command: c.LLMCommand, Timeout: 15 * time.Second}
	}
	return opts
}

func (r CommandReviewer) ReviewSimilarity(a, b CommitmentEntry, score float64) (LLMReview, error) {
	if strings.TrimSpace(r.Command) == "" {
		return LLMReview{}, fmt.Errorf("llm command is empty")
	}
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", r.Command)
	cmd.Stdin = strings.NewReader(llmPrompt(a, b, score))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return LLMReview{}, fmt.Errorf("llm command failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	var out LLMReview
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &out); err != nil {
		return LLMReview{}, fmt.Errorf("llm output must be JSON: %w", err)
	}
	if out.Confidence < 0 {
		out.Confidence = 0
	}
	if out.Confidence > 1 {
		out.Confidence = 1
	}
	return out, nil
}

func llmPrompt(a, b CommitmentEntry, score float64) string {
	payload := map[string]interface{}{
		"instruction":         "Decide whether two GTD commitments represent the same outcome. Return only JSON with same, confidence, and reasoning.",
		"deterministic_score": score,
		"a":                   compactCommitmentForLLM(a),
		"b":                   compactCommitmentForLLM(b),
	}
	body, _ := json.Marshal(payload)
	return string(body)
}

func compactCommitmentForLLM(entry CommitmentEntry) map[string]interface{} {
	c := entry.Commitment
	return map[string]interface{}{
		"path":     entry.Path,
		"outcome":  c.Outcome,
		"title":    c.Title,
		"people":   c.People,
		"project":  c.Project,
		"track":    c.EffectiveTrack(),
		"due":      c.Due,
		"followup": c.FollowUp,
		"sources":  compactSourceBindings(c.SourceBindings),
	}
}

func compactSourceBindings(bindings []SourceBinding) []map[string]string {
	out := make([]map[string]string, 0, len(bindings))
	for _, binding := range bindings {
		out = append(out, map[string]string{
			"provider": binding.Provider,
			"ref":      binding.Ref,
			"summary":  binding.Summary,
		})
	}
	return out
}
