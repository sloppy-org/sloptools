package email

import (
	"context"
	"fmt"
	"net/mail"
	"net/url"
	"strings"
)

var _ DraftProvider = (*ExchangeMailProvider)(nil)

func (p *ExchangeMailProvider) CreateDraft(ctx context.Context, input DraftInput) (Draft, error) {
	if p == nil || p.client == nil {
		return Draft{}, fmt.Errorf("exchange provider is not configured")
	}
	normalized, err := NormalizeDraftInput(input)
	if err != nil {
		return Draft{}, err
	}
	req := exchangeDraftRequest(normalized)
	var message Message
	if err := p.client.doJSON(ctx, "POST", "/v1.0/me/messages", nil, req, &message); err != nil {
		return Draft{}, fmt.Errorf("exchange create draft: %w", err)
	}
	return Draft{ID: strings.TrimSpace(message.ID), ThreadID: strings.TrimSpace(message.ConversationID)}, nil
}

func (p *ExchangeMailProvider) CreateReplyDraft(ctx context.Context, messageID string, input DraftInput) (Draft, error) {
	if p == nil || p.client == nil {
		return Draft{}, fmt.Errorf("exchange provider is not configured")
	}
	var message Message
	if err := p.client.doJSON(ctx, "POST", "/v1.0/me/messages/"+url.PathEscape(strings.TrimSpace(messageID))+"/createReply", nil, nil, &message); err != nil {
		return Draft{}, fmt.Errorf("exchange create reply draft: %w", err)
	}
	reply := input
	if len(reply.To) == 0 && message.From != nil {
		reply.To = []string{strings.TrimSpace(message.From.EmailAddress.Address)}
	}
	if strings.TrimSpace(reply.Subject) == "" {
		reply.Subject = ensureReplySubject(message.Subject)
	} else {
		reply.Subject = ensureReplySubject(reply.Subject)
	}
	if strings.TrimSpace(reply.ThreadID) == "" {
		reply.ThreadID = strings.TrimSpace(message.ConversationID)
	}
	updated, err := p.UpdateDraft(ctx, strings.TrimSpace(message.ID), reply)
	if err != nil {
		return Draft{}, err
	}
	if updated.ThreadID == "" {
		updated.ThreadID = strings.TrimSpace(message.ConversationID)
	}
	return updated, nil
}

func (p *ExchangeMailProvider) UpdateDraft(ctx context.Context, draftID string, input DraftInput) (Draft, error) {
	if p == nil || p.client == nil {
		return Draft{}, fmt.Errorf("exchange provider is not configured")
	}
	normalized, err := NormalizeDraftInput(input)
	if err != nil {
		return Draft{}, err
	}
	if err := p.client.doJSON(ctx, "PATCH", "/v1.0/me/messages/"+url.PathEscape(strings.TrimSpace(draftID)), nil, exchangeDraftRequest(normalized), nil); err != nil {
		return Draft{}, fmt.Errorf("exchange update draft: %w", err)
	}
	message, err := p.client.GetMessage(ctx, strings.TrimSpace(draftID))
	if err != nil {
		return Draft{}, fmt.Errorf("exchange get updated draft: %w", err)
	}
	return Draft{ID: strings.TrimSpace(message.ID), ThreadID: strings.TrimSpace(message.ConversationID)}, nil
}

func (p *ExchangeMailProvider) SendDraft(ctx context.Context, draftID string, _ DraftInput) error {
	if p == nil || p.client == nil {
		return fmt.Errorf("exchange provider is not configured")
	}
	if err := p.client.doJSON(ctx, "POST", "/v1.0/me/messages/"+url.PathEscape(strings.TrimSpace(draftID))+"/send", nil, nil, nil); err != nil {
		return fmt.Errorf("exchange send draft: %w", err)
	}
	return nil
}

func exchangeDraftRequest(input DraftInput) map[string]any {
	req := map[string]any{
		"subject": strings.TrimSpace(input.Subject),
		"body": map[string]string{
			"contentType": "text",
			"content":     strings.TrimSpace(input.Body),
		},
		"toRecipients":  exchangeRecipients(input.To),
		"ccRecipients":  exchangeRecipients(input.Cc),
		"bccRecipients": exchangeRecipients(input.Bcc),
	}
	if strings.TrimSpace(input.From) != "" {
		req["from"] = exchangeRecipient(strings.TrimSpace(input.From))
	}
	return req
}

func exchangeRecipients(values []string) []map[string]any {
	out := make([]map[string]any, 0, len(values))
	for _, value := range values {
		recipient := exchangeRecipient(value)
		if len(recipient) > 0 {
			out = append(out, recipient)
		}
	}
	return out
}

func exchangeRecipient(value string) map[string]any {
	clean := strings.TrimSpace(value)
	if clean == "" {
		return nil
	}
	if parsed, err := mail.ParseAddress(clean); err == nil {
		clean = strings.TrimSpace(parsed.Address)
	}
	if clean == "" {
		return nil
	}
	return map[string]any{
		"emailAddress": map[string]string{
			"address": clean,
		},
	}
}
