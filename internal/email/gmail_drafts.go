package email

import (
	"context"
	"fmt"
	"net/mail"
	"strings"

	"google.golang.org/api/gmail/v1"
)

var _ DraftProvider = (*GmailClient)(nil)

func (c *GmailClient) CreateDraft(ctx context.Context, input DraftInput) (Draft, error) {
	normalized, err := NormalizeDraftInput(input)
	if err != nil {
		return Draft{}, err
	}
	raw, err := encodeGmailRawMessage(normalized)
	if err != nil {
		return Draft{}, err
	}
	service, err := c.getService(ctx)
	if err != nil {
		return Draft{}, err
	}
	message := &gmail.Message{Raw: raw}
	if normalized.ThreadID != "" {
		message.ThreadId = normalized.ThreadID
	}
	created, err := service.Users.Drafts.Create("me", &gmail.Draft{Message: message}).Context(ctx).Do()
	if err != nil {
		return Draft{}, fmt.Errorf("gmail create draft: %w", err)
	}
	threadID := normalized.ThreadID
	if created != nil && created.Message != nil && strings.TrimSpace(created.Message.ThreadId) != "" {
		threadID = strings.TrimSpace(created.Message.ThreadId)
	}
	return Draft{ID: strings.TrimSpace(created.Id), ThreadID: threadID}, nil
}

func (c *GmailClient) CreateReplyDraft(ctx context.Context, messageID string, input DraftInput) (Draft, error) {
	service, err := c.getService(ctx)
	if err != nil {
		return Draft{}, err
	}
	original, err := service.Users.Messages.Get("me", strings.TrimSpace(messageID)).Format("full").Context(ctx).Do()
	if err != nil {
		return Draft{}, fmt.Errorf("gmail get reply source: %w", err)
	}
	reply := input
	headers := gmailHeaderMap(original)
	if len(reply.To) == 0 {
		reply.To = []string{strings.TrimSpace(headers["Reply-To"])}
		if strings.TrimSpace(reply.To[0]) == "" {
			reply.To = []string{strings.TrimSpace(headers["From"])}
		}
	}
	if strings.TrimSpace(reply.Subject) == "" {
		reply.Subject = ensureReplySubject(strings.TrimSpace(headers["Subject"]))
	} else {
		reply.Subject = ensureReplySubject(reply.Subject)
	}
	if strings.TrimSpace(reply.ThreadID) == "" {
		reply.ThreadID = strings.TrimSpace(original.ThreadId)
	}
	if strings.TrimSpace(reply.InReplyTo) == "" {
		reply.InReplyTo = strings.TrimSpace(headers["Message-ID"])
	}
	if len(reply.References) == 0 && strings.TrimSpace(headers["References"]) != "" {
		reply.References = strings.Fields(strings.TrimSpace(headers["References"]))
	}
	if reply.InReplyTo != "" && len(reply.References) == 0 {
		reply.References = []string{reply.InReplyTo}
	}
	return c.CreateDraft(ctx, reply)
}

func (c *GmailClient) UpdateDraft(ctx context.Context, draftID string, input DraftInput) (Draft, error) {
	normalized, err := NormalizeDraftInput(input)
	if err != nil {
		return Draft{}, err
	}
	raw, err := encodeGmailRawMessage(normalized)
	if err != nil {
		return Draft{}, err
	}
	service, err := c.getService(ctx)
	if err != nil {
		return Draft{}, err
	}
	message := &gmail.Message{Raw: raw}
	if normalized.ThreadID != "" {
		message.ThreadId = normalized.ThreadID
	}
	updated, err := service.Users.Drafts.Update("me", strings.TrimSpace(draftID), &gmail.Draft{
		Id:      strings.TrimSpace(draftID),
		Message: message,
	}).Context(ctx).Do()
	if err != nil {
		return Draft{}, fmt.Errorf("gmail update draft: %w", err)
	}
	threadID := normalized.ThreadID
	if updated != nil && updated.Message != nil && strings.TrimSpace(updated.Message.ThreadId) != "" {
		threadID = strings.TrimSpace(updated.Message.ThreadId)
	}
	return Draft{ID: strings.TrimSpace(updated.Id), ThreadID: threadID}, nil
}

func (c *GmailClient) SendDraft(ctx context.Context, draftID string, _ DraftInput) error {
	service, err := c.getService(ctx)
	if err != nil {
		return err
	}
	_, err = service.Users.Drafts.Send("me", &gmail.Draft{Id: strings.TrimSpace(draftID)}).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("gmail send draft: %w", err)
	}
	return nil
}

func gmailHeaderMap(message *gmail.Message) map[string]string {
	out := map[string]string{}
	if message == nil || message.Payload == nil {
		return out
	}
	for _, header := range message.Payload.Headers {
		name := strings.TrimSpace(header.Name)
		if name == "" {
			continue
		}
		out[name] = strings.TrimSpace(header.Value)
	}
	if from := strings.TrimSpace(out["From"]); from != "" {
		if addr, err := mail.ParseAddress(from); err == nil {
			out["From"] = strings.TrimSpace(addr.Address)
		}
	}
	if replyTo := strings.TrimSpace(out["Reply-To"]); replyTo != "" {
		if addr, err := mail.ParseAddress(replyTo); err == nil {
			out["Reply-To"] = strings.TrimSpace(addr.Address)
		}
	}
	return out
}
