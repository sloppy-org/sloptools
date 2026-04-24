package email

import (
	"context"
	"fmt"
	"net/mail"
	"strings"
	"sync"
	"time"
)

type Recipient struct {
	EmailAddress Address `json:"emailAddress"`
}

type Address struct {
	Name    string `json:"name"`
	Address string `json:"address"`
}

var QuotaCosts = map[string]float64{"messages.list": 5, "messages.get": 5, "messages.modify": 5, "messages.batchModify": 50, "messages.trash": 5, "messages.delete": 10, "messages.send": 100, "labels.list": 1, "labels.get": 1}

type RateLimiter struct {
	maxQuotaPerMinute float64
	tokens            float64
	lastRefill        time.Time
	mu                sync.Mutex
} // RateLimiter implements a token bucket rate limiter for Gmail API quota.

func NewRateLimiter(maxQuotaPerMinute float64) *RateLimiter {
	if maxQuotaPerMinute <= 0 {
		maxQuotaPerMinute = 15000
	}
	return &RateLimiter{maxQuotaPerMinute: maxQuotaPerMinute, tokens: maxQuotaPerMinute, lastRefill: time.Now()}
}

func (r *RateLimiter) Acquire(operation string) {
	r.AcquireN(operation, 1)
}

func (r *RateLimiter) AcquireN(operation string, count int) {
	cost := QuotaCosts[operation]
	if cost == 0 {
		cost = 5
	}
	totalCost := cost * float64(count)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.refill()
	for r.tokens < totalCost {
		waitTime := time.Duration((totalCost-r.tokens)/(r.maxQuotaPerMinute/60)*1000) * time.Millisecond
		r.mu.Unlock()
		time.Sleep(waitTime)
		r.mu.Lock()
		r.refill()
	}
	r.tokens -= totalCost
}

func (r *RateLimiter) refill() {
	now := time.Now()
	elapsed := now.Sub(r.lastRefill).Seconds()
	r.tokens += elapsed * r.maxQuotaPerMinute / 60
	if r.tokens > r.maxQuotaPerMinute {
		r.tokens = r.maxQuotaPerMinute
	}
	r.lastRefill = now
}

func (r *RateLimiter) Tokens() float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.refill()
	return r.tokens
}

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

type ServerFilterCriteria struct {
	From          string `json:"from,omitempty"`
	To            string `json:"to,omitempty"`
	Subject       string `json:"subject,omitempty"`
	Query         string `json:"query,omitempty"`
	NegatedQuery  string `json:"negated_query,omitempty"`
	HasAttachment *bool  `json:"has_attachment,omitempty"`
}

type ServerFilterAction struct {
	Archive      bool     `json:"archive,omitempty"`
	Trash        bool     `json:"trash,omitempty"`
	MarkRead     bool     `json:"mark_read,omitempty"`
	MoveTo       string   `json:"move_to,omitempty"`
	ForwardTo    []string `json:"forward_to,omitempty"`
	AddLabels    []string `json:"add_labels,omitempty"`
	RemoveLabels []string `json:"remove_labels,omitempty"`
}

type ServerFilter struct {
	ID       string               `json:"id,omitempty"`
	Name     string               `json:"name"`
	Enabled  bool                 `json:"enabled"`
	Criteria ServerFilterCriteria `json:"criteria,omitempty"`
	Action   ServerFilterAction   `json:"action,omitempty"`
}

type ServerFilterCapabilities struct {
	Provider          string `json:"provider,omitempty"`
	SupportsList      bool   `json:"supports_list"`
	SupportsUpsert    bool   `json:"supports_upsert"`
	SupportsDelete    bool   `json:"supports_delete"`
	SupportsArchive   bool   `json:"supports_archive"`
	SupportsTrash     bool   `json:"supports_trash"`
	SupportsMoveTo    bool   `json:"supports_move_to"`
	SupportsMarkRead  bool   `json:"supports_mark_read"`
	SupportsForward   bool   `json:"supports_forward"`
	SupportsAddLabels bool   `json:"supports_add_labels"`
	SupportsQuery     bool   `json:"supports_query"`
}

type ServerFilterProvider interface {
	ServerFilterCapabilities() ServerFilterCapabilities
	ListServerFilters(context.Context) ([]ServerFilter, error)
	UpsertServerFilter(context.Context, ServerFilter) (ServerFilter, error)
	DeleteServerFilter(context.Context, string) error
}

type NamedFolderProvider interface {
	MoveToFolder(context.Context, []string, string) (int, error)
}

type NamedLabelProvider interface {
	ApplyNamedLabel(context.Context, []string, string, bool) (int, error)
}
