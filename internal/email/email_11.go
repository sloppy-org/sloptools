package email

import (
	"context"
	"fmt"
	imap "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"strconv"
	"strings"
	"sync"
	"time"
)

func buildIMAPSearchCriteria(opts SearchOptions) *imap.SearchCriteria {
	criteria := &imap.SearchCriteria{}
	if opts.Text != "" {
		criteria.Text = append(criteria.Text, opts.Text)
	}
	if opts.Subject != "" {
		criteria.Header = append(criteria.Header, imap.SearchCriteriaHeaderField{Key: "Subject", Value: opts.Subject})
	}
	if opts.From != "" {
		criteria.Header = append(criteria.Header, imap.SearchCriteriaHeaderField{Key: "From", Value: opts.From})
	}
	if opts.To != "" {
		criteria.Header = append(criteria.Header, imap.SearchCriteriaHeaderField{Key: "To", Value: opts.To})
	}
	if !opts.After.IsZero() {
		criteria.Since = opts.After
	}
	if !opts.Before.IsZero() {
		criteria.Before = opts.Before
	}
	if !opts.Since.IsZero() {
		criteria.Since = opts.Since
	}
	if !opts.Until.IsZero() {
		criteria.Before = opts.Until.AddDate(0, 0, 1)
	}
	if opts.IsRead != nil {
		if *opts.IsRead {
			criteria.Flag = append(criteria.Flag, imap.FlagSeen)
		} else {
			criteria.NotFlag = append(criteria.NotFlag, imap.FlagSeen)
		}
	}
	if opts.IsFlagged != nil {
		if *opts.IsFlagged {
			criteria.Flag = append(criteria.Flag, imap.FlagFlagged)
		} else {
			criteria.NotFlag = append(criteria.NotFlag, imap.FlagFlagged)
		}
	}
	if opts.SizeGreater > 0 {
		criteria.Larger = opts.SizeGreater
	}
	if opts.SizeLess > 0 {
		criteria.Smaller = opts.SizeLess
	}
	return criteria
}

func (c *IMAPClient) GetMessage(ctx context.Context, messageID, format string) (*providerdata.EmailMessage, error) {
	folder, uid, err := parseMessageID(messageID)
	if err != nil {
		return nil, err
	}
	if err := c.connect(ctx); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err = c.client.Select(folder, &imap.SelectOptions{ReadOnly: true}).Wait()
	if err != nil {
		return nil, fmt.Errorf("failed to select folder %q: %w", folder, err)
	}
	fetchOpts := &imap.FetchOptions{UID: true, Flags: true, Envelope: true}
	if format == "full" {
		fetchOpts.BodySection = []*imap.FetchItemBodySection{{Peek: true}}
	}
	uidSet := imap.UIDSet{}
	uidSet.AddNum(uid)
	fetchCmd := c.client.Fetch(uidSet, fetchOpts)
	msg := fetchCmd.Next()
	if msg == nil {
		return nil, fmt.Errorf("message not found: %s", messageID)
	}
	msgBuf, err := msg.Collect()
	if err != nil {
		return nil, fmt.Errorf("failed to collect message: %w", err)
	}
	for fetchCmd.Next() != nil {
	}
	return parseIMAPMessage(folder, msgBuf, format == "full")
}

func (c *IMAPClient) GetMessages(ctx context.Context, messageIDs []string, format string) ([]*providerdata.EmailMessage, error) {
	if len(messageIDs) == 0 {
		return nil, nil
	}
	byFolder := make(map[string][]imap.UID)
	for _, id := range messageIDs {
		folder, uid, err := parseMessageID(id)
		if err != nil {
			continue
		}
		byFolder[folder] = append(byFolder[folder], uid)
	}
	var results []*providerdata.EmailMessage
	var resultsMu sync.Mutex
	for folder, uids := range byFolder {
		msgs, err := c.fetchMessages(ctx, folder, uids, format)
		if err != nil {
			continue
		}
		resultsMu.Lock()
		results = append(results, msgs...)
		resultsMu.Unlock()
	}
	return results, nil
}

func (c *IMAPClient) fetchMessages(ctx context.Context, folder string, uids []imap.UID, format string) ([]*providerdata.EmailMessage, error) {
	if err := c.connect(ctx); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err := c.client.Select(folder, &imap.SelectOptions{ReadOnly: true}).Wait()
	if err != nil {
		return nil, fmt.Errorf("failed to select folder %q: %w", folder, err)
	}
	fetchOpts := &imap.FetchOptions{UID: true, Flags: true, Envelope: true}
	if format == "full" {
		fetchOpts.BodySection = []*imap.FetchItemBodySection{{Peek: true}}
	}
	uidSet := imap.UIDSet{}
	for _, uid := range uids {
		uidSet.AddNum(uid)
	}
	fetchCmd := c.client.Fetch(uidSet, fetchOpts)
	var results []*providerdata.EmailMessage
	for {
		msg := fetchCmd.Next()
		if msg == nil {
			break
		}
		msgBuf, err := msg.Collect()
		if err != nil {
			continue
		}
		email, err := parseIMAPMessage(folder, msgBuf, format == "full")
		if err != nil {
			continue
		}
		results = append(results, email)
	}
	return results, nil
}

func (c *IMAPClient) MarkRead(ctx context.Context, messageIDs []string) (int, error) {
	return c.storeFlags(ctx, messageIDs, imap.StoreFlagsAdd, []imap.Flag{imap.FlagSeen})
}

func (c *IMAPClient) MarkUnread(ctx context.Context, messageIDs []string) (int, error) {
	return c.storeFlags(ctx, messageIDs, imap.StoreFlagsDel, []imap.Flag{imap.FlagSeen})
}

func (c *IMAPClient) Archive(ctx context.Context, messageIDs []string) (int, error) {
	return c.moveMessages(ctx, messageIDs, []string{"Archive", "Archiv"}, "Archive", "archive")
}

func (c *IMAPClient) MoveToInbox(ctx context.Context, messageIDs []string) (int, error) {
	return c.moveMessages(ctx, messageIDs, []string{"INBOX"}, "INBOX", "move to inbox")
}

func (c *IMAPClient) Trash(ctx context.Context, messageIDs []string) (int, error) {
	return c.moveMessages(ctx, messageIDs, []string{"Gelöschte Elemente", "Trash", "TRASH", "Deleted Items", "Deleted Messages"}, "Trash", "trash")
}

func (c *IMAPClient) Delete(ctx context.Context, messageIDs []string) (int, error) {
	if len(messageIDs) == 0 {
		return 0, nil
	}
	byFolder, invalidIDs := groupMessageIDsByFolder(messageIDs)
	if err := c.connect(ctx); err != nil {
		return 0, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	caps := c.client.Caps()
	supportsUIDExpunge := caps.Has(imap.CapUIDPlus) || caps.Has(imap.CapIMAP4rev2)
	succeeded := 0
	var errMsgs []string
	for folder, uids := range byFolder {
		if len(uids) == 0 {
			continue
		}
		if _, err := c.client.Select(folder, nil).Wait(); err != nil {
			errMsgs = append(errMsgs, fmt.Sprintf("%s: select failed: %v", folder, err))
			continue
		}
		uidSet := uidSetFromSlice(uids)
		storeCmd := c.client.Store(uidSet, &imap.StoreFlags{Op: imap.StoreFlagsAdd, Silent: true, Flags: []imap.Flag{imap.FlagDeleted}}, nil)
		if err := storeCmd.Close(); err != nil {
			errMsgs = append(errMsgs, fmt.Sprintf("%s: mark deleted failed: %v", folder, err))
			continue
		}
		var expungeErr error
		if supportsUIDExpunge {
			expungeErr = c.client.UIDExpunge(uidSet).Close()
		} else {
			expungeErr = c.client.Expunge().Close()
		}
		if expungeErr != nil {
			errMsgs = append(errMsgs, fmt.Sprintf("%s: expunge failed: %v", folder, expungeErr))
			continue
		}
		succeeded += len(uids)
	}
	for _, id := range invalidIDs {
		errMsgs = append(errMsgs, fmt.Sprintf("%s: invalid message ID", id))
	}
	if len(errMsgs) > 0 {
		return succeeded, fmt.Errorf("delete completed with errors: %s", strings.Join(errMsgs, "; "))
	}
	return succeeded, nil
}

func (c *IMAPClient) Defer(_ context.Context, messageID string, _ time.Time) (MessageActionResult, error) {
	return MessageActionResult{Provider: c.ProviderName(), Action: "defer", MessageID: messageID, Status: "stub_not_supported", EffectiveProviderMode: "stub", StubReason: "DEFER_NOT_SUPPORTED_FOR_PROVIDER"}, nil
}

func (c *IMAPClient) SupportsNativeDefer() bool {
	return false
}

func (c *IMAPClient) moveMessages(ctx context.Context, messageIDs []string, destinationCandidates []string, fallbackDestination, opName string) (int, error) {
	if len(messageIDs) == 0 {
		return 0, nil
	}
	byFolder, invalidIDs := groupMessageIDsByFolder(messageIDs)
	if err := c.connect(ctx); err != nil {
		return 0, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	destination, err := c.resolveOrCreateMailboxLocked(destinationCandidates, fallbackDestination)
	if err != nil {
		return 0, err
	}
	succeeded := 0
	var errMsgs []string
	for folder, uids := range byFolder {
		if len(uids) == 0 {
			continue
		}
		if folder == destination {
			succeeded += len(uids)
			continue
		}
		if _, err := c.client.Select(folder, nil).Wait(); err != nil {
			errMsgs = append(errMsgs, fmt.Sprintf("%s: select failed: %v", folder, err))
			continue
		}
		uidSet := uidSetFromSlice(uids)
		if _, err := c.client.Move(uidSet, destination).Wait(); err != nil {
			errMsgs = append(errMsgs, fmt.Sprintf("%s: move failed: %v", folder, err))
			continue
		}
		succeeded += len(uids)
	}
	for _, id := range invalidIDs {
		errMsgs = append(errMsgs, fmt.Sprintf("%s: invalid message ID", id))
	}
	if len(errMsgs) > 0 {
		return succeeded, fmt.Errorf("%s completed with errors: %s", opName, strings.Join(errMsgs, "; "))
	}
	return succeeded, nil
}

func (c *IMAPClient) storeFlags(ctx context.Context, messageIDs []string, op imap.StoreFlagsOp, flags []imap.Flag) (int, error) {
	if len(messageIDs) == 0 {
		return 0, nil
	}
	byFolder, invalidIDs := groupMessageIDsByFolder(messageIDs)
	if err := c.connect(ctx); err != nil {
		return 0, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	succeeded := 0
	var errMsgs []string
	for folder, uids := range byFolder {
		if len(uids) == 0 {
			continue
		}
		if _, err := c.client.Select(folder, nil).Wait(); err != nil {
			errMsgs = append(errMsgs, fmt.Sprintf("%s: select failed: %v", folder, err))
			continue
		}
		uidSet := uidSetFromSlice(uids)
		storeCmd := c.client.Store(uidSet, &imap.StoreFlags{Op: op, Silent: true, Flags: flags}, nil)
		if err := storeCmd.Close(); err != nil {
			errMsgs = append(errMsgs, fmt.Sprintf("%s: store failed: %v", folder, err))
			continue
		}
		succeeded += len(uids)
	}
	for _, id := range invalidIDs {
		errMsgs = append(errMsgs, fmt.Sprintf("%s: invalid message ID", id))
	}
	if len(errMsgs) > 0 {
		return succeeded, fmt.Errorf("flag update completed with errors: %s", strings.Join(errMsgs, "; "))
	}
	return succeeded, nil
}

func (c *IMAPClient) resolveOrCreateMailboxLocked(candidates []string, fallback string) (string, error) {
	mailboxes, err := c.client.List("", "*", nil).Collect()
	if err != nil {
		return "", fmt.Errorf("failed to list mailboxes: %w", err)
	}
	existing := make(map[string]struct{}, len(mailboxes))
	for _, mbox := range mailboxes {
		existing[mbox.Mailbox] = struct{}{}
	}
	for _, name := range candidates {
		if _, ok := existing[name]; ok {
			return name, nil
		}
	}
	if fallback == "" {
		return "", fmt.Errorf("no destination mailbox available")
	}
	if err := c.client.Create(fallback, nil).Wait(); err != nil {
		if _, ok := existing[fallback]; !ok {
			return "", fmt.Errorf("failed to create mailbox %q: %w", fallback, err)
		}
	}
	c.mailboxCacheMu.Lock()
	c.mailboxCache = nil
	c.mailboxCacheTime = time.Time{}
	c.mailboxCacheMu.Unlock()
	return fallback, nil
}

func groupMessageIDsByFolder(messageIDs []string) (map[string][]imap.UID, []string) {
	byFolder := make(map[string][]imap.UID)
	seen := make(map[string]map[imap.UID]bool)
	var invalid []string
	for _, id := range messageIDs {
		folder, uid, err := parseMessageID(id)
		if err != nil {
			invalid = append(invalid, id)
			continue
		}
		if seen[folder] == nil {
			seen[folder] = make(map[imap.UID]bool)
		}
		if seen[folder][uid] {
			continue
		}
		seen[folder][uid] = true
		byFolder[folder] = append(byFolder[folder], uid)
	}
	return byFolder, invalid
}

func uidSetFromSlice(uids []imap.UID) imap.UIDSet {
	set := imap.UIDSet{}
	for _, uid := range uids {
		set.AddNum(uid)
	}
	return set
}

func (c *IMAPClient) ProviderName() string {
	return c.name
}

func (c *IMAPClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for {
		select {
		case conn := <-c.pool:
			conn.Close()
		default:
			goto poolDrained
		}
	}
poolDrained:
	if c.client != nil {
		err := c.client.Close()
		c.client = nil
		return err
	}
	return nil
}

func parseMessageID(messageID string) (folder string, uid imap.UID, err error) {
	parts := strings.SplitN(messageID, ":", 2)
	if len(parts) != 2 {
		return "", 0, fmt.Errorf("invalid message ID format: %s", messageID)
	}
	uidNum, err := strconv.ParseUint(parts[1], 10, 32)
	if err != nil {
		return "", 0, fmt.Errorf("invalid UID in message ID: %s", messageID)
	}
	return parts[0], imap.UID(uidNum), nil
}

func parseIMAPMessage(folder string, msg *imapclient.FetchMessageBuffer, fetchBody bool) (*providerdata.EmailMessage, error) {
	email := &providerdata.EmailMessage{ID: fmt.Sprintf("%s:%d", folder, msg.UID), Labels: []string{folder}, Folder: folder}
	seenFound := false
	flaggedFound := false
	for _, flag := range msg.Flags {
		if flag == imap.FlagSeen {
			seenFound = true
		}
		if flag == imap.FlagFlagged {
			flaggedFound = true
		}
	}
	email.IsRead = seenFound
	email.IsFlagged = flaggedFound
	if msg.Envelope != nil {
		env := msg.Envelope
		email.Subject = env.Subject
		if email.Subject == "" {
			email.Subject = "(No subject)"
		}
		if len(env.From) > 0 {
			email.Sender = formatAddress(env.From[0])
		}
		for _, addr := range env.To {
			email.Recipients = append(email.Recipients, formatAddress(addr))
		}
		if !env.Date.IsZero() {
			email.Date = env.Date
		} else {
			email.Date = time.Now()
		}
		email.ThreadID = env.MessageID
	}
	if fetchBody && len(msg.BodySection) > 0 {
		for _, section := range msg.BodySection {
			bodyText, bodyHTML := extractIMAPBody(section.Bytes)
			if bodyText != "" {
				email.BodyText = &bodyText
			}
			if bodyHTML != "" {
				email.BodyHTML = &bodyHTML
			}
		}
	}
	return email, nil
}

func formatAddress(addr imap.Address) string {
	if addr.Name != "" {
		return fmt.Sprintf("%s <%s@%s>", addr.Name, addr.Mailbox, addr.Host)
	}
	return fmt.Sprintf("%s@%s", addr.Mailbox, addr.Host)
}
