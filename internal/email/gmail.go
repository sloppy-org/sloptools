package email

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/mail"
	"strings"
	"sync"
	"time"

	"github.com/krystophny/sloppy/internal/googleauth"
	"github.com/krystophny/sloppy/internal/providerdata"
	"golang.org/x/oauth2"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

var gmailScopes = append([]string(nil), googleauth.DefaultScopes...)

func configDir() string {
	return defaultSlopshellConfigDir()
}

// GmailClient provides access to Gmail API with rate limiting.
type GmailClient struct {
	rateLimiter     *RateLimiter
	auth            *googleauth.Session
	credentialsPath string
	tokenPath       string
}

// Compile-time check that GmailClient implements EmailProvider.
var _ EmailProvider = (*GmailClient)(nil)
var _ MessageActionProvider = (*GmailClient)(nil)
var _ MessagePageProvider = (*GmailClient)(nil)
var _ NamedLabelProvider = (*GmailClient)(nil)
var _ ServerFilterProvider = (*GmailClient)(nil)
var _ RawMessageProvider = (*GmailClient)(nil)

// NewGmail creates a new Gmail client.
func NewGmail() (*GmailClient, error) {
	return NewGmailWithFiles("", "")
}

// NewGmailWithFiles creates a Gmail client with explicit credential and token paths.
func NewGmailWithFiles(credentialsPath, tokenPath string) (*GmailClient, error) {
	auth, err := googleauth.New(credentialsPath, tokenPath, gmailScopes)
	if err != nil {
		return nil, err
	}
	client := &GmailClient{
		rateLimiter:     NewRateLimiter(15000),
		auth:            auth,
		credentialsPath: auth.CredentialsPath(),
		tokenPath:       auth.TokenPath(),
	}
	return client, nil
}

// GetAuthURL returns the URL for OAuth authorization.
func (c *GmailClient) GetAuthURL() string {
	return c.auth.GetAuthURL()
}

// ExchangeCode exchanges an authorization code for a token.
func (c *GmailClient) ExchangeCode(ctx context.Context, code string) error {
	return c.auth.ExchangeCode(ctx, code)
}

func (c *GmailClient) getTokenSource(ctx context.Context) (oauth2.TokenSource, error) {
	return c.auth.TokenSource(ctx)
}

func (c *GmailClient) getService(ctx context.Context) (*gmail.Service, error) {
	tokenSource, err := c.getTokenSource(ctx)
	if err != nil {
		return nil, err
	}
	return gmail.NewService(ctx, option.WithTokenSource(tokenSource))
}

// ListLabels returns all Gmail labels.
func (c *GmailClient) ListLabels(ctx context.Context) ([]providerdata.Label, error) {
	service, err := c.getService(ctx)
	if err != nil {
		return nil, err
	}

	c.rateLimiter.Acquire("labels.list")

	result, err := service.Users.Labels.List("me").Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("failed to list labels: %w", err)
	}

	labels := make([]providerdata.Label, 0, len(result.Labels))
	for _, lbl := range result.Labels {
		labels = append(labels, providerdata.Label{
			ID:             lbl.Id,
			Name:           lbl.Name,
			Type:           lbl.Type,
			MessagesTotal:  int(lbl.MessagesTotal),
			MessagesUnread: int(lbl.MessagesUnread),
		})
	}

	return labels, nil
}

// ListMessages returns message IDs matching the search options.
func (c *GmailClient) ListMessages(ctx context.Context, opts SearchOptions) ([]string, error) {
	service, err := c.getService(ctx)
	if err != nil {
		return nil, err
	}

	maxResults := opts.MaxResults
	if maxResults == 0 {
		maxResults = 100
	}

	query := buildGmailQuery(opts)

	var messageIDs []string
	pageToken := ""

	for int64(len(messageIDs)) < maxResults {
		c.rateLimiter.Acquire("messages.list")

		call := service.Users.Messages.List("me").
			Context(ctx).
			MaxResults(minInt64(500, maxResults-int64(len(messageIDs)))).
			IncludeSpamTrash(opts.IncludeSpamTrash)

		if query != "" {
			call = call.Q(query)
		}
		if len(opts.LabelIDs) > 0 {
			call = call.LabelIds(opts.LabelIDs...)
		}
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}

		result, err := call.Do()
		if err != nil {
			return nil, fmt.Errorf("failed to list messages: %w", err)
		}

		for _, msg := range result.Messages {
			if int64(len(messageIDs)) >= maxResults {
				break
			}
			messageIDs = append(messageIDs, msg.Id)
		}

		pageToken = result.NextPageToken
		if pageToken == "" {
			break
		}
	}

	return messageIDs, nil
}

func (c *GmailClient) ListMessagesPage(ctx context.Context, opts SearchOptions, pageToken string) (MessagePage, error) {
	service, err := c.getService(ctx)
	if err != nil {
		return MessagePage{}, err
	}

	maxResults := opts.MaxResults
	if maxResults <= 0 {
		maxResults = 100
	}
	query := buildGmailQuery(opts)

	c.rateLimiter.Acquire("messages.list")
	call := service.Users.Messages.List("me").
		Context(ctx).
		MaxResults(minInt64(500, maxResults)).
		IncludeSpamTrash(opts.IncludeSpamTrash)
	if query != "" {
		call = call.Q(query)
	}
	if len(opts.LabelIDs) > 0 {
		call = call.LabelIds(opts.LabelIDs...)
	}
	if strings.TrimSpace(pageToken) != "" {
		call = call.PageToken(strings.TrimSpace(pageToken))
	}
	result, err := call.Do()
	if err != nil {
		return MessagePage{}, fmt.Errorf("failed to list messages: %w", err)
	}
	page := MessagePage{
		IDs:           make([]string, 0, len(result.Messages)),
		NextPageToken: strings.TrimSpace(result.NextPageToken),
	}
	for _, msg := range result.Messages {
		if id := strings.TrimSpace(msg.Id); id != "" {
			page.IDs = append(page.IDs, id)
		}
	}
	return page, nil
}

// buildGmailQuery converts SearchOptions to a Gmail query string.
func buildGmailQuery(opts SearchOptions) string {
	var parts []string

	if opts.Folder != "" {
		parts = append(parts, fmt.Sprintf("label:%s", opts.Folder))
	}

	if opts.Text != "" {
		parts = append(parts, opts.Text)
	}

	if opts.Subject != "" {
		parts = append(parts, fmt.Sprintf("subject:%s", opts.Subject))
	}

	if opts.From != "" {
		parts = append(parts, fmt.Sprintf("from:%s", opts.From))
	}

	if opts.To != "" {
		parts = append(parts, fmt.Sprintf("to:%s", opts.To))
	}

	if !opts.After.IsZero() {
		parts = append(parts, fmt.Sprintf("after:%s", opts.After.Format("2006/01/02")))
	}

	if !opts.Before.IsZero() {
		parts = append(parts, fmt.Sprintf("before:%s", opts.Before.Format("2006/01/02")))
	}

	if !opts.Since.IsZero() {
		parts = append(parts, fmt.Sprintf("after:%s", opts.Since.AddDate(0, 0, -1).Format("2006/01/02")))
	}

	if !opts.Until.IsZero() {
		parts = append(parts, fmt.Sprintf("before:%s", opts.Until.AddDate(0, 0, 1).Format("2006/01/02")))
	}

	if opts.HasAttachment != nil && *opts.HasAttachment {
		parts = append(parts, "has:attachment")
	}

	if opts.IsRead != nil {
		if *opts.IsRead {
			parts = append(parts, "-is:unread")
		} else {
			parts = append(parts, "is:unread")
		}
	}

	if opts.IsFlagged != nil {
		if *opts.IsFlagged {
			parts = append(parts, "is:starred")
		} else {
			parts = append(parts, "-is:starred")
		}
	}

	if opts.SizeGreater > 0 {
		parts = append(parts, fmt.Sprintf("larger:%d", opts.SizeGreater))
	}

	if opts.SizeLess > 0 {
		parts = append(parts, fmt.Sprintf("smaller:%d", opts.SizeLess))
	}

	return strings.Join(parts, " ")
}

// GetMessage retrieves a single message.
func (c *GmailClient) GetMessage(ctx context.Context, messageID, format string) (*providerdata.EmailMessage, error) {
	service, err := c.getService(ctx)
	if err != nil {
		return nil, err
	}

	if format == "" {
		format = "full"
	}

	c.rateLimiter.Acquire("messages.get")

	msg, err := service.Users.Messages.Get("me", messageID).
		Context(ctx).
		Format(format).
		Do()
	if err != nil {
		return nil, fmt.Errorf("failed to get message: %w", err)
	}

	return parseGmailMessage(msg), nil
}

// GetMessages retrieves multiple messages concurrently.
func (c *GmailClient) GetMessages(ctx context.Context, messageIDs []string, format string) ([]*providerdata.EmailMessage, error) {
	if len(messageIDs) == 0 {
		return nil, nil
	}

	if format == "" {
		format = "metadata"
	}

	results := make([]*providerdata.EmailMessage, len(messageIDs))
	errors := make([]error, len(messageIDs))

	var wg sync.WaitGroup
	sem := make(chan struct{}, 50)

	for i, id := range messageIDs {
		wg.Add(1)
		go func(idx int, msgID string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			msg, err := c.GetMessage(ctx, msgID, format)
			results[idx] = msg
			errors[idx] = err
		}(i, id)
	}

	wg.Wait()

	var validResults []*providerdata.EmailMessage
	for i, msg := range results {
		if errors[i] == nil && msg != nil {
			validResults = append(validResults, msg)
		}
	}

	return validResults, nil
}

// ModifyLabels modifies labels on messages.
func (c *GmailClient) ModifyLabels(ctx context.Context, messageIDs, addLabels, removeLabels []string) (int, error) {
	if len(messageIDs) == 0 {
		return 0, nil
	}

	service, err := c.getService(ctx)
	if err != nil {
		return 0, err
	}

	succeeded := 0
	batchSize := 50

	for i := 0; i < len(messageIDs); i += batchSize {
		end := i + batchSize
		if end > len(messageIDs) {
			end = len(messageIDs)
		}
		batch := messageIDs[i:end]

		c.rateLimiter.Acquire("messages.batchModify")

		req := &gmail.BatchModifyMessagesRequest{
			Ids:            batch,
			AddLabelIds:    addLabels,
			RemoveLabelIds: removeLabels,
		}

		err := service.Users.Messages.BatchModify("me", req).Context(ctx).Do()
		if err == nil {
			succeeded += len(batch)
		}
	}

	return succeeded, nil
}

// MarkRead marks messages as read.
func (c *GmailClient) MarkRead(ctx context.Context, messageIDs []string) (int, error) {
	return c.ModifyLabels(ctx, messageIDs, nil, []string{"UNREAD"})
}

// MarkUnread marks messages as unread.
func (c *GmailClient) MarkUnread(ctx context.Context, messageIDs []string) (int, error) {
	return c.ModifyLabels(ctx, messageIDs, []string{"UNREAD"}, nil)
}

// Archive removes messages from inbox.
func (c *GmailClient) Archive(ctx context.Context, messageIDs []string) (int, error) {
	return c.ModifyLabels(ctx, messageIDs, nil, []string{"INBOX"})
}

// MoveToInbox restores messages to inbox.
func (c *GmailClient) MoveToInbox(ctx context.Context, messageIDs []string) (int, error) {
	return c.ModifyLabels(ctx, messageIDs, []string{"INBOX"}, nil)
}

// Trash moves messages to trash.
func (c *GmailClient) Trash(ctx context.Context, messageIDs []string) (int, error) {
	if len(messageIDs) == 0 {
		return 0, nil
	}

	service, err := c.getService(ctx)
	if err != nil {
		return 0, err
	}

	succeeded := 0
	for _, id := range messageIDs {
		c.rateLimiter.Acquire("messages.trash")
		_, err := service.Users.Messages.Trash("me", id).Context(ctx).Do()
		if err == nil {
			succeeded++
		}
	}

	return succeeded, nil
}

func (c *GmailClient) ApplyNamedLabel(ctx context.Context, messageIDs []string, label string, archive bool) (int, error) {
	labelID, err := c.ensureUserLabel(ctx, label)
	if err != nil {
		return 0, err
	}
	remove := []string(nil)
	if archive {
		remove = []string{"INBOX"}
	}
	return c.ModifyLabels(ctx, messageIDs, []string{labelID}, remove)
}

func (c *GmailClient) ServerFilterCapabilities() ServerFilterCapabilities {
	return ServerFilterCapabilities{
		Provider:          c.ProviderName(),
		SupportsList:      true,
		SupportsUpsert:    true,
		SupportsDelete:    true,
		SupportsArchive:   true,
		SupportsTrash:     false,
		SupportsMoveTo:    true,
		SupportsMarkRead:  true,
		SupportsForward:   true,
		SupportsAddLabels: true,
		SupportsQuery:     true,
	}
}

func (c *GmailClient) ListServerFilters(ctx context.Context) ([]ServerFilter, error) {
	service, err := c.getService(ctx)
	if err != nil {
		return nil, err
	}
	c.rateLimiter.Acquire("settings.filters.list")
	result, err := service.Users.Settings.Filters.List("me").Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("failed to list gmail filters: %w", err)
	}
	labels, err := c.ListLabels(ctx)
	if err != nil {
		return nil, err
	}
	labelByID := make(map[string]string, len(labels))
	for _, label := range labels {
		labelByID[strings.TrimSpace(label.ID)] = strings.TrimSpace(label.Name)
	}
	out := make([]ServerFilter, 0, len(result.Filter))
	for _, filter := range result.Filter {
		out = append(out, gmailFilterToServerFilter(filter, labelByID))
	}
	return out, nil
}

func (c *GmailClient) UpsertServerFilter(ctx context.Context, filter ServerFilter) (ServerFilter, error) {
	service, err := c.getService(ctx)
	if err != nil {
		return ServerFilter{}, err
	}
	payload, err := c.serverFilterToGmailFilter(ctx, filter)
	if err != nil {
		return ServerFilter{}, err
	}
	if strings.TrimSpace(filter.ID) != "" {
		c.rateLimiter.Acquire("settings.filters.delete")
		if err := service.Users.Settings.Filters.Delete("me", strings.TrimSpace(filter.ID)).Context(ctx).Do(); err != nil {
			return ServerFilter{}, fmt.Errorf("failed to delete gmail filter %s: %w", filter.ID, err)
		}
	}
	c.rateLimiter.Acquire("settings.filters.create")
	created, err := service.Users.Settings.Filters.Create("me", payload).Context(ctx).Do()
	if err != nil {
		return ServerFilter{}, fmt.Errorf("failed to create gmail filter: %w", err)
	}
	labels, err := c.ListLabels(ctx)
	if err != nil {
		return ServerFilter{}, err
	}
	labelByID := make(map[string]string, len(labels))
	for _, label := range labels {
		labelByID[strings.TrimSpace(label.ID)] = strings.TrimSpace(label.Name)
	}
	return gmailFilterToServerFilter(created, labelByID), nil
}

func (c *GmailClient) DeleteServerFilter(ctx context.Context, id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("gmail filter id is required")
	}
	service, err := c.getService(ctx)
	if err != nil {
		return err
	}
	c.rateLimiter.Acquire("settings.filters.delete")
	return service.Users.Settings.Filters.Delete("me", strings.TrimSpace(id)).Context(ctx).Do()
}

// Delete permanently deletes messages.
func (c *GmailClient) Delete(ctx context.Context, messageIDs []string) (int, error) {
	if len(messageIDs) == 0 {
		return 0, nil
	}

	service, err := c.getService(ctx)
	if err != nil {
		return 0, err
	}

	succeeded := 0
	for _, id := range messageIDs {
		c.rateLimiter.Acquire("messages.delete")
		err := service.Users.Messages.Delete("me", id).Context(ctx).Do()
		if err == nil {
			succeeded++
		}
	}

	return succeeded, nil
}

// Defer applies Gmail-native deferred handling using the system SNOOZED label.
func (c *GmailClient) Defer(ctx context.Context, messageID string, untilAt time.Time) (MessageActionResult, error) {
	service, err := c.getService(ctx)
	if err != nil {
		return MessageActionResult{}, err
	}
	c.rateLimiter.Acquire("messages.modify")
	req := &gmail.ModifyMessageRequest{
		AddLabelIds:    []string{"SNOOZED"},
		RemoveLabelIds: []string{"INBOX"},
	}
	if _, err := service.Users.Messages.Modify("me", messageID, req).Context(ctx).Do(); err != nil {
		return MessageActionResult{}, err
	}
	return MessageActionResult{
		Provider:              c.ProviderName(),
		Action:                "defer",
		MessageID:             messageID,
		Status:                "ok",
		EffectiveProviderMode: "native",
		DeferredUntilAt:       untilAt.UTC().Format(time.RFC3339),
	}, nil
}

func (c *GmailClient) SupportsNativeDefer() bool {
	return true
}

// ProviderName returns the name of the provider.
func (c *GmailClient) ProviderName() string {
	return "gmail"
}

// Close releases any resources held by the client.
func (c *GmailClient) Close() error {
	return nil
}

func (c *GmailClient) ensureUserLabel(ctx context.Context, name string) (string, error) {
	clean := strings.TrimSpace(name)
	if clean == "" {
		return "", fmt.Errorf("gmail label name is required")
	}
	labels, err := c.ListLabels(ctx)
	if err != nil {
		return "", err
	}
	for _, label := range labels {
		if strings.EqualFold(strings.TrimSpace(label.Name), clean) {
			return strings.TrimSpace(label.ID), nil
		}
	}
	service, err := c.getService(ctx)
	if err != nil {
		return "", err
	}
	c.rateLimiter.Acquire("labels.create")
	created, err := service.Users.Labels.Create("me", &gmail.Label{
		Name:                  clean,
		LabelListVisibility:   "labelShow",
		MessageListVisibility: "show",
	}).Context(ctx).Do()
	if err != nil {
		return "", fmt.Errorf("failed to create gmail label %q: %w", clean, err)
	}
	return strings.TrimSpace(created.Id), nil
}

func gmailFilterToServerFilter(filter *gmail.Filter, labelByID map[string]string) ServerFilter {
	if filter == nil {
		return ServerFilter{}
	}
	out := ServerFilter{
		ID:      strings.TrimSpace(filter.Id),
		Name:    "gmail-filter",
		Enabled: true,
	}
	if filter.Criteria != nil {
		out.Criteria = ServerFilterCriteria{
			From:         strings.TrimSpace(filter.Criteria.From),
			To:           strings.TrimSpace(filter.Criteria.To),
			Subject:      strings.TrimSpace(filter.Criteria.Subject),
			Query:        strings.TrimSpace(filter.Criteria.Query),
			NegatedQuery: strings.TrimSpace(filter.Criteria.NegatedQuery),
		}
		if filter.Criteria.HasAttachment {
			value := true
			out.Criteria.HasAttachment = &value
		}
	}
	if filter.Action != nil {
		addLabels := make([]string, 0, len(filter.Action.AddLabelIds))
		removeLabels := make([]string, 0, len(filter.Action.RemoveLabelIds))
		for _, id := range filter.Action.AddLabelIds {
			addLabels = append(addLabels, lookupLabelName(labelByID, id))
		}
		for _, id := range filter.Action.RemoveLabelIds {
			removeLabels = append(removeLabels, lookupLabelName(labelByID, id))
		}
		out.Action = ServerFilterAction{
			MarkRead:     slicesContainsFold(removeLabels, "UNREAD"),
			Archive:      slicesContainsFold(removeLabels, "INBOX"),
			ForwardTo:    compactStrings([]string{strings.TrimSpace(filter.Action.Forward)}),
			AddLabels:    compactStrings(addLabels),
			RemoveLabels: compactStrings(removeLabels),
		}
		if moveTarget := firstUserLabelName(out.Action.AddLabels); moveTarget != "" && out.Action.Archive {
			out.Action.MoveTo = moveTarget
		}
	}
	return out
}

func (c *GmailClient) serverFilterToGmailFilter(ctx context.Context, filter ServerFilter) (*gmail.Filter, error) {
	result := &gmail.Filter{
		Criteria: &gmail.FilterCriteria{
			From:         strings.TrimSpace(filter.Criteria.From),
			To:           strings.TrimSpace(filter.Criteria.To),
			Subject:      strings.TrimSpace(filter.Criteria.Subject),
			Query:        strings.TrimSpace(filter.Criteria.Query),
			NegatedQuery: strings.TrimSpace(filter.Criteria.NegatedQuery),
		},
		Action: &gmail.FilterAction{},
	}
	if filter.Criteria.HasAttachment != nil {
		result.Criteria.HasAttachment = *filter.Criteria.HasAttachment
	}
	addIDs := make([]string, 0, len(filter.Action.AddLabels)+1)
	removeIDs := make([]string, 0, len(filter.Action.RemoveLabels)+2)
	if filter.Action.Archive || strings.TrimSpace(filter.Action.MoveTo) != "" {
		removeIDs = append(removeIDs, "INBOX")
	}
	if filter.Action.MarkRead {
		removeIDs = append(removeIDs, "UNREAD")
	}
	for _, label := range filter.Action.AddLabels {
		labelID, err := c.ensureUserLabel(ctx, label)
		if err != nil {
			return nil, err
		}
		addIDs = append(addIDs, labelID)
	}
	for _, label := range filter.Action.RemoveLabels {
		if strings.EqualFold(strings.TrimSpace(label), "inbox") || strings.EqualFold(strings.TrimSpace(label), "unread") {
			removeIDs = append(removeIDs, strings.ToUpper(strings.TrimSpace(label)))
			continue
		}
		labelID, err := c.ensureUserLabel(ctx, label)
		if err != nil {
			return nil, err
		}
		removeIDs = append(removeIDs, labelID)
	}
	if moveTarget := strings.TrimSpace(filter.Action.MoveTo); moveTarget != "" {
		labelID, err := c.ensureUserLabel(ctx, moveTarget)
		if err != nil {
			return nil, err
		}
		addIDs = append(addIDs, labelID)
	}
	result.Action.AddLabelIds = compactStrings(addIDs)
	result.Action.RemoveLabelIds = compactStrings(removeIDs)
	if len(filter.Action.ForwardTo) > 0 {
		result.Action.Forward = strings.TrimSpace(filter.Action.ForwardTo[0])
	}
	if filter.Action.Trash {
		return nil, fmt.Errorf("gmail server filters do not support trash safely")
	}
	return result, nil
}

func lookupLabelName(labelByID map[string]string, id string) string {
	if name := strings.TrimSpace(labelByID[strings.TrimSpace(id)]); name != "" {
		return name
	}
	return strings.TrimSpace(id)
}

func firstUserLabelName(values []string) string {
	for _, value := range values {
		clean := strings.TrimSpace(value)
		switch strings.ToUpper(clean) {
		case "", "INBOX", "UNREAD":
			continue
		default:
			return clean
		}
	}
	return ""
}

func slicesContainsFold(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(target)) {
			return true
		}
	}
	return false
}

func compactStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		clean := strings.TrimSpace(value)
		if clean == "" {
			continue
		}
		key := strings.ToLower(clean)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, clean)
	}
	return out
}

func parseGmailMessage(msg *gmail.Message) *providerdata.EmailMessage {
	headers := make(map[string]string)
	if msg.Payload != nil {
		for _, h := range msg.Payload.Headers {
			headers[h.Name] = h.Value
		}
	}

	isRead := true
	isFlagged := false
	for _, lbl := range msg.LabelIds {
		if lbl == "UNREAD" {
			isRead = false
		}
		if lbl == "STARRED" {
			isFlagged = true
		}
	}

	email := &providerdata.EmailMessage{
		ID:        msg.Id,
		ThreadID:  msg.ThreadId,
		Subject:   headers["Subject"],
		Sender:    headers["From"],
		Snippet:   msg.Snippet,
		Labels:    msg.LabelIds,
		IsRead:    isRead,
		IsFlagged: isFlagged,
	}

	if email.Subject == "" {
		email.Subject = "(No subject)"
	}

	if to := headers["To"]; to != "" {
		email.Recipients = strings.Split(to, ",")
		for i := range email.Recipients {
			email.Recipients[i] = strings.TrimSpace(email.Recipients[i])
		}
	}

	if dateStr := headers["Date"]; dateStr != "" {
		if t, err := mail.ParseDate(dateStr); err == nil {
			email.Date = t
		}
	}
	if email.Date.IsZero() {
		email.Date = time.Now()
	}

	if msg.Payload != nil {
		email.BodyText = extractGmailBody(msg.Payload, "text/plain")
		email.BodyHTML = extractGmailBody(msg.Payload, "text/html")
	}

	return email
}

func extractGmailBody(payload *gmail.MessagePart, mimeType string) *string {
	if payload.MimeType == mimeType && payload.Body != nil && payload.Body.Data != "" {
		data, err := base64.URLEncoding.DecodeString(payload.Body.Data)
		if err == nil {
			s := string(data)
			return &s
		}
	}

	for _, part := range payload.Parts {
		if body := extractGmailBody(part, mimeType); body != nil {
			return body
		}
	}

	return nil
}

func (c *GmailClient) ExportRawMessage(ctx context.Context, messageID string) ([]byte, error) {
	service, err := c.getService(ctx)
	if err != nil {
		return nil, err
	}
	c.rateLimiter.Acquire("messages.get")
	msg, err := service.Users.Messages.Get("me", messageID).
		Context(ctx).
		Format("raw").
		Do()
	if err != nil {
		return nil, fmt.Errorf("failed to get raw message: %w", err)
	}
	return base64.URLEncoding.WithPadding(base64.NoPadding).DecodeString(msg.Raw)
}

func (c *GmailClient) ImportRawMessage(ctx context.Context, mimeContent []byte, folder string) (string, error) {
	service, err := c.getService(ctx)
	if err != nil {
		return "", err
	}
	labelIDs := gmailImportLabelIDs(folder)
	encoded := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(mimeContent)
	c.rateLimiter.Acquire("messages.insert")
	msg, err := service.Users.Messages.Insert("me", &gmail.Message{
		Raw:      encoded,
		LabelIds: labelIDs,
	}).Context(ctx).Do()
	if err != nil {
		return "", fmt.Errorf("failed to insert message: %w", err)
	}
	return strings.TrimSpace(msg.Id), nil
}

func gmailImportLabelIDs(folder string) []string {
	clean := strings.TrimSpace(folder)
	if clean == "" {
		return []string{"INBOX"}
	}
	upper := strings.ToUpper(clean)
	switch upper {
	case "INBOX", "SENT", "TRASH", "SPAM", "DRAFT", "STARRED", "IMPORTANT", "UNREAD":
		return []string{upper}
	}
	return []string{clean}
}

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
