package brain

import (
	"net/url"
	"path/filepath"
	"strings"
)

// IntegrityReport summarises validation diagnostics across a vault. It is the
// programmatic form of `sloptools brain vault validate` and is the basis of
// the post-apply hard gate guarding move/cleanup/consolidate/dream-prune.
type IntegrityReport struct {
	Sphere       Sphere   `json:"sphere"`
	Notes        int      `json:"notes"`
	TotalIssues  int      `json:"total_issues"`
	BrokenLinks  int      `json:"broken_links"`
	LinkExamples []string `json:"link_examples,omitempty"`
}

// brokenLinkExampleLimit caps the number of broken-link strings carried back
// in the report so callers can print actionable context without spamming
// stderr after a large regression.
const brokenLinkExampleLimit = 10

// IntegrityScan walks every brain note in the vault, runs the same per-kind
// validation as `brain vault validate`, and additionally checks every
// wikilink and relative Markdown link for an on-disk target. The two failure
// modes have different cures: TotalIssues catches schema/required-section
// drift, BrokenLinks catches dangling references that move/consolidate runs
// introduce when link rewriting goes wrong.
func IntegrityScan(cfg *Config, sphere Sphere) (IntegrityReport, error) {
	vault, err := cfgVault(cfg, sphere)
	if err != nil {
		return IntegrityReport{}, err
	}
	report := IntegrityReport{Sphere: sphere}
	walkErr := WalkVaultNotes(cfg, sphere, func(snap NoteSnapshot) error {
		report.Notes++
		report.TotalIssues += len(collectIntegrityDiagnostics(cfg, snap))
		broken := scanBrokenLinks(vault, snap)
		for _, target := range broken {
			report.BrokenLinks++
			if len(report.LinkExamples) < brokenLinkExampleLimit {
				report.LinkExamples = append(report.LinkExamples,
					filepath.ToSlash(snap.Source.Rel)+" -> "+target)
			}
		}
		return nil
	})
	if walkErr != nil {
		return IntegrityReport{}, walkErr
	}
	return report, nil
}

// scanBrokenLinks returns the link target strings inside snap.Body whose
// resolved on-disk path does not exist. Wikilink and relative Markdown link
// resolution mirrors the rules in cleanup.go and typed_notes.go.
func scanBrokenLinks(vault Vault, snap NoteSnapshot) []string {
	var broken []string
	noteDir := filepath.Dir(snap.Source.Path)
	for _, raw := range extractWikilinks(snap.Body) {
		target := normalizeWikilinkTarget(raw)
		if target == "" {
			continue
		}
		if !wikilinkTargetExists(vault, target) {
			broken = append(broken, "[["+raw+"]]")
		}
	}
	for _, raw := range extractMarkdownLinks(snap.Body) {
		path, ok := resolveRelativeMarkdownLink(vault, noteDir, raw)
		if !ok {
			continue
		}
		if !integrityPathExists(path) {
			rel, err := filepath.Rel(vault.Root, path)
			if err != nil {
				rel = path
			}
			broken = append(broken, "("+filepath.ToSlash(rel)+")")
		}
	}
	return broken
}

func wikilinkTargetExists(vault Vault, target string) bool {
	candidate := filepath.Join(vault.BrainRoot(), filepath.FromSlash(target)+".md")
	return pathExists(candidate)
}

// integrityPathExists is a thin wrapper used only by the broken-link scanner;
// it exists so the os.Lstat-vs-os.Stat choice is documented next to its use.
// We use the existing pathExists (os.Stat, follows symlinks) because vaults
// may legitimately contain symlinked notes whose targets we want to count as
// resolvable.
func integrityPathExists(path string) bool { return pathExists(path) }

// resolveRelativeMarkdownLink resolves the target half of a Markdown link to
// an absolute on-disk path inside the vault. URL-scheme targets and links
// that escape the vault return ok=false because broken external URLs are not
// what the gate is responsible for.
func resolveRelativeMarkdownLink(vault Vault, noteDir, raw string) (string, bool) {
	target := strings.TrimSpace(raw)
	if target == "" || hasURLScheme(target) {
		return "", false
	}
	if hash := strings.Index(target, "#"); hash >= 0 {
		target = target[:hash]
	}
	if target == "" {
		return "", false
	}
	if unescaped, err := url.PathUnescape(target); err == nil {
		target = unescaped
	}
	target = filepath.FromSlash(target)
	abs := target
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(noteDir, target)
	}
	abs = filepath.Clean(abs)
	if !isWithin(vault.Root, abs) {
		return "", false
	}
	return abs, true
}

// collectIntegrityDiagnostics dispatches to the kind-specific validator. It
// mirrors the cmd-side inspector but keeps the result a flat diagnostic slice
// the gate can count without caring about note shape.
func collectIntegrityDiagnostics(cfg *Config, snap NoteSnapshot) []MarkdownDiagnostic {
	ctx := LinkValidationContext{Config: cfg, Sphere: snap.Source.Sphere, Path: snap.Source.Path}
	switch snap.Kind {
	case "folder":
		_, diags := ValidateFolderNote(snap.Body, ctx)
		return diags
	case "glossary":
		_, diags := ValidateGlossaryNote(snap.Body, ctx)
		return diags
	}
	return snap.Diagnostics
}

// isBrokenLinkDiagnostic matches diagnostics emitted by validateNoteLinks /
// validateCanonicalTopic. It looks for the substrings the validators use so
// the gate stays robust if new validators reuse the same phrasing.
func isBrokenLinkDiagnostic(diag MarkdownDiagnostic) bool {
	msg := strings.ToLower(diag.Message)
	if strings.Contains(msg, "is not resolvable") {
		return true
	}
	if strings.Contains(msg, "canonical_topic target is not resolvable") {
		return true
	}
	return false
}

// IntegrityRegression captures the broken-link delta a hard gate observed
// across an apply step. Returning a structured value (rather than just a
// boolean) lets callers print an actionable diff and terminate the run with
// non-zero status without rolling back the partial state we already wrote.
type IntegrityRegression struct {
	Before         IntegrityReport `json:"before"`
	After          IntegrityReport `json:"after"`
	NewBrokenLinks int             `json:"new_broken_links"`
	NewIssues      int             `json:"new_issues"`
}

// IsRegression reports whether the after-snapshot is strictly worse than the
// before-snapshot. The gate fires only on regressions because a vault that
// already had broken links pre-apply must not be held responsible for them.
func (r IntegrityRegression) IsRegression() bool {
	return r.NewBrokenLinks > 0 || r.NewIssues > 0
}

// CompareIntegrity computes the delta. It only flags regressions: if the
// after-state is cleaner the result has zero deltas even when before/after
// counts differ.
func CompareIntegrity(before, after IntegrityReport) IntegrityRegression {
	reg := IntegrityRegression{Before: before, After: after}
	if after.BrokenLinks > before.BrokenLinks {
		reg.NewBrokenLinks = after.BrokenLinks - before.BrokenLinks
	}
	if after.TotalIssues > before.TotalIssues {
		reg.NewIssues = after.TotalIssues - before.TotalIssues
	}
	return reg
}
