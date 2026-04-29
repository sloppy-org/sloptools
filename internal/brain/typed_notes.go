package brain

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

type FolderNote struct {
	Kind          string   `json:"kind"`
	Vault         string   `json:"vault,omitempty"`
	Sphere        string   `json:"sphere,omitempty"`
	SourceFolder  string   `json:"source_folder,omitempty"`
	Status        string   `json:"status,omitempty"`
	Projects      []string `json:"projects,omitempty"`
	People        []string `json:"people,omitempty"`
	Institutions  []string `json:"institutions,omitempty"`
	Topics        []string `json:"topics,omitempty"`
	MarkdownLinks []string `json:"markdown_links,omitempty"`
	Wikilinks     []string `json:"wikilinks,omitempty"`
}

type GlossaryNote struct {
	Kind             string   `json:"kind"`
	DisplayName      string   `json:"display_name,omitempty"`
	Aliases          []string `json:"aliases,omitempty"`
	Sphere           string   `json:"sphere,omitempty"`
	CanonicalTopic   string   `json:"canonical_topic,omitempty"`
	DoNotConfuseWith []string `json:"do_not_confuse_with,omitempty"`
	Ambiguous        bool     `json:"ambiguous,omitempty"`
	Definition       string   `json:"definition,omitempty"`
	MarkdownLinks    []string `json:"markdown_links,omitempty"`
	Wikilinks        []string `json:"wikilinks,omitempty"`
}

type AttentionFields struct {
	Kind      string `json:"kind,omitempty"`
	Status    string `json:"status,omitempty"`
	Focus     string `json:"focus,omitempty"`
	Cadence   string `json:"cadence,omitempty"`
	Strategic *bool  `json:"strategic,omitempty"`
	Enjoyment string `json:"enjoyment,omitempty"`
}

type LinkValidationContext struct {
	Config *Config
	Sphere Sphere
	Path   string
}

var (
	noteWikilinkPattern     = regexp.MustCompile(`\[\[([^\]\n]+)\]\]`)
	noteMarkdownLinkPattern = regexp.MustCompile(`\[[^\]\n]*\]\(([^)\s]+)(?:\s+[^)]*)?\)`)
	folderStatuses          = stringSet("active", "stale", "frozen", "unknown")
	attentionFocus          = stringSet("core", "active", "watch", "parked")
	attentionCadence        = stringSet("daily", "weekly", "monthly", "quarterly", "annual", "none")
	attentionKinds          = stringSet("human", "project", "topic", "institution")
)

func ParseFolderNote(src string) (FolderNote, []MarkdownDiagnostic) {
	note, diags := ParseMarkdownNote(src, MarkdownParseOptions{})
	parsed := FolderNote{
		Kind:          scalarField(note, "kind"),
		Vault:         scalarField(note, "vault"),
		Sphere:        scalarField(note, "sphere"),
		SourceFolder:  scalarField(note, "source_folder"),
		Status:        scalarField(note, "status"),
		Projects:      listField(note, "projects"),
		People:        listField(note, "people"),
		Institutions:  listField(note, "institutions"),
		Topics:        listField(note, "topics"),
		MarkdownLinks: extractMarkdownLinks(src),
		Wikilinks:     extractWikilinks(src),
	}
	return parsed, diags
}

func ValidateFolderNote(src string, ctx LinkValidationContext) (FolderNote, []MarkdownDiagnostic) {
	parsed, diags := ParseFolderNote(src)
	_, noteDiags := ParseMarkdownNote(src, MarkdownParseOptions{RequiredSections: []string{
		"Summary", "Key Facts", "Important Files", "Related Folders", "Related Notes", "Notes", "Open Questions",
	}})
	diags = append(diags, noteDiags...)
	diags = append(diags, requiredScalar("kind", parsed.Kind)...)
	if parsed.Kind != "" && parsed.Kind != "folder" {
		diags = append(diags, noteDiag("kind must be folder"))
	}
	diags = append(diags, requiredScalar("source_folder", parsed.SourceFolder)...)
	diags = append(diags, requiredScalar("status", parsed.Status)...)
	if parsed.Status != "" && !folderStatuses[parsed.Status] {
		diags = append(diags, noteDiag("folder status must be active, stale, frozen, or unknown"))
	}
	diags = append(diags, validateNoteLinks(parsed.MarkdownLinks, parsed.Wikilinks, ctx)...)
	return parsed, diags
}

func ParseGlossaryNote(src string) (GlossaryNote, []MarkdownDiagnostic) {
	note, diags := ParseMarkdownNote(src, MarkdownParseOptions{})
	sections := note.Sections()
	parsed := GlossaryNote{
		Kind:             scalarField(note, "kind"),
		DisplayName:      scalarField(note, "display_name"),
		Aliases:          listField(note, "aliases"),
		Sphere:           scalarField(note, "sphere"),
		CanonicalTopic:   scalarField(note, "canonical_topic"),
		DoNotConfuseWith: listField(note, "do_not_confuse_with"),
		Ambiguous:        boolField(note, "ambiguous"),
		MarkdownLinks:    extractMarkdownLinks(src),
		Wikilinks:        extractWikilinks(src),
	}
	if parsed.DisplayName != "" && !containsFold(parsed.Aliases, parsed.DisplayName) {
		parsed.Aliases = append([]string{parsed.DisplayName}, parsed.Aliases...)
	}
	parsed.Definition = firstSectionParagraph(sections, "Definition", "Allgemein", "Summary")
	return parsed, diags
}

func ValidateGlossaryNote(src string, ctx LinkValidationContext) (GlossaryNote, []MarkdownDiagnostic) {
	parsed, diags := ParseGlossaryNote(src)
	if parsed.Kind != "glossary" {
		diags = append(diags, noteDiag("kind must be glossary"))
	}
	diags = append(diags, requiredScalar("display_name", parsed.DisplayName)...)
	if len(parsed.Aliases) == 0 {
		diags = append(diags, noteDiag("aliases must not be empty"))
	}
	if isAcronym(parsed.DisplayName) && !containsFold(parsed.Aliases, parsed.DisplayName) {
		diags = append(diags, noteDiag("acronym display_name must appear in aliases"))
	}
	diags = append(diags, validateCanonicalTopic(parsed.CanonicalTopic, ctx)...)
	diags = append(diags, validateNoteLinks(parsed.MarkdownLinks, parsed.Wikilinks, ctx)...)
	return parsed, diags
}

func ParseAttentionFields(src string) (AttentionFields, []MarkdownDiagnostic) {
	note, diags := ParseMarkdownNote(src, MarkdownParseOptions{})
	parsed := AttentionFields{
		Kind:      scalarField(note, "kind"),
		Status:    scalarField(note, "status"),
		Focus:     scalarField(note, "focus"),
		Cadence:   scalarField(note, "cadence"),
		Strategic: optionalBoolField(note, "strategic"),
		Enjoyment: scalarField(note, "enjoyment"),
	}
	return parsed, diags
}

func ValidateAttentionFields(src string) (AttentionFields, []MarkdownDiagnostic) {
	parsed, diags := ParseAttentionFields(src)
	kind := parsed.Kind
	if !attentionKinds[kind] {
		return parsed, diags
	}
	if kind == "human" && parsed.Status == "deceased" {
		if parsed.Focus != "" || parsed.Cadence != "" {
			diags = append(diags, noteDiag("deceased people must not use focus or cadence"))
		}
		return parsed, diags
	}
	if !attentionFocus[parsed.Focus] {
		diags = append(diags, noteDiag("focus must be core, active, watch, or parked"))
	}
	if !attentionCadence[parsed.Cadence] {
		diags = append(diags, noteDiag("cadence must be daily, weekly, monthly, quarterly, annual, or none"))
	}
	if parsed.Enjoyment != "" && !stringSet("1", "2", "3")[parsed.Enjoyment] {
		diags = append(diags, noteDiag("enjoyment must be 1, 2, or 3"))
	}
	return parsed, diags
}

func ResolveWikilink(cfg *Config, sphere Sphere, raw string) (ResolvedPath, error) {
	if cfg == nil {
		return ResolvedPath{}, &PathError{Kind: ErrorInvalidConfig, Sphere: sphere}
	}
	vault, ok := cfg.Vault(sphere)
	if !ok {
		return ResolvedPath{}, &PathError{Kind: ErrorUnknownVault, Sphere: sphere}
	}
	target := strings.TrimSpace(strings.SplitN(strings.SplitN(raw, "|", 2)[0], "#", 2)[0])
	if target == "" || hasURLScheme(target) {
		return ResolvedPath{}, &PathError{Kind: ErrorUnsupportedLink, Sphere: sphere, Link: raw}
	}
	target = strings.TrimSuffix(filepath.FromSlash(target), ".md") + ".md"
	return resolveCandidate(vault, filepath.Join(vault.BrainRoot(), target), OpLink)
}

func validateCanonicalTopic(value string, ctx LinkValidationContext) []MarkdownDiagnostic {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	links := extractWikilinks(value)
	if len(links) != 1 || !strings.HasPrefix(strings.SplitN(links[0], "|", 2)[0], "topics/") {
		return []MarkdownDiagnostic{noteDiag("canonical_topic must be exactly one [[topics/...]] wikilink")}
	}
	if ctx.Config != nil {
		if _, err := ResolveWikilink(ctx.Config, ctx.Sphere, links[0]); err != nil {
			return []MarkdownDiagnostic{noteDiag("canonical_topic target is not resolvable: %v", err)}
		}
	}
	return nil
}

func validateNoteLinks(markdownLinks, wikilinks []string, ctx LinkValidationContext) []MarkdownDiagnostic {
	if ctx.Config == nil || strings.TrimSpace(ctx.Path) == "" {
		return nil
	}
	vault, ok := ctx.Config.Vault(ctx.Sphere)
	if !ok {
		return []MarkdownDiagnostic{noteDiag("unknown vault %q", ctx.Sphere)}
	}
	var diags []MarkdownDiagnostic
	for _, link := range markdownLinks {
		if _, err := resolveMarkdownLink(vault, ctx.Path, link); err != nil {
			diags = append(diags, noteDiag("markdown link %q is not resolvable: %v", link, err))
		}
	}
	for _, link := range wikilinks {
		if _, err := ResolveWikilink(ctx.Config, ctx.Sphere, link); err != nil {
			diags = append(diags, noteDiag("wikilink %q is not resolvable: %v", link, err))
		}
	}
	return diags
}

func scalarField(note *MarkdownNote, name string) string {
	node, ok := note.FrontMatterField(name)
	if !ok {
		return ""
	}
	return strings.TrimSpace(node.Value)
}

func listField(note *MarkdownNote, name string) []string {
	node, ok := note.FrontMatterField(name)
	if !ok {
		return nil
	}
	var out []string
	if err := node.Decode(&out); err != nil {
		out = splitListScalar(node.Value)
	}
	for i := range out {
		out[i] = strings.TrimSpace(out[i])
	}
	return compactStrings(out)
}

func boolField(note *MarkdownNote, name string) bool {
	value := optionalBoolField(note, name)
	return value != nil && *value
}

func optionalBoolField(note *MarkdownNote, name string) *bool {
	node, ok := note.FrontMatterField(name)
	if !ok {
		return nil
	}
	var out bool
	if err := node.Decode(&out); err != nil {
		return nil
	}
	return &out
}

func extractWikilinks(src string) []string {
	matches := noteWikilinkPattern.FindAllStringSubmatch(src, -1)
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		out = append(out, strings.TrimSpace(match[1]))
	}
	return out
}

func extractMarkdownLinks(src string) []string {
	matches := noteMarkdownLinkPattern.FindAllStringSubmatch(src, -1)
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		out = append(out, strings.TrimSpace(match[1]))
	}
	return out
}

func firstSectionParagraph(sections []MarkdownSection, names ...string) string {
	for _, name := range names {
		for _, section := range sections {
			if normalizeSectionName(section.Name) != normalizeSectionName(name) {
				continue
			}
			body := strings.TrimSpace(section.Body)
			if body == "" {
				continue
			}
			return strings.ReplaceAll(strings.SplitN(body, "\n\n", 2)[0], "\n", " ")
		}
	}
	return ""
}

func requiredScalar(name, value string) []MarkdownDiagnostic {
	if strings.TrimSpace(value) == "" {
		return []MarkdownDiagnostic{noteDiag("%s is required", name)}
	}
	return nil
}

func noteDiag(format string, args ...any) MarkdownDiagnostic {
	return MarkdownDiagnostic{Message: fmt.Sprintf(format, args...)}
}

func stringSet(values ...string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[value] = true
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

func isAcronym(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) < 2 {
		return false
	}
	return value == strings.ToUpper(value)
}

func splitListScalar(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ';' })
}

func compactStrings(values []string) []string {
	out := values[:0]
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, strings.TrimSpace(value))
		}
	}
	return out
}
