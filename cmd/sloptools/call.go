package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sloppy-org/sloptools/internal/mcp"
	"github.com/sloppy-org/sloptools/internal/protocol"
	"github.com/sloppy-org/sloptools/internal/store"
)

func cmdTools(args []string) int {
	if len(args) == 0 {
		printToolsHelp()
		return 2
	}
	switch args[0] {
	case "list":
		return cmdToolsList(args[1:])
	case "call":
		return cmdToolsCall(args[1:])
	case "help", "-h", "--help":
		printToolsHelp()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown tools subcommand: %s\n", args[0])
		printToolsHelp()
		return 2
	}
}

func printToolsHelp() {
	fmt.Println("sloptools tools <list|call> [flags]")
	fmt.Println()
	fmt.Println("list flags:")
	fmt.Println("  --data-dir PATH      sloppy data dir (default ~/.local/share/sloppy)")
	fmt.Println("  --project-dir PATH   project dir (default .)")
	fmt.Println("  --format FMT         table (default) or json")
	fmt.Println()
	fmt.Println("call flags:")
	fmt.Println("  --data-dir PATH      sloppy data dir (default ~/.local/share/sloppy)")
	fmt.Println("  --project-dir PATH   project dir (default .)")
	fmt.Println("  --args JSON          full arguments object as JSON")
	fmt.Println("  --args-file PATH     read arguments JSON object from file")
	fmt.Println("  --arg key=value      single argument; repeatable; value parsed as JSON when possible")
}

type stringPairsFlag []string

func (s *stringPairsFlag) String() string { return strings.Join(*s, ",") }

func (s *stringPairsFlag) Set(value string) error {
	if !strings.Contains(value, "=") {
		return fmt.Errorf("--arg expects key=value, got %q", value)
	}
	*s = append(*s, value)
	return nil
}

func newToolsServer(projectDir, dataDir string) (*mcp.Server, *store.Store, error) {
	res, err := protocol.BootstrapProject(projectDir)
	if err != nil {
		return nil, nil, fmt.Errorf("bootstrap project: %w", err)
	}
	dir := strings.TrimSpace(dataDir)
	if dir == "" {
		dir = defaultDataDir()
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("create data dir: %w", err)
	}
	st, err := store.New(filepath.Join(dir, "sloppy.db"))
	if err != nil {
		return nil, nil, fmt.Errorf("open store: %w", err)
	}
	return mcp.NewServerWithStore(res.Paths.ProjectDir, st), st, nil
}

func cmdToolsList(args []string) int {
	fs := flag.NewFlagSet("tools list", flag.ContinueOnError)
	dataDir := fs.String("data-dir", defaultDataDir(), "sloppy data dir")
	projectDir := fs.String("project-dir", ".", "project dir")
	format := fs.String("format", "table", "output format: table or json")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	srv, st, err := newToolsServer(*projectDir, *dataDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer st.Close()

	resp := srv.DispatchMessage(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/list",
		"params":  map[string]interface{}{},
	})
	if code, msg, ok := rpcErrorFromResponse(resp); ok {
		fmt.Fprintln(os.Stderr, msg)
		_ = code
		return 2
	}
	result, _ := resp["result"].(map[string]interface{})
	tools, _ := result["tools"].([]map[string]interface{})
	if tools == nil {
		// dispatch returns []map[string]interface{}; tolerate generic []interface{} just in case
		if generic, ok := result["tools"].([]interface{}); ok {
			tools = make([]map[string]interface{}, 0, len(generic))
			for _, t := range generic {
				if m, ok := t.(map[string]interface{}); ok {
					tools = append(tools, m)
				}
			}
		}
	}

	switch strings.ToLower(strings.TrimSpace(*format)) {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(tools); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	case "", "table":
		for _, t := range tools {
			name, _ := t["name"].(string)
			desc, _ := t["description"].(string)
			fmt.Fprintf(os.Stdout, "%s\t%s\n", name, desc)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown --format %q (want table or json)\n", *format)
		return 2
	}
	return 0
}

func cmdToolsCall(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "tools call requires <tool-name>")
		return 2
	}
	toolName := strings.TrimSpace(args[0])
	if toolName == "" || strings.HasPrefix(toolName, "-") {
		fmt.Fprintln(os.Stderr, "tools call requires a non-empty tool name as first positional argument")
		return 2
	}
	fs := flag.NewFlagSet("tools call", flag.ContinueOnError)
	dataDir := fs.String("data-dir", defaultDataDir(), "sloppy data dir")
	projectDir := fs.String("project-dir", ".", "project dir")
	argsJSON := fs.String("args", "", "arguments object as JSON")
	argsFile := fs.String("args-file", "", "path to arguments JSON file")
	var pairs stringPairsFlag
	fs.Var(&pairs, "arg", "single argument key=value (repeatable)")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	if rest := fs.Args(); len(rest) > 0 {
		fmt.Fprintf(os.Stderr, "tools call: unexpected extra arguments after flags: %v\n", rest)
		return 2
	}

	arguments, err := buildToolArguments(*argsJSON, *argsFile, pairs)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	srv, st, err := newToolsServer(*projectDir, *dataDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer st.Close()

	resp := srv.DispatchMessage(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params":  map[string]interface{}{"name": toolName, "arguments": arguments},
	})
	if _, msg, ok := rpcErrorFromResponse(resp); ok {
		fmt.Fprintln(os.Stderr, msg)
		return 2
	}
	result, _ := resp["result"].(map[string]interface{})
	if result == nil {
		fmt.Fprintln(os.Stderr, "tools/call: empty result")
		return 1
	}
	isError, _ := result["isError"].(bool)
	if isError {
		fmt.Fprintln(os.Stderr, joinContentText(result["content"]))
		return 1
	}
	if structured, ok := result["structuredContent"].(map[string]interface{}); ok && structured != nil {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(structured); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
	fmt.Fprintln(os.Stdout, joinContentText(result["content"]))
	return 0
}

func buildToolArguments(rawJSON, file string, pairs []string) (map[string]interface{}, error) {
	sources := 0
	if strings.TrimSpace(rawJSON) != "" {
		sources++
	}
	if strings.TrimSpace(file) != "" {
		sources++
	}
	if len(pairs) > 0 {
		sources++
	}
	if sources > 1 {
		return nil, errors.New("--args, --args-file, and --arg are mutually exclusive")
	}
	if strings.TrimSpace(rawJSON) != "" {
		return decodeArgumentsJSON([]byte(rawJSON), "--args")
	}
	if path := strings.TrimSpace(file); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read --args-file: %w", err)
		}
		return decodeArgumentsJSON(data, "--args-file")
	}
	out := make(map[string]interface{}, len(pairs))
	for _, p := range pairs {
		idx := strings.IndexByte(p, '=')
		if idx <= 0 {
			return nil, fmt.Errorf("--arg expects key=value, got %q", p)
		}
		key := p[:idx]
		value := p[idx+1:]
		var parsed interface{}
		if err := json.Unmarshal([]byte(value), &parsed); err == nil {
			out[key] = parsed
		} else {
			out[key] = value
		}
	}
	return out, nil
}

func decodeArgumentsJSON(data []byte, source string) (map[string]interface{}, error) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return map[string]interface{}{}, nil
	}
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(trimmed), &out); err != nil {
		return nil, fmt.Errorf("invalid %s JSON: %w", source, err)
	}
	if out == nil {
		return nil, fmt.Errorf("%s must be a JSON object", source)
	}
	return out, nil
}

func rpcErrorFromResponse(resp map[string]interface{}) (int, string, bool) {
	if resp == nil {
		return 0, "", false
	}
	raw, ok := resp["error"]
	if !ok || raw == nil {
		return 0, "", false
	}
	switch v := raw.(type) {
	case map[string]interface{}:
		code, _ := v["code"].(int)
		if code == 0 {
			if f, ok := v["code"].(float64); ok {
				code = int(f)
			}
		}
		msg, _ := v["message"].(string)
		return code, msg, true
	case mcp.RPCError:
		return v.Code, v.Message, true
	default:
		return 0, fmt.Sprintf("%v", v), true
	}
}

func joinContentText(raw interface{}) string {
	switch items := raw.(type) {
	case []map[string]string:
		parts := make([]string, 0, len(items))
		for _, item := range items {
			if t := item["text"]; t != "" {
				parts = append(parts, t)
			}
		}
		return strings.Join(parts, "\n")
	case []interface{}:
		parts := make([]string, 0, len(items))
		for _, item := range items {
			m, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			if t, ok := m["text"].(string); ok && t != "" {
				parts = append(parts, t)
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}
