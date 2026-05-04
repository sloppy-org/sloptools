package email

import (
	"sort"
	"strings"
)

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
	values := []string{strings.TrimSpace(message.Subject), strings.TrimSpace(message.BodyPreview), exchangeSenderString(message.From)}
	values = append(values, exchangeRecipientStrings(message.ToRecipients, message.CcRecipients)...)
	return values
}
