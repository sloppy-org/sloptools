package email

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"regexp"
	"strings"
	"testing"
)

func TestNormalizeDraftInputAllowsBlankDrafts(t *testing.T) {
	normalized, err := NormalizeDraftInput(DraftInput{Subject: "  Draft subject  ", Body: "draft body\r\n"})
	if err != nil {
		t.Fatalf("NormalizeDraftInput() error: %v", err)
	}
	if normalized.Subject != "Draft subject" {
		t.Fatalf("subject = %q, want Draft subject", normalized.Subject)
	}
	if normalized.Body != "draft body" {
		t.Fatalf("body = %q, want draft body", normalized.Body)
	}
}

func TestNormalizeDraftSendInputRequiresRecipient(t *testing.T) {
	if _, err := NormalizeDraftSendInput(DraftInput{Subject: "Draft"}); err == nil {
		t.Fatal("NormalizeDraftSendInput() error = nil, want recipient validation")
	}
}

func TestNormalizeDraftAddressesNameWithAngleBrackets(t *testing.T) {
	normalized, err := NormalizeDraftInput(DraftInput{To: []string{"Client <client@example.com>"}, Subject: "Test"})
	if err != nil {
		t.Fatalf("NormalizeDraftInput() error: %v", err)
	}
	if len(normalized.To) != 1 || normalized.To[0] != "client@example.com" {
		t.Fatalf("To = %#v, want [client@example.com]", normalized.To)
	}
}

func TestNormalizeDraftAddressesNameWithComma(t *testing.T) {
	normalized, err := NormalizeDraftInput(DraftInput{To: []string{`"Smith, John" <john@example.com>`}, Subject: "Test"})
	if err != nil {
		t.Fatalf("NormalizeDraftInput() error: %v", err)
	}
	if len(normalized.To) != 1 || normalized.To[0] != "john@example.com" {
		t.Fatalf("To = %#v, want [john@example.com]", normalized.To)
	}
}

func TestNormalizeDraftAddressesMultipleInOneString(t *testing.T) {
	normalized, err := NormalizeDraftInput(DraftInput{To: []string{"alice@example.com, Bob <bob@example.com>"}, Subject: "Test"})
	if err != nil {
		t.Fatalf("NormalizeDraftInput() error: %v", err)
	}
	if len(normalized.To) != 2 {
		t.Fatalf("To = %#v, want 2 addresses", normalized.To)
	}
	if normalized.To[0] != "alice@example.com" || normalized.To[1] != "bob@example.com" {
		t.Fatalf("To = %#v, want [alice@example.com bob@example.com]", normalized.To)
	}
}

func TestNormalizeDraftAddressesDeduplicates(t *testing.T) {
	normalized, err := NormalizeDraftInput(DraftInput{To: []string{"Alice <alice@example.com>", "alice@example.com"}, Subject: "Test"})
	if err != nil {
		t.Fatalf("NormalizeDraftInput() error: %v", err)
	}
	if len(normalized.To) != 1 || normalized.To[0] != "alice@example.com" {
		t.Fatalf("To = %#v, want [alice@example.com]", normalized.To)
	}
}

func TestBuildRFC822MessagePlainTextByDefault(t *testing.T) {
	raw, err := buildRFC822Message(DraftInput{From: "albert@tugraz.at", To: []string{"alice@example.com"}, Subject: "Hello", Body: "first line\nsecond line"})
	if err != nil {
		t.Fatalf("buildRFC822Message() error: %v", err)
	}
	headers, body, err := splitMIMEHeader(raw)
	if err != nil {
		t.Fatalf("split MIME: %v", err)
	}
	if ct := headers.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type = %q, want text/plain...", ct)
	}
	if headers.Get("MIME-Version") != "1.0" {
		t.Fatalf("MIME-Version must be 1.0, got %q", headers.Get("MIME-Version"))
	}
	if !strings.Contains(string(body), "first line\r\nsecond line") {
		t.Fatalf("body should preserve lines with CRLF, got: %q", body)
	}
}

func TestBuildRFC822MessageWithAttachmentsIsMultipart(t *testing.T) {
	payload := []byte("PK\x03\x04fake-zip-body")
	raw, err := buildRFC822Message(DraftInput{From: "albert@tugraz.at", To: []string{"alice@example.com"}, Subject: "Report", Body: "See attached.", Attachments: []DraftAttachment{{Filename: "report.zip", ContentType: "application/zip", Content: payload}, {Filename: "notes.txt", Content: []byte("plain notes")}}})
	if err != nil {
		t.Fatalf("buildRFC822Message() error: %v", err)
	}
	headers, body, err := splitMIMEHeader(raw)
	if err != nil {
		t.Fatalf("split MIME: %v", err)
	}
	mediaType, params, err := mime.ParseMediaType(headers.Get("Content-Type"))
	if err != nil {
		t.Fatalf("parse Content-Type: %v", err)
	}
	if mediaType != "multipart/mixed" {
		t.Fatalf("Content-Type = %q, want multipart/mixed", mediaType)
	}
	boundary := params["boundary"]
	if boundary == "" {
		t.Fatalf("missing multipart boundary")
	}
	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	type capturedPart struct {
		headers map[string][]string
		body    []byte
	}
	var parts []capturedPart
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read part: %v", err)
		}
		bodyBytes, err := io.ReadAll(part)
		if err != nil {
			t.Fatalf("read part body: %v", err)
		}
		parts = append(parts, capturedPart{headers: part.Header, body: bodyBytes})
		_ = part.Close()
	}
	if len(parts) != 3 {
		t.Fatalf("expected 3 MIME parts (body + 2 attachments), got %d", len(parts))
	}
	textCT, _, _ := mime.ParseMediaType(partHeader(parts[0].headers, "Content-Type"))
	if textCT != "text/plain" {
		t.Fatalf("first part Content-Type = %q, want text/plain", textCT)
	}
	if !strings.Contains(string(parts[0].body), "See attached.") {
		t.Fatalf("first part missing body text, got %q", parts[0].body)
	}
	zip := parts[1]
	if zipCT := partHeader(zip.headers, "Content-Type"); !strings.HasPrefix(zipCT, "application/zip") {
		t.Fatalf("attachment 1 Content-Type = %q, want application/zip", zipCT)
	}
	if dispo := partHeader(zip.headers, "Content-Disposition"); !strings.Contains(dispo, "attachment") {
		t.Fatalf("attachment 1 Content-Disposition = %q", dispo)
	}
	if enc := partHeader(zip.headers, "Content-Transfer-Encoding"); enc != "base64" {
		t.Fatalf("attachment encoding = %q, want base64", enc)
	}
	decoded, err := base64.StdEncoding.DecodeString(regexp.MustCompile(`\s+`).ReplaceAllString(string(zip.body), ""))
	if err != nil {
		t.Fatalf("decode attachment base64: %v", err)
	}
	if !bytes.Equal(decoded, payload) {
		t.Fatalf("decoded attachment content mismatch: got %q want %q", decoded, payload)
	}
	defaultPart := parts[2]
	if ct := partHeader(defaultPart.headers, "Content-Type"); !strings.HasPrefix(ct, "application/octet-stream") {
		t.Fatalf("default attachment Content-Type = %q, want application/octet-stream", ct)
	}
}

func partHeader(headers map[string][]string, key string) string {
	vs := headers[key]
	if len(vs) == 0 {
		return ""
	}
	return vs[0]
}

func splitMIMEHeader(raw []byte) (mail.Header, []byte, error) {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return nil, nil, err
	}
	body, err := io.ReadAll(msg.Body)
	if err != nil {
		return nil, nil, err
	}
	return msg.Header, body, nil
}

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
	provider, err := NewExchangeEWSMailProvider(ExchangeEWSConfig{Endpoint: server.URL, Username: "ert", Password: "secret"})
	if err != nil {
		t.Fatalf("NewExchangeEWSMailProvider() error: %v", err)
	}
	defer provider.Close()
	draft, err := provider.CreateDraft(t.Context(), DraftInput{From: "ert@example.test", To: []string{"alice@example.test"}, Subject: "Hello", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateDraft() error: %v", err)
	}
	if draft.ID != "draft-1" || draft.ThreadID != "thread-1" {
		t.Fatalf("draft = %#v", draft)
	}
	updated, err := provider.UpdateDraft(t.Context(), draft.ID, DraftInput{From: "ert@example.test", To: []string{"alice@example.test"}, Subject: "Hello updated", Body: "Body updated"})
	if err != nil {
		t.Fatalf("UpdateDraft() error: %v", err)
	}
	if updated.ID != "draft-1" || updated.ThreadID != "thread-1" {
		t.Fatalf("updated draft = %#v", updated)
	}
	if err := provider.SendDraft(t.Context(), draft.ID, DraftInput{From: "ert@example.test", To: []string{"alice@example.test"}, Subject: "Hello updated", Body: "Body updated"}); err != nil {
		t.Fatalf("SendDraft() error: %v", err)
	}
	want := []string{"http://schemas.microsoft.com/exchange/services/2006/messages/CreateItem", "http://schemas.microsoft.com/exchange/services/2006/messages/UpdateItem", "http://schemas.microsoft.com/exchange/services/2006/messages/GetItem", "http://schemas.microsoft.com/exchange/services/2006/messages/UpdateItem", "http://schemas.microsoft.com/exchange/services/2006/messages/GetItem", "http://schemas.microsoft.com/exchange/services/2006/messages/SendItem"}
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
	provider, err := NewExchangeEWSMailProvider(ExchangeEWSConfig{Endpoint: server.URL, Username: "ert", Password: "secret"})
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
	tests := map[string]string{"Archive": "", "Archive/padova2023": "padova2023", `Archive\simons24`: "simons24", "Posteingang": "Posteingang", "Archive/work/projectA": "projectA"}
	for input, want := range tests {
		if got := exchangeEWSDisplayFolderName(input); got != want {
			t.Fatalf("exchangeEWSDisplayFolderName(%q) = %q, want %q", input, got, want)
		}
	}
}
