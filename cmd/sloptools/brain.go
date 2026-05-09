package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/sloppy-org/sloptools/internal/brain"
)

func cmdBrain(args []string) int {
	if len(args) == 0 {
		printBrainHelp()
		return 2
	}
	switch args[0] {
	case "search":
		return cmdBrainSearch(args[1:])
	case "backlinks":
		return cmdBrainBacklinks(args[1:])
	case "gtd":
		return cmdBrainGTD(args[1:])
	case "people":
		return cmdBrainPeople(args[1:])
	case "folder":
		return cmdBrainFolder(args[1:])
	case "entities":
		return cmdBrainEntities(args[1:])
	case "glossary":
		return cmdBrainGlossary(args[1:])
	case "attention":
		return cmdBrainAttention(args[1:])
	case "links":
		return cmdBrainLinks(args[1:])
	case "vault":
		return cmdBrainVault(args[1:])
	case "ingest":
		return cmdBrainIngest(args[1:])
	case "move":
		return cmdBrainMove(args[1:])
	case "cleanup-dead-dirs":
		return cmdBrainCleanupDeadDirs(args[1:])
	case "consolidate":
		return cmdBrainConsolidate(args[1:])
	case "dream":
		return cmdBrainDream(args[1:])
	case "archive":
		return cmdBrainArchive(args[1:])
	case "sleep":
		return cmdBrainSleep(args[1:])
	case "night":
		return cmdBrainNight(args[1:])
	case "bench":
		return cmdBrainBench(args[1:])
	case "help", "-h", "--help":
		printBrainHelp()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown brain subcommand: %s\n", args[0])
		printBrainHelp()
		return 2
	}
}

func printBrainHelp() {
	fmt.Println("sloptools brain <search|backlinks|gtd|people|folder|entities|glossary|attention|links|vault|ingest|move|cleanup-dead-dirs|consolidate|dream|archive|sleep|night|bench> [flags]")
	fmt.Println()
	fmt.Println("search flags:")
	fmt.Println("  --config PATH   vault config path (default ~/.config/sloptools/vaults.toml)")
	fmt.Println("  --sphere NAME   vault sphere: work or private")
	fmt.Println("  --query TEXT    search query")
	fmt.Println("  --mode MODE     text, regex, wikilink, markdown_link, or alias")
	fmt.Println("  --limit N       maximum results (default 50)")
	fmt.Println()
	fmt.Println("backlinks flags:")
	fmt.Println("  --config PATH   vault config path (default ~/.config/sloptools/vaults.toml)")
	fmt.Println("  --sphere NAME   vault sphere: work or private")
	fmt.Println("  --target PATH   target note path")
	fmt.Println("  --limit N       maximum results (default 50)")
	fmt.Println()
	fmt.Println("gtd flags:")
	fmt.Println("  --config PATH   vault config path (default ~/.config/sloptools/vaults.toml)")
	fmt.Println("  --sphere NAME   vault sphere: work or private")
	fmt.Println("  --path PATH     GTD note path")
	fmt.Println("  subcommands: parse validate list update write organize resurface dashboard review-batch ingest")
	fmt.Println()
	fmt.Println("people flags:")
	fmt.Println("  --config PATH   vault config path (default ~/.config/sloptools/vaults.toml)")
	fmt.Println("  --sphere NAME   vault sphere: work or private")
	fmt.Println("  subcommands: monthly-index")
	fmt.Println()
	fmt.Println("folder flags:")
	fmt.Println("  --config PATH   vault config path (default ~/.config/sloptools/vaults.toml)")
	fmt.Println("  --sphere NAME   vault sphere: work or private")
	fmt.Println("  --path PATH     folder note path")
	fmt.Println()
	fmt.Println("glossary flags:")
	fmt.Println("  --config PATH   vault config path (default ~/.config/sloptools/vaults.toml)")
	fmt.Println("  --sphere NAME   vault sphere: work or private")
	fmt.Println("  --path PATH     glossary note path")
	fmt.Println()
	fmt.Println("attention flags:")
	fmt.Println("  --config PATH   vault config path (default ~/.config/sloptools/vaults.toml)")
	fmt.Println("  --sphere NAME   vault sphere: work or private")
	fmt.Println("  --path PATH     attention note path")
	fmt.Println()
	fmt.Println("links flags:")
	fmt.Println("  --config PATH   vault config path (default ~/.config/sloptools/vaults.toml)")
	fmt.Println("  --sphere NAME   vault sphere: work or private")
	fmt.Println("  --path PATH     note path")
	fmt.Println("  --link TEXT     link to resolve")
	fmt.Println()
	fmt.Println("vault flags:")
	fmt.Println("  --config PATH   vault config path (default ~/.config/sloptools/vaults.toml)")
	fmt.Println("  --sphere NAME   vault sphere: work or private")
	fmt.Println()
	fmt.Println("sleep flags:")
	fmt.Println("  --config PATH   vault config path")
	fmt.Println("  --sphere NAME   vault sphere: work or private")
	fmt.Println("  --budget N      REM notes to dream over (default 20)")
	fmt.Println("  --nrem-budget N NREM consolidation rows to replay (default 60)")
	fmt.Println("  --coverage-budget N folder coverage changes before NREM (default 40)")
	fmt.Println("  --autonomy NAME full (default) or plan-only")
	fmt.Println("  --backend NAME  codex (default) or none")
	fmt.Println("  --model NAME    codex model (default gpt-5.4-mini)")
	fmt.Println("  --dry-run       skip LLM, do not apply prune-links, do not write report file")
	fmt.Println()
	fmt.Println("night flags:")
	fmt.Println("  --config PATH       vault config path")
	fmt.Println("  --sphere NAME       vault sphere: work or private")
	fmt.Println("  --only-stage NAME   sweep | scout | judge (default: all)")
	fmt.Println("  --claude-tier NAME  force Anthropic at tier: haiku | sonnet | opus")
	fmt.Println("  --openai-tier NAME  force OpenAI at tier: mini | full")
	fmt.Println("  --force-local       pin every stage to the configured local OpenCode Qwen model")
	fmt.Println("  --autonomy NAME     full (default) or plan-only")
	fmt.Println("  --brain-toml PATH   override brain.toml path")
	fmt.Println("  --dry-run           skip LLM, do not apply prune-links, do not write report")
	fmt.Println()
	fmt.Println("bench flags:")
	fmt.Println("  --config PATH    vault config path")
	fmt.Println("  --sphere NAME    vault sphere: work or private")
	fmt.Println("  --tasks LIST     comma-separated task ids (v1: folder-note)")
	fmt.Println("  --models LIST    comma-separated model labels (default: full v1 matrix)")
	fmt.Println("  --out-dir PATH   override output directory")
	fmt.Println("  --post-comment N post rendered report.md as a comment on sloptools issue N")
	fmt.Println("  --llm-judge LABEL run a second-pass LLM judge per cell (e.g. claude-sonnet-4-6)")
}

func cmdBrainSearch(args []string) int {
	fs := flag.NewFlagSet("brain search", flag.ContinueOnError)
	configPath := fs.String("config", "", "vault config path")
	sphere := fs.String("sphere", "", "vault sphere")
	query := fs.String("query", "", "search query")
	mode := fs.String("mode", string(brain.SearchText), "search mode")
	limit := fs.Int("limit", 50, "maximum results")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*sphere) == "" {
		fmt.Fprintln(os.Stderr, "--sphere is required")
		return 2
	}
	if strings.TrimSpace(*query) == "" {
		fmt.Fprintln(os.Stderr, "--query is required")
		return 2
	}
	cfg, err := brain.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	results, err := brain.Search(context.Background(), cfg, brain.SearchOptions{
		Sphere: brain.Sphere(*sphere),
		Query:  *query,
		Mode:   brain.SearchMode(*mode),
		Limit:  *limit,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return printBrainJSON(map[string]interface{}{"sphere": *sphere, "mode": *mode, "query": *query, "results": results, "count": len(results)})
}

func cmdBrainBacklinks(args []string) int {
	fs := flag.NewFlagSet("brain backlinks", flag.ContinueOnError)
	configPath := fs.String("config", "", "vault config path")
	sphere := fs.String("sphere", "", "vault sphere")
	target := fs.String("target", "", "target note path")
	limit := fs.Int("limit", 50, "maximum results")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*sphere) == "" {
		fmt.Fprintln(os.Stderr, "--sphere is required")
		return 2
	}
	if strings.TrimSpace(*target) == "" {
		fmt.Fprintln(os.Stderr, "--target is required")
		return 2
	}
	cfg, err := brain.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	results, err := brain.Backlinks(context.Background(), cfg, brain.BacklinkOptions{
		Sphere: brain.Sphere(*sphere),
		Target: *target,
		Limit:  *limit,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return printBrainJSON(map[string]interface{}{"sphere": *sphere, "target": *target, "results": results, "count": len(results)})
}

func printBrainJSON(value interface{}) int {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(value); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
