package peoplebrief

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// PersonFieldMatches reports whether `value` (a frontmatter field such as
// `waiting_for`) names the same person as `person`. Single-token values are
// allowed to match any whitespace-separated token of the candidate name.
func PersonFieldMatches(value, person string) bool {
	return personFieldMatches(value, person)
}

// PeopleFieldMatches reports whether any entry in `values` (typically a
// `people:` list) names the same person.
func PeopleFieldMatches(values []string, person string) bool {
	return peopleFieldMatches(values, person)
}

// NormalizePersonName collapses a person name to lower-case, ASCII-folded,
// whitespace-trimmed form with parenthetical disambiguators stripped. It
// is the single canonical name comparator the brief uses across notes,
// commitments, and meeting wikilinks.
func NormalizePersonName(name string) string {
	return normalizePersonName(name)
}

// SingleToken reports whether the normalized value is a single
// whitespace-free token, useful for callers that resolve fuzzy
// first-name-only references.
func SingleToken(value string) bool {
	return singleToken(value)
}

// NameContainsToken reports whether any whitespace-separated token of
// `name` equals `token` exactly. The caller is responsible for
// normalizing inputs through NormalizePersonName first.
func NameContainsToken(name, token string) bool {
	return nameContainsToken(name, token)
}

func personFieldMatches(value, person string) bool {
	clean := normalizePersonName(value)
	canonical := normalizePersonName(person)
	if clean == "" {
		return false
	}
	if clean == canonical {
		return true
	}
	return singleToken(clean) && nameContainsToken(canonical, clean)
}

func peopleFieldMatches(values []string, person string) bool {
	for _, value := range values {
		if personFieldMatches(value, person) {
			return true
		}
	}
	return false
}

func normalizePersonName(name string) string {
	folded := asciiFold(stripParenthetical(name))
	return strings.ToLower(strings.Join(strings.Fields(folded), " "))
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

func singleToken(value string) bool {
	return value != "" && !strings.Contains(value, " ")
}

func nameContainsToken(name, token string) bool {
	for _, field := range strings.Fields(name) {
		if field == token {
			return true
		}
	}
	return false
}
