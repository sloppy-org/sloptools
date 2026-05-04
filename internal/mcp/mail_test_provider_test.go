package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sloppy-org/sloptools/internal/email"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/store"
)

type fakeMailProvider struct {
	labels             []providerdata.Label
	listIDs            []string
	pageIDs            []string
	nextPage           string
	messages           map[string]*providerdata.EmailMessage
	attachment         *providerdata.AttachmentData
	filters            []email.ServerFilter
	resolvedIDs        map[string]string
	lastOpts           email.SearchOptions
	lastAction         string
	lastIDs            []string
	lastFolder         string
	lastLabel          string
	lastUntil          time.Time
	lastFormat         string
	lastFlag           email.Flag
	lastCategories     []string
	getMessagesFormats []string
	supportsDefer      bool
}

func (p *fakeMailProvider) SetFlag(_ context.Context, ids []string, flag email.Flag) (int, error) {
	p.record("set_flag", ids)
	p.lastFlag = flag
	return len(ids), nil
}

func (p *fakeMailProvider) ClearFlag(_ context.Context, ids []string) (int, error) {
	p.record("clear_flag", ids)
	p.lastFlag = email.Flag{}
	return len(ids), nil
}

func (p *fakeMailProvider) SetCategories(_ context.Context, ids []string, categories []string) (int, error) {
	p.record("set_categories", ids)
	p.lastCategories = append([]string(nil), categories...)
	return len(ids), nil
}

func (p *fakeMailProvider) ListLabels(_ context.Context) ([]providerdata.Label, error) {
	return append([]providerdata.Label(nil), p.labels...), nil
}

func (p *fakeMailProvider) ListMessages(_ context.Context, opts email.SearchOptions) ([]string, error) {
	p.lastOpts = opts
	return append([]string(nil), p.listIDs...), nil
}

func (p *fakeMailProvider) ListMessagesPage(_ context.Context, opts email.SearchOptions, _ string) (email.MessagePage, error) {
	p.lastOpts = opts
	ids := p.pageIDs
	if len(ids) == 0 {
		ids = p.listIDs
	}
	return email.MessagePage{IDs: append([]string(nil), ids...), NextPageToken: p.nextPage}, nil
}

func (p *fakeMailProvider) GetMessage(_ context.Context, messageID, _ string) (*providerdata.EmailMessage, error) {
	msg := p.messages[messageID]
	if msg == nil {
		return nil, nil
	}
	return cloneMailMessage(msg), nil
}

func (p *fakeMailProvider) GetMessages(_ context.Context, messageIDs []string, format string) ([]*providerdata.EmailMessage, error) {
	p.getMessagesFormats = append(p.getMessagesFormats, format)
	p.lastFormat = format
	out := make([]*providerdata.EmailMessage, 0, len(messageIDs))
	for _, id := range messageIDs {
		msg := p.messages[id]
		if msg == nil {
			out = append(out, nil)
			continue
		}
		clone := cloneMailMessage(msg)
		if format == "metadata" {
			clone.BodyText = nil
			clone.BodyHTML = nil
			clone.Attachments = nil
		}
		out = append(out, clone)
	}
	return out, nil
}

func (p *fakeMailProvider) GetAttachment(_ context.Context, _, _ string) (*providerdata.AttachmentData, error) {
	if p.attachment == nil {
		return nil, nil
	}
	copyValue := *p.attachment
	copyValue.Content = append([]byte(nil), p.attachment.Content...)
	return &copyValue, nil
}

func (p *fakeMailProvider) MarkRead(_ context.Context, ids []string) (int, error) {
	return p.record("mark_read", ids), nil
}

func (p *fakeMailProvider) MarkUnread(_ context.Context, ids []string) (int, error) {
	return p.record("mark_unread", ids), nil
}

func (p *fakeMailProvider) Archive(_ context.Context, ids []string) (int, error) {
	return p.record("archive", ids), nil
}

func (p *fakeMailProvider) ArchiveResolved(_ context.Context, ids []string) ([]email.ActionResolution, error) {
	p.record("archive", ids)
	return p.resolutions(ids), nil
}

func (p *fakeMailProvider) MoveToInbox(_ context.Context, ids []string) (int, error) {
	return p.record("move_to_inbox", ids), nil
}

func (p *fakeMailProvider) MoveToInboxResolved(_ context.Context, ids []string) ([]email.ActionResolution, error) {
	p.record("move_to_inbox", ids)
	return p.resolutions(ids), nil
}

func TestMailToolsListReadAndAttachment(t *testing.T) {
	s, account, provider := newMailToolsFixture(t)
	listed, err := s.callTool("mail_account_list", map[string]interface{}{})
	if err != nil {
		t.Fatalf("mail_account_list failed: %v", err)
	}
	accounts, _ := listed["accounts"].([]store.ExternalAccount)
	if len(accounts) != 1 || accounts[0].ID != account.ID {
		t.Fatalf("accounts = %+v", accounts)
	}
	messages, err := s.callTool("mail_message_list", map[string]interface{}{"account_id": account.ID, "page_token": "next-1"})
	if err != nil {
		t.Fatalf("mail_message_list failed: %v", err)
	}
	if got := messages["next_page_token"]; got != "next-2" {
		t.Fatalf("next_page_token = %#v", got)
	}
	if provider.lastFormat != "metadata" {
		t.Fatalf("list format = %q, want metadata", provider.lastFormat)
	}
	if provider.lastOpts.MaxResults != compactListLimit {
		t.Fatalf("default list limit = %d, want %d", provider.lastOpts.MaxResults, compactListLimit)
	}
	message, err := s.callTool("mail_message_get", map[string]interface{}{"account_id": account.ID, "message_id": "m1"})
	if err != nil {
		t.Fatalf("mail_message_get failed: %v", err)
	}
	gotMessage, _ := message["message"].(*providerdata.EmailMessage)
	if gotMessage == nil || gotMessage.ID != "m1" {
		t.Fatalf("message = %#v", message["message"])
	}
	destDir := t.TempDir()
	attachment, err := s.callTool("mail_attachment_get", map[string]interface{}{"account_id": account.ID, "message_id": "m1", "attachment_id": "att-1", "dest_dir": destDir})
	if err != nil {
		t.Fatalf("mail_attachment_get failed: %v", err)
	}
	gotAttachment, _ := attachment["attachment"].(map[string]interface{})
	if gotAttachment["id"] != "att-1" {
		t.Fatalf("attachment id = %#v", gotAttachment["id"])
	}
	if _, hasB64 := gotAttachment["content_base64"]; hasB64 {
		t.Fatalf("attachment must not contain content_base64: %#v", gotAttachment)
	}
	pathAny, ok := gotAttachment["path"].(string)
	if !ok || pathAny == "" {
		t.Fatalf("attachment path missing: %#v", gotAttachment)
	}
	if !strings.HasPrefix(pathAny, destDir) {
		t.Fatalf("attachment path %q not under destDir %q", pathAny, destDir)
	}
	data, err := os.ReadFile(pathAny)
	if err != nil {
		t.Fatalf("read saved attachment: %v", err)
	}
	if string(data) != "pdfbytes" {
		t.Fatalf("saved attachment bytes = %q", data)
	}
	if gotAttachment["size_bytes"] != len([]byte("pdfbytes")) {
		t.Fatalf("size_bytes = %#v", gotAttachment["size_bytes"])
	}
	if filepath.Base(pathAny) == "" {
		t.Fatalf("empty basename for %q", pathAny)
	}
}

func TestMailMessageListDefaultsToSphereAccount(t *testing.T) {
	s, account, _ := newMailToolsFixture(t)
	messages, err := s.callTool("mail_message_list", map[string]interface{}{"sphere": store.SphereWork, "limit": 3})
	if err != nil {
		t.Fatalf("mail_message_list by sphere failed: %v", err)
	}
	gotAccount, ok := messages["account"].(store.ExternalAccount)
	if !ok {
		t.Fatalf("account payload = %#v", messages["account"])
	}
	if gotAccount.ID != account.ID {
		t.Fatalf("account id = %d, want %d", gotAccount.ID, account.ID)
	}
	if got := messages["count"]; got != 1 {
		t.Fatalf("count = %#v, want 1", got)
	}
}

func TestMailMessageListCanRequestBody(t *testing.T) {
	s, account, provider := newMailToolsFixture(t)
	if _, err := s.callTool("mail_message_list", map[string]interface{}{"account_id": account.ID, "include_body": true}); err != nil {
		t.Fatalf("mail_message_list failed: %v", err)
	}
	if provider.lastFormat != "full" {
		t.Fatalf("list format = %q, want full", provider.lastFormat)
	}
}
