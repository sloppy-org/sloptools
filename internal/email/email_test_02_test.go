package email

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExchangeEWSListMessagesPageUsesPagingOffsets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		body, _ := io.ReadAll(r.Body)
		action := strings.Trim(r.Header.Get("SOAPAction"), `"`)
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		switch {
		case strings.HasSuffix(action, "/FindFolder"):
			_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/" xmlns:m="http://schemas.microsoft.com/exchange/services/2006/messages" xmlns:t="http://schemas.microsoft.com/exchange/services/2006/types">
  <soap:Body>
    <m:FindFolderResponse>
      <m:ResponseMessages>
        <m:FindFolderResponseMessage ResponseClass="Success">
          <m:ResponseCode>NoError</m:ResponseCode>
          <m:RootFolder IncludesLastItemInRange="true" IndexedPagingOffset="0" TotalItemsInView="1">
            <t:Folders>
              <t:Folder>
                <t:FolderId Id="folder-inbox" ChangeKey="ck1" />
                <t:DisplayName>Posteingang</t:DisplayName>
                <t:TotalCount>3</t:TotalCount>
                <t:UnreadCount>1</t:UnreadCount>
              </t:Folder>
            </t:Folders>
          </m:RootFolder>
        </m:FindFolderResponseMessage>
      </m:ResponseMessages>
    </m:FindFolderResponse>
  </soap:Body>
</soap:Envelope>`)
		case strings.HasSuffix(action, "/FindItem"):
			switch {
			case strings.Contains(string(body), `Offset="0"`):
				_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/" xmlns:m="http://schemas.microsoft.com/exchange/services/2006/messages" xmlns:t="http://schemas.microsoft.com/exchange/services/2006/types">
  <soap:Body>
    <m:FindItemResponse>
      <m:ResponseMessages>
        <m:FindItemResponseMessage ResponseClass="Success">
          <m:ResponseCode>NoError</m:ResponseCode>
          <m:RootFolder IncludesLastItemInRange="false" IndexedPagingOffset="2" TotalItemsInView="3">
            <t:Items>
              <t:Message><t:ItemId Id="msg-1" ChangeKey="a" /></t:Message>
              <t:Message><t:ItemId Id="msg-2" ChangeKey="b" /></t:Message>
            </t:Items>
          </m:RootFolder>
        </m:FindItemResponseMessage>
      </m:ResponseMessages>
    </m:FindItemResponse>
  </soap:Body>
</soap:Envelope>`)
			case strings.Contains(string(body), `Offset="2"`):
				_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/" xmlns:m="http://schemas.microsoft.com/exchange/services/2006/messages" xmlns:t="http://schemas.microsoft.com/exchange/services/2006/types">
  <soap:Body>
    <m:FindItemResponse>
      <m:ResponseMessages>
        <m:FindItemResponseMessage ResponseClass="Success">
          <m:ResponseCode>NoError</m:ResponseCode>
          <m:RootFolder IncludesLastItemInRange="true" IndexedPagingOffset="0" TotalItemsInView="3">
            <t:Items>
              <t:Message><t:ItemId Id="msg-3" ChangeKey="c" /></t:Message>
            </t:Items>
          </m:RootFolder>
        </m:FindItemResponseMessage>
      </m:ResponseMessages>
    </m:FindItemResponse>
  </soap:Body>
</soap:Envelope>`)
			default:
				t.Fatalf("unexpected FindItem body: %s", string(body))
			}
		case strings.HasSuffix(action, "/GetItem"):
			switch {
			case strings.Contains(string(body), `Id="msg-1"`):
				_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/" xmlns:m="http://schemas.microsoft.com/exchange/services/2006/messages" xmlns:t="http://schemas.microsoft.com/exchange/services/2006/types">
  <soap:Body>
    <m:GetItemResponse>
      <m:ResponseMessages>
        <m:GetItemResponseMessage ResponseClass="Success">
          <m:ResponseCode>NoError</m:ResponseCode>
          <m:Items>
            <t:Message><t:ItemId Id="msg-1" ChangeKey="a" /><t:Subject>One</t:Subject></t:Message>
            <t:Message><t:ItemId Id="msg-2" ChangeKey="b" /><t:Subject>Two</t:Subject></t:Message>
          </m:Items>
        </m:GetItemResponseMessage>
      </m:ResponseMessages>
    </m:GetItemResponse>
  </soap:Body>
</soap:Envelope>`)
			case strings.Contains(string(body), `Id="msg-3"`):
				_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/" xmlns:m="http://schemas.microsoft.com/exchange/services/2006/messages" xmlns:t="http://schemas.microsoft.com/exchange/services/2006/types">
  <soap:Body>
    <m:GetItemResponse>
      <m:ResponseMessages>
        <m:GetItemResponseMessage ResponseClass="Success">
          <m:ResponseCode>NoError</m:ResponseCode>
          <m:Items>
            <t:Message><t:ItemId Id="msg-3" ChangeKey="c" /><t:Subject>Three</t:Subject></t:Message>
          </m:Items>
        </m:GetItemResponseMessage>
      </m:ResponseMessages>
    </m:GetItemResponse>
  </soap:Body>
</soap:Envelope>`)
			default:
				t.Fatalf("unexpected GetItem body: %s", string(body))
			}
		default:
			t.Fatalf("unexpected SOAP action %q body=%s", action, string(body))
		}
	}))
	defer server.Close()
	provider, err := NewExchangeEWSMailProvider(ExchangeEWSConfig{Endpoint: server.URL, Username: "ert", Password: "secret"})
	if err != nil {
		t.Fatalf("NewExchangeEWSMailProvider() error: %v", err)
	}
	defer provider.Close()
	firstPage, err := provider.ListMessagesPage(t.Context(), DefaultSearchOptions().WithFolder("INBOX").WithMaxResults(2), "")
	if err != nil {
		t.Fatalf("ListMessagesPage(first) error: %v", err)
	}
	if got := strings.Join(firstPage.IDs, ","); got != "msg-1,msg-2" {
		t.Fatalf("firstPage.IDs = %q, want msg-1,msg-2", got)
	}
	if firstPage.NextPageToken != "2" {
		t.Fatalf("firstPage.NextPageToken = %q, want 2", firstPage.NextPageToken)
	}
	secondPage, err := provider.ListMessagesPage(t.Context(), DefaultSearchOptions().WithFolder("INBOX").WithMaxResults(2), firstPage.NextPageToken)
	if err != nil {
		t.Fatalf("ListMessagesPage(second) error: %v", err)
	}
	if got := strings.Join(secondPage.IDs, ","); got != "msg-3" {
		t.Fatalf("secondPage.IDs = %q, want msg-3", got)
	}
	if secondPage.NextPageToken != "" {
		t.Fatalf("secondPage.NextPageToken = %q, want empty", secondPage.NextPageToken)
	}
}

func TestExchangeEWSTrashResolvedUsesMoveToDeletedItems(t *testing.T) {
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		data, _ := io.ReadAll(r.Body)
		body = string(data)
		action := strings.Trim(r.Header.Get("SOAPAction"), `"`)
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		if !strings.HasSuffix(action, "/MoveItem") {
			t.Fatalf("unexpected SOAP action %q", action)
		}
		_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/" xmlns:m="http://schemas.microsoft.com/exchange/services/2006/messages" xmlns:t="http://schemas.microsoft.com/exchange/services/2006/types">
  <soap:Body>
    <m:MoveItemResponse>
      <m:ResponseMessages>
        <m:MoveItemResponseMessage ResponseClass="Success">
          <m:ResponseCode>NoError</m:ResponseCode>
          <m:Items>
            <t:Message><t:ItemId Id="trash-1" ChangeKey="ck1" /></t:Message>
          </m:Items>
        </m:MoveItemResponseMessage>
      </m:ResponseMessages>
    </m:MoveItemResponse>
  </soap:Body>
</soap:Envelope>`)
	}))
	defer server.Close()
	provider, err := NewExchangeEWSMailProvider(ExchangeEWSConfig{Endpoint: server.URL, Username: "ert", Password: "secret"})
	if err != nil {
		t.Fatalf("NewExchangeEWSMailProvider() error: %v", err)
	}
	defer provider.Close()
	resolutions, err := provider.TrashResolved(t.Context(), []string{"msg-1"})
	if err != nil {
		t.Fatalf("TrashResolved() error: %v", err)
	}
	if len(resolutions) != 1 {
		t.Fatalf("len(resolutions) = %d, want 1", len(resolutions))
	}
	if resolutions[0].ResolvedMessageID != "trash-1" {
		t.Fatalf("resolved id = %q, want trash-1", resolutions[0].ResolvedMessageID)
	}
	if !strings.Contains(body, `<t:DistinguishedFolderId Id="deleteditems" />`) {
		t.Fatalf("MoveItem body missing deleteditems folder: %s", body)
	}
}

func TestExchangeEWSGetMessagesSkipsMissingItemsOnBatchFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		body, _ := io.ReadAll(r.Body)
		action := strings.Trim(r.Header.Get("SOAPAction"), `"`)
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		switch {
		case strings.HasSuffix(action, "/GetItem") && strings.Contains(string(body), `Id="missing"`):
			_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/" xmlns:m="http://schemas.microsoft.com/exchange/services/2006/messages">
  <soap:Body>
    <m:GetItemResponse>
      <m:ResponseMessages>
        <m:GetItemResponseMessage ResponseClass="Error">
          <m:ResponseCode>ErrorItemNotFound</m:ResponseCode>
        </m:GetItemResponseMessage>
      </m:ResponseMessages>
    </m:GetItemResponse>
  </soap:Body>
</soap:Envelope>`)
		case strings.HasSuffix(action, "/GetItem") && strings.Contains(string(body), `Id="good"`):
			_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/" xmlns:m="http://schemas.microsoft.com/exchange/services/2006/messages" xmlns:t="http://schemas.microsoft.com/exchange/services/2006/types">
  <soap:Body>
    <m:GetItemResponse>
      <m:ResponseMessages>
        <m:GetItemResponseMessage ResponseClass="Success">
          <m:ResponseCode>NoError</m:ResponseCode>
          <m:Items>
            <t:Message>
              <t:ItemId Id="good" ChangeKey="ck1" />
              <t:ParentFolderId Id="inbox-id" ChangeKey="f1" />
              <t:ConversationId Id="thread-1" />
              <t:Subject>Good message</t:Subject>
              <t:DateTimeReceived>2026-03-16T12:00:00Z</t:DateTimeReceived>
            </t:Message>
          </m:Items>
        </m:GetItemResponseMessage>
      </m:ResponseMessages>
    </m:GetItemResponse>
  </soap:Body>
</soap:Envelope>`)
		case strings.HasSuffix(action, "/FindFolder"):
			_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/" xmlns:m="http://schemas.microsoft.com/exchange/services/2006/messages" xmlns:t="http://schemas.microsoft.com/exchange/services/2006/types">
  <soap:Body>
    <m:FindFolderResponse>
      <m:ResponseMessages>
        <m:FindFolderResponseMessage ResponseClass="Success">
          <m:ResponseCode>NoError</m:ResponseCode>
          <m:RootFolder IncludesLastItemInRange="true" TotalItemsInView="1">
            <t:Folders>
              <t:Folder>
                <t:FolderId Id="inbox-id" ChangeKey="f1" />
                <t:DisplayName>Posteingang</t:DisplayName>
                <t:FolderClass>IPF.Note</t:FolderClass>
                <t:TotalCount>1</t:TotalCount>
                <t:UnreadCount>1</t:UnreadCount>
              </t:Folder>
            </t:Folders>
          </m:RootFolder>
        </m:FindFolderResponseMessage>
      </m:ResponseMessages>
    </m:FindFolderResponse>
  </soap:Body>
</soap:Envelope>`)
		default:
			t.Fatalf("unexpected SOAP action %q body=%s", action, string(body))
		}
	}))
	defer server.Close()
	provider, err := NewExchangeEWSMailProvider(ExchangeEWSConfig{Endpoint: server.URL, Username: "ert", Password: "secret"})
	if err != nil {
		t.Fatalf("NewExchangeEWSMailProvider() error: %v", err)
	}
	defer provider.Close()
	messages, err := provider.GetMessages(t.Context(), []string{"good", "missing"}, "full")
	if err != nil {
		t.Fatalf("GetMessages() error: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("messages len = %d, want 1", len(messages))
	}
	if messages[0].ID != "good" {
		t.Fatalf("message id = %q, want good", messages[0].ID)
	}
	if len(messages[0].Labels) < 1 || messages[0].Labels[0] != "Posteingang" {
		t.Fatalf("labels = %#v, want first label Posteingang", messages[0].Labels)
	}
}

func TestExchangeEWSGetMessagesMapsParentFolderToLabels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		action := strings.Trim(r.Header.Get("SOAPAction"), `"`)
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		switch {
		case strings.HasSuffix(action, "/GetItem"):
			_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/" xmlns:m="http://schemas.microsoft.com/exchange/services/2006/messages" xmlns:t="http://schemas.microsoft.com/exchange/services/2006/types">
  <soap:Body>
    <m:GetItemResponse>
      <m:ResponseMessages>
        <m:GetItemResponseMessage ResponseClass="Success">
          <m:ResponseCode>NoError</m:ResponseCode>
          <m:Items>
            <t:Message>
              <t:ItemId Id="msg-1" ChangeKey="ck1" />
              <t:ConversationId Id="thread-1" />
              <t:ParentFolderId Id="folder-cc" ChangeKey="f1" />
              <t:Subject>CC note</t:Subject>
            </t:Message>
            <t:Message>
              <t:ItemId Id="msg-2" ChangeKey="ck2" />
              <t:ConversationId Id="thread-2" />
              <t:ParentFolderId Id="folder-inbox" ChangeKey="f2" />
              <t:Subject>Inbox note</t:Subject>
            </t:Message>
          </m:Items>
        </m:GetItemResponseMessage>
      </m:ResponseMessages>
    </m:GetItemResponse>
  </soap:Body>
</soap:Envelope>`)
		case strings.HasSuffix(action, "/FindFolder"):
			_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/" xmlns:m="http://schemas.microsoft.com/exchange/services/2006/messages" xmlns:t="http://schemas.microsoft.com/exchange/services/2006/types">
  <soap:Body>
    <m:FindFolderResponse>
      <m:ResponseMessages>
        <m:FindFolderResponseMessage ResponseClass="Success">
          <m:ResponseCode>NoError</m:ResponseCode>
          <m:RootFolder IncludesLastItemInRange="true" IndexedPagingOffset="0" TotalItemsInView="2">
            <t:Folders>
              <t:Folder>
                <t:FolderId Id="folder-cc" ChangeKey="f1" />
                <t:DisplayName>CC</t:DisplayName>
                <t:TotalCount>1</t:TotalCount>
                <t:UnreadCount>0</t:UnreadCount>
              </t:Folder>
              <t:Folder>
                <t:FolderId Id="folder-inbox" ChangeKey="f2" />
                <t:DisplayName>Posteingang</t:DisplayName>
                <t:TotalCount>1</t:TotalCount>
                <t:UnreadCount>1</t:UnreadCount>
              </t:Folder>
            </t:Folders>
          </m:RootFolder>
        </m:FindFolderResponseMessage>
      </m:ResponseMessages>
    </m:FindFolderResponse>
  </soap:Body>
</soap:Envelope>`)
		default:
			t.Fatalf("unexpected SOAP action %q", action)
		}
	}))
	defer server.Close()
	provider, err := NewExchangeEWSMailProvider(ExchangeEWSConfig{Endpoint: server.URL, Username: "ert", Password: "secret"})
	if err != nil {
		t.Fatalf("NewExchangeEWSMailProvider() error: %v", err)
	}
	defer provider.Close()
	messages, err := provider.GetMessages(t.Context(), []string{"msg-1", "msg-2"}, "full")
	if err != nil {
		t.Fatalf("GetMessages() error: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("len(messages) = %d, want 2", len(messages))
	}
	if got := strings.Join(messages[0].Labels, ","); got != "CC" {
		t.Fatalf("messages[0].Labels = %q, want CC", got)
	}
	if got := strings.Join(messages[1].Labels, ","); got != "Posteingang,INBOX" {
		t.Fatalf("messages[1].Labels = %q, want Posteingang,INBOX", got)
	}
	metadata, err := provider.GetMessages(t.Context(), []string{"msg-1"}, "metadata")
	if err != nil {
		t.Fatalf("GetMessages(metadata) error: %v", err)
	}
	if len(metadata) != 1 {
		t.Fatalf("len(metadata) = %d, want 1", len(metadata))
	}
	if metadata[0].BodyText != nil {
		t.Fatalf("metadata body = %v, want nil", *metadata[0].BodyText)
	}
}

func TestExchangeConfigHelpers(t *testing.T) {
	cfg, err := ExchangeConfigFromMap("Work Mail", map[string]any{"client_id": "client-id", "tenant_id": "tenant-id", "scopes": []any{"Mail.ReadWrite", "offline_access"}})
	if err != nil {
		t.Fatalf("ExchangeConfigFromMap() error: %v", err)
	}
	if cfg.ClientID != "client-id" || cfg.TenantID != "tenant-id" {
		t.Fatalf("ExchangeConfigFromMap() = %+v", cfg)
	}
	if got := ExchangeSecretEnvVar("Work Mail"); got != "SLOPPY_EXCHANGE_SECRET_WORK_MAIL" {
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
	want := exchangeToken{AccessToken: "access-token", RefreshToken: "refresh-token", TokenType: "Bearer", ExpiresAt: time.Date(2026, time.March, 9, 12, 0, 0, 0, time.UTC)}
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
