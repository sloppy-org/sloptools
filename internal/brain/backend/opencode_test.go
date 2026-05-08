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
