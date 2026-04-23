package email

import (
	"context"
	"errors"
	"time"

	"github.com/sloppy-org/sloptools/internal/providerdata"
)

// ErrCapabilityUnsupported signals that the provider does not implement a
// requested capability (for example Gmail has no flag "complete" status).
// MCP handlers translate this into a machine-readable capability_unsupported
// response so callers can distinguish a missing capability from a real error.
var ErrCapabilityUnsupported = errors.New("capability_unsupported")

// Flag status values accepted by FlagMutator.SetFlag. EWS supports all three;
// Gmail only supports notFlagged and flagged.
const (
	FlagStatusNotFlagged = "notFlagged"
	FlagStatusFlagged    = "flagged"
	FlagStatusComplete   = "complete"
)

// Flag carries the target flag status plus an optional due date used by
// backends that support follow-up flags (EWS). DueAt is ignored by backends
// that only model a boolean star/flag (Gmail).
type Flag struct {
	Status string
	DueAt  *time.Time
}

// FlagMutator is implemented by providers that can set or clear a follow-up
// flag on one or more messages. Backends that cannot honour a specific status
// must return ErrCapabilityUnsupported so MCP callers can react accordingly.
type FlagMutator interface {
	SetFlag(ctx context.Context, messageIDs []string, flag Flag) (int, error)
	ClearFlag(ctx context.Context, messageIDs []string) (int, error)
}

// CategoryMutator is implemented by providers that can replace the set of
// categories on one or more messages. Gmail maps categories onto user labels
// (creating them on demand); EWS writes to ItemCategories natively.
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

type Recipient struct {
	EmailAddress Address `json:"emailAddress"`
}

type Address struct {
	Name    string `json:"name"`
	Address string `json:"address"`
}
