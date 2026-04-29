package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captureRun runs run(args) with stdout and stderr redirected to pipes,
// returning the captured stdout, stderr and exit code.
func captureRun(t *testing.T, args []string) (string, string, int) {
	t.Helper()
	origOut, origErr := os.Stdout, os.Stderr
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdout: %v", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stderr: %v", err)
	}
	os.Stdout = outW
	os.Stderr = errW
	done := make(chan struct{})
	var outBuf, errBuf strings.Builder
	go func() {
		_, _ = io.Copy(&outBuf, outR)
		close(done)
	}()
	doneErr := make(chan struct{})
	go func() {
		_, _ = io.Copy(&errBuf, errR)
		close(doneErr)
	}()
	code := run(args)
	_ = outW.Close()
	_ = errW.Close()
	<-done
	<-doneErr
	os.Stdout = origOut
	os.Stderr = origErr
	return outBuf.String(), errBuf.String(), code
}

func TestMCPServerAcceptsCanonicalStdioFlags(t *testing.T) {
	tmp := t.TempDir()
	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdin: %v", err)
	}
	_ = inW.Close()
	origIn := os.Stdin
	os.Stdin = inR
	t.Cleanup(func() {
		os.Stdin = origIn
		_ = inR.Close()
	})

	_, stderr, code := captureRun(t, []string{
		"mcp-server",
		"--stdio",
		"--vault-config", filepath.Join(tmp, "vaults.toml"),
		"--project-dir", filepath.Join(tmp, "project"),
		"--data-dir", filepath.Join(tmp, "data"),
	})
	if code != 0 {
		t.Fatalf("mcp-server canonical flags code=%d stderr=%q", code, stderr)
	}
}

func TestToolsListJSONIncludesKnownTools(t *testing.T) {
	tmp := t.TempDir()
	dataDir := filepath.Join(tmp, "data")
	projectDir := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}
	stdout, stderr, code := captureRun(t, []string{
		"tools", "list",
		"--data-dir", dataDir,
		"--project-dir", projectDir,
		"--format", "json",
	})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr)
	}
	if stderr != "" {
		t.Fatalf("unexpected stderr: %q", stderr)
	}
	var tools []map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &tools); err != nil {
		t.Fatalf("decode JSON: %v\nstdout=%s", err, stdout)
	}
	if len(tools) == 0 {
		t.Fatalf("expected non-empty tools list")
	}
	names := make(map[string]bool, len(tools))
	for _, tool := range tools {
		name, _ := tool["name"].(string)
		if name == "" {
			t.Fatalf("tool entry missing name: %v", tool)
		}
		names[name] = true
	}
	for _, want := range []string{"mail_send", "calendar_events", "handoff.create"} {
		if !names[want] {
			t.Errorf("expected tool %q in list, got %d entries", want, len(names))
		}
	}
}

func TestToolsListTableEmitsRowPerTool(t *testing.T) {
	tmp := t.TempDir()
	stdout, stderr, code := captureRun(t, []string{
		"tools", "list",
		"--data-dir", filepath.Join(tmp, "data"),
		"--project-dir", filepath.Join(tmp, "project"),
	})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr)
	}
	if stderr != "" {
		t.Fatalf("unexpected stderr: %q", stderr)
	}
	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if len(lines) < 5 {
		t.Fatalf("expected several rows, got %d: %q", len(lines), stdout)
	}
	jsonOut, _, jsonCode := captureRun(t, []string{
		"tools", "list",
		"--data-dir", filepath.Join(tmp, "data2"),
		"--project-dir", filepath.Join(tmp, "project2"),
		"--format", "json",
	})
	if jsonCode != 0 {
		t.Fatalf("json exit = %d", jsonCode)
	}
	var tools []map[string]interface{}
	if err := json.Unmarshal([]byte(jsonOut), &tools); err != nil {
		t.Fatalf("decode tools json: %v", err)
	}
	if len(lines) != len(tools) {
		t.Fatalf("table rows=%d, json tools=%d", len(lines), len(tools))
	}
	for _, line := range lines {
		if !strings.Contains(line, "\t") {
			t.Fatalf("table row missing tab separator: %q", line)
		}
	}
}

func TestToolsCallUnknownToolExitsOneWithStderr(t *testing.T) {
	tmp := t.TempDir()
	stdout, stderr, code := captureRun(t, []string{
		"tools", "call", "definitely_not_a_tool",
		"--data-dir", filepath.Join(tmp, "data"),
		"--project-dir", filepath.Join(tmp, "project"),
	})
	if code != 1 {
		t.Fatalf("exit code = %d, want 1; stdout=%q stderr=%q", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "unknown tool") {
		t.Fatalf("stderr missing 'unknown tool': %q", stderr)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Fatalf("unexpected stdout: %q", stdout)
	}
}

func TestToolsCallTempFileCreateProducesJSON(t *testing.T) {
	tmp := t.TempDir()
	projectDir := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}
	stdout, stderr, code := captureRun(t, []string{
		"tools", "call", "temp_file_create",
		"--data-dir", filepath.Join(tmp, "data"),
		"--project-dir", projectDir,
		"--args", `{"name":"pi-test","content":"hello"}`,
	})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr)
	}
	if stderr != "" {
		t.Fatalf("unexpected stderr: %q", stderr)
	}
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("decode JSON: %v\nstdout=%s", err, stdout)
	}
	if ok, _ := result["ok"].(bool); !ok {
		t.Fatalf("expected ok=true, got: %v", result)
	}
	abs, _ := result["abs_path"].(string)
	if abs == "" {
		t.Fatalf("expected abs_path, got: %v", result)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("read created file: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("file content = %q, want %q", string(data), "hello")
	}
}

func TestLoadEnvFilePreservesExistingValues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "env")
	t.Setenv("SLOPPY_EXISTING", "from-process")
	if err := os.WriteFile(path, []byte("SLOPPY_EXISTING=from-file\nexport SLOPPY_NEW='new value'\n"), 0o644); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	if err := loadEnvFile(path); err != nil {
		t.Fatalf("loadEnvFile failed: %v", err)
	}
	if got := os.Getenv("SLOPPY_EXISTING"); got != "from-process" {
		t.Fatalf("SLOPPY_EXISTING = %q, want process value", got)
	}
	if got := os.Getenv("SLOPPY_NEW"); got != "new value" {
		t.Fatalf("SLOPPY_NEW = %q, want parsed file value", got)
	}
}

func TestToolsCallArgPairsParseJSONValues(t *testing.T) {
	args, err := buildToolArguments("", "", []string{"foo=bar", "n=3", "flag=true", "obj={\"k\":1}"})
	if err != nil {
		t.Fatalf("buildToolArguments: %v", err)
	}
	if got, want := args["foo"], "bar"; got != want {
		t.Errorf("foo = %v (%T), want %v", got, got, want)
	}
	nFloat, ok := args["n"].(float64)
	if !ok {
		t.Fatalf("n type = %T, want float64 (JSON number)", args["n"])
	}
	if int(nFloat) != 3 {
		t.Errorf("n = %v, want 3", nFloat)
	}
	if got, ok := args["flag"].(bool); !ok || !got {
		t.Errorf("flag = %v (%T), want true bool", args["flag"], args["flag"])
	}
	obj, ok := args["obj"].(map[string]interface{})
	if !ok {
		t.Fatalf("obj type = %T, want map", args["obj"])
	}
	if v, _ := obj["k"].(float64); int(v) != 1 {
		t.Errorf("obj.k = %v, want 1", obj["k"])
	}
}

func TestToolsCallArgPairsBuildIntegerThroughCLI(t *testing.T) {
	tmp := t.TempDir()
	projectDir := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	stdout, stderr, code := captureRun(t, []string{
		"tools", "call", "temp_file_create",
		"--data-dir", filepath.Join(tmp, "data"),
		"--project-dir", projectDir,
		"--arg", "prefix=pi",
		"--arg", "n=3",
	})
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%q", code, stderr)
	}
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	path, _ := result["path"].(string)
	if !strings.Contains(path, "/pi-") {
		t.Fatalf("expected prefix 'pi' in path %q", path)
	}
}

func TestToolsCallArgsSourcesMutuallyExclusive(t *testing.T) {
	if _, err := buildToolArguments(`{"a":1}`, "", []string{"b=2"}); err == nil {
		t.Fatalf("expected mutual-exclusion error")
	}
	if _, err := buildToolArguments(`{"a":1}`, "/some/path", nil); err == nil {
		t.Fatalf("expected mutual-exclusion error")
	}
}

func TestToolsListUnknownFormatRejected(t *testing.T) {
	tmp := t.TempDir()
	_, stderr, code := captureRun(t, []string{
		"tools", "list",
		"--data-dir", filepath.Join(tmp, "data"),
		"--project-dir", filepath.Join(tmp, "project"),
		"--format", "xml",
	})
	if code != 2 {
		t.Fatalf("exit = %d, want 2; stderr=%q", code, stderr)
	}
	if !strings.Contains(stderr, "unknown --format") {
		t.Fatalf("stderr missing format message: %q", stderr)
	}
}
