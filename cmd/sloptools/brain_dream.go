package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/sloppy-org/sloptools/internal/brain"
)

// cmdBrainDream dispatches `sloptools brain dream` subcommands.
func cmdBrainDream(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "brain dream requires a subcommand: report | prune-links")
		return 2
	}
	switch args[0] {
	case "report":
		return cmdBrainDreamReport(args[1:])
	case "prune-links":
		return cmdBrainDreamPruneLinks(args[1:])
	case "help", "-h", "--help":
		fmt.Println("sloptools brain dream <report|prune-links> [flags]")
		fmt.Println()
		fmt.Println("report flags:")
		fmt.Println("  --config PATH   vault config path (default ~/.config/sloptools/vaults.toml)")
		fmt.Println("  --sphere NAME   vault sphere: work or private")
		fmt.Println("  --budget N      number of notes to dream over (default 10)")
		fmt.Println()
		fmt.Println("prune-links flags:")
		fmt.Println("  --config PATH   vault config path")
		fmt.Println("  --sphere NAME   vault sphere")
		fmt.Println("  --mode MODE     scan (default) or apply")
		fmt.Println("  --confirm HEX   digest from a fresh scan; required for --mode apply")
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown brain dream subcommand: %s\n", args[0])
		return 2
	}
}

func cmdBrainDreamReport(args []string) int {
	fs := flag.NewFlagSet("brain dream report", flag.ContinueOnError)
	configPath := fs.String("config", "", "vault config path")
	sphere := fs.String("sphere", "", "vault sphere")
	budget := fs.Int("budget", 10, "notes to dream over")
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
	report, err := brain.DreamReportRun(cfg, brain.Sphere(*sphere), *budget)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return printBrainJSON(report)
}

func cmdBrainDreamPruneLinks(args []string) int {
	fs := flag.NewFlagSet("brain dream prune-links", flag.ContinueOnError)
	configPath := fs.String("config", "", "vault config path")
	sphere := fs.String("sphere", "", "vault sphere")
	mode := fs.String("mode", "scan", "scan or apply")
	confirm := fs.String("confirm", "", "digest from a fresh scan; required for --mode apply")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*sphere) == "" {
		fmt.Fprintln(os.Stderr, "--sphere is required")
		return 2
	}
	switch strings.TrimSpace(*mode) {
	case "scan", "":
		return runDreamPruneLinksScan(*configPath, *sphere)
	case "apply":
		if strings.TrimSpace(*confirm) == "" {
			fmt.Fprintln(os.Stderr, "--confirm is required for --mode apply")
			return 2
		}
		return runDreamPruneLinksApply(*configPath, *sphere, *confirm)
	default:
		fmt.Fprintf(os.Stderr, "unknown --mode: %s\n", *mode)
		return 2
	}
}

func runDreamPruneLinksScan(configPath, sphere string) int {
	cfg, err := brain.LoadConfig(configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	cold, err := brain.DreamPruneLinksScan(cfg, brain.Sphere(sphere))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	plan, err := brain.BuildDreamPrunePlan(cfg, brain.Sphere(sphere), cold)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return printBrainJSON(map[string]interface{}{
		"sphere": sphere,
		"cold":   cold,
		"count":  len(cold),
		"digest": plan.Digest,
	})
}

func runDreamPruneLinksApply(configPath, sphere, confirm string) int {
	cfg, err := brain.LoadConfig(configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	summary, err := brain.DreamPruneLinksApply(cfg, brain.Sphere(sphere), confirm)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return printBrainJSON(summary)
}
