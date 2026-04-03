package email

import (
	"context"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/krystophny/sloppy/internal/email/imaptest"
)

func TestIMAPClient_ListLabels(t *testing.T) {
	server, err := imaptest.NewServer("test", "password")
	if err != nil {
		t.Fatalf("Failed to create test server: %v", err)
	}
	defer server.Close()

	server.AddMailbox("Sent")
	server.AddMailbox("Archive")

	client := NewIMAPClient("test", "127.0.0.1", server.Port(), "test", "password", false, false)
	defer client.Close()

	ctx := context.Background()
	labels, err := client.ListLabels(ctx)
	if err != nil {
		t.Fatalf("ListLabels failed: %v", err)
	}

	if len(labels) != 3 {
		t.Errorf("Expected 3 labels, got %d", len(labels))
	}

	labelNames := make(map[string]bool)
	for _, lbl := range labels {
		labelNames[lbl.Name] = true
	}

	for _, expected := range []string{"INBOX", "Sent", "Archive"} {
		if !labelNames[expected] {
			t.Errorf("Expected label %q not found", expected)
		}
	}
}

func TestIMAPClient_ListMessages(t *testing.T) {
	server, err := imaptest.NewServer("test", "password")
	if err != nil {
		t.Fatalf("Failed to create test server: %v", err)
	}
	defer server.Close()

	server.AddMessage("INBOX", imaptest.TestMessage{
		Subject: "Test Message 1",
		From:    "alice@example.com",
		To:      "bob@example.com",
		Body:    "Hello, this is a test message.",
	})
	server.AddMessage("INBOX", imaptest.TestMessage{
		Subject: "Test Message 2",
		From:    "charlie@example.com",
		To:      "bob@example.com",
		Body:    "Another test message.",
	})

	client := NewIMAPClient("test", "127.0.0.1", server.Port(), "test", "password", false, false)
	defer client.Close()

	ctx := context.Background()
	opts := DefaultSearchOptions()
	messageIDs, err := client.ListMessages(ctx, opts)
	if err != nil {
		t.Fatalf("ListMessages failed: %v", err)
	}

	if len(messageIDs) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(messageIDs))
	}
}

func TestIMAPClient_SearchByFrom(t *testing.T) {
	server, err := imaptest.NewServer("test", "password")
	if err != nil {
		t.Fatalf("Failed to create test server: %v", err)
	}
	defer server.Close()

	server.AddMessage("INBOX", imaptest.TestMessage{
		Subject: "From Alice",
		From:    "alice@example.com",
		To:      "bob@example.com",
		Body:    "Message from Alice",
	})
	server.AddMessage("INBOX", imaptest.TestMessage{
		Subject: "From Charlie",
		From:    "charlie@example.com",
		To:      "bob@example.com",
		Body:    "Message from Charlie",
	})

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

	server.AddMessage("INBOX", imaptest.TestMessage{
		Subject: "Meeting Tomorrow",
		From:    "alice@example.com",
		To:      "bob@example.com",
		Body:    "Let's meet tomorrow.",
	})
	server.AddMessage("INBOX", imaptest.TestMessage{
		Subject: "Project Update",
		From:    "charlie@example.com",
		To:      "bob@example.com",
		Body:    "Here is the project update.",
	})

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
	server.AddMessage("INBOX", imaptest.TestMessage{
		Subject: "Recent Message",
		From:    "alice@example.com",
		To:      "bob@example.com",
		Date:    now.Add(-24 * time.Hour),
		Body:    "Recent message.",
	})
	server.AddMessage("INBOX", imaptest.TestMessage{
		Subject: "Old Message",
		From:    "charlie@example.com",
		To:      "bob@example.com",
		Date:    now.Add(-30 * 24 * time.Hour),
		Body:    "Old message.",
	})

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

	server.AddMessage("INBOX", imaptest.TestMessage{
		UID:     123,
		Subject: "Test Subject",
		From:    "alice@example.com",
		To:      "bob@example.com",
		Body:    "This is the body.",
	})

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

	server.AddMessage("INBOX", imaptest.TestMessage{
		UID:     456,
		Subject: "Test with Body",
		From:    "alice@example.com",
		To:      "bob@example.com",
		Body:    "This is the message body content.",
	})

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

	server.AddMessage("INBOX", imaptest.TestMessage{
		UID:     1,
		Subject: "Message 1",
		From:    "alice@example.com",
		To:      "bob@example.com",
	})
	server.AddMessage("INBOX", imaptest.TestMessage{
		UID:     2,
		Subject: "Message 2",
		From:    "charlie@example.com",
		To:      "bob@example.com",
	})

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
	server.AddMessage("INBOX", imaptest.TestMessage{
		UID:     1,
		Subject: "Archive me",
		From:    "alice@example.com",
		To:      "bob@example.com",
	})

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
	server.AddMessage("INBOX", imaptest.TestMessage{
		UID:     1,
		Subject: "Trash me",
		From:    "alice@example.com",
		To:      "bob@example.com",
	})

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

	server.AddMessage("INBOX", imaptest.TestMessage{
		UID:     1,
		Subject: "Read state",
		From:    "alice@example.com",
		To:      "bob@example.com",
		Flags:   []imap.Flag{},
	})

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

	server.AddMessage("INBOX", imaptest.TestMessage{
		UID:     1,
		Subject: "Delete me",
		From:    "alice@example.com",
		To:      "bob@example.com",
	})

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
	opts := DefaultSearchOptions().
		WithFolder("INBOX").
		WithFrom("alice@example.com").
		WithTo("bob@example.com").
		WithSubject("Test").
		WithText("keyword").
		WithLastDays(7).
		WithIsRead(false).
		WithIsFlagged(true).
		WithHasAttachment(true).
		WithMaxResults(50)

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
