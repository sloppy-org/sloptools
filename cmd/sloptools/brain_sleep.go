package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

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
	res, err := brain.RunSleep(cfg, brain.SleepOpts{
		Sphere:  brain.Sphere(*sphere),
		Budget:  *budget,
		Backend: *backend,
		Model:   *model,
		DryRun:  *dryRun,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return printBrainJSON(res)
}
