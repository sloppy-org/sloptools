package scout

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// stageRecord is one entry in the audit trail captured per pick.
// Every bulk / self-resolve / escalate pass appends one of these to the
// per-pick auditTrail, which is then flushed to <reportPath>.audit.json
// when the pick finishes. It is the post-hoc record of every model call
// the harness made for this entity, including the deterministic
// classifier reason that decided the next step.
type stageRecord struct {
	Stage        string    `json:"stage"`
	Backend      string    `json:"backend"`
	Provider     string    `json:"provider"`
	Model        string    `json:"model"`
	Tier         string    `json:"tier"`
	StartedAt    time.Time `json:"started_at"`
	WallMS       int64     `json:"wall_ms"`
	TokensIn     int64     `json:"tokens_in"`
	TokensOut    int64     `json:"tokens_out"`
	CostHint     float64   `json:"cost_hint,omitempty"`
	RawPath      string    `json:"raw_path,omitempty"`
	CleanedPath  string    `json:"cleaned_path,omitempty"`
	RawBytes     int       `json:"raw_bytes"`
	CleanedBytes int       `json:"cleaned_bytes"`
	// ReasonAfter is the deterministic classifier output evaluated
	// against this stage's cleaned body. It explains why the next stage
	// happened — e.g. "≥3 conflict bullets", "explicit needs-paid-review
	// marker", "cry-for-help phrase: unable to verify". Empty means the
	// classifier did not flag this stage's output (or this is the final
	// stage).
	ReasonAfter string `json:"reason_after,omitempty"`
	// TriggerReason is the classifier reason that caused THIS stage to
	// run (i.e. the previous stage's ReasonAfter). Empty for the bulk
	// stage which is unconditional.
	TriggerReason string `json:"trigger_reason,omitempty"`
}

// auditFile is the per-pick audit summary written next to the canonical
// report. One entity → one .audit.json. The schema is intentionally
// flat-ish so jq one-liners stay readable.
type auditFile struct {
	Path         string        `json:"path"`
	Title        string        `json:"title,omitempty"`
	ReportPath   string        `json:"report_path"`
	RunID        string        `json:"run_id"`
	Sphere       string        `json:"sphere"`
	StartedAt    time.Time     `json:"started_at"`
	EndedAt      time.Time     `json:"ended_at"`
	FinalStage   string        `json:"final_stage"`
	SelfResolved bool          `json:"self_resolved"`
	Escalated    bool          `json:"escalated"`
	Stages       []stageRecord `json:"stages"`
}

// writeStageArtifact writes the cleaned body to
// <reportPath>.<suffix>.md and (only when the raw model output differs)
// the raw body to <reportPath>.<suffix>.raw.md. Returns the absolute
// paths so the audit trail can record them. Empty input bodies are not
// written; callers must check for the empty-body short-circuit before
// calling this.
//
// suffix is something like "bulk", "resolve.1", "escalate.codex"; do
// not include the leading dot. The .md extension is appended here.
func writeStageArtifact(reportPath, suffix, raw, cleaned string) (rawPath, cleanedPath string, err error) {
	if reportPath == "" {
		return "", "", fmt.Errorf("artifacts: empty reportPath")
	}
	if suffix == "" {
		return "", "", fmt.Errorf("artifacts: empty suffix")
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

// writeAuditFile flushes the per-pick audit trail to JSON next to the
// canonical report path. Encoding is pretty-printed so a curious user
// can `cat` the file without piping through jq.
func writeAuditFile(reportPath string, audit auditFile) error {
	if reportPath == "" {
		return fmt.Errorf("artifacts: empty reportPath")
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
