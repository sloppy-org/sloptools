package email

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
)

var _ DraftProvider = (*IMAPClient)(nil)

func (c *IMAPClient) CreateDraft(ctx context.Context, input DraftInput) (Draft, error) {
	normalized, err := c.normalizeIMAPDraftInput(input)
	if err != nil {
		return Draft{}, err
	}
	if err := c.connect(ctx); err != nil {
		return Draft{}, err
	}
	raw, err := buildRFC822Message(normalized)
	if err != nil {
		return Draft{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	mailbox, err := c.resolveDraftMailboxLocked()
	if err != nil {
		return Draft{}, err
	}
	draftID, err := c.appendDraftLocked(mailbox, raw)
	if err != nil {
		return Draft{}, err
	}
	return Draft{ID: draftID, ThreadID: normalized.ThreadID}, nil
}

func (c *IMAPClient) CreateReplyDraft(ctx context.Context, messageID string, input DraftInput) (Draft, error) {
	reply := input
	message, err := c.GetMessage(ctx, strings.TrimSpace(messageID), "full")
	if err == nil && message != nil {
		if len(reply.To) == 0 && strings.TrimSpace(message.Sender) != "" {
			reply.To = []string{strings.TrimSpace(message.Sender)}
		}
		if strings.TrimSpace(reply.Subject) == "" {
			reply.Subject = ensureReplySubject(message.Subject)
		} else {
			reply.Subject = ensureReplySubject(reply.Subject)
		}
		if strings.TrimSpace(reply.InReplyTo) == "" && strings.TrimSpace(message.ThreadID) != "" {
			reply.InReplyTo = strings.TrimSpace(message.ThreadID)
		}
		if len(reply.References) == 0 && strings.TrimSpace(message.ThreadID) != "" {
			reply.References = []string{strings.TrimSpace(message.ThreadID)}
		}
	}
	return c.CreateDraft(ctx, reply)
}

func (c *IMAPClient) UpdateDraft(ctx context.Context, draftID string, input DraftInput) (Draft, error) {
	folder, uid, err := parseMessageID(strings.TrimSpace(draftID))
	if err != nil {
		return Draft{}, err
	}
	normalized, err := c.normalizeIMAPDraftInput(input)
	if err != nil {
		return Draft{}, err
	}
	if err := c.connect(ctx); err != nil {
		return Draft{}, err
	}
	raw, err := buildRFC822Message(normalized)
	if err != nil {
		return Draft{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.deleteDraftLocked(folder, uid); err != nil {
		return Draft{}, err
	}
	nextID, err := c.appendDraftLocked(folder, raw)
	if err != nil {
		return Draft{}, err
	}
	return Draft{ID: nextID, ThreadID: normalized.ThreadID}, nil
}

func (c *IMAPClient) SendDraft(ctx context.Context, draftID string, input DraftInput) error {
	normalized, err := c.normalizeIMAPDraftInput(input)
	if err != nil {
		return err
	}
	recipients := append([]string{}, normalized.To...)
	recipients = append(recipients, normalized.Cc...)
	recipients = append(recipients, normalized.Bcc...)
	raw, err := buildRFC822Message(normalized)
	if err != nil {
		return err
	}
	sender := c.smtpSend
	if sender == nil {
		sender = defaultSMTPSender
	}
	if err := sender(ctx, c.smtpConfig, normalized.From, recipients, raw); err != nil {
		return fmt.Errorf("imap smtp send: %w", err)
	}
	if strings.TrimSpace(draftID) == "" {
		return nil
	}
	folder, uid, err := parseMessageID(strings.TrimSpace(draftID))
	if err != nil {
		return err
	}
	if err := c.connect(ctx); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.deleteDraftLocked(folder, uid)
}

func (c *IMAPClient) normalizeIMAPDraftInput(input DraftInput) (DraftInput, error) {
	reply := input
	if strings.TrimSpace(reply.From) == "" {
		reply.From = strings.TrimSpace(c.smtpConfig.From)
	}
	if strings.TrimSpace(reply.From) == "" {
		reply.From = strings.TrimSpace(c.smtpConfig.Username)
	}
	if strings.TrimSpace(reply.From) == "" {
		reply.From = strings.TrimSpace(c.username)
	}
	return NormalizeDraftInput(reply)
}

func (c *IMAPClient) resolveDraftMailboxLocked() (string, error) {
	candidates := []string{}
	if strings.TrimSpace(c.smtpConfig.DraftsBox) != "" {
		candidates = append(candidates, strings.TrimSpace(c.smtpConfig.DraftsBox))
	}
	candidates = append(candidates, "Drafts", "Entwürfe")
	fallback := strings.TrimSpace(c.smtpConfig.DraftsBox)
	if fallback == "" {
		fallback = "Drafts"
	}
	return c.resolveOrCreateMailboxLocked(candidates, fallback)
}

func (c *IMAPClient) appendDraftLocked(mailbox string, raw []byte) (string, error) {
	cmd := c.client.Append(mailbox, int64(len(raw)), &imap.AppendOptions{
		Flags: []imap.Flag{imap.FlagDraft},
		Time:  time.Now(),
	})
	if _, err := cmd.Write(raw); err != nil {
		_ = cmd.Close()
		return "", fmt.Errorf("imap append draft write: %w", err)
	}
	if err := cmd.Close(); err != nil {
		return "", fmt.Errorf("imap append draft close: %w", err)
	}
	result, err := cmd.Wait()
	if err != nil {
		return "", fmt.Errorf("imap append draft: %w", err)
	}
	if result == nil || result.UID == 0 {
		return "", fmt.Errorf("imap append draft did not return a UID")
	}
	return formatMessageID(mailbox, result.UID), nil
}

func (c *IMAPClient) deleteDraftLocked(mailbox string, uid imap.UID) error {
	if _, err := c.client.Select(mailbox, nil).Wait(); err != nil {
		return fmt.Errorf("imap select draft mailbox: %w", err)
	}
	uidSet := uidSetFromSlice([]imap.UID{uid})
	storeCmd := c.client.Store(uidSet, &imap.StoreFlags{
		Op:     imap.StoreFlagsAdd,
		Silent: true,
		Flags:  []imap.Flag{imap.FlagDeleted},
	}, nil)
	if err := storeCmd.Close(); err != nil {
		return fmt.Errorf("imap mark draft deleted: %w", err)
	}
	caps := c.client.Caps()
	if caps.Has(imap.CapUIDPlus) || caps.Has(imap.CapIMAP4rev2) {
		if err := c.client.UIDExpunge(uidSet).Close(); err != nil {
			return fmt.Errorf("imap expunge draft: %w", err)
		}
		return nil
	}
	if err := c.client.Expunge().Close(); err != nil {
		return fmt.Errorf("imap expunge draft: %w", err)
	}
	return nil
}

func formatMessageID(mailbox string, uid imap.UID) string {
	return fmt.Sprintf("%s:%d", strings.TrimSpace(mailbox), uid)
}
