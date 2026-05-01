package mcp

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/brain"
	braingtd "github.com/sloppy-org/sloptools/internal/brain/gtd"
	"github.com/sloppy-org/sloptools/internal/meetings"
)

const meetingsProvider = "meetings"

type meetingsIngestSummary struct {
	Created   []string `json:"created"`
	Closed    []string `json:"closed"`
	Skipped   []string `json:"skipped"`
	Stamped   []string `json:"stamped"`
	Affected  []string `json:"affected"`
	Walked    []string `json:"walked"`
	LegacyHit []string `json:"legacy_hit"`
}

// meetingBindingIndex is the lookup the ingest pass uses to decide
// whether a parsed task is "already known" or needs a fresh commitment.
// New stable IDs hit byStableID directly. During the post-stable-ID
// transition window we also accept the legacy importer ref shape
// `meetings:<sphere>:<slug>:<person>:<task-hash>` and dispense those
// matches in source order, FIFO per (slug, normalized-person).
type meetingBindingIndex struct {
	byStableID map[string]dedupNote
	legacy     map[meetings.LegacyRefKey][]dedupNote
}

type meetingIngestContext struct {
	cfg        *brain.Config
	sphere     string
	bindings   meetingBindingIndex
	sphereCfg  meetings.SphereConfig
	candidates []string
	now        string
}

func ingestMeetingsResult(summary meetingsIngestSummary, sphere string) map[string]interface{} {
	return map[string]interface{}{
		"sphere":     sphere,
		"source":     meetingsProvider,
		"count":      len(summary.Affected),
		"paths":      summary.Affected,
		"created":    summary.Created,
		"closed":     summary.Closed,
		"skipped":    summary.Skipped,
		"stamped":    summary.Stamped,
		"walked":     summary.Walked,
		"legacy_hit": summary.LegacyHit,
		"updated":    len(summary.Affected) > 0 || len(summary.Stamped) > 0,
	}
}

// IngestMeetings runs `brain.gtd.ingest --source meetings` outside of the
// MCP request loop. It is the entry point used by the `sloptools meetings`
// CLI so the watcher can trigger ingestion in-process after writing a
// `MEETING_NOTES.md` or detecting an mtime change. brainConfigPath, sphere,
// and paths follow the same semantics as the MCP tool; sourcesPath defaults
// to ~/.config/sloptools/sources.toml when empty.
func IngestMeetings(brainConfigPath, sphere string, paths []string, sourcesPath string) (map[string]interface{}, error) {
	cfg, err := brain.LoadConfig(brainConfigPath)
	if err != nil {
		return nil, err
	}
	summary, err := ingestMeetingsPaths(cfg, sphere, paths, sourcesPath, sourcesPath != "")
	if err != nil {
		return nil, err
	}
	return ingestMeetingsResult(summary, sphere), nil
}

func ingestMeetingsTool(cfg *brain.Config, sphere string, paths []string, configPath string, configExplicit bool) (map[string]interface{}, error) {
	summary, err := ingestMeetingsPaths(cfg, sphere, paths, configPath, configExplicit)
	if err != nil {
		return nil, err
	}
	return ingestMeetingsResult(summary, sphere), nil
}

func ingestMeetingsPaths(cfg *brain.Config, sphere string, paths []string, configPath string, configExplicit bool) (meetingsIngestSummary, error) {
	summary := meetingsIngestSummary{}
	bindings, err := loadMeetingBindings(cfg, sphere)
	if err != nil {
		return summary, err
	}
	meetingsCfg, err := loadMeetingsSphereConfig(configPath, configExplicit, sphere)
	if err != nil {
		return summary, err
	}
	candidates, err := loadBrainPeopleCandidates(cfg, sphere)
	if err != nil {
		return summary, err
	}
	resolved, walked, err := resolveMeetingPaths(cfg, sphere, paths, meetingsCfg.MeetingsRoot)
	if err != nil {
		return summary, err
	}
	summary.Walked = walked
	if len(resolved) == 0 {
		return summary, nil
	}
	ctx := meetingIngestContext{
		cfg:        cfg,
		sphere:     sphere,
		bindings:   bindings,
		sphereCfg:  meetingsCfg,
		candidates: candidates,
		now:        time.Now().UTC().Format(time.RFC3339),
	}
	for _, rel := range resolved {
		if err := ctx.processNote(&summary, rel); err != nil {
			return summary, err
		}
	}
	return summary, nil
}

func (c *meetingIngestContext) processNote(summary *meetingsIngestSummary, rel string) error {
	resolved, data, err := brain.ReadNoteFile(c.cfg, brain.Sphere(c.sphere), rel)
	if err != nil {
		return err
	}
	slug := meetingSlugFromRel(resolved.Rel)
	updated, tasks, changed := meetings.AssignIDs(slug, string(data))
	if changed {
		if err := os.WriteFile(resolved.Path, []byte(updated), 0o644); err != nil {
			return err
		}
		summary.Stamped = append(summary.Stamped, resolved.Rel)
	}
	for _, task := range tasks {
		owner := meetings.ResolvePerson(c.sphereCfg.ResolveAlias(task.Person), c.sphereCfg.OwnerAliases, c.candidates)
		task.Person = owner
		if err := c.processTask(summary, slug, resolved.Rel, task); err != nil {
			return err
		}
	}
	return nil
}

func (c *meetingIngestContext) processTask(summary *meetingsIngestSummary, slug, sourceRel string, task meetings.Task) error {
	binding := braingtd.SourceBinding{
		Provider:  meetingsProvider,
		Ref:       slug + "#" + task.ID,
		Location:  braingtd.BindingLocation{Path: sourceRel, Anchor: meetings.FormatComment(task.ID)},
		Writeable: true,
	}
	if note, ok := c.bindings.byStableID[binding.StableID()]; ok {
		return c.applyExisting(summary, note, task, false)
	}
	if note, ok := c.bindings.takeLegacy(slug, task.Person); ok {
		summary.LegacyHit = append(summary.LegacyHit, note.Entry.Path)
		return c.applyExisting(summary, note, task, true)
	}
	if task.Done {
		return nil
	}
	created, err := createMeetingCommitment(c.cfg, c.sphere, slug, sourceRel, task, binding)
	if err != nil {
		return err
	}
	summary.Created = append(summary.Created, created)
	summary.Affected = append(summary.Affected, created)
	return nil
}

func (c *meetingIngestContext) applyExisting(summary *meetingsIngestSummary, note dedupNote, task meetings.Task, fromLegacy bool) error {
	closed, err := closeIfDone(note, task, c.now)
	if err != nil {
		return err
	}
	if closed {
		summary.Closed = append(summary.Closed, note.Entry.Path)
		summary.Affected = append(summary.Affected, note.Entry.Path)
		return nil
	}
	if fromLegacy {
		summary.Affected = append(summary.Affected, note.Entry.Path)
	}
	summary.Skipped = append(summary.Skipped, note.Entry.Path)
	return nil
}

func loadMeetingBindings(cfg *brain.Config, sphere string) (meetingBindingIndex, error) {
	vault, ok := cfg.Vault(brain.Sphere(sphere))
	if !ok {
		return meetingBindingIndex{}, fmt.Errorf("unknown vault %q", sphere)
	}
	notes, err := readDedupNotes(vault)
	if err != nil {
		return meetingBindingIndex{}, err
	}
	index := meetingBindingIndex{
		byStableID: make(map[string]dedupNote, len(notes)),
		legacy:     make(map[meetings.LegacyRefKey][]dedupNote),
	}
	for _, note := range notes {
		for _, binding := range note.Entry.Commitment.SourceBindings {
			if !strings.EqualFold(binding.Provider, meetingsProvider) {
				continue
			}
			id := binding.StableID()
			if id == "" {
				continue
			}
			if isStableMeetingsRef(binding.Ref) {
				index.byStableID[id] = note
				continue
			}
			if key, ok := meetings.ParseLegacyRef(binding.Ref); ok {
				index.legacy[key] = append(index.legacy[key], note)
			}
		}
	}
	return index, nil
}

func (i *meetingBindingIndex) takeLegacy(slug, person string) (dedupNote, bool) {
	key := meetings.LegacyRefKey{Slug: slug, Person: meetings.NormalizePersonName(person)}
	if i.legacy == nil {
		return dedupNote{}, false
	}
	queue, ok := i.legacy[key]
	if !ok || len(queue) == 0 {
		return dedupNote{}, false
	}
	note := queue[0]
	i.legacy[key] = queue[1:]
	return note, true
}

func isStableMeetingsRef(ref string) bool {
	clean := strings.TrimSpace(ref)
	return clean != "" && strings.Contains(clean, "#")
}

func loadMeetingsSphereConfig(path string, explicit bool, sphere string) (meetings.SphereConfig, error) {
	resolved, hadExplicit, err := sloptoolsConfigPath(path, "sources.toml")
	if err != nil {
		return meetings.SphereConfig{}, err
	}
	cfg, err := meetings.Load(resolved, hadExplicit || explicit)
	if err != nil {
		return meetings.SphereConfig{}, err
	}
	if entry, ok := cfg.Sphere(sphere); ok {
		return entry, nil
	}
	return meetings.SphereConfig{Sphere: strings.ToLower(strings.TrimSpace(sphere)), ShortMemoSeconds: meetings.DefaultShortMemoSeconds}, nil
}

func loadBrainPeopleCandidates(cfg *brain.Config, sphere string) ([]string, error) {
	vault, ok := cfg.Vault(brain.Sphere(sphere))
	if !ok {
		return nil, fmt.Errorf("unknown vault %q", sphere)
	}
	dir := filepath.Join(vault.BrainRoot(), "people")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		out = append(out, strings.TrimSuffix(entry.Name(), ".md"))
	}
	sort.Strings(out)
	return out, nil
}

// resolveMeetingPaths returns the inputs the ingest pass should process.
// Explicit paths pass through unchanged so callers can mix vault-
// relative refs with absolute paths. When no paths are provided the
// configured meetings_root is walked and the discovered absolute paths
// are returned, alongside their vault-relative form for the audit trail.
func resolveMeetingPaths(cfg *brain.Config, sphere string, paths []string, meetingsRoot string) ([]string, []string, error) {
	if len(paths) > 0 {
		return paths, nil, nil
	}
	if strings.TrimSpace(meetingsRoot) == "" {
		return nil, nil, errors.New("paths are required: configure meetings_root in sources.toml or pass `paths`")
	}
	vault, ok := cfg.Vault(brain.Sphere(sphere))
	if !ok {
		return nil, nil, fmt.Errorf("unknown vault %q", sphere)
	}
	discovered, err := meetings.Discover(meetingsRoot)
	if err != nil {
		return nil, nil, err
	}
	walked := make([]string, 0, len(discovered.Paths))
	for _, abs := range discovered.Paths {
		rel, err := filepath.Rel(vault.Root, abs)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		walked = append(walked, filepath.ToSlash(rel))
	}
	return discovered.Paths, walked, nil
}

func closeIfDone(note dedupNote, task meetings.Task, now string) (bool, error) {
	if !task.Done {
		return false, nil
	}
	commitment := note.Entry.Commitment
	if commitmentClosed(commitment) {
		return false, nil
	}
	commitment.LocalOverlay.Status = "closed"
	commitment.LocalOverlay.ClosedAt = now
	commitment.LocalOverlay.ClosedVia = "brain.gtd.ingest"
	note.Entry.Commitment = commitment
	if err := writeDedupNotes(note); err != nil {
		return false, err
	}
	return true, nil
}

func createMeetingCommitment(cfg *brain.Config, sphere, slug, sourceRel string, task meetings.Task, binding braingtd.SourceBinding) (string, error) {
	out := filepath.ToSlash(filepath.Join("brain", "gtd", "ingest", slug+"-"+task.ID+".md"))
	target, err := brain.ResolveNotePath(cfg, brain.Sphere(sphere), out)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(target.Path), 0o755); err != nil {
		return "", err
	}
	rendered := renderMeetingCommitment(sphere, sourceRel, task, binding)
	if err := validateRenderedBrainGTD(rendered); err != nil {
		return "", err
	}
	if err := os.WriteFile(target.Path, []byte(rendered), 0o644); err != nil {
		return "", err
	}
	return target.Rel, nil
}

func renderMeetingCommitment(sphere, sourceRel string, task meetings.Task, binding braingtd.SourceBinding) string {
	people := ""
	if person := strings.TrimSpace(task.Person); person != "" {
		people = fmt.Sprintf("people:\n  - %q\n", person)
	}
	dueLine := ""
	if task.Due != "" {
		dueLine = "due: " + task.Due + "\n"
	}
	followLine := ""
	if task.FollowUp != "" {
		followLine = "follow_up: " + task.FollowUp + "\n"
	}
	projectLine := ""
	if task.Project != "" {
		projectLine = fmt.Sprintf("project: %q\n", task.Project)
	}
	return strings.TrimSpace(fmt.Sprintf(`---
kind: commitment
sphere: %s
title: %q
status: inbox
context: meetings
%s%s%s%ssource_bindings:
  - provider: %s
    ref: %q
    writeable: true
    location:
      path: %s
      anchor: %q
---
# %s

## Summary
Meetings task from %s.

## Next Action
- [ ] %s

## Evidence
- %s

## Linked Items
- None.

## Review Notes
- Ingested from meetings notes.
`,
		sphere,
		task.Text,
		people,
		dueLine,
		followLine,
		projectLine,
		binding.Provider,
		binding.Ref,
		binding.Location.Path,
		binding.Location.Anchor,
		task.Text,
		sourceRel,
		task.Text,
		sourceRel,
	)) + "\n"
}

func meetingSlugFromRel(rel string) string {
	rel = strings.TrimSpace(filepath.ToSlash(rel))
	rel = strings.TrimSuffix(rel, ".md")
	if rel == "" {
		return "meeting"
	}
	base := filepath.Base(rel)
	if strings.EqualFold(base, "MEETING_NOTES") || base == "" {
		parent := filepath.Base(filepath.Dir(rel))
		if parent != "" && parent != "." && parent != "/" {
			return slugify(parent)
		}
	}
	return slugify(base)
}
