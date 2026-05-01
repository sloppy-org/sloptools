package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/sloppy-org/sloptools/internal/brain"
	"github.com/sloppy-org/sloptools/internal/meetings"
)

// cmdMeetings dispatches the `sloptools meetings <subcommand>` family.
// `watch` enforces the canonical-host contract from sources.toml and
// runs the polling INBOX pipeline; `ingest-once` is a manual trigger
// that drains the INBOX exactly once and exits.
func cmdMeetings(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "meetings <watch|ingest-once> [flags]")
		return 2
	}
	switch args[0] {
	case "watch":
		return cmdMeetingsWatch(args[1:], false)
	case "ingest-once":
		return cmdMeetingsWatch(args[1:], true)
	default:
		fmt.Fprintf(os.Stderr, "unknown meetings subcommand: %s\n", args[0])
		return 2
	}
}

func cmdMeetingsWatch(args []string, oneShot bool) int {
	fs := flag.NewFlagSet("meetings watch", flag.ContinueOnError)
	sphere := fs.String("sphere", "", "vault sphere (work|private)")
	configPath := fs.String("vault-config", "", "vault config path; defaults to ~/.config/sloptools/vaults.toml")
	sourcesConfig := fs.String("sources-config", "", "meetings/sources config path; defaults to ~/.config/sloptools/sources.toml")
	intervalFlag := fs.Duration("interval", meetings.DefaultWatcherInterval, "polling interval between scans")
	hostnameFlag := fs.String("hostname", "", "override hostname for canonical-host check (defaults to os.Hostname)")
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
	if _, ok := cfg.Vault(brain.Sphere(*sphere)); !ok {
		fmt.Fprintf(os.Stderr, "unknown vault %q\n", *sphere)
		return 1
	}
	resolvedSources, err := defaultMeetingsConfigPath(*sourcesConfig)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	meetingsCfg, err := meetings.Load(resolvedSources, *sourcesConfig != "")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	sphereCfg, ok := meetingsCfg.Sphere(*sphere)
	if !ok {
		fmt.Fprintf(os.Stderr, "no [meetings.%s] section in %s\n", *sphere, resolvedSources)
		return 1
	}
	host := *hostnameFlag
	if strings.TrimSpace(host) == "" {
		current, err := os.Hostname()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		host = current
	}
	pipeline := meetingsPipelineFromConfig(sphereCfg, *sphere)
	watcher, err := meetings.NewWatcher(sphereCfg, host, pipeline, *intervalFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if oneShot {
		if err := watcher.RunOnce(ctx); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
	if err := watcher.Run(ctx); err != nil && !isContextCancelled(err) {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

// meetingsPipelineFromConfig wires the production pipeline. The default
// transcribe and render commands come from the per-sphere config; the
// long-memo branch writes a `MEETING_NOTES.md` under meetings_root and
// shells out to `sloptools brain gtd ingest --source meetings` so the
// idempotent ingest path is the single source of truth.
func meetingsPipelineFromConfig(cfg meetings.SphereConfig, sphere string) meetings.Pipeline {
	now := func() time.Time { return time.Now().UTC() }
	pipeline := meetings.Pipeline{
		Cfg:           cfg,
		Sphere:        sphere,
		Probe:         meetings.FFProbeDurationProbe(""),
		Transcribe:    meetings.CommandTranscriber(cfg.TranscribeCommand),
		QuickRender:   wrapRenderer(meetings.CommandRenderer(cfg.RenderCommand, map[string]string{"MEMO_KIND": "quick"})),
		LongRender:    wrapLongRenderer(meetings.CommandRenderer(cfg.RenderCommand, map[string]string{"MEMO_KIND": "long"})),
		WriteQuick:    writeQuickCommitment(cfg, sphere),
		IngestMeeting: ingestLongMeeting(cfg, sphere),
		NowFunc:       now,
	}
	return pipeline
}

func wrapRenderer(fn func(ctx context.Context, transcript string) (string, error)) meetings.QuickRenderer {
	return func(ctx context.Context, transcript string) (string, error) { return fn(ctx, transcript) }
}

func wrapLongRenderer(fn func(ctx context.Context, transcript string) (string, error)) meetings.LongRenderer {
	return func(ctx context.Context, slug, transcript string) (string, error) {
		body, err := fn(ctx, transcript)
		if err != nil {
			return "", err
		}
		if !strings.Contains(body, "## Action Checklist") {
			return "", fmt.Errorf("renderer output missing required `## Action Checklist` section for slug %s", slug)
		}
		return body, nil
	}
}

func writeQuickCommitment(cfg meetings.SphereConfig, sphere string) meetings.QuickWriter {
	return func(_ context.Context, _ string, outcome, transcript, audioPath string) error {
		dir := filepath.Join(cfg.MeetingsRoot, "_quick")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		stamp := time.Now().UTC().Format("20060102-150405")
		path := filepath.Join(dir, stamp+"-"+filepath.Base(audioPath)+".md")
		body := fmt.Sprintf("---\nkind: commitment\nsphere: %s\ntitle: %q\nstatus: inbox\ncontext: meetings\n---\n# %s\n\n## Summary\nQuick voice memo on %s.\n\n## Next Action\n- [ ] %s\n\n## Evidence\n- transcript: %s\n\n## Linked Items\n- None.\n\n## Review Notes\n- Captured from voice memo %s.\n",
			sphere, outcome, outcome, stamp, outcome, transcriptOneLine(transcript), filepath.Base(audioPath))
		return os.WriteFile(path, []byte(body), 0o644)
	}
}

func ingestLongMeeting(cfg meetings.SphereConfig, sphere string) meetings.LongIngester {
	return func(_ context.Context, _ string, slug, body string) (string, error) {
		dir := filepath.Join(cfg.MeetingsRoot, slug)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
		path := filepath.Join(dir, "MEETING_NOTES.md")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			return "", err
		}
		_ = sphere
		return path, nil
	}
}

func transcriptOneLine(transcript string) string {
	collapsed := strings.Join(strings.Fields(transcript), " ")
	if len(collapsed) > 280 {
		return collapsed[:280] + "..."
	}
	return collapsed
}

func defaultMeetingsConfigPath(path string) (string, error) {
	clean := strings.TrimSpace(path)
	if clean != "" {
		if strings.HasPrefix(clean, "~/") {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			clean = filepath.Join(home, strings.TrimPrefix(clean, "~/"))
		}
		return filepath.Clean(clean), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "sloptools", "sources.toml"), nil
}

func isContextCancelled(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, context.Canceled.Error()) || strings.Contains(msg, context.DeadlineExceeded.Error())
}
