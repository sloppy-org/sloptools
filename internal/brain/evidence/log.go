// Package evidence maintains the append-only evidence event log that
// connects scout findings to judge edits. Each entry records one verified
// or conflicting claim about a canonical entity, its source, and whether
// it has been applied to the vault.
//
// The log lives at <brain>/evidence/log.jsonl and is committed to git so
// the picker can compute per-entity yield ratios across nightly runs.
// Entries older than 90 days are pruned on every ReadRecent call.
package evidence

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Verdict classifies an evidence entry's finding.
const (
	VerdictVerified    = "verified"
	VerdictConflicting = "conflicting"
	VerdictOpen        = "open"
	VerdictSkipped     = "skipped" // note too sparse, no agent spawned
)

// Entry is one evidence claim extracted from a scout report or ingest
// triage decision. It is appended atomically to log.jsonl.
type Entry struct {
	TS            time.Time  `json:"ts"`
	RunID         string     `json:"run_id"`
	Entity        string     `json:"entity"` // vault-relative path, e.g. people/X.md
	Claim         string     `json:"claim"`
	Verdict       string     `json:"verdict"`
	Source        string     `json:"source,omitempty"`
	Confidence    float64    `json:"confidence"`
	SuggestedEdit string     `json:"suggested_edit,omitempty"`
	Applied       bool       `json:"applied"`
	AppliedAt     *time.Time `json:"applied_at,omitempty"`
	Reverted      bool       `json:"reverted,omitempty"`
}

// logPath returns the canonical path for the evidence log.
func logPath(brainRoot string) string {
	return filepath.Join(brainRoot, "evidence", "log.jsonl")
}

// Append adds entries to the log. Creates the file and its parent directory
// if they do not exist. Each entry is written as one JSON line.
func Append(brainRoot string, entries []Entry) error {
	if len(entries) == 0 {
		return nil
	}
	path := logPath(brainRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("evidence: mkdir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("evidence: open log: %w", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, e := range entries {
		if e.TS.IsZero() {
			e.TS = time.Now().UTC()
		}
		if err := enc.Encode(e); err != nil {
			return fmt.Errorf("evidence: encode entry: %w", err)
		}
	}
	return nil
}

// ReadRecent reads entries from the last `days` days, pruning older entries
// from the file atomically. Returns entries sorted oldest-first.
func ReadRecent(brainRoot string, days int) ([]Entry, error) {
	path := logPath(brainRoot)
	cutoff := time.Now().UTC().AddDate(0, 0, -days)

	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("evidence: open: %w", err)
	}
	defer f.Close()

	var kept []Entry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 256*1024), 256*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var e Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue // skip malformed lines
		}
		if e.TS.After(cutoff) {
			kept = append(kept, e)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("evidence: scan: %w", err)
	}
	f.Close()

	// Atomically rewrite with only kept entries.
	if err := rewrite(path, kept); err != nil {
		return nil, fmt.Errorf("evidence: rewrite: %w", err)
	}
	return kept, nil
}

// ReadForEntity returns unapplied entries for a specific entity path.
func ReadForEntity(brainRoot, entity string, days int) ([]Entry, error) {
	all, err := ReadRecent(brainRoot, days)
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, e := range all {
		if e.Entity == entity && !e.Applied && !e.Reverted {
			out = append(out, e)
		}
	}
	return out, nil
}

// ReadUnapplied returns all unapplied, non-reverted entries from the last
// `days` days across all run IDs. Used by the triage stage so that evidence
// gathered by partial or killed prior runs is not orphaned.
func ReadUnapplied(brainRoot string, days int) ([]Entry, error) {
	all, err := ReadRecent(brainRoot, days)
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, e := range all {
		if !e.Applied && !e.Reverted {
			out = append(out, e)
		}
	}
	return out, nil
}

// ReadByRunID returns all entries written by a specific run.
func ReadByRunID(brainRoot, runID string) ([]Entry, error) {
	path := logPath(brainRoot)
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("evidence: open: %w", err)
	}
	defer f.Close()

	var out []Entry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 256*1024), 256*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var e Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		if e.RunID == runID {
			out = append(out, e)
		}
	}
	return out, scanner.Err()
}

// MarkApplied sets applied=true and applied_at=now for all unapplied entries
// matching the given entity and runID. Rewrites the file atomically.
func MarkApplied(brainRoot, entity, runID string) error {
	path := logPath(brainRoot)
	all, err := readAll(path)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	changed := false
	for i := range all {
		if all[i].Entity == entity && all[i].RunID == runID && !all[i].Applied {
			all[i].Applied = true
			all[i].AppliedAt = &now
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return rewrite(path, all)
}

// MarkAppliedAll sets applied=true for all unapplied entries for an entity,
// regardless of run ID. Used after a successful edit so evidence from prior
// partial runs is not re-applied on the next night.
func MarkAppliedAll(brainRoot, entity string) error {
	path := logPath(brainRoot)
	all, err := readAll(path)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	changed := false
	for i := range all {
		if all[i].Entity == entity && !all[i].Applied {
			all[i].Applied = true
			all[i].AppliedAt = &now
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return rewrite(path, all)
}

// MarkReverted sets reverted=true for entries matching the given entity
// that were previously applied. Used by the feedback stage when git reverts
// are detected.
func MarkReverted(brainRoot, entity string) error {
	path := logPath(brainRoot)
	all, err := readAll(path)
	if err != nil {
		return err
	}
	changed := false
	for i := range all {
		if all[i].Entity == entity && all[i].Applied && !all[i].Reverted {
			all[i].Reverted = true
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return rewrite(path, all)
}

// YieldRatio computes the fraction of applied entries vs total for an
// entity over the last `days` days. Returns 0.5 when there is no history.
func YieldRatio(brainRoot, entity string, days int) float64 {
	all, err := ReadRecent(brainRoot, days)
	if err != nil || len(all) == 0 {
		return 0.5
	}
	var total, applied int
	for _, e := range all {
		if e.Entity != entity {
			continue
		}
		if e.Verdict == VerdictSkipped {
			continue
		}
		total++
		if e.Applied && !e.Reverted {
			applied++
		}
	}
	if total == 0 {
		return 0.5
	}
	return float64(applied) / float64(total)
}

// ParseBullets parses a scout report body and returns evidence entries.
// It extracts bullets from ## Verified, ## Conflicting / outdated, and
// ## Suggestions sections. No LLM — pure line-by-line parsing.
func ParseBullets(runID, entity, body string, now time.Time) []Entry {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var entries []Entry
	lines := strings.Split(body, "\n")
	section := ""
	for _, raw := range lines {
		trim := strings.TrimSpace(raw)
		if strings.HasPrefix(trim, "##") {
			lower := strings.ToLower(trim)
			switch {
			case strings.Contains(lower, "verified"):
				section = "verified"
			case strings.Contains(lower, "conflicting") || strings.Contains(lower, "outdated"):
				section = "conflicting"
			case strings.Contains(lower, "suggestion"):
				section = "suggestion"
			default:
				section = ""
			}
			continue
		}
		if section == "" || !strings.HasPrefix(trim, "- ") {
			continue
		}
		bullet := strings.TrimSpace(trim[2:])
		if bullet == "" || strings.EqualFold(bullet, "(none)") {
			continue
		}
		// Extract source from "(source: ...)" suffix.
		source := ""
		if idx := strings.Index(bullet, "(source:"); idx >= 0 {
			end := strings.LastIndex(bullet, ")")
			if end > idx {
				source = strings.TrimSpace(bullet[idx+8 : end])
				bullet = strings.TrimSpace(bullet[:idx])
			}
		}
		// Truncate long claims to keep the log compact.
		claim := bullet
		if len(claim) > 300 {
			claim = claim[:297] + "..."
		}
		var e Entry
		e.TS = now
		e.RunID = runID
		e.Entity = entity
		e.Claim = claim
		e.Source = source
		switch section {
		case "verified":
			e.Verdict = VerdictVerified
			e.Confidence = 0.9
		case "conflicting":
			e.Verdict = VerdictConflicting
			e.Confidence = 0.85
		case "suggestion":
			e.Verdict = VerdictVerified
			e.Confidence = 0.8
			e.SuggestedEdit = claim
		}
		entries = append(entries, e)
	}
	return entries
}

func readAll(path string) ([]Entry, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("evidence: open: %w", err)
	}
	defer f.Close()
	var entries []Entry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 256*1024), 256*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var e Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		entries = append(entries, e)
	}
	return entries, scanner.Err()
}

func rewrite(path string, entries []Entry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("evidence rewrite: mkdir: %w", err)
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("evidence rewrite: open tmp: %w", err)
	}
	enc := json.NewEncoder(f)
	for _, e := range entries {
		if err := enc.Encode(e); err != nil {
			f.Close()
			os.Remove(tmp)
			return fmt.Errorf("evidence rewrite: encode: %w", err)
		}
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("evidence rewrite: close: %w", err)
	}
	return os.Rename(tmp, path)
}
