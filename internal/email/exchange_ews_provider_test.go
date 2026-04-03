package email

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExchangeEWSDraftLifecycle(t *testing.T) {
	t.Helper()
	var actions []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		body, _ := io.ReadAll(r.Body)
		action := strings.Trim(r.Header.Get("SOAPAction"), `"`)
		actions = append(actions, action)
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		switch {
		case strings.HasSuffix(action, "/CreateItem"):
			if !strings.Contains(string(body), "MimeContent") {
				t.Fatalf("CreateItem body missing MimeContent: %s", string(body))
			}
			_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/" xmlns:m="http://schemas.microsoft.com/exchange/services/2006/messages" xmlns:t="http://schemas.microsoft.com/exchange/services/2006/types">
  <soap:Body>
    <m:CreateItemResponse>
      <m:ResponseMessages>
        <m:CreateItemResponseMessage ResponseClass="Success">
          <m:ResponseCode>NoError</m:ResponseCode>
          <m:Items>
            <t:Message>
              <t:ItemId Id="draft-1" ChangeKey="ck1" />
              <t:ConversationId Id="thread-1" />
              <t:Subject>Hello</t:Subject>
            </t:Message>
          </m:Items>
        </m:CreateItemResponseMessage>
      </m:ResponseMessages>
    </m:CreateItemResponse>
  </soap:Body>
</soap:Envelope>`)
		case strings.HasSuffix(action, "/UpdateItem"):
			if !strings.Contains(string(body), "MimeContent") {
				t.Fatalf("UpdateItem body missing MimeContent: %s", string(body))
			}
			_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/" xmlns:m="http://schemas.microsoft.com/exchange/services/2006/messages">
  <soap:Body>
    <m:UpdateItemResponse>
      <m:ResponseMessages>
        <m:UpdateItemResponseMessage ResponseClass="Success">
          <m:ResponseCode>NoError</m:ResponseCode>
        </m:UpdateItemResponseMessage>
      </m:ResponseMessages>
    </m:UpdateItemResponse>
  </soap:Body>
</soap:Envelope>`)
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
              <t:ItemId Id="draft-1" ChangeKey="ck2" />
              <t:ConversationId Id="thread-1" />
              <t:Subject>Hello updated</t:Subject>
            </t:Message>
          </m:Items>
        </m:GetItemResponseMessage>
      </m:ResponseMessages>
    </m:GetItemResponse>
  </soap:Body>
</soap:Envelope>`)
		case strings.HasSuffix(action, "/SendItem"):
			_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/" xmlns:m="http://schemas.microsoft.com/exchange/services/2006/messages">
  <soap:Body>
    <m:SendItemResponse>
      <m:ResponseMessages>
        <m:SendItemResponseMessage ResponseClass="Success">
          <m:ResponseCode>NoError</m:ResponseCode>
        </m:SendItemResponseMessage>
      </m:ResponseMessages>
    </m:SendItemResponse>
  </soap:Body>
</soap:Envelope>`)
		default:
			t.Fatalf("unexpected SOAP action %q body=%s", action, string(body))
		}
	}))
	defer server.Close()

	provider, err := NewExchangeEWSMailProvider(ExchangeEWSConfig{
		Endpoint: server.URL,
		Username: "ert",
		Password: "secret",
	})
	if err != nil {
		t.Fatalf("NewExchangeEWSMailProvider() error: %v", err)
	}
	defer provider.Close()

	draft, err := provider.CreateDraft(t.Context(), DraftInput{
		From:    "ert@example.test",
		To:      []string{"alice@example.test"},
		Subject: "Hello",
		Body:    "Body",
	})
	if err != nil {
		t.Fatalf("CreateDraft() error: %v", err)
	}
	if draft.ID != "draft-1" || draft.ThreadID != "thread-1" {
		t.Fatalf("draft = %#v", draft)
	}

	updated, err := provider.UpdateDraft(t.Context(), draft.ID, DraftInput{
		From:    "ert@example.test",
		To:      []string{"alice@example.test"},
		Subject: "Hello updated",
		Body:    "Body updated",
	})
	if err != nil {
		t.Fatalf("UpdateDraft() error: %v", err)
	}
	if updated.ID != "draft-1" || updated.ThreadID != "thread-1" {
		t.Fatalf("updated draft = %#v", updated)
	}

	if err := provider.SendDraft(t.Context(), draft.ID, DraftInput{
		From:    "ert@example.test",
		To:      []string{"alice@example.test"},
		Subject: "Hello updated",
		Body:    "Body updated",
	}); err != nil {
		t.Fatalf("SendDraft() error: %v", err)
	}

	want := []string{
		"http://schemas.microsoft.com/exchange/services/2006/messages/CreateItem",
		"http://schemas.microsoft.com/exchange/services/2006/messages/UpdateItem",
		"http://schemas.microsoft.com/exchange/services/2006/messages/GetItem",
		"http://schemas.microsoft.com/exchange/services/2006/messages/UpdateItem",
		"http://schemas.microsoft.com/exchange/services/2006/messages/GetItem",
		"http://schemas.microsoft.com/exchange/services/2006/messages/SendItem",
	}
	if strings.Join(actions, "\n") != strings.Join(want, "\n") {
		t.Fatalf("actions = %#v, want %#v", actions, want)
	}
}

func TestExchangeEWSGetAttachmentMapsContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		body, _ := io.ReadAll(r.Body)
		action := strings.Trim(r.Header.Get("SOAPAction"), `"`)
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		if !strings.HasSuffix(action, "/GetAttachment") {
			t.Fatalf("unexpected SOAP action %q body=%s", action, string(body))
		}
		if !strings.Contains(string(body), `AttachmentId Id="att-1"`) {
			t.Fatalf("GetAttachment body missing attachment id: %s", string(body))
		}
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
              <t:Name>Datenblatt UNI BJ2025.xlsx</t:Name>
              <t:ContentType>application/vnd.openxmlformats-officedocument.spreadsheetml.sheet</t:ContentType>
              <t:Size>10</t:Size>
              <t:IsInline>false</t:IsInline>
              <t:Content>c2hlZXRieXRlcw==</t:Content>
            </t:FileAttachment>
          </m:Attachments>
        </m:GetAttachmentResponseMessage>
      </m:ResponseMessages>
    </m:GetAttachmentResponse>
  </soap:Body>
</soap:Envelope>`)
	}))
	defer server.Close()

	provider, err := NewExchangeEWSMailProvider(ExchangeEWSConfig{
		Endpoint: server.URL,
		Username: "ert",
		Password: "secret",
	})
	if err != nil {
		t.Fatalf("NewExchangeEWSMailProvider() error: %v", err)
	}
	defer provider.Close()

	attachment, err := provider.GetAttachment(t.Context(), "msg-1", "att-1")
	if err != nil {
		t.Fatalf("GetAttachment() error: %v", err)
	}
	if attachment.ID != "att-1" {
		t.Fatalf("attachment id = %q, want att-1", attachment.ID)
	}
	if attachment.Filename != "Datenblatt UNI BJ2025.xlsx" {
		t.Fatalf("attachment filename = %q", attachment.Filename)
	}
	if string(attachment.Content) != "sheetbytes" {
		t.Fatalf("attachment content = %q, want sheetbytes", string(attachment.Content))
	}
}

func TestExchangeEWSDisplayFolderNameNormalizesArchiveDisplay(t *testing.T) {
	tests := map[string]string{
		"Archive":               "",
		"Archive/padova2023":    "padova2023",
		`Archive\simons24`:      "simons24",
		"Posteingang":           "Posteingang",
		"Archive/work/projectA": "projectA",
	}
	for input, want := range tests {
		if got := exchangeEWSDisplayFolderName(input); got != want {
			t.Fatalf("exchangeEWSDisplayFolderName(%q) = %q, want %q", input, got, want)
		}
	}
}

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

	provider, err := NewExchangeEWSMailProvider(ExchangeEWSConfig{
		Endpoint: server.URL,
		Username: "ert",
		Password: "secret",
	})
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

	provider, err := NewExchangeEWSMailProvider(ExchangeEWSConfig{
		Endpoint: server.URL,
		Username: "ert",
		Password: "secret",
	})
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

	provider, err := NewExchangeEWSMailProvider(ExchangeEWSConfig{
		Endpoint: server.URL,
		Username: "ert",
		Password: "secret",
	})
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

	provider, err := NewExchangeEWSMailProvider(ExchangeEWSConfig{
		Endpoint: server.URL,
		Username: "ert",
		Password: "secret",
	})
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
