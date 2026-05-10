package mcp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/sloppy-org/sloptools/internal/email"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/store"
	"mime"
	"mime/multipart"
	"strings"
	"testing"
	"time"
)

func TestMailHandoffLifecycle(t *testing.T) {
	s, st, _ := newDomainServerForTest(t)
	account, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderExchangeEWS, "TU Graz", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	bodyText := "Plain text summary"
	bodyHTML := "<p>Plain text summary</p>"
	firstDate := time.Date(2026, time.March, 20, 9, 30, 0, 0, time.UTC)
	secondDate := firstDate.Add(2 * time.Hour)
	provider := &fakeMailProvider{messages: map[string]*providerdata.EmailMessage{"m1": {ID: "m1", ThreadID: "thread-1", InternetMessageID: "<m1@example.test>", Subject: "Quarterly review", Sender: "Ada <ada@example.com>", Recipients: []string{"team@example.com", "ops@example.com"}, Date: firstDate, Snippet: "Summary", Labels: []string{"Inbox", "Important"}, IsRead: true, IsFlagged: true, BodyText: &bodyText, BodyHTML: &bodyHTML, Attachments: []providerdata.Attachment{{ID: "att-1", Filename: "report.pdf", MimeType: "application/pdf", Size: 128}}}, "m2": {ID: "m2", ThreadID: "thread-2", InternetMessageID: "<m2@example.test>", Subject: "Follow-up", Sender: "Grace <grace@example.com>", Recipients: []string{"team@example.com"}, Date: secondDate, Snippet: "Next steps"}}}
	s.newEmailProvider = func(context.Context, store.ExternalAccount) (email.EmailProvider, error) {
		return provider, nil
	}
	created, err := s.callTool("sloppy_handoff", map[string]interface{}{"action": "create", "kind": "mail", "selector": map[string]interface{}{"account_id": account.ID, "message_ids": []interface{}{"m1", "m2"}}, "policy": map[string]interface{}{"max_consumes": 2}})
	if err != nil {
		t.Fatalf("handoff.create failed: %v", err)
	}
	createdMap := normalizeMap(t, created)
	handoffID := stringValue(t, createdMap["handoff_id"])
	if handoffID == "" {
		t.Fatal("handoff_id = empty")
	}
	if got := stringValue(t, createdMap["kind"]); got != handoffKindMail {
		t.Fatalf("kind = %q", got)
	}
	meta := mapValue(t, createdMap["meta"])
	if got := intValue(t, meta["message_count"]); got != 2 {
		t.Fatalf("message_count = %d", got)
	}
	if got := stringSliceValue(t, meta["message_ids"]); len(got) != 2 || got[0] != "m1" || got[1] != "m2" {
		t.Fatalf("message_ids = %#v", got)
	}
	if got := stringSliceValue(t, meta["subjects"]); len(got) != 2 || got[0] != "Quarterly review" || got[1] != "Follow-up" {
		t.Fatalf("subjects = %#v", got)
	}
	if got := stringSliceValue(t, meta["senders"]); len(got) != 2 || got[0] != "Ada <ada@example.com>" || got[1] != "Grace <grace@example.com>" {
		t.Fatalf("senders = %#v", got)
	}
	policySummary := mapValue(t, createdMap["policy_summary"])
	if got := intValue(t, policySummary["remaining_consumes"]); got != 2 {
		t.Fatalf("remaining_consumes = %d", got)
	}
	peeked, err := s.callTool("sloppy_handoff", map[string]interface{}{"action": "peek", "handoff_id": handoffID})
	if err != nil {
		t.Fatalf("handoff.peek failed: %v", err)
	}
	peekMap := normalizeMap(t, peeked)
	if _, ok := peekMap["payload"]; ok {
		t.Fatalf("handoff.peek payload = %#v, want none", peekMap["payload"])
	}
	consumed, err := s.callTool("sloppy_handoff", map[string]interface{}{"action": "consume", "handoff_id": handoffID})
	if err != nil {
		t.Fatalf("handoff.consume failed: %v", err)
	}
	consumeMap := normalizeMap(t, consumed)
	payload := mapValue(t, consumeMap["payload"])
	messages := sliceValue(t, payload["messages"])
	if len(messages) != 2 {
		t.Fatalf("payload.messages len = %d", len(messages))
	}
	firstMessage := mapValue(t, messages[0])
	if got := stringValue(t, firstMessage["message_id"]); got != "m1" {
		t.Fatalf("message_id = %q", got)
	}
	if got := stringValue(t, firstMessage["subject"]); got != "Quarterly review" {
		t.Fatalf("subject = %q", got)
	}
	if got := stringValue(t, firstMessage["sender"]); got != "Ada <ada@example.com>" {
		t.Fatalf("sender = %q", got)
	}
	if got := stringSliceValue(t, firstMessage["recipients"]); len(got) != 2 || got[0] != "team@example.com" || got[1] != "ops@example.com" {
		t.Fatalf("recipients = %#v", got)
	}
	if got := stringValue(t, firstMessage["date"]); got != firstDate.Format(time.RFC3339) {
		t.Fatalf("date = %q", got)
	}
	if got := stringValue(t, firstMessage["body_text"]); got != bodyText {
		t.Fatalf("body_text = %q", got)
	}
	attachments := sliceValue(t, firstMessage["attachments"])
	if len(attachments) != 1 {
		t.Fatalf("attachments len = %d", len(attachments))
	}
	policyState := mapValue(t, consumeMap["policy"])
	if got := intValue(t, policyState["consumed_count"]); got != 1 {
		t.Fatalf("consumed_count = %d", got)
	}
	if got := intValue(t, policyState["remaining_consumes"]); got != 1 {
		t.Fatalf("remaining_consumes = %d", got)
	}
	status, err := s.callTool("sloppy_handoff", map[string]interface{}{"action": "status", "handoff_id": handoffID})
	if err != nil {
		t.Fatalf("handoff.status failed: %v", err)
	}
	statusMap := normalizeMap(t, status)
	statusPolicy := mapValue(t, statusMap["policy_summary"])
	if got := intValue(t, statusPolicy["consumed_count"]); got != 1 {
		t.Fatalf("status consumed_count = %d", got)
	}
	revoked, err := s.callTool("sloppy_handoff", map[string]interface{}{"action": "revoke", "handoff_id": handoffID})
	if err != nil {
		t.Fatalf("handoff.revoke failed: %v", err)
	}
	revokedMap := normalizeMap(t, revoked)
	if !boolValue(t, revokedMap["revoked"]) {
		t.Fatalf("revoked = %#v", revokedMap["revoked"])
	}
	if _, err := s.callTool("sloppy_handoff", map[string]interface{}{"action": "consume", "handoff_id": handoffID}); err == nil {
		t.Fatal("handoff.consume after revoke error = nil")
	}
}

func TestMailHandoffConsumeLimit(t *testing.T) {
	s, st, _ := newDomainServerForTest(t)
	account, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderGmail, "Work Gmail", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	provider := &fakeMailProvider{messages: map[string]*providerdata.EmailMessage{"m1": {ID: "m1", Subject: "Only once", Sender: "ada@example.com", Date: time.Date(2026, time.March, 21, 10, 0, 0, 0, time.UTC)}}}
	s.newEmailProvider = func(context.Context, store.ExternalAccount) (email.EmailProvider, error) {
		return provider, nil
	}
	created, err := s.callTool("sloppy_handoff", map[string]interface{}{"action": "create", "kind": "mail", "selector": map[string]interface{}{"account_id": account.ID, "message_id": "m1"}})
	if err != nil {
		t.Fatalf("handoff.create failed: %v", err)
	}
	handoffID := stringValue(t, normalizeMap(t, created)["handoff_id"])
	if _, err := s.callTool("sloppy_handoff", map[string]interface{}{"action": "consume", "handoff_id": handoffID}); err != nil {
		t.Fatalf("first handoff.consume failed: %v", err)
	}
	_, err = s.callTool("sloppy_handoff", map[string]interface{}{"action": "consume", "handoff_id": handoffID})
	if err == nil {
		t.Fatal("second handoff.consume error = nil")
	}
	if got := err.Error(); got != "handoff has no remaining consumes" {
		t.Fatalf("error = %q", got)
	}
}

func TestHandoffToolDefinitions(t *testing.T) {
	defs := toolDefinitions()
	names := map[string]map[string]interface{}{}
	for _, def := range defs {
		name, _ := def["name"].(string)
		names[name] = def
	}
	if names["sloppy_handoff"] == nil {
		t.Fatal("sloppy_handoff missing from tool definitions")
	}
	// Verify sloppy_handoff description covers handoff actions.
	desc, _ := names["sloppy_handoff"]["description"].(string)
	for _, action := range []string{"create", "peek", "consume", "revoke", "status"} {
		if !strings.Contains(desc, action) {
			t.Errorf("sloppy_handoff description missing action %q", action)
		}
	}
}

func normalizeMap(t *testing.T, value interface{}) map[string]interface{} {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	return out
}

func mapValue(t *testing.T, value interface{}) map[string]interface{} {
	t.Helper()
	typed, ok := value.(map[string]interface{})
	if !ok {
		t.Fatalf("value = %#v, want map", value)
	}
	return typed
}

func sliceValue(t *testing.T, value interface{}) []interface{} {
	t.Helper()
	typed, ok := value.([]interface{})
	if !ok {
		t.Fatalf("value = %#v, want slice", value)
	}
	return typed
}

func stringValue(t *testing.T, value interface{}) string {
	t.Helper()
	typed, ok := value.(string)
	if !ok {
		t.Fatalf("value = %#v, want string", value)
	}
	return typed
}

func stringSliceValue(t *testing.T, value interface{}) []string {
	t.Helper()
	items := sliceValue(t, value)
	out := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			t.Fatalf("item = %#v, want string", item)
		}
		out = append(out, text)
	}
	return out
}

func intValue(t *testing.T, value interface{}) int {
	t.Helper()
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	default:
		t.Fatalf("value = %#v, want number", value)
		return 0
	}
}

func boolValue(t *testing.T, value interface{}) bool {
	t.Helper()
	typed, ok := value.(bool)
	if !ok {
		t.Fatalf("value = %#v, want bool", value)
	}
	return typed
}

type fakeDraftMailProvider struct {
	fakeMailProvider
	lastDraft          email.DraftInput
	lastDraftID        string
	lastSendID         string
	lastRawMIME        []byte
	sentInput          email.DraftInput
	createCalls        int
	sendCalls          int
	sendExistingCalls  int
	lastSendExistingID string
	createDraft        email.Draft
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

func (p *fakeDraftMailProvider) SendExistingDraft(_ context.Context, draftID string) error {
	p.sendExistingCalls++
	p.lastSendExistingID = draftID
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
	provider := &fakeDraftMailProvider{fakeMailProvider: fakeMailProvider{messages: map[string]*providerdata.EmailMessage{"m1": {ID: "m1", ThreadID: "thread-src", InternetMessageID: "<abc@gcc.gnu.org>", Subject: "PR123 issue", Sender: "Jane Dev <jane@gcc.gnu.org>", Recipients: []string{"gcc-patches@gcc.gnu.org", "albert@tugraz.at"}, Date: now, BodyText: &body}}}}
	s.newEmailProvider = func(context.Context, store.ExternalAccount) (email.EmailProvider, error) {
		return provider, nil
	}
	return s, account, provider
}

func TestMailSendDispatchBuildsMultipartAndSends(t *testing.T) {
	s, account, provider := setupComposeFixture(t)
	args := map[string]interface{}{"action": "send", "account_id": float64(account.ID), "to": []interface{}{"alice@example.com"}, "cc": []interface{}{"Bob <bob@example.com>"}, "subject": "Test", "body": "Hello there.", "attachments": []interface{}{map[string]interface{}{"filename": "hello.txt", "content_base64": base64.StdEncoding.EncodeToString([]byte("file-content")), "content_type": "text/plain"}}}
	out, err := s.callTool("sloppy_mail", args)
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
	provider.lastDraft.Attachments = nil
}

func TestMailSendDraftOnlyDoesNotSend(t *testing.T) {
	s, account, provider := setupComposeFixture(t)
	out, err := s.callTool("sloppy_mail", map[string]interface{}{"action": "send", "account_id": float64(account.ID), "to": []interface{}{"alice@example.com"}, "subject": "Test", "body": "Hello.", "draft_only": true})
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
	out, err := s.callTool("sloppy_mail", map[string]interface{}{"action": "reply", "account_id": float64(account.ID), "message_id": "m1", "body": "Confirmed, reverting."})
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
	out, err := s.callTool("sloppy_mail", map[string]interface{}{"action": "reply", "account_id": float64(account.ID), "message_id": "m1", "body": "Thank you, attached.", "quote_style": "top_post"})
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
	if _, err := s.callTool("sloppy_mail", map[string]interface{}{"action": "reply", "account_id": float64(account.ID), "message_id": "m1", "body": "Thanks.", "reply_all": true}); err != nil {
		t.Fatalf("mail_reply error: %v", err)
	}
	if len(provider.lastDraft.Cc) != 1 || provider.lastDraft.Cc[0] != "gcc-patches@gcc.gnu.org" {
		t.Fatalf("reply_all Cc = %#v, want [gcc-patches@gcc.gnu.org]", provider.lastDraft.Cc)
	}
}

type minimalDraftProvider struct{ fakeMailProvider }

func (p *minimalDraftProvider) CreateDraft(_ context.Context, _ email.DraftInput) (email.Draft, error) {
	return email.Draft{}, fmt.Errorf("not used in this test")
}

func (p *minimalDraftProvider) CreateReplyDraft(_ context.Context, _ string, _ email.DraftInput) (email.Draft, error) {
	return email.Draft{}, fmt.Errorf("not used in this test")
}

func (p *minimalDraftProvider) UpdateDraft(_ context.Context, _ string, _ email.DraftInput) (email.Draft, error) {
	return email.Draft{}, fmt.Errorf("not used in this test")
}

func (p *minimalDraftProvider) SendDraft(_ context.Context, _ string, _ email.DraftInput) error {
	return fmt.Errorf("not used in this test")
}

func TestMailDraftSendDispatchesSendExistingDraft(t *testing.T) {
	s, account, provider := setupComposeFixture(t)
	out, err := s.callTool("sloppy_mail", map[string]interface{}{"action": "draft_send", "account_id": float64(account.ID), "draft_id": "draft-abc"})
	if err != nil {
		t.Fatalf("mail_draft_send error: %v", err)
	}
	if out["sent"] != true {
		t.Fatalf("expected sent=true, got %v", out["sent"])
	}
	if out["draft_id"] != "draft-abc" {
		t.Fatalf("draft_id echo = %v, want draft-abc", out["draft_id"])
	}
	if provider.sendExistingCalls != 1 {
		t.Fatalf("sendExistingCalls = %d, want 1", provider.sendExistingCalls)
	}
	if provider.lastSendExistingID != "draft-abc" {
		t.Fatalf("lastSendExistingID = %q, want draft-abc", provider.lastSendExistingID)
	}
	if provider.sendCalls != 0 {
		t.Fatalf("sendCalls = %d; mail_draft_send must not route through SendDraft", provider.sendCalls)
	}
	if provider.createCalls != 0 {
		t.Fatalf("createCalls = %d; mail_draft_send must not recreate the draft", provider.createCalls)
	}
}

func TestMailDraftSendRequiresDraftID(t *testing.T) {
	s, account, _ := setupComposeFixture(t)
	if _, err := s.callTool("sloppy_mail", map[string]interface{}{"action": "draft_send", "account_id": float64(account.ID)}); err == nil {
		t.Fatalf("expected error when draft_id is missing")
	}
}
