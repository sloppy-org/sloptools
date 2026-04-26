package mcp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/sloppy-org/sloptools/internal/email"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/store"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"
)

func (s *Server) mailHandoffSelection(selector map[string]interface{}) (store.ExternalAccount, email.EmailProvider, []string, error) {
	accountID, err := int64Arg(selector, "account_id")
	if err != nil {
		return store.ExternalAccount{}, nil, nil, err
	}
	messageIDs := mailMessageIDsArg(selector)
	if len(messageIDs) == 0 {
		return store.ExternalAccount{}, nil, nil, errors.New("message_id or message_ids is required")
	}
	account, provider, err := s.mailProviderForTool(map[string]interface{}{"account_id": accountID})
	if err != nil {
		return store.ExternalAccount{}, nil, nil, err
	}
	return account, provider, messageIDs, nil
}

func orderedMailMessages(messageIDs []string, messages []*providerdata.EmailMessage) ([]*providerdata.EmailMessage, error) {
	byID := make(map[string]*providerdata.EmailMessage, len(messages))
	for _, message := range messages {
		if message == nil {
			continue
		}
		byID[strings.TrimSpace(message.ID)] = message
	}
	out := make([]*providerdata.EmailMessage, 0, len(messageIDs))
	for _, messageID := range messageIDs {
		message := byID[strings.TrimSpace(messageID)]
		if message == nil {
			return nil, fmt.Errorf("message not found: %s", messageID)
		}
		out = append(out, message)
	}
	return out, nil
}

type handoffPolicyState struct {
	expiresAt   time.Time
	maxConsumes int
}

func parseHandoffPolicy(policy map[string]interface{}, now time.Time) (handoffPolicyState, error) {
	out := handoffPolicyState{maxConsumes: defaultHandoffMaxConsumes}
	if policy == nil {
		return out, nil
	}
	if value, ok, err := optionalIntArg(policy, "max_consumes"); err != nil {
		return handoffPolicyState{}, err
	} else if ok {
		if value <= 0 {
			return handoffPolicyState{}, errors.New("max_consumes must be positive")
		}
		out.maxConsumes = value
	}
	if expiresAtRaw := strings.TrimSpace(strArg(policy, "expires_at")); expiresAtRaw != "" {
		expiresAt, err := time.Parse(time.RFC3339, expiresAtRaw)
		if err != nil {
			return handoffPolicyState{}, errors.New("expires_at must be RFC3339")
		}
		out.expiresAt = expiresAt.UTC()
	}
	if value, ok, err := optionalIntArg(policy, "ttl_seconds"); err != nil {
		return handoffPolicyState{}, err
	} else if ok {
		if value <= 0 {
			return handoffPolicyState{}, errors.New("ttl_seconds must be positive")
		}
		ttlExpiry := now.Add(time.Duration(value) * time.Second).UTC()
		if out.expiresAt.IsZero() || ttlExpiry.Before(out.expiresAt) {
			out.expiresAt = ttlExpiry
		}
	}
	return out, nil
}

func optionalIntArg(args map[string]interface{}, key string) (int, bool, error) {
	value, ok := args[key]
	if !ok {
		return 0, false, nil
	}
	switch typed := value.(type) {
	case int:
		return typed, true, nil
	case int64:
		return int(typed), true, nil
	case float64:
		return int(typed), true, nil
	default:
		return 0, false, fmt.Errorf("%s must be an integer", key)
	}
}

func newHandoffID(kind string) (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return kind + "-" + hex.EncodeToString(buf), nil
}

func (r *handoffRegistry) store(record *storedHandoff) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handoffs[record.envelope.HandoffID] = record
}

func (r *handoffRegistry) lookup(handoffID string) (*storedHandoff, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record := r.handoffs[strings.TrimSpace(handoffID)]
	if record == nil {
		return nil, errors.New("handoff not found")
	}
	copyValue := *record
	return &copyValue, nil
}

func (r *handoffRegistry) consume(handoffID string) (map[string]interface{}, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record := r.handoffs[strings.TrimSpace(handoffID)]
	if record == nil {
		return nil, errors.New("handoff not found")
	}
	if record.revoked {
		return nil, errors.New("handoff is revoked")
	}
	if record.expired(time.Now().UTC()) {
		return nil, errors.New("handoff is expired")
	}
	if record.maxConsumes > 0 && record.consumedCount >= record.maxConsumes {
		return nil, errors.New("handoff has no remaining consumes")
	}
	record.consumedCount++
	return handoffEnvelopePayload(record), nil
}

func (r *handoffRegistry) revoke(handoffID string) (map[string]interface{}, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record := r.handoffs[strings.TrimSpace(handoffID)]
	if record == nil {
		return nil, errors.New("handoff not found")
	}
	record.revoked = true
	out := handoffSummary(record)
	out["revoked"] = true
	return out, nil
}

func (h *storedHandoff) expired(now time.Time) bool {
	return !h.expiresAt.IsZero() && !now.Before(h.expiresAt)
}

func handoffSummary(record *storedHandoff) map[string]interface{} {
	return map[string]interface{}{"spec_version": record.envelope.SpecVersion, "handoff_id": record.envelope.HandoffID, "kind": record.envelope.Kind, "created_at": record.envelope.CreatedAt, "meta": record.envelope.Meta, "policy_summary": handoffPolicySummary(record)}
}

func handoffStatus(record *storedHandoff) map[string]interface{} {
	out := handoffSummary(record)
	out["revoked"] = record.revoked
	out["expired"] = record.expired(time.Now().UTC())
	return out
}

func handoffEnvelopePayload(record *storedHandoff) map[string]interface{} {
	return map[string]interface{}{"spec_version": record.envelope.SpecVersion, "handoff_id": record.envelope.HandoffID, "kind": record.envelope.Kind, "created_at": record.envelope.CreatedAt, "meta": record.envelope.Meta, "payload": record.envelope.Payload, "policy": handoffPolicySummary(record)}
}

func handoffPolicySummary(record *storedHandoff) map[string]interface{} {
	remaining := 0
	if record.maxConsumes > 0 {
		remaining = max(record.maxConsumes-record.consumedCount, 0)
	}
	out := map[string]interface{}{"max_consumes": record.maxConsumes, "consumed_count": record.consumedCount, "remaining_consumes": remaining, "revoked": record.revoked, "expired": record.expired(time.Now().UTC())}
	if !record.expiresAt.IsZero() {
		out["expires_at"] = record.expiresAt.Format(time.RFC3339)
	}
	return out
}

func mailHandoffMeta(account store.ExternalAccount, messages []*providerdata.EmailMessage) map[string]interface{} {
	messageIDs := make([]string, 0, len(messages))
	subjects := make([]string, 0, len(messages))
	senders := make([]string, 0, len(messages))
	recipients := make([]string, 0, len(messages))
	dates := make([]string, 0, len(messages))
	internetMessageIDs := make([]string, 0, len(messages))
	threadIDs := make([]string, 0, len(messages))
	for _, message := range messages {
		messageIDs = append(messageIDs, strings.TrimSpace(message.ID))
		subjects = append(subjects, strings.TrimSpace(message.Subject))
		senders = append(senders, strings.TrimSpace(message.Sender))
		recipients = append(recipients, message.Recipients...)
		if !message.Date.IsZero() {
			dates = append(dates, message.Date.UTC().Format(time.RFC3339))
		}
		if value := strings.TrimSpace(message.InternetMessageID); value != "" {
			internetMessageIDs = append(internetMessageIDs, value)
		}
		if value := strings.TrimSpace(message.ThreadID); value != "" {
			threadIDs = append(threadIDs, value)
		}
	}
	return map[string]interface{}{"account": map[string]interface{}{"id": account.ID, "name": account.AccountName, "provider": account.Provider, "sphere": account.Sphere}, "message_count": len(messages), "message_ids": messageIDs, "subjects": subjects, "senders": compactStringList(senders), "recipients": compactStringList(recipients), "dates": dates, "internet_message_ids": compactStringList(internetMessageIDs), "thread_ids": compactStringList(threadIDs), "attachment_count": mailAttachmentCount(messages), "contains_rich_content": mailContainsRichContent(messages)}
}

func mailHandoffMessages(messages []*providerdata.EmailMessage) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(messages))
	for _, message := range messages {
		entry := map[string]interface{}{"message_id": strings.TrimSpace(message.ID), "thread_id": strings.TrimSpace(message.ThreadID), "internet_message_id": strings.TrimSpace(message.InternetMessageID), "subject": strings.TrimSpace(message.Subject), "sender": strings.TrimSpace(message.Sender), "recipients": append([]string(nil), message.Recipients...), "snippet": strings.TrimSpace(message.Snippet), "labels": append([]string(nil), message.Labels...), "is_read": message.IsRead, "is_flagged": message.IsFlagged, "attachments": mailHandoffAttachments(message.Attachments), "has_body_text": message.BodyText != nil, "has_body_html": message.BodyHTML != nil, "attachment_count": len(message.Attachments), "recipient_count": len(message.Recipients), "label_count": len(message.Labels), "contains_attachments": len(message.Attachments) > 0}
		if !message.Date.IsZero() {
			entry["date"] = message.Date.UTC().Format(time.RFC3339)
		}
		if message.BodyText != nil {
			entry["body_text"] = *message.BodyText
		}
		if message.BodyHTML != nil {
			entry["body_html"] = *message.BodyHTML
		}
		out = append(out, entry)
	}
	return out
}

func mailHandoffAttachments(attachments []providerdata.Attachment) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(attachments))
	for _, attachment := range attachments {
		out = append(out, map[string]interface{}{"id": strings.TrimSpace(attachment.ID), "filename": strings.TrimSpace(attachment.Filename), "mime_type": strings.TrimSpace(attachment.MimeType), "size": attachment.Size, "is_inline": attachment.IsInline})
	}
	return out
}

func mailAttachmentCount(messages []*providerdata.EmailMessage) int {
	count := 0
	for _, message := range messages {
		count += len(message.Attachments)
	}
	return count
}

func mailContainsRichContent(messages []*providerdata.EmailMessage) bool {
	return slices.ContainsFunc(messages, func(message *providerdata.EmailMessage) bool {
		return message != nil && (message.BodyText != nil || message.BodyHTML != nil)
	})
}

const mcpEmailBindingObjectType = "email"

func (s *Server) mailAccountList(args map[string]interface{}) (map[string]interface{}, error) {
	st, err := s.requireStore()
	if err != nil {
		return nil, err
	}
	sphere := strings.TrimSpace(strArg(args, "sphere"))
	accounts, err := st.ListExternalAccounts(sphere)
	if err != nil {
		return nil, err
	}
	out := make([]store.ExternalAccount, 0, len(accounts))
	for _, account := range accounts {
		if account.Enabled && store.IsEmailProvider(account.Provider) {
			out = append(out, account)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Sphere == out[j].Sphere {
			return strings.ToLower(out[i].AccountName) < strings.ToLower(out[j].AccountName)
		}
		return out[i].Sphere < out[j].Sphere
	})
	return map[string]interface{}{"accounts": out, "count": len(out)}, nil
}

func (s *Server) mailLabelList(args map[string]interface{}) (map[string]interface{}, error) {
	account, provider, err := s.mailProviderForTool(args)
	if err != nil {
		return nil, err
	}
	defer provider.Close()
	labels, err := provider.ListLabels(context.Background())
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"account": account, "labels": labels, "count": len(labels)}, nil
}

func (s *Server) mailMessageList(args map[string]interface{}) (map[string]interface{}, error) {
	account, provider, err := s.mailProviderForTool(args)
	if err != nil {
		return nil, err
	}
	defer provider.Close()
	opts, pageToken, err := mailSearchOptionsFromArgs(args)
	if err != nil {
		return nil, err
	}
	if opts.MaxResults <= 0 || opts.MaxResults > 50 {
		opts.MaxResults = 20
	}
	ids, nextPageToken, err := listMailMessageIDs(context.Background(), provider, opts, pageToken)
	if err != nil {
		return nil, err
	}
	format := "metadata"
	if boolArg(args, "include_body") {
		format = "full"
	}
	messages, err := provider.GetMessages(context.Background(), ids, format)
	if err != nil {
		return nil, err
	}
	sort.Slice(messages, func(i, j int) bool {
		return messages[i].Date.After(messages[j].Date)
	})
	return map[string]interface{}{"account": account, "messages": mailMessageListPayloads(messages, format == "full"), "count": len(messages), "page_token": pageToken, "next_page_token": nextPageToken}, nil
}

func (s *Server) mailMessageGet(args map[string]interface{}) (map[string]interface{}, error) {
	account, provider, err := s.mailProviderForTool(args)
	if err != nil {
		return nil, err
	}
	defer provider.Close()
	messageID := strings.TrimSpace(strArg(args, "message_id"))
	if messageID == "" {
		return nil, fmt.Errorf("message_id is required")
	}
	message, err := provider.GetMessage(context.Background(), messageID, "full")
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"account": account, "message": message}, nil
}

func (s *Server) mailAttachmentGet(args map[string]interface{}) (map[string]interface{}, error) {
	account, provider, err := s.mailProviderForTool(args)
	if err != nil {
		return nil, err
	}
	defer provider.Close()
	messageID := strings.TrimSpace(strArg(args, "message_id"))
	if messageID == "" {
		return nil, fmt.Errorf("message_id is required")
	}
	attachmentID := strings.TrimSpace(strArg(args, "attachment_id"))
	if attachmentID == "" {
		return nil, fmt.Errorf("attachment_id is required")
	}
	attachmentProvider, ok := provider.(email.AttachmentProvider)
	if !ok {
		return nil, fmt.Errorf("attachments are not supported for this account")
	}
	attachment, err := attachmentProvider.GetAttachment(context.Background(), messageID, attachmentID)
	if err != nil {
		return nil, err
	}
	destDir, err := resolveAttachmentDir(strArg(args, "dest_dir"))
	if err != nil {
		return nil, err
	}
	absPath, err := writeAttachmentFile(destDir, account.AccountName, messageID, attachment)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"account": account, "attachment": map[string]interface{}{"id": strings.TrimSpace(attachment.ID), "filename": strings.TrimSpace(attachment.Filename), "mime_type": strings.TrimSpace(attachment.MimeType), "size": attachment.Size, "is_inline": attachment.IsInline, "path": absPath, "size_bytes": len(attachment.Content)}}, nil
}

func resolveAttachmentDir(arg string) (string, error) {
	dest := strings.TrimSpace(arg)
	if dest == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home dir: %w", err)
		}
		dest = filepath.Join(home, "Downloads", "sloppy-attachments")
	}
	if strings.HasPrefix(dest, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home dir: %w", err)
		}
		dest = filepath.Join(home, strings.TrimPrefix(dest, "~/"))
	}
	absDir, err := filepath.Abs(dest)
	if err != nil {
		return "", fmt.Errorf("resolve dest_dir: %w", err)
	}
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		return "", fmt.Errorf("create dest_dir: %w", err)
	}
	return absDir, nil
}

func writeAttachmentFile(destDir, account, messageID string, a *providerdata.AttachmentData) (string, error) {
	filename := sanitizeFilenameComponent(strings.TrimSpace(a.Filename))
	if filename == "" {
		filename = sanitizeFilenameComponent(strings.TrimSpace(a.ID))
	}
	if filename == "" {
		filename = "attachment"
	}
	prefix := sanitizeFilenameComponent(strings.TrimSpace(account))
	if prefix == "" {
		prefix = "unknown-account"
	}
	msgTag := sanitizeFilenameComponent(strings.TrimSpace(messageID))
	if len(msgTag) > 16 {
		msgTag = msgTag[:16]
	}
	if msgTag != "" {
		prefix = prefix + "_" + msgTag
	}
	base := prefix + "_" + filename
	absPath := filepath.Join(destDir, base)
	absPath, err := writeNoClobber(absPath, a.Content)
	if err != nil {
		return "", fmt.Errorf("write attachment: %w", err)
	}
	return absPath, nil
}

func writeNoClobber(path string, data []byte) (string, error) {
	candidate := path
	for i := 0; i < 1000; i++ {
		f, err := os.OpenFile(candidate, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			defer f.Close()
			if _, werr := f.Write(data); werr != nil {
				return "", werr
			}
			return candidate, nil
		}
		if !os.IsExist(err) {
			return "", err
		}
		ext := filepath.Ext(path)
		stem := strings.TrimSuffix(path, ext)
		candidate = fmt.Sprintf("%s-%d%s", stem, i+1, ext)
	}
	return "", fmt.Errorf("too many filename collisions for %s", path)
}

func sanitizeFilenameComponent(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			b.WriteRune(r)
		case r == '.' || r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	cleaned := strings.Trim(b.String(), ".")
	if len(cleaned) > 120 {
		cleaned = cleaned[:120]
	}
	return cleaned
}
