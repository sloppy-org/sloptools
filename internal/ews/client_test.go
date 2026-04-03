package ews

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestClientUpdateInboxRulesBuildsOperations(t *testing.T) {
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		data, _ := io.ReadAll(r.Body)
		body = string(data)
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/" xmlns:m="http://schemas.microsoft.com/exchange/services/2006/messages">
  <soap:Body>
    <m:UpdateInboxRulesResponse>
      <m:ResponseCode>NoError</m:ResponseCode>
    </m:UpdateInboxRulesResponse>
  </soap:Body>
</soap:Envelope>`)
	}))
	defer server.Close()

	client, err := NewClient(Config{
		Endpoint: server.URL,
		Username: "ert",
		Password: "secret",
	})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer client.Close()

	err = client.UpdateInboxRules(t.Context(), []RuleOperation{
		{
			Kind: RuleOperationCreate,
			Rule: Rule{
				Name:     "Move project mail",
				Priority: 1,
				Enabled:  true,
				Conditions: RuleConditions{
					ContainsSubjectStrings: []string{"project"},
				},
				Actions: RuleActions{
					MoveToFolderID:      "inbox",
					StopProcessingRules: true,
				},
			},
		},
		{
			Kind: RuleOperationDelete,
			Rule: Rule{ID: "rule-1"},
		},
	})
	if err != nil {
		t.Fatalf("UpdateInboxRules() error: %v", err)
	}

	for _, snippet := range []string{
		"<m:UpdateInboxRules>",
		"<t:CreateRuleOperation>",
		"<t:ContainsSubjectStrings><t:String>project</t:String></t:ContainsSubjectStrings>",
		"<t:MoveToFolder><t:DistinguishedFolderId Id=\"inbox\" /></t:MoveToFolder>",
		"<t:DeleteRuleOperation><t:RuleId>rule-1</t:RuleId></t:DeleteRuleOperation>",
	} {
		if !strings.Contains(body, snippet) {
			t.Fatalf("request body missing %q:\n%s", snippet, body)
		}
	}
}

func TestClientGetMessagesSanitizesIllegalXMLCharacters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = io.WriteString(w, "<?xml version=\"1.0\" encoding=\"utf-8\"?>\n"+
			"<soap:Envelope xmlns:soap=\"http://schemas.xmlsoap.org/soap/envelope/\" xmlns:m=\"http://schemas.microsoft.com/exchange/services/2006/messages\" xmlns:t=\"http://schemas.microsoft.com/exchange/services/2006/types\">\n"+
			"  <soap:Body>\n"+
			"    <m:GetItemResponse>\n"+
			"      <m:ResponseMessages>\n"+
			"        <m:GetItemResponseMessage>\n"+
			"          <m:ResponseCode>NoError</m:ResponseCode>\n"+
			"          <m:Items>\n"+
			"            <t:Message>\n"+
			"              <t:ItemId Id=\"msg-1\" ChangeKey=\"ck-1\" />\n"+
			"              <t:ParentFolderId Id=\"inbox\" ChangeKey=\"fold-1\" />\n"+
			"              <t:ConversationId Id=\"thread-1\" ChangeKey=\"conv-1\" />\n"+
			"              <t:Subject>Hello\x1b World</t:Subject>\n"+
			"              <t:Body BodyType=\"Text\">Body\x1b text</t:Body>\n"+
			"              <t:DateTimeReceived>2026-03-16T14:00:00Z</t:DateTimeReceived>\n"+
			"              <t:IsRead>false</t:IsRead>\n"+
			"            </t:Message>\n"+
			"          </m:Items>\n"+
			"        </m:GetItemResponseMessage>\n"+
			"      </m:ResponseMessages>\n"+
			"    </m:GetItemResponse>\n"+
			"  </soap:Body>\n"+
			"</soap:Envelope>")
	}))
	defer server.Close()

	client, err := NewClient(Config{
		Endpoint: server.URL,
		Username: "ert",
		Password: "secret",
	})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer client.Close()

	messages, err := client.GetMessages(t.Context(), []string{"msg-1"})
	if err != nil {
		t.Fatalf("GetMessages() error: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1", len(messages))
	}
	if messages[0].Subject != "Hello World" {
		t.Fatalf("Subject = %q, want %q", messages[0].Subject, "Hello World")
	}
	if messages[0].Body != "Body text" {
		t.Fatalf("Body = %q, want %q", messages[0].Body, "Body text")
	}
}

func TestClientGetMessagesSanitizesIllegalXMLCharacterReferences(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = io.WriteString(w, "<?xml version=\"1.0\" encoding=\"utf-8\"?>\n"+
			"<soap:Envelope xmlns:soap=\"http://schemas.xmlsoap.org/soap/envelope/\" xmlns:m=\"http://schemas.microsoft.com/exchange/services/2006/messages\" xmlns:t=\"http://schemas.microsoft.com/exchange/services/2006/types\">\n"+
			"  <soap:Body>\n"+
			"    <m:GetItemResponse>\n"+
			"      <m:ResponseMessages>\n"+
			"        <m:GetItemResponseMessage>\n"+
			"          <m:ResponseCode>NoError</m:ResponseCode>\n"+
			"          <m:Items>\n"+
			"            <t:Message>\n"+
			"              <t:ItemId Id=\"msg-1\" ChangeKey=\"ck-1\" />\n"+
			"              <t:ParentFolderId Id=\"inbox\" ChangeKey=\"fold-1\" />\n"+
			"              <t:ConversationId Id=\"thread-1\" ChangeKey=\"conv-1\" />\n"+
			"              <t:Subject>Hello&#x1B; World</t:Subject>\n"+
			"              <t:Body BodyType=\"Text\">Body&#27; text</t:Body>\n"+
			"              <t:DateTimeReceived>2026-03-16T14:00:00Z</t:DateTimeReceived>\n"+
			"              <t:IsRead>false</t:IsRead>\n"+
			"            </t:Message>\n"+
			"          </m:Items>\n"+
			"        </m:GetItemResponseMessage>\n"+
			"      </m:ResponseMessages>\n"+
			"    </m:GetItemResponse>\n"+
			"  </soap:Body>\n"+
			"</soap:Envelope>")
	}))
	defer server.Close()

	client, err := NewClient(Config{
		Endpoint: server.URL,
		Username: "ert",
		Password: "secret",
	})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer client.Close()

	messages, err := client.GetMessages(t.Context(), []string{"msg-1"})
	if err != nil {
		t.Fatalf("GetMessages() error: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1", len(messages))
	}
	if messages[0].Subject != "Hello World" {
		t.Fatalf("Subject = %q, want %q", messages[0].Subject, "Hello World")
	}
	if messages[0].Body != "Body text" {
		t.Fatalf("Body = %q, want %q", messages[0].Body, "Body text")
	}
}

func TestClientGetMessageSummariesRequestsMetadataShape(t *testing.T) {
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		data, _ := io.ReadAll(r.Body)
		body = string(data)
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/" xmlns:m="http://schemas.microsoft.com/exchange/services/2006/messages" xmlns:t="http://schemas.microsoft.com/exchange/services/2006/types">
  <soap:Body>
    <m:GetItemResponse>
      <m:ResponseMessages>
        <m:GetItemResponseMessage ResponseClass="Success">
          <m:ResponseCode>NoError</m:ResponseCode>
          <m:Items>
            <t:Message>
              <t:ItemId Id="msg-1" ChangeKey="ck-1" />
              <t:ParentFolderId Id="inbox" ChangeKey="fold-1" />
              <t:ConversationId Id="thread-1" ChangeKey="conv-1" />
              <t:Subject>Hello World</t:Subject>
              <t:DateTimeReceived>2026-03-16T14:00:00Z</t:DateTimeReceived>
              <t:IsRead>false</t:IsRead>
            </t:Message>
          </m:Items>
        </m:GetItemResponseMessage>
      </m:ResponseMessages>
    </m:GetItemResponse>
  </soap:Body>
</soap:Envelope>`)
	}))
	defer server.Close()

	client, err := NewClient(Config{
		Endpoint: server.URL,
		Username: "ert",
		Password: "secret",
	})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer client.Close()

	messages, err := client.GetMessageSummaries(t.Context(), []string{"msg-1"})
	if err != nil {
		t.Fatalf("GetMessageSummaries() error: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1", len(messages))
	}
	if messages[0].Body != "" {
		t.Fatalf("Body = %q, want empty summary body", messages[0].Body)
	}
	if !strings.Contains(body, `<t:BaseShape>AllProperties</t:BaseShape>`) {
		t.Fatalf("request body missing AllProperties base shape: %s", body)
	}
	if !strings.Contains(body, `<t:BodyType>Text</t:BodyType>`) {
		t.Fatalf("request body missing text body selection: %s", body)
	}
	if strings.Contains(body, `<t:AdditionalProperties>`) {
		t.Fatalf("request body unexpectedly used additional properties: %s", body)
	}
}

func TestClientGetAttachmentDecodesContent(t *testing.T) {
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		data, _ := io.ReadAll(r.Body)
		body = string(data)
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/" xmlns:m="http://schemas.microsoft.com/exchange/services/2006/messages" xmlns:t="http://schemas.microsoft.com/exchange/services/2006/types">
  <soap:Body>
    <m:GetAttachmentResponse>
      <m:ResponseMessages>
        <m:GetAttachmentResponseMessage ResponseClass="Success">
          <m:ResponseCode>NoError</m:ResponseCode>
          <m:Attachments>
            <t:FileAttachment>
              <t:AttachmentId Id="att-1" />
              <t:Name>report.txt</t:Name>
              <t:ContentType>text/plain</t:ContentType>
              <t:Size>5</t:Size>
              <t:IsInline>false</t:IsInline>
              <t:Content>aGVsbG8=</t:Content>
            </t:FileAttachment>
          </m:Attachments>
        </m:GetAttachmentResponseMessage>
      </m:ResponseMessages>
    </m:GetAttachmentResponse>
  </soap:Body>
</soap:Envelope>`)
	}))
	defer server.Close()

	client, err := NewClient(Config{
		Endpoint: server.URL,
		Username: "ert",
		Password: "secret",
	})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer client.Close()

	attachment, err := client.GetAttachment(t.Context(), "att-1")
	if err != nil {
		t.Fatalf("GetAttachment() error: %v", err)
	}
	if attachment.ID != "att-1" {
		t.Fatalf("attachment id = %q, want att-1", attachment.ID)
	}
	if attachment.Name != "report.txt" {
		t.Fatalf("attachment name = %q, want report.txt", attachment.Name)
	}
	if string(attachment.Content) != "hello" {
		t.Fatalf("attachment content = %q, want hello", string(attachment.Content))
	}
	if !strings.Contains(body, `<m:GetAttachment>`) || !strings.Contains(body, `<t:AttachmentId Id="att-1" />`) {
		t.Fatalf("request body missing attachment id: %s", body)
	}
}

func TestClientMoveItemsReturnsResolvedIDs(t *testing.T) {
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		data, _ := io.ReadAll(r.Body)
		body = string(data)
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/" xmlns:m="http://schemas.microsoft.com/exchange/services/2006/messages" xmlns:t="http://schemas.microsoft.com/exchange/services/2006/types">
  <soap:Body>
    <m:MoveItemResponse>
      <m:ResponseMessages>
        <m:MoveItemResponseMessage ResponseClass="Success">
          <m:ResponseCode>NoError</m:ResponseCode>
          <m:Items>
            <t:Message><t:ItemId Id="m1-new" ChangeKey="ck1" /></t:Message>
          </m:Items>
        </m:MoveItemResponseMessage>
        <m:MoveItemResponseMessage ResponseClass="Success">
          <m:ResponseCode>NoError</m:ResponseCode>
          <m:Items>
            <t:Message><t:ItemId Id="m2-new" ChangeKey="ck2" /></t:Message>
          </m:Items>
        </m:MoveItemResponseMessage>
      </m:ResponseMessages>
    </m:MoveItemResponse>
  </soap:Body>
</soap:Envelope>`)
	}))
	defer server.Close()

	client, err := NewClient(Config{
		Endpoint: server.URL,
		Username: "ert",
		Password: "secret",
	})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer client.Close()

	ids, err := client.MoveItems(t.Context(), []string{"m1", "m2"}, "deleteditems")
	if err != nil {
		t.Fatalf("MoveItems() error: %v", err)
	}
	if strings.Join(ids, ",") != "m1-new,m2-new" {
		t.Fatalf("resolved ids = %v, want [m1-new m2-new]", ids)
	}
	if !strings.Contains(body, `<t:DistinguishedFolderId Id="deleteditems" />`) {
		t.Fatalf("MoveItem body missing deleteditems folder: %s", body)
	}
}

func TestClientCallParsesServerBusyBackoffFault(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>
<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/">
  <s:Body>
    <s:Fault>
      <faultcode xmlns:a="http://schemas.microsoft.com/exchange/services/2006/types">a:ErrorServerBusy</faultcode>
      <faultstring xml:lang="de-AT">The server cannot service this request right now. Try again later.</faultstring>
      <detail>
        <e:ResponseCode xmlns:e="http://schemas.microsoft.com/exchange/services/2006/errors">ErrorServerBusy</e:ResponseCode>
        <e:Message xmlns:e="http://schemas.microsoft.com/exchange/services/2006/errors">The server cannot service this request right now. Try again later.</e:Message>
        <t:MessageXml xmlns:t="http://schemas.microsoft.com/exchange/services/2006/types">
          <t:Value Name="BackOffMilliseconds">12345</t:Value>
        </t:MessageXml>
      </detail>
    </s:Fault>
  </s:Body>
</s:Envelope>`)
	}))
	defer server.Close()

	client, err := NewClient(Config{
		Endpoint: server.URL,
		Username: "ert",
		Password: "secret",
	})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer client.Close()

	err = client.UpdateInboxRules(t.Context(), nil)
	if err == nil {
		t.Fatalf("UpdateInboxRules() error = nil, want server busy error")
	}
	var backoffErr *BackoffError
	if !strings.Contains(err.Error(), "retry after") {
		t.Fatalf("error = %v, want retry-after message", err)
	}
	if got := err; got != nil {
		var ok bool
		backoffErr, ok = got.(*BackoffError)
		if !ok {
			t.Fatalf("error type = %T, want *BackoffError", err)
		}
	}
	if backoffErr.Backoff != 12345*time.Millisecond {
		t.Fatalf("Backoff = %v, want %v", backoffErr.Backoff, 12345*time.Millisecond)
	}
}

func TestClientSharesAffinityHeadersAndCookiesAcrossClients(t *testing.T) {
	var (
		requests   int32
		cookieSeen atomic.Bool
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := atomic.AddInt32(&requests, 1)
		if got := strings.TrimSpace(r.Header.Get("X-AnchorMailbox")); got != "ert" {
			t.Fatalf("X-AnchorMailbox = %q, want ert", got)
		}
		if got := strings.TrimSpace(r.Header.Get("X-PreferServerAffinity")); got != "true" {
			t.Fatalf("X-PreferServerAffinity = %q, want true", got)
		}
		if strings.Contains(r.Header.Get("Cookie"), "X-BackEndCookie=sticky") {
			cookieSeen.Store(true)
		}
		if call == 1 {
			http.SetCookie(w, &http.Cookie{Name: "X-BackEndCookie", Value: "sticky", Path: "/"})
		}
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/" xmlns:m="http://schemas.microsoft.com/exchange/services/2006/messages" xmlns:t="http://schemas.microsoft.com/exchange/services/2006/types">
  <soap:Body>
    <m:GetItemResponse>
      <m:ResponseMessages>
        <m:GetItemResponseMessage ResponseClass="Success">
          <m:ResponseCode>NoError</m:ResponseCode>
          <m:Items>
            <t:Message><t:ItemId Id="msg-1" ChangeKey="ck-1" /></t:Message>
          </m:Items>
        </m:GetItemResponseMessage>
      </m:ResponseMessages>
    </m:GetItemResponse>
  </soap:Body>
</soap:Envelope>`)
	}))
	defer server.Close()

	first, err := NewClient(Config{Endpoint: server.URL, Username: "ert", Password: "secret"})
	if err != nil {
		t.Fatalf("NewClient(first) error: %v", err)
	}
	defer first.Close()
	second, err := NewClient(Config{Endpoint: server.URL, Username: "ert", Password: "secret"})
	if err != nil {
		t.Fatalf("NewClient(second) error: %v", err)
	}
	defer second.Close()

	if _, err := first.GetMessages(t.Context(), []string{"msg-1"}); err != nil {
		t.Fatalf("first GetMessages() error: %v", err)
	}
	if _, err := second.GetMessages(t.Context(), []string{"msg-1"}); err != nil {
		t.Fatalf("second GetMessages() error: %v", err)
	}
	if !cookieSeen.Load() {
		t.Fatal("second request did not reuse backend affinity cookie")
	}
}

func TestClientListFoldersUsesSharedCache(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
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
	}))
	defer server.Close()

	first, err := NewClient(Config{Endpoint: server.URL, Username: "ert", Password: "secret"})
	if err != nil {
		t.Fatalf("NewClient(first) error: %v", err)
	}
	defer first.Close()
	second, err := NewClient(Config{Endpoint: server.URL, Username: "ert", Password: "secret"})
	if err != nil {
		t.Fatalf("NewClient(second) error: %v", err)
	}
	defer second.Close()

	if _, err := first.ListFolders(t.Context()); err != nil {
		t.Fatalf("first ListFolders() error: %v", err)
	}
	if _, err := second.ListFolders(t.Context()); err != nil {
		t.Fatalf("second ListFolders() error: %v", err)
	}
	if requests.Load() != 1 {
		t.Fatalf("requests = %d, want 1 cached folder request", requests.Load())
	}
}

func TestClientSerializesShortRequestsPerMailbox(t *testing.T) {
	var (
		current       int32
		maxConcurrent int32
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		active := atomic.AddInt32(&current, 1)
		for {
			max := atomic.LoadInt32(&maxConcurrent)
			if active <= max || atomic.CompareAndSwapInt32(&maxConcurrent, max, active) {
				break
			}
		}
		time.Sleep(75 * time.Millisecond)
		atomic.AddInt32(&current, -1)
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/" xmlns:m="http://schemas.microsoft.com/exchange/services/2006/messages" xmlns:t="http://schemas.microsoft.com/exchange/services/2006/types">
  <soap:Body>
    <m:GetItemResponse>
      <m:ResponseMessages>
        <m:GetItemResponseMessage ResponseClass="Success">
          <m:ResponseCode>NoError</m:ResponseCode>
          <m:Items>
            <t:Message><t:ItemId Id="msg-1" ChangeKey="ck-1" /></t:Message>
          </m:Items>
        </m:GetItemResponseMessage>
      </m:ResponseMessages>
    </m:GetItemResponse>
  </soap:Body>
</soap:Envelope>`)
	}))
	defer server.Close()

	first, err := NewClient(Config{Endpoint: server.URL, Username: "ert", Password: "secret"})
	if err != nil {
		t.Fatalf("NewClient(first) error: %v", err)
	}
	defer first.Close()
	second, err := NewClient(Config{Endpoint: server.URL, Username: "ert", Password: "secret"})
	if err != nil {
		t.Fatalf("NewClient(second) error: %v", err)
	}
	defer second.Close()

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, client := range []*Client{first, second} {
		wg.Add(1)
		go func(client *Client) {
			defer wg.Done()
			_, err := client.GetMessages(t.Context(), []string{"msg-1"})
			errs <- err
		}(client)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("GetMessages() error: %v", err)
		}
	}
	if atomic.LoadInt32(&maxConcurrent) != 1 {
		t.Fatalf("max concurrent requests = %d, want 1", atomic.LoadInt32(&maxConcurrent))
	}
}

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

	client, err := NewClient(Config{
		Endpoint: server.URL,
		Username: "ert",
		Password: "secret",
	})
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
	for _, snippet := range []string{
		"<m:SyncFolderItems>",
		"<m:SyncState>state-1</m:SyncState>",
		"<m:MaxChangesReturned>200</m:MaxChangesReturned>",
	} {
		if !strings.Contains(body, snippet) {
			t.Fatalf("request body missing %q:\n%s", snippet, body)
		}
	}
}
