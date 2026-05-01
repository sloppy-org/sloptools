package ews

import (
	"context"
	"crypto/tls"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	ntlmssp "github.com/Azure/go-ntlmssp"
	"net/http"
	"net/http/cookiejar"
)

const (
	defaultEndpoint      = "https://exchange.tugraz.at/EWS/Exchange.asmx"
	defaultServerVersion = "Exchange2013"
	defaultBatchSize     = 50
	folderCacheTTL       = time.Minute
)

var xmlNumericEntityPattern = regexp.MustCompile(`&#(?:x([0-9A-Fa-f]+)|([0-9]+));`)

type Config struct {
	Endpoint      string
	Username      string
	Password      string
	ServerVersion string
	BatchSize     int
	InsecureTLS   bool
}

type Client struct {
	cfg                 Config
	httpClient          *http.Client
	streamingHTTPClient *http.Client
	session             *sharedSessionState
}

type sharedSessionState struct {
	requestGate  chan struct{}
	jar          http.CookieJar
	mu           sync.Mutex
	folderCached []Folder
	folderExpiry time.Time
}

var sharedSessions = struct {
	mu    sync.Mutex
	state map[string]*sharedSessionState
}{state: map[string]*sharedSessionState{}}

type BackoffError struct {
	Operation    string
	ResponseCode string
	Message      string
	Backoff      time.Duration
}

func (e *BackoffError) Error() string {
	if e == nil {
		return ""
	}
	parts := []string{"exchange server busy"}
	if clean := strings.TrimSpace(e.Operation); clean != "" {
		parts = append(parts, "during "+clean)
	}
	if e.Backoff > 0 {
		parts = append(parts, "retry after "+e.Backoff.Round(time.Second).String())
	}
	if clean := strings.TrimSpace(e.Message); clean != "" {
		parts = append(parts, clean)
	}
	return strings.Join(parts, ": ")
}

type SOAPFaultError struct {
	Operation    string
	StatusCode   int
	FaultCode    string
	ResponseCode string
	Message      string
}

func (e *SOAPFaultError) Error() string {
	if e == nil {
		return ""
	}
	if clean := strings.TrimSpace(e.Message); clean != "" {
		return clean
	}
	if clean := strings.TrimSpace(e.ResponseCode); clean != "" {
		return "exchange error: " + clean
	}
	if clean := strings.TrimSpace(e.FaultCode); clean != "" {
		return "exchange fault: " + clean
	}
	return "exchange soap fault"
}

type FolderKind string

const (
	FolderKindGeneric  FolderKind = "folder"
	FolderKindCalendar FolderKind = "calendar"
	FolderKindContacts FolderKind = "contacts"
	FolderKindTasks    FolderKind = "tasks"
)

type Folder struct {
	ID               string
	ChangeKey        string
	Name             string
	Kind             FolderKind
	TotalCount       int
	ChildFolderCount int
	UnreadCount      int
}

type Mailbox struct {
	Name        string
	Email       string
	RoutingType string
	MailboxType string
}

type Attachment struct {
	ID          string
	Name        string
	ContentType string
	Size        int64
	IsInline    bool
}

type AttachmentContent struct {
	ID          string
	Name        string
	ContentType string
	Size        int64
	IsInline    bool
	Content     []byte
}

type AttachmentFile struct {
	Name        string
	ContentType string
	Content     []byte // AttachmentFile describes a single file to attach via CreateAttachment.

	IsInline bool
}

type Message struct {
	ID                string
	ChangeKey         string
	ParentFolderID    string
	ConversationID    string
	ConversationTopic string
	InternetMessageID string
	Subject           string
	Body              string
	BodyType          string
	From              Mailbox
	Sender            Mailbox
	To                []Mailbox
	Cc                []Mailbox
	DisplayTo         string
	DisplayCc         string
	WebLink           string
	IsRead            bool
	IsDraft           bool
	HasAttachments    bool
	ReceivedAt        time.Time
	SentAt            time.Time
	CreatedAt         time.Time
	FlagStatus        string
	Attachments       []Attachment
}

type Contact struct {
	ID             string
	ChangeKey      string
	ParentFolderID string
	DisplayName    string
	CompanyName    string
	Email          string
	Phones         []string
}

type Event struct {
	ID             string
	ChangeKey      string
	ParentFolderID string
	Subject        string
	Body           string
	BodyType       string
	Location       string
	Start          time.Time
	End            time.Time
	IsAllDay       bool
	UID            string
	Organizer      string
	Attendees      []EventAttendee
	Recurrence     string
	Status         string
}

type EventAttendee struct {
	Email    string
	Name     string
	Response string
	Required bool
}

type Task struct {
	ID             string
	ChangeKey      string
	ParentFolderID string
	Subject        string
	Body           string
	BodyType       string
	Status         string
	StartDate      *time.Time
	DueDate        *time.Time
	CompleteDate   *time.Time
	IsComplete     bool
}

type Rule struct {
	ID         string
	Name       string
	Priority   int
	Enabled    bool
	Conditions RuleConditions
	Exceptions RuleConditions
	Actions    RuleActions
}

type RuleConditions struct {
	ContainsSenderStrings  []string
	ContainsSubjectStrings []string
	FromAddresses          []Mailbox
	SentToAddresses        []Mailbox
	NotSentToMe            bool
	SentCcMe               bool
}

type RuleActions struct {
	Delete               bool
	MarkAsRead           bool
	StopProcessingRules  bool
	MoveToFolderID       string
	RedirectToRecipients []Mailbox
}

type FindItemsResult struct {
	ItemIDs          []string
	NextOffset       int
	TotalItemsInView int
	IncludesLastPage bool
}

type SyncItemsResult struct {
	SyncState        string
	ItemIDs          []string
	DeletedItemIDs   []string
	IncludesLastItem bool
}

type DraftMessage struct {
	Subject    string
	MIME       []byte
	ThreadID   string
	InReplyTo  string
	References []string
}

type RuleOperationKind string

const (
	RuleOperationCreate RuleOperationKind = "create"
	RuleOperationSet    RuleOperationKind = "set"
	RuleOperationDelete RuleOperationKind = "delete"
)

type RuleOperation struct {
	Kind RuleOperationKind
	Rule Rule
}

type WatchOptions struct {
	SubscribeToAllFolders bool
	FolderIDs             []string
	ConnectionTimeout     time.Duration
}

type StreamEvent struct {
	Type              string
	ItemID            string
	OldItemID         string
	FolderID          string
	ParentFolderID    string
	OldParentFolderID string
	Watermark         string
}

type StreamBatch struct {
	SubscriptionID    string
	PreviousWatermark string
	MoreEvents        bool
	Events            []StreamEvent
}

func NewClient(cfg Config) (*Client, error) {
	cfg.Endpoint = strings.TrimSpace(cfg.Endpoint)
	cfg.Username = strings.TrimSpace(cfg.Username)
	cfg.Password = strings.TrimSpace(cfg.Password)
	cfg.ServerVersion = strings.TrimSpace(cfg.ServerVersion)
	if cfg.Endpoint == "" {
		cfg.Endpoint = defaultEndpoint
	}
	if cfg.ServerVersion == "" {
		cfg.ServerVersion = defaultServerVersion
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = defaultBatchSize
	}
	if cfg.Username == "" {
		return nil, fmt.Errorf("ews username is required")
	}
	if cfg.Password == "" {
		return nil, fmt.Errorf("ews password is required")
	}
	session, err := sharedSession(cfg)
	if err != nil {
		return nil, err
	}
	baseTransport := http.DefaultTransport.(*http.Transport).Clone()
	if cfg.InsecureTLS {
		baseTransport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	httpClient := &http.Client{Transport: ntlmssp.Negotiator{RoundTripper: baseTransport}, Timeout: 90 * time.Second, Jar: session.jar}
	streamingHTTPClient := &http.Client{Transport: ntlmssp.Negotiator{RoundTripper: baseTransport.Clone()}, Timeout: 35 * time.Minute, Jar: session.jar}
	return &Client{cfg: cfg, httpClient: httpClient, streamingHTTPClient: streamingHTTPClient, session: session}, nil
}

func (c *Client) Close() error {
	return nil
}

func sharedSession(cfg Config) (*sharedSessionState, error) {
	key := strings.TrimSpace(cfg.Endpoint) + "\n" + strings.TrimSpace(strings.ToLower(cfg.Username))
	sharedSessions.mu.Lock()
	defer sharedSessions.mu.Unlock()
	if existing := sharedSessions.state[key]; existing != nil {
		return existing, nil
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	state := &sharedSessionState{requestGate: make(chan struct{}, 1), jar: jar}
	sharedSessions.state[key] = state
	return state, nil
}

func (c *Client) cachedFolders() ([]Folder, bool) {
	if c == nil || c.session == nil {
		return nil, false
	}
	c.session.mu.Lock()
	defer c.session.mu.Unlock()
	if len(c.session.folderCached) == 0 || time.Now().After(c.session.folderExpiry) {
		return nil, false
	}
	return append([]Folder(nil), c.session.folderCached...), true
}

func (c *Client) storeCachedFolders(folders []Folder) {
	if c == nil || c.session == nil {
		return
	}
	c.session.mu.Lock()
	defer c.session.mu.Unlock()
	c.session.folderCached = append([]Folder(nil), folders...)
	c.session.folderExpiry = time.Now().Add(folderCacheTTL)
}

func (c *Client) ListFolders(ctx context.Context) ([]Folder, error) {
	if cached, ok := c.cachedFolders(); ok {
		return cached, nil
	}
	var resp findFolderEnvelope
	if err := c.call(ctx, "FindFolder", `<m:FindFolder Traversal="Deep">
      <m:FolderShape><t:BaseShape>Default</t:BaseShape></m:FolderShape>
      <m:ParentFolderIds><t:DistinguishedFolderId Id="msgfolderroot" /></m:ParentFolderIds>
    </m:FindFolder>`, &resp); err != nil {
		return nil, err
	}
	out := make([]Folder, 0, len(resp.Body.FindFolderResponse.ResponseMessages.Message.Root.Folders.Items))
	for _, raw := range resp.Body.FindFolderResponse.ResponseMessages.Message.Root.Folders.Items {
		out = append(out, raw.toFolder())
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	c.storeCachedFolders(out)
	return out, nil
}

func (c *Client) FindMessages(ctx context.Context, folderID string, offset, max int) (FindItemsResult, error) {
	if strings.TrimSpace(folderID) == "" {
		folderID = "inbox"
	}
	if max <= 0 {
		max = c.cfg.BatchSize
	}
	body := fmt.Sprintf(`<m:FindItem Traversal="Shallow">
      <m:ItemShape><t:BaseShape>IdOnly</t:BaseShape></m:ItemShape>
      <m:IndexedPageItemView MaxEntriesReturned="%d" Offset="%d" BasePoint="Beginning" />
      <m:ParentFolderIds>%s</m:ParentFolderIds>
    </m:FindItem>`, max, maxInt(offset, 0), folderIDXML(folderID))
	var resp findItemEnvelope
	if err := c.call(ctx, "FindItem", body, &resp); err != nil {
		return FindItemsResult{}, err
	}
	root := resp.Body.FindItemResponse.ResponseMessages.Message.Root
	out := FindItemsResult{NextOffset: root.IndexedPagingOffset, TotalItemsInView: root.TotalItemsInView, IncludesLastPage: root.IncludesLastItemInRange, ItemIDs: make([]string, 0, len(root.Items.Items))}
	for _, item := range root.Items.Items {
		if clean := strings.TrimSpace(item.ItemID.ID); clean != "" {
			out.ItemIDs = append(out.ItemIDs, clean)
		}
	}
	return out, nil
}

type FindRestriction struct {
	From          string
	HasAttachment *bool
	After         time.Time
	Before        time.Time
}

func (c *Client) FindMessagesRestricted(ctx context.Context, folderID string, offset, max int, restriction FindRestriction) (FindItemsResult, error) {
	if strings.TrimSpace(folderID) == "" {
		folderID = "inbox"
	}
	if max <= 0 {
		max = c.cfg.BatchSize
	}
	restrictionXML := buildRestrictionXML(restriction)
	body := fmt.Sprintf(`<m:FindItem Traversal="Shallow">
      <m:ItemShape><t:BaseShape>IdOnly</t:BaseShape></m:ItemShape>
      <m:IndexedPageItemView MaxEntriesReturned="%d" Offset="%d" BasePoint="Beginning" />%s
      <m:ParentFolderIds>%s</m:ParentFolderIds>
    </m:FindItem>`, max, maxInt(offset, 0), restrictionXML, folderIDXML(folderID))
	var resp findItemEnvelope
	if err := c.call(ctx, "FindItem", body, &resp); err != nil {
		return FindItemsResult{}, err
	}
	root := resp.Body.FindItemResponse.ResponseMessages.Message.Root
	out := FindItemsResult{NextOffset: root.IndexedPagingOffset, TotalItemsInView: root.TotalItemsInView, IncludesLastPage: root.IncludesLastItemInRange, ItemIDs: make([]string, 0, len(root.Items.Items))}
	for _, item := range root.Items.Items {
		if clean := strings.TrimSpace(item.ItemID.ID); clean != "" {
			out.ItemIDs = append(out.ItemIDs, clean)
		}
	}
	return out, nil
}

// senderStringPropertyTags lists the MAPI extended-property tags whose String
// values hold the sender's address or display name. EWS exposes message:From
// as a structured Mailbox element, so <t:Contains> against it never matches a
// substring; matching the underlying string properties does.
var senderStringPropertyTags = []string{
	"0x0C1F", // PR_SENDER_EMAIL_ADDRESS_W
	"0x0C1A", // PR_SENDER_NAME_W
	"0x0065", // PR_SENT_REPRESENTING_EMAIL_ADDRESS_W
	"0x0042", // PR_SENT_REPRESENTING_NAME_W
}

func buildRestrictionXML(r FindRestriction) string {
	var conditions []string
	if from := strings.TrimSpace(r.From); from != "" {
		conditions = append(conditions, buildFromRestrictionXML(from))
	}
	if r.HasAttachment != nil {
		value := "false"
		if *r.HasAttachment {
			value = "true"
		}
		conditions = append(conditions, `<t:IsEqualTo><t:FieldURI FieldURI="item:HasAttachments" /><t:FieldURIOrConstant><t:Constant Value="`+value+`" /></t:FieldURIOrConstant></t:IsEqualTo>`)
	}
	if !r.After.IsZero() {
		conditions = append(conditions, `<t:IsGreaterThanOrEqualTo><t:FieldURI FieldURI="item:DateTimeReceived" /><t:FieldURIOrConstant><t:Constant Value="`+r.After.UTC().Format(time.RFC3339)+`" /></t:FieldURIOrConstant></t:IsGreaterThanOrEqualTo>`)
	}
	if !r.Before.IsZero() {
		conditions = append(conditions, `<t:IsLessThanOrEqualTo><t:FieldURI FieldURI="item:DateTimeReceived" /><t:FieldURIOrConstant><t:Constant Value="`+r.Before.UTC().Format(time.RFC3339)+`" /></t:FieldURIOrConstant></t:IsLessThanOrEqualTo>`)
	}
	if len(conditions) == 0 {
		return ""
	}
	if len(conditions) == 1 {
		return "\n      <m:Restriction>" + conditions[0] + "</m:Restriction>"
	}
	return "\n      <m:Restriction><t:And>" + strings.Join(conditions, "") + "</t:And></m:Restriction>"
}

// buildFromRestrictionXML emits an <t:Or> of substring matches against the
// sender's address/name extended properties so a needle like "tugraz" matches
// whether it appears in PR_SENDER_EMAIL_ADDRESS_W or PR_SENDER_NAME_W (and the
// representing variants for messages sent on behalf of someone else).
func buildFromRestrictionXML(needle string) string {
	escaped := xmlEscapeAttr(needle)
	var b strings.Builder
	b.WriteString(`<t:Or>`)
	for _, tag := range senderStringPropertyTags {
		b.WriteString(`<t:Contains ContainmentMode="Substring" ContainmentComparison="IgnoreCase"><t:ExtendedFieldURI PropertyTag="`)
		b.WriteString(tag)
		b.WriteString(`" PropertyType="String" /><t:Constant Value="`)
		b.WriteString(escaped)
		b.WriteString(`" /></t:Contains>`)
	}
	b.WriteString(`</t:Or>`)
	return b.String()
}
