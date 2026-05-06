package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/brain"
)

const retiredStubMaxAge = 30 * 24 * time.Hour

// cmdBrainConsolidate dispatches the Phase 6 consolidation subcommands.
func cmdBrainConsolidate(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "brain consolidate requires a subcommand")
		return 2
	}
	switch args[0] {
	case "plan":
		return cmdBrainConsolidatePlan(args[1:])
	case "apply":
		return cmdBrainConsolidateApply(args[1:])
	case "prune-stubs":
		return cmdBrainConsolidatePruneStubs(args[1:])
	case "help", "-h", "--help":
		fmt.Println("sloptools brain consolidate <plan|apply|prune-stubs> [flags]")
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown brain consolidate subcommand: %s\n", args[0])
		return 2
	}
}

func cmdBrainConsolidatePlan(args []string) int {
	fs := flag.NewFlagSet("brain consolidate plan", flag.ContinueOnError)
	configPath := fs.String("config", "", "vault config path")
	sphere := fs.String("sphere", "", "vault sphere")
	limit := fs.Int("limit", 0, "maximum rows; 0 = all")
	outcome := fs.String("outcome", "", "filter by outcome (retire, consolidate, demote, archive, keep, delete)")
	tsv := fs.Bool("tsv", false, "emit TSV instead of JSON")
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
	rows, err := brain.ConsolidatePlan(cfg, brain.Sphere(*sphere))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	rows = filterConsolidateRows(rows, *outcome, *limit)
	if *tsv {
		writeConsolidateTSV(os.Stdout, rows)
		return 0
	}
	return printBrainJSON(map[string]interface{}{"sphere": *sphere, "items": rows, "count": len(rows)})
}

func filterConsolidateRows(rows []brain.ConsolidateRow, outcome string, limit int) []brain.ConsolidateRow {
	outcome = strings.TrimSpace(outcome)
	out := make([]brain.ConsolidateRow, 0, len(rows))
	for _, row := range rows {
		if outcome != "" && string(row.Outcome) != outcome {
			continue
		}
		out = append(out, row)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func writeConsolidateTSV(w *os.File, rows []brain.ConsolidateRow) {
	for _, row := range rows {
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\t%s\n",
			row.Sphere, row.Outcome, row.Score, row.Path,
			tsvEscape(row.Rationale), tsvEscape(row.Proposed),
		)
	}
}

func tsvEscape(value string) string {
	value = strings.ReplaceAll(value, "\t", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	return value
}

func cmdBrainConsolidateApply(args []string) int {
	fs := flag.NewFlagSet("brain consolidate apply", flag.ContinueOnError)
	configPath := fs.String("config", "", "vault config path")
	sphere := fs.String("sphere", "", "vault sphere")
	merge := fs.Bool("merge", false, "merge two notes")
	from := fs.String("from", "", "loser note path")
	into := fs.String("into", "", "survivor note path")
	dryRun := fs.Bool("dry-run", false, "print merge plan without applying")
	apply := fs.Bool("apply", false, "apply merge after manual conflict resolution")
	confirm := fs.String("confirm", "", "digest from dry-run plan")
	skipGate := fs.Bool("no-validate-after", false, "skip the post-apply integrity gate")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if !*merge {
		fmt.Fprintln(os.Stderr, "--merge is required")
		return 2
	}
	if strings.TrimSpace(*sphere) == "" || strings.TrimSpace(*from) == "" || strings.TrimSpace(*into) == "" {
		fmt.Fprintln(os.Stderr, "--sphere, --from, --into are required")
		return 2
	}
	if *dryRun == *apply {
		fmt.Fprintln(os.Stderr, "exactly one of --dry-run or --apply is required")
		return 2
	}
	cfg, err := brain.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	plan, err := brain.PrepareMerge(cfg, brain.Sphere(*sphere), *from, *into)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if *dryRun {
		return printBrainJSON(plan)
	}
	if strings.TrimSpace(*confirm) == "" {
		fmt.Fprintln(os.Stderr, "--confirm is required for --apply")
		return 2
	}
	commitMsg := fmt.Sprintf("brain consolidate: merged %s into %s", plan.Loser, plan.Survivor)
	if err := applyIntegrityGate(cfg, brain.Sphere(*sphere), *skipGate, commitMsg, func() error {
		return applyConsolidateMerge(cfg, brain.Sphere(*sphere), plan, *confirm)
	}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return printBrainJSON(map[string]interface{}{"applied": true, "loser": plan.Loser, "survivor": plan.Survivor})
}

// applyConsolidateMerge writes the merged frontmatter+body into the survivor
// and moves the loser to the retired bucket via ApplyMove.
func applyConsolidateMerge(cfg *brain.Config, sphere brain.Sphere, plan *brain.MergePlan, confirm string) error {
	survivorResolved, survivorData, err := brain.ReadNoteFile(cfg, sphere, plan.Survivor)
	if err != nil {
		return fmt.Errorf("read survivor: %w", err)
	}
	if brain.MergeBodyHasUnresolvedConflicts(string(survivorData)) {
		return fmt.Errorf("survivor still contains conflict markers; resolve them before --apply")
	}
	if plan.LinkPlan == nil {
		return fmt.Errorf("merge plan missing link plan")
	}
	merged := buildMergedFile(plan)
	if err := os.WriteFile(survivorResolved.Path, []byte(merged), 0o644); err != nil {
		return fmt.Errorf("write survivor: %w", err)
	}
	if err := brain.ApplyMove(cfg, plan.LinkPlan, confirm); err != nil {
		return fmt.Errorf("apply move: %w", err)
	}
	return nil
}

func buildMergedFile(plan *brain.MergePlan) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString(strings.TrimRight(plan.YAML, "\n"))
	b.WriteString("\n---\n")
	b.WriteString(strings.TrimLeft(plan.Body, "\n"))
	return b.String()
}

func cmdBrainConsolidatePruneStubs(args []string) int {
	fs := flag.NewFlagSet("brain consolidate prune-stubs", flag.ContinueOnError)
	configPath := fs.String("config", "", "vault config path")
	sphere := fs.String("sphere", "", "vault sphere")
	apply := fs.Bool("apply", false, "delete stale stubs (default is dry-run)")
	skipGate := fs.Bool("no-validate-after", false, "skip the post-apply integrity gate")
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
	stubs, err := brain.FindRetiredStubs(cfg, brain.Sphere(*sphere), retiredStubMaxAge, time.Now())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if !*apply {
		return printBrainJSON(map[string]interface{}{"sphere": *sphere, "stubs": stubs, "count": len(stubs)})
	}
	var deleted []string
	if err := applyIntegrityGate(cfg, brain.Sphere(*sphere), *skipGate, "brain consolidate prune-stubs", func() error {
		out, pruneErr := pruneRetiredStubs(cfg, brain.Sphere(*sphere), stubs)
		deleted = out
		return pruneErr
	}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return printBrainJSON(map[string]interface{}{"sphere": *sphere, "deleted": deleted, "count": len(deleted)})
}

func pruneRetiredStubs(cfg *brain.Config, sphere brain.Sphere, stubs []brain.PruneRetiredStub) ([]string, error) {
	deleted := make([]string, 0, len(stubs))
	for _, stub := range stubs {
		plan, err := brain.PlanMove(cfg, sphere, stub.Path, "/dev/null")
		if err != nil {
			return deleted, fmt.Errorf("plan delete %s: %w", stub.Path, err)
		}
		if err := brain.ApplyMove(cfg, plan, plan.Digest); err != nil {
			return deleted, fmt.Errorf("apply delete %s: %w", stub.Path, err)
		}
		deleted = append(deleted, filepath.ToSlash(stub.Path))
	}
	return deleted, nil
}
