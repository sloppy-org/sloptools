package email

import (
	"context"
	"encoding/base64"
	"fmt"
	imap "github.com/emersion/go-imap/v2"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	gmail "google.golang.org/api/gmail/v1"
	"net/mail"
	"strings"
	"time"
)

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

func (c *GmailClient) Defer(ctx context.Context, messageID string, untilAt time.Time) (MessageActionResult, error) {
	service, err := c.getService(ctx)
	if err != nil {
		return MessageActionResult{}, err
	}
	c.rateLimiter.Acquire("messages.modify")
	req := &gmail.ModifyMessageRequest{AddLabelIds: []string{"SNOOZED"}, RemoveLabelIds: []string{"INBOX"}}
	if _, err := service.Users.Messages.Modify("me", messageID, req).Context(ctx).Do(); err != nil {
		return MessageActionResult{}, err
	}
	return MessageActionResult{Provider: c.ProviderName(), Action: "defer", MessageID: messageID, Status: "ok", EffectiveProviderMode: "native", DeferredUntilAt: untilAt.UTC().Format(time.RFC3339)}, nil
}

func (c *GmailClient) SupportsNativeDefer() bool {
	return true
}

func (c *GmailClient) ProviderName() string {
	return "gmail"
}

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
	created, err := service.Users.Labels.Create("me", &gmail.Label{Name: clean, LabelListVisibility: "labelShow", MessageListVisibility: "show"}).Context(ctx).Do()
	if err != nil {
		return "", fmt.Errorf("failed to create gmail label %q: %w", clean, err)
	}
	return strings.TrimSpace(created.Id), nil
}

func gmailFilterToServerFilter(filter *gmail.Filter, labelByID map[string]string) ServerFilter {
	if filter == nil {
		return ServerFilter{}
	}
	out := ServerFilter{ID: strings.TrimSpace(filter.Id), Name: "gmail-filter", Enabled: true}
	if filter.Criteria != nil {
		out.Criteria = ServerFilterCriteria{From: strings.TrimSpace(filter.Criteria.From), To: strings.TrimSpace(filter.Criteria.To), Subject: strings.TrimSpace(filter.Criteria.Subject), Query: strings.TrimSpace(filter.Criteria.Query), NegatedQuery: strings.TrimSpace(filter.Criteria.NegatedQuery)}
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
		out.Action = ServerFilterAction{MarkRead: slicesContainsFold(removeLabels, "UNREAD"), Archive: slicesContainsFold(removeLabels, "INBOX"), ForwardTo: compactStrings([]string{strings.TrimSpace(filter.Action.Forward)}), AddLabels: compactStrings(addLabels), RemoveLabels: compactStrings(removeLabels)}
		if moveTarget := firstUserLabelName(out.Action.AddLabels); moveTarget != "" && out.Action.Archive {
			out.Action.MoveTo = moveTarget
		}
	}
	return out
}

func (c *GmailClient) serverFilterToGmailFilter(ctx context.Context, filter ServerFilter) (*gmail.Filter, error) {
	result := &gmail.Filter{Criteria: &gmail.FilterCriteria{From: strings.TrimSpace(filter.Criteria.From), To: strings.TrimSpace(filter.Criteria.To), Subject: strings.TrimSpace(filter.Criteria.Subject), Query: strings.TrimSpace(filter.Criteria.Query), NegatedQuery: strings.TrimSpace(filter.Criteria.NegatedQuery)}, Action: &gmail.FilterAction{}}
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
	email := &providerdata.EmailMessage{ID: msg.Id, ThreadID: msg.ThreadId, Subject: headers["Subject"], Sender: headers["From"], Snippet: msg.Snippet, Labels: msg.LabelIds, Folder: gmailFolderFromLabels(msg.LabelIds), IsRead: isRead, IsFlagged: isFlagged}
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
		email.Attachments = collectGmailAttachments(msg.Payload)
	}
	return email
}

// gmailAttachmentIDPrefix marks IDs we mint from the immutable gmail PartId,
// so the caller can re-fetch an attachment after Gmail has rotated its
// short-lived attachmentId tokens.
const gmailAttachmentIDPrefix = "part:"

func collectGmailAttachments(part *gmail.MessagePart) []providerdata.Attachment {
	if part == nil {
		return nil
	}
	var out []providerdata.Attachment
	if part.Body != nil && strings.TrimSpace(part.Body.AttachmentId) != "" {
		filename := strings.TrimSpace(part.Filename)
		mimeType := strings.TrimSpace(part.MimeType)
		isInline := false
		for _, h := range part.Headers {
			if strings.EqualFold(h.Name, "Content-Disposition") {
				if strings.Contains(strings.ToLower(h.Value), "inline") {
					isInline = true
				}
			}
		}
		out = append(out, providerdata.Attachment{ID: gmailAttachmentIDPrefix + strings.TrimSpace(part.PartId), Filename: filename, MimeType: mimeType, Size: part.Body.Size, IsInline: isInline})
	}
	for _, sub := range part.Parts {
		out = append(out, collectGmailAttachments(sub)...)
	}
	return out
}

func (c *GmailClient) GetAttachment(ctx context.Context, messageID, attachmentID string) (*providerdata.AttachmentData, error) {
	if strings.TrimSpace(messageID) == "" {
		return nil, fmt.Errorf("message_id is required")
	}
	cleanAttachmentID := strings.TrimSpace(attachmentID)
	if cleanAttachmentID == "" {
		return nil, fmt.Errorf("attachment_id is required")
	}
	service, err := c.getService(ctx)
	if err != nil {
		return nil, err
	}
	c.rateLimiter.Acquire("messages.get")
	msg, err := service.Users.Messages.Get("me", messageID).Context(ctx).Format("full").Do()
	if err != nil {
		return nil, fmt.Errorf("failed to get message for attachment: %w", err)
	}
	var part *gmail.MessagePart
	if strings.HasPrefix(cleanAttachmentID, gmailAttachmentIDPrefix) {
		partID := strings.TrimPrefix(cleanAttachmentID, gmailAttachmentIDPrefix)
		part = findGmailPartByID(msg.Payload, partID)
	} else {
		part = findGmailPartByAttachmentID(msg.Payload, cleanAttachmentID)
	}
	if part == nil || part.Body == nil || strings.TrimSpace(part.Body.AttachmentId) == "" {
		return nil, fmt.Errorf("attachment %q not found in message", cleanAttachmentID)
	}
	freshAttachmentID := part.Body.AttachmentId
	c.rateLimiter.Acquire("messages.attachments.get")
	body, err := service.Users.Messages.Attachments.Get("me", messageID, freshAttachmentID).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("failed to get attachment: %w", err)
	}
	data, err := base64.URLEncoding.WithPadding(base64.NoPadding).DecodeString(body.Data)
	if err != nil {
		data, err = base64.URLEncoding.DecodeString(body.Data)
		if err != nil {
			return nil, fmt.Errorf("decode attachment data: %w", err)
		}
	}
	isInline := false
	for _, h := range part.Headers {
		if strings.EqualFold(h.Name, "Content-Disposition") && strings.Contains(strings.ToLower(h.Value), "inline") {
			isInline = true
		}
	}
	return &providerdata.AttachmentData{ID: cleanAttachmentID, Filename: strings.TrimSpace(part.Filename), MimeType: strings.TrimSpace(part.MimeType), Size: int64(len(data)), IsInline: isInline, Content: data}, nil
}

func findGmailPartByID(part *gmail.MessagePart, partID string) *gmail.MessagePart {
	if part == nil {
		return nil
	}
	if strings.TrimSpace(part.PartId) == partID {
		return part
	}
	for _, sub := range part.Parts {
		if m := findGmailPartByID(sub, partID); m != nil {
			return m
		}
	}
	return nil
}

func findGmailPartByAttachmentID(part *gmail.MessagePart, attachmentID string) *gmail.MessagePart {
	if part == nil {
		return nil
	}
	if part.Body != nil && strings.TrimSpace(part.Body.AttachmentId) == attachmentID {
		return part
	}
	for _, sub := range part.Parts {
		if m := findGmailPartByAttachmentID(sub, attachmentID); m != nil {
			return m
		}
	}
	return nil
}

// gmailFolderFromLabels picks the folder string used by D5 mapping. Gmail
// represents folders as labels: system labels INBOX/SENT/DRAFT/SPAM/TRASH
// stand in for top-level folders, while user labels with `/` (e.g.
// `INBOX/Teaching`) carry sub-folder paths. Plain user labels stay in
// `Labels[]` only and do not surface as Folder.
func gmailFolderFromLabels(labels []string) string {
	const (
		inbox = "INBOX"
		sent  = "SENT"
		draft = "DRAFT"
		spam  = "SPAM"
		trash = "TRASH"
	)
	hasSystem := ""
	pathLabel := ""
	for _, raw := range labels {
		clean := strings.TrimSpace(raw)
		switch clean {
		case inbox, sent, draft, spam, trash:
			if hasSystem == "" || clean == inbox {
				hasSystem = clean
			}
		}
		if pathLabel == "" && strings.Contains(clean, "/") {
			pathLabel = clean
		}
	}
	if pathLabel != "" {
		return pathLabel
	}
	return hasSystem
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
	msg, err := service.Users.Messages.Get("me", messageID).Context(ctx).Format("raw").Do()
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
	msg, err := service.Users.Messages.Insert("me", &gmail.Message{Raw: encoded, LabelIds: labelIDs}).Context(ctx).Do()
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

var _ DraftProvider = (*GmailClient)(nil)

var _ ExistingDraftSender = (*GmailClient)(nil)

type searchResult struct {
	folder string
	uid    imap.UID
	date   time.Time
} // searchResult holds a message reference with its date for sorting.
