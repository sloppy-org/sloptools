package ews

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestClientSyncFolderItemsParsesChanges(t *testing.T) {
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		data, _ := io.ReadAll(r.Body)
		body = string(data)
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/" xmlns:m="http://schemas.microsoft.com/exchange/services/2006/messages" xmlns:t="http://schemas.microsoft.com/exchange/services/2006/types">
  <soap:Body>
    <m:SyncFolderItemsResponse>
      <m:ResponseMessages>
        <m:SyncFolderItemsResponseMessage ResponseClass="Success">
          <m:ResponseCode>NoError</m:ResponseCode>
          <m:SyncState>state-2</m:SyncState>
          <m:IncludesLastItemInRange>false</m:IncludesLastItemInRange>
          <m:Changes>
            <t:Create><t:Message><t:ItemId Id="msg-1" ChangeKey="a" /></t:Message></t:Create>
            <t:Update><t:Message><t:ItemId Id="msg-2" ChangeKey="b" /></t:Message></t:Update>
            <t:Delete><t:ItemId Id="msg-gone" ChangeKey="c" /></t:Delete>
            <t:ReadFlagChange><t:ItemId Id="msg-3" ChangeKey="d" /></t:ReadFlagChange>
          </m:Changes>
        </m:SyncFolderItemsResponseMessage>
      </m:ResponseMessages>
    </m:SyncFolderItemsResponse>
  </soap:Body>
</soap:Envelope>`)
	}))
	defer server.Close()
	client, err := NewClient(Config{Endpoint: server.URL, Username: "ert", Password: "secret"})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer client.Close()
	result, err := client.SyncFolderItems(t.Context(), "inbox", "state-1", 200)
	if err != nil {
		t.Fatalf("SyncFolderItems() error: %v", err)
	}
	if result.SyncState != "state-2" {
		t.Fatalf("SyncState = %q, want state-2", result.SyncState)
	}
	if result.IncludesLastItem {
		t.Fatal("IncludesLastItem = true, want false")
	}
	if got := strings.Join(result.ItemIDs, ","); got != "msg-1,msg-2,msg-3" {
		t.Fatalf("ItemIDs = %q, want msg-1,msg-2,msg-3", got)
	}
	if got := strings.Join(result.DeletedItemIDs, ","); got != "msg-gone" {
		t.Fatalf("DeletedItemIDs = %q, want msg-gone", got)
	}
	for _, snippet := range []string{"<m:SyncFolderItems>", "<m:SyncState>state-1</m:SyncState>", "<m:MaxChangesReturned>200</m:MaxChangesReturned>"} {
		if !strings.Contains(body, snippet) {
			t.Fatalf("request body missing %q:\n%s", snippet, body)
		}
	}
}

func TestClientCreateAttachmentReturnsNewChangeKey(t *testing.T) {
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		body = string(data)
		r.Body.Close()
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/" xmlns:m="http://schemas.microsoft.com/exchange/services/2006/messages" xmlns:t="http://schemas.microsoft.com/exchange/services/2006/types">
  <soap:Body>
    <m:CreateAttachmentResponse>
      <m:ResponseMessages>
        <m:CreateAttachmentResponseMessage ResponseClass="Success">
          <m:ResponseCode>NoError</m:ResponseCode>
          <m:Attachments>
            <t:FileAttachment>
              <t:AttachmentId Id="att-1" RootItemId="draft-1" RootItemChangeKey="ck-updated" />
              <t:Name>note.pdf</t:Name>
            </t:FileAttachment>
          </m:Attachments>
        </m:CreateAttachmentResponseMessage>
      </m:ResponseMessages>
    </m:CreateAttachmentResponse>
  </soap:Body>
</soap:Envelope>`)
	}))
	defer server.Close()
	client, err := NewClient(Config{Endpoint: server.URL, Username: "ert-att", Password: "secret"})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer client.Close()
	newKey, err := client.CreateAttachment(t.Context(), "draft-1", "ck-old", AttachmentFile{Name: "note.pdf", ContentType: "application/pdf", Content: []byte("hello")})
	if err != nil {
		t.Fatalf("CreateAttachment() error: %v", err)
	}
	if newKey != "ck-updated" {
		t.Fatalf("new change key = %q, want ck-updated", newKey)
	}
	for _, snippet := range []string{`<m:ParentItemId Id="draft-1" ChangeKey="ck-old" />`, `<t:Name>note.pdf</t:Name>`, `<t:ContentType>application/pdf</t:ContentType>`, `<t:Content>aGVsbG8=</t:Content>`} {
		if !strings.Contains(body, snippet) {
			t.Fatalf("request body missing %q:\n%s", snippet, body)
		}
	}
}

func TestClientRetriesOn401WithFreshSession(t *testing.T) {
	var (
		requests   atomic.Int32
		cookieSeen atomic.Int32
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := requests.Add(1)
		if strings.Contains(r.Header.Get("Cookie"), "X-BackEndCookie=") {
			cookieSeen.Add(1)
		}
		if n == 1 {
			http.SetCookie(w, &http.Cookie{Name: "X-BackEndCookie", Value: "sticky", Path: "/"})
			w.Header().Set("Content-Type", "text/xml; charset=utf-8")
			_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/" xmlns:m="http://schemas.microsoft.com/exchange/services/2006/messages" xmlns:t="http://schemas.microsoft.com/exchange/services/2006/types">
  <soap:Body>
    <m:GetItemResponse>
      <m:ResponseMessages>
        <m:GetItemResponseMessage ResponseClass="Success">
          <m:ResponseCode>NoError</m:ResponseCode>
          <m:Items><t:Message><t:ItemId Id="msg-1" ChangeKey="ck-1" /></t:Message></m:Items>
        </m:GetItemResponseMessage>
      </m:ResponseMessages>
    </m:GetItemResponse>
  </soap:Body>
</soap:Envelope>`)
			return
		}
		if n == 2 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if strings.Contains(r.Header.Get("Cookie"), "X-BackEndCookie=") {
			t.Errorf("retry request still carries stale session cookie")
		}
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/" xmlns:m="http://schemas.microsoft.com/exchange/services/2006/messages" xmlns:t="http://schemas.microsoft.com/exchange/services/2006/types">
  <soap:Body>
    <m:GetItemResponse>
      <m:ResponseMessages>
        <m:GetItemResponseMessage ResponseClass="Success">
          <m:ResponseCode>NoError</m:ResponseCode>
          <m:Items><t:Message><t:ItemId Id="msg-2" ChangeKey="ck-2" /></t:Message></m:Items>
        </m:GetItemResponseMessage>
      </m:ResponseMessages>
    </m:GetItemResponse>
  </soap:Body>
</soap:Envelope>`)
	}))
	defer server.Close()
	client, err := NewClient(Config{Endpoint: server.URL, Username: "ert-retry", Password: "secret"})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer client.Close()
	if _, err := client.GetMessages(t.Context(), []string{"msg-1"}); err != nil {
		t.Fatalf("first GetMessages() error: %v", err)
	}
	if _, err := client.GetMessages(t.Context(), []string{"msg-2"}); err != nil {
		t.Fatalf("second GetMessages() error: %v", err)
	}
	if got := requests.Load(); got != 3 {
		t.Fatalf("server saw %d requests, want 3 (1 priming + 1 rejected + 1 retry)", got)
	}
	if got := cookieSeen.Load(); got != 1 {
		t.Fatalf("requests carrying stale session cookie = %d, want 1", got)
	}
}
