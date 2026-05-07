// Package glossary auto-injects local-vocabulary terms into scout /
// folder-note / triage packets so the agent does not e.g. confuse
// "1/ν transport" with "neutrino transport". It reads
// `<vault>/brain/glossary/*.md` once per process (cached by directory
// mtime) and emits a bounded `## Glossary context` packet section.
package glossary

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"gopkg.in/yaml.v3"
)

// Term is a single matched glossary entry, ready to embed in a
// scout / folder-note / triage packet. The packet builder lifts the
// definition prose from the note body but caps it so a packet stays
// lean: long topic prose belongs in `brain/topics/`, not the glossary.
type Term struct {
	// File is the absolute path of the matched glossary note.
	File string
	// VaultRel is the vault-relative path (e.g. "brain/glossary/cxrs.md").
	VaultRel string
	// DisplayName is the human-readable label from frontmatter
	// `display_name`, falling back to the H1 if absent.
	DisplayName string
	// Aliases are the surface forms recognised in note bodies.
	Aliases []string
	// Definition is the first paragraph (or `## Definition` section, if
	// present) of the glossary note, trimmed and capped at MaxTermBytes.
	Definition string
	// CanonicalTopic is the wikilink target in frontmatter
	// `canonical_topic`, if any. The agent should follow that link for
	// fuller context rather than re-explain the term.
	CanonicalTopic string
	// DoNotConfuseWith lists frontmatter `do_not_confuse_with` items so
	// the agent does not silently substitute a sibling term.
	DoNotConfuseWith []string
	// MatchedSurface is the alias / display name / filename token that
	// triggered the inclusion. Useful for debugging false matches.
	MatchedSurface string
}

// MaxTermBytes caps the per-term definition prose. 500 bytes is enough
// for one-sentence glossary stubs; longer entries are referenced by
// CanonicalTopic and the agent must follow the link if it needs more.
const MaxTermBytes = 500

// MaxTerms caps the number of glossary entries injected per
// packet. The packet builder ranks by alias-match-length descending so
// longer multi-word aliases beat short prefix matches.
const MaxTerms = 8

// MaxGlossaryPacketBytes is the total size cap of the rendered packet
// section across all included terms; we drop terms past the cap rather
// than truncate one mid-sentence.
const MaxGlossaryPacketBytes = 3000

type glossaryCacheEntry struct {
	terms []glossaryRecord
	mtime time.Time
}

type glossaryRecord struct {
	file             string
	vaultRel         string
	displayName      string
	aliases          []string
	definition       string
	canonicalTopic   string
	doNotConfuseWith []string
}

var (
	glossaryCache   = map[string]glossaryCacheEntry{}
	glossaryCacheMu sync.Mutex
)

// Load reads every glossary/*.md file once per process (cached
// by directory mtime) and returns a flat slice of records. The root
// can be either a vault root (we look at <root>/brain/glossary) or a
// brain root (we look at <root>/glossary). Whichever exists wins.
//
// Errors reading individual notes are swallowed so a single malformed
// file does not mask the rest; the validator owns strict reporting.
func Load(root string) []glossaryRecord {
	if root == "" {
		return nil
	}
	dir := resolveGlossaryDir(root)
	if dir == "" {
		return nil
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return nil
	}
	glossaryCacheMu.Lock()
	defer glossaryCacheMu.Unlock()
	if cached, ok := glossaryCache[dir]; ok && cached.mtime.Equal(info.ModTime()) {
		return cached.terms
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []glossaryRecord
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") || e.Name() == "index.md" {
			continue
		}
		full := filepath.Join(dir, e.Name())
		body, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		parsed, _ := parseGlossaryFrontmatter(string(body))
		rec := glossaryRecord{
			file:             full,
			vaultRel:         filepath.ToSlash(filepath.Join("brain", "glossary", e.Name())),
			displayName:      parsed.DisplayName,
			aliases:          dedupeStrings(parsed.Aliases),
			canonicalTopic:   parsed.CanonicalTopic,
			doNotConfuseWith: dedupeStrings(parsed.DoNotConfuseWith),
			definition:       extractGlossaryDefinition(string(body)),
		}
		if rec.displayName == "" {
			rec.displayName = strings.TrimSuffix(e.Name(), ".md")
		}
		// Filename without extension is also a recognised surface form.
		base := strings.TrimSuffix(e.Name(), ".md")
		rec.aliases = appendIfMissing(rec.aliases, base)
		// And the display name itself.
		rec.aliases = appendIfMissing(rec.aliases, rec.displayName)
		out = append(out, rec)
	}
	glossaryCache[dir] = glossaryCacheEntry{terms: out, mtime: info.ModTime()}
	return out
}

// resolveGlossaryDir picks the right glossary directory for either a
// vault-root layout (~/Nextcloud) or a brain-root layout
// (~/Nextcloud/brain). Returns "" when neither exists.
func resolveGlossaryDir(root string) string {
	candidates := []string{
		filepath.Join(root, "brain", "glossary"),
		filepath.Join(root, "glossary"),
	}
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && info.IsDir() {
			return c
		}
	}
	return ""
}

// RelevantTerms scans noteBody for surface forms of glossary
// aliases and returns the matching entries, ranked by longest alias
// first so multi-word terms (e.g. "1/ν transport") win over single-word
// prefix collisions. Bounded by MaxTerms.
//
// Matching is case-insensitive and respects word boundaries: an alias
// must appear bracketed by non-letter / non-digit characters (or
// start/end of string) to count. This avoids matching "RMP" inside
// "RMP-induced" but will match "RMP" inside "RMP."
func RelevantTerms(vaultRoot, noteBody string) []Term {
	if strings.TrimSpace(noteBody) == "" {
		return nil
	}
	records := Load(vaultRoot)
	if len(records) == 0 {
		return nil
	}
	lowerBody := strings.ToLower(noteBody)
	type candidate struct {
		rec     glossaryRecord
		surface string
	}
	var hits []candidate
	for _, rec := range records {
		// Build a unique alias set per record to keep ranking stable.
		seen := map[string]bool{}
		for _, alias := range rec.aliases {
			a := strings.TrimSpace(alias)
			if a == "" {
				continue
			}
			la := strings.ToLower(a)
			if seen[la] {
				continue
			}
			seen[la] = true
			if !containsWordBoundary(lowerBody, la) {
				continue
			}
			hits = append(hits, candidate{rec: rec, surface: a})
			break // one match per record is enough
		}
	}
	// Rank: longest matched surface wins, then display_name alphabetical
	// for stability across runs.
	sort.SliceStable(hits, func(i, j int) bool {
		li, lj := len(hits[i].surface), len(hits[j].surface)
		if li != lj {
			return li > lj
		}
		return hits[i].rec.displayName < hits[j].rec.displayName
	})
	if len(hits) > MaxTerms {
		hits = hits[:MaxTerms]
	}
	out := make([]Term, 0, len(hits))
	for _, h := range hits {
		def := h.rec.definition
		if len(def) > MaxTermBytes {
			def = def[:MaxTermBytes]
			if i := strings.LastIndexAny(def, ".!?\n"); i > MaxTermBytes/2 {
				def = def[:i+1]
			}
			def = strings.TrimSpace(def)
		}
		out = append(out, Term{
			File:             h.rec.file,
			VaultRel:         h.rec.vaultRel,
			DisplayName:      h.rec.displayName,
			Aliases:          h.rec.aliases,
			Definition:       def,
			CanonicalTopic:   h.rec.canonicalTopic,
			DoNotConfuseWith: h.rec.doNotConfuseWith,
			MatchedSurface:   h.surface,
		})
	}
	return out
}

// FormatPacketSection renders matched terms into a packet
// section. Returns "" when terms is empty so the caller can omit the
// section entirely. Total output is capped at MaxGlossaryPacketBytes;
// any term that would push past the cap is dropped (not truncated).
func FormatPacketSection(terms []Term) string {
	if len(terms) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Glossary context\n\n")
	b.WriteString("Local-vocabulary terms recognised in this entity. Use these definitions verbatim — do not reinterpret common acronyms by general knowledge. Follow the canonical_topic link for fuller context.\n\n")
	prefixLen := b.Len()
	for _, t := range terms {
		var item strings.Builder
		item.WriteString("- **")
		item.WriteString(t.DisplayName)
		item.WriteString("**: ")
		if t.Definition != "" {
			item.WriteString(t.Definition)
			if !endsWithSentenceTerminator(t.Definition) {
				item.WriteString(".")
			}
		}
		item.WriteString(" (path: `")
		item.WriteString(t.VaultRel)
		item.WriteString("`")
		if t.CanonicalTopic != "" {
			item.WriteString("; canonical: ")
			item.WriteString(t.CanonicalTopic)
		}
		if len(t.DoNotConfuseWith) > 0 {
			item.WriteString("; do not confuse with: ")
			item.WriteString(strings.Join(t.DoNotConfuseWith, ", "))
		}
		item.WriteString(")\n")
		if b.Len()-prefixLen+item.Len() > MaxGlossaryPacketBytes {
			break
		}
		b.WriteString(item.String())
	}
	b.WriteString("\n")
	return b.String()
}

// extractGlossaryDefinition picks a short defining string out of the
// glossary body. Preference order: a `## Definition` section, then the
// first non-empty paragraph after the H1, then frontmatter
// `definition:` if neither produced anything.
func extractGlossaryDefinition(src string) string {
	// Frontmatter `definition:` (single-line scalar) — fallback only.
	parsed, _ := parseGlossaryFrontmatter(src)
	frontmatterDef := strings.TrimSpace(parsed.Definition)

	// Strip frontmatter for body scanning.
	body := stripFrontmatterBlock(src)
	lines := strings.Split(body, "\n")

	// 1) Look for ## Definition or ### Definition.
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if !strings.HasPrefix(trim, "##") {
			continue
		}
		heading := strings.TrimSpace(strings.TrimLeft(trim, "#"))
		if !strings.EqualFold(heading, "Definition") {
			continue
		}
		para := firstParagraphAfter(lines, i+1)
		if para != "" {
			return para
		}
	}

	// 2) First paragraph after the H1.
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "# ") {
			para := firstParagraphAfter(lines, i+1)
			if para != "" {
				return para
			}
			break
		}
	}

	// 3) Fall back to frontmatter definition: scalar.
	return frontmatterDef
}

func firstParagraphAfter(lines []string, start int) string {
	var paragraph []string
	inPara := false
	for i := start; i < len(lines); i++ {
		trim := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trim, "##") || strings.HasPrefix(trim, "# ") {
			break
		}
		if trim == "" {
			if inPara {
				break
			}
			continue
		}
		inPara = true
		paragraph = append(paragraph, trim)
	}
	return strings.Join(paragraph, " ")
}

func stripFrontmatterBlock(src string) string {
	if !strings.HasPrefix(src, "---\n") && !strings.HasPrefix(src, "---\r\n") {
		return src
	}
	rest := src[4:]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return src
	}
	rest = rest[idx+4:]
	return strings.TrimLeft(rest, "\r\n")
}

func containsWordBoundary(haystack, needle string) bool {
	if needle == "" {
		return false
	}
	from := 0
	for {
		i := strings.Index(haystack[from:], needle)
		if i < 0 {
			return false
		}
		abs := from + i
		end := abs + len(needle)
		// Boundary check: position before/after must not be a letter/digit.
		if !boundaryOK(haystack, abs-1) || !boundaryOK(haystack, end) {
			from = abs + 1
			continue
		}
		return true
	}
}

func boundaryOK(s string, idx int) bool {
	if idx < 0 || idx >= len(s) {
		return true
	}
	r := rune(s[idx])
	return !unicode.IsLetter(r) && !unicode.IsDigit(r)
}

func endsWithSentenceTerminator(s string) bool {
	s = strings.TrimRight(s, " \t\r\n")
	if s == "" {
		return false
	}
	switch s[len(s)-1] {
	case '.', '!', '?':
		return true
	}
	return false
}

// glossaryFrontmatter is the subset of YAML frontmatter the inject
// helper needs. We parse it inline (rather than re-using the larger
// brain.ParseGlossaryNote) so this package has no inverted dependency
// on package brain.
type glossaryFrontmatter struct {
	DisplayName      string   `yaml:"display_name"`
	Aliases          []string `yaml:"aliases"`
	CanonicalTopic   string   `yaml:"canonical_topic"`
	DoNotConfuseWith []string `yaml:"do_not_confuse_with"`
	Definition       string   `yaml:"definition"`
}

// parseGlossaryFrontmatter extracts the leading `---\n…\n---` YAML
// block from the markdown source and decodes the fields we need. A
// missing or malformed frontmatter yields an empty struct, never an
// error: the caller treats glossary entries as opportunistic; bad
// files are silently skipped (the brain validator owns strict
// reporting).
func parseGlossaryFrontmatter(src string) (glossaryFrontmatter, error) {
	var fm glossaryFrontmatter
	if !strings.HasPrefix(src, "---\n") && !strings.HasPrefix(src, "---\r\n") {
		return fm, nil
	}
	rest := src[4:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return fm, nil
	}
	yamlBlock := rest[:end]
	_ = yaml.Unmarshal([]byte(yamlBlock), &fm)
	return fm, nil
}

// dedupeStrings keeps order, drops empties and case-insensitive
// duplicates. Same shape as the package brain helper but local so the
// glossary package has no inverted dependency.
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

func appendIfMissing(items []string, v string) []string {
	v = strings.TrimSpace(v)
	if v == "" {
		return items
	}
	for _, x := range items {
		if strings.EqualFold(x, v) {
			return items
		}
	}
	return append(items, v)
}
