package meetings

import "strings"

// LegacyRefKey is the index key used to match a legacy meetings binding
// to a parsed task during the transition period. Tasks and refs that
// share the same (slug, normalized-person) tuple are treated as
// equivalent in FIFO order.
type LegacyRefKey struct {
	Slug   string
	Person string
}

// ParseLegacyRef recognises the legacy importer ref shape
// `<sphere>:<slug>:<person>:<task-hash>` (without a `meetings:` prefix —
// that's the binding's `provider` field). Anything that does not split
// into at least three colon-separated components is rejected so the
// caller can fall back to stable-ID matching only.
func ParseLegacyRef(ref string) (LegacyRefKey, bool) {
	clean := strings.TrimSpace(ref)
	if clean == "" {
		return LegacyRefKey{}, false
	}
	parts := strings.Split(clean, ":")
	if len(parts) < 3 {
		return LegacyRefKey{}, false
	}
	slug := strings.TrimSpace(parts[1])
	person := NormalizePersonName(parts[2])
	if slug == "" || person == "" {
		return LegacyRefKey{}, false
	}
	return LegacyRefKey{Slug: slug, Person: person}, true
}
