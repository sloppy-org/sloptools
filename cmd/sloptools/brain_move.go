package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/sloppy-org/sloptools/internal/brain"
)

func cmdBrainMove(args []string) int {
	fs := flag.NewFlagSet("brain move", flag.ContinueOnError)
	configPath := fs.String("config", "", "vault config path")
	sphere := fs.String("sphere", "", "vault sphere: work or private")
	from := fs.String("from", "", "vault-relative source path")
	to := fs.String("to", "", "vault-relative destination path or /dev/null")
	dryRun := fs.Bool("dry-run", false, "print the move plan without applying")
	apply := fs.Bool("apply", false, "apply the move (requires --confirm)")
	confirm := fs.String("confirm", "", "plan digest from a prior --dry-run")
	skipGate := fs.Bool("no-validate-after", false, "skip the post-apply integrity gate (bulk runners only)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*sphere) == "" {
		fmt.Fprintln(os.Stderr, "--sphere is required")
		return 2
	}
	if strings.TrimSpace(*from) == "" || strings.TrimSpace(*to) == "" {
		fmt.Fprintln(os.Stderr, "--from and --to are required")
		return 2
	}
	if *apply && *dryRun {
		fmt.Fprintln(os.Stderr, "--apply and --dry-run are mutually exclusive")
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
	plan, err := brain.PlanMove(cfg, brain.Sphere(*sphere), *from, *to)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if !*apply {
		return printBrainJSON(plan)
	}
	if err := applyIntegrityGate(cfg, brain.Sphere(*sphere), *skipGate, func() error {
		return brain.ApplyMove(cfg, plan, *confirm)
	}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return printBrainJSON(map[string]interface{}{
		"ok":     true,
		"sphere": plan.Sphere,
		"from":   plan.From,
		"to":     plan.To,
		"digest": plan.Digest,
	})
}
