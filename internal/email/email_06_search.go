package email

import (
	"strings"

	"github.com/sloppy-org/sloptools/internal/ews"
)

func matchExchangeEWSMessage(message ews.Message, opts SearchOptions) bool {
	if opts.IsRead != nil && message.IsRead != *opts.IsRead {
		return false
	}
	if opts.HasAttachment != nil && message.HasAttachments != *opts.HasAttachment {
		return false
	}
	if opts.IsFlagged != nil {
		flagged := strings.EqualFold(strings.TrimSpace(message.FlagStatus), "Flagged")
		if flagged != *opts.IsFlagged {
			return false
		}
	}
	haystack := strings.ToLower(strings.Join([]string{message.Subject, message.Body, message.From.Name, message.From.Email, message.DisplayTo, message.DisplayCc}, "\n"))
	if opts.Text != "" && !strings.Contains(haystack, strings.ToLower(strings.TrimSpace(opts.Text))) {
		return false
	}
	if opts.Subject != "" && !strings.Contains(strings.ToLower(message.Subject), strings.ToLower(strings.TrimSpace(opts.Subject))) {
		return false
	}
	if opts.From != "" {
		from := strings.ToLower(message.From.Name + "\n" + message.From.Email)
		if !strings.Contains(from, strings.ToLower(strings.TrimSpace(opts.From))) {
			return false
		}
	}
	if opts.To != "" {
		var recipients []string
		for _, mb := range append([]ews.Mailbox(nil), append(message.To, message.Cc...)...) {
			recipients = append(recipients, mb.Name, mb.Email)
		}
		if !strings.Contains(strings.ToLower(strings.Join(recipients, "\n")), strings.ToLower(strings.TrimSpace(opts.To))) {
			return false
		}
	}
	received := message.ReceivedAt
	if !opts.After.IsZero() && (received.IsZero() || received.Before(opts.After)) {
		return false
	}
	if !opts.Before.IsZero() && !received.IsZero() && !received.Before(opts.Before) {
		return false
	}
	if !opts.Since.IsZero() && (received.IsZero() || received.Before(opts.Since)) {
		return false
	}
	if !opts.Until.IsZero() && !received.IsZero() && received.After(opts.Until) {
		return false
	}
	return true
}

func exchangeEWSNeedsMessageFilter(opts SearchOptions) bool {
	return opts.IsRead != nil || opts.HasAttachment != nil || opts.IsFlagged != nil || opts.Text != "" || opts.Subject != "" || opts.From != "" || opts.To != "" || !opts.After.IsZero() || !opts.Before.IsZero() || !opts.Since.IsZero() || !opts.Until.IsZero()
}

func exchangeEWSBuildRestriction(opts SearchOptions) *ews.FindRestriction {
	if !exchangeEWSNeedsMessageFilter(opts) {
		return nil
	}
	r := &ews.FindRestriction{From: strings.TrimSpace(opts.From), HasAttachment: opts.HasAttachment}
	if !opts.After.IsZero() {
		r.After = opts.After
	}
	if !opts.Since.IsZero() && (r.After.IsZero() || opts.Since.After(r.After)) {
		r.After = opts.Since
	}
	if !opts.Before.IsZero() {
		r.Before = opts.Before
	}
	if !opts.Until.IsZero() && (r.Before.IsZero() || opts.Until.Before(r.Before)) {
		r.Before = opts.Until
	}
	if r.From == "" && r.HasAttachment == nil && r.After.IsZero() && r.Before.IsZero() {
		return nil
	}
	return r
}

func exchangeEWSNeedsClientFilter(opts SearchOptions) bool {
	return opts.IsRead != nil || opts.IsFlagged != nil || opts.Text != "" || opts.Subject != "" || opts.To != ""
}
