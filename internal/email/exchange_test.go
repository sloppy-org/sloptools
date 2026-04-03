package email

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExchangeConfigHelpers(t *testing.T) {
	cfg, err := ExchangeConfigFromMap("Work Mail", map[string]any{
		"client_id": "client-id",
		"tenant_id": "tenant-id",
		"scopes":    []any{"Mail.ReadWrite", "offline_access"},
	})
	if err != nil {
		t.Fatalf("ExchangeConfigFromMap() error: %v", err)
	}
	if cfg.ClientID != "client-id" || cfg.TenantID != "tenant-id" {
		t.Fatalf("ExchangeConfigFromMap() = %+v", cfg)
	}
	if got := ExchangeSecretEnvVar("Work Mail"); got != "SLOPSHELL_EXCHANGE_SECRET_WORK_MAIL" {
		t.Fatalf("ExchangeSecretEnvVar() = %q", got)
	}
	tokenPath := ExchangeTokenPath("/tmp/slopshell", "Work Mail")
	wantPath := filepath.Join("/tmp/slopshell", "tokens", "exchange_work_mail.json")
	if tokenPath != wantPath {
		t.Fatalf("ExchangeTokenPath() = %q, want %q", tokenPath, wantPath)
	}
}

func TestExchangeTokenFileRoundTripUsesRestrictedPermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tokens", "exchange_work_mail.json")
	want := exchangeToken{
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		TokenType:    "Bearer",
		ExpiresAt:    time.Date(2026, time.March, 9, 12, 0, 0, 0, time.UTC),
	}
	if err := saveExchangeTokenFile(path, want); err != nil {
		t.Fatalf("saveExchangeTokenFile() error: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error: %v", err)
	}
	if perms := info.Mode().Perm(); perms != 0o600 {
		t.Fatalf("token file perms = %o, want 600", perms)
	}
	got, err := loadExchangeTokenFile(path)
	if err != nil {
		t.Fatalf("loadExchangeTokenFile() error: %v", err)
	}
	if got != want {
		t.Fatalf("loadExchangeTokenFile() = %+v, want %+v", got, want)
	}
}

func TestExchangeClientUsesRefreshTokenForGraphOperations(t *testing.T) {
	tokensPath := filepath.Join(t.TempDir(), "exchange.json")
	if err := saveExchangeTokenFile(tokensPath, exchangeToken{
		RefreshToken: "refresh-token",
		ExpiresAt:    time.Date(2026, time.March, 9, 8, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("saveExchangeTokenFile() error: %v", err)
	}

	var tokenCalls int
	var movedIDs []string
	var deletedIDs []string
	var readStates []bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/tenant/oauth2/v2.0/token":
			tokenCalls++
			if err := r.ParseForm(); err != nil {
				t.Fatalf("ParseForm() error: %v", err)
			}
			if got := r.Form.Get("refresh_token"); got != "refresh-token" {
				t.Fatalf("refresh token = %q, want refresh-token", got)
			}
			if got := r.Form.Get("client_secret"); got != "secret-value" {
				t.Fatalf("client secret = %q, want secret-value", got)
			}
			writeJSON(t, w, map[string]any{
				"access_token":  "access-1",
				"refresh_token": "refresh-2",
				"token_type":    "Bearer",
				"expires_in":    3600,
			})
		case r.URL.Path == "/v1.0/me/mailFolders":
			requireBearer(t, r, "access-1")
			writeJSON(t, w, map[string]any{
				"value": []map[string]any{{
					"id":               "inbox-id",
					"displayName":      "Inbox",
					"wellKnownName":    "inbox",
					"childFolderCount": 0,
					"totalItemCount":   12,
					"unreadItemCount":  4,
				}},
			})
		case r.URL.Path == "/v1.0/me/mailFolders/inbox-id/messages":
			requireBearer(t, r, "access-1")
			if got := r.URL.Query().Get("$top"); got != "5" {
				t.Fatalf("$top = %q, want 5", got)
			}
			if got := r.URL.Query().Get("$select"); got != "id,subject" {
				t.Fatalf("$select = %q, want id,subject", got)
			}
			writeJSON(t, w, map[string]any{
				"value": []map[string]any{{
					"id":               "msg-1",
					"conversationId":   "conv-1",
					"subject":          "Quarterly review",
					"bodyPreview":      "Agenda attached",
					"isRead":           false,
					"parentFolderId":   "inbox-id",
					"receivedDateTime": "2026-03-09T09:00:00Z",
					"webLink":          "https://example.invalid/mail/msg-1",
				}},
			})
		case r.URL.Path == "/v1.0/me/messages/msg-1" && r.Method == http.MethodGet:
			requireBearer(t, r, "access-1")
			writeJSON(t, w, map[string]any{
				"id":               "msg-1",
				"conversationId":   "conv-1",
				"subject":          "Quarterly review",
				"bodyPreview":      "Agenda attached",
				"isRead":           false,
				"parentFolderId":   "inbox-id",
				"receivedDateTime": "2026-03-09T09:00:00Z",
			})
		case r.URL.Path == "/v1.0/me/messages/msg-1/move":
			requireBearer(t, r, "access-1")
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("Decode(move) error: %v", err)
			}
			movedIDs = append(movedIDs, body["destinationId"])
			writeJSON(t, w, map[string]any{"id": "msg-1"})
		case r.URL.Path == "/v1.0/me/messages/msg-1" && r.Method == http.MethodPatch:
			requireBearer(t, r, "access-1")
			var body map[string]bool
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("Decode(patch) error: %v", err)
			}
			readStates = append(readStates, body["isRead"])
			writeJSON(t, w, map[string]any{"id": "msg-1"})
		case r.URL.Path == "/v1.0/me/messages/msg-1" && r.Method == http.MethodDelete:
			requireBearer(t, r, "access-1")
			deletedIDs = append(deletedIDs, "msg-1")
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	now := time.Date(2026, time.March, 9, 10, 0, 0, 0, time.UTC)
	client, err := NewExchangeClient(ExchangeConfig{
		Label:       "Work Mail",
		ClientID:    "client-id",
		TenantID:    "tenant",
		BaseURL:     server.URL,
		AuthBaseURL: server.URL,
		TokenPath:   tokensPath,
	}, WithExchangeClock(func() time.Time { return now }), WithExchangeEnvLookup(func(key string) (string, bool) {
		if key != "SLOPSHELL_EXCHANGE_SECRET_WORK_MAIL" {
			t.Fatalf("env key = %q", key)
		}
		return "secret-value", true
	}))
	if err != nil {
		t.Fatalf("NewExchangeClient() error: %v", err)
	}

	folders, err := client.ListFolders(context.Background())
	if err != nil {
		t.Fatalf("ListFolders() error: %v", err)
	}
	if len(folders) != 1 || folders[0].DisplayName != "Inbox" {
		t.Fatalf("ListFolders() = %+v", folders)
	}
	messages, err := client.ListMessages(context.Background(), ListMessageOptions{
		FolderID: "inbox-id",
		Top:      5,
		Select:   []string{"id", "subject"},
	})
	if err != nil {
		t.Fatalf("ListMessages() error: %v", err)
	}
	if len(messages) != 1 || messages[0].ID != "msg-1" {
		t.Fatalf("ListMessages() = %+v", messages)
	}
	message, err := client.GetMessage(context.Background(), "msg-1")
	if err != nil {
		t.Fatalf("GetMessage() error: %v", err)
	}
	if message.Subject != "Quarterly review" {
		t.Fatalf("GetMessage() subject = %q", message.Subject)
	}
	if err := client.ArchiveMessage(context.Background(), "msg-1"); err != nil {
		t.Fatalf("ArchiveMessage() error: %v", err)
	}
	if err := client.MarkRead(context.Background(), "msg-1"); err != nil {
		t.Fatalf("MarkRead() error: %v", err)
	}
	if err := client.MarkUnread(context.Background(), "msg-1"); err != nil {
		t.Fatalf("MarkUnread() error: %v", err)
	}
	if err := client.DeleteMessage(context.Background(), "msg-1"); err != nil {
		t.Fatalf("DeleteMessage() error: %v", err)
	}

	if tokenCalls != 1 {
		t.Fatalf("token calls = %d, want 1", tokenCalls)
	}
	if len(movedIDs) != 1 || movedIDs[0] != "archive" {
		t.Fatalf("archive calls = %+v", movedIDs)
	}
	if len(readStates) != 2 || !readStates[0] || readStates[1] {
		t.Fatalf("read states = %+v", readStates)
	}
	if len(deletedIDs) != 1 || deletedIDs[0] != "msg-1" {
		t.Fatalf("deleted ids = %+v", deletedIDs)
	}

	saved, err := loadExchangeTokenFile(tokensPath)
	if err != nil {
		t.Fatalf("loadExchangeTokenFile() error: %v", err)
	}
	if saved.RefreshToken != "refresh-2" || saved.AccessToken != "access-1" {
		t.Fatalf("saved token = %+v", saved)
	}
}

type exchangeMailProviderTestActions struct {
	markReadIDs   []string
	markUnreadIDs []string
	archiveIDs    []string
	deleteIDs     []string
}

func TestExchangeMailProviderSupportsSearchAndFetch(t *testing.T) {
	provider, actions := newExchangeMailProviderForTest(t)
	assertExchangeMailProviderSearchAndFetch(t, provider)
	assertExchangeMailProviderMutations(t, provider, actions)
}

func newExchangeMailProviderForTest(t *testing.T) (*ExchangeMailProvider, *exchangeMailProviderTestActions) {
	t.Helper()
	tokensPath := filepath.Join(t.TempDir(), "exchange.json")
	if err := saveExchangeTokenFile(tokensPath, exchangeToken{
		RefreshToken: "refresh-token",
		ExpiresAt:    time.Date(2026, time.March, 9, 8, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("saveExchangeTokenFile() error: %v", err)
	}
	actions := &exchangeMailProviderTestActions{}
	server := newExchangeMailProviderTestServer(t, actions)
	t.Cleanup(server.Close)

	provider, err := NewExchangeMailProvider(ExchangeConfig{
		Label:       "Work Mail",
		ClientID:    "client-id",
		TenantID:    "tenant",
		BaseURL:     server.URL,
		AuthBaseURL: server.URL,
		TokenPath:   tokensPath,
	}, WithExchangeClock(func() time.Time {
		return time.Date(2026, time.March, 9, 10, 0, 0, 0, time.UTC)
	}), WithExchangeEnvLookup(func(string) (string, bool) {
		return "secret-value", true
	}))
	if err != nil {
		t.Fatalf("NewExchangeMailProvider() error: %v", err)
	}
	return provider, actions
}

func newExchangeMailProviderTestServer(t *testing.T, actions *exchangeMailProviderTestActions) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/tenant/oauth2/v2.0/token":
			if err := r.ParseForm(); err != nil {
				t.Fatalf("ParseForm() error: %v", err)
			}
			writeJSON(t, w, map[string]any{
				"access_token":  "access-1",
				"refresh_token": "refresh-2",
				"token_type":    "Bearer",
				"expires_in":    3600,
			})
		case r.URL.Path == "/v1.0/me/mailFolders":
			requireBearer(t, r, "access-1")
			writeJSON(t, w, exchangeMailProviderFoldersResponse())
		case r.URL.Path == "/v1.0/me/mailFolders/inbox-id/messages":
			requireBearer(t, r, "access-1")
			writeJSON(t, w, exchangeMailProviderListResponse())
		case r.URL.Path == "/v1.0/me/messages/msg-1" && r.Method == http.MethodGet:
			requireBearer(t, r, "access-1")
			writeJSON(t, w, exchangeMailProviderMessageResponse())
		case r.URL.Path == "/v1.0/me/messages/msg-1" && r.Method == http.MethodPatch:
			requireBearer(t, r, "access-1")
			var body map[string]bool
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("Decode(patch) error: %v", err)
			}
			if body["isRead"] {
				actions.markReadIDs = append(actions.markReadIDs, "msg-1")
			} else {
				actions.markUnreadIDs = append(actions.markUnreadIDs, "msg-1")
			}
			writeJSON(t, w, map[string]any{"id": "msg-1"})
		case r.URL.Path == "/v1.0/me/messages/msg-1/move":
			requireBearer(t, r, "access-1")
			actions.archiveIDs = append(actions.archiveIDs, "msg-1")
			writeJSON(t, w, map[string]any{"id": "msg-1"})
		case r.URL.Path == "/v1.0/me/messages/msg-1" && r.Method == http.MethodDelete:
			requireBearer(t, r, "access-1")
			actions.deleteIDs = append(actions.deleteIDs, "msg-1")
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
}

func exchangeMailProviderFoldersResponse() map[string]any {
	return map[string]any{
		"value": []map[string]any{
			{
				"id":               "inbox-id",
				"displayName":      "Inbox",
				"wellKnownName":    "inbox",
				"totalItemCount":   12,
				"unreadItemCount":  3,
				"childFolderCount": 0,
			},
			{
				"id":               "contracts-id",
				"displayName":      "Contracts",
				"wellKnownName":    "",
				"totalItemCount":   4,
				"unreadItemCount":  1,
				"childFolderCount": 0,
			},
		},
	}
}

func exchangeMailProviderListResponse() map[string]any {
	return map[string]any{
		"value": []map[string]any{
			{
				"id":               "msg-1",
				"conversationId":   "conv-1",
				"subject":          "Quarterly review",
				"bodyPreview":      "Please review the budget appendix",
				"isRead":           false,
				"hasAttachments":   true,
				"flag":             map[string]any{"flagStatus": "flagged"},
				"parentFolderId":   "contracts-id",
				"receivedDateTime": "2026-03-09T09:00:00Z",
				"from":             map[string]any{"emailAddress": map[string]any{"name": "Ada", "address": "ada@example.com"}},
				"toRecipients":     []map[string]any{{"emailAddress": map[string]any{"name": "Team", "address": "team@example.com"}}},
			},
			{
				"id":               "msg-2",
				"conversationId":   "conv-2",
				"subject":          "Archive me",
				"bodyPreview":      "No action",
				"isRead":           true,
				"hasAttachments":   false,
				"flag":             map[string]any{"flagStatus": "notFlagged"},
				"parentFolderId":   "inbox-id",
				"receivedDateTime": "2026-03-08T09:00:00Z",
				"from":             map[string]any{"emailAddress": map[string]any{"name": "Bob", "address": "bob@example.com"}},
			},
		},
	}
}

func exchangeMailProviderMessageResponse() map[string]any {
	return map[string]any{
		"id":               "msg-1",
		"conversationId":   "conv-1",
		"subject":          "Quarterly review",
		"bodyPreview":      "Please review the budget appendix",
		"body":             map[string]any{"contentType": "html", "content": "<p>Please review the budget appendix by March 12.</p>"},
		"isRead":           false,
		"hasAttachments":   true,
		"flag":             map[string]any{"flagStatus": "flagged"},
		"parentFolderId":   "contracts-id",
		"receivedDateTime": "2026-03-09T09:00:00Z",
		"webLink":          "https://example.invalid/mail/msg-1",
		"from":             map[string]any{"emailAddress": map[string]any{"name": "Ada", "address": "ada@example.com"}},
		"toRecipients":     []map[string]any{{"emailAddress": map[string]any{"name": "Team", "address": "team@example.com"}}},
		"ccRecipients":     []map[string]any{{"emailAddress": map[string]any{"name": "Ops", "address": "ops@example.com"}}},
	}
}

func assertExchangeMailProviderSearchAndFetch(t *testing.T, provider *ExchangeMailProvider) {
	t.Helper()
	labels, err := provider.ListLabels(context.Background())
	if err != nil {
		t.Fatalf("ListLabels() error: %v", err)
	}
	if len(labels) != 2 || labels[1].Name != "Contracts" {
		t.Fatalf("ListLabels() = %+v", labels)
	}

	ids, err := provider.ListMessages(context.Background(), DefaultSearchOptions().
		WithFolder("INBOX").
		WithIsRead(false).
		WithIsFlagged(true).
		WithSubject("Quarterly").
		WithText("budget").
		WithHasAttachment(true).
		WithMaxResults(10))
	if err != nil {
		t.Fatalf("ListMessages() error: %v", err)
	}
	if len(ids) != 1 || ids[0] != "msg-1" {
		t.Fatalf("ListMessages() = %+v, want [msg-1]", ids)
	}

	message, err := provider.GetMessage(context.Background(), "msg-1", "full")
	if err != nil {
		t.Fatalf("GetMessage() error: %v", err)
	}
	if message.Subject != "Quarterly review" || message.ThreadID != "conv-1" {
		t.Fatalf("GetMessage() = %+v", message)
	}
	if message.BodyText == nil || !strings.Contains(*message.BodyText, "review the budget appendix") {
		t.Fatalf("GetMessage().BodyText = %v", message.BodyText)
	}
	if len(message.Labels) == 0 || message.Labels[0] != "Contracts" {
		t.Fatalf("GetMessage().Labels = %+v, want Contracts first", message.Labels)
	}
	if len(message.Attachments) != 1 {
		t.Fatalf("GetMessage().Attachments = %+v, want one attachment placeholder", message.Attachments)
	}

	messages, err := provider.GetMessages(context.Background(), []string{"msg-1"}, "full")
	if err != nil {
		t.Fatalf("GetMessages() error: %v", err)
	}
	if len(messages) != 1 || messages[0].ID != "msg-1" {
		t.Fatalf("GetMessages() = %+v", messages)
	}
}

func assertExchangeMailProviderMutations(t *testing.T, provider *ExchangeMailProvider, actions *exchangeMailProviderTestActions) {
	t.Helper()
	if _, err := provider.MarkRead(context.Background(), []string{"msg-1"}); err != nil {
		t.Fatalf("MarkRead() error: %v", err)
	}
	if _, err := provider.MarkUnread(context.Background(), []string{"msg-1"}); err != nil {
		t.Fatalf("MarkUnread() error: %v", err)
	}
	if _, err := provider.Archive(context.Background(), []string{"msg-1"}); err != nil {
		t.Fatalf("Archive() error: %v", err)
	}
	if _, err := provider.Trash(context.Background(), []string{"msg-1"}); err != nil {
		t.Fatalf("Trash() error: %v", err)
	}
	if _, err := provider.Delete(context.Background(), []string{"msg-1"}); err != nil {
		t.Fatalf("Delete() error: %v", err)
	}
	if provider.ProviderName() != "exchange" {
		t.Fatalf("ProviderName() = %q, want exchange", provider.ProviderName())
	}

	if len(actions.markReadIDs) != 1 || len(actions.markUnreadIDs) != 1 || len(actions.archiveIDs) != 1 || len(actions.deleteIDs) != 2 {
		t.Fatalf("actions read=%v unread=%v archive=%v delete=%v", actions.markReadIDs, actions.markUnreadIDs, actions.archiveIDs, actions.deleteIDs)
	}
}

func TestExchangeClientFallsBackToDeviceCodeFlow(t *testing.T) {
	var promptInfo DeviceCodeInfo
	var tokenPolls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/tenant/oauth2/v2.0/devicecode":
			if err := r.ParseForm(); err != nil {
				t.Fatalf("ParseForm() error: %v", err)
			}
			if got := r.Form.Get("scope"); got != "offline_access Mail.ReadWrite Contacts.Read" {
				t.Fatalf("scope = %q", got)
			}
			writeJSON(t, w, map[string]any{
				"device_code":      "device-code",
				"user_code":        "ABCD-EFGH",
				"verification_uri": "https://microsoft.com/devicelogin",
				"verification_url": "https://microsoft.com/devicelogin",
				"message":          "Open the browser and enter the code.",
				"expires_in":       900,
				"interval":         1,
			})
		case "/tenant/oauth2/v2.0/token":
			tokenPolls++
			if err := r.ParseForm(); err != nil {
				t.Fatalf("ParseForm() error: %v", err)
			}
			if tokenPolls == 1 {
				w.WriteHeader(http.StatusBadRequest)
				writeJSON(t, w, map[string]any{
					"error": map[string]any{
						"code":    "authorization_pending",
						"message": "authorization pending",
					},
				})
				return
			}
			writeJSON(t, w, map[string]any{
				"access_token":  "device-access",
				"refresh_token": "device-refresh",
				"token_type":    "Bearer",
				"expires_in":    3600,
			})
		case "/v1.0/me/mailFolders":
			requireBearer(t, r, "device-access")
			writeJSON(t, w, map[string]any{"value": []map[string]any{}})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	sleepCalls := 0
	client, err := NewExchangeClient(ExchangeConfig{
		Label:       "Work Mail",
		ClientID:    "client-id",
		TenantID:    "tenant",
		BaseURL:     server.URL,
		AuthBaseURL: server.URL,
		TokenPath:   filepath.Join(t.TempDir(), "exchange.json"),
	}, WithExchangeDeviceCodePrompt(func(info DeviceCodeInfo) error {
		promptInfo = info
		return nil
	}), WithExchangeSleep(func(context.Context, time.Duration) error {
		sleepCalls++
		return nil
	}), WithExchangeClock(func() time.Time {
		return time.Date(2026, time.March, 9, 10, 0, 0, 0, time.UTC)
	}), WithExchangeEnvLookup(func(string) (string, bool) {
		return "", false
	}))
	if err != nil {
		t.Fatalf("NewExchangeClient() error: %v", err)
	}

	folders, err := client.ListFolders(context.Background())
	if err != nil {
		t.Fatalf("ListFolders() error: %v", err)
	}
	if len(folders) != 0 {
		t.Fatalf("ListFolders() = %+v, want empty", folders)
	}
	if promptInfo.UserCode != "ABCD-EFGH" || !strings.Contains(promptInfo.Message, "enter the code") {
		t.Fatalf("prompt info = %+v", promptInfo)
	}
	if tokenPolls != 2 {
		t.Fatalf("token polls = %d, want 2", tokenPolls)
	}
	if sleepCalls != 2 {
		t.Fatalf("sleep calls = %d, want 2", sleepCalls)
	}
}

func requireBearer(t *testing.T, r *http.Request, want string) {
	t.Helper()
	if got := r.Header.Get("Authorization"); got != "Bearer "+want {
		t.Fatalf("Authorization = %q, want %q", got, "Bearer "+want)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, body map[string]any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(body); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}
}
