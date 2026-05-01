package mcp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/brain"
	braingtd "github.com/sloppy-org/sloptools/internal/brain/gtd"
	"github.com/sloppy-org/sloptools/internal/meetings"
)

const meetingsProvider = "meetings"

type meetingsIngestSummary struct {
	Created  []string `json:"created"`
	Closed   []string `json:"closed"`
	Skipped  []string `json:"skipped"`
	Stamped  []string `json:"stamped"`
	Affected []string `json:"affected"`
}

func (s *Server) ingestMeetings(cfg *brain.Config, sphere string, paths []string) (map[string]interface{}, error) {
	summary, err := s.ingestMeetingsPaths(cfg, sphere, paths)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"sphere":  sphere,
		"source":  meetingsProvider,
		"count":   len(summary.Affected),
		"paths":   summary.Affected,
		"created": summary.Created,
		"closed":  summary.Closed,
		"skipped": summary.Skipped,
		"stamped": summary.Stamped,
		"updated": len(summary.Affected) > 0 || len(summary.Stamped) > 0,
	}, nil
}

func (s *Server) ingestMeetingsPaths(cfg *brain.Config, sphere string, paths []string) (meetingsIngestSummary, error) {
	summary := meetingsIngestSummary{}
	existing, err := s.loadMeetingBindings(cfg, sphere)
	if err != nil {
		return summary, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, raw := range paths {
		resolved, data, err := brain.ReadNoteFile(cfg, brain.Sphere(sphere), raw)
		if err != nil {
			return summary, err
		}
		slug := meetingSlugFromRel(resolved.Rel)
		updated, tasks, changed := meetings.AssignIDs(slug, string(data))
		if changed {
			if err := os.WriteFile(resolved.Path, []byte(updated), 0o644); err != nil {
				return summary, err
			}
			summary.Stamped = append(summary.Stamped, resolved.Rel)
		}
		if len(tasks) == 0 {
			continue
		}
		for _, task := range tasks {
			ref := slug + "#" + task.ID
			binding := braingtd.SourceBinding{
				Provider:  meetingsProvider,
				Ref:       ref,
				Location:  braingtd.BindingLocation{Path: resolved.Rel, Anchor: meetings.FormatComment(task.ID)},
				Writeable: true,
			}
			note, found := existing[binding.StableID()]
			if found {
				closed, err := closeIfDone(note, task, now)
				if err != nil {
					return summary, err
				}
				if closed {
					summary.Closed = append(summary.Closed, note.Entry.Path)
					summary.Affected = append(summary.Affected, note.Entry.Path)
				} else {
					summary.Skipped = append(summary.Skipped, note.Entry.Path)
				}
				continue
			}
			if task.Done {
				continue
			}
			created, err := createMeetingCommitment(cfg, sphere, slug, resolved.Rel, task, binding)
			if err != nil {
				return summary, err
			}
			summary.Created = append(summary.Created, created)
			summary.Affected = append(summary.Affected, created)
		}
	}
	return summary, nil
}

func (s *Server) loadMeetingBindings(cfg *brain.Config, sphere string) (map[string]dedupNote, error) {
	vault, ok := cfg.Vault(brain.Sphere(sphere))
	if !ok {
		return nil, fmt.Errorf("unknown vault %q", sphere)
	}
	notes, err := readDedupNotes(vault)
	if err != nil {
		return nil, err
	}
	out := make(map[string]dedupNote, len(notes))
	for _, note := range notes {
		for _, binding := range note.Entry.Commitment.SourceBindings {
			if !strings.EqualFold(binding.Provider, meetingsProvider) {
				continue
			}
			id := binding.StableID()
			if id == "" {
				continue
			}
			out[id] = note
		}
	}
	return out, nil
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
