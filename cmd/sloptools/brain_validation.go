package main

import (
	"fmt"
	"strings"

	"github.com/sloppy-org/sloptools/internal/brain"
	braingtd "github.com/sloppy-org/sloptools/internal/brain/gtd"
)

func validateRenderedBrainNote(rendered string) error {
	if diags := brain.ValidateMarkdownNote(rendered, brain.MarkdownParseOptions{}); len(diags) != 0 {
		return fmt.Errorf("rendered Markdown note failed validation: %s", formatBrainDiagnostics(diags))
	}
	return nil
}

func validateRenderedBrainGTD(rendered string) error {
	if diags := braingtd.ValidateRenderedCommitment(rendered); len(diags) != 0 {
		return fmt.Errorf("rendered GTD note failed validation: %s", formatBrainDiagnostics(diags))
	}
	return nil
}

func formatBrainDiagnostics(diags []brain.MarkdownDiagnostic) string {
	parts := make([]string, 0, len(diags))
	for _, diag := range diags {
		if diag.Line > 0 {
			parts = append(parts, fmt.Sprintf("line %d: %s", diag.Line, diag.Message))
			continue
		}
		parts = append(parts, diag.Message)
	}
	return strings.Join(parts, "; ")
}
