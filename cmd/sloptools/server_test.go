package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRunServerStopsCleanlyOnSignalForUnixSocket(t *testing.T) {
	projectDir := t.TempDir()
	dataDir := filepath.Join(t.TempDir(), "data")
	socketPath := filepath.Join(t.TempDir(), "runtime", "mcp.sock")

	origSignalNotifyContext := signalNotifyContext
	t.Cleanup(func() {
		signalNotifyContext = origSignalNotifyContext
	})
	signalNotifyContext = func(parent context.Context, _ ...os.Signal) (context.Context, context.CancelFunc) {
		ctx, cancel := context.WithCancel(parent)
		go func() {
			time.Sleep(300 * time.Millisecond)
			cancel()
		}()
		return ctx, func() {}
	}

	code := runServer(&serverConfig{
		projectDir:    projectDir,
		dataDir:       dataDir,
		mcpUnixSocket: socketPath,
	})
	if code != 0 {
		t.Fatalf("runServer(unix socket) code = %d, want 0", code)
	}
	if _, err := os.Stat(socketPath); !os.IsNotExist(err) {
		t.Fatalf("socket path still exists after shutdown: err=%v", err)
	}
}
