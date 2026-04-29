package gtd

import (
	"fmt"
	"strings"

	"github.com/sloppy-org/sloptools/internal/brain"
)

type Commitment struct {
	Title          string          `json:"title,omitempty" yaml:"title,omitempty"`
	Kind           string          `json:"kind,omitempty" yaml:"kind,omitempty"`
	Sphere         string          `json:"sphere,omitempty" yaml:"sphere,omitempty"`
	Status         string          `json:"status,omitempty" yaml:"status,omitempty"`
	Outcome        string          `json:"outcome,omitempty" yaml:"outcome,omitempty"`
	NextAction     string          `json:"next_action,omitempty" yaml:"next_action,omitempty"`
	Context        string          `json:"context,omitempty" yaml:"context,omitempty"`
	FollowUp       string          `json:"follow_up,omitempty" yaml:"follow_up,omitempty"`
	Due            string          `json:"due,omitempty" yaml:"due,omitempty"`
	Actor          string          `json:"actor,omitempty" yaml:"actor,omitempty"`
	WaitingFor     string          `json:"waiting_for,omitempty" yaml:"waiting_for,omitempty"`
	Project        string          `json:"project,omitempty" yaml:"project,omitempty"`
	LastEvidenceAt string          `json:"last_evidence_at,omitempty" yaml:"last_evidence_at,omitempty"`
	ReviewState    string          `json:"review_state,omitempty" yaml:"review_state,omitempty"`
	People         []string        `json:"people,omitempty" yaml:"people,omitempty"`
	Labels         []string        `json:"labels,omitempty" yaml:"labels,omitempty"`
	SourceBindings []SourceBinding `json:"source_bindings,omitempty" yaml:"source_bindings,omitempty"`
	LocalOverlay   LocalOverlay    `json:"local_overlay,omitempty" yaml:"local_overlay,omitempty"`
	Dedup          DedupState      `json:"dedup,omitempty" yaml:"dedup,omitempty"`
	LegacySources  []string        `json:"legacy_sources,omitempty" yaml:"legacy_sources,omitempty"`
}

type SourceBinding struct {
	Provider         string          `json:"provider" yaml:"provider"`
	Ref              string          `json:"ref" yaml:"ref"`
	Location         BindingLocation `json:"location,omitempty" yaml:"location,omitempty"`
	URL              string          `json:"url,omitempty" yaml:"url,omitempty"`
	Writeable        bool            `json:"writeable,omitempty" yaml:"writeable,omitempty"`
	AuthoritativeFor []string        `json:"authoritative_for,omitempty" yaml:"authoritative_for,omitempty"`
	Summary          string          `json:"summary,omitempty" yaml:"summary,omitempty"`
	CreatedAt        string          `json:"created_at,omitempty" yaml:"created_at,omitempty"`
	UpdatedAt        string          `json:"updated_at,omitempty" yaml:"updated_at,omitempty"`
}

type BindingLocation struct {
	Path   string `json:"path,omitempty" yaml:"path,omitempty"`
	Anchor string `json:"anchor,omitempty" yaml:"anchor,omitempty"`
}

type LocalOverlay struct {
	Status    string `json:"status,omitempty" yaml:"status,omitempty"`
	FollowUp  string `json:"follow_up,omitempty" yaml:"follow_up,omitempty"`
	Due       string `json:"due,omitempty" yaml:"due,omitempty"`
	Actor     string `json:"actor,omitempty" yaml:"actor,omitempty"`
	ClosedAt  string `json:"closed_at,omitempty" yaml:"closed_at,omitempty"`
	ClosedVia string `json:"closed_via,omitempty" yaml:"closed_via,omitempty"`
}

type DedupState struct {
	EquivalentTo  string              `json:"equivalent_to,omitempty" yaml:"equivalent_to,omitempty"`
	NotDuplicates []string            `json:"not_duplicates,omitempty" yaml:"not_duplicates,omitempty"`
	Deferred      []string            `json:"deferred,omitempty" yaml:"deferred,omitempty"`
	MergeHistory  []DedupHistoryEntry `json:"merge_history,omitempty" yaml:"merge_history,omitempty"`
}

type DedupHistoryEntry struct {
	ID         string   `json:"id,omitempty" yaml:"id,omitempty"`
	MergedFrom []string `json:"merged_from,omitempty" yaml:"merged_from,omitempty"`
	DecidedAt  string   `json:"decided_at,omitempty" yaml:"decided_at,omitempty"`
}

func ParseCommitmentMarkdown(src string) (*Commitment, *brain.MarkdownNote, []brain.MarkdownDiagnostic) {
	note, diags := brain.ParseMarkdownNote(src, brain.MarkdownParseOptions{})
	commitment := &Commitment{}
	if node, ok := note.FrontMatterField("kind"); ok {
		commitment.Kind = strings.TrimSpace(node.Value)
	}
	if node, ok := note.FrontMatterField("title"); ok {
		commitment.Title = strings.TrimSpace(node.Value)
	}
	if node, ok := note.FrontMatterField("sphere"); ok {
		commitment.Sphere = strings.TrimSpace(node.Value)
	}
	if node, ok := note.FrontMatterField("status"); ok {
		commitment.Status = strings.TrimSpace(node.Value)
	}
	if node, ok := note.FrontMatterField("outcome"); ok {
		commitment.Outcome = strings.TrimSpace(node.Value)
	}
	if node, ok := note.FrontMatterField("next_action"); ok {
		commitment.NextAction = strings.TrimSpace(node.Value)
	}
	if node, ok := note.FrontMatterField("context"); ok {
		commitment.Context = strings.TrimSpace(node.Value)
	}
	if node, ok := note.FrontMatterField("follow_up"); ok {
		commitment.FollowUp = strings.TrimSpace(node.Value)
	}
	if node, ok := note.FrontMatterField("due"); ok {
		commitment.Due = strings.TrimSpace(node.Value)
	}
	if node, ok := note.FrontMatterField("actor"); ok {
		commitment.Actor = strings.TrimSpace(node.Value)
	}
	if node, ok := note.FrontMatterField("waiting_for"); ok {
		commitment.WaitingFor = strings.TrimSpace(node.Value)
	}
	if node, ok := note.FrontMatterField("project"); ok {
		commitment.Project = strings.TrimSpace(node.Value)
	}
	if node, ok := note.FrontMatterField("last_evidence_at"); ok {
		commitment.LastEvidenceAt = strings.TrimSpace(node.Value)
	}
	if node, ok := note.FrontMatterField("review_state"); ok {
		commitment.ReviewState = strings.TrimSpace(node.Value)
	}
	commitment.People = stringSliceField(note, "people")
	commitment.Labels = stringSliceField(note, "labels")
	commitment.SourceBindings, diags = appendBindingDiagnostics(diags, note)
	commitment.LocalOverlay, diags = appendOverlayDiagnostics(diags, note)
	commitment.Dedup, diags = appendDedupDiagnostics(diags, note)
	commitment.LegacySources = stringSliceField(note, "source_refs")
	if len(commitment.SourceBindings) == 0 {
		commitment.SourceBindings = bindingsFromLegacyRefs(commitment.LegacySources)
	}
	return commitment, note, diags
}

func ApplyCommitment(note *brain.MarkdownNote, commitment Commitment) error {
	if note == nil {
		return fmt.Errorf("note is nil")
	}
	if err := note.SetFrontMatterField("source_bindings", commitment.SourceBindings); err != nil {
		return err
	}
	if !commitment.LocalOverlay.Empty() {
		if err := note.SetFrontMatterField("local_overlay", commitment.LocalOverlay); err != nil {
			return err
		}
	}
	if !commitment.Dedup.Empty() {
		if err := note.SetFrontMatterField("dedup", commitment.Dedup); err != nil {
			return err
		}
	}
	return nil
}

func (b SourceBinding) StableID() string {
	provider := normalizeBindingPart(b.Provider)
	ref := strings.TrimSpace(b.Ref)
	if provider == "" || ref == "" {
		return ""
	}
	return provider + ":" + ref
}

func (c Commitment) DedupHints() []string {
	seen := map[string]bool{}
	var out []string
	for _, binding := range c.SourceBindings {
		if id := binding.StableID(); id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	title := strings.ToLower(strings.Join(strings.Fields(c.Title), " "))
	if title != "" && !seen[title] {
		out = append(out, title)
	}
	return out
}

func (o LocalOverlay) Empty() bool {
	return o.Status == "" && o.FollowUp == "" && o.Due == "" && o.Actor == "" && o.ClosedAt == "" && o.ClosedVia == ""
}

func (d DedupState) Empty() bool {
	return d.EquivalentTo == "" && len(d.NotDuplicates) == 0 && len(d.Deferred) == 0 && len(d.MergeHistory) == 0
}

func appendBindingDiagnostics(diags []brain.MarkdownDiagnostic, note *brain.MarkdownNote) ([]SourceBinding, []brain.MarkdownDiagnostic) {
	node, ok := note.FrontMatterField("source_bindings")
	if !ok {
		return nil, diags
	}
	var bindings []SourceBinding
	if err := node.Decode(&bindings); err != nil {
		return nil, append(diags, brain.MarkdownDiagnostic{Message: "invalid source_bindings: " + err.Error()})
	}
	for i := range bindings {
		bindings[i].Provider = normalizeBindingPart(bindings[i].Provider)
		bindings[i].Ref = strings.TrimSpace(bindings[i].Ref)
	}
	return bindings, diags
}

func appendOverlayDiagnostics(diags []brain.MarkdownDiagnostic, note *brain.MarkdownNote) (LocalOverlay, []brain.MarkdownDiagnostic) {
	node, ok := note.FrontMatterField("local_overlay")
	if !ok {
		return LocalOverlay{}, diags
	}
	var overlay LocalOverlay
	if err := node.Decode(&overlay); err != nil {
		return LocalOverlay{}, append(diags, brain.MarkdownDiagnostic{Message: "invalid local_overlay: " + err.Error()})
	}
	return overlay, diags
}

func appendDedupDiagnostics(diags []brain.MarkdownDiagnostic, note *brain.MarkdownNote) (DedupState, []brain.MarkdownDiagnostic) {
	node, ok := note.FrontMatterField("dedup")
	if !ok {
		return DedupState{}, diags
	}
	var dedup DedupState
	if err := node.Decode(&dedup); err != nil {
		return DedupState{}, append(diags, brain.MarkdownDiagnostic{Message: "invalid dedup: " + err.Error()})
	}
	dedup.EquivalentTo = strings.TrimSpace(dedup.EquivalentTo)
	dedup.NotDuplicates = compactStrings(dedup.NotDuplicates)
	dedup.Deferred = compactStrings(dedup.Deferred)
	return dedup, diags
}

func stringSliceField(note *brain.MarkdownNote, name string) []string {
	node, ok := note.FrontMatterField(name)
	if !ok {
		return nil
	}
	var out []string
	if err := node.Decode(&out); err != nil {
		if strings.TrimSpace(node.Value) != "" {
			return []string{strings.TrimSpace(node.Value)}
		}
		return nil
	}
	for i := range out {
		out[i] = strings.TrimSpace(out[i])
	}
	return out
}

func bindingsFromLegacyRefs(refs []string) []SourceBinding {
	bindings := make([]SourceBinding, 0, len(refs))
	for _, raw := range refs {
		provider, ref := splitLegacyRef(raw)
		if ref == "" {
			continue
		}
		bindings = append(bindings, SourceBinding{Provider: provider, Ref: ref})
	}
	return bindings
}

func splitLegacyRef(raw string) (string, string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	provider := "manual"
	ref := raw
	if i := strings.IndexByte(raw, ':'); i > 0 {
		provider = raw[:i]
		ref = raw[i+1:]
	}
	return normalizeBindingPart(provider), strings.TrimSpace(ref)
}

func normalizeBindingPart(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func compactStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		clean := strings.TrimSpace(value)
		if clean != "" && !seen[clean] {
			seen[clean] = true
			out = append(out, clean)
		}
	}
	return out
}
