package brainprojects

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/sloppy-org/sloptools/internal/brain"
	braingtd "github.com/sloppy-org/sloptools/internal/brain/gtd"
)

const openLoopsHeading = "Open Loops"

type HubSummary struct {
	Path   string         `json:"path"`
	Link   string         `json:"link"`
	Counts ProjectCounts  `json:"counts"`
	Items  ProjectBuckets `json:"items,omitempty"`
}

type ProjectCounts struct {
	Next    int `json:"next"`
	Waiting int `json:"waiting"`
	Closed  int `json:"closed"`
}

type ProjectBuckets struct {
	Next    []ProjectCommitment `json:"next"`
	Waiting []ProjectCommitment `json:"waiting"`
	Closed  []ProjectCommitment `json:"closed"`
}

type ProjectCommitment struct {
	Path       string `json:"path"`
	Link       string `json:"link"`
	Outcome    string `json:"outcome"`
	Status     string `json:"status"`
	Person     string `json:"person,omitempty"`
	Due        string `json:"due,omitempty"`
	FollowUp   string `json:"follow_up,omitempty"`
	ClosedAt   string `json:"closed_at,omitempty"`
	Project    string `json:"project"`
	SortDue    string `json:"-"`
	SortFollow string `json:"-"`
}

type RenderResult struct {
	Sphere  string     `json:"sphere"`
	Hub     HubSummary `json:"hub"`
	Changed bool       `json:"changed"`
}

type Rule struct {
	Hub   string    `toml:"hub"`
	Match RuleMatch `toml:"match"`
}

type RuleMatch struct {
	People   []string `toml:"people"`
	Keywords []string `toml:"keywords"`
}

type BulkLinkResult struct {
	Sphere    string          `json:"sphere"`
	Linked    int             `json:"linked"`
	Skipped   int             `json:"skipped"`
	Ambiguous []AmbiguousLink `json:"ambiguous,omitempty"`
	Paths     []string        `json:"paths,omitempty"`
}

type AmbiguousLink struct {
	Path  string   `json:"path"`
	Rules []string `json:"rules"`
}

type commitmentNote struct {
	Source     brain.ResolvedPath
	Note       *brain.MarkdownNote
	Commitment braingtd.Commitment
}

type compiledRule struct {
	Key      string
	Project  string
	People   []string
	Patterns []*regexp.Regexp
}

type rulesFile struct {
	Projects map[string]Rule `toml:"project"`
}

func RenderHub(cfg *brain.Config, sphere brain.Sphere, hubPath string, now time.Time) (RenderResult, error) {
	vault, resolved, err := resolveHub(cfg, sphere, hubPath)
	if err != nil {
		return RenderResult{}, err
	}
	notes, err := readCommitments(cfg, vault.Sphere)
	if err != nil {
		return RenderResult{}, err
	}
	hub := summarizeHub(vault, resolved, notes, now, true)
	data, err := os.ReadFile(resolved.Path)
	if err != nil {
		return RenderResult{}, err
	}
	rendered := renderOpenLoops(string(data), formatOpenLoops(hub.Items))
	if rendered == string(data) {
		return RenderResult{Sphere: string(vault.Sphere), Hub: hub}, nil
	}
	if err := validateMarkdown(rendered); err != nil {
		return RenderResult{}, err
	}
	if err := os.WriteFile(resolved.Path, []byte(rendered), 0o644); err != nil {
		return RenderResult{}, err
	}
	return RenderResult{Sphere: string(vault.Sphere), Hub: hub, Changed: true}, nil
}

func ListHubs(cfg *brain.Config, sphere brain.Sphere, now time.Time) ([]HubSummary, error) {
	vault, err := cfgVault(cfg, sphere)
	if err != nil {
		return nil, err
	}
	notes, err := readCommitments(cfg, vault.Sphere)
	if err != nil {
		return nil, err
	}
	hubs, err := projectHubPaths(vault)
	if err != nil {
		return nil, err
	}
	out := make([]HubSummary, 0, len(hubs))
	for _, hub := range hubs {
		out = append(out, summarizeHub(vault, hub, notes, now, false))
	}
	return out, nil
}

func BulkLink(cfg *brain.Config, sphere brain.Sphere, rulesPath string) (BulkLinkResult, error) {
	vault, err := cfgVault(cfg, sphere)
	if err != nil {
		return BulkLinkResult{}, err
	}
	rules, err := LoadRules(rulesPath, vault)
	if err != nil {
		return BulkLinkResult{}, err
	}
	notes, err := readCommitments(cfg, vault.Sphere)
	if err != nil {
		return BulkLinkResult{}, err
	}
	return applyRules(vault.Sphere, notes, rules)
}

func LoadRules(path string, vault brain.Vault) ([]compiledRule, error) {
	clean := expandPath(path)
	if clean == "" {
		return nil, fmt.Errorf("rules path is required")
	}
	var raw rulesFile
	if _, err := toml.DecodeFile(clean, &raw); err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(raw.Projects))
	for key := range raw.Projects {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	rules := make([]compiledRule, 0, len(keys))
	for _, key := range keys {
		rule, err := compileRule(key, raw.Projects[key], vault)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

func applyRules(sphere brain.Sphere, notes []commitmentNote, rules []compiledRule) (BulkLinkResult, error) {
	result := BulkLinkResult{Sphere: string(sphere)}
	for _, note := range notes {
		if strings.TrimSpace(note.Commitment.Project) != "" {
			result.Skipped++
			continue
		}
		matches := matchingRules(note, rules)
		switch len(matches) {
		case 0:
			result.Skipped++
		case 1:
			if err := writeProject(note, matches[0].Project); err != nil {
				return BulkLinkResult{}, err
			}
			result.Linked++
			result.Paths = append(result.Paths, note.Source.Rel)
		default:
			result.Skipped++
			result.Ambiguous = append(result.Ambiguous, AmbiguousLink{Path: note.Source.Rel, Rules: ruleKeys(matches)})
		}
	}
	return result, nil
}

func summarizeHub(vault brain.Vault, hub brain.ResolvedPath, notes []commitmentNote, now time.Time, includeItems bool) HubSummary {
	project := projectLink(vault, hub)
	summary := HubSummary{Path: hub.Rel, Link: project}
	for _, note := range notes {
		if !sameProject(note.Commitment.Project, project) {
			continue
		}
		item := projectCommitment(vault, note, project)
		switch commitmentBucket(note.Commitment, now, false) {
		case "next":
			summary.Counts.Next++
			if includeItems {
				summary.Items.Next = append(summary.Items.Next, item)
			}
		case "waiting":
			summary.Counts.Waiting++
			if includeItems {
				summary.Items.Waiting = append(summary.Items.Waiting, item)
			}
		case "closed":
			summary.Counts.Closed++
			if includeItems && commitmentClosedRecently(note.Commitment, now) {
				summary.Items.Closed = append(summary.Items.Closed, item)
			}
		}
	}
	sortProjectItems(summary.Items)
	return summary
}

func readCommitments(cfg *brain.Config, sphere brain.Sphere) ([]commitmentNote, error) {
	var notes []commitmentNote
	err := brain.WalkVaultNotes(cfg, sphere, func(snapshot brain.NoteSnapshot) error {
		if snapshot.Kind != "commitment" {
			return nil
		}
		commitment, note, diags := braingtd.ParseCommitmentMarkdown(snapshot.Body)
		if len(diags) != 0 {
			return nil
		}
		notes = append(notes, commitmentNote{Source: snapshot.Source, Note: note, Commitment: *commitment})
		return nil
	})
	return notes, err
}

func projectHubPaths(vault brain.Vault) ([]brain.ResolvedPath, error) {
	root := filepath.Join(vault.BrainRoot(), "projects")
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var hubs []brain.ResolvedPath
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		path := filepath.Join(root, entry.Name())
		rel, err := filepath.Rel(vault.Root, path)
		if err != nil {
			return nil, err
		}
		hubs = append(hubs, brain.ResolvedPath{Sphere: vault.Sphere, VaultRoot: vault.Root, BrainRoot: vault.BrainRoot(), Path: path, Rel: filepath.ToSlash(rel)})
	}
	sort.Slice(hubs, func(i, j int) bool { return hubs[i].Rel < hubs[j].Rel })
	return hubs, nil
}

func projectCommitment(vault brain.Vault, note commitmentNote, project string) ProjectCommitment {
	c := note.Commitment
	outcome := commitmentOutcome(c)
	return ProjectCommitment{
		Path:       note.Source.Rel,
		Link:       noteLink(vault, note.Source, outcome),
		Outcome:    outcome,
		Status:     effectiveStatus(c),
		Person:     strings.TrimSpace(c.WaitingFor),
		Due:        firstNonEmpty(c.LocalOverlay.Due, c.Due),
		FollowUp:   firstNonEmpty(c.LocalOverlay.FollowUp, c.FollowUp),
		ClosedAt:   closedAt(c),
		Project:    project,
		SortDue:    firstNonEmpty(c.LocalOverlay.Due, c.Due),
		SortFollow: firstNonEmpty(c.LocalOverlay.FollowUp, c.FollowUp),
	}
}

func compileRule(key string, rule Rule, vault brain.Vault) (compiledRule, error) {
	hub, err := resolveHubInVault(vault, rule.Hub)
	if err != nil {
		return compiledRule{}, err
	}
	out := compiledRule{Key: key, Project: projectLink(vault, hub), People: compact(rule.Match.People)}
	for _, raw := range rule.Match.Keywords {
		pattern, err := regexp.Compile(raw)
		if err != nil {
			return compiledRule{}, fmt.Errorf("project.%s keyword %q: %w", key, raw, err)
		}
		out.Patterns = append(out.Patterns, pattern)
	}
	if len(out.People) == 0 && len(out.Patterns) == 0 {
		return compiledRule{}, fmt.Errorf("project.%s has no match rules", key)
	}
	return out, nil
}

func matchingRules(note commitmentNote, rules []compiledRule) []compiledRule {
	var matches []compiledRule
	for _, rule := range rules {
		if ruleMatches(note, rule) {
			matches = append(matches, rule)
		}
	}
	return matches
}

func ruleMatches(note commitmentNote, rule compiledRule) bool {
	for _, person := range commitmentPeople(note.Commitment) {
		if containsFold(rule.People, person) {
			return true
		}
	}
	text := searchableText(note)
	for _, pattern := range rule.Patterns {
		if pattern.MatchString(text) {
			return true
		}
	}
	return false
}

func writeProject(note commitmentNote, project string) error {
	if err := note.Note.SetFrontMatterField("project", project); err != nil {
		return err
	}
	rendered, err := note.Note.Render()
	if err != nil {
		return err
	}
	if err := validateMarkdown(rendered); err != nil {
		return err
	}
	return os.WriteFile(note.Source.Path, []byte(rendered), 0o644)
}

func resolveHub(cfg *brain.Config, sphere brain.Sphere, hubPath string) (brain.Vault, brain.ResolvedPath, error) {
	vault, err := cfgVault(cfg, sphere)
	if err != nil {
		return brain.Vault{}, brain.ResolvedPath{}, err
	}
	hub, err := resolveHubInVault(vault, hubPath)
	return vault, hub, err
}

func resolveHubInVault(vault brain.Vault, hubPath string) (brain.ResolvedPath, error) {
	candidate := strings.TrimSpace(hubPath)
	if candidate == "" {
		return brain.ResolvedPath{}, fmt.Errorf("hub is required")
	}
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(vault.Root, filepath.FromSlash(candidate))
	}
	rel, err := filepath.Rel(vault.Root, filepath.Clean(candidate))
	if err != nil {
		return brain.ResolvedPath{}, err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return brain.ResolvedPath{}, fmt.Errorf("hub %q is outside vault", hubPath)
	}
	return brain.ResolvedPath{Sphere: vault.Sphere, VaultRoot: vault.Root, BrainRoot: vault.BrainRoot(), Path: filepath.Clean(candidate), Rel: filepath.ToSlash(rel)}, nil
}

func cfgVault(cfg *brain.Config, sphere brain.Sphere) (brain.Vault, error) {
	if cfg == nil {
		return brain.Vault{}, fmt.Errorf("config is nil")
	}
	vault, ok := cfg.Vault(sphere)
	if !ok {
		return brain.Vault{}, fmt.Errorf("unknown vault %q", sphere)
	}
	return vault, nil
}
