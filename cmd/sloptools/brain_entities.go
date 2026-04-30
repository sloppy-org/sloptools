package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/sloppy-org/sloptools/internal/brain"
)

func cmdBrainEntities(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "brain entities requires candidates")
		return 2
	}
	switch args[0] {
	case "candidates":
		return cmdBrainEntitiesCandidates(args[1:])
	case "help", "-h", "--help":
		fmt.Println("sloptools brain entities candidates [flags]")
		fmt.Println("  --config PATH   vault config path (default ~/.config/sloptools/vaults.toml)")
		fmt.Println("  --sphere NAME   vault sphere: work or private")
		fmt.Println("  --limit N       maximum candidates")
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown brain entities subcommand: %s\n", args[0])
		return 2
	}
}

func cmdBrainEntitiesCandidates(args []string) int {
	fs := flag.NewFlagSet("brain entities candidates", flag.ContinueOnError)
	configPath := fs.String("config", "", "vault config path")
	sphere := fs.String("sphere", "", "vault sphere")
	limit := fs.Int("limit", 0, "maximum candidates")
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
	candidates, err := brain.EntityCandidates(cfg, brain.Sphere(*sphere))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if *limit > 0 && len(candidates) > *limit {
		candidates = candidates[:*limit]
	}
	return printBrainJSON(map[string]interface{}{
		"sphere":     *sphere,
		"candidates": candidates,
		"count":      len(candidates),
	})
}
