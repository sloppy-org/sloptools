package backend

import "testing"

func TestScrapeCodexTokens_PromptCompletionPair(t *testing.T) {
	stderr := "[2026-01-15T12:34:56] tokens used\nprompt_tokens=1234 completion_tokens=567 total_tokens=1801\n"
	in, out := scrapeCodexTokens(stderr)
	if in != 1234 || out != 567 {
		t.Fatalf("got in=%d out=%d, want 1234/567", in, out)
	}
}

func TestScrapeCodexTokens_JSONStyle(t *testing.T) {
	stderr := `{"input_tokens": 4096, "output_tokens": 256}`
	in, out := scrapeCodexTokens(stderr)
	if in != 4096 || out != 256 {
		t.Fatalf("got in=%d out=%d, want 4096/256", in, out)
	}
}

func TestScrapeCodexTokens_TotalOnly(t *testing.T) {
	stderr := "[2026-01-15T12:34:56] tokens used: 9876\n"
	in, out := scrapeCodexTokens(stderr)
	if in != 0 || out != 9876 {
		t.Fatalf("got in=%d out=%d, want 0/9876", in, out)
	}
}

func TestScrapeCodexTokens_LastWins(t *testing.T) {
	stderr := "prompt_tokens=10 completion_tokens=20\nprompt_tokens=100 completion_tokens=200\n"
	in, out := scrapeCodexTokens(stderr)
	if in != 100 || out != 200 {
		t.Fatalf("got in=%d out=%d, want 100/200", in, out)
	}
}

func TestScrapeCodexTokens_NoMatchReturnsZero(t *testing.T) {
	stderr := "hello world\nno usage line here\n"
	in, out := scrapeCodexTokens(stderr)
	if in != 0 || out != 0 {
		t.Fatalf("got in=%d out=%d, want 0/0", in, out)
	}
}

func TestParseIntOr_Garbage(t *testing.T) {
	if parseIntOr("nope") != 0 {
		t.Fatalf("garbage should be zero")
	}
	if parseIntOr("  42 ") != 42 {
		t.Fatalf("trim failure")
	}
}
