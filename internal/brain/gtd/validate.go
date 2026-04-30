package gtd

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/sloppy-org/sloptools/internal/brain"
)

type ValidationResult struct {
	Commitment  Commitment                 `json:"commitment"`
	Diagnostics []brain.MarkdownDiagnostic `json:"diagnostics,omitempty"`
}

var (
	datePattern      = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)
	validStatuses    = set("inbox", "next", "waiting", "deferred", "someday", "maybe_stale", "closed", "done", "dropped")
	validSpheres     = set("work", "private")
	validReviewState = set("", "ok", "needs_human", "blocked_credentials", "needs_evidence")
	requiredSections = []string{"Summary", "Next Action", "Evidence", "Linked Items", "Review Notes"}
)

func ParseAndValidate(src string) ValidationResult {
	commitment, note, diags := ParseCommitmentMarkdown(src)
	diags = append(diags, ValidateCommitment(*commitment, note)...)
	return ValidationResult{Commitment: *commitment, Diagnostics: diags}
}

func ValidateRenderedCommitment(src string) []brain.MarkdownDiagnostic {
	result := ParseAndValidate(src)
	return result.Diagnostics
}

func ValidateCommitment(commitment Commitment, note *brain.MarkdownNote) []brain.MarkdownDiagnostic {
	var diags []brain.MarkdownDiagnostic
	diags = append(diags, requireValue("kind", commitment.Kind)...)
	if commitment.Kind != "" && commitment.Kind != "commitment" {
		diags = append(diags, diagf("kind must be commitment"))
	}
	diags = append(diags, requireValue("sphere", commitment.Sphere)...)
	if commitment.Sphere != "" && !validSpheres[strings.ToLower(commitment.Sphere)] {
		diags = append(diags, diagf("sphere must be work or private"))
	}
	diags = append(diags, validateStatus(commitment)...)
	diags = append(diags, validateDates(commitment)...)
	diags = append(diags, validateRequiredText(commitment)...)
	diags = append(diags, validateSources(commitment)...)
	diags = append(diags, validateSections(note)...)
	return diags
}

func validateStatus(commitment Commitment) []brain.MarkdownDiagnostic {
	status := strings.ToLower(strings.TrimSpace(commitment.Status))
	if status == "" {
		return []brain.MarkdownDiagnostic{diagf("status is required")}
	}
	if !validStatuses[status] {
		return []brain.MarkdownDiagnostic{diagf("unknown status %q", commitment.Status)}
	}
	var diags []brain.MarkdownDiagnostic
	switch status {
	case "next":
		if commitment.NextAction == "" {
			diags = append(diags, diagf("next commitments require next_action"))
		}
	case "waiting":
		if commitment.WaitingFor == "" {
			diags = append(diags, diagf("waiting commitments require waiting_for"))
		}
	case "deferred":
		if commitment.FollowUp == "" {
			diags = append(diags, diagf("deferred commitments require follow_up"))
		}
	}
	return diags
}

func validateDates(commitment Commitment) []brain.MarkdownDiagnostic {
	var diags []brain.MarkdownDiagnostic
	for _, field := range []struct {
		name  string
		value string
	}{
		{"due", commitment.Due},
		{"follow_up", commitment.FollowUp},
	} {
		if field.value != "" && !datePattern.MatchString(field.value) {
			diags = append(diags, diagf("%s must be YYYY-MM-DD or empty", field.name))
		}
	}
	if strings.EqualFold(commitment.Status, "deferred") && commitment.FollowUp == "" && commitment.Due != "" {
		diags = append(diags, diagf("defer/start dates belong in follow_up; due is only a hard deadline"))
	}
	if commitment.ReviewState != "" && !validReviewState[strings.ToLower(commitment.ReviewState)] {
		diags = append(diags, diagf("unknown review_state %q", commitment.ReviewState))
	}
	return diags
}

func validateRequiredText(commitment Commitment) []brain.MarkdownDiagnostic {
	var diags []brain.MarkdownDiagnostic
	if commitment.Outcome == "" && commitment.Title == "" {
		diags = append(diags, diagf("outcome or title is required"))
	}
	if commitment.Context == "" {
		diags = append(diags, diagf("context is required"))
	}
	return diags
}

func validateSources(commitment Commitment) []brain.MarkdownDiagnostic {
	if len(commitment.SourceBindings) == 0 && len(commitment.LegacySources) == 0 {
		return []brain.MarkdownDiagnostic{diagf("source_bindings or legacy source_refs is required")}
	}
	var diags []brain.MarkdownDiagnostic
	for i, binding := range commitment.SourceBindings {
		if strings.TrimSpace(binding.Provider) == "" {
			diags = append(diags, diagf("source_bindings[%d].provider is required", i))
		}
		if strings.TrimSpace(binding.Ref) == "" {
			diags = append(diags, diagf("source_bindings[%d].ref is required", i))
		}
	}
	return diags
}

func validateSections(note *brain.MarkdownNote) []brain.MarkdownDiagnostic {
	if note == nil {
		return []brain.MarkdownDiagnostic{diagf("note is nil")}
	}
	var diags []brain.MarkdownDiagnostic
	for _, name := range requiredSections {
		if _, ok := note.Section(name); !ok {
			diags = append(diags, diagf("missing required section %q", name))
		}
	}
	return diags
}

func requireValue(name, value string) []brain.MarkdownDiagnostic {
	if strings.TrimSpace(value) == "" {
		return []brain.MarkdownDiagnostic{diagf("%s is required", name)}
	}
	return nil
}

func set(values ...string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[value] = true
	}
	return out
}

func diagf(format string, args ...any) brain.MarkdownDiagnostic {
	return brain.MarkdownDiagnostic{Message: fmt.Sprintf(format, args...)}
}
