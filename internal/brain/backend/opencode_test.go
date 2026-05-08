package backend

import (
	"strings"
	"testing"
)

// TestOpencodeArgvDoesNotEmbedPacket guards against #128: a 200 KB sleep
// packet baked into the opencode argv triggers fork/exec
// "argument list too long" because the kernel ARG_MAX (~128 KB on Linux,
// ~256 KB on macOS) caps the total environment + argv size. Buld the
// argv via the same helper Run uses, with a 200 KB synthetic prompt, and
// assert no single element exceeds 16 KB and the prompt body is not
// embedded anywhere in the argv. The packet must arrive on stdin.
func TestOpencodeArgvDoesNotEmbedPacket(t *testing.T) {
	const maxArg = 16 * 1024
	prompt := strings.Repeat("a", 200*1024)
	args := opencodeArgs("brain-stage", "llamacpp/qwen", "high", "/tmp/work")
	for i, a := range args {
		if len(a) > maxArg {
			t.Fatalf("argv[%d] length %d exceeds %d-byte cap (kernel ARG_MAX risk)", i, len(a), maxArg)
		}
	}
	for i, a := range args {
		if strings.Contains(a, prompt) {
			t.Fatalf("argv[%d] contains the prompt body; packet must travel on stdin not argv", i)
		}
	}
	// Concatenated argv stays well under kernel ARG_MAX even with a 200 KB
	// prompt because the prompt is not in argv at all.
	total := 0
	for _, a := range args {
		total += len(a) + 1
	}
	if total > 8*1024 {
		t.Fatalf("argv total bytes %d unexpectedly large; argv should be CLI flags only", total)
	}
}

func TestParseOpencodeJSON_TextOnly(t *testing.T) {
	raw := strings.Join([]string{
		`{"type":"step_start","part":{"type":"step-start"}}`,
		`{"type":"text","part":{"type":"text","text":"# Scout report\n\n## Verified\n- a (source: x)\n"}}`,
		`{"type":"step_finish","part":{"type":"step-finish","tokens":{"input":120,"output":40}}}`,
	}, "\n")
	body, tin, tout := parseOpencodeJSON([]byte(raw))
	if !strings.HasPrefix(body, "# Scout report") {
		t.Fatalf("body = %q, want it to start with markdown heading", body)
	}
	if !strings.Contains(body, "## Verified") {
		t.Fatalf("body lost section heading: %q", body)
	}
	if tin != 120 || tout != 40 {
		t.Fatalf("tokens = %d/%d, want 120/40", tin, tout)
	}
}

func TestParseOpencodeJSON_PrefersWriteToolContent(t *testing.T) {
	report := "# Scout report — Foo\n\n## Verified\n- bar (source: y)\n"
	raw := strings.Join([]string{
		`{"type":"step_start","part":{"type":"step-start"}}`,
		`{"type":"text","part":{"type":"text","text":"Now I have all the evidence. Let me compile the report."}}`,
		`{"type":"tool_use","part":{"type":"tool","tool":"write","state":{"status":"completed","input":{"filePath":"/tmp/report.md","content":` + jsonString(report) + `},"output":"Wrote file."}}}`,
		`{"type":"text","part":{"type":"text","text":"Here's a summary of what was resolved."}}`,
		`{"type":"step_finish","part":{"type":"step-finish","tokens":{"input":900,"output":600}}}`,
	}, "\n")
	body, tin, tout := parseOpencodeJSON([]byte(raw))
	if body != strings.TrimSpace(report) {
		t.Fatalf("body =\n%q\nwant exactly the write-tool content:\n%q", body, strings.TrimSpace(report))
	}
	if strings.Contains(body, "summary of what was resolved") || strings.Contains(body, "Now I have all the evidence") {
		t.Fatalf("body leaked text-channel narration: %q", body)
	}
	if tin != 900 || tout != 600 {
		t.Fatalf("tokens = %d/%d, want 900/600", tin, tout)
	}
}

func TestParseOpencodeJSON_LastWriteWins(t *testing.T) {
	first := "# First write\n"
	second := "# Second write — final\n"
	raw := strings.Join([]string{
		`{"type":"tool_use","part":{"type":"tool","tool":"write","state":{"status":"completed","input":{"filePath":"/tmp/a.md","content":` + jsonString(first) + `}}}}`,
		`{"type":"tool_use","part":{"type":"tool","tool":"write","state":{"status":"completed","input":{"filePath":"/tmp/a.md","content":` + jsonString(second) + `}}}}`,
		`{"type":"text","part":{"type":"text","text":"Done."}}`,
	}, "\n")
	body, _, _ := parseOpencodeJSON([]byte(raw))
	if body != strings.TrimSpace(second) {
		t.Fatalf("body = %q, want last write content %q", body, strings.TrimSpace(second))
	}
}

func TestParseOpencodeJSON_EditToolIgnored(t *testing.T) {
	raw := strings.Join([]string{
		`{"type":"text","part":{"type":"text","text":"# Scout report\n\nbody from text channel\n"}}`,
		`{"type":"tool_use","part":{"type":"tool","tool":"edit","state":{"status":"completed","input":{"filePath":"/tmp/a.md","oldString":"foo","newString":"bar"}}}}`,
	}, "\n")
	body, _, _ := parseOpencodeJSON([]byte(raw))
	if !strings.Contains(body, "body from text channel") {
		t.Fatalf("edit-only call should not displace text content: got %q", body)
	}
}

func TestParseOpencodeJSON_FallsBackOnEmptyWriteContent(t *testing.T) {
	raw := strings.Join([]string{
		`{"type":"text","part":{"type":"text","text":"# Scout report\n- bullet\n"}}`,
		`{"type":"tool_use","part":{"type":"tool","tool":"write","state":{"status":"completed","input":{"filePath":"/tmp/a.md","content":""}}}}`,
	}, "\n")
	body, _, _ := parseOpencodeJSON([]byte(raw))
	if !strings.Contains(body, "# Scout report") {
		t.Fatalf("empty write should not displace text content: got %q", body)
	}
}

func TestParseOpencodeJSON_StripsWrappingFences(t *testing.T) {
	raw := `{"type":"text","part":{"type":"text","text":"` + "```markdown\\n# Scout report\\n## Verified\\n- a\\n```" + `"}}`
	body, _, _ := parseOpencodeJSON([]byte(raw))
	if strings.HasPrefix(body, "```") || strings.HasSuffix(body, "```") {
		t.Fatalf("fence not stripped: %q", body)
	}
	if !strings.HasPrefix(body, "# Scout report") {
		t.Fatalf("body = %q, want fence-stripped markdown", body)
	}
}

func TestParseOpencodeJSON_TopLevelUsage(t *testing.T) {
	raw := strings.Join([]string{
		`{"type":"text","part":{"type":"text","text":"hi"}}`,
		`{"type":"step_finish","usage":{"input_tokens":1500,"output_tokens":42}}`,
	}, "\n")
	_, tin, tout := parseOpencodeJSON([]byte(raw))
	if tin != 1500 || tout != 42 {
		t.Fatalf("tokens = %d/%d, want 1500/42", tin, tout)
	}
}

func TestOpencodeAgentFrontmatterDeniesWriteAndBash(t *testing.T) {
	tmpl := opencodeAgentFrontmatter()
	// Edit/write/apply_patch is the file-write side channel — must remain denied.
	for _, want := range []string{"  edit: deny", "  '*': allow", "mode: primary"} {
		if !strings.Contains(tmpl, want) {
			t.Fatalf("agent frontmatter missing %q:\n%s", want, tmpl)
		}
	}
	// Bash is now an allowlisted object, not a flat deny.
	if !strings.Contains(tmpl, "  bash:") {
		t.Fatalf("agent frontmatter missing bash key:\n%s", tmpl)
	}
	if strings.Contains(tmpl, "  bash: deny") {
		t.Fatalf("bash is no longer a flat deny — should be an object allowlist:\n%s", tmpl)
	}
	if !strings.Contains(tmpl, "    \"*\": deny") {
		t.Fatalf("bash allowlist must default-deny via \"*\":\n%s", tmpl)
	}
	for _, want := range []string{
		"\"ls\": allow",
		"\"ls *\": allow",
		"\"head *\": allow",
		"\"tail *\": allow",
		"\"wc *\": allow",
		"\"file *\": allow",
		"\"find *\": allow",
		"\"stat *\": allow",
		"\"pwd\": allow",
		"\"rg --files*\": allow",
	} {
		if !strings.Contains(tmpl, want) {
			t.Fatalf("bash allowlist missing %q:\n%s", want, tmpl)
		}
	}
	// Negative: cat / pdftotext / curl / wget / awk / sed / grep / git
	// must NOT be in the allowlist. Helpy MCP tools cover the bounded
	// equivalents (pdf_read, web_fetch).
	for _, deny := range []string{
		"\"cat\": allow",
		"\"cat *\": allow",
		"\"pdftotext",
		"\"pdfinfo",
		"\"curl",
		"\"wget",
		"\"awk",
		"\"sed",
		"\"grep ",
		"\"git ",
		"\"python",
		"\"bash ",
	} {
		if strings.Contains(tmpl, deny) {
			t.Fatalf("bash allowlist must NOT include %q:\n%s", deny, tmpl)
		}
	}
	if !strings.HasPrefix(tmpl, "---\n") || !strings.HasSuffix(tmpl, "\n---") {
		t.Fatalf("agent frontmatter is not a closed YAML block:\n%s", tmpl)
	}
}

// TestExtractToolFailures_ErroredStateSurfacesUnderlyingError covers
// sloptools issue #130: brain-night runs were logging only "mcp:
// sloppy/brain.search (failed)" with no diagnostic, so we could not
// tell whether the cluster of failures was a connection reset, a rate
// limit, or an upstream config. The fix surfaces the innermost error
// from the opencode JSON event stream's tool_use.state shape into a
// structured (server, tool, attempt, last_error) record, which the
// caller logs.
func TestExtractToolFailures_ErroredStateSurfacesUnderlyingError(t *testing.T) {
	raw := strings.Join([]string{
		`{"type":"text","part":{"type":"text","text":"Searching..."}}`,
		`{"type":"tool_use","part":{"type":"tool","tool":"sloppy_brain_search","state":{"status":"errored","error":"mcp: connection reset by peer","input":{"query":"Stadler"}}}}`,
		`{"type":"tool_use","part":{"type":"tool","tool":"helpy_web_search","state":{"status":"errored","error":"web search unavailable","input":{"query":"Georg Stadler NYU"}}}}`,
		`{"type":"tool_use","part":{"type":"tool","tool":"sloppy_brain_search","state":{"status":"completed","input":{"query":"foo"},"output":"ok"}}}`,
	}, "\n")
	failures := extractToolFailures([]byte(raw))
	if len(failures) != 2 {
		t.Fatalf("got %d failures, want 2 (one completed call must not be reported): %+v", len(failures), failures)
	}
	if failures[0].Tool != "sloppy_brain_search" || failures[0].LastError != "mcp: connection reset by peer" {
		t.Fatalf("first failure mismatched: %+v", failures[0])
	}
	if failures[0].Attempt != 1 {
		t.Fatalf("first attempt for sloppy_brain_search should be 1, got %d", failures[0].Attempt)
	}
	if failures[1].Tool != "helpy_web_search" || failures[1].LastError != "web search unavailable" {
		t.Fatalf("second failure mismatched: %+v", failures[1])
	}
}

// TestExtractToolFailures_AttemptCountsPerTool verifies that repeated
// failures of the same tool within one opencode run get a monotonic
// attempt counter, matching the (item=…, stage=…, attempt=N, last_error=…)
// tagging requirement in #130.
func TestExtractToolFailures_AttemptCountsPerTool(t *testing.T) {
	raw := strings.Join([]string{
		`{"type":"tool_use","part":{"type":"tool","tool":"helpy_web_search","state":{"status":"errored","error":"connection reset"}}}`,
		`{"type":"tool_use","part":{"type":"tool","tool":"helpy_web_search","state":{"status":"errored","error":"connection reset"}}}`,
		`{"type":"tool_use","part":{"type":"tool","tool":"helpy_web_search","state":{"status":"errored","error":"connection reset"}}}`,
		`{"type":"tool_use","part":{"type":"tool","tool":"sloppy_brain_search","state":{"status":"errored","error":"broken pipe"}}}`,
	}, "\n")
	failures := extractToolFailures([]byte(raw))
	if len(failures) != 4 {
		t.Fatalf("got %d failures, want 4: %+v", len(failures), failures)
	}
	if failures[0].Attempt != 1 || failures[1].Attempt != 2 || failures[2].Attempt != 3 {
		t.Fatalf("helpy_web_search attempts should be 1,2,3; got %d,%d,%d",
			failures[0].Attempt, failures[1].Attempt, failures[2].Attempt)
	}
	if failures[3].Tool != "sloppy_brain_search" || failures[3].Attempt != 1 {
		t.Fatalf("sloppy_brain_search attempt should restart at 1: %+v", failures[3])
	}
}

// TestExtractToolFailures_FallsBackToOutputWhenErrorMissing covers the
// case where opencode wrote only state.output (an error message string)
// rather than a dedicated state.error field. The instrumentation must
// still surface a non-empty last_error.
func TestExtractToolFailures_FallsBackToOutputWhenErrorMissing(t *testing.T) {
	raw := `{"type":"tool_use","part":{"type":"tool","tool":"helpy_web_search","state":{"status":"errored","output":"429 Too Many Requests"}}}`
	failures := extractToolFailures([]byte(raw))
	if len(failures) != 1 {
		t.Fatalf("got %d failures, want 1", len(failures))
	}
	if failures[0].LastError != "429 Too Many Requests" {
		t.Fatalf("last_error should fall back to state.output: %+v", failures[0])
	}
}

// TestExtractToolFailures_AcceptsErrorStatusVariant covers opencode's
// alternate "error" spelling alongside "errored". Both must be treated
// as failures.
func TestExtractToolFailures_AcceptsErrorStatusVariant(t *testing.T) {
	raw := `{"type":"tool_use","part":{"type":"tool","tool":"helpy_web_search","state":{"status":"error","error":"timeout"}}}`
	failures := extractToolFailures([]byte(raw))
	if len(failures) != 1 || failures[0].LastError != "timeout" {
		t.Fatalf("error-status variant must surface: %+v", failures)
	}
}

// TestExtractToolFailures_NoFailuresOnHappyPath ensures we do not emit
// false-positive failure log lines when every tool call succeeded.
func TestExtractToolFailures_NoFailuresOnHappyPath(t *testing.T) {
	raw := strings.Join([]string{
		`{"type":"text","part":{"type":"text","text":"# Scout report\n"}}`,
		`{"type":"tool_use","part":{"type":"tool","tool":"helpy_web_search","state":{"status":"completed","input":{"query":"x"},"output":"results..."}}}`,
	}, "\n")
	failures := extractToolFailures([]byte(raw))
	if len(failures) != 0 {
		t.Fatalf("happy path must yield zero failures, got %+v", failures)
	}
}

// TestFormatToolFailureLogLine pins the structured wire format used to
// emit each failure to stderr. Downstream log scrapers (sleep ingestion
// of brain-night transcripts, future #130 follow-ups) parse this line
// shape, so changing it is a breaking change for those consumers.
func TestFormatToolFailureLogLine(t *testing.T) {
	line := formatToolFailureLogLine("scout", ToolFailure{
		Tool:      "helpy_web_search",
		Attempt:   3,
		LastError: "connection reset by peer",
	})
	want := "mcp tool_failure: stage=scout tool=helpy_web_search attempt=3 last_error=\"connection reset by peer\""
	if line != want {
		t.Fatalf("log line mismatch:\nhave: %s\nwant: %s", line, want)
	}
}

// TestFormatToolFailureLogLine_SanitizesNewlines guards against multi-
// line error strings breaking log parsing. Any embedded newline must be
// replaced with a literal "\n" inside the quoted last_error field.
func TestFormatToolFailureLogLine_SanitizesNewlines(t *testing.T) {
	line := formatToolFailureLogLine("sleep", ToolFailure{
		Tool:      "sloppy_brain_search",
		Attempt:   1,
		LastError: "stderr line one\nstderr line two",
	})
	if strings.Count(line, "\n") != 0 {
		t.Fatalf("log line must be a single line, got: %q", line)
	}
	if !strings.Contains(line, `last_error="stderr line one\nstderr line two"`) {
		t.Fatalf("newline must be escaped to \\n inside last_error: %q", line)
	}
}

// jsonString quotes s as a JSON string literal so test fixtures can
// embed multi-line markdown without manual escaping.
func jsonString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				continue
			}
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
