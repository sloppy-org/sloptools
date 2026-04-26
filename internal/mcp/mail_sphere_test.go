package mcp

import (
	"testing"

	"github.com/sloppy-org/sloptools/internal/store"
)

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
