package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/brain"
)

const cleanupDeleteTarget = "/dev/null"

func cmdBrainCleanupDeadDirs(args []string) int {
	fs := flag.NewFlagSet("brain cleanup-dead-dirs", flag.ContinueOnError)
	configPath := fs.String("config", "", "vault config path")
	sphere := fs.String("sphere", "", "vault sphere")
	mode := fs.String("mode", "scan", "mode: scan or apply")
	confirm := fs.String("confirm", "", "candidate-list digest required for --mode apply")
	maxApply := fs.Int("max", 25, "maximum candidates to apply per run")
	includeLinked := fs.Bool("include-linked", false, "apply candidates with confidence=medium too")
	reasons := fs.String("reasons", "", "comma-separated reason filter (svn,pycache,node-modules,empty,old-with-live-sibling,bak-with-live-sibling)")
	excludePrefixes := fs.String("exclude-prefix", "", "comma-separated path prefixes to exclude")
	skipGate := fs.Bool("no-validate-after", false, "skip the post-apply integrity gate")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	reasonSet := parseCSVSet(*reasons)
	excludeList := parseCSVList(*excludePrefixes)
	if strings.TrimSpace(*sphere) == "" {
		fmt.Fprintln(os.Stderr, "--sphere is required")
		return 2
	}
	cfg, err := brain.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	switch strings.ToLower(strings.TrimSpace(*mode)) {
	case "", "scan":
		return runCleanupScan(cfg, brain.Sphere(*sphere), reasonSet, excludeList)
	case "apply":
		return runCleanupApply(cfg, brain.Sphere(*sphere), strings.TrimSpace(*confirm), *maxApply, *includeLinked, reasonSet, excludeList, *skipGate)
	default:
		fmt.Fprintf(os.Stderr, "unknown mode: %s\n", *mode)
		return 2
	}
}

func parseCSVSet(raw string) map[string]bool {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	out := map[string]bool{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out[part] = true
		}
	}
	return out
}

func parseCSVList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func candidateAccepted(c brain.DeadDirCandidate, reasonSet map[string]bool, excludePrefixes []string) bool {
	if reasonSet != nil && !reasonSet[c.Reason] {
		return false
	}
	for _, prefix := range excludePrefixes {
		if c.Path == prefix || strings.HasPrefix(c.Path, prefix+"/") {
			return false
		}
	}
	return true
}

func filterCandidates(in []brain.DeadDirCandidate, reasonSet map[string]bool, excludePrefixes []string) []brain.DeadDirCandidate {
	if reasonSet == nil && len(excludePrefixes) == 0 {
		return in
	}
	out := make([]brain.DeadDirCandidate, 0, len(in))
	for _, c := range in {
		if candidateAccepted(c, reasonSet, excludePrefixes) {
			out = append(out, c)
		}
	}
	return out
}

func runCleanupScan(cfg *brain.Config, sphere brain.Sphere, reasonSet map[string]bool, excludePrefixes []string) int {
	all, err := brain.CleanupDeadDirsScan(cfg, sphere)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	candidates := filterCandidates(all, reasonSet, excludePrefixes)
	digest, err := candidatesDigest(candidates)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return printBrainJSON(map[string]interface{}{
		"sphere":     string(sphere),
		"count":      len(candidates),
		"digest":     digest,
		"candidates": candidates,
	})
}

func runCleanupApply(cfg *brain.Config, sphere brain.Sphere, confirm string, maxApply int, includeLinked bool, reasonSet map[string]bool, excludePrefixes []string, skipGate bool) int {
	if confirm == "" {
		fmt.Fprintln(os.Stderr, "--confirm is required for --mode apply")
		return 2
	}
	all, err := brain.CleanupDeadDirsScan(cfg, sphere)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	candidates := filterCandidates(all, reasonSet, excludePrefixes)
	digest, err := candidatesDigest(candidates)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if digest != confirm {
		fmt.Fprintf(os.Stderr, "candidate digest changed since scan: have %s, got %s\n", digest, confirm)
		return 1
	}
	var before brain.IntegrityReport
	if !skipGate {
		before, err = brain.IntegrityScan(cfg, sphere)
		if err != nil {
			fmt.Fprintf(os.Stderr, "integrity scan (before): %v\n", err)
			return 1
		}
	}
	encoder := json.NewEncoder(os.Stdout)
	applied, skipped, failed := 0, 0, 0
	for _, candidate := range candidates {
		if !cleanupApplicable(candidate, includeLinked) {
			skipped++
			emitApplyEvent(encoder, candidate, "skipped", "low confidence")
			continue
		}
		if applied >= maxApply {
			skipped++
			emitApplyEvent(encoder, candidate, "skipped", "max reached")
			continue
		}
		// Fast path: scan already verified zero inbound links across both
		// vaults. PlanMove's cross-vault scan would do nothing useful and
		// costs several seconds per call. Delete directly and log.
		if candidate.Inbound == 0 {
			if err := fastDeleteCandidate(cfg, candidate); err != nil {
				failed++
				emitApplyEvent(encoder, candidate, "error", err.Error())
				continue
			}
			applied++
			emitApplyEvent(encoder, candidate, "ok", "")
			continue
		}
		plan, err := brain.PlanMove(cfg, candidate.Sphere, candidate.Path, cleanupDeleteTarget)
		if err != nil {
			failed++
			emitApplyEvent(encoder, candidate, "error", err.Error())
			continue
		}
		if err := brain.ApplyMove(cfg, plan, plan.Digest); err != nil {
			failed++
			emitApplyEvent(encoder, candidate, "error", err.Error())
			continue
		}
		applied++
		emitApplyEvent(encoder, candidate, "ok", "")
	}
	if err := encoder.Encode(map[string]interface{}{
		"summary": map[string]interface{}{
			"sphere":  string(sphere),
			"applied": applied,
			"skipped": skipped,
			"failed":  failed,
			"total":   len(candidates),
		},
	}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if !skipGate && applied > 0 {
		after, scanErr := brain.IntegrityScan(cfg, sphere)
		if scanErr != nil {
			fmt.Fprintf(os.Stderr, "integrity scan (after): %v\n", scanErr)
			return 1
		}
		reg := brain.CompareIntegrity(before, after)
		if reg.IsRegression() {
			emitIntegrityRegression(reg)
			fmt.Fprintf(os.Stderr, "integrity gate: cleanup introduced %d new broken link(s), %d new issue(s)\n",
				reg.NewBrokenLinks, reg.NewIssues)
			return 1
		}
	}
	if failed > 0 {
		return 1
	}
	return 0
}

// fastDeleteCandidate removes a dead directory whose scan-time inbound count
// was zero. Defends in depth: refuses paths inside personal/ and refuses
// protected brain areas (commitments/gtd/glossary).
func fastDeleteCandidate(cfg *brain.Config, candidate brain.DeadDirCandidate) error {
	rel := filepath.ToSlash(filepath.Clean(candidate.Path))
	if rel == "personal" || strings.HasPrefix(rel, "personal/") {
		return fmt.Errorf("refusing path inside personal/: %s", rel)
	}
	if brain.IsProtectedPath(rel) {
		return fmt.Errorf("refusing protected brain path: %s", rel)
	}
	vault, ok := cfg.Vault(candidate.Sphere)
	if !ok {
		return fmt.Errorf("unknown vault sphere: %s", candidate.Sphere)
	}
	abs := filepath.Join(vault.Root, filepath.FromSlash(rel))
	if err := os.RemoveAll(abs); err != nil {
		return fmt.Errorf("remove %q: %w", rel, err)
	}
	return appendCleanupArchivalLog(vault.Root, rel, candidate.Reason)
}

func appendCleanupArchivalLog(vaultRoot, rel, reason string) error {
	logPath := filepath.Join(vaultRoot, "brain", "generated", "archival-log.md")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return fmt.Errorf("ensure log dir: %w", err)
	}
	line := fmt.Sprintf("%s  delete  %s  (cleanup-dead-dirs: %s)\n",
		time.Now().UTC().Format("2006-01-02"), rel, reason)
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open archival log: %w", err)
	}
	defer f.Close()
	if _, err := f.WriteString(line); err != nil {
		return fmt.Errorf("write archival log: %w", err)
	}
	return nil
}

func cleanupApplicable(candidate brain.DeadDirCandidate, includeLinked bool) bool {
	if candidate.Confidence == "high" {
		return true
	}
	return includeLinked && candidate.Confidence == "medium"
}

func emitApplyEvent(encoder *json.Encoder, candidate brain.DeadDirCandidate, status, errMsg string) {
	event := map[string]interface{}{
		"sphere": string(candidate.Sphere),
		"path":   candidate.Path,
		"reason": candidate.Reason,
		"status": status,
	}
	if errMsg != "" {
		event["error"] = errMsg
	}
	if err := encoder.Encode(event); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
}

func candidatesDigest(candidates []brain.DeadDirCandidate) (string, error) {
	canonical, err := json.Marshal(candidates)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}
