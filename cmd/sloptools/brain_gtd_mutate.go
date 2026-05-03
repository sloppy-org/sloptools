package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/brain"
	braingtd "github.com/sloppy-org/sloptools/internal/brain/gtd"
	"github.com/sloppy-org/sloptools/internal/braincatalog"
)

func cmdBrainGTDWrite(args []string) int {
	fs := flag.NewFlagSet("brain gtd write", flag.ContinueOnError)
	configPath := fs.String("config", "", "vault config path")
	sphere := fs.String("sphere", "", "vault sphere")
	path := fs.String("path", "", "GTD note path")
	title := fs.String("title", "", "commitment title")
	status := fs.String("status", "", "commitment status")
	outcome := fs.String("outcome", "", "commitment outcome")
	nextAction := fs.String("next-action", "", "commitment next action")
	context := fs.String("context", "", "commitment context")
	followUp := fs.String("follow-up", "", "commitment follow-up")
	due := fs.String("due", "", "commitment due date")
	actor := fs.String("actor", "", "commitment actor")
	waitingFor := fs.String("waiting-for", "", "commitment waiting-for")
	project := fs.String("project", "", "commitment project")
	track := fs.String("track", "", "commitment track")
	clearTrack := fs.Bool("clear-track", false, "clear commitment track")
	lastEvidenceAt := fs.String("last-evidence-at", "", "commitment last evidence timestamp")
	reviewState := fs.String("review-state", "", "commitment review state")
	people := fs.String("people", "", "comma-separated people")
	labels := fs.String("labels", "", "comma-separated labels")
	bindingProvider := fs.String("binding-provider", "", "source binding provider")
	bindingRef := fs.String("binding-ref", "", "source binding ref")
	bindingURL := fs.String("binding-url", "", "source binding URL")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*sphere) == "" {
		fmt.Fprintln(os.Stderr, "--sphere is required")
		return 2
	}
	if strings.TrimSpace(*path) == "" {
		fmt.Fprintln(os.Stderr, "--path is required")
		return 2
	}
	cfg, err := brain.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	resolved, err := brain.ResolveNotePath(cfg, brain.Sphere(*sphere), *path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	body, readErr := os.ReadFile(resolved.Path)
	updated := braingtd.Commitment{Kind: "commitment", Sphere: *sphere}
	var note *brain.MarkdownNote
	var diags []brain.MarkdownDiagnostic
	if readErr == nil {
		commitment, parsedNote, parsedDiags := braingtd.ParseCommitmentMarkdown(string(body))
		if len(parsedDiags) != 0 {
			return printBrainJSON(map[string]interface{}{
				"source":      resolved,
				"diagnostics": parsedDiags,
				"count":       len(parsedDiags),
			})
		}
		updated = *commitment
		note = parsedNote
		diags = parsedDiags
	} else if !errors.Is(readErr, os.ErrNotExist) {
		fmt.Fprintln(os.Stderr, readErr)
		return 1
	}
	if strings.TrimSpace(*title) != "" {
		updated.Title = strings.TrimSpace(*title)
	}
	if strings.TrimSpace(*status) != "" {
		updated.Status = strings.TrimSpace(*status)
	}
	if strings.TrimSpace(*outcome) != "" {
		updated.Outcome = strings.TrimSpace(*outcome)
	}
	if strings.TrimSpace(*nextAction) != "" {
		updated.NextAction = strings.TrimSpace(*nextAction)
	}
	if strings.TrimSpace(*context) != "" {
		updated.Context = strings.TrimSpace(*context)
	}
	if strings.TrimSpace(*followUp) != "" {
		updated.FollowUp = strings.TrimSpace(*followUp)
	}
	if strings.TrimSpace(*due) != "" {
		updated.Due = strings.TrimSpace(*due)
	}
	if strings.TrimSpace(*actor) != "" {
		updated.Actor = strings.TrimSpace(*actor)
	}
	if strings.TrimSpace(*waitingFor) != "" {
		updated.WaitingFor = strings.TrimSpace(*waitingFor)
	}
	if strings.TrimSpace(*project) != "" {
		updated.Project = strings.TrimSpace(*project)
	}
	if strings.TrimSpace(*lastEvidenceAt) != "" {
		updated.LastEvidenceAt = strings.TrimSpace(*lastEvidenceAt)
	}
	if strings.TrimSpace(*reviewState) != "" {
		updated.ReviewState = strings.TrimSpace(*reviewState)
	}
	if strings.TrimSpace(*people) != "" {
		updated.People = splitCommaList(*people)
	}
	if strings.TrimSpace(*labels) != "" {
		updated.Labels = splitCommaList(*labels)
	}
	if *clearTrack {
		updated.Labels = braingtd.WithTrackLabel(updated.Labels, "")
		updated.Track = ""
	} else if strings.TrimSpace(*track) != "" {
		updated.Labels = braingtd.WithTrackLabel(updated.Labels, strings.TrimSpace(*track))
		updated.Track = ""
	}
	if strings.TrimSpace(*bindingProvider) != "" || strings.TrimSpace(*bindingRef) != "" || strings.TrimSpace(*bindingURL) != "" {
		updated.SourceBindings = []braingtd.SourceBinding{{
			Provider: strings.TrimSpace(*bindingProvider),
			Ref:      strings.TrimSpace(*bindingRef),
			URL:      strings.TrimSpace(*bindingURL),
		}}
	}
	if readErr == nil {
		if err := writeCommitmentFrontMatter(note, updated); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if err := braingtd.ApplyCommitment(note, updated); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		rendered, err := note.Render()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if err := validateRenderedBrainGTD(rendered); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if err := os.WriteFile(resolved.Path, []byte(rendered), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return printBrainJSON(map[string]interface{}{
			"source":      resolved,
			"commitment":  updated,
			"diagnostics": diags,
			"count":       len(diags),
			"valid":       len(diags) == 0,
		})
	}
	rendered, err := braincatalog.BuildGTDCommitmentMarkdown(updated)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := validateRenderedBrainGTD(rendered); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := os.MkdirAll(filepath.Dir(resolved.Path), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := os.WriteFile(resolved.Path, []byte(rendered), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return printBrainJSON(map[string]interface{}{
		"source":      resolved,
		"commitment":  updated,
		"diagnostics": []brain.MarkdownDiagnostic{},
		"count":       0,
		"valid":       true,
	})
}

func cmdBrainGTDOrganize(args []string) int {
	fs := flag.NewFlagSet("brain gtd organize", flag.ContinueOnError)
	configPath := fs.String("config", "", "vault config path")
	sphere := fs.String("sphere", "", "vault sphere")
	path := fs.String("path", "", "output note path")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*sphere) == "" {
		fmt.Fprintln(os.Stderr, "--sphere is required")
		return 2
	}
	cfg, err := brain.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	items, err := braincatalog.ListGTDVault(cfg, brain.Sphere(*sphere), braincatalog.GTDListFilter{})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	output := strings.TrimSpace(*path)
	if output == "" {
		output = filepath.ToSlash(filepath.Join("brain", "gtd", "organize.md"))
	}
	resolved, err := brain.ResolveNotePath(cfg, brain.Sphere(*sphere), output)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := os.MkdirAll(filepath.Dir(resolved.Path), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	rendered := braincatalog.BuildGTDIndexMarkdown(items, *sphere)
	if err := validateRenderedBrainNote(rendered); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := os.WriteFile(resolved.Path, []byte(rendered), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return printBrainJSON(map[string]interface{}{"sphere": *sphere, "path": resolved.Rel, "count": len(items), "updated": true})
}

func cmdBrainGTDResurface(args []string) int {
	fs := flag.NewFlagSet("brain gtd resurface", flag.ContinueOnError)
	configPath := fs.String("config", "", "vault config path")
	sphere := fs.String("sphere", "", "vault sphere")
	path := fs.String("path", "", "optional commitment path")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*sphere) == "" {
		fmt.Fprintln(os.Stderr, "--sphere is required")
		return 2
	}
	cfg, err := brain.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	changed := make([]string, 0)
	if strings.TrimSpace(*path) != "" {
		if resurfaceOneCommitment(cfg, brain.Sphere(*sphere), strings.TrimSpace(*path)) {
			changed = append(changed, strings.TrimSpace(*path))
		}
	} else {
		if err := brain.WalkVaultNotes(cfg, brain.Sphere(*sphere), func(snapshot brain.NoteSnapshot) error {
			if snapshot.Kind != "commitment" {
				return nil
			}
			commitment, note, diags := braingtd.ParseCommitmentMarkdown(snapshot.Body)
			if len(diags) != 0 {
				return nil
			}
			if !resurfaceCommitment(commitment, time.Now().UTC()) {
				return nil
			}
			if err := braingtd.ApplyCommitment(note, *commitment); err != nil {
				return err
			}
			rendered, err := note.Render()
			if err != nil {
				return err
			}
			if err := validateRenderedBrainGTD(rendered); err != nil {
				return err
			}
			if err := os.WriteFile(snapshot.Source.Path, []byte(rendered), 0o644); err != nil {
				return err
			}
			changed = append(changed, snapshot.Source.Rel)
			return nil
		}); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	return printBrainJSON(map[string]interface{}{"sphere": *sphere, "count": len(changed), "paths": changed, "updated": len(changed) > 0})
}

func cmdBrainGTDDashboard(args []string) int {
	fs := flag.NewFlagSet("brain gtd dashboard", flag.ContinueOnError)
	configPath := fs.String("config", "", "vault config path")
	sphere := fs.String("sphere", "", "vault sphere")
	name := fs.String("name", "", "dashboard subject")
	path := fs.String("path", "", "output note path")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*sphere) == "" {
		fmt.Fprintln(os.Stderr, "--sphere is required")
		return 2
	}
	if strings.TrimSpace(*name) == "" {
		fmt.Fprintln(os.Stderr, "--name is required")
		return 2
	}
	cfg, err := brain.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	items, err := braincatalog.ListGTDVault(cfg, brain.Sphere(*sphere), braincatalog.GTDListFilter{})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	output := strings.TrimSpace(*path)
	if output == "" {
		output = filepath.ToSlash(filepath.Join("brain", "gtd", "dashboards", slugify(*name)+".md"))
	}
	resolved, err := brain.ResolveNotePath(cfg, brain.Sphere(*sphere), output)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := os.MkdirAll(filepath.Dir(resolved.Path), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	rendered := braincatalog.BuildGTDDashboardMarkdown(items, *sphere, *name)
	if err := validateRenderedBrainNote(rendered); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := os.WriteFile(resolved.Path, []byte(rendered), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return printBrainJSON(map[string]interface{}{"sphere": *sphere, "name": *name, "path": resolved.Rel, "count": len(items), "updated": true})
}

func cmdBrainGTDReviewBatch(args []string) int {
	fs := flag.NewFlagSet("brain gtd review-batch", flag.ContinueOnError)
	configPath := fs.String("config", "", "vault config path")
	sphere := fs.String("sphere", "", "vault sphere")
	query := fs.String("q", "", "review query")
	track := fs.String("track", "", "attention track filter")
	path := fs.String("path", "", "output note path")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*sphere) == "" {
		fmt.Fprintln(os.Stderr, "--sphere is required")
		return 2
	}
	queryText := strings.TrimSpace(*query)
	trackText := strings.TrimSpace(*track)
	if queryText == "" && trackText != "" {
		queryText = trackText
	}
	if queryText == "" {
		fmt.Fprintln(os.Stderr, "--q or --track is required")
		return 2
	}
	cfg, err := brain.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	items, err := braincatalog.ListGTDVault(cfg, brain.Sphere(*sphere), braincatalog.GTDListFilter{Track: trackText})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	output := strings.TrimSpace(*path)
	if output == "" {
		output = filepath.ToSlash(filepath.Join("brain", "gtd", "reviews", slugify(queryText)+".md"))
	}
	resolved, err := brain.ResolveNotePath(cfg, brain.Sphere(*sphere), output)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := os.MkdirAll(filepath.Dir(resolved.Path), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	rendered := braincatalog.BuildGTDReviewBatchMarkdown(items, *sphere, queryText)
	if err := validateRenderedBrainNote(rendered); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := os.WriteFile(resolved.Path, []byte(rendered), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return printBrainJSON(map[string]interface{}{"sphere": *sphere, "q": queryText, "track": trackText, "path": resolved.Rel, "count": len(items), "updated": true})
}
