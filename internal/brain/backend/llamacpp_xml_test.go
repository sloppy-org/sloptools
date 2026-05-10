package backend

import (
	"testing"
)

func TestParseQwenXMLCallsParamFormat(t *testing.T) {
	content := `<tool_call>
<function=web_search>
<parameter=query>ASDEX Upgrade AUG Python diagnostics</parameter>
<parameter=limit>3</parameter>
</function>
</tool_call>
<tool_call>
<function=web_fetch>
<parameter=url>https://example.com</parameter>
</function>
</tool_call>`

	calls := parseQwenXMLCalls(content)
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}
	if calls[0].Name != "web_search" {
		t.Errorf("call[0].Name = %q, want web_search", calls[0].Name)
	}
	if calls[0].Args["query"] != "ASDEX Upgrade AUG Python diagnostics" {
		t.Errorf("call[0].Args[query] = %v", calls[0].Args["query"])
	}
	if calls[0].Args["limit"] != "3" {
		t.Errorf("call[0].Args[limit] = %v", calls[0].Args["limit"])
	}
	if calls[1].Name != "web_fetch" {
		t.Errorf("call[1].Name = %q, want web_fetch", calls[1].Name)
	}
}

func TestParseQwenXMLCallsJSONFormat(t *testing.T) {
	content := "<tool_call>\n{\"name\":\"web_search\",\"arguments\":{\"query\":\"test query\"}}\n</tool_call>"

	calls := parseQwenXMLCalls(content)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Name != "web_search" {
		t.Errorf("Name = %q, want web_search", calls[0].Name)
	}
	if calls[0].Args["query"] != "test query" {
		t.Errorf("Args[query] = %v", calls[0].Args["query"])
	}
}

func TestParseQwenXMLCallsNone(t *testing.T) {
	calls := parseQwenXMLCalls("This is a plain text response with no tool calls.")
	if calls != nil {
		t.Fatalf("expected nil, got %v", calls)
	}
}

func TestStripXMLToolCalls(t *testing.T) {
	content := "Some preamble.\n<tool_call>\n<function=web_search>\n<parameter=query>test</parameter>\n</function>\n</tool_call>\nSome suffix."
	got := stripXMLToolCalls(content)
	if got != "Some preamble.\n\nSome suffix." {
		t.Errorf("unexpected result: %q", got)
	}
}

func TestStripXMLToolCallsOnlyTags(t *testing.T) {
	content := "<tool_call>\n<function=web_search>\n<parameter=query>test</parameter>\n</function>\n</tool_call>"
	got := stripXMLToolCalls(content)
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}
