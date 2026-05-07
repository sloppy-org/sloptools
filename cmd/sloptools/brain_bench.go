package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/brain"
	"github.com/sloppy-org/sloptools/internal/brain/bench"
	"github.com/sloppy-org/sloptools/internal/brain/ledger"
	"github.com/sloppy-org/sloptools/internal/brain/prompts"
)

// cmdBrainBench dispatches `sloptools brain bench`. The default run
// covers the v1 task list (folder-note authoring) over every model in
// the v1 matrix, scores deterministically, writes matrix.tsv +
// report.md under <brain>/data/brain/bench/<date>/, and optionally
// posts the rendered report.md as a comment on a GitHub issue.
func cmdBrainBench(args []string) int {
	fs := flag.NewFlagSet("brain bench", flag.ContinueOnError)
	configPath := fs.String("config", "", "vault config path")
	sphere := fs.String("sphere", "", "vault sphere: work or private")
	tasks := fs.String("tasks", "folder-note", "comma-separated task ids (v1: folder-note)")
	models := fs.String("models", "", "comma-separated model labels (default: full v1 matrix)")
	outDir := fs.String("out-dir", "", "override output directory")
	postIssue := fs.Int("post-comment", 0, "post report.md as a comment on this sloptools issue")
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
		fmt.Fprintf(os.Stderr, "brain bench: unknown vault %q\n", *sphere)
		return 1
	}
	dateID := time.Now().UTC().Format("20060102-150405")
	dest := *outDir
	if dest == "" {
		dest = filepath.Join(vault.BrainRoot(), "data", "brain", "bench", dateID)
	}

	promptDir := filepath.Join(dest, "prompts")
	if _, err := prompts.Extract(promptDir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	ldg, err := ledger.New(vault.BrainRoot(), ledger.DefaultPlanCaps())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	taskList, err := buildTaskList(*tasks)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	modelList := bench.DefaultModelMatrix()
	if strings.TrimSpace(*models) != "" {
		modelList = filterModels(modelList, strings.Split(*models, ","))
	}
	if len(modelList) == 0 {
		fmt.Fprintln(os.Stderr, "no models selected")
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Minute)
	defer cancel()

	res, err := bench.Run(ctx, bench.Options{
		Tasks:     taskList,
		Models:    modelList,
		OutDir:    dest,
		PromptDir: promptDir,
		RunID:     dateID,
		Ledger:    ldg,
		Sphere:    *sphere,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := bench.Render(res); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println(bench.MatrixTSVPath(dest))
	fmt.Println(bench.ReportMDPath(dest))

	if *postIssue > 0 {
		if err := postReportComment(*postIssue, bench.ReportMDPath(dest)); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	return 0
}

func buildTaskList(spec string) ([]bench.Task, error) {
	out := make([]bench.Task, 0, 4)
	for _, raw := range strings.Split(spec, ",") {
		id := strings.TrimSpace(raw)
		switch id {
		case "folder-note":
			out = append(out, bench.FolderNoteTask{FixtureSet: bench.V1FolderNoteFixtures()})
		case "":
			continue
		default:
			return nil, fmt.Errorf("unknown task id: %s (v1 supports: folder-note)", id)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no tasks selected")
	}
	return out, nil
}

func filterModels(all []bench.ModelSpec, want []string) []bench.ModelSpec {
	wantSet := make(map[string]bool, len(want))
	for _, w := range want {
		wantSet[strings.TrimSpace(w)] = true
	}
	out := make([]bench.ModelSpec, 0, len(all))
	for _, m := range all {
		if wantSet[m.Label] || wantSet[m.Model] || wantSet[m.BackendID] {
			out = append(out, m)
		}
	}
	return out
}
