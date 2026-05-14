package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/brain"
	"github.com/sloppy-org/sloptools/internal/brain/ledger"
	"github.com/sloppy-org/sloptools/internal/brain/routing"
)

// cmdBrainSleep dispatches `sloptools brain sleep` and orchestrates the
// dream prune-links scan + apply, the dream report, and an optional
// routed judge pass over the rendered Markdown packet. Passing --model keeps
// the legacy direct-Codex path for one-off comparisons.
func cmdBrainSleep(args []string) int {
	fs := flag.NewFlagSet("brain sleep", flag.ContinueOnError)
	configPath := fs.String("config", "", "vault config path")
	sphere := fs.String("sphere", "", "vault sphere: work or private")
	budget := fs.Int("budget", brain.SleepDefaultBudget, "REM notes to dream over")
	nremBudget := fs.Int("nrem-budget", brain.SleepDefaultNREMBudget, "NREM consolidation rows to replay")
	coverageBudget := fs.Int("coverage-budget", brain.SleepDefaultCoverageBudget, "folder coverage changes before NREM")
	autonomy := fs.String("autonomy", brain.SleepDefaultAutonomy, "full or plan-only")
	backend := fs.String("backend", brain.SleepBackendCodex, "codex or none")
	model := fs.String("model", brain.SleepDefaultModel, "codex model (default gpt-5.5)")
	brainTOMLPath := fs.String("brain-toml", "", "override brain.toml path (default ~/.config/sloptools/brain.toml)")
	openaiTier := fs.String("openai-tier", "", "force OpenAI at tier: mini-native-web | full")
	forceLocal := fs.Bool("force-local", false, "pin every stage to the configured local llamacpp model")
	dryRun := fs.Bool("dry-run", false, "skip LLM, do not apply prune-links, do not write report file")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	modelExplicit := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "model" {
			modelExplicit = true
		}
	})
	if strings.TrimSpace(*sphere) == "" {
		fmt.Fprintln(os.Stderr, "--sphere is required")
		return 2
	}
	cfg, err := brain.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	var router *routing.Router
	var ldg *ledger.Ledger
	if strings.EqualFold(strings.TrimSpace(*backend), brain.SleepBackendCodex) && !modelExplicit {
		vault, ok := cfg.Vault(brain.Sphere(*sphere))
		if !ok {
			fmt.Fprintf(os.Stderr, "brain sleep: unknown vault %q\n", *sphere)
			return 1
		}
		fileCfg, err := routing.LoadFile(*brainTOMLPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		ldg, err = ledger.New(vault.BrainRoot(), fileCfg.PlanCaps())
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		router = routing.New(ldg, routing.Overrides{
			OpenAITier: strings.TrimSpace(strings.ToLower(*openaiTier)),
			ForceLocal: *forceLocal,
		})
		router.SetSessionStart(time.Now().UTC())
		if cfgStages, err := fileCfg.ApplyStages(routing.DefaultStageConfigs()); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		} else {
			for s, c := range cfgStages {
				router.SetStageConfig(s, c)
			}
		}
	}
	var res *brain.SleepResult
	run := func() error {
		var runErr error
		res, runErr = brain.RunSleep(cfg, brain.SleepOpts{
			Sphere:         brain.Sphere(*sphere),
			Budget:         *budget,
			NREMBudget:     *nremBudget,
			CoverageBudget: *coverageBudget,
			Backend:        *backend,
			Model:          *model,
			Autonomy:       *autonomy,
			DryRun:         *dryRun,
			Router:         router,
			Ledger:         ldg,
			RunID:          time.Now().UTC().Format("20060102-150405"),
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
	const skipIntegrityGate = true
	if err := applyIntegrityGate(cfg, brain.Sphere(*sphere), skipIntegrityGate, commitMsg, run); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return printBrainJSON(res)
}
