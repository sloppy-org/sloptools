package sleepconv

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// mdCell escapes a string for use in a Markdown table cell.
func mdCell(value string) string {
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "|", "\\|")
	value = strings.TrimSpace(value)
	if value == "" {
		return " "
	}
	return value
}

// Candidate is a proper-noun candidate extracted from the user's
// recent claude/codex prompts, with a count of mentions and a lookup
// against the canonical brain entity dirs.
type Candidate struct {
	Name     string
	Mentions int
	NotePath string // brain-relative path of the existing canonical note, empty when none
	NoteKind string // "people" | "projects" | "institutions" | "topics" | "glossary" | ""
	FromLink bool   // at least one mention was an explicit [[wikilink]]
}

// CandidateMaxRows caps how many candidates the sleep packet
// renders. The list is sorted descending by mention count and existence
// (existing notes first since updates are higher-leverage than creates).
const CandidateMaxRows = 60

// minCandidateMentions is the floor for surfacing a candidate that has
// no existing note. Single mentions are too noisy to recommend creating
// a new canonical note for. Wikilinks bypass this floor — if the user
// already linked it, it's an explicit pointer.
const minCandidateMentions = 2

// ExtractCandidates scans the user-typed prose for proper-noun
// candidates and resolves each against the canonical entity directories
// under brainRoot. Returns the sorted, capped list.
func ExtractCandidates(prompts []Prompt, brainRoot string) []Candidate {
	if len(prompts) == 0 {
		return nil
	}
	counts := map[string]int{}
	fromLink := map[string]bool{}
	for _, p := range prompts {
		for _, name := range scanWikilinkTargets(p.Prose) {
			counts[name]++
			fromLink[name] = true
		}
		for _, name := range scanProperNouns(p.Prose) {
			counts[name]++
		}
		for _, name := range scanAcronyms(p.Prose) {
			counts[name]++
		}
	}
	if len(counts) == 0 {
		return nil
	}
	existing := loadBrainEntityIndex(brainRoot)
	var out []Candidate
	for name, n := range counts {
		if !fromLink[name] && n < minCandidateMentions {
			continue
		}
		c := Candidate{Name: name, Mentions: n, FromLink: fromLink[name]}
		if hit, ok := existing[strings.ToLower(name)]; ok {
			c.NotePath = hit.path
			c.NoteKind = hit.kind
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		if (out[i].NotePath != "") != (out[j].NotePath != "") {
			return out[i].NotePath != ""
		}
		if out[i].Mentions != out[j].Mentions {
			return out[i].Mentions > out[j].Mentions
		}
		return out[i].Name < out[j].Name
	})
	if len(out) > CandidateMaxRows {
		out = out[:CandidateMaxRows]
	}
	return out
}

// scanWikilinkTargets pulls every [[target]] target from prose. The
// inner part may contain a `|alias` segment; we keep only the target.
var wikilinkRE = regexp.MustCompile(`\[\[([^\]\|]+)(?:\|[^\]]*)?\]\]`)

func scanWikilinkTargets(s string) []string {
	matches := wikilinkRE.FindAllStringSubmatch(s, -1)
	if len(matches) == 0 {
		return nil
	}
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		t := strings.TrimSpace(m[1])
		if i := strings.LastIndex(t, "/"); i >= 0 {
			t = t[i+1:]
		}
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

// scanProperNouns extracts capitalized multi-word sequences (Latin or
// German extended). Each segment may be either a capitalized word
// (Sebastian, Graz) or a 2-3 letter all-caps acronym (TU, EU); the
// combination handles "TU Graz", "EU AI Act", "Sebastian Riepl".
// Sentence-starter capitalized words (Met, Today, We, …) are stripped
// from the leading position so "Met Sebastian Riepl" yields
// "Sebastian Riepl", not the verb-led phrase.
var properNounRE = regexp.MustCompile(`\b(?:[A-ZÄÖÜ][a-zäöüß0-9]+|[A-ZÄÖÜ]{2,4})(?:[\s\-](?:[A-ZÄÖÜ][a-zäöüß0-9]+|[A-ZÄÖÜ]{2,4}))+\b`)

func scanProperNouns(s string) []string {
	s = stripCodeBlocks(s)
	matchIdx := properNounRE.FindAllStringIndex(s, -1)
	if len(matchIdx) == 0 {
		return nil
	}
	var out []string
	for _, idx := range matchIdx {
		match := s[idx[0]:idx[1]]
		if isAtSentenceStart(s, idx[0]) {
			match = stripSentenceStarter(match)
			if match == "" {
				continue
			}
		}
		if isStopwordPhrase(match) {
			continue
		}
		out = append(out, match)
	}
	return out
}

// isAtSentenceStart returns true when the position is the start of a
// sentence: either start-of-string or preceded by `.`, `!`, `?`,
// possibly through some intervening whitespace or quote characters.
func isAtSentenceStart(s string, pos int) bool {
	for pos > 0 {
		c := s[pos-1]
		switch c {
		case ' ', '\t', '\n', '\r', '"', '\'':
			pos--
			continue
		case '.', '!', '?':
			return true
		}
		return false
	}
	return true
}

// stripSentenceStarter removes the leading word from a multi-word
// proper-noun match when that word is a known sentence-starter
// (verb at sentence start, day/month name, pronoun). Returns "" when
// stripping leaves a single token that is itself a stopword.
func stripSentenceStarter(match string) string {
	words := strings.Fields(match)
	if len(words) == 0 {
		return ""
	}
	if _, ok := sentenceStarterWords[strings.ToLower(words[0])]; ok {
		if len(words) == 1 {
			return ""
		}
		return strings.Join(words[1:], " ")
	}
	return match
}

// sentenceStarterWords lists capitalized first-token words that occur
// at sentence start without being entity names. Add cases here when
// noise emerges; the conservative default is "leave the match alone".
var sentenceStarterWords = map[string]struct{}{
	"i": {}, "we": {}, "you": {}, "they": {}, "he": {}, "she": {}, "it": {},
	"the": {}, "a": {}, "an": {}, "this": {}, "that": {}, "these": {},
	"today": {}, "yesterday": {}, "tomorrow": {}, "now": {}, "then": {},
	"please": {}, "thanks": {}, "thank": {},
	"met": {}, "did": {}, "do": {}, "had": {}, "have": {}, "made": {},
	"got": {}, "saw": {}, "let": {}, "go": {}, "went": {}, "came": {},
	"asked": {}, "told": {}, "wrote": {}, "read": {}, "wanted": {},
	"need": {}, "want": {}, "use": {}, "used": {}, "tried": {}, "ran": {},
	"check": {}, "fix": {}, "look": {}, "make": {}, "build": {}, "test": {},
	"monday": {}, "tuesday": {}, "wednesday": {}, "thursday": {},
	"friday": {}, "saturday": {}, "sunday": {},
	"january": {}, "february": {}, "march": {}, "april": {}, "may": {},
	"june": {}, "july": {}, "august": {}, "september": {}, "october": {},
	"november": {}, "december": {},
	"montag": {}, "dienstag": {}, "mittwoch": {}, "donnerstag": {},
	"freitag": {}, "samstag": {}, "sonntag": {},
	"januar": {}, "februar": {}, "märz": {}, "mai": {},
	"juni": {}, "juli": {}, "oktober": {}, "dezember": {},
	"ich": {}, "wir": {}, "du": {}, "ihr": {}, "er": {}, "sie": {}, "es": {},
	"heute": {}, "gestern": {}, "morgen": {}, "bitte": {}, "danke": {},
}

// scanAcronyms extracts uppercase acronyms 3+ letters long, optionally
// with internal digits and dashes. "TU", "EU", "OK" are too short or
// too generic; the regex starts at 3.
var acronymRE = regexp.MustCompile(`\b[A-Z][A-Z0-9\-]{2,}\b`)

func scanAcronyms(s string) []string {
	s = stripCodeBlocks(s)
	matches := acronymRE.FindAllString(s, -1)
	if len(matches) == 0 {
		return nil
	}
	var out []string
	for _, m := range matches {
		if isStopwordAcronym(m) {
			continue
		}
		out = append(out, m)
	}
	return out
}

// stripCodeBlocks drops fenced code blocks and inline code from the
// scan input. Code identifiers are not entity candidates.
var codeFenceRE = regexp.MustCompile("(?s)```.*?```")
var inlineCodeRE = regexp.MustCompile("`[^`]*`")

func stripCodeBlocks(s string) string {
	s = codeFenceRE.ReplaceAllString(s, "")
	s = inlineCodeRE.ReplaceAllString(s, "")
	return s
}

// stopwordPhrases are sequences that match the proper-noun regex but
// are sentence-starter or filler patterns, not entity names. Lowercase
// keys; the matcher lowercases the candidate before lookup.
var stopwordPhrases = map[string]struct{}{
	"can you":       {},
	"could you":     {},
	"would you":     {},
	"please check":  {},
	"please fix":    {},
	"please write":  {},
	"please run":    {},
	"please use":    {},
	"please make":   {},
	"please do":     {},
	"thank you":     {},
	"thanks for":    {},
	"good morning":  {},
	"good evening":  {},
	"good night":    {},
	"new york":      {},
	"san francisco": {},
}

// stopwordSingles are common sentence-initial words that occasionally
// chain with another capitalized word and trip the multi-word regex.
var stopwordSingles = map[string]struct{}{
	"i": {}, "we": {}, "you": {}, "they": {}, "he": {}, "she": {}, "it": {},
	"the": {}, "a": {}, "an": {},
	"today": {}, "yesterday": {}, "tomorrow": {},
	"monday": {}, "tuesday": {}, "wednesday": {}, "thursday": {},
	"friday": {}, "saturday": {}, "sunday": {},
	"january": {}, "february": {}, "march": {}, "april": {}, "may": {},
	"june": {}, "july": {}, "august": {}, "september": {}, "october": {},
	"november": {}, "december": {},
	"montag": {}, "dienstag": {}, "mittwoch": {}, "donnerstag": {},
	"freitag": {}, "samstag": {}, "sonntag": {},
	"januar": {}, "februar": {}, "märz": {}, "april ": {}, "mai": {},
	"juni": {}, "juli": {}, "august ": {}, "september ": {}, "oktober": {},
	"november ": {}, "dezember": {},
	"ich": {}, "wir": {}, "du": {}, "ihr": {}, "er": {}, "sie": {}, "es": {},
	"heute": {}, "gestern": {}, "morgen": {},
}

func isStopwordPhrase(s string) bool {
	low := strings.ToLower(s)
	if _, ok := stopwordPhrases[low]; ok {
		return true
	}
	parts := strings.Fields(low)
	if len(parts) > 0 {
		if _, ok := stopwordSingles[parts[0]]; ok {
			return true
		}
	}
	return false
}

// stopwordAcronyms are acronyms that appear in everyday prose and are
// not entity candidates. Add to this set when noise emerges.
var stopwordAcronyms = map[string]struct{}{
	"PDF":          {},
	"JSON":         {},
	"YAML":         {},
	"TOML":         {},
	"HTML":         {},
	"CSS":          {},
	"URL":          {},
	"URI":          {},
	"API":          {},
	"CLI":          {},
	"CPU":          {},
	"GPU":          {},
	"RAM":          {},
	"SSD":          {},
	"HDD":          {},
	"USB":          {},
	"WIFI":         {},
	"FAQ":          {},
	"TODO":         {},
	"TBD":          {},
	"FIXME":        {},
	"OK":           {},
	"YES":          {},
	"NO":           {},
	"AGENTS":       {},
	"INSTRUCTIONS": {},
}

func isStopwordAcronym(s string) bool {
	_, ok := stopwordAcronyms[s]
	return ok
}

// brainEntityHit records where an existing canonical note lives.
type brainEntityHit struct {
	path string
	kind string
}

// loadBrainEntityIndex walks brain/{people,projects,institutions,
// topics,glossary} and returns lowercase-name → location. Filename
// without .md extension is the key. Aliases are not parsed for v1;
// the model can still find them via brain_search MCP if needed.
func loadBrainEntityIndex(brainRoot string) map[string]brainEntityHit {
	out := map[string]brainEntityHit{}
	if brainRoot == "" {
		return out
	}
	for _, kind := range []string{"people", "projects", "institutions", "topics", "glossary"} {
		dir := filepath.Join(brainRoot, kind)
		_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".md") {
				return nil
			}
			name := strings.TrimSuffix(filepath.Base(path), ".md")
			rel, _ := filepath.Rel(brainRoot, path)
			key := strings.ToLower(name)
			if _, exists := out[key]; !exists {
				out[key] = brainEntityHit{path: filepath.ToSlash(rel), kind: kind}
			}
			return nil
		})
	}
	// Don't error on missing dirs.
	_ = os.PathSeparator
	return out
}

// RenderCandidatesSection emits the Markdown checklist that
// follows the conversation prompts in the sleep packet. Returns empty
// when no candidates survive the floor.
func RenderCandidatesSection(candidates []Candidate) string {
	if len(candidates) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Entity candidates from conversation\n\n")
	b.WriteString("Names extracted deterministically from the prompt list above. **Treat this as a mandatory checklist for the conversation-log section's six-step rule.** For each row:\n\n")
	b.WriteString("- If `Note` is set: read that canonical note and update it in place with any new fact, status, decision, or relationship implied by the prompts. Add a dated bullet under the appropriate section if a discrete event happened. Bump `last_seen` if the prompts represent direct contact.\n")
	b.WriteString("- If `Note` is empty and `Mentions` ≥ 2 or `From [[link]]` is yes: this is a serious candidate. Verify from the prompt context what kind of entity it is, then create the canonical note under the matching kind directory using the schema in `brain/conventions/attention.md` (people) or `brain/conventions/entity-graph.md` (others). Skip only if the name is obviously a tool, library, code identifier, or generic noun.\n")
	b.WriteString("- Single-mention candidates without an existing note are NOT in this list — they have been filtered as noise. Do not invent them.\n\n")
	b.WriteString("| Entity | Mentions | From [[link]] | Note |\n")
	b.WriteString("|---|---|---|---|\n")
	for _, c := range candidates {
		linkMark := "no"
		if c.FromLink {
			linkMark = "yes"
		}
		note := c.NotePath
		if note == "" {
			note = "_(none — consider creating)_"
		}
		fmt.Fprintf(&b, "| %s | %d | %s | %s |\n",
			mdCell(c.Name), c.Mentions, linkMark, mdCell(note))
	}
	b.WriteString("\n")
	return b.String()
}
