package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/sloppy-org/sloptools/internal/brain"
	"github.com/sloppy-org/sloptools/internal/mcp"
	"github.com/sloppy-org/sloptools/internal/meetings"
)

// cmdMeetings dispatches the `sloptools meetings <subcommand>` family.
// `watch` enforces the canonical-host contract from sources.toml and
// runs the polling INBOX pipeline; `ingest-once` is a manual trigger
// that drains the INBOX exactly once and exits. `summary` and `share`
// mirror the meeting.summary.* and meeting.share.* MCP verbs.
func cmdMeetings(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "meetings <watch|ingest-once|summary|share> [flags]")
		return 2
	}
	switch args[0] {
	case "watch":
		return cmdMeetingsWatch(args[1:], false)
	case "ingest-once":
		return cmdMeetingsWatch(args[1:], true)
	case "summary":
		return cmdMeetingsSummary(args[1:])
	case "share":
		return cmdMeetingsShare(args[1:])
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
	pipeline := meetingsPipelineFromConfig(sphereCfg, *sphere, *configPath, resolvedSources)
	watcher, err := meetings.NewWatcher(sphereCfg, host, pipeline)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	watcher.SetNotesIngester(meetingsNotesIngester(*sphere, *configPath, resolvedSources))
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

// meetingsPipelineFromConfig wires the production pipeline. Both branches
// route through the same brain.gtd write/ingest paths: the short branch
// calls mcp.WriteQuickMeetingCommitment so the captured request becomes a
// real `provider: meetings, status: inbox` GTD commitment with the
// transcript as evidence; the long branch writes the rendered
// `MEETING_NOTES.md` and immediately invokes mcp.IngestMeetings so the
// idempotent ingest path remains the single source of truth. The quick
// branch uses meetings.OpencodeQuickRenderer so the canonical
// QuickMemoSystemPrompt and one-line outcome contract are enforced.
func meetingsPipelineFromConfig(cfg meetings.SphereConfig, sphere, brainConfigPath, sourcesPath string) meetings.Pipeline {
	now := func() time.Time { return time.Now().UTC() }
	pipeline := meetings.Pipeline{
		Cfg:           cfg,
		Sphere:        sphere,
		Probe:         meetings.FFProbeDurationProbe(""),
		Transcribe:    meetings.CommandTranscriber(cfg.TranscribeCommand),
		QuickRender:   meetings.OpencodeQuickRenderer(cfg.RenderCommand),
		LongRender:    wrapLongRenderer(meetings.CommandRenderer(cfg.RenderCommand, map[string]string{"MEMO_KIND": "long"})),
		WriteQuick:    writeQuickCommitment(brainConfigPath),
		IngestMeeting: ingestLongMeeting(cfg, brainConfigPath, sourcesPath),
		NowFunc:       now,
	}
	return pipeline
}

// meetingsNotesIngester returns the watcher callback that re-runs
// `brain.gtd.ingest --source meetings` for a single MEETING_NOTES.md (or
// loose meeting note) after the watcher detects an mtime change. The
// callback receives the absolute path on disk so we forward it as the
// only entry in the ingest paths list.
func meetingsNotesIngester(sphere, brainConfigPath, sourcesPath string) meetings.NotesIngester {
	return func(_ context.Context, notePath string) error {
		_, err := mcp.IngestMeetings(brainConfigPath, sphere, []string{notePath}, sourcesPath)
		return err
	}
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

func writeQuickCommitment(brainConfigPath string) meetings.QuickWriter {
	return func(_ context.Context, sphere, outcome, transcript, audioPath string) error {
		_, err := mcp.WriteQuickMeetingCommitment(brainConfigPath, sphere, outcome, transcript, audioPath)
		return err
	}
}

func ingestLongMeeting(cfg meetings.SphereConfig, brainConfigPath, sourcesPath string) meetings.LongIngester {
	return func(_ context.Context, sphere, slug, body string) (string, error) {
		if strings.TrimSpace(cfg.MeetingsRoot) == "" {
			return "", fmt.Errorf("meetings_root is not configured for sphere %q", sphere)
		}
		dir := filepath.Join(cfg.MeetingsRoot, slug)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
		path := filepath.Join(dir, "MEETING_NOTES.md")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			return "", err
		}
		if _, err := mcp.IngestMeetings(brainConfigPath, sphere, []string{path}, sourcesPath); err != nil {
			return "", fmt.Errorf("brain.gtd.ingest --source meetings %s: %w", path, err)
		}
		return path, nil
	}
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

// cmdMeetingsSummary mirrors the meeting.summary.draft / meeting.summary.send
// MCP verbs for shell scripting. Output is JSON on stdout so callers can pipe
// into jq.
func cmdMeetingsSummary(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "meetings summary <draft|send> [flags]")
		return 2
	}
	switch args[0] {
	case "draft":
		return cmdMeetingsSummaryDraft(args[1:])
	case "send":
		return cmdMeetingsSummarySend(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown meetings summary subcommand: %s\n", args[0])
		return 2
	}
}

func cmdMeetingsSummaryDraft(args []string) int {
	fs := flag.NewFlagSet("meetings summary draft", flag.ContinueOnError)
	sphere := fs.String("sphere", "", "vault sphere (work|private)")
	slug := fs.String("slug", "", "meeting slug under meetings_root")
	recipient := fs.String("recipient", "", "optional single recipient name")
	configPath := fs.String("vault-config", "", "vault config path; defaults to ~/.config/sloptools/vaults.toml")
	sourcesConfig := fs.String("sources-config", "", "meetings/sources config path; defaults to ~/.config/sloptools/sources.toml")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*sphere) == "" || strings.TrimSpace(*slug) == "" {
		fmt.Fprintln(os.Stderr, "--sphere and --slug are required")
		return 2
	}
	server := mcp.NewServerWithStoreAndBrainConfig(".", nil, *configPath)
	out, err := server.CallTool("meeting.summary.draft", map[string]interface{}{
		"sphere":         *sphere,
		"slug":           *slug,
		"recipient":      *recipient,
		"sources_config": *sourcesConfig,
		"config_path":    *configPath,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return printJSON(out)
}

func cmdMeetingsSummarySend(args []string) int {
	fs := flag.NewFlagSet("meetings summary send", flag.ContinueOnError)
	sphere := fs.String("sphere", "", "vault sphere (work|private)")
	slug := fs.String("slug", "", "meeting slug under meetings_root")
	recipient := fs.String("recipient", "", "recipient name (required)")
	to := fs.String("to", "", "optional explicit recipient email")
	accountID := fs.Int64("account-id", 0, "optional mail account id; defaults to [meetings.<sphere>].mail_account_id")
	sendNow := fs.Bool("send-now", false, "send immediately instead of saving as draft")
	configPath := fs.String("vault-config", "", "vault config path; defaults to ~/.config/sloptools/vaults.toml")
	sourcesConfig := fs.String("sources-config", "", "meetings/sources config path; defaults to ~/.config/sloptools/sources.toml")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*sphere) == "" || strings.TrimSpace(*slug) == "" || strings.TrimSpace(*recipient) == "" {
		fmt.Fprintln(os.Stderr, "--sphere, --slug, and --recipient are required")
		return 2
	}
	server := mcp.NewServerWithStoreAndBrainConfig(".", nil, *configPath)
	args2 := map[string]interface{}{
		"sphere":         *sphere,
		"slug":           *slug,
		"recipient":      *recipient,
		"to":             *to,
		"send_now":       *sendNow,
		"sources_config": *sourcesConfig,
		"config_path":    *configPath,
	}
	if *accountID > 0 {
		args2["account_id"] = *accountID
	}
	out, err := server.CallTool("meeting.summary.send", args2)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return printJSON(out)
}

func cmdMeetingsShare(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "meetings share <create|revoke> [flags]")
		return 2
	}
	switch args[0] {
	case "create":
		return cmdMeetingsShareCreate(args[1:])
	case "revoke":
		return cmdMeetingsShareRevoke(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown meetings share subcommand: %s\n", args[0])
		return 2
	}
}

func cmdMeetingsShareCreate(args []string) int {
	fs := flag.NewFlagSet("meetings share create", flag.ContinueOnError)
	sphere := fs.String("sphere", "", "vault sphere (work|private)")
	slug := fs.String("slug", "", "meeting slug under meetings_root")
	url := fs.String("url", "", "optional pre-existing share URL; when empty the verb creates a Nextcloud public share via OCS")
	token := fs.String("token", "", "optional share token; only used when --url is supplied")
	permissions := fs.String("permissions", "", "share permissions (edit|read|comment)")
	expiryDays := fs.Int("expiry-days", 0, "expiry window in days")
	password := fs.Bool("password", false, "share is password-protected")
	configPath := fs.String("vault-config", "", "vault config path; defaults to ~/.config/sloptools/vaults.toml")
	sourcesConfig := fs.String("sources-config", "", "meetings/sources config path; defaults to ~/.config/sloptools/sources.toml")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*sphere) == "" || strings.TrimSpace(*slug) == "" {
		fmt.Fprintln(os.Stderr, "--sphere and --slug are required")
		return 2
	}
	server := mcp.NewServerWithStoreAndBrainConfig(".", nil, *configPath)
	out, err := server.CallTool("meeting.share.create", map[string]interface{}{
		"sphere":         *sphere,
		"slug":           *slug,
		"url":            *url,
		"token":          *token,
		"permissions":    *permissions,
		"expiry_days":    *expiryDays,
		"password":       *password,
		"sources_config": *sourcesConfig,
		"config_path":    *configPath,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return printJSON(out)
}

func cmdMeetingsShareRevoke(args []string) int {
	fs := flag.NewFlagSet("meetings share revoke", flag.ContinueOnError)
	sphere := fs.String("sphere", "", "vault sphere (work|private)")
	slug := fs.String("slug", "", "meeting slug under meetings_root")
	configPath := fs.String("vault-config", "", "vault config path; defaults to ~/.config/sloptools/vaults.toml")
	sourcesConfig := fs.String("sources-config", "", "meetings/sources config path; defaults to ~/.config/sloptools/sources.toml")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*sphere) == "" || strings.TrimSpace(*slug) == "" {
		fmt.Fprintln(os.Stderr, "--sphere and --slug are required")
		return 2
	}
	server := mcp.NewServerWithStoreAndBrainConfig(".", nil, *configPath)
	out, err := server.CallTool("meeting.share.revoke", map[string]interface{}{
		"sphere":         *sphere,
		"slug":           *slug,
		"sources_config": *sourcesConfig,
		"config_path":    *configPath,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return printJSON(out)
}

func printJSON(payload map[string]interface{}) int {
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println(string(body))
	return 0
}
