package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/brain"
	braingtd "github.com/sloppy-org/sloptools/internal/brain/gtd"
	"github.com/sloppy-org/sloptools/internal/braincatalog"
)

func cmdBrainGTDList(args []string) int {
	fs := flag.NewFlagSet("brain gtd list", flag.ContinueOnError)
	configPath := fs.String("config", "", "vault config path")
	sphere := fs.String("sphere", "", "vault sphere")
	status := fs.String("status", "", "status filter")
	person := fs.String("person", "", "person filter")
	project := fs.String("project", "", "project filter")
	track := fs.String("track", "", "track filter")
	source := fs.String("source", "", "source filter")
	limit := fs.Int("limit", 0, "maximum results")
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
	items, err := braincatalog.ListGTDVault(cfg, brain.Sphere(*sphere), braincatalog.GTDListFilter{
		Status:  *status,
		Person:  *person,
		Project: *project,
		Track:   *track,
		Source:  *source,
		Limit:   *limit,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return printBrainJSON(map[string]interface{}{
		"sphere": *sphere,
		"filter": map[string]interface{}{
			"status":  *status,
			"person":  *person,
			"project": *project,
			"track":   *track,
			"source":  *source,
			"limit":   *limit,
		},
		"items": items,
		"count": len(items),
	})
}

func cmdBrainGTDUpdate(args []string) int {
	fs := flag.NewFlagSet("brain gtd update", flag.ContinueOnError)
	configPath := fs.String("config", "", "vault config path")
	sphere := fs.String("sphere", "", "vault sphere")
	path := fs.String("path", "", "GTD note path")
	status := fs.String("status", "", "overlay status")
	closedAt := fs.String("closed-at", "", "closed timestamp")
	closedVia := fs.String("closed-via", "", "closure source")
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
	if strings.TrimSpace(*status) == "" {
		fmt.Fprintln(os.Stderr, "--status is required")
		return 2
	}
	cfg, err := brain.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	resolved, body, err := brain.ReadNoteFile(cfg, brain.Sphere(*sphere), *path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	commitment, note, diags := braingtd.ParseCommitmentMarkdown(string(body))
	if len(diags) != 0 {
		if printBrainJSON(map[string]interface{}{
			"source":      resolved,
			"diagnostics": diags,
			"count":       len(diags),
		}) != 0 {
			return 1
		}
		return 1
	}
	commitment.LocalOverlay.Status = strings.TrimSpace(*status)
	if strings.TrimSpace(*closedAt) == "" && strings.TrimSpace(commitment.LocalOverlay.ClosedAt) == "" && closedStatus(*status) {
		commitment.LocalOverlay.ClosedAt = time.Now().UTC().Format(time.RFC3339)
	} else if strings.TrimSpace(*closedAt) != "" {
		commitment.LocalOverlay.ClosedAt = strings.TrimSpace(*closedAt)
	}
	if strings.TrimSpace(*closedVia) != "" {
		commitment.LocalOverlay.ClosedVia = strings.TrimSpace(*closedVia)
	} else if closedStatus(*status) {
		commitment.LocalOverlay.ClosedVia = "brain.gtd.update"
	}
	if err := braingtd.ApplyCommitment(note, *commitment); err != nil {
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
		"source":     resolved,
		"commitment": commitment,
		"status":     *status,
		"valid":      len(diags) == 0,
	})
}

func cmdBrainFolderAudit(args []string) int {
	fs := flag.NewFlagSet("brain folder audit", flag.ContinueOnError)
	configPath := fs.String("config", "", "vault config path")
	sphere := fs.String("sphere", "", "vault sphere")
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
	notes, err := brain.AuditFolderVault(cfg, brain.Sphere(*sphere))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	issues := 0
	for _, note := range notes {
		issues += note.Count
	}
	return printBrainJSON(map[string]interface{}{
		"sphere": *sphere,
		"notes":  notes,
		"count":  len(notes),
		"issues": issues,
		"valid":  issues == 0,
	})
}

func closedStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "closed", "done", "dropped":
		return true
	default:
		return false
	}
}
