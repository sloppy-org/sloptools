package email

import (
	"fmt"
	"net/mail"
	"strings"
	"time"
)

type ReplyQuoteStyle string

const (
	ReplyQuoteBottomPost ReplyQuoteStyle = "bottom_post"
	ReplyQuoteTopPost    ReplyQuoteStyle = "top_post"
)

type QuoteSource struct {
	From string
	Date time.Time
	Body string
}

func ParseReplyQuoteStyle(raw string) (ReplyQuoteStyle, error) {
	clean := strings.TrimSpace(strings.ToLower(raw))
	switch clean {
	case "", "bottom_post", "bottom-post", "bottom", "gcc", "mailing_list", "interleaved":
		return ReplyQuoteBottomPost, nil
	case "top_post", "top-post", "top", "business", "modern":
		return ReplyQuoteTopPost, nil
	default:
		return "", fmt.Errorf("unsupported quote style %q", raw)
	}
}

func FormatQuotedReply(style ReplyQuoteStyle, replyText string, source QuoteSource) string {
	reply := strings.TrimRight(replyText, "\n")
	attribution := buildAttributionLine(source)
	quoted := quoteBodyLines(source.Body)
	if quoted == "" && attribution == "" {
		if reply == "" {
			return ""
		}
		return reply + "\n"
	}
	switch style {
	case ReplyQuoteTopPost:
		var b strings.Builder
		if reply != "" {
			b.WriteString(reply)
			b.WriteString("\n\n")
		}
		if attribution != "" {
			b.WriteString(attribution)
			b.WriteString("\n")
		}
		b.WriteString(quoted)
		return b.String()
	default:
		var b strings.Builder
		if attribution != "" {
			b.WriteString(attribution)
			b.WriteString("\n")
		}
		b.WriteString(quoted)
		if reply != "" {
			b.WriteString("\n")
			b.WriteString(reply)
			b.WriteString("\n")
		}
		return b.String()
	}
}

func buildAttributionLine(source QuoteSource) string {
	who := strings.TrimSpace(source.From)
	if parsed, err := mail.ParseAddress(who); err == nil {
		if name := strings.TrimSpace(parsed.Name); name != "" {
			who = name
		} else {
			who = parsed.Address
		}
	}
	if who == "" && source.Date.IsZero() {
		return ""
	}
	if source.Date.IsZero() {
		if who == "" {
			return ""
		}
		return who + " wrote:"
	}
	when := source.Date.Local().Format("Mon, 02 Jan 2006 at 15:04")
	if who == "" {
		return "On " + when + " wrote:"
	}
	return "On " + when + ", " + who + " wrote:"
}

func quoteBodyLines(body string) string {
	clean := strings.ReplaceAll(body, "\r\n", "\n")
	clean = strings.TrimRight(clean, "\n")
	if clean == "" {
		return ""
	}
	lines := strings.Split(clean, "\n")
	var b strings.Builder
	b.Grow(len(clean) + 2*len(lines))
	for _, line := range lines {
		if line == "" {
			b.WriteString(">\n")
			continue
		}
		if strings.HasPrefix(line, ">") {
			b.WriteString(">")
		} else {
			b.WriteString("> ")
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}
