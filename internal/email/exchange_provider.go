package email

import (
	"context"
	"fmt"
	"html"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/providerdata"
)

var (
	_ EmailProvider       = (*ExchangeMailProvider)(nil)
	_ MessagePageProvider = (*ExchangeMailProvider)(nil)

	exchangeHTMLTagPattern = regexp.MustCompile(`<[^>]+>`)
)

type ExchangeMailProvider struct {
	client *ExchangeClient
}

func NewExchangeMailProvider(cfg ExchangeConfig, opts ...ExchangeOption) (*ExchangeMailProvider, error) {
	client, err := NewExchangeClient(cfg, opts...)
	if err != nil {
		return nil, err
	}
	return &ExchangeMailProvider{client: client}, nil
}

func (p *ExchangeMailProvider) ListLabels(ctx context.Context) ([]providerdata.Label, error) {
	folders, err := p.client.ListFolders(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]providerdata.Label, 0, len(folders))
	for _, folder := range folders {
		name := strings.TrimSpace(folder.DisplayName)
		if name == "" {
			name = strings.TrimSpace(folder.WellKnownName)
		}
		if name == "" {
			name = strings.TrimSpace(folder.ID)
		}
		out = append(out, providerdata.Label{
			ID:             strings.TrimSpace(folder.ID),
			Name:           name,
			Type:           "exchange",
			MessagesTotal:  folder.TotalItemCount,
			MessagesUnread: folder.UnreadItemCount,
		})
	}
	return out, nil
}

func (p *ExchangeMailProvider) ListMessages(ctx context.Context, opts SearchOptions) ([]string, error) {
	if p == nil || p.client == nil {
		return nil, fmt.Errorf("exchange provider is not configured")
	}
	top := exchangeTop(opts.MaxResults)
	folderID := ""
	if strings.TrimSpace(opts.Folder) != "" {
		folders, err := p.client.ListFolders(ctx)
		if err != nil {
			return nil, err
		}
		var ok bool
		folderID, ok = resolveExchangeFolderID(folders, opts.Folder)
		if !ok {
			return nil, fmt.Errorf("exchange folder %q not found", opts.Folder)
		}
	}
	messages, err := p.client.ListMessages(ctx, ListMessageOptions{
		FolderID: folderID,
		Top:      top,
		Select: []string{
			"id",
			"conversationId",
			"subject",
			"bodyPreview",
			"isRead",
			"hasAttachments",
			"flag",
			"parentFolderId",
			"receivedDateTime",
			"from",
			"toRecipients",
			"ccRecipients",
		},
	})
	if err != nil {
		return nil, err
	}
	filtered := filterExchangeMessages(messages, opts)
	ids := make([]string, 0, len(filtered))
	for _, message := range filtered {
		id := strings.TrimSpace(message.ID)
		if id != "" {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func (p *ExchangeMailProvider) ListMessagesPage(ctx context.Context, opts SearchOptions, pageToken string) (MessagePage, error) {
	if p == nil || p.client == nil {
		return MessagePage{}, fmt.Errorf("exchange provider is not configured")
	}
	offset := 0
	if strings.TrimSpace(pageToken) != "" {
		value, err := strconv.Atoi(strings.TrimSpace(pageToken))
		if err != nil || value < 0 {
			return MessagePage{}, fmt.Errorf("exchange invalid page token %q", pageToken)
		}
		offset = value
	}
	top := exchangeTop(opts.MaxResults)
	folderID := ""
	if strings.TrimSpace(opts.Folder) != "" {
		folders, err := p.client.ListFolders(ctx)
		if err != nil {
			return MessagePage{}, err
		}
		var ok bool
		folderID, ok = resolveExchangeFolderID(folders, opts.Folder)
		if !ok {
			return MessagePage{}, fmt.Errorf("exchange folder %q not found", opts.Folder)
		}
	}
	messages, err := p.client.ListMessages(ctx, ListMessageOptions{
		FolderID: folderID,
		Top:      top,
		Skip:     offset,
		Select: []string{
			"id",
			"conversationId",
			"subject",
			"bodyPreview",
			"isRead",
			"hasAttachments",
			"flag",
			"parentFolderId",
			"receivedDateTime",
			"from",
			"toRecipients",
			"ccRecipients",
		},
	})
	if err != nil {
		return MessagePage{}, err
	}
	filtered := filterExchangeMessages(messages, opts)
	page := MessagePage{IDs: make([]string, 0, len(filtered))}
	for _, message := range filtered {
		if id := strings.TrimSpace(message.ID); id != "" {
			page.IDs = append(page.IDs, id)
		}
	}
	if len(page.IDs) >= top {
		page.NextPageToken = strconv.Itoa(offset + len(page.IDs))
	}
	return page, nil
}

func (p *ExchangeMailProvider) GetMessage(ctx context.Context, messageID, format string) (*providerdata.EmailMessage, error) {
	if p == nil || p.client == nil {
		return nil, fmt.Errorf("exchange provider is not configured")
	}
	folders, err := p.client.ListFolders(ctx)
	if err != nil {
		return nil, err
	}
	message, err := p.client.GetMessage(ctx, messageID)
	if err != nil {
		return nil, err
	}
	out := decodeExchangeEmailMessage(message, folders)
	return &out, nil
}

func (p *ExchangeMailProvider) GetMessages(ctx context.Context, messageIDs []string, format string) ([]*providerdata.EmailMessage, error) {
	if p == nil || p.client == nil {
		return nil, fmt.Errorf("exchange provider is not configured")
	}
	folders, err := p.client.ListFolders(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*providerdata.EmailMessage, 0, len(messageIDs))
	for _, messageID := range messageIDs {
		messageID = strings.TrimSpace(messageID)
		if messageID == "" {
			continue
		}
		message, err := p.client.GetMessage(ctx, messageID)
		if err != nil {
			return nil, err
		}
		decoded := decodeExchangeEmailMessage(message, folders)
		out = append(out, &decoded)
	}
	return out, nil
}

func (p *ExchangeMailProvider) MarkRead(ctx context.Context, messageIDs []string) (int, error) {
	return p.applyMessageIDs(ctx, messageIDs, p.client.MarkRead)
}

func (p *ExchangeMailProvider) MarkUnread(ctx context.Context, messageIDs []string) (int, error) {
	return p.applyMessageIDs(ctx, messageIDs, p.client.MarkUnread)
}

func (p *ExchangeMailProvider) Archive(ctx context.Context, messageIDs []string) (int, error) {
	return p.applyMessageIDs(ctx, messageIDs, p.client.ArchiveMessage)
}

func (p *ExchangeMailProvider) MoveToInbox(ctx context.Context, messageIDs []string) (int, error) {
	return p.applyMessageIDs(ctx, messageIDs, p.client.MoveMessageToInbox)
}

func (p *ExchangeMailProvider) Trash(ctx context.Context, messageIDs []string) (int, error) {
	return p.applyMessageIDs(ctx, messageIDs, p.client.DeleteMessage)
}

func (p *ExchangeMailProvider) Delete(ctx context.Context, messageIDs []string) (int, error) {
	return p.applyMessageIDs(ctx, messageIDs, p.client.DeleteMessage)
}

func (p *ExchangeMailProvider) ProviderName() string {
	return "exchange"
}

func (p *ExchangeMailProvider) Close() error {
	if p == nil || p.client == nil {
		return nil
	}
	return p.client.Close()
}

func (p *ExchangeMailProvider) applyMessageIDs(ctx context.Context, messageIDs []string, apply func(context.Context, string) error) (int, error) {
	if p == nil || p.client == nil {
		return 0, fmt.Errorf("exchange provider is not configured")
	}
	count := 0
	for _, messageID := range messageIDs {
		messageID = strings.TrimSpace(messageID)
		if messageID == "" {
			continue
		}
		if err := apply(ctx, messageID); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func resolveExchangeFolderID(folders []Folder, raw string) (string, bool) {
	target := strings.ToLower(strings.TrimSpace(raw))
	if target == "" {
		return "", false
	}
	for _, folder := range folders {
		switch {
		case strings.EqualFold(strings.TrimSpace(folder.ID), target):
			return strings.TrimSpace(folder.ID), true
		case strings.EqualFold(strings.TrimSpace(folder.DisplayName), target):
			return strings.TrimSpace(folder.ID), true
		case strings.EqualFold(strings.TrimSpace(folder.WellKnownName), target):
			return strings.TrimSpace(folder.ID), true
		}
	}
	return "", false
}

func filterExchangeMessages(messages []Message, opts SearchOptions) []Message {
	out := make([]Message, 0, len(messages))
	for _, message := range messages {
		if !matchExchangeMessage(message, opts) {
			continue
		}
		out = append(out, message)
	}
	sort.Slice(out, func(i, j int) bool {
		left := exchangeMessageTime(out[i])
		right := exchangeMessageTime(out[j])
		switch {
		case left.Equal(right):
			return strings.TrimSpace(out[i].ID) < strings.TrimSpace(out[j].ID)
		case left.IsZero():
			return false
		case right.IsZero():
			return true
		default:
			return left.After(right)
		}
	})
	if opts.MaxResults > 0 && len(out) > int(opts.MaxResults) {
		limit := int(opts.MaxResults)
		return append([]Message(nil), out[:limit]...)
	}
	return out
}

func matchExchangeMessage(message Message, opts SearchOptions) bool {
	if opts.IsRead != nil && message.IsRead != *opts.IsRead {
		return false
	}
	if opts.IsFlagged != nil && exchangeMessageFlagged(message) != *opts.IsFlagged {
		return false
	}
	if opts.HasAttachment != nil && message.HasAttachments != *opts.HasAttachment {
		return false
	}
	if opts.Subject != "" && !containsFold(message.Subject, opts.Subject) {
		return false
	}
	if opts.From != "" && !containsFold(exchangeSenderString(message.From), opts.From) {
		return false
	}
	if opts.To != "" && !containsFold(strings.Join(exchangeRecipientStrings(message.ToRecipients, message.CcRecipients), " "), opts.To) {
		return false
	}
	if opts.Text != "" && !containsFold(strings.Join(exchangeSearchText(message), " "), opts.Text) {
		return false
	}
	receivedAt := exchangeMessageTime(message)
	if !opts.After.IsZero() && (receivedAt.IsZero() || receivedAt.Before(opts.After)) {
		return false
	}
	if !opts.Before.IsZero() && !receivedAt.IsZero() && !receivedAt.Before(opts.Before) {
		return false
	}
	if !opts.Since.IsZero() && (receivedAt.IsZero() || receivedAt.Before(opts.Since)) {
		return false
	}
	if !opts.Until.IsZero() && !receivedAt.IsZero() && receivedAt.After(opts.Until) {
		return false
	}
	return true
}

func exchangeSearchText(message Message) []string {
	values := []string{
		strings.TrimSpace(message.Subject),
		strings.TrimSpace(message.BodyPreview),
		exchangeSenderString(message.From),
	}
	values = append(values, exchangeRecipientStrings(message.ToRecipients, message.CcRecipients)...)
	return values
}

func exchangeRecipientStrings(groups ...[]Recipient) []string {
	out := []string{}
	for _, recipients := range groups {
		for _, recipient := range recipients {
			if value := strings.TrimSpace(formatExchangeAddress(recipient.EmailAddress)); value != "" {
				out = append(out, value)
			}
		}
	}
	return out
}

func exchangeSenderString(recipient *Recipient) string {
	if recipient == nil {
		return ""
	}
	return strings.TrimSpace(formatExchangeAddress(recipient.EmailAddress))
}

func exchangeMessageFlagged(message Message) bool {
	return strings.EqualFold(strings.TrimSpace(message.Flag.FlagStatus), "flagged")
}

func exchangeMessageTime(message Message) time.Time {
	value := strings.TrimSpace(message.ReceivedDateTime)
	if value == "" {
		return time.Time{}
	}
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed.UTC()
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.UTC()
	}
	return time.Time{}
}

func exchangeTop(maxResults int64) int {
	switch {
	case maxResults <= 0:
		return 100
	case maxResults > 500:
		return 500
	default:
		return int(maxResults)
	}
}

func decodeExchangeEmailMessage(message Message, folders []Folder) providerdata.EmailMessage {
	bodyText, bodyHTML := decodeExchangeBody(message.Body)
	labels := exchangeMessageLabels(message.ParentFolderID, folders)
	out := providerdata.EmailMessage{
		ID:          strings.TrimSpace(message.ID),
		ThreadID:    strings.TrimSpace(message.ConversationID),
		Subject:     strings.TrimSpace(message.Subject),
		Sender:      exchangeSenderString(message.From),
		Recipients:  exchangeRecipientStrings(message.ToRecipients, message.CcRecipients),
		Date:        exchangeMessageTime(message),
		Snippet:     strings.TrimSpace(message.BodyPreview),
		Labels:      labels,
		IsRead:      message.IsRead,
		IsFlagged:   exchangeMessageFlagged(message),
		BodyText:    bodyText,
		BodyHTML:    bodyHTML,
		Attachments: exchangeMessageAttachments(message),
	}
	return out
}

func exchangeMessageAttachments(message Message) []providerdata.Attachment {
	if !message.HasAttachments {
		return nil
	}
	return []providerdata.Attachment{{Filename: "attachment", MimeType: "", Size: 0}}
}

func exchangeMessageLabels(parentFolderID string, folders []Folder) []string {
	folderID := strings.TrimSpace(parentFolderID)
	if folderID == "" {
		return nil
	}
	for _, folder := range folders {
		if strings.TrimSpace(folder.ID) != folderID {
			continue
		}
		labels := []string{}
		if display := strings.TrimSpace(folder.DisplayName); display != "" {
			labels = append(labels, display)
		}
		if wellKnown := strings.TrimSpace(folder.WellKnownName); wellKnown != "" && !containsFold(strings.Join(labels, "\n"), wellKnown) {
			labels = append(labels, wellKnown)
		}
		if len(labels) > 0 {
			return labels
		}
	}
	return nil
}

func decodeExchangeBody(body MessageBody) (*string, *string) {
	content := strings.TrimSpace(body.Content)
	if content == "" {
		return nil, nil
	}
	switch strings.ToLower(strings.TrimSpace(body.ContentType)) {
	case "html":
		htmlValue := content
		textValue := normalizeExchangeBodyText(content)
		return &textValue, &htmlValue
	default:
		textValue := content
		return &textValue, nil
	}
}

func normalizeExchangeBodyText(content string) string {
	text := exchangeHTMLTagPattern.ReplaceAllString(content, " ")
	text = html.UnescapeString(text)
	return strings.Join(strings.Fields(text), " ")
}

func formatExchangeAddress(address Address) string {
	name := strings.TrimSpace(address.Name)
	emailAddress := strings.TrimSpace(address.Address)
	switch {
	case name != "" && emailAddress != "":
		return name + " <" + emailAddress + ">"
	case emailAddress != "":
		return emailAddress
	default:
		return name
	}
}

func containsFold(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(haystack)), strings.ToLower(strings.TrimSpace(needle)))
}
