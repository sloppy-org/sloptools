package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/sloppy-org/sloptools/internal/brain"
	braingtd "github.com/sloppy-org/sloptools/internal/brain/gtd"
	"github.com/sloppy-org/sloptools/internal/braincatalog"
)

func cmdBrainGTDIngest(args []string) int {
	fs := flag.NewFlagSet("brain gtd ingest", flag.ContinueOnError)
	configPath := fs.String("config", "", "vault config path")
	sphere := fs.String("sphere", "", "vault sphere")
	source := fs.String("source", "", "ingest source")
	var paths stringSliceFlag
	fs.Var(&paths, "path", "source note path (repeatable)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*sphere) == "" {
		fmt.Fprintln(os.Stderr, "--sphere is required")
		return 2
	}
	if strings.TrimSpace(*source) == "" {
		fmt.Fprintln(os.Stderr, "--source is required")
		return 2
	}
	if strings.ToLower(strings.TrimSpace(*source)) != "meetings" {
		fmt.Fprintln(os.Stderr, "unsupported ingest source")
		return 1
	}
	cfg, err := brain.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if len(paths) == 0 {
		fmt.Fprintln(os.Stderr, "--path is required")
		return 2
	}
	created := make([]string, 0)
	for _, rawPath := range paths {
		resolved, data, err := brain.ReadNoteFile(cfg, brain.Sphere(*sphere), rawPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		tasks := braincatalog.ExtractMeetingTasks(string(data))
		for i, task := range tasks {
			output := filepath.ToSlash(filepath.Join("brain", "gtd", "ingest", slugify(filepath.Base(resolved.Rel))+"-"+fmt.Sprintf("%02d", i+1)+".md"))
			target, err := brain.ResolveNotePath(cfg, brain.Sphere(*sphere), output)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
			if err := os.MkdirAll(filepath.Dir(target.Path), 0o755); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
			if err := os.WriteFile(target.Path, []byte(renderIngestCommitmentCLI(*sphere, resolved.Rel, task)), 0o644); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
			created = append(created, target.Rel)
		}
	}
	return printBrainJSON(map[string]interface{}{"sphere": *sphere, "source": *source, "count": len(created), "paths": created, "updated": len(created) > 0})
}

func resurfaceOneCommitment(cfg *brain.Config, sphere brain.Sphere, path string) bool {
	resolved, data, err := brain.ReadNoteFile(cfg, sphere, path)
	if err != nil {
		return false
	}
	commitment, note, diags := braingtd.ParseCommitmentMarkdown(string(data))
	if len(diags) != 0 {
		return false
	}
	if !resurfaceCommitment(commitment, time.Now().UTC()) {
		return false
	}
	if err := braingtd.ApplyCommitment(note, *commitment); err != nil {
		return false
	}
	rendered, err := note.Render()
	if err != nil {
		return false
	}
	if err := os.WriteFile(resolved.Path, []byte(rendered), 0o644); err != nil {
		return false
	}
	return true
}

func splitCommaList(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if clean := strings.TrimSpace(part); clean != "" {
			out = append(out, clean)
		}
	}
	return out
}

type stringSliceFlag []string

func (s *stringSliceFlag) String() string {
	return strings.Join(*s, ",")
}

func (s *stringSliceFlag) Set(value string) error {
	if clean := strings.TrimSpace(value); clean != "" {
		*s = append(*s, clean)
	}
	return nil
}

func renderIngestCommitmentCLI(sphere, meetingRel string, task braincatalog.MeetingTask) string {
	return strings.TrimSpace(fmt.Sprintf(`---
kind: commitment
sphere: %s
title: %s
status: inbox
context: meeting
source_bindings:
  - provider: meetings
    ref: %q
    location:
      path: %s
      anchor: %q
---
# %s

## Summary
Meeting task from %s.

## Next Action
- [ ] %s

## Evidence
- %s#L%d

## Linked Items
- None.

## Review Notes
- Ingested from meeting notes.
`, sphere, strconv.Quote(task.Text), meetingRel+"#"+strconv.Itoa(task.Line), meetingRel, task.Text, task.Text, meetingRel, task.Text, meetingRel, task.Line))
}

func writeCommitmentFrontMatter(note *brain.MarkdownNote, commitment braingtd.Commitment) error {
	for key, value := range map[string]interface{}{
		"kind":             commitment.Kind,
		"title":            commitment.Title,
		"sphere":           commitment.Sphere,
		"status":           commitment.Status,
		"outcome":          commitment.Outcome,
		"next_action":      commitment.NextAction,
		"context":          commitment.Context,
		"follow_up":        commitment.FollowUp,
		"due":              commitment.Due,
		"actor":            commitment.Actor,
		"waiting_for":      commitment.WaitingFor,
		"project":          commitment.Project,
		"last_evidence_at": commitment.LastEvidenceAt,
		"review_state":     commitment.ReviewState,
		"people":           commitment.People,
		"labels":           commitment.Labels,
	} {
		if err := note.SetFrontMatterField(key, value); err != nil {
			return err
		}
	}
	return nil
}

func resurfaceCommitment(commitment *braingtd.Commitment, now time.Time) bool {
	if commitment == nil {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(commitment.LocalOverlay.Status), "deferred") {
		return false
	}
	followUp := strings.TrimSpace(commitment.FollowUp)
	if followUp == "" {
		return false
	}
	dueDate := followUp
	if len(dueDate) >= len("2006-01-02T15:04:05Z07:00") {
		dueDate = dueDate[:10]
	}
	parsed, err := time.Parse("2006-01-02", dueDate)
	if err != nil {
		return false
	}
	if parsed.After(now.UTC()) {
		return false
	}
	commitment.LocalOverlay.Status = "next"
	return true
}

func slugify(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case unicode.IsSpace(r) || r == '-' || r == '_' || r == '.':
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "item"
	}
	return out
}
