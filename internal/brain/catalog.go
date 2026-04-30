package brain

import (
	"errors"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

type VaultSummary struct {
	Sphere    Sphere   `json:"sphere"`
	Root      string   `json:"root"`
	Label     string   `json:"label,omitempty"`
	Hub       bool     `json:"hub,omitempty"`
	BrainRoot string   `json:"brain_root,omitempty"`
	Exclude   []string `json:"exclude,omitempty"`
}

type NoteSnapshot struct {
	Source           ResolvedPath         `json:"source"`
	Kind             string               `json:"kind,omitempty"`
	Diagnostics      []MarkdownDiagnostic `json:"diagnostics,omitempty"`
	Count            int                  `json:"count,omitempty"`
	Valid            bool                 `json:"valid,omitempty"`
	Body             string               `json:"-"`
	Note             *MarkdownNote        `json:"-"`
	SectionTitles    []string             `json:"section_titles,omitempty"`
	RequiredSections []string             `json:"required_sections,omitempty"`
}

type FolderAuditRecord struct {
	Source      ResolvedPath         `json:"source"`
	Folder      FolderNote           `json:"folder"`
	Diagnostics []MarkdownDiagnostic `json:"diagnostics,omitempty"`
	Count       int                  `json:"count,omitempty"`
	Valid       bool                 `json:"valid,omitempty"`
}

type EntityCandidate struct {
	Name    string       `json:"name"`
	Kind    string       `json:"kind"`
	Source  ResolvedPath `json:"source"`
	Aliases []string     `json:"aliases,omitempty"`
	Labels  []string     `json:"labels,omitempty"`
	Why     string       `json:"why,omitempty"`
}

func ListVaults(cfg *Config) []VaultSummary {
	if cfg == nil {
		return nil
	}
	out := make([]VaultSummary, 0, len(cfg.Vaults))
	for _, vault := range cfg.Vaults {
		out = append(out, VaultSummary{
			Sphere:    vault.Sphere,
			Root:      vault.Root,
			Label:     vault.Label,
			Hub:       vault.Hub,
			BrainRoot: vault.BrainRoot(),
			Exclude:   append([]string(nil), vault.Exclude...),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Sphere != out[j].Sphere {
			return out[i].Sphere < out[j].Sphere
		}
		return out[i].Root < out[j].Root
	})
	return out
}

func WalkVaultNotes(cfg *Config, sphere Sphere, fn func(NoteSnapshot) error) error {
	vault, err := cfgVault(cfg, sphere)
	if err != nil {
		return err
	}
	return filepath.WalkDir(vault.BrainRoot(), func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}
		rel, err := filepath.Rel(vault.Root, path)
		if err != nil {
			return err
		}
		source, body, err := ReadNoteFile(cfg, sphere, filepath.ToSlash(rel))
		if err != nil {
			if kind := KindOf(err); kind == ErrorExcludedPath || kind == ErrorOutOfVault {
				return nil
			}
			return err
		}
		note, diags := ParseMarkdownNote(string(body), MarkdownParseOptions{})
		snapshot := NoteSnapshot{
			Source:           source,
			Kind:             NoteKind(note),
			Diagnostics:      diags,
			Count:            len(diags),
			Valid:            len(diags) == 0,
			Body:             string(body),
			Note:             note,
			SectionTitles:    sectionTitles(note),
			RequiredSections: nil,
		}
		return fn(snapshot)
	})
}

func NoteKind(note *MarkdownNote) string {
	if note == nil {
		return ""
	}
	node, ok := note.FrontMatterField("kind")
	if !ok {
		return ""
	}
	kind := strings.ToLower(strings.TrimSpace(node.Value))
	if kind == "gtd" {
		return "commitment"
	}
	return kind
}

func AuditFolderVault(cfg *Config, sphere Sphere) ([]FolderAuditRecord, error) {
	var out []FolderAuditRecord
	if err := WalkVaultNotes(cfg, sphere, func(snap NoteSnapshot) error {
		if snap.Kind != "folder" {
			return nil
		}
		parsed, diags := ValidateFolderNote(snap.Body, LinkValidationContext{Config: cfg, Sphere: snap.Source.Sphere, Path: snap.Source.Path})
		out = append(out, FolderAuditRecord{
			Source:      snap.Source,
			Folder:      parsed,
			Diagnostics: diags,
			Count:       len(diags),
			Valid:       len(diags) == 0,
		})
		return nil
	}); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Source.Rel < out[j].Source.Rel })
	return out, nil
}

func EntityCandidates(cfg *Config, sphere Sphere) ([]EntityCandidate, error) {
	seen := map[string]struct{}{}
	var out []EntityCandidate
	if err := WalkVaultNotes(cfg, sphere, func(snap NoteSnapshot) error {
		if !snap.Valid {
			return nil
		}
		switch snap.Kind {
		case "folder":
			parsed, _ := ParseFolderNote(snap.Body)
			addCandidate(&out, seen, entityCandidate{
				name:   parsed.SourceFolder,
				kind:   "folder",
				source: snap.Source,
				labels: dedupeStrings([]string{parsed.Status, "folder", "source_folder"}),
				why:    "source_folder",
			})
			for _, name := range parsed.Projects {
				addCandidate(&out, seen, entityCandidate{name: name, kind: "project", source: snap.Source, labels: dedupeStrings([]string{"folder", parsed.SourceFolder}), why: "projects"})
			}
			for _, name := range parsed.People {
				addCandidate(&out, seen, entityCandidate{name: name, kind: "human", source: snap.Source, labels: dedupeStrings([]string{"folder", parsed.SourceFolder}), why: "people"})
			}
			for _, name := range parsed.Institutions {
				addCandidate(&out, seen, entityCandidate{name: name, kind: "institution", source: snap.Source, labels: dedupeStrings([]string{"folder", parsed.SourceFolder}), why: "institutions"})
			}
			for _, name := range parsed.Topics {
				addCandidate(&out, seen, entityCandidate{name: name, kind: "topic", source: snap.Source, labels: dedupeStrings([]string{"folder", parsed.SourceFolder}), why: "topics"})
			}
		case "glossary":
			parsed, _ := ParseGlossaryNote(snap.Body)
			addCandidate(&out, seen, entityCandidate{
				name:    parsed.DisplayName,
				kind:    "term",
				source:  snap.Source,
				aliases: dedupeStrings(append([]string(nil), parsed.Aliases...)),
				labels:  dedupeStrings([]string{"glossary", parsed.Sphere}),
				why:     "display_name",
			})
		case "attention", "human", "project", "topic", "institution":
			parsed, _ := ParseAttentionFields(snap.Body)
			name := firstHeadingTitle(snap.Note)
			if name == "" {
				name = strings.TrimSuffix(filepath.Base(snap.Source.Rel), filepath.Ext(snap.Source.Rel))
			}
			if name != "" {
				addCandidate(&out, seen, entityCandidate{
					name:   name,
					kind:   snap.Kind,
					source: snap.Source,
					labels: dedupeStrings([]string{parsed.Status, parsed.Focus, parsed.Cadence, boolLabel("strategic", parsed.Strategic)}),
					why:    "heading",
				})
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Source.Rel < out[j].Source.Rel
	})
	return out, nil
}

func cfgVault(cfg *Config, sphere Sphere) (Vault, error) {
	if cfg == nil {
		return Vault{}, &PathError{Kind: ErrorInvalidConfig, Sphere: sphere, Err: errors.New("config is nil")}
	}
	vault, ok := cfg.Vault(sphere)
	if !ok {
		return Vault{}, &PathError{Kind: ErrorUnknownVault, Sphere: normalizeSphere(sphere)}
	}
	return vault, nil
}

type entityCandidate struct {
	name    string
	kind    string
	source  ResolvedPath
	aliases []string
	labels  []string
	why     string
}

func addCandidate(out *[]EntityCandidate, seen map[string]struct{}, candidate entityCandidate) {
	name := strings.TrimSpace(candidate.name)
	kind := strings.TrimSpace(candidate.kind)
	if name == "" || kind == "" {
		return
	}
	key := kind + "\x00" + strings.ToLower(name) + "\x00" + candidate.source.Rel
	if _, ok := seen[key]; ok {
		return
	}
	seen[key] = struct{}{}
	*out = append(*out, EntityCandidate{
		Name:    name,
		Kind:    kind,
		Source:  candidate.source,
		Aliases: dedupeStrings(candidate.aliases),
		Labels:  dedupeStrings(candidate.labels),
		Why:     candidate.why,
	})
}

func dedupeStrings(values []string) []string {
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

func firstHeadingTitle(note *MarkdownNote) string {
	if note == nil {
		return ""
	}
	for _, section := range note.Sections() {
		if section.Level == 1 && strings.TrimSpace(section.Name) != "" {
			return strings.TrimSpace(section.Name)
		}
	}
	for _, section := range note.Sections() {
		if strings.TrimSpace(section.Name) != "" {
			return strings.TrimSpace(section.Name)
		}
	}
	return ""
}

func sectionTitles(note *MarkdownNote) []string {
	if note == nil {
		return nil
	}
	var titles []string
	for _, section := range note.Sections() {
		if section.Level > 0 && strings.TrimSpace(section.Name) != "" {
			titles = append(titles, strings.TrimSpace(section.Name))
		}
	}
	return titles
}

func boolLabel(name string, value *bool) string {
	if value == nil {
		return ""
	}
	if *value {
		return name + ":true"
	}
	return name + ":false"
}
