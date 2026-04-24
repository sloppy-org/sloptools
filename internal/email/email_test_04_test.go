package email

import (
	"context"
	imap "github.com/emersion/go-imap/v2"
	"github.com/sloppy-org/sloptools/internal/email/imaptest"
	"github.com/sloppy-org/sloptools/internal/ews"
	gmail "google.golang.org/api/gmail/v1"
	"strings"
	"testing"
	"time"
)

func TestIMAPClient_SearchByFrom(t *testing.T) {
	server, err := imaptest.NewServer("test", "password")
	if err != nil {
		t.Fatalf("Failed to create test server: %v", err)
	}
	defer server.Close()
	server.AddMessage("INBOX", imaptest.TestMessage{Subject: "From Alice", From: "alice@example.com", To: "bob@example.com", Body: "Message from Alice"})
	server.AddMessage("INBOX", imaptest.TestMessage{Subject: "From Charlie", From: "charlie@example.com", To: "bob@example.com", Body: "Message from Charlie"})
	client := NewIMAPClient("test", "127.0.0.1", server.Port(), "test", "password", false, false)
	defer client.Close()
	ctx := context.Background()
	opts := DefaultSearchOptions().WithFrom("alice")
	messageIDs, err := client.ListMessages(ctx, opts)
	if err != nil {
		t.Fatalf("ListMessages failed: %v", err)
	}
	if len(messageIDs) != 1 {
		t.Errorf("Expected 1 message, got %d", len(messageIDs))
	}
}

func TestIMAPClient_SearchBySubject(t *testing.T) {
	server, err := imaptest.NewServer("test", "password")
	if err != nil {
		t.Fatalf("Failed to create test server: %v", err)
	}
	defer server.Close()
	server.AddMessage("INBOX", imaptest.TestMessage{Subject: "Meeting Tomorrow", From: "alice@example.com", To: "bob@example.com", Body: "Let's meet tomorrow."})
	server.AddMessage("INBOX", imaptest.TestMessage{Subject: "Project Update", From: "charlie@example.com", To: "bob@example.com", Body: "Here is the project update."})
	client := NewIMAPClient("test", "127.0.0.1", server.Port(), "test", "password", false, false)
	defer client.Close()
	ctx := context.Background()
	opts := DefaultSearchOptions().WithSubject("Meeting")
	messageIDs, err := client.ListMessages(ctx, opts)
	if err != nil {
		t.Fatalf("ListMessages failed: %v", err)
	}
	if len(messageIDs) != 1 {
		t.Errorf("Expected 1 message, got %d", len(messageIDs))
	}
}

func TestIMAPClient_SearchByDate(t *testing.T) {
	server, err := imaptest.NewServer("test", "password")
	if err != nil {
		t.Fatalf("Failed to create test server: %v", err)
	}
	defer server.Close()
	now := time.Now()
	server.AddMessage("INBOX", imaptest.TestMessage{Subject: "Recent Message", From: "alice@example.com", To: "bob@example.com", Date: now.Add(-24 * time.Hour), Body: "Recent message."})
	server.AddMessage("INBOX", imaptest.TestMessage{Subject: "Old Message", From: "charlie@example.com", To: "bob@example.com", Date: now.Add(-30 * 24 * time.Hour), Body: "Old message."})
	client := NewIMAPClient("test", "127.0.0.1", server.Port(), "test", "password", false, false)
	defer client.Close()
	ctx := context.Background()
	opts := DefaultSearchOptions().WithLastDays(7)
	messageIDs, err := client.ListMessages(ctx, opts)
	if err != nil {
		t.Fatalf("ListMessages failed: %v", err)
	}
	if len(messageIDs) != 1 {
		t.Errorf("Expected 1 message from last 7 days, got %d", len(messageIDs))
	}
}

func TestIMAPClient_GetMessage(t *testing.T) {
	server, err := imaptest.NewServer("test", "password")
	if err != nil {
		t.Fatalf("Failed to create test server: %v", err)
	}
	defer server.Close()
	server.AddMessage("INBOX", imaptest.TestMessage{UID: 123, Subject: "Test Subject", From: "alice@example.com", To: "bob@example.com", Body: "This is the body."})
	client := NewIMAPClient("test", "127.0.0.1", server.Port(), "test", "password", false, false)
	defer client.Close()
	ctx := context.Background()
	msg, err := client.GetMessage(ctx, "INBOX:123", "metadata")
	if err != nil {
		t.Fatalf("GetMessage failed: %v", err)
	}
	if msg.Subject != "Test Subject" {
		t.Errorf("Expected subject 'Test Subject', got %q", msg.Subject)
	}
	if msg.Sender != "alice@example.com" {
		t.Errorf("Expected sender 'alice@example.com', got %q", msg.Sender)
	}
}

func TestIMAPClient_GetMessageWithBody(t *testing.T) {
	server, err := imaptest.NewServer("test", "password")
	if err != nil {
		t.Fatalf("Failed to create test server: %v", err)
	}
	defer server.Close()
	server.AddMessage("INBOX", imaptest.TestMessage{UID: 456, Subject: "Test with Body", From: "alice@example.com", To: "bob@example.com", Body: "This is the message body content."})
	client := NewIMAPClient("test", "127.0.0.1", server.Port(), "test", "password", false, false)
	defer client.Close()
	ctx := context.Background()
	msg, err := client.GetMessage(ctx, "INBOX:456", "full")
	if err != nil {
		t.Fatalf("GetMessage failed: %v", err)
	}
	if msg.Subject != "Test with Body" {
		t.Errorf("Expected subject 'Test with Body', got %q", msg.Subject)
	}
	if msg.BodyText == nil {
		t.Error("Expected body text to be set")
	}
}

func TestIMAPClient_GetMessages(t *testing.T) {
	server, err := imaptest.NewServer("test", "password")
	if err != nil {
		t.Fatalf("Failed to create test server: %v", err)
	}
	defer server.Close()
	server.AddMessage("INBOX", imaptest.TestMessage{UID: 1, Subject: "Message 1", From: "alice@example.com", To: "bob@example.com"})
	server.AddMessage("INBOX", imaptest.TestMessage{UID: 2, Subject: "Message 2", From: "charlie@example.com", To: "bob@example.com"})
	client := NewIMAPClient("test", "127.0.0.1", server.Port(), "test", "password", false, false)
	defer client.Close()
	ctx := context.Background()
	msgs, err := client.GetMessages(ctx, []string{"INBOX:1", "INBOX:2"}, "metadata")
	if err != nil {
		t.Fatalf("GetMessages failed: %v", err)
	}
	if len(msgs) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(msgs))
	}
}

func TestIMAPClient_Archive(t *testing.T) {
	server, err := imaptest.NewServer("test", "password")
	if err != nil {
		t.Fatalf("Failed to create test server: %v", err)
	}
	defer server.Close()
	server.AddMailbox("Archive")
	server.AddMessage("INBOX", imaptest.TestMessage{UID: 1, Subject: "Archive me", From: "alice@example.com", To: "bob@example.com"})
	client := NewIMAPClient("test", "127.0.0.1", server.Port(), "test", "password", false, false)
	defer client.Close()
	ctx := context.Background()
	count, err := client.Archive(ctx, []string{"INBOX:1"})
	if err != nil {
		t.Fatalf("Archive failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("Expected 1 archived message, got %d", count)
	}
	inboxIDs, err := client.ListMessages(ctx, DefaultSearchOptions().WithFolder("INBOX"))
	if err != nil {
		t.Fatalf("ListMessages INBOX failed: %v", err)
	}
	if len(inboxIDs) != 0 {
		t.Fatalf("Expected INBOX to be empty after archive, got %d", len(inboxIDs))
	}
	archiveIDs, err := client.ListMessages(ctx, DefaultSearchOptions().WithFolder("Archive"))
	if err != nil {
		t.Fatalf("ListMessages Archive failed: %v", err)
	}
	if len(archiveIDs) != 1 {
		t.Fatalf("Expected 1 archived message, got %d", len(archiveIDs))
	}
}

func TestIMAPClient_Trash(t *testing.T) {
	server, err := imaptest.NewServer("test", "password")
	if err != nil {
		t.Fatalf("Failed to create test server: %v", err)
	}
	defer server.Close()
	server.AddMailbox("Trash")
	server.AddMessage("INBOX", imaptest.TestMessage{UID: 1, Subject: "Trash me", From: "alice@example.com", To: "bob@example.com"})
	client := NewIMAPClient("test", "127.0.0.1", server.Port(), "test", "password", false, false)
	defer client.Close()
	ctx := context.Background()
	count, err := client.Trash(ctx, []string{"INBOX:1"})
	if err != nil {
		t.Fatalf("Trash failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("Expected 1 trashed message, got %d", count)
	}
	trashIDs, err := client.ListMessages(ctx, DefaultSearchOptions().WithFolder("Trash"))
	if err != nil {
		t.Fatalf("ListMessages Trash failed: %v", err)
	}
	if len(trashIDs) != 1 {
		t.Fatalf("Expected 1 message in Trash, got %d", len(trashIDs))
	}
}

func TestIMAPClient_MarkReadAndUnread(t *testing.T) {
	server, err := imaptest.NewServer("test", "password")
	if err != nil {
		t.Fatalf("Failed to create test server: %v", err)
	}
	defer server.Close()
	server.AddMessage("INBOX", imaptest.TestMessage{UID: 1, Subject: "Read state", From: "alice@example.com", To: "bob@example.com", Flags: []imap.Flag{}})
	client := NewIMAPClient("test", "127.0.0.1", server.Port(), "test", "password", false, false)
	defer client.Close()
	ctx := context.Background()
	if _, err := client.MarkRead(ctx, []string{"INBOX:1"}); err != nil {
		t.Fatalf("MarkRead failed: %v", err)
	}
	msg, err := client.GetMessage(ctx, "INBOX:1", "metadata")
	if err != nil {
		t.Fatalf("GetMessage failed after MarkRead: %v", err)
	}
	if !msg.IsRead {
		t.Fatal("Expected message to be marked as read")
	}
	if _, err := client.MarkUnread(ctx, []string{"INBOX:1"}); err != nil {
		t.Fatalf("MarkUnread failed: %v", err)
	}
	msg, err = client.GetMessage(ctx, "INBOX:1", "metadata")
	if err != nil {
		t.Fatalf("GetMessage failed after MarkUnread: %v", err)
	}
	if msg.IsRead {
		t.Fatal("Expected message to be marked as unread")
	}
}

func TestIMAPClient_Delete(t *testing.T) {
	server, err := imaptest.NewServer("test", "password")
	if err != nil {
		t.Fatalf("Failed to create test server: %v", err)
	}
	defer server.Close()
	server.AddMessage("INBOX", imaptest.TestMessage{UID: 1, Subject: "Delete me", From: "alice@example.com", To: "bob@example.com"})
	client := NewIMAPClient("test", "127.0.0.1", server.Port(), "test", "password", false, false)
	defer client.Close()
	ctx := context.Background()
	count, err := client.Delete(ctx, []string{"INBOX:1"})
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("Expected 1 deleted message, got %d", count)
	}
	ids, err := client.ListMessages(ctx, DefaultSearchOptions().WithFolder("INBOX"))
	if err != nil {
		t.Fatalf("ListMessages INBOX failed: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("Expected INBOX to be empty after delete, got %d", len(ids))
	}
}

func TestIMAPClient_ProviderName(t *testing.T) {
	client := NewIMAPClient("myimap", "localhost", 993, "user", "pass", true, false)
	if client.ProviderName() != "myimap" {
		t.Errorf("Expected provider name 'myimap', got %q", client.ProviderName())
	}
}

func TestIMAPClient_InvalidMessageID(t *testing.T) {
	server, err := imaptest.NewServer("test", "password")
	if err != nil {
		t.Fatalf("Failed to create test server: %v", err)
	}
	defer server.Close()
	client := NewIMAPClient("test", "127.0.0.1", server.Port(), "test", "password", false, false)
	defer client.Close()
	ctx := context.Background()
	_, err = client.GetMessage(ctx, "invalid-message-id", "metadata")
	if err == nil {
		t.Error("Expected error for invalid message ID")
	}
}

func TestSearchOptions_Builder(t *testing.T) {
	opts := DefaultSearchOptions().WithFolder("INBOX").WithFrom("alice@example.com").WithTo("bob@example.com").WithSubject("Test").WithText("keyword").WithLastDays(7).WithIsRead(false).WithIsFlagged(true).WithHasAttachment(true).WithMaxResults(50)
	if opts.Folder != "INBOX" {
		t.Errorf("Expected folder 'INBOX', got %q", opts.Folder)
	}
	if opts.From != "alice@example.com" {
		t.Errorf("Expected from 'alice@example.com', got %q", opts.From)
	}
	if opts.To != "bob@example.com" {
		t.Errorf("Expected to 'bob@example.com', got %q", opts.To)
	}
	if opts.Subject != "Test" {
		t.Errorf("Expected subject 'Test', got %q", opts.Subject)
	}
	if opts.Text != "keyword" {
		t.Errorf("Expected text 'keyword', got %q", opts.Text)
	}
	if opts.IsRead == nil || *opts.IsRead {
		t.Error("Expected IsRead to be false")
	}
	if opts.IsFlagged == nil || !*opts.IsFlagged {
		t.Error("Expected IsFlagged to be true")
	}
	if opts.HasAttachment == nil || !*opts.HasAttachment {
		t.Error("Expected HasAttachment to be true")
	}
	if opts.MaxResults != 50 {
		t.Errorf("Expected MaxResults 50, got %d", opts.MaxResults)
	}
}

func TestFormatQuotedReplyBottomPostGCCStyle(t *testing.T) {
	source := QuoteSource{From: "Jane Dev <jane@gcc.gnu.org>", Date: time.Date(2026, 4, 21, 9, 0, 0, 0, time.UTC), Body: "PR93456: the new optimization breaks ppc.\nPlease revert."}
	out := FormatQuotedReply(ReplyQuoteBottomPost, "Confirmed, reverting in r123456.", source)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) < 4 {
		t.Fatalf("expected at least 4 lines, got %d: %q", len(lines), out)
	}
	if !strings.HasSuffix(lines[0], "Jane Dev wrote:") {
		t.Fatalf("first line should be attribution, got %q", lines[0])
	}
	quoteStart := -1
	for i, line := range lines {
		if strings.HasPrefix(line, "> ") {
			quoteStart = i
			break
		}
	}
	if quoteStart == -1 {
		t.Fatalf("missing quoted lines in: %q", out)
	}
	replyIdx := -1
	for i := quoteStart; i < len(lines); i++ {
		if !strings.HasPrefix(lines[i], ">") && lines[i] != "" {
			replyIdx = i
			break
		}
	}
	if replyIdx == -1 {
		t.Fatalf("reply text not found below quote: %q", out)
	}
	if !strings.Contains(lines[replyIdx], "Confirmed, reverting") {
		t.Fatalf("reply text placement wrong: %q", out)
	}
}

func TestFormatQuotedReplyTopPostBusinessStyle(t *testing.T) {
	source := QuoteSource{From: "Client <client@example.com>", Date: time.Date(2026, 4, 21, 14, 0, 0, 0, time.UTC), Body: "Please send the quarterly report."}
	out := FormatQuotedReply(ReplyQuoteTopPost, "Attached. Best regards, Albert.", source)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) < 4 {
		t.Fatalf("expected at least 4 lines, got %d: %q", len(lines), out)
	}
	if !strings.Contains(lines[0], "Attached. Best regards") {
		t.Fatalf("top-post must begin with the reply, got %q", lines[0])
	}
	var attributionIdx int = -1
	for i, line := range lines {
		if strings.HasSuffix(line, "Client wrote:") {
			attributionIdx = i
			break
		}
	}
	if attributionIdx == -1 {
		t.Fatalf("attribution line missing: %q", out)
	}
	quoteIdx := -1
	for i := attributionIdx; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "> ") {
			quoteIdx = i
			break
		}
	}
	if quoteIdx == -1 {
		t.Fatalf("expected quoted original below attribution, got %q", out)
	}
}

func TestFormatQuotedReplyDeepenQuotes(t *testing.T) {
	source := QuoteSource{Body: "> existing quote\nand my line"}
	out := FormatQuotedReply(ReplyQuoteBottomPost, "Reply.", source)
	if !strings.Contains(out, ">> existing quote") {
		t.Fatalf("nested quotes should deepen, got: %q", out)
	}
	if !strings.Contains(out, "> and my line") {
		t.Fatalf("non-quoted original line should get > prefix, got: %q", out)
	}
}

func TestParseReplyQuoteStyleAccepts(t *testing.T) {
	tests := map[string]ReplyQuoteStyle{"": ReplyQuoteBottomPost, "bottom_post": ReplyQuoteBottomPost, "GCC": ReplyQuoteBottomPost, "interleaved": ReplyQuoteBottomPost, "top_post": ReplyQuoteTopPost, "business": ReplyQuoteTopPost, "modern": ReplyQuoteTopPost}
	for input, want := range tests {
		got, err := ParseReplyQuoteStyle(input)
		if err != nil {
			t.Fatalf("ParseReplyQuoteStyle(%q) error: %v", input, err)
		}
		if got != want {
			t.Fatalf("ParseReplyQuoteStyle(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestParseReplyQuoteStyleRejectsUnknown(t *testing.T) {
	if _, err := ParseReplyQuoteStyle("sideways"); err == nil {
		t.Fatal("expected error for unknown quote style")
	}
}

func TestGmailFilterToServerFilterMapsArchiveAndLabels(t *testing.T) {
	filter := &gmail.Filter{Id: "filter-1", Criteria: &gmail.FilterCriteria{From: "boss@example.com", Query: "list:physics.example", Subject: "status"}, Action: &gmail.FilterAction{AddLabelIds: []string{"Label_1"}, RemoveLabelIds: []string{"INBOX", "UNREAD"}, Forward: "archive@example.com"}}
	out := gmailFilterToServerFilter(filter, map[string]string{"Label_1": "lists"})
	if out.ID != "filter-1" {
		t.Fatalf("ID = %q, want filter-1", out.ID)
	}
	if !out.Action.Archive {
		t.Fatal("Archive = false, want true")
	}
	if !out.Action.MarkRead {
		t.Fatal("MarkRead = false, want true")
	}
	if out.Action.MoveTo != "lists" {
		t.Fatalf("MoveTo = %q, want lists", out.Action.MoveTo)
	}
}

func TestExchangeEWSRuleToServerFilterMapsArchiveMove(t *testing.T) {
	provider := &ExchangeEWSMailProvider{cfg: ExchangeEWSConfig{ArchiveFolder: "Archive"}}
	out := provider.ruleToServerFilter(context.Background(), ews.Rule{ID: "rule-1", Name: "Reference", Enabled: true, Actions: ews.RuleActions{MoveToFolderID: "Archive"}})
	if out.ID != "rule-1" {
		t.Fatalf("ID = %q, want rule-1", out.ID)
	}
	if out.Action.MoveTo != "Archive" {
		t.Fatalf("MoveTo = %q, want Archive", out.Action.MoveTo)
	}
	if !out.Action.Archive {
		t.Fatal("Archive = false, want true")
	}
}
