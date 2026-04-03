package licensing

import (
	"os/exec"
	"strings"
	"testing"
)

func TestNoKnownGPLSidecarDependenciesAreLinkedIntoGoBinary(t *testing.T) {
	t.Parallel()

	cmd := exec.Command("go", "list", "-deps", "-f", "{{if .Module}}{{.Module.Path}}{{end}}", "./cmd/sloppy")
	cmd.Dir = repoRoot(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list deps failed: %v\n%s", err, string(out))
	}

	forbidden := []string{"piper", "ffmpeg", "whisper", "openwakeword", "llama.cpp"}
	for _, line := range strings.Split(string(out), "\n") {
		module := strings.TrimSpace(line)
		if module == "" {
			continue
		}
		lowered := strings.ToLower(module)
		for _, token := range forbidden {
			if strings.Contains(lowered, token) {
				t.Fatalf("forbidden sidecar dependency token %q found in module path %q", token, module)
			}
		}
	}
}
