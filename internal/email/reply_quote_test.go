package email

import (
	"strings"
	"testing"
	"time"
)

func TestFormatQuotedReplyBottomPostGCCStyle(t *testing.T) {
	source := QuoteSource{
		From: "Jane Dev <jane@gcc.gnu.org>",
		Date: time.Date(2026, 4, 21, 9, 0, 0, 0, time.UTC),
		Body: "PR93456: the new optimization breaks ppc.\nPlease revert.",
	}
	out := FormatQuotedReply(ReplyQuoteBottomPost, "Confirmed, reverting in r123456.", source)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) < 4 {
		t.Fatalf("expected at least 4 lines, got %d: %q", len(lines), out)
	}
	if !strings.HasSuffix(lines[0], "Jane Dev wrote:") {
		t.Fatalf("first line should be attribution, got %q", lines[0])
	}
	quoteStart := -1
	for i, line := range lines {
		if strings.HasPrefix(line, "> ") {
			quoteStart = i
			break
		}
	}
	if quoteStart == -1 {
		t.Fatalf("missing quoted lines in: %q", out)
	}
	replyIdx := -1
	for i := quoteStart; i < len(lines); i++ {
		if !strings.HasPrefix(lines[i], ">") && lines[i] != "" {
			replyIdx = i
			break
		}
	}
	if replyIdx == -1 {
		t.Fatalf("reply text not found below quote: %q", out)
	}
	if !strings.Contains(lines[replyIdx], "Confirmed, reverting") {
		t.Fatalf("reply text placement wrong: %q", out)
	}
}

func TestFormatQuotedReplyTopPostBusinessStyle(t *testing.T) {
	source := QuoteSource{
		From: "Client <client@example.com>",
		Date: time.Date(2026, 4, 21, 14, 0, 0, 0, time.UTC),
		Body: "Please send the quarterly report.",
	}
	out := FormatQuotedReply(ReplyQuoteTopPost, "Attached. Best regards, Albert.", source)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) < 4 {
		t.Fatalf("expected at least 4 lines, got %d: %q", len(lines), out)
	}
	if !strings.Contains(lines[0], "Attached. Best regards") {
		t.Fatalf("top-post must begin with the reply, got %q", lines[0])
	}
	var attributionIdx int = -1
	for i, line := range lines {
		if strings.HasSuffix(line, "Client wrote:") {
			attributionIdx = i
			break
		}
	}
	if attributionIdx == -1 {
		t.Fatalf("attribution line missing: %q", out)
	}
	quoteIdx := -1
	for i := attributionIdx; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "> ") {
			quoteIdx = i
			break
		}
	}
	if quoteIdx == -1 {
		t.Fatalf("expected quoted original below attribution, got %q", out)
	}
}

func TestFormatQuotedReplyDeepenQuotes(t *testing.T) {
	source := QuoteSource{
		Body: "> existing quote\nand my line",
	}
	out := FormatQuotedReply(ReplyQuoteBottomPost, "Reply.", source)
	if !strings.Contains(out, ">> existing quote") {
		t.Fatalf("nested quotes should deepen, got: %q", out)
	}
	if !strings.Contains(out, "> and my line") {
		t.Fatalf("non-quoted original line should get > prefix, got: %q", out)
	}
}

func TestParseReplyQuoteStyleAccepts(t *testing.T) {
	tests := map[string]ReplyQuoteStyle{
		"":            ReplyQuoteBottomPost,
		"bottom_post": ReplyQuoteBottomPost,
		"GCC":         ReplyQuoteBottomPost,
		"interleaved": ReplyQuoteBottomPost,
		"top_post":    ReplyQuoteTopPost,
		"business":    ReplyQuoteTopPost,
		"modern":      ReplyQuoteTopPost,
	}
	for input, want := range tests {
		got, err := ParseReplyQuoteStyle(input)
		if err != nil {
			t.Fatalf("ParseReplyQuoteStyle(%q) error: %v", input, err)
		}
		if got != want {
			t.Fatalf("ParseReplyQuoteStyle(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestParseReplyQuoteStyleRejectsUnknown(t *testing.T) {
	if _, err := ParseReplyQuoteStyle("sideways"); err == nil {
		t.Fatal("expected error for unknown quote style")
	}
}
