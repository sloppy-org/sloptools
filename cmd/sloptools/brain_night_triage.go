package main

import (
	"context"
	"fmt"

	"github.com/sloppy-org/sloptools/internal/brain"
	brainEdit "github.com/sloppy-org/sloptools/internal/brain/edit"
	"github.com/sloppy-org/sloptools/internal/brain/evidence"
	"github.com/sloppy-org/sloptools/internal/brain/ledger"
	"github.com/sloppy-org/sloptools/internal/brain/routing"
	"github.com/sloppy-org/sloptools/internal/brain/scout"
	"github.com/sloppy-org/sloptools/internal/brain/triage"
)

// runTriageEditStages runs triage → per-entity edit → propose → feedback.
// It reads tonight's evidence log entries, asks qwen-MoE to rank which
// entities need attention, then runs a MoE bulk pass per entity to apply edits.
func runTriageEditStages(ctx context.Context, vault brain.Vault, ldg *ledger.Ledger, router *routing.Router, runID, sphere string, dryRun bool, report *nightReport) error {
	brainRoot := vault.BrainRoot()

	// Load tonight's evidence entries.
	entries, err := evidence.ReadByRunID(brainRoot, runID)
	if err != nil {
		return fmt.Errorf("read evidence: %w", err)
	}

	// Get entity candidate paths from scout picker for fallback.
	picks, _ := scout.PickEntities(scout.PickerOpts{BrainRoot: brainRoot, TopN: 50})
	var entityPaths []string
	for _, p := range picks {
		entityPaths = append(entityPaths, p.Path)
	}

	// Triage: rank which entities need editorial attention tonight.
	items, err := triage.Run(ctx, triage.Opts{
		BrainRoot:      brainRoot,
		RunID:          runID,
		ActivityDigest: report.ActivityDigest,
		Entries:        entries,
		EntityPaths:    entityPaths,
		Now:            report.StartedAt,
	})
	if err != nil {
		return fmt.Errorf("triage: %w", err)
	}
	report.Triage = items

	if dryRun {
		return nil
	}

	// Edit: per-entity focused pass.
	editReport, err := brainEdit.Run(ctx, brainEdit.Opts{
		BrainRoot:      brainRoot,
		RunID:          runID,
		Sphere:         sphere,
		Items:          items,
		AllEntries:     entries,
		ActivityDigest: report.ActivityDigest,
		Router:         router,
		Ledger:         ldg,
		Now:            report.StartedAt,
	})
	if err != nil {
		return fmt.Errorf("edit: %w", err)
	}
	report.Edit = editReport

	// Propose: conflicting high-confidence entries → commitments/_proposed/.
	_ = brainEdit.ProposeConflicts(brainRoot, entries, report.StartedAt)

	// Feedback: detect reverts, update evidence log.
	_, _ = brainEdit.DetectReverts(brainRoot, runID, report.StartedAt.AddDate(0, 0, -1))

	return nil
}
