package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/store"
)

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
