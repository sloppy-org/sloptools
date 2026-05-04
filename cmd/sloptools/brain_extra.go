package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/brain"
	braingtd "github.com/sloppy-org/sloptools/internal/brain/gtd"
	"github.com/sloppy-org/sloptools/internal/braincatalog"
)

func cmdBrainIngest(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "brain ingest requires relation-candidates, runtime-plan, final-report, or stream-opencode-report")
		return 2
	}
	switch args[0] {
	case "folder-quality":
		return cmdBrainIngestFolderQuality(args[1:])
	case "folder-review-queue":
		return cmdBrainIngestFolderReviewQueue(args[1:])
	case "folder-review-apply":
		return cmdBrainIngestFolderReviewApply(args[1:])
	case "folder-review-packet":
		return cmdBrainIngestFolderReviewPacket(args[1:])
	case "folder-stability":
		return cmdBrainIngestFolderStability(args[1:])
	case "folder-units":
		return cmdBrainIngestFolderUnits(args[1:])
	case "archive-candidates":
		return cmdBrainIngestArchiveCandidates(args[1:])
	case "relation-candidates":
		return cmdBrainIngestRelations(args[1:])
	case "runtime-plan":
		return cmdBrainIngestRuntime(args[1:])
	case "final-report":
		return cmdBrainIngestFinalReport(args[1:])
	case "stream-opencode-report":
		return cmdBrainIngestStreamOpencode(args[1:])
	case "help", "-h", "--help":
		fmt.Println("sloptools brain ingest <folder-quality|folder-review-queue|folder-review-apply|folder-review-packet|folder-stability|folder-units|archive-candidates|relation-candidates|runtime-plan|final-report|stream-opencode-report> [flags]")
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown brain ingest subcommand: %s\n", args[0])
		return 2
	}
}

func cmdBrainIngestFolderQuality(args []string) int {
	cfg, sphere, status := brainIngestConfigSphere("brain ingest folder-quality", args)
	if status != 0 {
		return status
	}
	rows, err := brain.FolderQuality(cfg, brain.Sphere(sphere))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return printBrainJSON(map[string]interface{}{"sphere": sphere, "candidates": len(rows), "items": rows})
}

func cmdBrainIngestFolderReviewQueue(args []string) int {
	fs := flag.NewFlagSet("brain ingest folder-review-queue", flag.ContinueOnError)
	configPath := fs.String("config", "", "vault config path")
	sphere := fs.String("sphere", "", "vault sphere")
	limit := fs.Int("limit", 200, "maximum rows")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg, err := brain.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	rows, err := brain.FolderReviewQueue(cfg, brain.Sphere(*sphere), *limit)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return printBrainJSON(map[string]interface{}{"sphere": *sphere, "items": rows, "count": len(rows)})
}

func cmdBrainIngestFolderReviewApply(args []string) int {
	fs := flag.NewFlagSet("brain ingest folder-review-apply", flag.ContinueOnError)
	configPath := fs.String("config", "", "vault config path")
	sphere := fs.String("sphere", "", "vault sphere")
	path := fs.String("path", "", "folder note path")
	reviewPath := fs.String("review", "", "review output path")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg, err := brain.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	review, err := os.ReadFile(*reviewPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	resolved, changed, err := brain.ApplyFolderReview(cfg, brain.Sphere(*sphere), *path, string(review))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return printBrainJSON(map[string]interface{}{"source": resolved, "changed": changed})
}

func cmdBrainIngestFolderReviewPacket(args []string) int {
	fs := flag.NewFlagSet("brain ingest folder-review-packet", flag.ContinueOnError)
	configPath := fs.String("config", "", "vault config path")
	sphere := fs.String("sphere", "", "vault sphere")
	path := fs.String("path", "", "folder note path")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg, err := brain.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	packet, err := brain.FolderReviewPacket(cfg, brain.Sphere(*sphere), *path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Print(packet)
	return 0
}

func cmdBrainIngestFolderStability(args []string) int {
	cfg, sphere, status := brainIngestConfigSphere("brain ingest folder-stability", args)
	if status != 0 {
		return status
	}
	row, err := brain.FolderStability(cfg, brain.Sphere(sphere))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return printBrainJSON(row)
}

func cmdBrainIngestFolderUnits(args []string) int {
	fs := flag.NewFlagSet("brain ingest folder-units", flag.ContinueOnError)
	root := fs.String("root", ".", "brain-ingest root")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	issues, err := brain.ValidateWorkUnits(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return printBrainJSON(map[string]interface{}{"ok": len(issues) == 0, "issues": issues, "count": len(issues)})
}

func cmdBrainIngestArchiveCandidates(args []string) int {
	fs := flag.NewFlagSet("brain ingest archive-candidates", flag.ContinueOnError)
	root := fs.String("root", ".", "brain-ingest root")
	limit := fs.Int("limit", 160, "maximum rows")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rows, err := brain.ArchiveCandidates(*root, *limit)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return printBrainJSON(map[string]interface{}{"candidates": len(rows), "items": rows})
}

func cmdBrainIngestRelations(args []string) int {
	fs := flag.NewFlagSet("brain ingest relation-candidates", flag.ContinueOnError)
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
	rows, err := brain.RelationCandidates(cfg, brain.Sphere(*sphere))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return printBrainJSON(map[string]interface{}{"sphere": *sphere, "candidates": len(rows), "relations": rows})
}

func cmdBrainIngestRuntime(args []string) int {
	fs := flag.NewFlagSet("brain ingest runtime-plan", flag.ContinueOnError)
	configPath := fs.String("config", "", "vault config path")
	root := fs.String("root", ".", "brain-ingest root")
	slots := fs.Int("slots", 1, "parallel slots")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg, err := brain.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	plan, err := brain.RuntimeEstimate(*root, cfg, *slots)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return printBrainJSON(plan)
}

func cmdBrainIngestFinalReport(args []string) int {
	fs := flag.NewFlagSet("brain ingest final-report", flag.ContinueOnError)
	configPath := fs.String("config", "", "vault config path")
	root := fs.String("root", ".", "brain-ingest root")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg, err := brain.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return printBrainJSON(brain.BuildFinalReport(*root, cfg))
}

func cmdBrainIngestStreamOpencode(args []string) int {
	fs := flag.NewFlagSet("brain ingest stream-opencode-report", flag.ContinueOnError)
	reportPath := fs.String("report", "", "report output path")
	eventsPath := fs.String("events", "", "raw event output path")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*reportPath) == "" || strings.TrimSpace(*eventsPath) == "" {
		fmt.Fprintln(os.Stderr, "--report and --events are required")
		return 2
	}
	if err := os.MkdirAll(filepath.Dir(*reportPath), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := os.MkdirAll(filepath.Dir(*eventsPath), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	report, err := os.Create(*reportPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer report.Close()
	events, err := os.Create(*eventsPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer events.Close()
	if err := brain.StreamOpencodeReport(os.Stdin, report, events); err != nil && err != io.EOF {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return printBrainJSON(map[string]interface{}{"ok": true, "report": *reportPath, "events": *eventsPath})
}

func brainIngestConfigSphere(command string, args []string) (*brain.Config, string, int) {
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	configPath := fs.String("config", "", "vault config path")
	sphere := fs.String("sphere", "", "vault sphere")
	if err := fs.Parse(args); err != nil {
		return nil, "", 2
	}
	if strings.TrimSpace(*sphere) == "" {
		fmt.Fprintln(os.Stderr, "--sphere is required")
		return nil, "", 2
	}
	cfg, err := brain.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return nil, "", 1
	}
	return cfg, *sphere, 0
}

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
