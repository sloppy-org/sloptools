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

	server := httptest.NewServer(zulipMessagesFixtureHandler(t, cutoff))
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

func zulipMessagesFixtureHandler(t *testing.T, cutoff time.Time) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		assertMessagesRequest(t, r)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(messagesFixture(cutoff))
	}
}

func assertMessagesRequest(t *testing.T, r *http.Request) {
	t.Helper()
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
	assertNarrow(t, query.Get("narrow"))
}

func assertNarrow(t *testing.T, encoded string) {
	t.Helper()
	var narrow []narrowTerm
	if err := json.Unmarshal([]byte(encoded), &narrow); err != nil {
		t.Fatalf("decode narrow: %v", err)
	}
	got := map[string]string{}
	for _, term := range narrow {
		got[term.Operator] = term.Operand
	}
	if got["stream"] != "plasma-orga" || got["topic"] != "2026-05-04 sync" {
		t.Fatalf("narrow = %#v", got)
	}
}

func messagesFixture(cutoff time.Time) map[string]interface{} {
	return map[string]interface{}{
		"result": "success",
		"messages": []map[string]interface{}{
			messageFixture(1, "Ada Example", "ada@example.org", "2026-05-04 sync", cutoff.Add(-15*time.Hour), "I want to sync with @**Bo Coder** on grant numbers."),
			messageFixture(2, "Off Topic", "off@example.org", "2026-05-04 decisions", cutoff.Add(-2*time.Hour), "wrong topic"),
			messageFixture(3, "Too Old", "old@example.org", "2026-05-04 sync", cutoff.Add(-39*time.Hour), "too early"),
			messageFixture(4, "Too New", "new@example.org", "2026-05-04 sync", cutoff.Add(3*time.Hour), "after cutoff"),
		},
	}
}

func messageFixture(id int64, sender, email, topic string, ts time.Time, content string) map[string]interface{} {
	return map[string]interface{}{
		"id":                id,
		"sender_full_name":  sender,
		"sender_email":      email,
		"display_recipient": "plasma-orga",
		"subject":           topic,
		"timestamp":         ts.Unix(),
		"content":           content,
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

func TestSearchAddsSearchNarrowAndFiltersWindow(t *testing.T) {
	cutoff := time.Date(2026, 5, 4, 9, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertSearchRequest(t, r)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(messagesFixture(cutoff))
	}))
	defer server.Close()
	client, err := NewClient(Config{BaseURL: server.URL, Email: "bot@example.org", APIKey: "secret"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	got, err := client.Search(context.Background(), SearchParams{
		Query:  "grant",
		Stream: "plasma-orga",
		After:  cutoff.Add(-24 * time.Hour),
		Before: cutoff,
		Limit:  7,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("messages = %#v, want two in-window messages", got)
	}
}

func assertSearchRequest(t *testing.T, r *http.Request) {
	t.Helper()
	query := r.URL.Query()
	if query.Get("num_before") != "7" {
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
	if got["stream"] != "plasma-orga" || got["search"] != "grant" {
		t.Fatalf("narrow = %#v", got)
	}
}

func TestStreamsListsSubscriptions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/users/me/subscriptions" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"result": "success",
			"subscriptions": []map[string]interface{}{
				{"stream_id": 1, "name": "plasma-orga", "description": "team"},
				{"stream_id": 2, "name": "", "description": "ignored"},
			},
		})
	}))
	defer server.Close()
	client, err := NewClient(Config{BaseURL: server.URL, Email: "bot@example.org", APIKey: "secret"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	got, err := client.Streams(context.Background())
	if err != nil {
		t.Fatalf("Streams: %v", err)
	}
	if len(got) != 1 || got[0].Name != "plasma-orga" || got[0].ID != 1 {
		t.Fatalf("streams = %#v", got)
	}
}
