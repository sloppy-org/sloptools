package email

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"regexp"
	"strings"
	"testing"
)

func TestNormalizeDraftInputAllowsBlankDrafts(t *testing.T) {
	normalized, err := NormalizeDraftInput(DraftInput{
		Subject: "  Draft subject  ",
		Body:    "draft body\r\n",
	})
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
	normalized, err := NormalizeDraftInput(DraftInput{
		To:      []string{"Client <client@example.com>"},
		Subject: "Test",
	})
	if err != nil {
		t.Fatalf("NormalizeDraftInput() error: %v", err)
	}
	if len(normalized.To) != 1 || normalized.To[0] != "client@example.com" {
		t.Fatalf("To = %#v, want [client@example.com]", normalized.To)
	}
}

func TestNormalizeDraftAddressesNameWithComma(t *testing.T) {
	normalized, err := NormalizeDraftInput(DraftInput{
		To:      []string{`"Smith, John" <john@example.com>`},
		Subject: "Test",
	})
	if err != nil {
		t.Fatalf("NormalizeDraftInput() error: %v", err)
	}
	if len(normalized.To) != 1 || normalized.To[0] != "john@example.com" {
		t.Fatalf("To = %#v, want [john@example.com]", normalized.To)
	}
}

func TestNormalizeDraftAddressesMultipleInOneString(t *testing.T) {
	normalized, err := NormalizeDraftInput(DraftInput{
		To:      []string{"alice@example.com, Bob <bob@example.com>"},
		Subject: "Test",
	})
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
	normalized, err := NormalizeDraftInput(DraftInput{
		To:      []string{"Alice <alice@example.com>", "alice@example.com"},
		Subject: "Test",
	})
	if err != nil {
		t.Fatalf("NormalizeDraftInput() error: %v", err)
	}
	if len(normalized.To) != 1 || normalized.To[0] != "alice@example.com" {
		t.Fatalf("To = %#v, want [alice@example.com]", normalized.To)
	}
}

func TestBuildRFC822MessagePlainTextByDefault(t *testing.T) {
	raw, err := buildRFC822Message(DraftInput{
		From:    "albert@tugraz.at",
		To:      []string{"alice@example.com"},
		Subject: "Hello",
		Body:    "first line\nsecond line",
	})
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
	raw, err := buildRFC822Message(DraftInput{
		From:    "albert@tugraz.at",
		To:      []string{"alice@example.com"},
		Subject: "Report",
		Body:    "See attached.",
		Attachments: []DraftAttachment{
			{Filename: "report.zip", ContentType: "application/zip", Content: payload},
			{Filename: "notes.txt", Content: []byte("plain notes")},
		},
	})
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
		parts = append(parts, capturedPart{
			headers: part.Header,
			body:    bodyBytes,
		})
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
