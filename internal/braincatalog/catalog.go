package braincatalog

import (
	"strings"

	"github.com/sloppy-org/sloptools/internal/brain"
	braingtd "github.com/sloppy-org/sloptools/internal/brain/gtd"
)

type GTDRecord struct {
	Source      brain.ResolvedPath         `json:"source"`
	Commitment  braingtd.Commitment        `json:"commitment"`
	Diagnostics []brain.MarkdownDiagnostic `json:"diagnostics,omitempty"`
	Count       int                        `json:"count,omitempty"`
	Valid       bool                       `json:"valid,omitempty"`
}

type GTDListFilter struct {
	Status  string `json:"status,omitempty"`
	Person  string `json:"person,omitempty"`
	Project string `json:"project,omitempty"`
	Source  string `json:"source,omitempty"`
	Limit   int    `json:"limit,omitempty"`
}

type GTDListItem struct {
	Source     brain.ResolvedPath `json:"source"`
	Path       string             `json:"path"`
	Title      string             `json:"title"`
	Status     string             `json:"status"`
	Project    string             `json:"project,omitempty"`
	Actor      string             `json:"actor,omitempty"`
	WaitingFor string             `json:"waiting_for,omitempty"`
	Due        string             `json:"due,omitempty"`
	FollowUp   string             `json:"follow_up,omitempty"`
	Labels     []string           `json:"labels,omitempty"`
	Bindings   []string           `json:"bindings,omitempty"`
	Why        string             `json:"why,omitempty"`
}

func ParseGTDVault(cfg *brain.Config, sphere brain.Sphere) ([]GTDRecord, error) {
	var out []GTDRecord
	if err := brain.WalkVaultNotes(cfg, sphere, func(snap brain.NoteSnapshot) error {
		if snap.Kind != "commitment" {
			return nil
		}
		commitment, _, diags := braingtd.ParseCommitmentMarkdown(snap.Body)
		out = append(out, GTDRecord{
			Source:      snap.Source,
			Commitment:  *commitment,
			Diagnostics: diags,
			Count:       len(diags),
			Valid:       len(diags) == 0,
		})
		return nil
	}); err != nil {
		return nil, err
	}
	return out, nil
}

func ListGTDVault(cfg *brain.Config, sphere brain.Sphere, filter GTDListFilter) ([]GTDListItem, error) {
	records, err := ParseGTDVault(cfg, sphere)
	if err != nil {
		return nil, err
	}
	out := make([]GTDListItem, 0, len(records))
	for _, record := range records {
		item := gtdListItemFromRecord(record)
		if !gtdListMatches(item, filter) {
			continue
		}
		out = append(out, item)
		if filter.Limit > 0 && len(out) >= filter.Limit {
			break
		}
	}
	return out, nil
}

func gtdListItemFromRecord(record GTDRecord) GTDListItem {
	commitment := record.Commitment
	title := strings.TrimSpace(commitment.Outcome)
	if title == "" {
		title = strings.TrimSpace(commitment.Title)
	}
	if title == "" {
		title = record.Source.Rel
	}
	labels := compactStrings(commitment.Labels)
	bindings := make([]string, 0, len(commitment.SourceBindings))
	for _, binding := range commitment.SourceBindings {
		if id := binding.StableID(); id != "" {
			bindings = append(bindings, id)
		}
	}
	return GTDListItem{
		Source:     record.Source,
		Path:       record.Source.Rel,
		Title:      title,
		Status:     gtdListStatus(commitment),
		Project:    strings.TrimSpace(commitment.Project),
		Actor:      strings.TrimSpace(commitment.Actor),
		WaitingFor: strings.TrimSpace(commitment.WaitingFor),
		Due:        strings.TrimSpace(commitment.Due),
		FollowUp:   strings.TrimSpace(commitment.FollowUp),
		Labels:     labels,
		Bindings:   compactStrings(bindings),
	}
}

func gtdListStatus(commitment braingtd.Commitment) string {
	status := strings.TrimSpace(commitment.LocalOverlay.Status)
	if status != "" {
		return strings.ToLower(status)
	}
	return strings.ToLower(strings.TrimSpace(commitment.Status))
}

func gtdListMatches(item GTDListItem, filter GTDListFilter) bool {
	if strings.TrimSpace(filter.Status) != "" && !strings.EqualFold(item.Status, strings.TrimSpace(filter.Status)) {
		return false
	}
	if strings.TrimSpace(filter.Person) != "" {
		person := strings.TrimSpace(filter.Person)
		if !containsFold([]string{item.Actor, item.WaitingFor}, person) {
			return false
		}
	}
	if strings.TrimSpace(filter.Project) != "" && !strings.EqualFold(item.Project, strings.TrimSpace(filter.Project)) {
		return false
	}
	if strings.TrimSpace(filter.Source) != "" {
		source := strings.TrimSpace(filter.Source)
		if !strings.Contains(strings.ToLower(item.Path), strings.ToLower(source)) && !containsFold(item.Bindings, source) {
			return false
		}
	}
	return true
}

func compactStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" || seen[strings.ToLower(value)] {
			continue
		}
		seen[strings.ToLower(value)] = true
		out = append(out, value)
	}
	return out
}

func containsFold(values []string, want string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(want)) {
			return true
		}
	}
	return false
}
