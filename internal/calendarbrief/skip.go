package calendarbrief

import "strings"

// shouldSkip implements the scope-guard rules from issue #92:
//   - Maker time (focus blocks)
//   - Family-floor markers ("Emil", "Mama")
//   - All-day "Kleinkram" rotations
//   - Events tagged "no_brief: true" in their description
func shouldSkip(ev Event) bool {
	summary := strings.ToLower(ev.Summary)
	if strings.Contains(summary, "maker time") {
		return true
	}
	if matchesFamilyMarker(summary) {
		return true
	}
	if ev.AllDay && strings.Contains(summary, "kleinkram") {
		return true
	}
	if hasNoBriefMarker(ev.Description) {
		return true
	}
	return false
}

var familyMarkers = []string{"emil", "mama"}

func matchesFamilyMarker(summary string) bool {
	for _, marker := range familyMarkers {
		if containsWord(summary, marker) {
			return true
		}
	}
	return false
}

// containsWord reports whether haystack contains needle as a whitespace- or
// punctuation-bounded token. We deliberately avoid plain substring matching
// here so a meeting titled "Email triage" does not get classified as the
// "Emil" family floor.
func containsWord(haystack, needle string) bool {
	if needle == "" {
		return false
	}
	for _, token := range tokenize(haystack) {
		if token == needle {
			return true
		}
	}
	return false
}

func tokenize(value string) []string {
	out := make([]string, 0, 4)
	current := make([]rune, 0, len(value))
	flush := func() {
		if len(current) == 0 {
			return
		}
		out = append(out, string(current))
		current = current[:0]
	}
	for _, r := range value {
		if isWordRune(r) {
			current = append(current, r)
			continue
		}
		flush()
	}
	flush()
	return out
}

func isWordRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
}

func hasNoBriefMarker(description string) bool {
	if description == "" {
		return false
	}
	for _, line := range strings.Split(description, "\n") {
		clean := strings.ToLower(strings.TrimSpace(line))
		if !strings.HasPrefix(clean, "no_brief") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(clean, "no_brief"))
		value = strings.TrimSpace(strings.TrimPrefix(value, ":"))
		value = strings.TrimSpace(strings.TrimPrefix(value, "="))
		switch value {
		case "true", "yes", "1":
			return true
		}
	}
	return false
}
