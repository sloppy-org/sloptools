package backend_test

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/sloppy-org/sloptools/internal/brain/backend"
)

func TestLlamacppBackendNoToolSingleShot(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1 to run live LLM tests")
	}
	// Skip if the slopgate endpoint is not reachable.
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(backend.LlamacppBaseURL + "/health")
	if err != nil || resp.StatusCode >= 500 {
		t.Skipf("slopgate %s unreachable: %v", backend.LlamacppBaseURL, err)
	}
	if resp != nil {
		_ = resp.Body.Close()
	}

	// Write a minimal system prompt to a temp file.
	f, err := os.CreateTemp("", "llamacpp-test-prompt-*.md")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString("You are a test assistant. Reply with a single short sentence."); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	// Build a minimal sandbox (workdir only; no MCP needed).
	sb := &backend.Sandbox{
		Root:    t.TempDir(),
		WorkDir: t.TempDir(),
	}

	be := backend.LlamacppBackend{}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	got, err := be.Run(ctx, backend.Request{
		Stage:            "test-single-shot",
		Packet:           "Say hello in one sentence.",
		SystemPromptPath: f.Name(),
		Model:            "llamacpp/qwen27b",
		Reasoning:        backend.ReasoningHigh,
		AllowEdits:       false,
		Sandbox:          sb,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got.Output == "" {
		t.Fatal("expected non-empty output")
	}
}
