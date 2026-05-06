package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/sloppy-org/sloptools/internal/brain"
	brainpeople "github.com/sloppy-org/sloptools/internal/brain/people"
)

func cmdBrainPeople(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "brain people requires monthly-index")
		return 2
	}
	switch args[0] {
	case "monthly-index":
		return cmdBrainPeopleMonthlyIndex(args[1:])
	case "help", "-h", "--help":
		fmt.Println("sloptools brain people <monthly-index> [flags]")
		fmt.Println("  --config PATH   vault config path (default ~/.config/sloptools/vaults.toml)")
		fmt.Println("  --sphere NAME   vault sphere: work or private")
		fmt.Println("  --dry-run       count writes without modifying files")
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown brain people subcommand: %s\n", args[0])
		return 2
	}
}

func cmdBrainPeopleMonthlyIndex(args []string) int {
	fs := flag.NewFlagSet("brain people monthly-index", flag.ContinueOnError)
	configPath := fs.String("config", "", "vault config path")
	sphere := fs.String("sphere", "", "vault sphere")
	dryRun := fs.Bool("dry-run", false, "count writes without modifying files")
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
	vault, ok := cfg.Vault(brain.Sphere(*sphere))
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown vault %q\n", *sphere)
		return 1
	}
	var res brainpeople.Result
	run := func() error {
		var err error
		res, err = brainpeople.WriteMonthlyIndexes(vault.BrainRoot(), *dryRun)
		return err
	}
	var runErr error
	if *dryRun {
		runErr = run()
	} else {
		runErr = brain.WithGitCommit(cfg, brain.Sphere(*sphere), "brain people monthly-index", run)
	}
	if runErr != nil {
		fmt.Fprintln(os.Stderr, runErr)
		return 1
	}
	return printBrainJSON(map[string]interface{}{
		"sphere":  vault.Sphere,
		"vault":   vault,
		"months":  res.Months,
		"writes":  res.Writes,
		"dry_run": res.DryRun,
	})
}
