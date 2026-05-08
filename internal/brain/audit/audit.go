// Package audit holds the per-stage sidecar + audit-file helpers that
// every brain-night pipeline (scout, sleep, future stages) writes next
// to the canonical report. Extracted from internal/brain/scout so the
// bulk → resolve → escalate pattern is reusable without duplicating the
// path-suffix conventions or the JSON schema.
//
// File layout per pick:
//
//	<reportPath>.bulk.md          cleaned bulk-tier output
//	<reportPath>.bulk.raw.md      raw output, only when narration trimmed
//	<reportPath>.resolve.<N>.md   per self-resolve pass
//	<reportPath>.escalate.<bk>.md per paid escalation by backend id
//	<reportPath>.audit.json       per-pick chain summary
//
// The schema is intentionally flat-ish so a curious user can `cat` the
// audit JSON without piping through jq.
package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// StageRecord is one entry in the audit trail captured per pick. Every
// bulk / self-resolve / escalate pass appends one record to the chain;
// the deterministic classifier reason that decided the next step is
// recorded on `ReasonAfter` for the stage that produced it and on
// `TriggerReason` for the next stage that ran because of it.
type StageRecord struct {
	Stage         string    `json:"stage"`
	Backend       string    `json:"backend"`
	Provider      string    `json:"provider"`
	Model         string    `json:"model"`
	Tier          string    `json:"tier"`
	StartedAt     time.Time `json:"started_at"`
	WallMS        int64     `json:"wall_ms"`
	TokensIn      int64     `json:"tokens_in"`
	TokensOut     int64     `json:"tokens_out"`
	CostHint      float64   `json:"cost_hint,omitempty"`
	RawPath       string    `json:"raw_path,omitempty"`
	CleanedPath   string    `json:"cleaned_path,omitempty"`
	RawBytes      int       `json:"raw_bytes"`
	CleanedBytes  int       `json:"cleaned_bytes"`
	ReasonAfter   string    `json:"reason_after,omitempty"`
	TriggerReason string    `json:"trigger_reason,omitempty"`
}

// File is the per-pick audit summary written next to the canonical
// report.
type File struct {
	Path         string        `json:"path"`
	Title        string        `json:"title,omitempty"`
	ReportPath   string        `json:"report_path"`
	RunID        string        `json:"run_id"`
	Sphere       string        `json:"sphere"`
	StartedAt    time.Time     `json:"started_at"`
	EndedAt      time.Time     `json:"ended_at"`
	FinalStage   string        `json:"final_stage"`
	SelfResolved bool          `json:"self_resolved,omitempty"`
	Escalated    bool          `json:"escalated"`
	Stages       []StageRecord `json:"stages"`
}

// WriteStageArtifact writes the cleaned body to
// <reportPath>.<suffix>.md and (only when the raw model output differs)
// the raw body to <reportPath>.<suffix>.raw.md. Returns the absolute
// paths so the audit trail can record them. An empty cleaned body
// short-circuits the cleaned write; callers should check before
// invoking.
//
// suffix is something like "bulk", "resolve.1", "escalate.codex"; do
// not include the leading dot. The .md extension is appended here.
func WriteStageArtifact(reportPath, suffix, raw, cleaned string) (rawPath, cleanedPath string, err error) {
	if reportPath == "" {
		return "", "", fmt.Errorf("audit: empty reportPath")
	}
	if suffix == "" {
		return "", "", fmt.Errorf("audit: empty suffix")
	}
	base := strings.TrimSuffix(reportPath, ".md")
	cleanedPath = base + "." + suffix + ".md"
	if cleaned != "" {
		if err := os.WriteFile(cleanedPath, []byte(cleaned+"\n"), 0o644); err != nil {
			return "", "", fmt.Errorf("write %s: %w", cleanedPath, err)
		}
	} else {
		cleanedPath = ""
	}
	// Only write the raw sidecar when narration was actually trimmed.
	// Skipping the duplicate keeps the directory tidy on the common
	// well-formed-output case.
	if strings.TrimSpace(raw) != strings.TrimSpace(cleaned) && raw != "" {
		rawPath = base + "." + suffix + ".raw.md"
		if err := os.WriteFile(rawPath, []byte(raw), 0o644); err != nil {
			return "", "", fmt.Errorf("write %s: %w", rawPath, err)
		}
	}
	return rawPath, cleanedPath, nil
}

// WriteFile flushes the per-pick audit trail to JSON next to the
// canonical report path. Encoding is pretty-printed.
func WriteFile(reportPath string, audit File) error {
	if reportPath == "" {
		return fmt.Errorf("audit: empty reportPath")
	}
	base := strings.TrimSuffix(reportPath, ".md")
	auditPath := base + ".audit.json"
	buf, err := json.MarshalIndent(audit, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal audit: %w", err)
	}
	buf = append(buf, '\n')
	if err := os.WriteFile(auditPath, buf, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", auditPath, err)
	}
	return nil
}
