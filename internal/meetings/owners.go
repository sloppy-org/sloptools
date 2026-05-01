package meetings

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// ResolvePerson takes the raw person name parsed out of a `### <Person>`
// heading and returns a canonical name. The resolver applies, in order:
//
//  1. The owner-alias map (case-insensitive on the alias key).
//  2. An exact normalised match against `candidates` (ASCII fold,
//     parenthetical strip, lower-case, whitespace-collapsed).
//  3. A single-token unique match against `candidates` when the parsed
//     name normalises to a single token (first-name shorthand).
//
// When no candidate resolves, the input is returned trimmed unchanged so
// the rest of the ingest pipeline keeps the LLM-supplied label visible.
// candidates is the list of slugs (filenames without the `.md` suffix)
// found under `brain/people/` for the relevant sphere.
func ResolvePerson(name string, aliases map[string]string, candidates []string) string {
	clean := strings.TrimSpace(name)
	if clean == "" {
		return ""
	}
	if aliases != nil {
		if mapped, ok := aliases[strings.ToLower(clean)]; ok {
			if trimmed := strings.TrimSpace(mapped); trimmed != "" {
				clean = trimmed
			}
		}
	}
	normalizedQuery := NormalizePersonName(clean)
	if normalizedQuery == "" {
		return clean
	}
	exact := matchExact(normalizedQuery, candidates)
	if len(exact) == 1 {
		return exact[0]
	}
	if isSingleToken(normalizedQuery) {
		token := matchToken(normalizedQuery, candidates)
		if len(token) == 1 {
			return token[0]
		}
	}
	return clean
}

// NormalizePersonName lower-cases, ASCII-folds, and collapses whitespace
// in name; parenthetical groups (e.g. honorifics) are stripped before
// folding. Two distinct names that round-trip to the same value are
// considered equivalent for resolver purposes.
func NormalizePersonName(name string) string {
	folded := asciiFold(stripParenthetical(name))
	return strings.ToLower(strings.Join(strings.Fields(folded), " "))
}

func matchExact(normalizedQuery string, candidates []string) []string {
	var out []string
	for _, candidate := range candidates {
		if NormalizePersonName(candidate) == normalizedQuery {
			out = append(out, candidate)
		}
	}
	return out
}

func matchToken(token string, candidates []string) []string {
	var out []string
	for _, candidate := range candidates {
		for _, field := range strings.Fields(NormalizePersonName(candidate)) {
			if field == token {
				out = append(out, candidate)
				break
			}
		}
	}
	return out
}

func isSingleToken(value string) bool {
	return value != "" && !strings.Contains(value, " ")
}

func asciiFold(value string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(value) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		if r < 128 {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func stripParenthetical(value string) string {
	var b strings.Builder
	depth := 0
	for _, r := range value {
		switch r {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}
