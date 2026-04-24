package mcp

import (
	"context"
	"errors"
	"fmt"
	"github.com/sloppy-org/sloptools/internal/email"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/store"
	"strings"
	"time"
)

func applyMailActionGeneric(ctx context.Context, account store.ExternalAccount, provider email.EmailProvider, action string, messageIDs []string, folder, label string, archive *bool, untilAt time.Time) (mcpMailActionApplyResult, error) {
	switch action {
	case "mark_read":
		count, err := provider.MarkRead(ctx, messageIDs)
		return mcpMailActionApplyResult{Count: count}, err
	case "mark_unread":
		count, err := provider.MarkUnread(ctx, messageIDs)
		return mcpMailActionApplyResult{Count: count}, err
	case "archive":
		if resolvedProvider, ok := provider.(email.ResolvedArchiveProvider); ok {
			resolutions, err := resolvedProvider.ArchiveResolved(ctx, messageIDs)
			return mcpMailActionApplyResult{Count: len(resolutions), Resolutions: resolutions}, err
		}
		count, err := provider.Archive(ctx, messageIDs)
		return mcpMailActionApplyResult{Count: count}, err
	case "move_to_inbox":
		if resolvedProvider, ok := provider.(email.ResolvedMoveToInboxProvider); ok {
			resolutions, err := resolvedProvider.MoveToInboxResolved(ctx, messageIDs)
			return mcpMailActionApplyResult{Count: len(resolutions), Resolutions: resolutions}, err
		}
		count, err := provider.MoveToInbox(ctx, messageIDs)
		return mcpMailActionApplyResult{Count: count}, err
	case "trash":
		if resolvedProvider, ok := provider.(email.ResolvedTrashProvider); ok {
			resolutions, err := resolvedProvider.TrashResolved(ctx, messageIDs)
			return mcpMailActionApplyResult{Count: len(resolutions), Resolutions: resolutions}, err
		}
		count, err := provider.Trash(ctx, messageIDs)
		return mcpMailActionApplyResult{Count: count}, err
	case "delete":
		count, err := provider.Delete(ctx, messageIDs)
		return mcpMailActionApplyResult{Count: count}, err
	case "defer":
		actionProvider, ok := provider.(email.MessageActionProvider)
		if !ok || !actionProvider.SupportsNativeDefer() {
			return mcpMailActionApplyResult{}, fmt.Errorf("defer is not supported for provider %s", account.Provider)
		}
		count := 0
		for _, messageID := range messageIDs {
			if _, err := actionProvider.Defer(ctx, messageID, untilAt); err != nil {
				return mcpMailActionApplyResult{}, err
			}
			count++
		}
		return mcpMailActionApplyResult{Count: count}, nil
	case "move_to_folder":
		folderProvider, ok := provider.(email.NamedFolderProvider)
		if !ok {
			return mcpMailActionApplyResult{}, fmt.Errorf("move_to_folder is not supported for this account")
		}
		if folder == "" {
			return mcpMailActionApplyResult{}, fmt.Errorf("folder is required")
		}
		if resolvedProvider, ok := provider.(email.ResolvedNamedFolderProvider); ok {
			resolutions, err := resolvedProvider.MoveToFolderResolved(ctx, messageIDs, folder)
			return mcpMailActionApplyResult{Count: len(resolutions), Resolutions: resolutions}, err
		}
		count, err := folderProvider.MoveToFolder(ctx, messageIDs, folder)
		return mcpMailActionApplyResult{Count: count}, err
	case "apply_label":
		labelProvider, ok := provider.(email.NamedLabelProvider)
		if !ok {
			return mcpMailActionApplyResult{}, fmt.Errorf("apply_label is not supported for this account")
		}
		if label == "" {
			return mcpMailActionApplyResult{}, fmt.Errorf("label is required")
		}
		archiveValue := false
		if archive != nil {
			archiveValue = *archive
		}
		count, err := labelProvider.ApplyNamedLabel(ctx, messageIDs, label, archiveValue)
		return mcpMailActionApplyResult{Count: count}, err
	case "archive_label":
		if label == "" {
			return mcpMailActionApplyResult{}, fmt.Errorf("label is required")
		}
		if folderProvider, ok := provider.(email.NamedFolderProvider); ok {
			target := label
			if account.Provider == store.ExternalProviderExchangeEWS {
				target = "Archive/" + label
			}
			if resolvedProvider, ok := provider.(email.ResolvedNamedFolderProvider); ok {
				resolutions, err := resolvedProvider.MoveToFolderResolved(ctx, messageIDs, target)
				return mcpMailActionApplyResult{Count: len(resolutions), Resolutions: resolutions}, err
			}
			count, err := folderProvider.MoveToFolder(ctx, messageIDs, target)
			return mcpMailActionApplyResult{Count: count}, err
		}
		if labelProvider, ok := provider.(email.NamedLabelProvider); ok {
			count, err := labelProvider.ApplyNamedLabel(ctx, messageIDs, label, true)
			return mcpMailActionApplyResult{Count: count}, err
		}
		if resolvedProvider, ok := provider.(email.ResolvedArchiveProvider); ok {
			resolutions, err := resolvedProvider.ArchiveResolved(ctx, messageIDs)
			return mcpMailActionApplyResult{Count: len(resolutions), Resolutions: resolutions}, err
		}
		count, err := provider.Archive(ctx, messageIDs)
		return mcpMailActionApplyResult{Count: count}, err
	default:
		return mcpMailActionApplyResult{}, fmt.Errorf("unsupported action")
	}
}

func applyMailActionResolutionsStore(st *store.Store, account store.ExternalAccount, action, targetFolder string, resolutions []email.ActionResolution) error {
	if st == nil || len(resolutions) == 0 {
		return nil
	}
	var (
		containerRef *string
		itemState    *string
	)
	if strings.TrimSpace(targetFolder) != "" {
		containerRef = &targetFolder
	}
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "move_to_inbox":
		state := store.ItemStateInbox
		itemState = &state
	case "archive", "archive_label", "trash", "delete", "move_to_folder":
		state := store.ItemStateDone
		itemState = &state
	}
	updates := make([]store.ExternalBindingReconcileUpdate, 0, len(resolutions))
	for _, resolution := range resolutions {
		updates = append(updates, store.ExternalBindingReconcileUpdate{ObjectType: mcpEmailBindingObjectType, OldRemoteID: resolution.OriginalMessageID, NewRemoteID: resolution.ResolvedMessageID, ContainerRef: containerRef, FollowUpItemState: itemState})
	}
	return st.ApplyExternalBindingReconcileUpdates(account.ID, account.Provider, updates)
}

func mcpMailActionTargetFolder(account store.ExternalAccount, action, folder, label string) string {
	switch action {
	case "move_to_inbox":
		if account.Provider == store.ExternalProviderExchangeEWS {
			return "Posteingang"
		}
		return "inbox"
	case "trash":
		if account.Provider == store.ExternalProviderExchangeEWS {
			return "Gelöschte Elemente"
		}
		return "trash"
	case "archive":
		if account.Provider == store.ExternalProviderExchangeEWS {
			return "Archive"
		}
		return "archive"
	case "defer":
		return "snoozed"
	case "move_to_folder":
		return folder
	case "archive_label":
		if account.Provider == store.ExternalProviderExchangeEWS {
			return "Archive/" + label
		}
		return label
	case "apply_label":
		return label
	default:
		return ""
	}
}

func parseMailActionUntil(args map[string]interface{}, action string) (time.Time, string, error) {
	if action != "defer" {
		return time.Time{}, "", nil
	}
	untilAt, untilRaw, err := parseCalendarToolTimeArg(args, "until")
	if err != nil {
		return time.Time{}, untilRaw, err
	}
	return untilAt, untilRaw, nil
}

func mcpMailActionMessageFolder(message *providerdata.EmailMessage) string {
	if message == nil || len(message.Labels) == 0 {
		return ""
	}
	return strings.Join(message.Labels, ",")
}

func mcpMailActionMessageSubject(message *providerdata.EmailMessage) string {
	if message == nil {
		return ""
	}
	return strings.TrimSpace(message.Subject)
}

func mcpMailActionMessageSender(message *providerdata.EmailMessage) string {
	if message == nil {
		return ""
	}
	return strings.TrimSpace(message.Sender)
}

func compactStringList(values []string) []string {
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

type MailSendRequest struct {
	AccountID   int64
	To          []string
	Cc          []string
	Bcc         []string
	Subject     string
	Body        string
	InReplyTo   string
	References  []string
	Attachments []email.DraftAttachment
	DraftOnly   bool
}

type MailReplyRequest struct {
	AccountID   int64
	MessageID   string
	Body        string
	QuoteStyle  email.ReplyQuoteStyle
	ReplyAll    bool
	To          []string
	Cc          []string
	Bcc         []string
	Subject     string
	Attachments []email.DraftAttachment
	DraftOnly   bool
}

type MailComposeResult struct {
	Account  store.ExternalAccount `json:"account"`
	DraftID  string                `json:"draft_id"`
	ThreadID string                `json:"thread_id,omitempty"`
	Sent     bool                  `json:"sent"`
	Reply    *MailComposeReplyInfo `json:"reply,omitempty"`
	Composed MailComposedEnvelope  `json:"composed"`
}

type MailComposeReplyInfo struct {
	MessageID       string `json:"message_id"`
	InReplyTo       string `json:"in_reply_to,omitempty"`
	ThreadID        string `json:"thread_id,omitempty"`
	OriginalSubject string `json:"original_subject,omitempty"`
	QuoteStyle      string `json:"quote_style"`
}

type MailComposedEnvelope struct {
	From            string   `json:"from,omitempty"`
	To              []string `json:"to"`
	Cc              []string `json:"cc,omitempty"`
	Bcc             []string `json:"bcc,omitempty"`
	Subject         string   `json:"subject"`
	AttachmentCount int      `json:"attachment_count"`
	BodyBytes       int      `json:"body_bytes"`
}

func (s *Server) mailSend(args map[string]interface{}) (map[string]interface{}, error) {
	req, err := parseMailSendArgs(args)
	if err != nil {
		return nil, err
	}
	account, provider, err := s.mailDraftProviderForAccount(context.Background(), req.AccountID)
	if err != nil {
		return nil, err
	}
	defer provider.Close()
	result, err := ExecuteMailSend(context.Background(), account, provider, req)
	if err != nil {
		return nil, err
	}
	return mailComposeResultToMap(result), nil
}

func (s *Server) mailDraftSend(args map[string]interface{}) (map[string]interface{}, error) {
	accountID, err := int64Arg(args, "account_id")
	if err != nil {
		return nil, err
	}
	draftID := strings.TrimSpace(strArg(args, "draft_id"))
	if draftID == "" {
		return nil, errors.New("draft_id is required")
	}
	ctx := context.Background()
	account, provider, err := s.ResolveMailAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	defer provider.Close()
	sender, ok := provider.(email.ExistingDraftSender)
	if !ok {
		return nil, fmt.Errorf("account %q (%s) does not support sending an existing draft by id", account.AccountName, account.Provider)
	}
	if err := sender.SendExistingDraft(ctx, draftID); err != nil {
		return nil, err
	}
	return map[string]interface{}{"account": account, "draft_id": draftID, "sent": true}, nil
}

func (s *Server) mailReply(args map[string]interface{}) (map[string]interface{}, error) {
	req, err := parseMailReplyArgs(args)
	if err != nil {
		return nil, err
	}
	account, provider, err := s.mailDraftProviderForAccount(context.Background(), req.AccountID)
	if err != nil {
		return nil, err
	}
	defer provider.Close()
	result, err := ExecuteMailReply(context.Background(), account, provider, req)
	if err != nil {
		return nil, err
	}
	return mailComposeResultToMap(result), nil
}

func (s *Server) mailDraftProviderForAccount(ctx context.Context, accountID int64) (store.ExternalAccount, email.EmailProvider, error) {
	return s.ResolveMailAccount(ctx, accountID)
}

func (s *Server) ResolveMailAccount(ctx context.Context, accountID int64) (store.ExternalAccount, email.EmailProvider, error) {
	st, err := s.requireStore()
	if err != nil {
		return store.ExternalAccount{}, nil, err
	}
	account, err := st.GetExternalAccount(accountID)
	if err != nil {
		return store.ExternalAccount{}, nil, err
	}
	provider, err := s.emailProviderForAccount(ctx, account)
	if err != nil {
		return store.ExternalAccount{}, nil, err
	}
	return account, provider, nil
}

func ExecuteMailSend(ctx context.Context, account store.ExternalAccount, provider email.EmailProvider, req MailSendRequest) (MailComposeResult, error) {
	drafts, ok := provider.(email.DraftProvider)
	if !ok {
		return MailComposeResult{}, fmt.Errorf("account %q (%s) does not support sending mail", account.AccountName, account.Provider)
	}
	input := email.DraftInput{From: account.AccountName, To: req.To, Cc: req.Cc, Bcc: req.Bcc, Subject: req.Subject, Body: req.Body, InReplyTo: req.InReplyTo, References: req.References, Attachments: req.Attachments}
	return finalizeCompose(ctx, account, drafts, input, req.DraftOnly, nil)
}

func ExecuteMailReply(ctx context.Context, account store.ExternalAccount, provider email.EmailProvider, req MailReplyRequest) (MailComposeResult, error) {
	drafts, ok := provider.(email.DraftProvider)
	if !ok {
		return MailComposeResult{}, fmt.Errorf("account %q (%s) does not support sending mail", account.AccountName, account.Provider)
	}
	messageID := strings.TrimSpace(req.MessageID)
	if messageID == "" {
		return MailComposeResult{}, errors.New("message_id is required")
	}
	source, err := provider.GetMessage(ctx, messageID, "full")
	if err != nil {
		return MailComposeResult{}, fmt.Errorf("load source message: %w", err)
	}
	if source == nil {
		return MailComposeResult{}, fmt.Errorf("source message %q not found", messageID)
	}
	style := req.QuoteStyle
	if style == "" {
		style = email.ReplyQuoteBottomPost
	}
	body := email.FormatQuotedReply(style, req.Body, email.QuoteSource{From: source.Sender, Date: source.Date, Body: preferPlainBody(source)})
	to := req.To
	if len(to) == 0 {
		to = []string{source.Sender}
	}
	cc := req.Cc
	if req.ReplyAll {
		cc = append(cc, filterRecipients(source.Recipients, account.AccountName)...)
	}
	subject := strings.TrimSpace(req.Subject)
	if subject == "" {
		subject = source.Subject
	}
	originalMessageID := strings.TrimSpace(source.InternetMessageID)
	references := []string{}
	if originalMessageID != "" {
		references = append(references, originalMessageID)
	}
	input := email.DraftInput{From: account.AccountName, To: to, Cc: cc, Bcc: req.Bcc, Subject: email.EnsureReplySubject(subject), Body: body, ThreadID: strings.TrimSpace(source.ThreadID), InReplyTo: originalMessageID, References: references, Attachments: req.Attachments}
	reply := &MailComposeReplyInfo{MessageID: messageID, InReplyTo: originalMessageID, ThreadID: strings.TrimSpace(source.ThreadID), OriginalSubject: source.Subject, QuoteStyle: string(style)}
	return finalizeCompose(ctx, account, drafts, input, req.DraftOnly, reply)
}

func finalizeCompose(ctx context.Context, account store.ExternalAccount, drafts email.DraftProvider, input email.DraftInput, draftOnly bool, reply *MailComposeReplyInfo) (MailComposeResult, error) {
	draft, err := drafts.CreateDraft(ctx, input)
	if err != nil {
		return MailComposeResult{}, fmt.Errorf("create draft: %w", err)
	}
	sent := false
	if !draftOnly {
		if err := drafts.SendDraft(ctx, draft.ID, input); err != nil {
			return MailComposeResult{}, fmt.Errorf("send draft %s: %w", draft.ID, err)
		}
		sent = true
	}
	normalized, err := email.NormalizeDraftInput(input)
	if err != nil {
		return MailComposeResult{}, err
	}
	return MailComposeResult{Account: account, DraftID: strings.TrimSpace(draft.ID), ThreadID: strings.TrimSpace(draft.ThreadID), Sent: sent, Reply: reply, Composed: MailComposedEnvelope{From: normalized.From, To: normalized.To, Cc: normalized.Cc, Bcc: normalized.Bcc, Subject: normalized.Subject, AttachmentCount: len(normalized.Attachments), BodyBytes: len(normalized.Body)}}, nil
}

func mailComposeResultToMap(result MailComposeResult) map[string]interface{} {
	out := map[string]interface{}{"account": result.Account, "draft_id": result.DraftID, "sent": result.Sent, "composed": result.Composed}
	if result.ThreadID != "" {
		out["thread_id"] = result.ThreadID
	}
	if result.Reply != nil {
		out["reply"] = result.Reply
	}
	return out
}

func parseMailSendArgs(args map[string]interface{}) (MailSendRequest, error) {
	accountID, err := int64Arg(args, "account_id")
	if err != nil {
		return MailSendRequest{}, err
	}
	to := stringListArg(args, "to")
	if len(to) == 0 {
		return MailSendRequest{}, errors.New("to must contain at least one recipient")
	}
	subject := strings.TrimSpace(strArg(args, "subject"))
	if subject == "" {
		return MailSendRequest{}, errors.New("subject is required")
	}
	body := strArg(args, "body")
	if strings.TrimSpace(body) == "" {
		return MailSendRequest{}, errors.New("body is required")
	}
	attachments, err := parseAttachmentsArg(args, "attachments")
	if err != nil {
		return MailSendRequest{}, err
	}
	return MailSendRequest{AccountID: accountID, To: to, Cc: stringListArg(args, "cc"), Bcc: stringListArg(args, "bcc"), Subject: subject, Body: body, InReplyTo: strings.TrimSpace(strArg(args, "in_reply_to")), References: stringListArg(args, "references"), Attachments: attachments, DraftOnly: boolArg(args, "draft_only")}, nil
}

func parseMailReplyArgs(args map[string]interface{}) (MailReplyRequest, error) {
	accountID, err := int64Arg(args, "account_id")
	if err != nil {
		return MailReplyRequest{}, err
	}
	messageID := strings.TrimSpace(strArg(args, "message_id"))
	if messageID == "" {
		return MailReplyRequest{}, errors.New("message_id is required")
	}
	body := strArg(args, "body")
	if strings.TrimSpace(body) == "" {
		return MailReplyRequest{}, errors.New("body is required")
	}
	style, err := email.ParseReplyQuoteStyle(strArg(args, "quote_style"))
	if err != nil {
		return MailReplyRequest{}, err
	}
	attachments, err := parseAttachmentsArg(args, "attachments")
	if err != nil {
		return MailReplyRequest{}, err
	}
	return MailReplyRequest{AccountID: accountID, MessageID: messageID, Body: body, QuoteStyle: style, ReplyAll: boolArg(args, "reply_all"), To: stringListArg(args, "to"), Cc: stringListArg(args, "cc"), Bcc: stringListArg(args, "bcc"), Subject: strings.TrimSpace(strArg(args, "subject")), Attachments: attachments, DraftOnly: boolArg(args, "draft_only")}, nil
}
