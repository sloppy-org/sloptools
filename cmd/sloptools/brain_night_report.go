package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sloppy-org/sloptools/internal/brain"
	"github.com/sloppy-org/sloptools/internal/brain/backend"
	"github.com/sloppy-org/sloptools/internal/brain/ledger"
)

func computeSpend(ldg *ledger.Ledger, sessionStart, now time.Time) *spendSummary {
	out := &spendSummary{SessionStart: sessionStart.Format(time.RFC3339)}
	if v, err := ldg.RollingShare(backend.ProviderAnthropic, sessionStart, now); err == nil {
		out.AnthropicSessionShare = v
	}
	if v, err := ldg.RollingShare(backend.ProviderOpenAI, sessionStart, now); err == nil {
		out.OpenAISessionShare = v
	}
	if v, err := ldg.WeeklyShare(backend.ProviderAnthropic, now); err == nil {
		out.AnthropicWeeklyShare = v
	}
	if v, err := ldg.WeeklyShare(backend.ProviderOpenAI, now); err == nil {
		out.OpenAIWeeklyShare = v
	}
	return out
}

func writeNightReport(vault brain.Vault, runID string, r *nightReport) error {
	if r.DryRun {
		return nil
	}
	dir := filepath.Join(vault.BrainRoot(), "reports", "night")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("night: mkdir report dir: %w", err)
	}
	path := filepath.Join(dir, runID+".json")
	body, err := encodeIndentJSON(r)
	if err != nil {
		return err
	}
	return os.WriteFile(path, body, 0o644)
}
