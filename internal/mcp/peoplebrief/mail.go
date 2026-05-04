package peoplebrief

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/email"
	"github.com/sloppy-org/sloptools/internal/providerdata"
)

const personMailMaxResults = 10

// MailLister is the minimal mail-provider surface LatestPersonMail needs.
// `email.EmailProvider` satisfies it, and tests can hand-roll a tiny
// fake without implementing the entire mailbox-mutation contract.
type MailLister interface {
	ListMessages(ctx context.Context, opts email.SearchOptions) ([]string, error)
	GetMessages(ctx context.Context, messageIDs []string, format string) ([]*providerdata.EmailMessage, error)
}

// LatestPersonMail asks the provider for the most recent message from the
// person's email and returns the metadata-only digest. The provider is not
// closed by this helper; callers own its lifecycle.
func LatestPersonMail(ctx context.Context, provider MailLister, accountID int64, personEmail string) (*Mail, error) {
	personEmail = strings.TrimSpace(personEmail)
	if personEmail == "" {
		return nil, errors.New("person email is required")
	}
	opts := email.DefaultSearchOptions()
	opts.From = personEmail
	if opts.MaxResults <= 0 || opts.MaxResults > personMailMaxResults {
		opts.MaxResults = personMailMaxResults
	}
	ids, err := listMailMessageIDs(ctx, provider, opts)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	messages, err := provider.GetMessages(ctx, ids, "metadata")
	if err != nil {
		return nil, err
	}
	latest := newestMail(messages)
	if latest == nil {
		return nil, nil
	}
	return &Mail{
		AccountID: accountID,
		MessageID: latest.ID,
		ThreadID:  latest.ThreadID,
		Subject:   latest.Subject,
		Date:      formatMailDate(latest.Date),
		Folder:    latest.Folder,
	}, nil
}

func listMailMessageIDs(ctx context.Context, provider MailLister, opts email.SearchOptions) ([]string, error) {
	if pager, ok := provider.(email.MessagePageProvider); ok {
		page, err := pager.ListMessagesPage(ctx, opts, "")
		if err != nil {
			return nil, err
		}
		return page.IDs, nil
	}
	return provider.ListMessages(ctx, opts)
}

func newestMail(messages []*providerdata.EmailMessage) *providerdata.EmailMessage {
	var latest *providerdata.EmailMessage
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		if latest == nil || msg.Date.After(latest.Date) {
			latest = msg
		}
	}
	return latest
}

func formatMailDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
