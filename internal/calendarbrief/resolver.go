package calendarbrief

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sloppy-org/sloptools/internal/brain"
	"github.com/sloppy-org/sloptools/internal/mcp/peoplebrief"
)

// VaultResolver resolves a calendar attendee to a brain/people/ note inside
// the supplied vault. Lookup proceeds in two steps: an exact normalized-name
// match against the people directory, then an email match against each
// candidate note's frontmatter or top-of-note `Email:` bullet.
type VaultResolver struct {
	vault brain.Vault
}

// NewVaultResolver returns a resolver bound to the supplied vault. Callers
// must pre-resolve the vault from brain.Config.Vault().
func NewVaultResolver(vault brain.Vault) *VaultResolver {
	return &VaultResolver{vault: vault}
}

// Resolve implements PersonResolver. It first tries to match attendee.Name
// against people-note filenames, then falls back to scanning every people
// note for an email match. Returns ok=false when no canonical note exists.
func (r *VaultResolver) Resolve(_ context.Context, att Attendee) (Person, bool, error) {
	if r == nil {
		return Person{}, false, errors.New("calendarbrief: VaultResolver is nil")
	}
	peopleDir := filepath.Join(r.vault.BrainRoot(), "people")
	entries, err := os.ReadDir(peopleDir)
	if err != nil {
		if os.IsNotExist(err) {
			return Person{}, false, nil
		}
		return Person{}, false, err
	}
	candidates := personCandidates(entries)
	if person, ok := matchByName(peopleDir, candidates, att.Name); ok {
		return person, true, nil
	}
	if att.Email != "" {
		person, ok, err := matchByEmail(peopleDir, candidates, att.Email)
		if err != nil {
			return Person{}, false, err
		}
		if ok {
			return person, true, nil
		}
	}
	return Person{}, false, nil
}

func personCandidates(entries []os.DirEntry) []string {
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		out = append(out, strings.TrimSuffix(entry.Name(), ".md"))
	}
	sort.Strings(out)
	return out
}

func matchByName(peopleDir string, candidates []string, rawName string) (Person, bool) {
	name := strings.TrimSpace(rawName)
	if name == "" {
		return Person{}, false
	}
	normalized := peoplebrief.NormalizePersonName(name)
	if normalized == "" {
		return Person{}, false
	}
	matches := candidates[:0:0]
	for _, candidate := range candidates {
		if peoplebrief.NormalizePersonName(candidate) == normalized {
			matches = append(matches, candidate)
		}
	}
	if len(matches) != 1 {
		return Person{}, false
	}
	return Person{Name: matches[0], Path: filepath.Join(peopleDir, matches[0]+".md")}, true
}

func matchByEmail(peopleDir string, candidates []string, email string) (Person, bool, error) {
	target := strings.ToLower(strings.TrimSpace(email))
	if target == "" {
		return Person{}, false, nil
	}
	for _, name := range candidates {
		path := filepath.Join(peopleDir, name+".md")
		src, err := os.ReadFile(path)
		if err != nil {
			return Person{}, false, err
		}
		note, _ := brain.ParseMarkdownNote(string(src), brain.MarkdownParseOptions{})
		candidate := strings.ToLower(strings.TrimSpace(peoplebrief.PersonEmail(note, string(src))))
		if candidate != "" && candidate == target {
			return Person{Name: name, Path: path, Email: candidate}, true, nil
		}
	}
	return Person{}, false, nil
}
