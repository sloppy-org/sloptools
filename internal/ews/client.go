package ews

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Azure/go-ntlmssp"
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
	requestGate chan struct{}
	jar         http.CookieJar

	mu           sync.Mutex
	folderCached []Folder
	folderExpiry time.Time
}

var sharedSessions = struct {
	mu    sync.Mutex
	state map[string]*sharedSessionState
}{
	state: map[string]*sharedSessionState{},
}

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
		baseTransport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
	}
	httpClient := &http.Client{
		Transport: ntlmssp.Negotiator{RoundTripper: baseTransport},
		Timeout:   90 * time.Second,
		Jar:       session.jar,
	}
	streamingHTTPClient := &http.Client{
		Transport: ntlmssp.Negotiator{RoundTripper: baseTransport.Clone()},
		Timeout:   35 * time.Minute,
		Jar:       session.jar,
	}
	return &Client{cfg: cfg, httpClient: httpClient, streamingHTTPClient: streamingHTTPClient, session: session}, nil
}

func (c *Client) Close() error { return nil }

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
	state := &sharedSessionState{
		requestGate: make(chan struct{}, 1),
		jar:         jar,
	}
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
	out := FindItemsResult{
		NextOffset:       root.IndexedPagingOffset,
		TotalItemsInView: root.TotalItemsInView,
		IncludesLastPage: root.IncludesLastItemInRange,
		ItemIDs:          make([]string, 0, len(root.Items.Items)),
	}
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
	out := FindItemsResult{
		NextOffset:       root.IndexedPagingOffset,
		TotalItemsInView: root.TotalItemsInView,
		IncludesLastPage: root.IncludesLastItemInRange,
		ItemIDs:          make([]string, 0, len(root.Items.Items)),
	}
	for _, item := range root.Items.Items {
		if clean := strings.TrimSpace(item.ItemID.ID); clean != "" {
			out.ItemIDs = append(out.ItemIDs, clean)
		}
	}
	return out, nil
}

func buildRestrictionXML(r FindRestriction) string {
	var conditions []string
	if strings.TrimSpace(r.From) != "" {
		conditions = append(conditions, `<t:Contains ContainmentMode="Substring" ContainmentComparison="IgnoreCase"><t:FieldURI FieldURI="message:From" /><t:Constant Value="`+xmlEscapeAttr(r.From)+`" /></t:Contains>`)
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

func (c *Client) SyncFolderItems(ctx context.Context, folderID, syncState string, maxChanges int) (SyncItemsResult, error) {
	if strings.TrimSpace(folderID) == "" {
		folderID = "inbox"
	}
	if maxChanges <= 0 {
		maxChanges = c.cfg.BatchSize
	}
	body := `<m:SyncFolderItems><m:ItemShape><t:BaseShape>IdOnly</t:BaseShape></m:ItemShape><m:SyncFolderId>` +
		folderIDXML(folderID) +
		`</m:SyncFolderId>`
	if clean := strings.TrimSpace(syncState); clean != "" {
		body += `<m:SyncState>` + xmlEscapeText(clean) + `</m:SyncState>`
	}
	body += `<m:MaxChangesReturned>` + strconv.Itoa(maxChanges) + `</m:MaxChangesReturned></m:SyncFolderItems>`
	var resp syncFolderItemsEnvelope
	if err := c.call(ctx, "SyncFolderItems", body, &resp); err != nil {
		return SyncItemsResult{}, err
	}
	msg := resp.Body.SyncFolderItemsResponse.ResponseMessages.Message
	out := SyncItemsResult{
		SyncState:        strings.TrimSpace(msg.SyncState),
		IncludesLastItem: msg.IncludesLastItemInRange,
	}
	for _, change := range msg.Changes.Values {
		itemID := strings.TrimSpace(change.ResolveItemID())
		if itemID == "" {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(change.XMLName.Local)) {
		case "delete":
			out.DeletedItemIDs = append(out.DeletedItemIDs, itemID)
		default:
			out.ItemIDs = append(out.ItemIDs, itemID)
		}
	}
	return out, nil
}

func (c *Client) GetMessages(ctx context.Context, ids []string) ([]Message, error) {
	return c.getMessages(ctx, ids, true)
}

func (c *Client) GetMessageSummaries(ctx context.Context, ids []string) ([]Message, error) {
	return c.getMessages(ctx, ids, false)
}

func (c *Client) GetAttachment(ctx context.Context, attachmentID string) (AttachmentContent, error) {
	cleanID := strings.TrimSpace(attachmentID)
	if cleanID == "" {
		return AttachmentContent{}, fmt.Errorf("attachment id is required")
	}
	var resp getAttachmentEnvelope
	if err := c.call(ctx, "GetAttachment", getAttachmentBody(cleanID), &resp); err != nil {
		return AttachmentContent{}, err
	}
	files := resp.Body.GetAttachmentResponse.ResponseMessages.Message.Attachments.Files
	if len(files) == 0 {
		return AttachmentContent{}, fmt.Errorf("attachment %q not found", cleanID)
	}
	raw := files[0]
	content := []byte{}
	if encoded := strings.TrimSpace(raw.Content); encoded != "" {
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return AttachmentContent{}, err
		}
		content = decoded
	}
	return AttachmentContent{
		ID:          strings.TrimSpace(raw.ID.ID),
		Name:        strings.TrimSpace(raw.Name),
		ContentType: strings.TrimSpace(raw.ContentType),
		Size:        parseInt64(raw.Size),
		IsInline:    parseBool(raw.IsInline),
		Content:     content,
	}, nil
}

func (c *Client) GetMessageMIME(ctx context.Context, id string) ([]byte, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("message id is required")
	}
	var b strings.Builder
	b.WriteString(`<m:GetItem><m:ItemShape>`)
	b.WriteString(`<t:BaseShape>IdOnly</t:BaseShape>`)
	b.WriteString(`<t:IncludeMimeContent>true</t:IncludeMimeContent>`)
	b.WriteString(`</m:ItemShape><m:ItemIds>`)
	b.WriteString(`<t:ItemId Id="`)
	b.WriteString(xmlEscapeAttr(id))
	b.WriteString(`" />`)
	b.WriteString(`</m:ItemIds></m:GetItem>`)
	var resp getItemEnvelope
	if err := c.call(ctx, "GetItem", b.String(), &resp); err != nil {
		return nil, err
	}
	items := resp.Body.GetItemResponse.ResponseMessages.Message.Items.Values
	if len(items) == 0 {
		return nil, fmt.Errorf("message %q not found", id)
	}
	encoded := strings.TrimSpace(items[0].MimeContent)
	if encoded == "" {
		return nil, fmt.Errorf("message %q has no MIME content", id)
	}
	return base64.StdEncoding.DecodeString(encoded)
}

func (c *Client) CreateMessageInFolder(ctx context.Context, folderID string, mime []byte, markRead bool) (Message, error) {
	encoded := base64.StdEncoding.EncodeToString(mime)
	var b strings.Builder
	b.WriteString(`<m:CreateItem MessageDisposition="SaveOnly">`)
	b.WriteString(`<m:SavedItemFolderId>`)
	b.WriteString(folderIDXML(folderID))
	b.WriteString(`</m:SavedItemFolderId>`)
	b.WriteString(`<m:Items><t:Message>`)
	b.WriteString(`<t:MimeContent CharacterSet="UTF-8">`)
	b.WriteString(encoded)
	b.WriteString(`</t:MimeContent>`)
	if markRead {
		b.WriteString(`<t:IsRead>true</t:IsRead>`)
	}
	b.WriteString(`</t:Message></m:Items></m:CreateItem>`)
	var resp createItemEnvelope
	if err := c.call(ctx, "CreateItem", b.String(), &resp); err != nil {
		return Message{}, err
	}
	items := resp.Body.CreateItemResponse.ResponseMessages.Message.Items.Values
	if len(items) == 0 {
		return Message{}, fmt.Errorf("ews CreateItem returned no items")
	}
	return items[0].toMessage(), nil
}

func (c *Client) getMessages(ctx context.Context, ids []string, includeBody bool) ([]Message, error) {
	ids = compactStrings(ids)
	if len(ids) == 0 {
		return nil, nil
	}
	out := make([]Message, 0, len(ids))
	for start := 0; start < len(ids); start += c.cfg.BatchSize {
		end := start + c.cfg.BatchSize
		if end > len(ids) {
			end = len(ids)
		}
		var resp getItemEnvelope
		if err := c.call(ctx, "GetItem", getItemBody(ids[start:end], includeBody), &resp); err != nil {
			return nil, err
		}
		for _, raw := range resp.Body.GetItemResponse.ResponseMessages.Message.Items.Values {
			message := raw.toMessage()
			if !includeBody {
				message.Body = ""
				message.BodyType = ""
			}
			out = append(out, message)
		}
	}
	return out, nil
}

func (c *Client) GetContacts(ctx context.Context, folderID string, offset, max int) ([]Contact, error) {
	rawItems, err := c.getTypedItems(ctx, folderIDOrDistinguished(folderID, "contacts"), offset, max, "Contact")
	if err != nil {
		return nil, err
	}
	out := make([]Contact, 0, len(rawItems))
	for _, raw := range rawItems {
		out = append(out, raw.toContact())
	}
	return out, nil
}

func (c *Client) GetCalendarEvents(ctx context.Context, folderID string, offset, max int) ([]Event, error) {
	rawItems, err := c.getTypedItems(ctx, folderIDOrDistinguished(folderID, "calendar"), offset, max, "CalendarItem")
	if err != nil {
		return nil, err
	}
	out := make([]Event, 0, len(rawItems))
	for _, raw := range rawItems {
		out = append(out, raw.toEvent())
	}
	return out, nil
}

func (c *Client) GetTasks(ctx context.Context, folderID string, offset, max int) ([]Task, error) {
	rawItems, err := c.getTypedItems(ctx, folderIDOrDistinguished(folderID, "tasks"), offset, max, "Task")
	if err != nil {
		return nil, err
	}
	out := make([]Task, 0, len(rawItems))
	for _, raw := range rawItems {
		out = append(out, raw.toTask())
	}
	return out, nil
}

func (c *Client) GetInboxRules(ctx context.Context) ([]Rule, error) {
	var resp getInboxRulesEnvelope
	if err := c.call(ctx, "GetInboxRules", `<m:GetInboxRules />`, &resp); err != nil {
		return nil, err
	}
	out := make([]Rule, 0, len(resp.Body.GetInboxRulesResponse.Rules))
	for _, raw := range resp.Body.GetInboxRulesResponse.Rules {
		out = append(out, raw.toRule())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Priority < out[j].Priority })
	return out, nil
}

func (c *Client) CreateDraft(ctx context.Context, message DraftMessage) (Message, error) {
	body := `<m:CreateItem MessageDisposition="SaveOnly"><m:SavedItemFolderId><t:DistinguishedFolderId Id="drafts" /></m:SavedItemFolderId><m:Items>` +
		draftMessageXML(message) +
		`</m:Items></m:CreateItem>`
	var resp createItemEnvelope
	if err := c.call(ctx, "CreateItem", body, &resp); err != nil {
		return Message{}, err
	}
	if len(resp.Body.CreateItemResponse.ResponseMessages.Message.Items.Values) == 0 {
		return Message{}, fmt.Errorf("ews CreateItem returned no items")
	}
	return resp.Body.CreateItemResponse.ResponseMessages.Message.Items.Values[0].toMessage(), nil
}

func (c *Client) UpdateDraft(ctx context.Context, itemID string, message DraftMessage) (Message, error) {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return Message{}, fmt.Errorf("draft id is required")
	}
	body := `<m:UpdateItem MessageDisposition="SaveOnly" ConflictResolution="AlwaysOverwrite"><m:ItemChanges><t:ItemChange><t:ItemId Id="` +
		xmlEscapeAttr(itemID) +
		`" /><t:Updates>` +
		setDraftMimeContentXML(message) +
		`</t:Updates></t:ItemChange></m:ItemChanges></m:UpdateItem>`
	var resp updateItemEnvelope
	if err := c.call(ctx, "UpdateItem", body, &resp); err != nil {
		return Message{}, err
	}
	items, err := c.GetMessages(ctx, []string{itemID})
	if err != nil {
		return Message{}, err
	}
	if len(items) == 0 {
		return Message{}, fmt.Errorf("draft %q not found after update", itemID)
	}
	return items[0], nil
}

func (c *Client) SendDraft(ctx context.Context, itemID string) error {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return fmt.Errorf("draft id is required")
	}
	body := `<m:SendItem SaveItemToFolder="true"><m:ItemIds><t:ItemId Id="` +
		xmlEscapeAttr(itemID) +
		`" /></m:ItemIds><m:SavedItemFolderId><t:DistinguishedFolderId Id="sentitems" /></m:SavedItemFolderId></m:SendItem>`
	var resp simpleResponseEnvelope
	return c.call(ctx, "SendItem", body, &resp)
}

func (c *Client) UpdateInboxRules(ctx context.Context, operations []RuleOperation) error {
	opsXML, err := ruleOperationsXML(operations)
	if err != nil {
		return err
	}
	body := `<m:UpdateInboxRules><m:RemoveOutlookRuleBlob>true</m:RemoveOutlookRuleBlob><m:Operations>` +
		opsXML +
		`</m:Operations></m:UpdateInboxRules>`
	var resp updateInboxRulesEnvelope
	return c.call(ctx, "UpdateInboxRules", body, &resp)
}

func (c *Client) MoveItems(ctx context.Context, ids []string, folderID string) ([]string, error) {
	ids = compactStrings(ids)
	if len(ids) == 0 {
		return nil, nil
	}
	body := `<m:MoveItem><m:ToFolderId>` + folderIDXML(folderID) + `</m:ToFolderId><m:ItemIds>` + itemIDsXML(ids) + `</m:ItemIds></m:MoveItem>`
	var resp moveItemEnvelope
	if err := c.call(ctx, "MoveItem", body, &resp); err != nil {
		return nil, err
	}
	return resp.ResolvedItemIDs(), nil
}

func (c *Client) DeleteItems(ctx context.Context, ids []string, hardDelete bool) error {
	ids = compactStrings(ids)
	if len(ids) == 0 {
		return nil
	}
	deleteType := "MoveToDeletedItems"
	if hardDelete {
		deleteType = "HardDelete"
	}
	body := fmt.Sprintf(`<m:DeleteItem DeleteType="%s" SendMeetingCancellations="SendToNone" AffectedTaskOccurrences="AllOccurrences"><m:ItemIds>%s</m:ItemIds></m:DeleteItem>`, deleteType, itemIDsXML(ids))
	var resp simpleResponseEnvelope
	return c.call(ctx, "DeleteItem", body, &resp)
}

func (c *Client) SetReadState(ctx context.Context, ids []string, isRead bool) error {
	ids = compactStrings(ids)
	if len(ids) == 0 {
		return nil
	}
	value := "false"
	if isRead {
		value = "true"
	}
	var changes strings.Builder
	for _, id := range ids {
		changes.WriteString(`<t:ItemChange><t:ItemId Id="`)
		changes.WriteString(xmlEscapeAttr(id))
		changes.WriteString(`" /><t:Updates><t:SetItemField><t:FieldURI FieldURI="message:IsRead" /><t:Message><t:IsRead>`)
		changes.WriteString(value)
		changes.WriteString(`</t:IsRead></t:Message></t:SetItemField></t:Updates></t:ItemChange>`)
	}
	body := `<m:UpdateItem MessageDisposition="SaveOnly" ConflictResolution="AlwaysOverwrite"><m:ItemChanges>` + changes.String() + `</m:ItemChanges></m:UpdateItem>`
	var resp updateItemEnvelope
	return c.call(ctx, "UpdateItem", body, &resp)
}

func (c *Client) FindFolderByName(ctx context.Context, name string) (*Folder, error) {
	target := strings.ToLower(strings.TrimSpace(name))
	if target == "" {
		return nil, nil
	}
	folders, err := c.ListFolders(ctx)
	if err != nil {
		return nil, err
	}
	for i := range folders {
		if strings.EqualFold(strings.TrimSpace(folders[i].Name), target) {
			return &folders[i], nil
		}
	}
	return nil, nil
}

func (c *Client) Watch(ctx context.Context, opts WatchOptions, onEvents func(StreamBatch) error) error {
	if onEvents == nil {
		return fmt.Errorf("ews watch callback is required")
	}
	subscriptionID, err := c.subscribeStreaming(ctx, opts)
	if err != nil {
		return err
	}
	connectionTimeout := streamingConnectionTimeoutMinutes(opts.ConnectionTimeout)
	for {
		batch, err := c.getStreamingEvents(ctx, subscriptionID, connectionTimeout)
		if err != nil {
			return err
		}
		if len(batch.Events) == 0 {
			continue
		}
		if err := onEvents(batch); err != nil {
			return err
		}
	}
}

func (c *Client) getTypedItems(ctx context.Context, folderID string, offset, max int, expectedLocal string) ([]itemXML, error) {
	found, err := c.FindMessages(ctx, folderID, offset, max)
	if err != nil {
		return nil, err
	}
	if len(found.ItemIDs) == 0 {
		return nil, nil
	}
	var resp getItemEnvelope
	if err := c.call(ctx, "GetItem", getItemBody(found.ItemIDs, true), &resp); err != nil {
		return nil, err
	}
	out := make([]itemXML, 0, len(resp.Body.GetItemResponse.ResponseMessages.Message.Items.Values))
	for _, raw := range resp.Body.GetItemResponse.ResponseMessages.Message.Items.Values {
		if strings.EqualFold(raw.XMLName.Local, expectedLocal) {
			out = append(out, raw)
		}
	}
	return out, nil
}

func (c *Client) call(ctx context.Context, soapAction, innerXML string, target any) error {
	return c.callWithHTTPClient(ctx, c.httpClient, soapAction, innerXML, target)
}

func (c *Client) callWithHTTPClient(ctx context.Context, client *http.Client, soapAction, innerXML string, target any) error {
	release, err := c.acquireRequestSlot(ctx, client)
	if err != nil {
		return err
	}
	defer release()

	body := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
               xmlns:m="http://schemas.microsoft.com/exchange/services/2006/messages"
               xmlns:t="http://schemas.microsoft.com/exchange/services/2006/types"
               xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Header><t:RequestServerVersion Version="%s" /></soap:Header>
  <soap:Body>%s</soap:Body>
</soap:Envelope>`, xmlEscapeAttr(c.cfg.ServerVersion), innerXML)

	// Exchange sometimes returns a bare 401 on write operations when the
	// cached affinity/NTLM session cookie has expired server-side. The
	// negotiator won't re-handshake without a WWW-Authenticate hint, so
	// drop the session cookies and retry once with a fresh handshake.
	var sanitized []byte
	var status int
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.Endpoint, strings.NewReader(body))
		if err != nil {
			return err
		}
		req.SetBasicAuth(c.cfg.Username, c.cfg.Password)
		req.Header.Set("Content-Type", "text/xml; charset=utf-8")
		req.Header.Set("Accept", "text/xml")
		req.Header.Set("SOAPAction", fmt.Sprintf(`"http://schemas.microsoft.com/exchange/services/2006/messages/%s"`, soapAction))
		if mailbox := strings.TrimSpace(c.cfg.Username); mailbox != "" {
			req.Header.Set("X-AnchorMailbox", mailbox)
			req.Header.Set("X-PreferServerAffinity", "true")
		}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		data, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return readErr
		}
		sanitized, _ = sanitizeXML10Document(data)
		status = resp.StatusCode
		if status == http.StatusUnauthorized && attempt == 0 {
			c.resetSessionCookies(client)
			continue
		}
		break
	}
	if faultErr := parseSOAPFaultError(soapAction, status, sanitized); faultErr != nil {
		return faultErr
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("ews http %d: %s", status, strings.TrimSpace(string(sanitized)))
	}
	if err := xml.Unmarshal(sanitized, target); err != nil {
		return fmt.Errorf("decode ews %s response: %w", soapAction, err)
	}
	if rc := responseCode(target); rc != "" && !strings.EqualFold(rc, "NoError") {
		return fmt.Errorf("ews %s: %s", soapAction, rc)
	}
	return nil
}

// resetSessionCookies drops the cached cookie jar shared across all clients
// for this mailbox so the next request re-runs the NTLM handshake. Only the
// passed-in http.Client's Jar field is swapped; peer clients that still point
// at the previous jar will pick up the fresh jar on their next call because
// the shared session holds the canonical reference.
func (c *Client) resetSessionCookies(client *http.Client) {
	if c == nil || c.session == nil {
		return
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return
	}
	c.session.mu.Lock()
	c.session.jar = jar
	c.session.mu.Unlock()
	if client != nil {
		client.Jar = jar
	}
	if c.httpClient != nil && c.httpClient != client {
		c.httpClient.Jar = jar
	}
	if c.streamingHTTPClient != nil && c.streamingHTTPClient != client {
		c.streamingHTTPClient.Jar = jar
	}
}

func (c *Client) acquireRequestSlot(ctx context.Context, client *http.Client) (func(), error) {
	if c == nil || c.session == nil || client == nil || client == c.streamingHTTPClient {
		return func() {}, nil
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case c.session.requestGate <- struct{}{}:
		return func() {
			select {
			case <-c.session.requestGate:
			default:
			}
		}, nil
	}
}

type soapFaultEnvelope struct {
	Body struct {
		Fault struct {
			FaultCode   string `xml:"faultcode"`
			FaultString string `xml:"faultstring"`
			Detail      struct {
				ResponseCode string `xml:"ResponseCode"`
				Message      string `xml:"Message"`
				MessageXML   struct {
					Values []soapFaultMessageXMLValue `xml:"Value"`
				} `xml:"MessageXml"`
			} `xml:"detail"`
		} `xml:"Fault"`
	} `xml:"Body"`
}

type soapFaultMessageXMLValue struct {
	Name  string `xml:"Name,attr"`
	Value string `xml:",chardata"`
}

func parseSOAPFaultError(operation string, statusCode int, data []byte) error {
	if !bytes.Contains(data, []byte("<Fault")) && !bytes.Contains(data, []byte(":Fault")) {
		return nil
	}
	var env soapFaultEnvelope
	if err := xml.Unmarshal(data, &env); err != nil {
		return &SOAPFaultError{
			Operation:  operation,
			StatusCode: statusCode,
			Message:    strings.TrimSpace(string(data)),
		}
	}
	fault := env.Body.Fault
	message := strings.TrimSpace(fault.Detail.Message)
	if message == "" {
		message = strings.TrimSpace(fault.FaultString)
	}
	responseCode := strings.TrimSpace(fault.Detail.ResponseCode)
	if strings.EqualFold(responseCode, "ErrorServerBusy") {
		backoff := parseSOAPFaultBackoff(fault.Detail.MessageXML.Values)
		return &BackoffError{
			Operation:    operation,
			ResponseCode: responseCode,
			Message:      message,
			Backoff:      backoff,
		}
	}
	return &SOAPFaultError{
		Operation:    operation,
		StatusCode:   statusCode,
		FaultCode:    strings.TrimSpace(fault.FaultCode),
		ResponseCode: responseCode,
		Message:      message,
	}
}

func parseSOAPFaultBackoff(values []soapFaultMessageXMLValue) time.Duration {
	for _, value := range values {
		if !strings.EqualFold(strings.TrimSpace(value.Name), "BackOffMilliseconds") {
			continue
		}
		ms, err := strconv.Atoi(strings.TrimSpace(value.Value))
		if err != nil || ms <= 0 {
			return 0
		}
		return time.Duration(ms) * time.Millisecond
	}
	return 0
}

func sanitizeXML10Document(data []byte) ([]byte, int) {
	if len(data) == 0 {
		return data, 0
	}
	original := data
	removed := 0
	data, refsRemoved := sanitizeIllegalXML10EntityRefs(data)
	removed += refsRemoved
	var out bytes.Buffer
	out.Grow(len(data))
	for len(data) > 0 {
		r, size := utf8.DecodeRune(data)
		if r == utf8.RuneError && size == 1 {
			removed++
			data = data[1:]
			continue
		}
		if isXML10Rune(r) {
			out.WriteRune(r)
		} else {
			removed++
		}
		data = data[size:]
	}
	if removed == 0 {
		return original, 0
	}
	return out.Bytes(), removed
}

func sanitizeIllegalXML10EntityRefs(data []byte) ([]byte, int) {
	removed := 0
	sanitized := xmlNumericEntityPattern.ReplaceAllFunc(data, func(match []byte) []byte {
		groups := xmlNumericEntityPattern.FindSubmatch(match)
		if len(groups) != 3 {
			return match
		}
		base := 10
		raw := groups[2]
		if len(groups[1]) > 0 {
			base = 16
			raw = groups[1]
		}
		value, err := strconv.ParseInt(string(raw), base, 32)
		if err != nil {
			removed++
			return nil
		}
		if isXML10Rune(rune(value)) {
			return match
		}
		removed++
		return nil
	})
	return sanitized, removed
}

func isXML10Rune(r rune) bool {
	switch {
	case r == 0x9 || r == 0xA || r == 0xD:
		return true
	case r >= 0x20 && r <= 0xD7FF:
		return true
	case r >= 0xE000 && r <= 0xFFFD:
		return true
	case r >= 0x10000 && r <= 0x10FFFF:
		return true
	default:
		return false
	}
}

func (c *Client) subscribeStreaming(ctx context.Context, opts WatchOptions) (string, error) {
	body := streamingSubscribeBody(opts)
	var resp subscribeEnvelope
	if err := c.call(ctx, "Subscribe", body, &resp); err != nil {
		return "", err
	}
	return strings.TrimSpace(resp.Body.SubscribeResponse.ResponseMessages.Message.SubscriptionID), nil
}

func (c *Client) getStreamingEvents(ctx context.Context, subscriptionID string, connectionTimeout int) (StreamBatch, error) {
	body := fmt.Sprintf(`<m:GetStreamingEvents><m:SubscriptionIds><t:SubscriptionId>%s</t:SubscriptionId></m:SubscriptionIds><m:ConnectionTimeout>%d</m:ConnectionTimeout></m:GetStreamingEvents>`,
		xmlEscapeText(strings.TrimSpace(subscriptionID)),
		connectionTimeout,
	)
	var resp getStreamingEventsEnvelope
	if err := c.callWithHTTPClient(ctx, c.streamingHTTPClient, "GetStreamingEvents", body, &resp); err != nil {
		return StreamBatch{}, err
	}
	return resp.Body.GetStreamingEventsResponse.ResponseMessages.Message.toBatch(), nil
}

func getItemBody(ids []string, includeBody bool) string {
	var b strings.Builder
	b.WriteString(`<m:GetItem><m:ItemShape>`)
	b.WriteString(`<t:BaseShape>AllProperties</t:BaseShape><t:BodyType>Text</t:BodyType>`)
	b.WriteString(`</m:ItemShape><m:ItemIds>`)
	for _, id := range ids {
		b.WriteString(`<t:ItemId Id="`)
		b.WriteString(xmlEscapeAttr(id))
		b.WriteString(`" />`)
	}
	b.WriteString(`</m:ItemIds></m:GetItem>`)
	return b.String()
}

func getAttachmentBody(attachmentID string) string {
	var b strings.Builder
	b.WriteString(`<m:GetAttachment><m:AttachmentIds><t:AttachmentId Id="`)
	b.WriteString(xmlEscapeAttr(strings.TrimSpace(attachmentID)))
	b.WriteString(`" /></m:AttachmentIds></m:GetAttachment>`)
	return b.String()
}

func draftMessageXML(message DraftMessage) string {
	encoded := base64.StdEncoding.EncodeToString(message.MIME)
	var b strings.Builder
	b.WriteString(`<t:Message>`)
	if strings.TrimSpace(message.Subject) != "" {
		b.WriteString(`<t:Subject>`)
		b.WriteString(xmlEscapeText(strings.TrimSpace(message.Subject)))
		b.WriteString(`</t:Subject>`)
	}
	if encoded != "" {
		b.WriteString(`<t:MimeContent CharacterSet="UTF-8">`)
		b.WriteString(encoded)
		b.WriteString(`</t:MimeContent>`)
	}
	b.WriteString(`</t:Message>`)
	return b.String()
}

func setDraftMimeContentXML(message DraftMessage) string {
	encoded := base64.StdEncoding.EncodeToString(message.MIME)
	var b strings.Builder
	if strings.TrimSpace(message.Subject) != "" {
		b.WriteString(`<t:SetItemField><t:FieldURI FieldURI="item:Subject" /><t:Message><t:Subject>`)
		b.WriteString(xmlEscapeText(strings.TrimSpace(message.Subject)))
		b.WriteString(`</t:Subject></t:Message></t:SetItemField>`)
	}
	b.WriteString(`<t:SetItemField><t:FieldURI FieldURI="item:MimeContent" /><t:Message><t:MimeContent CharacterSet="UTF-8">`)
	b.WriteString(encoded)
	b.WriteString(`</t:MimeContent></t:Message></t:SetItemField>`)
	return b.String()
}

func folderIDXML(folderID string) string {
	folderID = strings.TrimSpace(folderID)
	if folderID == "" {
		return `<t:DistinguishedFolderId Id="inbox" />`
	}
	switch strings.ToLower(folderID) {
	case "inbox", "calendar", "contacts", "tasks", "drafts", "sentitems", "deleteditems", "junkemail", "msgfolderroot":
		return fmt.Sprintf(`<t:DistinguishedFolderId Id="%s" />`, xmlEscapeAttr(strings.ToLower(folderID)))
	default:
		return fmt.Sprintf(`<t:FolderId Id="%s" />`, xmlEscapeAttr(folderID))
	}
}

func folderRefXML(folderID string) string {
	folderID = strings.TrimSpace(folderID)
	if folderID == "" {
		return ""
	}
	switch strings.ToLower(folderID) {
	case "inbox", "calendar", "contacts", "tasks", "drafts", "sentitems", "deleteditems", "junkemail", "msgfolderroot":
		return fmt.Sprintf(`<t:DistinguishedFolderId Id="%s" />`, xmlEscapeAttr(strings.ToLower(folderID)))
	default:
		return fmt.Sprintf(`<t:FolderId Id="%s" />`, xmlEscapeAttr(folderID))
	}
}

func streamingConnectionTimeoutMinutes(value time.Duration) int {
	if value <= 0 {
		return 29
	}
	minutes := int(value / time.Minute)
	if minutes <= 0 {
		minutes = 1
	}
	if minutes > 30 {
		minutes = 30
	}
	return minutes
}

func streamingSubscribeBody(opts WatchOptions) string {
	subscribeAll := opts.SubscribeToAllFolders || len(opts.FolderIDs) == 0
	var b strings.Builder
	b.WriteString(`<m:Subscribe><m:StreamingSubscriptionRequest`)
	if subscribeAll {
		b.WriteString(` SubscribeToAllFolders="true"`)
	}
	b.WriteString(`>`)
	if !subscribeAll {
		b.WriteString(`<m:FolderIds>`)
		for _, folderID := range compactStrings(opts.FolderIDs) {
			b.WriteString(folderIDXML(folderID))
		}
		b.WriteString(`</m:FolderIds>`)
	}
	b.WriteString(`<t:EventTypes>`)
	for _, eventType := range []string{
		"CopiedEvent",
		"CreatedEvent",
		"DeletedEvent",
		"FreeBusyChangedEvent",
		"ModifiedEvent",
		"MovedEvent",
		"NewMailEvent",
	} {
		b.WriteString(`<t:EventType>`)
		b.WriteString(eventType)
		b.WriteString(`</t:EventType>`)
	}
	b.WriteString(`</t:EventTypes></m:StreamingSubscriptionRequest></m:Subscribe>`)
	return b.String()
}

func folderIDOrDistinguished(folderID, fallback string) string {
	if strings.TrimSpace(folderID) != "" {
		return folderID
	}
	return fallback
}

func compactStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		clean := strings.TrimSpace(value)
		if clean == "" {
			continue
		}
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		out = append(out, clean)
	}
	return out
}

func itemIDsXML(ids []string) string {
	var b strings.Builder
	for _, id := range ids {
		b.WriteString(`<t:ItemId Id="`)
		b.WriteString(xmlEscapeAttr(id))
		b.WriteString(`" />`)
	}
	return b.String()
}

func maxInt(v, fallback int) int {
	if v > 0 {
		return v
	}
	return fallback
}

func xmlEscapeAttr(value string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(strings.TrimSpace(value)))
	return strings.ReplaceAll(b.String(), `"`, "&quot;")
}

func xmlEscapeText(value string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(value))
	return b.String()
}

func parseTime(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC()
		}
	}
	return time.Time{}
}

func parseBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1":
		return true
	default:
		return false
	}
}

func parseInt(value string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(value))
	return n
}

func parseInt64(value string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	return n
}

func mailboxSlice(values []mailboxXML) []Mailbox {
	out := make([]Mailbox, 0, len(values))
	for _, value := range values {
		out = append(out, value.toMailbox())
	}
	return out
}

func responseCode(target any) string {
	switch typed := target.(type) {
	case *findFolderEnvelope:
		return typed.Body.FindFolderResponse.ResponseMessages.Message.ResponseCode
	case *findItemEnvelope:
		return typed.Body.FindItemResponse.ResponseMessages.Message.ResponseCode
	case *getItemEnvelope:
		return typed.Body.GetItemResponse.ResponseMessages.Message.ResponseCode
	case *getInboxRulesEnvelope:
		return typed.Body.GetInboxRulesResponse.ResponseCode
	case *simpleResponseEnvelope:
		return typed.Body.Response.Messages.ResponseCode
	case *updateItemEnvelope:
		return typed.Body.Response.Messages.FirstCode()
	case *subscribeEnvelope:
		return typed.Body.SubscribeResponse.ResponseMessages.Message.ResponseCode
	case *getStreamingEventsEnvelope:
		return typed.Body.GetStreamingEventsResponse.ResponseMessages.Message.ResponseCode
	case *createItemEnvelope:
		return typed.Body.CreateItemResponse.ResponseMessages.Message.ResponseCode
	case *moveItemEnvelope:
		return typed.Body.MoveItemResponse.ResponseMessages.FirstCode()
	case *updateInboxRulesEnvelope:
		return typed.Body.UpdateInboxRulesResponse.ResponseCode
	default:
		return ""
	}
}

type simpleResponseEnvelope struct {
	Body struct {
		Response struct {
			Messages struct {
				ResponseCode string `xml:"ResponseCode"`
			} `xml:",any"`
		} `xml:"Body"`
	} `xml:"Body"`
}

type updateItemEnvelope struct {
	Body struct {
		Response struct {
			Messages updateResponseMessages `xml:"UpdateItemResponse>ResponseMessages"`
		} `xml:"Body"`
	} `xml:"Body"`
}

type createItemEnvelope struct {
	Body struct {
		CreateItemResponse struct {
			ResponseMessages struct {
				Message struct {
					ResponseCode string `xml:"ResponseCode"`
					Items        struct {
						Values []itemXML `xml:",any"`
					} `xml:"Items"`
				} `xml:"CreateItemResponseMessage"`
			} `xml:"ResponseMessages"`
		} `xml:"CreateItemResponse"`
	} `xml:"Body"`
}

type moveItemEnvelope struct {
	Body struct {
		MoveItemResponse struct {
			ResponseMessages moveItemResponseMessages `xml:"ResponseMessages"`
		} `xml:"MoveItemResponse"`
	} `xml:"Body"`
}

func (e *moveItemEnvelope) ResolvedItemIDs() []string {
	if e == nil {
		return nil
	}
	return e.Body.MoveItemResponse.ResponseMessages.ResolvedItemIDs()
}

type moveItemResponseMessages struct {
	Items []struct {
		ResponseCode string `xml:"ResponseCode"`
		Items        struct {
			Values []itemXML `xml:",any"`
		} `xml:"Items"`
	} `xml:",any"`
}

func (m moveItemResponseMessages) FirstCode() string {
	for _, item := range m.Items {
		if clean := strings.TrimSpace(item.ResponseCode); clean != "" {
			return clean
		}
	}
	return ""
}

func (m moveItemResponseMessages) ResolvedItemIDs() []string {
	var ids []string
	for _, item := range m.Items {
		for _, value := range item.Items.Values {
			if id := strings.TrimSpace(value.ItemID.ID); id != "" {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

type updateResponseMessages struct {
	Items []struct {
		ResponseCode string `xml:"ResponseCode"`
	} `xml:",any"`
}

func (m updateResponseMessages) FirstCode() string {
	for _, item := range m.Items {
		if clean := strings.TrimSpace(item.ResponseCode); clean != "" {
			return clean
		}
	}
	return ""
}

type folderIDXMLNode struct {
	ID        string `xml:"Id,attr"`
	ChangeKey string `xml:"ChangeKey,attr"`
}

type mailboxXML struct {
	Name        string `xml:"Name"`
	Email       string `xml:"EmailAddress"`
	RoutingType string `xml:"RoutingType"`
	MailboxType string `xml:"MailboxType"`
}

func (m mailboxXML) toMailbox() Mailbox {
	return Mailbox{
		Name:        strings.TrimSpace(m.Name),
		Email:       strings.TrimSpace(m.Email),
		RoutingType: strings.TrimSpace(m.RoutingType),
		MailboxType: strings.TrimSpace(m.MailboxType),
	}
}

type findFolderEnvelope struct {
	Body struct {
		FindFolderResponse struct {
			ResponseMessages struct {
				Message struct {
					ResponseCode string `xml:"ResponseCode"`
					Root         struct {
						Folders struct {
							Items []folderXML `xml:",any"`
						} `xml:"Folders"`
					} `xml:"RootFolder"`
				} `xml:"FindFolderResponseMessage"`
			} `xml:"ResponseMessages"`
		} `xml:"FindFolderResponse"`
	} `xml:"Body"`
}

type folderXML struct {
	XMLName          xml.Name        `xml:""`
	FolderID         folderIDXMLNode `xml:"FolderId"`
	DisplayName      string          `xml:"DisplayName"`
	TotalCount       string          `xml:"TotalCount"`
	ChildFolderCount string          `xml:"ChildFolderCount"`
	UnreadCount      string          `xml:"UnreadCount"`
}

func (f folderXML) toFolder() Folder {
	kind := FolderKindGeneric
	switch strings.ToLower(strings.TrimSpace(f.XMLName.Local)) {
	case "calendarfolder":
		kind = FolderKindCalendar
	case "contactsfolder":
		kind = FolderKindContacts
	case "tasksfolder":
		kind = FolderKindTasks
	}
	return Folder{
		ID:               strings.TrimSpace(f.FolderID.ID),
		ChangeKey:        strings.TrimSpace(f.FolderID.ChangeKey),
		Name:             strings.TrimSpace(f.DisplayName),
		Kind:             kind,
		TotalCount:       parseInt(f.TotalCount),
		ChildFolderCount: parseInt(f.ChildFolderCount),
		UnreadCount:      parseInt(f.UnreadCount),
	}
}

type findItemEnvelope struct {
	Body struct {
		FindItemResponse struct {
			ResponseMessages struct {
				Message struct {
					ResponseCode string `xml:"ResponseCode"`
					Root         struct {
						IndexedPagingOffset     int  `xml:"IndexedPagingOffset,attr"`
						TotalItemsInView        int  `xml:"TotalItemsInView,attr"`
						IncludesLastItemInRange bool `xml:"IncludesLastItemInRange,attr"`
						Items                   struct {
							Items []itemIDOnlyXML `xml:",any"`
						} `xml:"Items"`
					} `xml:"RootFolder"`
				} `xml:"FindItemResponseMessage"`
			} `xml:"ResponseMessages"`
		} `xml:"FindItemResponse"`
	} `xml:"Body"`
}

type itemIDOnlyXML struct {
	XMLName xml.Name `xml:""`
	ItemID  struct {
		ID string `xml:"Id,attr"`
	} `xml:"ItemId"`
}

type getItemEnvelope struct {
	Body struct {
		GetItemResponse struct {
			ResponseMessages struct {
				Message struct {
					ResponseCode string `xml:"ResponseCode"`
					Items        struct {
						Values []itemXML `xml:",any"`
					} `xml:"Items"`
				} `xml:"GetItemResponseMessage"`
			} `xml:"ResponseMessages"`
		} `xml:"GetItemResponse"`
	} `xml:"Body"`
}

type getAttachmentEnvelope struct {
	Body struct {
		GetAttachmentResponse struct {
			ResponseMessages struct {
				Message struct {
					ResponseCode string `xml:"ResponseCode"`
					Attachments  struct {
						Files []attachmentContentXML `xml:"FileAttachment"`
					} `xml:"Attachments"`
				} `xml:"GetAttachmentResponseMessage"`
			} `xml:"ResponseMessages"`
		} `xml:"GetAttachmentResponse"`
	} `xml:"Body"`
}

type syncFolderItemsEnvelope struct {
	Body struct {
		SyncFolderItemsResponse struct {
			ResponseMessages struct {
				Message struct {
					ResponseCode            string `xml:"ResponseCode"`
					SyncState               string `xml:"SyncState"`
					IncludesLastItemInRange bool   `xml:"IncludesLastItemInRange"`
					Changes                 struct {
						Values []syncChangeXML `xml:",any"`
					} `xml:"Changes"`
				} `xml:"SyncFolderItemsResponseMessage"`
			} `xml:"ResponseMessages"`
		} `xml:"SyncFolderItemsResponse"`
	} `xml:"Body"`
}

type syncChangeXML struct {
	XMLName xml.Name `xml:""`
	Message struct {
		ItemID folderIDXMLNode `xml:"ItemId"`
	} `xml:"Message"`
	ItemID folderIDXMLNode `xml:"ItemId"`
}

func (c syncChangeXML) ResolveItemID() string {
	if clean := strings.TrimSpace(c.Message.ItemID.ID); clean != "" {
		return clean
	}
	return strings.TrimSpace(c.ItemID.ID)
}

type itemXML struct {
	XMLName        xml.Name        `xml:""`
	ItemID         folderIDXMLNode `xml:"ItemId"`
	MimeContent    string          `xml:"MimeContent"`
	ParentFolderID folderIDXMLNode `xml:"ParentFolderId"`
	ConversationID struct {
		ID string `xml:"Id,attr"`
	} `xml:"ConversationId"`
	Subject string `xml:"Subject"`
	Body    struct {
		Type  string `xml:"BodyType,attr"`
		Value string `xml:",chardata"`
	} `xml:"Body"`
	DisplayTo         string `xml:"DisplayTo"`
	DisplayCc         string `xml:"DisplayCc"`
	WebLink           string `xml:"WebClientReadFormQueryString"`
	InternetMessageID string `xml:"InternetMessageId"`
	DateTimeReceived  string `xml:"DateTimeReceived"`
	DateTimeSent      string `xml:"DateTimeSent"`
	DateTimeCreated   string `xml:"DateTimeCreated"`
	IsRead            string `xml:"IsRead"`
	IsDraft           string `xml:"IsDraft"`
	HasAttachments    string `xml:"HasAttachments"`
	ConversationTopic string `xml:"ConversationTopic"`
	From              struct {
		Mailbox mailboxXML `xml:"Mailbox"`
	} `xml:"From"`
	Sender struct {
		Mailbox mailboxXML `xml:"Mailbox"`
	} `xml:"Sender"`
	ToRecipients struct {
		Mailboxes []mailboxXML `xml:"Mailbox"`
	} `xml:"ToRecipients"`
	CcRecipients struct {
		Mailboxes []mailboxXML `xml:"Mailbox"`
	} `xml:"CcRecipients"`
	Flag struct {
		Status string `xml:"FlagStatus"`
	} `xml:"Flag"`
	Attachments struct {
		Files []attachmentXML `xml:"FileAttachment"`
	} `xml:"Attachments"`
	CompanyName    string `xml:"CompanyName"`
	EmailAddresses struct {
		Entries []contactEmailXML `xml:"Entry"`
	} `xml:"EmailAddresses"`
	PhoneNumbers struct {
		Entries []labeledStringXML `xml:"Entry"`
	} `xml:"PhoneNumbers"`
	PhysicalAddresses struct{} `xml:"PhysicalAddresses"`
	Location          string   `xml:"Location"`
	Start             string   `xml:"Start"`
	End               string   `xml:"End"`
	IsAllDayEvent     string   `xml:"IsAllDayEvent"`
	Status            string   `xml:"Status"`
	StartDate         string   `xml:"StartDate"`
	DueDate           string   `xml:"DueDate"`
	CompleteDate      string   `xml:"CompleteDate"`
}

type attachmentXML struct {
	ID struct {
		ID string `xml:"Id,attr"`
	} `xml:"AttachmentId"`
	Name        string `xml:"Name"`
	ContentType string `xml:"ContentType"`
	Size        string `xml:"Size"`
	IsInline    string `xml:"IsInline"`
}

type attachmentContentXML struct {
	ID struct {
		ID string `xml:"Id,attr"`
	} `xml:"AttachmentId"`
	Name        string `xml:"Name"`
	ContentType string `xml:"ContentType"`
	Content     string `xml:"Content"`
	Size        string `xml:"Size"`
	IsInline    string `xml:"IsInline"`
}

type contactEmailXML struct {
	Key   string `xml:"Key,attr"`
	Name  string `xml:"Name"`
	Value string `xml:"EmailAddress"`
}

type labeledStringXML struct {
	Key   string `xml:"Key,attr"`
	Value string `xml:",chardata"`
}

func (i itemXML) toMessage() Message {
	attachments := make([]Attachment, 0, len(i.Attachments.Files))
	for _, attachment := range i.Attachments.Files {
		attachments = append(attachments, Attachment{
			ID:          strings.TrimSpace(attachment.ID.ID),
			Name:        strings.TrimSpace(attachment.Name),
			ContentType: strings.TrimSpace(attachment.ContentType),
			Size:        parseInt64(attachment.Size),
			IsInline:    parseBool(attachment.IsInline),
		})
	}
	return Message{
		ID:                strings.TrimSpace(i.ItemID.ID),
		ChangeKey:         strings.TrimSpace(i.ItemID.ChangeKey),
		ParentFolderID:    strings.TrimSpace(i.ParentFolderID.ID),
		ConversationID:    strings.TrimSpace(i.ConversationID.ID),
		ConversationTopic: strings.TrimSpace(i.ConversationTopic),
		InternetMessageID: strings.TrimSpace(i.InternetMessageID),
		Subject:           strings.TrimSpace(i.Subject),
		Body:              strings.TrimSpace(i.Body.Value),
		BodyType:          strings.TrimSpace(i.Body.Type),
		From:              i.From.Mailbox.toMailbox(),
		Sender:            i.Sender.Mailbox.toMailbox(),
		To:                mailboxSlice(i.ToRecipients.Mailboxes),
		Cc:                mailboxSlice(i.CcRecipients.Mailboxes),
		DisplayTo:         strings.TrimSpace(i.DisplayTo),
		DisplayCc:         strings.TrimSpace(i.DisplayCc),
		WebLink:           strings.TrimSpace(i.WebLink),
		IsRead:            parseBool(i.IsRead),
		IsDraft:           parseBool(i.IsDraft),
		HasAttachments:    parseBool(i.HasAttachments),
		ReceivedAt:        parseTime(i.DateTimeReceived),
		SentAt:            parseTime(i.DateTimeSent),
		CreatedAt:         parseTime(i.DateTimeCreated),
		FlagStatus:        strings.TrimSpace(i.Flag.Status),
		Attachments:       attachments,
	}
}

func (i itemXML) toContact() Contact {
	emailAddress := ""
	for _, entry := range i.EmailAddresses.Entries {
		if clean := strings.TrimSpace(entry.Value); clean != "" {
			emailAddress = clean
			break
		}
	}
	phones := make([]string, 0, len(i.PhoneNumbers.Entries))
	for _, entry := range i.PhoneNumbers.Entries {
		if clean := strings.TrimSpace(entry.Value); clean != "" {
			phones = append(phones, clean)
		}
	}
	return Contact{
		ID:             strings.TrimSpace(i.ItemID.ID),
		ChangeKey:      strings.TrimSpace(i.ItemID.ChangeKey),
		ParentFolderID: strings.TrimSpace(i.ParentFolderID.ID),
		DisplayName:    strings.TrimSpace(i.Subject),
		CompanyName:    strings.TrimSpace(i.CompanyName),
		Email:          emailAddress,
		Phones:         phones,
	}
}

func (i itemXML) toEvent() Event {
	return Event{
		ID:             strings.TrimSpace(i.ItemID.ID),
		ChangeKey:      strings.TrimSpace(i.ItemID.ChangeKey),
		ParentFolderID: strings.TrimSpace(i.ParentFolderID.ID),
		Subject:        strings.TrimSpace(i.Subject),
		Body:           strings.TrimSpace(i.Body.Value),
		BodyType:       strings.TrimSpace(i.Body.Type),
		Location:       strings.TrimSpace(i.Location),
		Start:          parseTime(i.Start),
		End:            parseTime(i.End),
		IsAllDay:       parseBool(i.IsAllDayEvent),
	}
}

func (i itemXML) toTask() Task {
	start := parseTime(i.StartDate)
	due := parseTime(i.DueDate)
	complete := parseTime(i.CompleteDate)
	return Task{
		ID:             strings.TrimSpace(i.ItemID.ID),
		ChangeKey:      strings.TrimSpace(i.ItemID.ChangeKey),
		ParentFolderID: strings.TrimSpace(i.ParentFolderID.ID),
		Subject:        strings.TrimSpace(i.Subject),
		Body:           strings.TrimSpace(i.Body.Value),
		BodyType:       strings.TrimSpace(i.Body.Type),
		Status:         strings.TrimSpace(i.Status),
		StartDate:      timePtrIfSet(start),
		DueDate:        timePtrIfSet(due),
		CompleteDate:   timePtrIfSet(complete),
		IsComplete:     strings.EqualFold(strings.TrimSpace(i.Status), "Completed") || !complete.IsZero(),
	}
}

func timePtrIfSet(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	v := value
	return &v
}

type getInboxRulesEnvelope struct {
	Body struct {
		GetInboxRulesResponse struct {
			ResponseCode string    `xml:"ResponseCode"`
			Rules        []ruleXML `xml:"InboxRules>Rule"`
		} `xml:"GetInboxRulesResponse"`
	} `xml:"Body"`
}

type updateInboxRulesEnvelope struct {
	Body struct {
		UpdateInboxRulesResponse struct {
			ResponseCode string `xml:"ResponseCode"`
		} `xml:"UpdateInboxRulesResponse"`
	} `xml:"Body"`
}

type subscribeEnvelope struct {
	Body struct {
		SubscribeResponse struct {
			ResponseMessages struct {
				Message struct {
					ResponseCode   string `xml:"ResponseCode"`
					SubscriptionID string `xml:"SubscriptionId"`
				} `xml:"SubscribeResponseMessage"`
			} `xml:"ResponseMessages"`
		} `xml:"SubscribeResponse"`
	} `xml:"Body"`
}

type getStreamingEventsEnvelope struct {
	Body struct {
		GetStreamingEventsResponse struct {
			ResponseMessages struct {
				Message streamingResponseMessageXML `xml:"GetStreamingEventsResponseMessage"`
			} `xml:"ResponseMessages"`
		} `xml:"GetStreamingEventsResponse"`
	} `xml:"Body"`
}

type streamingResponseMessageXML struct {
	ResponseCode  string                     `xml:"ResponseCode"`
	Notifications []streamingNotificationXML `xml:"Notifications>Notification"`
}

func (m streamingResponseMessageXML) toBatch() StreamBatch {
	out := StreamBatch{}
	for _, notification := range m.Notifications {
		batch := notification.toBatch()
		if out.SubscriptionID == "" {
			out.SubscriptionID = batch.SubscriptionID
		}
		if out.PreviousWatermark == "" {
			out.PreviousWatermark = batch.PreviousWatermark
		}
		out.MoreEvents = out.MoreEvents || batch.MoreEvents
		out.Events = append(out.Events, batch.Events...)
	}
	return out
}

type streamingNotificationXML struct {
	SubscriptionID    string              `xml:"SubscriptionId"`
	PreviousWatermark string              `xml:"PreviousWatermark"`
	MoreEvents        string              `xml:"MoreEvents"`
	CopiedEvents      []streamingEventXML `xml:"CopiedEvent"`
	CreatedEvents     []streamingEventXML `xml:"CreatedEvent"`
	DeletedEvents     []streamingEventXML `xml:"DeletedEvent"`
	FreeBusyEvents    []streamingEventXML `xml:"FreeBusyChangedEvent"`
	ModifiedEvents    []streamingEventXML `xml:"ModifiedEvent"`
	MovedEvents       []streamingEventXML `xml:"MovedEvent"`
	NewMailEvents     []streamingEventXML `xml:"NewMailEvent"`
}

func (n streamingNotificationXML) toBatch() StreamBatch {
	out := StreamBatch{
		SubscriptionID:    strings.TrimSpace(n.SubscriptionID),
		PreviousWatermark: strings.TrimSpace(n.PreviousWatermark),
		MoreEvents:        parseBool(n.MoreEvents),
	}
	out.Events = append(out.Events, appendStreamingEvents("copied", n.CopiedEvents)...)
	out.Events = append(out.Events, appendStreamingEvents("created", n.CreatedEvents)...)
	out.Events = append(out.Events, appendStreamingEvents("deleted", n.DeletedEvents)...)
	out.Events = append(out.Events, appendStreamingEvents("free_busy_changed", n.FreeBusyEvents)...)
	out.Events = append(out.Events, appendStreamingEvents("modified", n.ModifiedEvents)...)
	out.Events = append(out.Events, appendStreamingEvents("moved", n.MovedEvents)...)
	out.Events = append(out.Events, appendStreamingEvents("new_mail", n.NewMailEvents)...)
	return out
}

type streamingEventXML struct {
	Watermark         string          `xml:"Watermark"`
	ItemID            folderIDXMLNode `xml:"ItemId"`
	OldItemID         folderIDXMLNode `xml:"OldItemId"`
	FolderID          folderIDXMLNode `xml:"FolderId"`
	ParentFolderID    folderIDXMLNode `xml:"ParentFolderId"`
	OldParentFolderID folderIDXMLNode `xml:"OldParentFolderId"`
}

func appendStreamingEvents(eventType string, values []streamingEventXML) []StreamEvent {
	out := make([]StreamEvent, 0, len(values))
	for _, value := range values {
		out = append(out, StreamEvent{
			Type:              eventType,
			ItemID:            strings.TrimSpace(value.ItemID.ID),
			OldItemID:         strings.TrimSpace(value.OldItemID.ID),
			FolderID:          strings.TrimSpace(value.FolderID.ID),
			ParentFolderID:    strings.TrimSpace(value.ParentFolderID.ID),
			OldParentFolderID: strings.TrimSpace(value.OldParentFolderID.ID),
			Watermark:         strings.TrimSpace(value.Watermark),
		})
	}
	return out
}

type ruleXML struct {
	ID         string            `xml:"RuleId"`
	Name       string            `xml:"DisplayName"`
	Priority   string            `xml:"Priority"`
	IsEnabled  string            `xml:"IsEnabled"`
	Conditions ruleConditionsXML `xml:"Conditions"`
	Exceptions ruleConditionsXML `xml:"Exceptions"`
	Actions    ruleActionsXML    `xml:"Actions"`
}

type ruleConditionsXML struct {
	ContainsSenderStrings  []string     `xml:"ContainsSenderStrings>String"`
	ContainsSubjectStrings []string     `xml:"ContainsSubjectStrings>String"`
	FromAddresses          []mailboxXML `xml:"FromAddresses>Address"`
	SentToAddresses        []mailboxXML `xml:"SentToAddresses>Address"`
	NotSentToMe            string       `xml:"NotSentToMe"`
	SentCcMe               string       `xml:"SentCcMe"`
}

type ruleActionsXML struct {
	Delete              string `xml:"Delete"`
	MarkAsRead          string `xml:"MarkAsRead"`
	StopProcessingRules string `xml:"StopProcessingRules"`
	MoveToFolder        struct {
		FolderID folderIDXMLNode `xml:"FolderId"`
	} `xml:"MoveToFolder"`
	RedirectToRecipients []mailboxXML `xml:"RedirectToRecipients>Address"`
}

func (r ruleXML) toRule() Rule {
	return Rule{
		ID:       strings.TrimSpace(r.ID),
		Name:     strings.TrimSpace(r.Name),
		Priority: parseInt(r.Priority),
		Enabled:  parseBool(r.IsEnabled),
		Conditions: RuleConditions{
			ContainsSenderStrings:  compactStrings(r.Conditions.ContainsSenderStrings),
			ContainsSubjectStrings: compactStrings(r.Conditions.ContainsSubjectStrings),
			FromAddresses:          mailboxSlice(r.Conditions.FromAddresses),
			SentToAddresses:        mailboxSlice(r.Conditions.SentToAddresses),
			NotSentToMe:            parseBool(r.Conditions.NotSentToMe),
			SentCcMe:               parseBool(r.Conditions.SentCcMe),
		},
		Exceptions: RuleConditions{
			ContainsSenderStrings:  compactStrings(r.Exceptions.ContainsSenderStrings),
			ContainsSubjectStrings: compactStrings(r.Exceptions.ContainsSubjectStrings),
			FromAddresses:          mailboxSlice(r.Exceptions.FromAddresses),
			SentToAddresses:        mailboxSlice(r.Exceptions.SentToAddresses),
			NotSentToMe:            parseBool(r.Exceptions.NotSentToMe),
			SentCcMe:               parseBool(r.Exceptions.SentCcMe),
		},
		Actions: RuleActions{
			Delete:               parseBool(r.Actions.Delete),
			MarkAsRead:           parseBool(r.Actions.MarkAsRead),
			StopProcessingRules:  parseBool(r.Actions.StopProcessingRules),
			MoveToFolderID:       strings.TrimSpace(r.Actions.MoveToFolder.FolderID.ID),
			RedirectToRecipients: mailboxSlice(r.Actions.RedirectToRecipients),
		},
	}
}

func ruleOperationsXML(operations []RuleOperation) (string, error) {
	var b strings.Builder
	for _, operation := range operations {
		switch operation.Kind {
		case RuleOperationCreate:
			b.WriteString(`<t:CreateRuleOperation><t:Rule>`)
			b.WriteString(ruleXMLBody(operation.Rule, false))
			b.WriteString(`</t:Rule></t:CreateRuleOperation>`)
		case RuleOperationSet:
			if strings.TrimSpace(operation.Rule.ID) == "" {
				return "", fmt.Errorf("rule id is required for set operation")
			}
			b.WriteString(`<t:SetRuleOperation><t:Rule>`)
			b.WriteString(ruleXMLBody(operation.Rule, true))
			b.WriteString(`</t:Rule></t:SetRuleOperation>`)
		case RuleOperationDelete:
			if strings.TrimSpace(operation.Rule.ID) == "" {
				return "", fmt.Errorf("rule id is required for delete operation")
			}
			b.WriteString(`<t:DeleteRuleOperation><t:RuleId>`)
			b.WriteString(xmlEscapeText(strings.TrimSpace(operation.Rule.ID)))
			b.WriteString(`</t:RuleId></t:DeleteRuleOperation>`)
		default:
			return "", fmt.Errorf("unsupported rule operation %q", operation.Kind)
		}
	}
	return b.String(), nil
}

func ruleXMLBody(rule Rule, includeID bool) string {
	var b strings.Builder
	if includeID && strings.TrimSpace(rule.ID) != "" {
		b.WriteString(`<t:RuleId>`)
		b.WriteString(xmlEscapeText(strings.TrimSpace(rule.ID)))
		b.WriteString(`</t:RuleId>`)
	}
	b.WriteString(`<t:DisplayName>`)
	b.WriteString(xmlEscapeText(strings.TrimSpace(rule.Name)))
	b.WriteString(`</t:DisplayName><t:Priority>`)
	b.WriteString(strconv.Itoa(rule.Priority))
	b.WriteString(`</t:Priority><t:IsEnabled>`)
	if rule.Enabled {
		b.WriteString(`true`)
	} else {
		b.WriteString(`false`)
	}
	b.WriteString(`</t:IsEnabled>`)
	b.WriteString(ruleConditionsBodyXML(rule.Conditions))
	b.WriteString(ruleExceptionsBodyXML(rule.Exceptions))
	b.WriteString(ruleActionsBodyXML(rule.Actions))
	return b.String()
}

func ruleConditionsBodyXML(conditions RuleConditions) string {
	var b strings.Builder
	b.WriteString(`<t:Conditions>`)
	if len(conditions.ContainsSenderStrings) > 0 {
		b.WriteString(`<t:ContainsSenderStrings>`)
		for _, value := range compactStrings(conditions.ContainsSenderStrings) {
			b.WriteString(`<t:String>`)
			b.WriteString(xmlEscapeText(value))
			b.WriteString(`</t:String>`)
		}
		b.WriteString(`</t:ContainsSenderStrings>`)
	}
	if len(conditions.ContainsSubjectStrings) > 0 {
		b.WriteString(`<t:ContainsSubjectStrings>`)
		for _, value := range compactStrings(conditions.ContainsSubjectStrings) {
			b.WriteString(`<t:String>`)
			b.WriteString(xmlEscapeText(value))
			b.WriteString(`</t:String>`)
		}
		b.WriteString(`</t:ContainsSubjectStrings>`)
	}
	if len(conditions.FromAddresses) > 0 {
		b.WriteString(`<t:FromAddresses>`)
		for _, mailbox := range conditions.FromAddresses {
			b.WriteString(mailboxAddressXML(mailbox))
		}
		b.WriteString(`</t:FromAddresses>`)
	}
	if len(conditions.SentToAddresses) > 0 {
		b.WriteString(`<t:SentToAddresses>`)
		for _, mailbox := range conditions.SentToAddresses {
			b.WriteString(mailboxAddressXML(mailbox))
		}
		b.WriteString(`</t:SentToAddresses>`)
	}
	if conditions.NotSentToMe {
		b.WriteString(`<t:NotSentToMe>true</t:NotSentToMe>`)
	}
	if conditions.SentCcMe {
		b.WriteString(`<t:SentCcMe>true</t:SentCcMe>`)
	}
	b.WriteString(`</t:Conditions>`)
	return b.String()
}

func ruleExceptionsBodyXML(conditions RuleConditions) string {
	if len(conditions.ContainsSenderStrings) == 0 &&
		len(conditions.ContainsSubjectStrings) == 0 &&
		len(conditions.FromAddresses) == 0 &&
		len(conditions.SentToAddresses) == 0 &&
		!conditions.NotSentToMe &&
		!conditions.SentCcMe {
		return `<t:Exceptions />`
	}
	var b strings.Builder
	b.WriteString(`<t:Exceptions>`)
	if len(conditions.ContainsSenderStrings) > 0 {
		b.WriteString(`<t:ContainsSenderStrings>`)
		for _, value := range compactStrings(conditions.ContainsSenderStrings) {
			b.WriteString(`<t:String>`)
			b.WriteString(xmlEscapeText(value))
			b.WriteString(`</t:String>`)
		}
		b.WriteString(`</t:ContainsSenderStrings>`)
	}
	if len(conditions.ContainsSubjectStrings) > 0 {
		b.WriteString(`<t:ContainsSubjectStrings>`)
		for _, value := range compactStrings(conditions.ContainsSubjectStrings) {
			b.WriteString(`<t:String>`)
			b.WriteString(xmlEscapeText(value))
			b.WriteString(`</t:String>`)
		}
		b.WriteString(`</t:ContainsSubjectStrings>`)
	}
	if len(conditions.FromAddresses) > 0 {
		b.WriteString(`<t:FromAddresses>`)
		for _, mailbox := range conditions.FromAddresses {
			b.WriteString(mailboxAddressXML(mailbox))
		}
		b.WriteString(`</t:FromAddresses>`)
	}
	if len(conditions.SentToAddresses) > 0 {
		b.WriteString(`<t:SentToAddresses>`)
		for _, mailbox := range conditions.SentToAddresses {
			b.WriteString(mailboxAddressXML(mailbox))
		}
		b.WriteString(`</t:SentToAddresses>`)
	}
	if conditions.NotSentToMe {
		b.WriteString(`<t:NotSentToMe>true</t:NotSentToMe>`)
	}
	if conditions.SentCcMe {
		b.WriteString(`<t:SentCcMe>true</t:SentCcMe>`)
	}
	b.WriteString(`</t:Exceptions>`)
	return b.String()
}

func ruleActionsBodyXML(actions RuleActions) string {
	var b strings.Builder
	b.WriteString(`<t:Actions>`)
	if actions.Delete {
		b.WriteString(`<t:Delete>true</t:Delete>`)
	}
	if actions.MarkAsRead {
		b.WriteString(`<t:MarkAsRead>true</t:MarkAsRead>`)
	}
	if actions.StopProcessingRules {
		b.WriteString(`<t:StopProcessingRules>true</t:StopProcessingRules>`)
	}
	if folderXML := folderRefXML(actions.MoveToFolderID); folderXML != "" {
		b.WriteString(`<t:MoveToFolder>`)
		b.WriteString(folderXML)
		b.WriteString(`</t:MoveToFolder>`)
	}
	if len(actions.RedirectToRecipients) > 0 {
		b.WriteString(`<t:RedirectToRecipients>`)
		for _, mailbox := range actions.RedirectToRecipients {
			b.WriteString(mailboxAddressXML(mailbox))
		}
		b.WriteString(`</t:RedirectToRecipients>`)
	}
	b.WriteString(`</t:Actions>`)
	return b.String()
}

func mailboxAddressXML(mailbox Mailbox) string {
	var b strings.Builder
	b.WriteString(`<t:Address>`)
	if strings.TrimSpace(mailbox.Name) != "" {
		b.WriteString(`<t:Name>`)
		b.WriteString(xmlEscapeText(strings.TrimSpace(mailbox.Name)))
		b.WriteString(`</t:Name>`)
	}
	if strings.TrimSpace(mailbox.Email) != "" {
		b.WriteString(`<t:EmailAddress>`)
		b.WriteString(xmlEscapeText(strings.TrimSpace(mailbox.Email)))
		b.WriteString(`</t:EmailAddress>`)
	}
	if strings.TrimSpace(mailbox.RoutingType) != "" {
		b.WriteString(`<t:RoutingType>`)
		b.WriteString(xmlEscapeText(strings.TrimSpace(mailbox.RoutingType)))
		b.WriteString(`</t:RoutingType>`)
	}
	if strings.TrimSpace(mailbox.MailboxType) != "" {
		b.WriteString(`<t:MailboxType>`)
		b.WriteString(xmlEscapeText(strings.TrimSpace(mailbox.MailboxType)))
		b.WriteString(`</t:MailboxType>`)
	}
	b.WriteString(`</t:Address>`)
	return b.String()
}
