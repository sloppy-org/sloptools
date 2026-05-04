package zulip

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Config carries the credentials and base URL the REST client needs.
// BaseURL is the realm root (no trailing slash, no `/api/v1`); the
// client appends the API path.
type Config struct {
	BaseURL    string
	Email      string
	APIKey     string
	HTTPClient *http.Client
	UserAgent  string
}

// Client implements MessagesProvider against a Zulip realm. It is safe
// for concurrent use as long as the configured http.Client is.
type Client struct {
	baseURL   string
	email     string
	apiKey    string
	http      *http.Client
	userAgent string
}

// NewClient validates the configuration and returns a ready client.
func NewClient(cfg Config) (*Client, error) {
	base := strings.TrimRight(cleanString(cfg.BaseURL), "/")
	email := cleanString(cfg.Email)
	apiKey := cleanString(cfg.APIKey)
	if base == "" || email == "" || apiKey == "" {
		return nil, ErrCredentialsMissing
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	ua := cleanString(cfg.UserAgent)
	if ua == "" {
		ua = "sloptools-zulip/0.1"
	}
	return &Client{baseURL: base, email: email, apiKey: apiKey, http: httpClient, userAgent: ua}, nil
}

type narrowTerm struct {
	Operator string `json:"operator"`
	Operand  string `json:"operand"`
}

type messagesResponse struct {
	Result   string    `json:"result"`
	Msg      string    `json:"msg"`
	Messages []rawMsg  `json:"messages"`
	Code     string    `json:"code,omitempty"`
	Anchor   anchorRef `json:"-"`
}

type rawMsg struct {
	ID               int64       `json:"id"`
	SenderName       string      `json:"sender_full_name"`
	SenderEmail      string      `json:"sender_email"`
	DisplayRecipient interface{} `json:"display_recipient"`
	Subject          string      `json:"subject"`
	Timestamp        json.Number `json:"timestamp"`
	Content          string      `json:"content"`
}

type anchorRef struct{}

// Messages fetches messages for the configured realm narrowed to a
// stream and topic between [After, Before). The Zulip API filters by
// narrow operators and anchors; the implementation pages the
// `messages` endpoint with a "newest" anchor and trims to the time
// window client-side because Zulip does not expose direct timestamp
// filtering.
func (c *Client) Messages(ctx context.Context, params MessagesParams) ([]Message, error) {
	stream := cleanString(params.Stream)
	if stream == "" {
		return nil, ErrStreamRequired
	}
	topic := cleanString(params.Topic)
	if topic == "" {
		return nil, ErrTopicRequired
	}
	limit := params.Limit
	if limit <= 0 {
		limit = defaultMessageLimit
	}
	narrow := []narrowTerm{
		{Operator: "stream", Operand: stream},
		{Operator: "topic", Operand: topic},
	}
	narrowJSON, err := json.Marshal(narrow)
	if err != nil {
		return nil, fmt.Errorf("encode zulip narrow: %w", err)
	}
	values := url.Values{}
	values.Set("anchor", "newest")
	values.Set("num_before", strconv.Itoa(limit))
	values.Set("num_after", "0")
	values.Set("apply_markdown", "false")
	values.Set("narrow", string(narrowJSON))
	requestURL := c.baseURL + "/api/v1/messages?" + values.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.email, c.apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("zulip messages request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("zulip messages: HTTP %d", resp.StatusCode)
	}
	var decoded messagesResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode zulip messages: %w", err)
	}
	if decoded.Result != "" && decoded.Result != "success" {
		return nil, fmt.Errorf("zulip messages: %s (%s)", decoded.Msg, decoded.Code)
	}
	return filterAndConvert(decoded.Messages, stream, topic, params.After, params.Before), nil
}

func filterAndConvert(raw []rawMsg, stream, topic string, after, before time.Time) []Message {
	out := make([]Message, 0, len(raw))
	for _, msg := range raw {
		ts, err := timestampToTime(msg.Timestamp)
		if err != nil {
			continue
		}
		if !after.IsZero() && ts.Before(after) {
			continue
		}
		if !before.IsZero() && !ts.Before(before) {
			continue
		}
		out = append(out, Message{
			ID:          msg.ID,
			SenderName:  cleanString(msg.SenderName),
			SenderEmail: cleanString(msg.SenderEmail),
			Stream:      streamName(msg.DisplayRecipient, stream),
			Topic:       cleanString(msg.Subject),
			Timestamp:   ts.UTC(),
			Content:     msg.Content,
		})
	}
	if topic != "" {
		filtered := out[:0]
		for _, msg := range out {
			if strings.EqualFold(msg.Topic, topic) {
				filtered = append(filtered, msg)
			}
		}
		out = filtered
	}
	return out
}

func timestampToTime(value json.Number) (time.Time, error) {
	if value == "" {
		return time.Time{}, fmt.Errorf("empty timestamp")
	}
	if i, err := value.Int64(); err == nil {
		return time.Unix(i, 0).UTC(), nil
	}
	f, err := value.Float64()
	if err != nil {
		return time.Time{}, err
	}
	sec := int64(f)
	nsec := int64((f - float64(sec)) * 1e9)
	return time.Unix(sec, nsec).UTC(), nil
}

func streamName(raw interface{}, fallback string) string {
	if s, ok := raw.(string); ok {
		clean := cleanString(s)
		if clean != "" {
			return clean
		}
	}
	return cleanString(fallback)
}
