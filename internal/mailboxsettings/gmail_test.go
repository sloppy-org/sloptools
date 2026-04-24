package mailboxsettings

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sloppy-org/sloptools/internal/providerdata"
	gmail "google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

func buildGmailStub(t *testing.T, handler http.HandlerFunc) *GmailProvider {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	provider := &GmailProvider{}
	provider.svcFn = func(ctx context.Context) (*gmail.Service, error) {
		return gmail.NewService(ctx, option.WithoutAuthentication(), option.WithEndpoint(srv.URL))
	}
	return provider
}

func TestGmailGetOOFParsesVacationSettings(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.Contains(r.URL.Path, "/gmail/v1/users/me/settings/vacation") {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		writeJSON(w, map[string]any{
			"enableAutoReply":       true,
			"responseBodyPlainText": "Out until Monday.",
			"restrictToContacts":    true,
			"startTime":             "1719360000000", // 2024-06-26 in UTC ms
			"endTime":               "1719964800000",
		})
	}
	provider := buildGmailStub(t, handler)

	settings, err := provider.GetOOF(context.Background())
	if err != nil {
		t.Fatalf("GetOOF: %v", err)
	}
	if !settings.Enabled {
		t.Fatal("Enabled = false, want true")
	}
	if settings.Scope != "contacts" {
		t.Fatalf("Scope = %q, want contacts", settings.Scope)
	}
	if settings.InternalReply != "Out until Monday." {
		t.Fatalf("InternalReply = %q", settings.InternalReply)
	}
	if settings.StartAt == nil || settings.EndAt == nil {
		t.Fatal("StartAt/EndAt should be set")
	}
}

func TestGmailSetOOFPostsVacationUpdate(t *testing.T) {
	called := 0
	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Fatalf("method = %s, want PUT", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/settings/vacation") {
			t.Fatalf("path = %q", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var in gmail.VacationSettings
		if err := json.Unmarshal(body, &in); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if !in.EnableAutoReply {
			t.Fatal("EnableAutoReply = false, want true")
		}
		if in.ResponseBodyPlainText != "Away!" {
			t.Fatalf("body = %q", in.ResponseBodyPlainText)
		}
		if !in.RestrictToContacts {
			t.Fatal("RestrictToContacts = false, want true for scope=contacts")
		}
		called++
		writeJSON(w, map[string]any{"enableAutoReply": true})
	}
	provider := buildGmailStub(t, handler)

	err := provider.SetOOF(context.Background(), providerdata.OOFSettings{
		Enabled:       true,
		InternalReply: "Away!",
		Scope:         "contacts",
	})
	if err != nil {
		t.Fatalf("SetOOF: %v", err)
	}
	if called != 1 {
		t.Fatalf("called = %d", called)
	}
}

func TestGmailListDelegatesMapsVerificationStatus(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.HasSuffix(r.URL.Path, "/settings/delegates") {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		writeJSON(w, map[string]any{
			"delegates": []map[string]any{
				{"delegateEmail": "alice@example.com", "verificationStatus": "accepted"},
				{"delegateEmail": "bob@example.com", "verificationStatus": "pending"},
				{"delegateEmail": "", "verificationStatus": "accepted"},
			},
		})
	}
	provider := buildGmailStub(t, handler)

	got, err := provider.ListDelegates(context.Background())
	if err != nil {
		t.Fatalf("ListDelegates: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2 (empty-email entry should be skipped)", len(got))
	}
	if got[0].Email != "alice@example.com" {
		t.Fatalf("got[0].Email = %q", got[0].Email)
	}
	if len(got[0].Permissions) != 1 || got[0].Permissions[0] != "verification:accepted" {
		t.Fatalf("got[0].Permissions = %v, want [verification:accepted]", got[0].Permissions)
	}
	if got[1].Permissions[0] != "verification:pending" {
		t.Fatalf("got[1].Permissions = %v, want [verification:pending]", got[1].Permissions)
	}
}

func TestGmailListDelegatesReturnsEmptyWhenResponseOmitsDelegates(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{})
	}
	provider := buildGmailStub(t, handler)

	got, err := provider.ListDelegates(context.Background())
	if err != nil {
		t.Fatalf("ListDelegates: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("len(got) = %d, want 0", len(got))
	}
}

func TestGmailListSharedMailboxesMapsForwardingAddresses(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.HasSuffix(r.URL.Path, "/settings/forwardingAddresses") {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		writeJSON(w, map[string]any{
			"forwardingAddresses": []map[string]any{
				{"forwardingEmail": "archive@example.com", "verificationStatus": "accepted"},
				{"forwardingEmail": "backup@example.com", "verificationStatus": "pending"},
			},
		})
	}
	provider := buildGmailStub(t, handler)

	got, err := provider.ListSharedMailboxes(context.Background())
	if err != nil {
		t.Fatalf("ListSharedMailboxes: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	if got[0].Email != "archive@example.com" || got[0].AccessLevel != "forwarding:accepted" {
		t.Fatalf("got[0] = %+v", got[0])
	}
	if got[1].AccessLevel != "forwarding:pending" {
		t.Fatalf("got[1].AccessLevel = %q", got[1].AccessLevel)
	}
}

func TestGmailDelegationProviderIsDeclared(t *testing.T) {
	var _ DelegationProvider = (*GmailProvider)(nil)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
