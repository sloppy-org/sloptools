package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/sourceitems"
)

func cmdSource(args []string) int {
	if len(args) == 0 {
		printSourceHelp()
		return 2
	}
	switch args[0] {
	case "list":
		return cmdSourceList(args[1:])
	case "comment":
		return cmdSourceComment(args[1:])
	case "close":
		return cmdSourceClose(args[1:])
	case "help", "-h", "--help":
		printSourceHelp()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown source subcommand: %s\n", args[0])
		printSourceHelp()
		return 2
	}
}

func printSourceHelp() {
	fmt.Println("sloptools source <list|comment|close> [flags]")
	fmt.Println()
	fmt.Println("list flags:")
	fmt.Println("  --project-dir PATH   project dir (default .)")
	fmt.Println("  --provider NAME      github, gitlab, or auto (default auto)")
	fmt.Println("  --format FMT         table (default) or json")
	fmt.Println()
	fmt.Println("comment flags:")
	fmt.Println("  --project-dir PATH   project dir (default .)")
	fmt.Println("  --provider NAME      github, gitlab, or auto (default auto)")
	fmt.Println("  --kind KIND          issue, pull_request, or merge_request")
	fmt.Println("  --number N           upstream number or IID")
	fmt.Println("  --body TEXT          comment body")
	fmt.Println()
	fmt.Println("close flags:")
	fmt.Println("  --project-dir PATH   project dir (default .)")
	fmt.Println("  --provider NAME      github, gitlab, or auto (default auto)")
	fmt.Println("  --kind KIND          issue, pull_request, or merge_request")
	fmt.Println("  --number N           upstream number or IID")
	fmt.Println("  --comment TEXT       optional comment posted before close")
}

type sourceCommonFlags struct {
	projectDir string
	provider   string
}

func bindSourceCommonFlags(fs *flag.FlagSet, c *sourceCommonFlags) {
	fs.StringVar(&c.projectDir, "project-dir", ".", "project dir")
	fs.StringVar(&c.provider, "provider", "auto", "source provider")
}

func newSourceProvider(projectDir, provider string) (sourceitems.Provider, error) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "", "auto":
		detected, err := sourceitems.DetectProvider(projectDir)
		if err != nil {
			return nil, fmt.Errorf("unable to detect GitHub or GitLab remote from %q: %w", projectDir, err)
		}
		return newSourceProvider(projectDir, detected)
	case sourceitems.GitHubProviderName:
		return sourceitems.NewGitHubProvider(projectDir)
	case sourceitems.GitLabProviderName:
		return sourceitems.NewGitLabProvider(projectDir)
	default:
		return nil, fmt.Errorf("unknown --provider %q", provider)
	}
}

func sourceItemSummary(item providerdata.SourceItem) string {
	review := strings.TrimSpace(item.ReviewStatus)
	if review == "" && len(item.Reviewers) > 0 {
		review = "review_requested"
	}
	return fmt.Sprintf("%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s",
		item.SourceRef,
		item.State,
		review,
		item.Title,
		strings.Join(item.Labels, ","),
		strings.Join(item.Assignees, ","),
		strings.Join(item.Reviewers, ","),
		item.URL,
	)
}

func cmdSourceList(args []string) int {
	fs := flag.NewFlagSet("source list", flag.ContinueOnError)
	var common sourceCommonFlags
	format := fs.String("format", "table", "output format: table or json")
	bindSourceCommonFlags(fs, &common)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	provider, err := newSourceProvider(common.projectDir, common.provider)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	items, err := provider.List(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	switch strings.ToLower(strings.TrimSpace(*format)) {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		if err := enc.Encode(items); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	case "", "table":
		for _, item := range items {
			fmt.Fprintln(os.Stdout, sourceItemSummary(item))
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown --format %q (want table or json)\n", *format)
		return 2
	}
	return 0
}

func cmdSourceComment(args []string) int {
	fs := flag.NewFlagSet("source comment", flag.ContinueOnError)
	var common sourceCommonFlags
	kind := fs.String("kind", "", "upstream kind")
	number := fs.Int64("number", 0, "upstream number or IID")
	body := fs.String("body", "", "comment body")
	bindSourceCommonFlags(fs, &common)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*kind) == "" {
		fmt.Fprintln(os.Stderr, "error: --kind is required")
		return 2
	}
	if *number <= 0 {
		fmt.Fprintln(os.Stderr, "error: --number is required")
		return 2
	}
	if strings.TrimSpace(*body) == "" {
		fmt.Fprintln(os.Stderr, "error: --body is required")
		return 2
	}
	provider, err := newSourceProvider(common.projectDir, common.provider)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	item := providerdata.SourceItem{Provider: strings.ToLower(strings.TrimSpace(common.provider)), Kind: *kind, Number: *number}
	if item.Provider == "auto" || item.Provider == "" {
		item.Provider = provider.ProviderName()
	}
	if err := provider.Comment(context.Background(), item, *body); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func cmdSourceClose(args []string) int {
	fs := flag.NewFlagSet("source close", flag.ContinueOnError)
	var common sourceCommonFlags
	kind := fs.String("kind", "", "upstream kind")
	number := fs.Int64("number", 0, "upstream number or IID")
	comment := fs.String("comment", "", "optional comment body")
	bindSourceCommonFlags(fs, &common)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*kind) == "" {
		fmt.Fprintln(os.Stderr, "error: --kind is required")
		return 2
	}
	if *number <= 0 {
		fmt.Fprintln(os.Stderr, "error: --number is required")
		return 2
	}
	provider, err := newSourceProvider(common.projectDir, common.provider)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	item := providerdata.SourceItem{Provider: strings.ToLower(strings.TrimSpace(common.provider)), Kind: *kind, Number: *number}
	if item.Provider == "auto" || item.Provider == "" {
		item.Provider = provider.ProviderName()
	}
	if err := provider.Close(context.Background(), item, *comment); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
