package zulip

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewClientRejectsMissingCredentials(t *testing.T) {
	if _, err := NewClient(Config{BaseURL: "", Email: "x", APIKey: "y"}); err != ErrCredentialsMissing {
		t.Fatalf("missing base url: err = %v, want %v", err, ErrCredentialsMissing)
	}
	if _, err := NewClient(Config{BaseURL: "https://example.com", Email: "", APIKey: "y"}); err != ErrCredentialsMissing {
		t.Fatalf("missing email: err = %v, want %v", err, ErrCredentialsMissing)
	}
	if _, err := NewClient(Config{BaseURL: "https://example.com", Email: "x", APIKey: ""}); err != ErrCredentialsMissing {
		t.Fatalf("missing api key: err = %v, want %v", err, ErrCredentialsMissing)
	}
}

func TestMessagesRequestsCorrectURLAndDecodesPayload(t *testing.T) {
	cutoff := time.Date(2026, 5, 4, 9, 0, 0, 0, time.UTC)
	after := cutoff.Add(-24 * time.Hour)
	insideTopic := time.Date(2026, 5, 3, 18, 0, 0, 0, time.UTC)
	beforeWindow := time.Date(2026, 5, 2, 18, 0, 0, 0, time.UTC)
	afterWindow := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	differentTopic := time.Date(2026, 5, 4, 7, 0, 0, 0, time.UTC)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/messages" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		user, key, ok := r.BasicAuth()
		if !ok || user != "bot@example.org" || key != "secret" {
			t.Fatalf("auth = %v %q %q", ok, user, key)
		}
		query := r.URL.Query()
		if query.Get("anchor") != "newest" {
			t.Fatalf("anchor = %q", query.Get("anchor"))
		}
		if query.Get("num_before") != "100" {
			t.Fatalf("num_before = %q", query.Get("num_before"))
		}
		var narrow []narrowTerm
		if err := json.Unmarshal([]byte(query.Get("narrow")), &narrow); err != nil {
			t.Fatalf("decode narrow: %v", err)
		}
		got := map[string]string{}
		for _, term := range narrow {
			got[term.Operator] = term.Operand
		}
		if got["stream"] != "plasma-orga" || got["topic"] != "2026-05-04 sync" {
			t.Fatalf("narrow = %#v", got)
		}
		body := map[string]interface{}{
			"result": "success",
			"messages": []map[string]interface{}{
				{
					"id":                1,
					"sender_full_name":  "Ada Example",
					"sender_email":      "ada@example.org",
					"display_recipient": "plasma-orga",
					"subject":           "2026-05-04 sync",
					"timestamp":         insideTopic.Unix(),
					"content":           "I want to sync with @**Bo Coder** on grant numbers.",
				},
				{
					"id":                2,
					"sender_full_name":  "Off Topic",
					"sender_email":      "off@example.org",
					"display_recipient": "plasma-orga",
					"subject":           "2026-05-04 decisions",
					"timestamp":         differentTopic.Unix(),
					"content":           "wrong topic",
				},
				{
					"id":                3,
					"sender_full_name":  "Too Old",
					"sender_email":      "old@example.org",
					"display_recipient": "plasma-orga",
					"subject":           "2026-05-04 sync",
					"timestamp":         beforeWindow.Unix(),
					"content":           "too early",
				},
				{
					"id":                4,
					"sender_full_name":  "Too New",
					"sender_email":      "new@example.org",
					"display_recipient": "plasma-orga",
					"subject":           "2026-05-04 sync",
					"timestamp":         afterWindow.Unix(),
					"content":           "after cutoff",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL, Email: "bot@example.org", APIKey: "secret"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	got, err := client.Messages(context.Background(), MessagesParams{
		Stream: "plasma-orga",
		Topic:  "2026-05-04 sync",
		After:  after,
		Before: cutoff,
	})
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if len(got) != 1 || got[0].ID != 1 {
		t.Fatalf("filtered messages = %#v, want only id=1", got)
	}
	if got[0].SenderName != "Ada Example" || got[0].Topic != "2026-05-04 sync" {
		t.Fatalf("decoded message = %#v", got[0])
	}
	if !strings.Contains(got[0].Content, "Bo Coder") {
		t.Fatalf("content lost mention text: %#v", got[0].Content)
	}
}

func TestMessagesRequiresStreamAndTopic(t *testing.T) {
	client, err := NewClient(Config{BaseURL: "https://example.com", Email: "x@x", APIKey: "k"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := client.Messages(context.Background(), MessagesParams{Topic: "t"}); err != ErrStreamRequired {
		t.Fatalf("missing stream: %v", err)
	}
	if _, err := client.Messages(context.Background(), MessagesParams{Stream: "s"}); err != ErrTopicRequired {
		t.Fatalf("missing topic: %v", err)
	}
}

func TestMessagesPropagatesAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"result": "error",
			"msg":    "Stream does not exist",
			"code":   "STREAM_NOT_FOUND",
		})
	}))
	defer server.Close()
	client, _ := NewClient(Config{BaseURL: server.URL, Email: "x@x", APIKey: "k"})
	_, err := client.Messages(context.Background(), MessagesParams{Stream: "s", Topic: "t"})
	if err == nil || !strings.Contains(err.Error(), "STREAM_NOT_FOUND") {
		t.Fatalf("err = %v, want stream-not-found error", err)
	}
}
