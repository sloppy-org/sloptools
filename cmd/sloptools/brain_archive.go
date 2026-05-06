package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/sloppy-org/sloptools/internal/brain"
)

// cmdBrainArchive dispatches `sloptools brain archive` subcommands. The
// three-stage flow (plan -> optional edit -> apply) parallels
// `brain consolidate apply --merge`: emit a digest-bound plan, hand off
// for human/Codex revision of the Distilled facts, then apply with the
// same digest under the integrity gate.
func cmdBrainArchive(args []string) int {
	fs := flag.NewFlagSet("brain archive", flag.ContinueOnError)
	configPath := fs.String("config", "", "vault config path")
	sphere := fs.String("sphere", "", "vault sphere: work or private")
	source := fs.String("from", "", "vault-relative source under <vault>/archive/")
	dryRun := fs.Bool("dry-run", false, "emit the archive plan as JSON without writing anything")
	apply := fs.Bool("apply", false, "execute the plan (requires --confirm DIGEST)")
	confirm := fs.String("confirm", "", "plan digest from a prior --dry-run")
	gigaHost := fs.String("giga-host", "giga", "ssh host name for cold archive")
	gigaBase := fs.String("giga-archive-base", "/mnt/files/archive/chris", "remote archive base")
	stagingDir := fs.String("staging-dir", "", "local staging dir (default $TMPDIR)")
	skipGate := fs.Bool("no-validate-after", false, "skip the post-apply integrity gate")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*sphere) == "" {
		fmt.Fprintln(os.Stderr, "--sphere is required")
		return 2
	}
	if strings.TrimSpace(*source) == "" {
		fmt.Fprintln(os.Stderr, "--from is required (path under <vault>/archive/)")
		return 2
	}
	if *apply == *dryRun {
		fmt.Fprintln(os.Stderr, "exactly one of --dry-run or --apply is required")
		return 2
	}
	if *apply && strings.TrimSpace(*confirm) == "" {
		fmt.Fprintln(os.Stderr, "--apply requires --confirm DIGEST")
		return 2
	}
	cfg, err := brain.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	plan, err := brain.PlanArchive(cfg, brain.Sphere(*sphere), *source)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if *dryRun {
		return printBrainJSON(plan)
	}
	commitMsg := fmt.Sprintf("brain archive: %s -> %s", plan.Source, plan.GigaDest)
	ac := brain.ArchiveApplyConfig{
		GigaHost:        *gigaHost,
		GigaArchiveBase: *gigaBase,
		StagingDir:      *stagingDir,
	}
	if err := applyIntegrityGate(cfg, brain.Sphere(*sphere), *skipGate, commitMsg, func() error {
		return brain.ApplyArchive(cfg, plan, *confirm, ac)
	}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return printBrainJSON(map[string]interface{}{
		"applied":   true,
		"sphere":    plan.Sphere,
		"source":    plan.Source,
		"giga_dest": plan.GigaDest,
		"event":     plan.EventNotePath,
		"backlinks": len(plan.BacklinkEdits),
		"digest":    plan.Digest,
	})
}
