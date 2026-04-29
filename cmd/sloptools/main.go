package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/mcp"
	"github.com/sloppy-org/sloptools/internal/protocol"
	"github.com/sloppy-org/sloptools/internal/serve"
	"github.com/sloppy-org/sloptools/internal/store"
)

const defaultBinaryVersion = "0.1.0"

var (
	version = defaultBinaryVersion
	commit  = "dev"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if err := loadDefaultEnvFiles(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if len(args) == 0 {
		printHelp()
		return 2
	}
	switch args[0] {
	case "bootstrap":
		return cmdBootstrap(args[1:])
	case "server":
		return cmdServer(args[1:])
	case "mcp-server":
		return cmdMCPServer(args[1:])
	case "brain":
		return cmdBrain(args[1:])
	case "mail":
		return cmdMail(args[1:])
	case "external-account":
		return cmdExternalAccount(args[1:])
	case "tools":
		return cmdTools(args[1:])
	case "source":
		return cmdSource(args[1:])
	case "version":
		return cmdVersion()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", args[0])
		printHelp()
		return 2
	}
}

func printHelp() {
	fmt.Println("sloptools <command> [flags]")
	fmt.Println("commands: bootstrap server mcp-server brain mail external-account tools version")
	fmt.Println("brain subcommands: search backlinks gtd folder glossary attention links vault")
	fmt.Println("mail subcommands: send reply")
	fmt.Println("external-account subcommands: list add update remove")
	fmt.Println("tools subcommands: list call")
	fmt.Println("source subcommands: list comment close")
}

type serverConfig struct {
	dataDir         string
	projectDir      string
	mcpHost         string
	mcpPort         int
	unsafePublicMCP bool
	mcpUnixSocket   string
}

func cmdBootstrap(args []string) int {
	fs := flag.NewFlagSet("bootstrap", flag.ContinueOnError)
	projectDir := fs.String("project-dir", ".", "project dir")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	res, err := protocol.BootstrapProject(*projectDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("project prepared: %s\n", res.Paths.ProjectDir)
	fmt.Printf("mcp config snippet: %s\n", res.Paths.MCPConfigPath)
	if res.GitInitialized {
		fmt.Println("git initialized")
	}
	return 0
}

func cmdMCPServer(args []string) int {
	fs := flag.NewFlagSet("mcp-server", flag.ContinueOnError)
	stdio := fs.Bool("stdio", true, "use stdio transport")
	vaultConfig := fs.String("vault-config", "", "default brain vault config path")
	projectDir := fs.String("project-dir", ".", "project dir")
	dataDir := fs.String("data-dir", filepath.Join(os.Getenv("HOME"), ".local", "share", "sloppy"), "data dir")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if !*stdio {
		fmt.Fprintln(os.Stderr, "mcp-server only supports stdio transport")
		return 2
	}
	res, err := protocol.BootstrapProject(*projectDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	st, err := store.New(filepath.Join(*dataDir, "sloppy.db"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer st.Close()
	return mcp.RunStdioWithStoreAndBrainConfig(res.Paths.ProjectDir, st, *vaultConfig)
}

func cmdServer(args []string) int {
	cfg, status := parseServerConfig(args)
	if status != 0 {
		return status
	}
	return runServer(cfg)
}

func parseServerConfig(args []string) (*serverConfig, int) {
	fs := flag.NewFlagSet("server", flag.ContinueOnError)
	cfg := &serverConfig{
		dataDir: filepath.Join(os.Getenv("HOME"), ".local", "share", "sloppy"),
	}
	projectDir := fs.String("project-dir", ".", "project dir")
	fs.StringVar(&cfg.dataDir, "data-dir", cfg.dataDir, "data dir")
	fs.StringVar(&cfg.mcpHost, "mcp-host", "127.0.0.1", "mcp listener host")
	fs.IntVar(&cfg.mcpPort, "mcp-port", serve.DefaultPort, "mcp listener port")
	fs.BoolVar(&cfg.unsafePublicMCP, "unsafe-public-mcp", false, "allow non-loopback MCP bind (unsafe)")
	fs.StringVar(&cfg.mcpUnixSocket, "mcp-unix-socket", "", "path to a Unix domain socket; when set, the MCP listener binds the socket (mode 0600) instead of a TCP port — only the file's owning user (and root) can connect")
	if err := fs.Parse(args); err != nil {
		return nil, 2
	}
	cfg.projectDir = *projectDir
	if cfg.mcpUnixSocket == "" && !cfg.unsafePublicMCP && !isLoopbackOnlyHost(cfg.mcpHost) {
		fmt.Fprintln(os.Stderr, "refusing non-loopback MCP bind; use --mcp-unix-socket /path or --unsafe-public-mcp to override")
		return nil, 2
	}
	return cfg, 0
}

func runServer(cfg *serverConfig) int {
	res, err := protocol.BootstrapProject(cfg.projectDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	mcpApp := serve.NewApp(res.Paths.ProjectDir, cfg.dataDir)
	mcpErrCh := make(chan error, 1)
	if cfg.mcpUnixSocket != "" {
		go func() {
			mcpErrCh <- mcpApp.StartUnix(cfg.mcpUnixSocket)
		}()
		if err := waitForMCPReadyUnix(cfg.mcpUnixSocket, 10*time.Second, mcpErrCh); err != nil {
			_ = mcpApp.Stop(context.Background())
			fmt.Fprintf(os.Stderr, "failed to start local MCP listener: %v\n", err)
			return 1
		}
		fmt.Printf("sloptools MCP server ready at http+unix://%s/mcp\n", cfg.mcpUnixSocket)
	} else {
		go func() {
			mcpErrCh <- mcpApp.Start(cfg.mcpHost, cfg.mcpPort)
		}()
		mcpURL := (&url.URL{
			Scheme: "http",
			Host:   net.JoinHostPort(cfg.mcpHost, fmt.Sprintf("%d", cfg.mcpPort)),
			Path:   "/mcp",
		}).String()
		if err := waitForMCPReady(cfg.mcpHost, cfg.mcpPort, 10*time.Second, mcpErrCh); err != nil {
			_ = mcpApp.Stop(context.Background())
			fmt.Fprintf(os.Stderr, "failed to start local MCP listener: %v\n", err)
			return 1
		}
		fmt.Printf("sloptools MCP server ready at %s\n", mcpURL)
	}
	select {
	case mcpErr := <-mcpErrCh:
		if mcpErr != nil {
			fmt.Fprintf(os.Stderr, "mcp listener failed: %v\n", mcpErr)
			return 1
		}
	}
	return 0
}

func isLoopbackOnlyHost(host string) bool {
	trimmed := strings.TrimSpace(strings.ToLower(host))
	trimmed = strings.Trim(trimmed, "[]")
	if trimmed == "localhost" {
		return true
	}
	switch trimmed {
	case "127.0.0.1", "::1":
		return true
	case "", "0.0.0.0", "::":
		return false
	}
	ip := net.ParseIP(trimmed)
	return ip != nil && ip.IsLoopback()
}

func waitForMCPReadyUnix(socketPath string, timeout time.Duration, mcpErrCh <-chan error) error {
	deadline := time.Now().Add(timeout)
	dial := func(_ context.Context, _, _ string) (net.Conn, error) {
		return net.Dial("unix", socketPath)
	}
	client := &http.Client{
		Timeout:   750 * time.Millisecond,
		Transport: &http.Transport{DialContext: dial},
	}
	for time.Now().Before(deadline) {
		select {
		case err := <-mcpErrCh:
			if err == nil {
				return errors.New("mcp listener exited before becoming healthy")
			}
			return fmt.Errorf("mcp listener failed to start: %w", err)
		default:
		}
		resp, err := client.Get("http://unix/health")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	select {
	case err := <-mcpErrCh:
		if err == nil {
			return errors.New("mcp listener exited before becoming healthy")
		}
		return fmt.Errorf("mcp listener failed to start: %w", err)
	default:
	}
	return errors.New("mcp health check timeout")
}

func waitForMCPReady(host string, port int, timeout time.Duration, mcpErrCh <-chan error) error {
	deadline := time.Now().Add(timeout)
	healthURL := fmt.Sprintf("http://%s/health", net.JoinHostPort(host, fmt.Sprintf("%d", port)))
	client := &http.Client{Timeout: 750 * time.Millisecond}
	for time.Now().Before(deadline) {
		select {
		case err := <-mcpErrCh:
			if err == nil {
				return errors.New("mcp listener exited before becoming healthy")
			}
			return fmt.Errorf("mcp listener failed to start: %w", err)
		default:
		}
		resp, err := client.Get(healthURL)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	select {
	case err := <-mcpErrCh:
		if err == nil {
			return errors.New("mcp listener exited before becoming healthy")
		}
		return fmt.Errorf("mcp listener failed to start: %w", err)
	default:
	}
	return errors.New("mcp health check timeout")
}

func cmdVersion() int {
	fmt.Println(formatVersionLine(version, commit, runtime.GOOS, runtime.GOARCH))
	return 0
}

func formatVersionLine(rawVersion, rawCommit, goos, goarch string) string {
	release := strings.TrimSpace(rawVersion)
	if release == "" {
		release = "0.0.0"
	}
	if !strings.HasPrefix(strings.ToLower(release), "v") {
		release = "v" + release
	}
	shortCommit := strings.TrimSpace(rawCommit)
	if shortCommit == "" {
		shortCommit = "unknown"
	}
	return fmt.Sprintf("sloptools %s (%s) %s/%s", release, shortCommit, goos, goarch)
}
