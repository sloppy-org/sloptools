package mcp

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/mail"
	"os"
	"path/filepath"
	"strings"

	"github.com/sloppy-org/sloptools/internal/email"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/store"
)

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
	return map[string]interface{}{
		"account":  account,
		"draft_id": draftID,
		"sent":     true,
	}, nil
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
	input := email.DraftInput{
		From:        account.AccountName,
		To:          req.To,
		Cc:          req.Cc,
		Bcc:         req.Bcc,
		Subject:     req.Subject,
		Body:        req.Body,
		InReplyTo:   req.InReplyTo,
		References:  req.References,
		Attachments: req.Attachments,
	}
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
	body := email.FormatQuotedReply(style, req.Body, email.QuoteSource{
		From: source.Sender,
		Date: source.Date,
		Body: preferPlainBody(source),
	})
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
	input := email.DraftInput{
		From:        account.AccountName,
		To:          to,
		Cc:          cc,
		Bcc:         req.Bcc,
		Subject:     email.EnsureReplySubject(subject),
		Body:        body,
		ThreadID:    strings.TrimSpace(source.ThreadID),
		InReplyTo:   originalMessageID,
		References:  references,
		Attachments: req.Attachments,
	}
	reply := &MailComposeReplyInfo{
		MessageID:       messageID,
		InReplyTo:       originalMessageID,
		ThreadID:        strings.TrimSpace(source.ThreadID),
		OriginalSubject: source.Subject,
		QuoteStyle:      string(style),
	}
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
	return MailComposeResult{
		Account:  account,
		DraftID:  strings.TrimSpace(draft.ID),
		ThreadID: strings.TrimSpace(draft.ThreadID),
		Sent:     sent,
		Reply:    reply,
		Composed: MailComposedEnvelope{
			From:            normalized.From,
			To:              normalized.To,
			Cc:              normalized.Cc,
			Bcc:             normalized.Bcc,
			Subject:         normalized.Subject,
			AttachmentCount: len(normalized.Attachments),
			BodyBytes:       len(normalized.Body),
		},
	}, nil
}

func mailComposeResultToMap(result MailComposeResult) map[string]interface{} {
	out := map[string]interface{}{
		"account":  result.Account,
		"draft_id": result.DraftID,
		"sent":     result.Sent,
		"composed": result.Composed,
	}
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
	return MailSendRequest{
		AccountID:   accountID,
		To:          to,
		Cc:          stringListArg(args, "cc"),
		Bcc:         stringListArg(args, "bcc"),
		Subject:     subject,
		Body:        body,
		InReplyTo:   strings.TrimSpace(strArg(args, "in_reply_to")),
		References:  stringListArg(args, "references"),
		Attachments: attachments,
		DraftOnly:   boolArg(args, "draft_only"),
	}, nil
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
	return MailReplyRequest{
		AccountID:   accountID,
		MessageID:   messageID,
		Body:        body,
		QuoteStyle:  style,
		ReplyAll:    boolArg(args, "reply_all"),
		To:          stringListArg(args, "to"),
		Cc:          stringListArg(args, "cc"),
		Bcc:         stringListArg(args, "bcc"),
		Subject:     strings.TrimSpace(strArg(args, "subject")),
		Attachments: attachments,
		DraftOnly:   boolArg(args, "draft_only"),
	}, nil
}

func parseAttachmentsArg(args map[string]interface{}, key string) ([]email.DraftAttachment, error) {
	v, ok := args[key]
	if !ok || v == nil {
		return nil, nil
	}
	list, ok := v.([]interface{})
	if !ok {
		return nil, fmt.Errorf("%s must be an array", key)
	}
	out := make([]email.DraftAttachment, 0, len(list))
	for i, raw := range list {
		item, ok := raw.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("%s[%d] must be an object", key, i)
		}
		att, err := parseAttachmentItem(item)
		if err != nil {
			return nil, fmt.Errorf("%s[%d]: %w", key, i, err)
		}
		out = append(out, att)
	}
	return out, nil
}

func parseAttachmentItem(item map[string]interface{}) (email.DraftAttachment, error) {
	filename := strings.TrimSpace(firstStringField(item, "filename", "name"))
	contentType := strings.TrimSpace(firstStringField(item, "content_type", "mime_type"))
	encoded := strings.TrimSpace(firstStringField(item, "content_base64", "content"))
	path := strings.TrimSpace(firstStringField(item, "path", "file"))
	if encoded == "" && path == "" {
		return email.DraftAttachment{}, errors.New("attachment requires either content_base64 or path")
	}
	var content []byte
	if encoded != "" {
		data, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return email.DraftAttachment{}, fmt.Errorf("decode content_base64: %w", err)
		}
		content = data
	} else {
		abs, err := filepath.Abs(path)
		if err != nil {
			return email.DraftAttachment{}, fmt.Errorf("resolve path: %w", err)
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			return email.DraftAttachment{}, fmt.Errorf("read %s: %w", abs, err)
		}
		content = data
		if filename == "" {
			filename = filepath.Base(abs)
		}
	}
	if filename == "" {
		return email.DraftAttachment{}, errors.New("attachment filename is required")
	}
	return email.DraftAttachment{
		Filename:    filename,
		ContentType: contentType,
		Content:     content,
	}, nil
}

func firstStringField(item map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		raw, ok := item[key]
		if !ok {
			continue
		}
		if str, ok := raw.(string); ok && str != "" {
			return str
		}
	}
	return ""
}

func preferPlainBody(message *providerdata.EmailMessage) string {
	if message == nil {
		return ""
	}
	if message.BodyText != nil && strings.TrimSpace(*message.BodyText) != "" {
		return *message.BodyText
	}
	if message.BodyHTML != nil {
		return htmlToPlain(*message.BodyHTML)
	}
	return message.Snippet
}

func htmlToPlain(html string) string {
	var b strings.Builder
	inTag := false
	for _, r := range html {
		switch {
		case r == '<':
			inTag = true
		case r == '>' && inTag:
			inTag = false
			b.WriteRune(' ')
		case !inTag:
			b.WriteRune(r)
		}
	}
	lines := strings.Split(b.String(), "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " \t")
	}
	return strings.Join(lines, "\n")
}

func filterRecipients(recipients []string, self string) []string {
	selfAddr := strings.ToLower(strings.TrimSpace(self))
	if parsed, err := mail.ParseAddress(selfAddr); err == nil {
		selfAddr = strings.ToLower(strings.TrimSpace(parsed.Address))
	}
	out := make([]string, 0, len(recipients))
	for _, raw := range recipients {
		addr := strings.TrimSpace(raw)
		if addr == "" {
			continue
		}
		if parsed, err := mail.ParseAddress(addr); err == nil {
			if strings.EqualFold(strings.TrimSpace(parsed.Address), selfAddr) {
				continue
			}
		} else if strings.EqualFold(addr, selfAddr) {
			continue
		}
		out = append(out, addr)
	}
	return out
}
