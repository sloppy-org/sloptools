package mcp

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"mime"
	"mime/multipart"
	"strings"
	"testing"
	"time"

	"github.com/sloppy-org/sloptools/internal/email"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/store"
)

type fakeDraftMailProvider struct {
	fakeMailProvider
	lastDraft    email.DraftInput
	lastDraftID  string
	lastSendID   string
	lastRawMIME  []byte
	sentInput    email.DraftInput
	createCalls  int
	sendCalls    int
	createDraft  email.Draft
}

func (p *fakeDraftMailProvider) CreateDraft(_ context.Context, input email.DraftInput) (email.Draft, error) {
	p.createCalls++
	p.lastDraft = input
	raw, err := email.ExportRFC822ForTest(input)
	if err != nil {
		return email.Draft{}, err
	}
	p.lastRawMIME = raw
	if p.createDraft.ID == "" {
		p.createDraft = email.Draft{ID: "draft-1", ThreadID: "thread-1"}
	}
	p.lastDraftID = p.createDraft.ID
	return p.createDraft, nil
}

func (p *fakeDraftMailProvider) CreateReplyDraft(_ context.Context, _ string, _ email.DraftInput) (email.Draft, error) {
	return email.Draft{}, fmt.Errorf("not used in tests")
}

func (p *fakeDraftMailProvider) UpdateDraft(_ context.Context, draftID string, input email.DraftInput) (email.Draft, error) {
	p.lastDraft = input
	return email.Draft{ID: draftID, ThreadID: p.createDraft.ThreadID}, nil
}

func (p *fakeDraftMailProvider) SendDraft(_ context.Context, draftID string, input email.DraftInput) error {
	p.sendCalls++
	p.lastSendID = draftID
	p.sentInput = input
	return nil
}

func setupComposeFixture(t *testing.T) (*Server, store.ExternalAccount, *fakeDraftMailProvider) {
	s, st, _ := newDomainServerForTest(t)
	account, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderExchangeEWS, "albert@tugraz.at", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	body := "Original question\nsecond line"
	now := time.Date(2026, time.April, 21, 10, 30, 0, 0, time.UTC)
	provider := &fakeDraftMailProvider{
		fakeMailProvider: fakeMailProvider{
			messages: map[string]*providerdata.EmailMessage{
				"m1": {
					ID:                "m1",
					ThreadID:          "thread-src",
					InternetMessageID: "<abc@gcc.gnu.org>",
					Subject:           "PR123 issue",
					Sender:            "Jane Dev <jane@gcc.gnu.org>",
					Recipients:        []string{"gcc-patches@gcc.gnu.org", "albert@tugraz.at"},
					Date:              now,
					BodyText:          &body,
				},
			},
		},
	}
	s.newEmailProvider = func(context.Context, store.ExternalAccount) (email.EmailProvider, error) {
		return provider, nil
	}
	return s, account, provider
}

func TestMailSendDispatchBuildsMultipartAndSends(t *testing.T) {
	s, account, provider := setupComposeFixture(t)
	args := map[string]interface{}{
		"account_id": float64(account.ID),
		"to":         []interface{}{"alice@example.com"},
		"cc":         []interface{}{"Bob <bob@example.com>"},
		"subject":    "Test",
		"body":       "Hello there.",
		"attachments": []interface{}{
			map[string]interface{}{
				"filename":       "hello.txt",
				"content_base64": base64.StdEncoding.EncodeToString([]byte("file-content")),
				"content_type":   "text/plain",
			},
		},
	}
	out, err := s.callTool("mail_send", args)
	if err != nil {
		t.Fatalf("mail_send error: %v", err)
	}
	if out["sent"] != true {
		t.Fatalf("expected sent=true, got %v", out["sent"])
	}
	if out["draft_id"] == nil || out["draft_id"] == "" {
		t.Fatalf("missing draft_id in %#v", out)
	}
	if provider.createCalls != 1 || provider.sendCalls != 1 {
		t.Fatalf("createCalls=%d sendCalls=%d, want 1/1", provider.createCalls, provider.sendCalls)
	}
	mediaType, params, err := mime.ParseMediaType(extractContentType(provider.lastRawMIME))
	if err != nil {
		t.Fatalf("parse Content-Type: %v", err)
	}
	if mediaType != "multipart/mixed" {
		t.Fatalf("media type = %q, want multipart/mixed", mediaType)
	}
	reader := multipart.NewReader(bytes.NewReader(extractMIMEBody(provider.lastRawMIME)), params["boundary"])
	partCount := 0
	for {
		part, err := reader.NextPart()
		if err != nil {
			break
		}
		partCount++
		_ = part.Close()
	}
	if partCount != 2 {
		t.Fatalf("got %d MIME parts, want 2 (body + 1 attachment)", partCount)
	}
	provider.lastDraft.Attachments = nil // silence lint
}

func TestMailSendDraftOnlyDoesNotSend(t *testing.T) {
	s, account, provider := setupComposeFixture(t)
	out, err := s.callTool("mail_send", map[string]interface{}{
		"account_id": float64(account.ID),
		"to":         []interface{}{"alice@example.com"},
		"subject":    "Test",
		"body":       "Hello.",
		"draft_only": true,
	})
	if err != nil {
		t.Fatalf("mail_send error: %v", err)
	}
	if out["sent"] != false {
		t.Fatalf("expected sent=false, got %v", out["sent"])
	}
	if provider.sendCalls != 0 {
		t.Fatalf("sendCalls = %d, want 0", provider.sendCalls)
	}
}

func TestMailReplyBottomPostDefault(t *testing.T) {
	s, account, provider := setupComposeFixture(t)
	out, err := s.callTool("mail_reply", map[string]interface{}{
		"account_id": float64(account.ID),
		"message_id": "m1",
		"body":       "Confirmed, reverting.",
	})
	if err != nil {
		t.Fatalf("mail_reply error: %v", err)
	}
	reply, ok := out["reply"].(*MailComposeReplyInfo)
	if !ok {
		t.Fatalf("reply metadata missing or wrong type: %#v", out["reply"])
	}
	if reply.QuoteStyle != "bottom_post" {
		t.Fatalf("default quote style = %q, want bottom_post", reply.QuoteStyle)
	}
	if reply.InReplyTo != "<abc@gcc.gnu.org>" {
		t.Fatalf("In-Reply-To = %q, want <abc@gcc.gnu.org>", reply.InReplyTo)
	}
	if !strings.HasPrefix(provider.lastDraft.Subject, "Re:") {
		t.Fatalf("Subject must start with Re:, got %q", provider.lastDraft.Subject)
	}
	lines := strings.Split(strings.TrimRight(provider.lastDraft.Body, "\n"), "\n")
	foundQuote := false
	foundReplyAfter := false
	replyIdx := -1
	for i, line := range lines {
		if strings.HasPrefix(line, "> ") {
			foundQuote = true
		} else if foundQuote && strings.Contains(line, "Confirmed, reverting") {
			foundReplyAfter = true
			replyIdx = i
		}
	}
	if !foundQuote || !foundReplyAfter || replyIdx == -1 {
		t.Fatalf("reply body must have quote followed by new text; got %q", provider.lastDraft.Body)
	}
}

func TestMailReplyTopPostBusinessStyle(t *testing.T) {
	s, account, provider := setupComposeFixture(t)
	out, err := s.callTool("mail_reply", map[string]interface{}{
		"account_id":  float64(account.ID),
		"message_id":  "m1",
		"body":        "Thank you, attached.",
		"quote_style": "top_post",
	})
	if err != nil {
		t.Fatalf("mail_reply error: %v", err)
	}
	reply := out["reply"].(*MailComposeReplyInfo)
	if reply.QuoteStyle != "top_post" {
		t.Fatalf("quote style = %q, want top_post", reply.QuoteStyle)
	}
	lines := strings.Split(strings.TrimRight(provider.lastDraft.Body, "\n"), "\n")
	if !strings.Contains(lines[0], "Thank you, attached.") {
		t.Fatalf("top-post must open with reply text, got first line %q", lines[0])
	}
}

func TestMailReplyAllAddsOriginalRecipients(t *testing.T) {
	s, account, provider := setupComposeFixture(t)
	if _, err := s.callTool("mail_reply", map[string]interface{}{
		"account_id": float64(account.ID),
		"message_id": "m1",
		"body":       "Thanks.",
		"reply_all":  true,
	}); err != nil {
		t.Fatalf("mail_reply error: %v", err)
	}
	if len(provider.lastDraft.Cc) != 1 || provider.lastDraft.Cc[0] != "gcc-patches@gcc.gnu.org" {
		t.Fatalf("reply_all Cc = %#v, want [gcc-patches@gcc.gnu.org]", provider.lastDraft.Cc)
	}
}

func extractContentType(raw []byte) string {
	header, _, _ := splitHeaderAndBody(raw)
	for _, line := range strings.Split(header, "\r\n") {
		if strings.HasPrefix(strings.ToLower(line), "content-type:") {
			return strings.TrimSpace(line[len("content-type:"):])
		}
	}
	return ""
}

func extractMIMEBody(raw []byte) []byte {
	_, body, _ := splitHeaderAndBody(raw)
	return []byte(body)
}

func splitHeaderAndBody(raw []byte) (string, string, bool) {
	idx := bytes.Index(raw, []byte("\r\n\r\n"))
	if idx < 0 {
		return string(raw), "", false
	}
	return string(raw[:idx]), string(raw[idx+4:]), true
}
