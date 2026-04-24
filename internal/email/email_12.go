package email

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	imap "github.com/emersion/go-imap/v2"
	gomessage "github.com/emersion/go-message/mail"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"io"
	"strings"
	"time"
)

func extractIMAPBody(literal []byte) (text, html string) {
	mr, err := gomessage.CreateReader(bytes.NewReader(literal))
	if err != nil {
		return string(literal), ""
	}
	for {
		part, err := mr.NextPart()
		if err != nil {
			break
		}
		switch h := part.Header.(type) {
		case *gomessage.InlineHeader:
			ct, _, _ := h.ContentType()
			body, err := io.ReadAll(part.Body)
			if err != nil {
				continue
			}
			switch ct {
			case "text/plain":
				if text == "" {
					text = string(body)
				}
			case "text/html":
				if html == "" {
					html = string(body)
				}
			}
		}
	}
	return text, html
}

var _ DraftProvider = (*IMAPClient)(nil)

func (c *IMAPClient) CreateDraft(ctx context.Context, input DraftInput) (Draft, error) {
	normalized, err := c.normalizeIMAPDraftInput(input)
	if err != nil {
		return Draft{}, err
	}
	if err := c.connect(ctx); err != nil {
		return Draft{}, err
	}
	raw, err := buildRFC822Message(normalized)
	if err != nil {
		return Draft{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	mailbox, err := c.resolveDraftMailboxLocked()
	if err != nil {
		return Draft{}, err
	}
	draftID, err := c.appendDraftLocked(mailbox, raw)
	if err != nil {
		return Draft{}, err
	}
	return Draft{ID: draftID, ThreadID: normalized.ThreadID}, nil
}

func (c *IMAPClient) CreateReplyDraft(ctx context.Context, messageID string, input DraftInput) (Draft, error) {
	reply := input
	message, err := c.GetMessage(ctx, strings.TrimSpace(messageID), "full")
	if err == nil && message != nil {
		if len(reply.To) == 0 && strings.TrimSpace(message.Sender) != "" {
			reply.To = []string{strings.TrimSpace(message.Sender)}
		}
		if strings.TrimSpace(reply.Subject) == "" {
			reply.Subject = ensureReplySubject(message.Subject)
		} else {
			reply.Subject = ensureReplySubject(reply.Subject)
		}
		if strings.TrimSpace(reply.InReplyTo) == "" && strings.TrimSpace(message.ThreadID) != "" {
			reply.InReplyTo = strings.TrimSpace(message.ThreadID)
		}
		if len(reply.References) == 0 && strings.TrimSpace(message.ThreadID) != "" {
			reply.References = []string{strings.TrimSpace(message.ThreadID)}
		}
	}
	return c.CreateDraft(ctx, reply)
}

func (c *IMAPClient) UpdateDraft(ctx context.Context, draftID string, input DraftInput) (Draft, error) {
	folder, uid, err := parseMessageID(strings.TrimSpace(draftID))
	if err != nil {
		return Draft{}, err
	}
	normalized, err := c.normalizeIMAPDraftInput(input)
	if err != nil {
		return Draft{}, err
	}
	if err := c.connect(ctx); err != nil {
		return Draft{}, err
	}
	raw, err := buildRFC822Message(normalized)
	if err != nil {
		return Draft{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.deleteDraftLocked(folder, uid); err != nil {
		return Draft{}, err
	}
	nextID, err := c.appendDraftLocked(folder, raw)
	if err != nil {
		return Draft{}, err
	}
	return Draft{ID: nextID, ThreadID: normalized.ThreadID}, nil
}

func (c *IMAPClient) SendDraft(ctx context.Context, draftID string, input DraftInput) error {
	normalized, err := c.normalizeIMAPDraftInput(input)
	if err != nil {
		return err
	}
	recipients := append([]string{}, normalized.To...)
	recipients = append(recipients, normalized.Cc...)
	recipients = append(recipients, normalized.Bcc...)
	raw, err := buildRFC822Message(normalized)
	if err != nil {
		return err
	}
	sender := c.smtpSend
	if sender == nil {
		sender = defaultSMTPSender
	}
	if err := sender(ctx, c.smtpConfig, normalized.From, recipients, raw); err != nil {
		return fmt.Errorf("imap smtp send: %w", err)
	}
	if strings.TrimSpace(draftID) == "" {
		return nil
	}
	folder, uid, err := parseMessageID(strings.TrimSpace(draftID))
	if err != nil {
		return err
	}
	if err := c.connect(ctx); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.deleteDraftLocked(folder, uid)
}

func (c *IMAPClient) normalizeIMAPDraftInput(input DraftInput) (DraftInput, error) {
	reply := input
	if strings.TrimSpace(reply.From) == "" {
		reply.From = strings.TrimSpace(c.smtpConfig.From)
	}
	if strings.TrimSpace(reply.From) == "" {
		reply.From = strings.TrimSpace(c.smtpConfig.Username)
	}
	if strings.TrimSpace(reply.From) == "" {
		reply.From = strings.TrimSpace(c.username)
	}
	return NormalizeDraftInput(reply)
}

func (c *IMAPClient) resolveDraftMailboxLocked() (string, error) {
	candidates := []string{}
	if strings.TrimSpace(c.smtpConfig.DraftsBox) != "" {
		candidates = append(candidates, strings.TrimSpace(c.smtpConfig.DraftsBox))
	}
	candidates = append(candidates, "Drafts", "Entwürfe")
	fallback := strings.TrimSpace(c.smtpConfig.DraftsBox)
	if fallback == "" {
		fallback = "Drafts"
	}
	return c.resolveOrCreateMailboxLocked(candidates, fallback)
}

func (c *IMAPClient) appendDraftLocked(mailbox string, raw []byte) (string, error) {
	cmd := c.client.Append(mailbox, int64(len(raw)), &imap.AppendOptions{Flags: []imap.Flag{imap.FlagDraft}, Time: time.Now()})
	if _, err := cmd.Write(raw); err != nil {
		_ = cmd.Close()
		return "", fmt.Errorf("imap append draft write: %w", err)
	}
	if err := cmd.Close(); err != nil {
		return "", fmt.Errorf("imap append draft close: %w", err)
	}
	result, err := cmd.Wait()
	if err != nil {
		return "", fmt.Errorf("imap append draft: %w", err)
	}
	if result == nil || result.UID == 0 {
		return "", fmt.Errorf("imap append draft did not return a UID")
	}
	return formatMessageID(mailbox, result.UID), nil
}

func (c *IMAPClient) deleteDraftLocked(mailbox string, uid imap.UID) error {
	if _, err := c.client.Select(mailbox, nil).Wait(); err != nil {
		return fmt.Errorf("imap select draft mailbox: %w", err)
	}
	uidSet := uidSetFromSlice([]imap.UID{uid})
	storeCmd := c.client.Store(uidSet, &imap.StoreFlags{Op: imap.StoreFlagsAdd, Silent: true, Flags: []imap.Flag{imap.FlagDeleted}}, nil)
	if err := storeCmd.Close(); err != nil {
		return fmt.Errorf("imap mark draft deleted: %w", err)
	}
	caps := c.client.Caps()
	if caps.Has(imap.CapUIDPlus) || caps.Has(imap.CapIMAP4rev2) {
		if err := c.client.UIDExpunge(uidSet).Close(); err != nil {
			return fmt.Errorf("imap expunge draft: %w", err)
		}
		return nil
	}
	if err := c.client.Expunge().Close(); err != nil {
		return fmt.Errorf("imap expunge draft: %w", err)
	}
	return nil
}

func formatMessageID(mailbox string, uid imap.UID) string {
	return fmt.Sprintf("%s:%d", strings.TrimSpace(mailbox), uid)
}

var ErrCapabilityUnsupported = errors.New("capability_unsupported") // ErrCapabilityUnsupported signals that the provider does not implement a

const (
	FlagStatusNotFlagged = "notFlagged"
	FlagStatusFlagged    = "flagged"
	FlagStatusComplete   = "complete"
)

type Flag struct {
	Status string
	DueAt  *time.Time
} // Flag carries the target flag status plus an optional due date used by

type FlagMutator interface {
	SetFlag(ctx context.Context, messageIDs []string, flag Flag) (int, error)
	ClearFlag(ctx context.Context, messageIDs []string) (int, error)
}

type CategoryMutator interface {
	SetCategories(ctx context.Context, messageIDs []string, categories []string) (int, error)
}

type EmailProvider interface {
	ListLabels(ctx context.Context) ([]providerdata.Label, error)
	ListMessages(ctx context.Context, opts SearchOptions) ([]string, error)
	GetMessage(ctx context.Context, messageID, format string) (*providerdata.EmailMessage, error)
	GetMessages(ctx context.Context, messageIDs []string, format string) ([]*providerdata.EmailMessage, error)
	MarkRead(ctx context.Context, messageIDs []string) (int, error)
	MarkUnread(ctx context.Context, messageIDs []string) (int, error)
	Archive(ctx context.Context, messageIDs []string) (int, error)
	MoveToInbox(ctx context.Context, messageIDs []string) (int, error)
	Trash(ctx context.Context, messageIDs []string) (int, error)
	Delete(ctx context.Context, messageIDs []string) (int, error)
	ProviderName() string
	Close() error
}

type AttachmentProvider interface {
	GetAttachment(ctx context.Context, messageID, attachmentID string) (*providerdata.AttachmentData, error)
}

type ActionResolution struct {
	OriginalMessageID string `json:"original_message_id"`
	ResolvedMessageID string `json:"resolved_message_id"`
}

type ResolvedArchiveProvider interface {
	ArchiveResolved(ctx context.Context, messageIDs []string) ([]ActionResolution, error)
}

type ResolvedMoveToInboxProvider interface {
	MoveToInboxResolved(ctx context.Context, messageIDs []string) ([]ActionResolution, error)
}

type ResolvedTrashProvider interface {
	TrashResolved(ctx context.Context, messageIDs []string) ([]ActionResolution, error)
}

type ResolvedNamedFolderProvider interface {
	MoveToFolderResolved(ctx context.Context, messageIDs []string, folder string) ([]ActionResolution, error)
}

type MessagePage struct {
	IDs           []string
	NextPageToken string
}

type MessagePageProvider interface {
	ListMessagesPage(ctx context.Context, opts SearchOptions, pageToken string) (MessagePage, error)
}

type FolderIncrementalSyncResult struct {
	Cursor     string
	IDs        []string
	DeletedIDs []string
	More       bool
}

type FolderIncrementalSyncProvider interface {
	SyncFolderChanges(ctx context.Context, folder, cursor string, maxChanges int) (FolderIncrementalSyncResult, error)
}

type MessageActionCapabilities struct {
	Provider              string `json:"provider,omitempty"`
	SupportsOpen          bool   `json:"supports_open"`
	SupportsArchive       bool   `json:"supports_archive"`
	SupportsDeleteToTrash bool   `json:"supports_delete_to_trash"`
	SupportsNativeDefer   bool   `json:"supports_native_defer"`
}

type MessageActionResult struct {
	Provider              string `json:"provider,omitempty"`
	Action                string `json:"action"`
	MessageID             string `json:"message_id"`
	Status                string `json:"status"`
	EffectiveProviderMode string `json:"effective_provider_mode"`
	DeferredUntilAt       string `json:"deferred_until_at,omitempty"`
	StubReason            string `json:"stub_reason,omitempty"`
	ErrorCode             string `json:"error_code,omitempty"`
	ErrorMessage          string `json:"error_message,omitempty"`
}

type MessageActionProvider interface {
	Defer(ctx context.Context, messageID string, untilAt time.Time) (MessageActionResult, error)
	SupportsNativeDefer() bool
}

type RawMessageProvider interface {
	ExportRawMessage(ctx context.Context, messageID string) ([]byte, error)
	ImportRawMessage(ctx context.Context, mimeContent []byte, folder string) (string, error)
}

type SearchOptions struct {
	Folder           string
	Text             string
	Subject          string
	From             string
	To               string
	After            time.Time
	Before           time.Time
	Since            time.Time
	Until            time.Time
	HasAttachment    *bool
	IsRead           *bool
	IsFlagged        *bool
	SizeGreater      int64
	SizeLess         int64
	LabelIDs         []string
	IncludeSpamTrash bool
	MaxResults       int64
}

func BoolPtr(b bool) *bool {
	return &b
}

func DefaultSearchOptions() SearchOptions {
	return SearchOptions{MaxResults: 100}
}

func (o SearchOptions) WithFolder(folder string) SearchOptions {
	o.Folder = folder
	return o
}

func (o SearchOptions) WithText(text string) SearchOptions {
	o.Text = text
	return o
}

func (o SearchOptions) WithSubject(subject string) SearchOptions {
	o.Subject = subject
	return o
}

func (o SearchOptions) WithFrom(from string) SearchOptions {
	o.From = from
	return o
}

func (o SearchOptions) WithTo(to string) SearchOptions {
	o.To = to
	return o
}

func (o SearchOptions) WithDateRange(after, before time.Time) SearchOptions {
	o.After = after
	o.Before = before
	return o
}

func (o SearchOptions) WithSince(since time.Time) SearchOptions {
	o.Since = since
	return o
}

func (o SearchOptions) WithLastDays(days int) SearchOptions {
	o.Since = time.Now().AddDate(0, 0, -days)
	return o
}

func (o SearchOptions) WithHasAttachment(has bool) SearchOptions {
	o.HasAttachment = &has
	return o
}

func (o SearchOptions) WithIsRead(read bool) SearchOptions {
	o.IsRead = &read
	return o
}

func (o SearchOptions) WithIsFlagged(flagged bool) SearchOptions {
	o.IsFlagged = &flagged
	return o
}

func (o SearchOptions) WithMaxResults(max int64) SearchOptions {
	o.MaxResults = max
	return o
}

func (o SearchOptions) WithIncludeSpamTrash(include bool) SearchOptions {
	o.IncludeSpamTrash = include
	return o
}

type ListMessageOptions struct {
	FolderID string
	Filter   string
	Select   []string
	Top      int
	Skip     int
}

type Folder struct {
	ID               string `json:"id"`
	DisplayName      string `json:"displayName"`
	WellKnownName    string `json:"wellKnownName"`
	ChildFolderCount int    `json:"childFolderCount"`
	TotalItemCount   int    `json:"totalItemCount"`
	UnreadItemCount  int    `json:"unreadItemCount"`
}

type Message struct {
	ID               string      `json:"id"`
	ConversationID   string      `json:"conversationId"`
	Subject          string      `json:"subject"`
	BodyPreview      string      `json:"bodyPreview"`
	Body             MessageBody `json:"body"`
	IsRead           bool        `json:"isRead"`
	HasAttachments   bool        `json:"hasAttachments"`
	Flag             MessageFlag `json:"flag"`
	ParentFolderID   string      `json:"parentFolderId"`
	ReceivedDateTime string      `json:"receivedDateTime"`
	WebLink          string      `json:"webLink"`
	From             *Recipient  `json:"from,omitempty"`
	ToRecipients     []Recipient `json:"toRecipients,omitempty"`
	CcRecipients     []Recipient `json:"ccRecipients,omitempty"`
}

type MessageBody struct {
	ContentType string `json:"contentType"`
	Content     string `json:"content"`
}

type MessageFlag struct {
	FlagStatus string `json:"flagStatus"`
}
