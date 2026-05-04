package mcp

import (
	"context"
	"time"

	"github.com/sloppy-org/sloptools/internal/email"
	"github.com/sloppy-org/sloptools/internal/providerdata"
)

type fakeMailProvider struct {
	labels             []providerdata.Label
	listIDs            []string
	pageIDs            []string
	nextPage           string
	messages           map[string]*providerdata.EmailMessage
	attachment         *providerdata.AttachmentData
	filters            []email.ServerFilter
	resolvedIDs        map[string]string
	lastOpts           email.SearchOptions
	lastAction         string
	lastIDs            []string
	lastFolder         string
	lastLabel          string
	lastUntil          time.Time
	lastFormat         string
	lastFlag           email.Flag
	lastCategories     []string
	getMessagesFormats []string
	supportsDefer      bool
}

func (p *fakeMailProvider) SetFlag(_ context.Context, ids []string, flag email.Flag) (int, error) {
	p.record("set_flag", ids)
	p.lastFlag = flag
	return len(ids), nil
}

func (p *fakeMailProvider) ClearFlag(_ context.Context, ids []string) (int, error) {
	p.record("clear_flag", ids)
	p.lastFlag = email.Flag{}
	return len(ids), nil
}

func (p *fakeMailProvider) SetCategories(_ context.Context, ids []string, categories []string) (int, error) {
	p.record("set_categories", ids)
	p.lastCategories = append([]string(nil), categories...)
	return len(ids), nil
}

func (p *fakeMailProvider) ListLabels(_ context.Context) ([]providerdata.Label, error) {
	return append([]providerdata.Label(nil), p.labels...), nil
}

func (p *fakeMailProvider) ListMessages(_ context.Context, opts email.SearchOptions) ([]string, error) {
	p.lastOpts = opts
	return append([]string(nil), p.listIDs...), nil
}

func (p *fakeMailProvider) ListMessagesPage(_ context.Context, opts email.SearchOptions, _ string) (email.MessagePage, error) {
	p.lastOpts = opts
	ids := p.pageIDs
	if len(ids) == 0 {
		ids = p.listIDs
	}
	return email.MessagePage{IDs: append([]string(nil), ids...), NextPageToken: p.nextPage}, nil
}

func (p *fakeMailProvider) GetMessage(_ context.Context, messageID, _ string) (*providerdata.EmailMessage, error) {
	msg := p.messages[messageID]
	if msg == nil {
		return nil, nil
	}
	return cloneMailMessage(msg), nil
}

func (p *fakeMailProvider) GetMessages(_ context.Context, messageIDs []string, format string) ([]*providerdata.EmailMessage, error) {
	p.getMessagesFormats = append(p.getMessagesFormats, format)
	p.lastFormat = format
	out := make([]*providerdata.EmailMessage, 0, len(messageIDs))
	for _, id := range messageIDs {
		msg := p.messages[id]
		if msg == nil {
			out = append(out, nil)
			continue
		}
		clone := cloneMailMessage(msg)
		if format == "metadata" {
			clone.BodyText = nil
			clone.BodyHTML = nil
			clone.Attachments = nil
		}
		out = append(out, clone)
	}
	return out, nil
}

func (p *fakeMailProvider) GetAttachment(_ context.Context, _, _ string) (*providerdata.AttachmentData, error) {
	if p.attachment == nil {
		return nil, nil
	}
	copyValue := *p.attachment
	copyValue.Content = append([]byte(nil), p.attachment.Content...)
	return &copyValue, nil
}

func (p *fakeMailProvider) MarkRead(_ context.Context, ids []string) (int, error) {
	return p.record("mark_read", ids), nil
}

func (p *fakeMailProvider) MarkUnread(_ context.Context, ids []string) (int, error) {
	return p.record("mark_unread", ids), nil
}

func (p *fakeMailProvider) Archive(_ context.Context, ids []string) (int, error) {
	return p.record("archive", ids), nil
}

func (p *fakeMailProvider) ArchiveResolved(_ context.Context, ids []string) ([]email.ActionResolution, error) {
	p.record("archive", ids)
	return p.resolutions(ids), nil
}

func (p *fakeMailProvider) MoveToInbox(_ context.Context, ids []string) (int, error) {
	return p.record("move_to_inbox", ids), nil
}

func (p *fakeMailProvider) MoveToInboxResolved(_ context.Context, ids []string) ([]email.ActionResolution, error) {
	p.record("move_to_inbox", ids)
	return p.resolutions(ids), nil
}
