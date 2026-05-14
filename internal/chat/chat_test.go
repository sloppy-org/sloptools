package chat

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sloppy-org/sloptools/internal/zulip"
)

type fakeZulip struct {
	streams      []zulip.Stream
	messages     []zulip.Message
	gotSearch    zulip.SearchParams
	gotMessages  zulip.MessagesParams
	searchCalled bool
}

func (f *fakeZulip) Messages(_ context.Context, params zulip.MessagesParams) ([]zulip.Message, error) {
	f.gotMessages = params
	return f.messages, nil
}

func (f *fakeZulip) Search(_ context.Context, params zulip.SearchParams) ([]zulip.Message, error) {
	f.searchCalled = true
	f.gotSearch = params
	return f.messages, nil
}

func (f *fakeZulip) Streams(context.Context) ([]zulip.Stream, error) {
	return f.streams, nil
}

func TestHandlerMessageSearchUsesChatConfig(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "sources.toml")
	body := `[chat.work]
provider = "zulip"

[chat.work.zulip]
base_url = "https://zulip.example.org"
email = "bot@example.org"
api_key = "secret"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	fake := &fakeZulip{messages: []zulip.Message{{
		ID: 3, SenderName: "Ada", SenderEmail: "ada@example.org",
		Stream: "plasma-orga", Topic: "ops",
		Timestamp: time.Date(2026, 5, 14, 8, 0, 0, 0, time.UTC),
		Content:   "status update",
	}}}
	handler := Handler{
		ConfigPath: path,
		Explicit:   true,
		NewZulip: func(cfg ZulipConfig) (ZulipProvider, error) {
			if cfg.BaseURL != "https://zulip.example.org" || cfg.Email != "bot@example.org" || cfg.APIKey != "secret" {
				t.Fatalf("zulip cfg = %#v", cfg)
			}
			return fake, nil
		},
	}
	out, err := handler.Call(context.Background(), map[string]interface{}{
		"action": "message_search",
		"sphere": "work",
		"query":  "status",
		"stream": "plasma-orga",
		"topic":  "ops",
		"limit":  float64(25),
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !fake.searchCalled || fake.gotSearch.Query != "status" || fake.gotSearch.Limit != 25 {
		t.Fatalf("search params = %#v", fake.gotSearch)
	}
	if out["count"] != 1 || out["provider"] != ProviderZulip {
		t.Fatalf("out = %#v", out)
	}
}

func TestHandlerFallsBackToMeetingZulipConfig(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "sources.toml")
	body := `[meetings.work.zulip]
base_url = "https://meetings-zulip.example.org"
email = "bot@example.org"
api_key = "secret"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	fake := &fakeZulip{streams: []zulip.Stream{{ID: 1, Name: "plasma-orga"}}}
	handler := Handler{
		ConfigPath: path,
		Explicit:   true,
		NewZulip: func(cfg ZulipConfig) (ZulipProvider, error) {
			if cfg.BaseURL != "https://meetings-zulip.example.org" {
				t.Fatalf("fallback cfg = %#v", cfg)
			}
			return fake, nil
		},
	}
	out, err := handler.Call(context.Background(), map[string]interface{}{"action": "stream_list", "sphere": "work"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if out["count"] != 1 {
		t.Fatalf("out = %#v", out)
	}
}
