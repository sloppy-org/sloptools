package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/brain"
)

// cmdBrainSleep dispatches `sloptools brain sleep` and orchestrates the
// dream prune-links scan + apply, the dream report, and an optional
// Codex CLI judge pass over the rendered Markdown packet.
func cmdBrainSleep(args []string) int {
	fs := flag.NewFlagSet("brain sleep", flag.ContinueOnError)
	configPath := fs.String("config", "", "vault config path")
	sphere := fs.String("sphere", "", "vault sphere: work or private")
	budget := fs.Int("budget", brain.SleepDefaultBudget, "notes to dream over")
	backend := fs.String("backend", brain.SleepBackendCodex, "codex or none")
	model := fs.String("model", brain.SleepDefaultModel, "codex model (e.g. gpt-5.5)")
	dryRun := fs.Bool("dry-run", false, "skip LLM, do not apply prune-links, do not write report file")
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
	var res *brain.SleepResult
	run := func() error {
		var runErr error
		res, runErr = brain.RunSleep(cfg, brain.SleepOpts{
			Sphere:  brain.Sphere(*sphere),
			Budget:  *budget,
			Backend: *backend,
			Model:   *model,
			DryRun:  *dryRun,
		})
		return runErr
	}
	if *dryRun {
		if err := run(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return printBrainJSON(res)
	}
	commitMsg := fmt.Sprintf("brain sleep: %s %s", brain.Sphere(*sphere), time.Now().Format("2006-01-02"))
	if err := applyIntegrityGate(cfg, brain.Sphere(*sphere), false, commitMsg, run); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return printBrainJSON(res)
}
