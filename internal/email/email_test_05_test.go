package email

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExchangeEWSListMessagesUsesInboxDistinguishedFolder(t *testing.T) {
	for _, folder := range []string{"INBOX", "Inbox", "Posteingang"} {
		t.Run(folder, func(t *testing.T) {
			server := newExchangeEWSFolderListServer(t, `<t:DistinguishedFolderId Id="inbox" />`, "")
			defer server.Close()
			provider, err := NewExchangeEWSMailProvider(ExchangeEWSConfig{Endpoint: server.URL, Username: "ert", Password: "secret"})
			if err != nil {
				t.Fatalf("NewExchangeEWSMailProvider() error: %v", err)
			}
			defer provider.Close()
			page, err := provider.ListMessagesPage(t.Context(), DefaultSearchOptions().WithFolder(folder).WithMaxResults(1), "")
			if err != nil {
				t.Fatalf("ListMessagesPage() error: %v", err)
			}
			if got := strings.Join(page.IDs, ","); got != "msg-1" {
				t.Fatalf("page.IDs = %q, want msg-1", got)
			}
		})
	}
}

func TestExchangeEWSListMessagesResolvesLocalizedInboxDisplayName(t *testing.T) {
	server := newExchangeEWSFolderListServer(t, `<t:FolderId Id="folder-inbox" />`, "Boite de reception")
	defer server.Close()
	provider, err := NewExchangeEWSMailProvider(ExchangeEWSConfig{Endpoint: server.URL, Username: "ert", Password: "secret"})
	if err != nil {
		t.Fatalf("NewExchangeEWSMailProvider() error: %v", err)
	}
	defer provider.Close()
	page, err := provider.ListMessagesPage(t.Context(), DefaultSearchOptions().WithFolder("Boite de reception").WithMaxResults(1), "")
	if err != nil {
		t.Fatalf("ListMessagesPage() error: %v", err)
	}
	if got := strings.Join(page.IDs, ","); got != "msg-1" {
		t.Fatalf("page.IDs = %q, want msg-1", got)
	}
}

func TestExchangeEWSListMessagesUsesDistinguishedFolderAliases(t *testing.T) {
	tests := map[string]string{"SENT": "sentitems", "DRAFTS": "drafts", "JUNK": "junkemail", "TRASH": "deleteditems", "ARCHIVE": "archivemsgfolderroot"}
	for folder, distinguished := range tests {
		t.Run(folder, func(t *testing.T) {
			wantXML := `<t:DistinguishedFolderId Id="` + distinguished + `" />`
			server := newExchangeEWSFolderListServer(t, wantXML, "")
			defer server.Close()
			cfg := ExchangeEWSConfig{Endpoint: server.URL, Username: "ert", Password: "secret"}
			if folder == "ARCHIVE" {
				cfg.ArchiveFolder = "archivemsgfolderroot"
			}
			provider, err := NewExchangeEWSMailProvider(cfg)
			if err != nil {
				t.Fatalf("NewExchangeEWSMailProvider() error: %v", err)
			}
			defer provider.Close()
			page, err := provider.ListMessagesPage(t.Context(), DefaultSearchOptions().WithFolder(folder).WithMaxResults(1), "")
			if err != nil {
				t.Fatalf("ListMessagesPage() error: %v", err)
			}
			if got := strings.Join(page.IDs, ","); got != "msg-1" {
				t.Fatalf("page.IDs = %q, want msg-1", got)
			}
		})
	}
}

func newExchangeEWSFolderListServer(t *testing.T, wantFindItemFolderXML, lookupFolder string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		body, _ := io.ReadAll(r.Body)
		action := strings.Trim(r.Header.Get("SOAPAction"), `"`)
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		switch {
		case strings.HasSuffix(action, "/FindFolder") && lookupFolder != "":
			_, _ = io.WriteString(w, exchangeEWSFindFolderResponse(lookupFolder))
		case strings.HasSuffix(action, "/FindFolder"):
			t.Fatalf("unexpected FindFolder body: %s", string(body))
		case strings.HasSuffix(action, "/FindItem"):
			if !strings.Contains(string(body), wantFindItemFolderXML) {
				t.Fatalf("FindItem body missing %s:\n%s", wantFindItemFolderXML, string(body))
			}
			_, _ = io.WriteString(w, exchangeEWSFindItemOneMessageResponse())
		default:
			t.Fatalf("unexpected SOAP action %q body=%s", action, string(body))
		}
	}))
}

func exchangeEWSFindFolderResponse(displayName string) string {
	return `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/" xmlns:m="http://schemas.microsoft.com/exchange/services/2006/messages" xmlns:t="http://schemas.microsoft.com/exchange/services/2006/types">
  <soap:Body><m:FindFolderResponse><m:ResponseMessages><m:FindFolderResponseMessage ResponseClass="Success"><m:ResponseCode>NoError</m:ResponseCode><m:RootFolder IncludesLastItemInRange="true" TotalItemsInView="1"><t:Folders><t:Folder><t:FolderId Id="folder-inbox" ChangeKey="ck1" /><t:DisplayName>` + displayName + `</t:DisplayName><t:TotalCount>1</t:TotalCount><t:UnreadCount>1</t:UnreadCount></t:Folder></t:Folders></m:RootFolder></m:FindFolderResponseMessage></m:ResponseMessages></m:FindFolderResponse></soap:Body>
</soap:Envelope>`
}

func exchangeEWSFindItemOneMessageResponse() string {
	return `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/" xmlns:m="http://schemas.microsoft.com/exchange/services/2006/messages" xmlns:t="http://schemas.microsoft.com/exchange/services/2006/types">
  <soap:Body><m:FindItemResponse><m:ResponseMessages><m:FindItemResponseMessage ResponseClass="Success"><m:ResponseCode>NoError</m:ResponseCode><m:RootFolder IncludesLastItemInRange="true" IndexedPagingOffset="0" TotalItemsInView="1"><t:Items><t:Message><t:ItemId Id="msg-1" ChangeKey="ck1" /></t:Message></t:Items></m:RootFolder></m:FindItemResponseMessage></m:ResponseMessages></m:FindItemResponse></soap:Body>
</soap:Envelope>`
}
