package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/brain"
)

func cmdBrainActivity(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "brain activity requires log or show")
		return 2
	}
	switch args[0] {
	case "log":
		return cmdBrainActivityLog(args[1:])
	case "show":
		return cmdBrainActivityShow(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown brain activity subcommand: %s\n", args[0])
		return 2
	}
}

func cmdBrainActivityLog(args []string) int {
	fs := flag.NewFlagSet("brain activity log", flag.ContinueOnError)
	configPath := fs.String("config", "", "vault config path")
	sphere := fs.String("sphere", "", "vault sphere: work or private")
	date := fs.String("date", "", "YYYY-MM-DD, defaults to today")
	operation := fs.String("operation", "note", "operation label")
	tool := fs.String("tool", "sloptools", "tool label")
	message := fs.String("message", "", "activity message")
	links := multiFlag{}
	commit := fs.Bool("commit", false, "run brain integrity gate and auto-commit")
	skipGate := fs.Bool("skip-gate", false, "skip brain integrity gate when --commit is set")
	fs.Var(&links, "link", "brain note link; may be repeated")
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
	var res *brain.ActivityLogResult
	write := func() error {
		var err error
		res, err = brain.WriteActivityLog(cfg, brain.ActivityLogOpts{
			Sphere:    brain.Sphere(*sphere),
			Date:      *date,
			Operation: *operation,
			Tool:      *tool,
			Message:   *message,
			Links:     links,
		})
		return err
	}
	if *commit {
		if err := applyIntegrityGate(cfg, brain.Sphere(*sphere), *skipGate, "brain activity log", write); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	} else if err := write(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return printBrainJSON(res)
}

func cmdBrainActivityShow(args []string) int {
	fs := flag.NewFlagSet("brain activity show", flag.ContinueOnError)
	configPath := fs.String("config", "", "vault config path")
	sphere := fs.String("sphere", "", "vault sphere: work or private")
	date := fs.String("date", "", "YYYY-MM-DD, defaults to today")
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
	stamp := *date
	if strings.TrimSpace(stamp) == "" {
		stamp = time.Now().Format("2006-01-02")
	}
	body, path, err := brain.ReadActivitySummary(cfg, brain.Sphere(*sphere), stamp)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return printBrainJSON(map[string]interface{}{"sphere": *sphere, "date": stamp, "path": path, "markdown": body})
}

type multiFlag []string

func (f *multiFlag) String() string {
	return strings.Join(*f, ",")
}

func (f *multiFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}
