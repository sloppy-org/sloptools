package scout

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sloppy-org/sloptools/internal/brain/backend"
	"github.com/sloppy-org/sloptools/internal/brain/ledger"
	"github.com/sloppy-org/sloptools/internal/brain/routing"
)

func escalateOne(ctx context.Context, opts RunOpts, entry *ReportEntry, p Pick, originalPacket, bulkReport, reason, reportPath string) error {
	pick, err := opts.Router.Pick(routing.StageTriage)
	if err != nil {
		return fmt.Errorf("router pick triage: %w", err)
	}
	if pick.Provider == backend.ProviderLocal {
		return fmt.Errorf("paid tiers saturated; staying on bulk")
	}
	be, err := backendForID(pick.BackendID)
	if err != nil {
		return fmt.Errorf("backendForID: %w", err)
	}
	stagePrompt, err := writeEscalatePrompt(opts.RunID)
	if err != nil {
		return fmt.Errorf("write prompt: %w", err)
	}
	defer os.Remove(stagePrompt)
	stage := "scout-escalate-" + sanitize(p.Path)
	sb, err := backend.NewSandbox(opts.RunID, stage, stagePrompt, backend.DefaultMCPConfig())
	if err != nil {
		return fmt.Errorf("sandbox: %w", err)
	}
	defer sb.Cleanup()
	packet := buildEscalatePacket(p, originalPacket, bulkReport, reason)
	resp, err := be.Run(ctx, backend.Request{
		Stage:            stage,
		Packet:           packet,
		SystemPromptPath: sb.SystemPromptIn,
		Model:            pick.Model,
		Reasoning:        pick.Reasoning,
		AllowEdits:       false,
		Sandbox:          sb,
	})
	if err != nil {
		return fmt.Errorf("backend run: %w", err)
	}
	body := strings.TrimSpace(resp.Output)
	if body == "" {
		return fmt.Errorf("empty escalation output")
	}
	if err := os.WriteFile(reportPath, []byte(body+"\n"), 0o644); err != nil {
		return fmt.Errorf("write escalated report: %w", err)
	}
	entry.Escalated = true
	entry.EscalationReason = reason
	entry.EscalationBackend = pick.BackendID
	entry.EscalationModel = pick.Model
	if opts.Ledger != nil {
		_ = opts.Ledger.Append(ledger.Entry{
			Sphere:    opts.Sphere,
			Stage:     stage,
			Provider:  pick.Provider,
			Backend:   pick.BackendID,
			Model:     pick.Model,
			TokensIn:  resp.TokensIn,
			TokensOut: resp.TokensOut,
			WallMS:    resp.WallMS,
			CostHint:  resp.CostHint,
			Extras:    map[string]string{"path": p.Path, "tier": string(pick.Tier), "escalation": "true"},
		})
	}
	return nil
}

func writeEscalatePrompt(runID string) (string, error) {
	dir, err := os.MkdirTemp("", "sloptools-escalate-prompt-")
	if err != nil {
		return "", err
	}
	body := strings.Join([]string{
		"You are a paid reviewer for Christopher Albert's brain vault.",
		"",
		"You receive a scout packet, plus a bulk-tier (opencode/qwen) evidence",
		"report that flagged conflicts or open questions. Resolve each conflict",
		"using sloppy and helpy MCP tools. Do not just rewrite the bulk report:",
		"address each conflict and each open question with a fresh, traceable",
		"answer. Output Markdown with the same section structure as the bulk",
		"report (Verified / Conflicting / outdated / Suggestions / Open questions).",
		"Cite sources by URL or DOI per claim. Mark anything you could not",
		"resolve as still open. Never edit canonical Markdown.",
	}, "\n")
	path := filepath.Join(dir, "escalate.md")
	return path, os.WriteFile(path, []byte(body), 0o600)
}

func buildEscalatePacket(p Pick, originalPacket, bulkReport, reason string) string {
	var b strings.Builder
	b.WriteString("# Scout escalation packet\n\n")
	fmt.Fprintf(&b, "Path: `%s`\n", p.Path)
	fmt.Fprintf(&b, "Title: %s\n", p.Title)
	fmt.Fprintf(&b, "Bulk-tier flagged for escalation because: %s\n\n", reason)
	b.WriteString("## Original scout packet\n\n")
	b.WriteString(originalPacket)
	b.WriteString("\n\n## Bulk-tier (opencode) report\n\n")
	b.WriteString(bulkReport)
	b.WriteString("\n\n## Your task\n\n")
	b.WriteString("Resolve each conflict and each open question listed in the bulk report. Output a refined Markdown report with the same section structure. Cite sources per claim.\n")
	return b.String()
}

// escalateDecision is the deterministic classifier output for one
// bulk-tier scout report. Reason is empty when no escalation is needed.
type escalateDecision struct {
	Escalate bool
	Reason   string
}

// classifyForEscalation reads a scout report body and decides whether
// to re-run it at a paid medium tier. The 2026-05-07 first-with-
// escalation run showed 100% trigger rate on the original
// "any-conflict-bullet-or-multiple-open-questions" heuristic — most
// honest scout reports surface at least one drift item (status, email,
// affiliation) that the bulk pass already resolved with a citation.
// Tighter triggers, from observation:
//
//   - explicit "needs paid review:" bullet anywhere — caller signal
//   - cry-for-help phrases ("unable to verify", "could not confirm",
//     "not externally accessible", "no source available") in any
//     Verified / Conflicting / Open question bullet — bulk gave up
//   - ≥3 substantive `## Conflicting / outdated` bullets — severe drift
//   - ≥3 substantive `## Open questions` bullets — bulk hit a wall
//
// Substantive means: not "(none)", "(unverified)" / "(unconfirmed)" /
// "(tbd)" alone, and not empty after trimming the leading dash.
func classifyForEscalation(body string) escalateDecision {
	if cryReason := scanCryForHelp(body); cryReason != "" {
		return escalateDecision{Escalate: true, Reason: cryReason}
	}
	conflicts := countSubstantiveBullets(body, "## Conflicting", "## Conflicting / outdated", "## Conflicting/outdated")
	questions := countSubstantiveBullets(body, "## Open Questions", "## Open questions")
	if conflicts >= 3 {
		return escalateDecision{Escalate: true, Reason: "≥3 conflict bullets"}
	}
	if questions >= 3 {
		return escalateDecision{Escalate: true, Reason: "≥3 open questions"}
	}
	return escalateDecision{}
}

// scanCryForHelp returns a non-empty reason when the body contains an
// explicit "needs paid review" line or a phrase the bulk model uses
// when it could not finish the job. Case-insensitive.
func scanCryForHelp(body string) string {
	lower := strings.ToLower(body)
	if strings.Contains(lower, "- needs paid review:") || strings.Contains(lower, "- needs paid review ") {
		return "explicit needs-paid-review marker"
	}
	for _, phrase := range []string{
		"unable to verify",
		"could not verify",
		"could not confirm",
		"unable to confirm",
		"unable to access",
		"not externally accessible",
		"no source available",
		"no external source",
	} {
		if strings.Contains(lower, phrase) {
			return "cry-for-help phrase: " + phrase
		}
	}
	return ""
}

// countSubstantiveBullets returns the number of bullet lines under the
// FIRST heading whose name matches any of the provided headingPrefixes.
// Bullets that are "(none)", "(unverified)", "(unconfirmed)", "(tbd)", or
// empty after trimming the leading dash do not count.
func countSubstantiveBullets(body string, headingPrefixes ...string) int {
	lines := strings.Split(body, "\n")
	inSection := false
	count := 0
	for _, raw := range lines {
		line := strings.TrimRight(raw, " \t")
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "##") {
			inSection = false
			for _, pref := range headingPrefixes {
				if strings.EqualFold(strings.TrimSpace(trim), pref) || strings.HasPrefix(strings.ToLower(trim), strings.ToLower(pref)) {
					inSection = true
					break
				}
			}
			continue
		}
		if !inSection {
			continue
		}
		if !strings.HasPrefix(trim, "- ") {
			continue
		}
		body := strings.TrimSpace(trim[2:])
		lower := strings.ToLower(body)
		switch lower {
		case "", "(none)", "none", "(unverified)", "(unconfirmed)", "(tbd)", "tbd":
			continue
		}
		count++
	}
	return count
}
