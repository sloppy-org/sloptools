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

	"github.com/krystophny/sloppy/internal/mcp"
	"github.com/krystophny/sloppy/internal/protocol"
	"github.com/krystophny/sloppy/internal/serve"
	"github.com/krystophny/sloppy/internal/store"
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
	case "version":
		return cmdVersion()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", args[0])
		printHelp()
		return 2
	}
}

func printHelp() {
	fmt.Println("sloppy <command> [flags]")
	fmt.Println("commands: bootstrap server mcp-server version")
}

type serverConfig struct {
	dataDir         string
	projectDir      string
	mcpHost         string
	mcpPort         int
	unsafePublicMCP bool
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
	projectDir := fs.String("project-dir", ".", "project dir")
	dataDir := fs.String("data-dir", filepath.Join(os.Getenv("HOME"), ".sloppy"), "data dir")
	if err := fs.Parse(args); err != nil {
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
	return mcp.RunStdioWithStore(res.Paths.ProjectDir, st)
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
		dataDir: filepath.Join(os.Getenv("HOME"), ".sloppy"),
	}
	projectDir := fs.String("project-dir", ".", "project dir")
	fs.StringVar(&cfg.dataDir, "data-dir", cfg.dataDir, "data dir")
	fs.StringVar(&cfg.mcpHost, "mcp-host", "127.0.0.1", "mcp listener host")
	fs.IntVar(&cfg.mcpPort, "mcp-port", serve.DefaultPort, "mcp listener port")
	fs.BoolVar(&cfg.unsafePublicMCP, "unsafe-public-mcp", false, "allow non-loopback MCP bind (unsafe)")
	if err := fs.Parse(args); err != nil {
		return nil, 2
	}
	cfg.projectDir = *projectDir
	if !cfg.unsafePublicMCP && !isLoopbackOnlyHost(cfg.mcpHost) {
		fmt.Fprintln(os.Stderr, "refusing non-loopback MCP bind; use --unsafe-public-mcp to override")
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
	fmt.Printf("sloppy MCP server ready at %s\n", mcpURL)
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
	return fmt.Sprintf("sloppy %s (%s) %s/%s", release, shortCommit, goos, goarch)
}
