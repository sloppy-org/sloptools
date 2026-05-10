package backend_test

import (
	"os/exec"
	"testing"

	"github.com/sloppy-org/sloptools/internal/brain/backend"
)

func TestMCPClientListTools(t *testing.T) {
	// Requires sloptools on PATH (the project builds and installs it).
	if _, err := exec.LookPath("sloptools"); err != nil {
		t.Skip("sloptools not on PATH:", err)
	}

	spec := backend.MCPServerSpec{
		Command: "sloptools",
		Args:    []string{"mcp-server"},
	}
	c, err := backend.NewMCPClient(spec, nil)
	if err != nil {
		t.Fatalf("NewMCPClient: %v", err)
	}
	defer c.Close()

	tools, err := c.ListTools()
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) == 0 {
		t.Fatal("expected at least one tool, got none")
	}
	found := false
	for _, td := range tools {
		if td.Name == "sloppy_brain" {
			found = true
			break
		}
	}
	if !found {
		names := make([]string, len(tools))
		for i, td := range tools {
			names[i] = td.Name
		}
		t.Fatalf("sloppy_brain not found in tools: %v", names)
	}
}
