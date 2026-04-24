package email

import (
	"context"
	"encoding/json"
	"github.com/sloppy-org/sloptools/internal/ews"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"regexp"
	"sort"
	"strings"
)

func (p *ExchangeEWSMailProvider) resolveFolderRef(ctx context.Context, folder string) (string, error) {
	clean := strings.TrimSpace(folder)
	if clean == "" {
		return "inbox", nil
	}
	switch strings.ToLower(clean) {
	case "inbox", "posteingang":
		return "inbox", nil
	case "drafts", "entwürfe", "entwuerfe":
		return "drafts", nil
	case "sent", "sentitems", "gesendete elemente":
		return "sentitems", nil
	case "trash", "deleteditems", "gelöschte elemente", "geloschte elemente":
		return "deleteditems", nil
	case "junk", "spam", "junkemail", "junk-e-mail":
		return "junkemail", nil
	case "archive":
		return p.resolveArchiveFolderID(ctx)
	}
	folderInfo, err := p.client.FindFolderByName(ctx, clean)
	if err != nil {
		return "", err
	}
	if folderInfo != nil && strings.TrimSpace(folderInfo.ID) != "" {
		return strings.TrimSpace(folderInfo.ID), nil
	}
	if idx := strings.LastIndex(clean, "/"); idx >= 0 && idx < len(clean)-1 {
		last := strings.TrimSpace(clean[idx+1:])
		folderInfo, err = p.client.FindFolderByName(ctx, last)
		if err != nil {
			return "", err
		}
		if folderInfo != nil && strings.TrimSpace(folderInfo.ID) != "" {
			return strings.TrimSpace(folderInfo.ID), nil
		}
	}
	return clean, nil
}

func (p *ExchangeEWSMailProvider) normalizeDraftInput(input DraftInput, requireRecipients bool) (DraftInput, error) {
	reply := input
	if strings.TrimSpace(reply.From) == "" {
		reply.From = strings.TrimSpace(p.cfg.Username)
	}
	if requireRecipients {
		return NormalizeDraftSendInput(reply)
	}
	return NormalizeDraftInput(reply)
}

func (p *ExchangeEWSMailProvider) resolveArchiveFolderID(ctx context.Context) (string, error) {
	folder, err := p.client.FindFolderByName(ctx, p.cfg.ArchiveFolder)
	if err != nil || folder == nil {
		return "", err
	}
	return folder.ID, nil
}

func compactMessageIDs(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		clean := strings.TrimSpace(value)
		if clean == "" {
			continue
		}
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		out = append(out, clean)
	}
	return out
}

func (p *ExchangeEWSMailProvider) ruleToServerFilter(ctx context.Context, rule ews.Rule) ServerFilter {
	moveTarget := strings.TrimSpace(rule.Actions.MoveToFolderID)
	if clean := p.cfg.ArchiveFolder; strings.EqualFold(moveTarget, "archive") && clean != "" {
		moveTarget = clean
	}
	out := ServerFilter{ID: strings.TrimSpace(rule.ID), Name: strings.TrimSpace(rule.Name), Enabled: rule.Enabled, Criteria: ServerFilterCriteria{Subject: strings.Join(rule.Conditions.ContainsSubjectStrings, " ")}, Action: ServerFilterAction{Trash: rule.Actions.Delete, MarkRead: rule.Actions.MarkAsRead, ForwardTo: mailboxesToStrings(rule.Actions.RedirectToRecipients), MoveTo: moveTarget}}
	if len(rule.Conditions.ContainsSenderStrings) > 0 {
		out.Criteria.From = strings.Join(rule.Conditions.ContainsSenderStrings, " ")
	} else if len(rule.Conditions.FromAddresses) > 0 {
		out.Criteria.From = mailboxString(rule.Conditions.FromAddresses[0])
	}
	if len(rule.Conditions.SentToAddresses) > 0 {
		out.Criteria.To = mailboxString(rule.Conditions.SentToAddresses[0])
	}
	if target := strings.TrimSpace(out.Action.MoveTo); target != "" {
		if p.client != nil {
			target = p.folderDisplayName(ctx, target)
			out.Action.MoveTo = target
		}
		if strings.EqualFold(target, exchangeEWSDisplayFolderName(p.cfg.ArchiveFolder)) || strings.EqualFold(target, p.cfg.ArchiveFolder) {
			out.Action.Archive = true
		}
	}
	return out
}

func (p *ExchangeEWSMailProvider) serverFilterToRule(ctx context.Context, filter ServerFilter) (ews.Rule, error) {
	rule := ews.Rule{ID: strings.TrimSpace(filter.ID), Name: strings.TrimSpace(filter.Name), Enabled: filter.Enabled, Priority: 1, Conditions: ews.RuleConditions{ContainsSubjectStrings: compactMessageIDs([]string{filter.Criteria.Subject}), ContainsSenderStrings: compactMessageIDs([]string{filter.Criteria.From})}, Actions: ews.RuleActions{Delete: filter.Action.Trash, MarkAsRead: filter.Action.MarkRead, RedirectToRecipients: stringsToMailboxes(filter.Action.ForwardTo)}}
	if to := strings.TrimSpace(filter.Criteria.To); to != "" {
		rule.Conditions.SentToAddresses = []ews.Mailbox{{Email: to}}
	}
	moveTo := strings.TrimSpace(filter.Action.MoveTo)
	if filter.Action.Archive && moveTo == "" {
		moveTo = p.cfg.ArchiveFolder
	}
	if moveTo != "" {
		folderID, err := p.resolveFolderRef(ctx, moveTo)
		if err != nil {
			return ews.Rule{}, err
		}
		rule.Actions.MoveToFolderID = folderID
	}
	return rule, nil
}

func (p *ExchangeEWSMailProvider) folderDisplayName(ctx context.Context, folderID string) string {
	clean := strings.TrimSpace(folderID)
	if clean == "" {
		return ""
	}
	folders, err := p.client.ListFolders(ctx)
	if err != nil {
		return clean
	}
	for _, folder := range folders {
		if strings.EqualFold(strings.TrimSpace(folder.ID), clean) {
			return exchangeEWSDisplayFolderName(folder.Name)
		}
	}
	switch strings.ToLower(clean) {
	case "inbox":
		return "Posteingang"
	case "deleteditems":
		return "Gelöschte Elemente"
	case "junkemail":
		return "Junk-E-Mail"
	case "sentitems":
		return "Gesendete Elemente"
	default:
		return clean
	}
}

func mailboxString(mailbox ews.Mailbox) string {
	if clean := strings.TrimSpace(mailbox.Email); clean != "" {
		return clean
	}
	return strings.TrimSpace(mailbox.Name)
}

func mailboxesToStrings(values []ews.Mailbox) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if clean := mailboxString(value); clean != "" {
			out = append(out, clean)
		}
	}
	return out
}

func stringsToMailboxes(values []string) []ews.Mailbox {
	out := make([]ews.Mailbox, 0, len(values))
	for _, value := range values {
		clean := strings.TrimSpace(value)
		if clean == "" {
			continue
		}
		out = append(out, ews.Mailbox{Email: clean})
	}
	return out
}

func sortedMessageIDs(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func exchangeEWSSpamOrTrash(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "junk-e-mail", "junk email", "spam", "geloschte elemente", "gelöschte elemente", "deleted items":
		return true
	default:
		return false
	}
}

func actionResolutions(originalIDs, resolvedIDs []string) []ActionResolution {
	ids := compactMessageIDs(originalIDs)
	if len(ids) == 0 {
		return nil
	}
	out := make([]ActionResolution, 0, len(ids))
	for index, original := range ids {
		resolved := original
		if index < len(resolvedIDs) {
			if clean := strings.TrimSpace(resolvedIDs[index]); clean != "" {
				resolved = clean
			}
		}
		out = append(out, ActionResolution{OriginalMessageID: original, ResolvedMessageID: resolved})
	}
	return out
}

func exchangeEWSDisplayFolderName(name string) string {
	clean := strings.TrimSpace(name)
	if clean == "" {
		return ""
	}
	if strings.EqualFold(clean, "Archive") {
		return ""
	}
	clean = strings.TrimPrefix(clean, "Archive/")
	clean = strings.TrimPrefix(clean, "Archive\\")
	if idx := strings.LastIndexAny(clean, `/\`); idx >= 0 {
		clean = clean[idx+1:]
	}
	return strings.TrimSpace(clean)
}

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

func exchangeEWSFolderIndex(folders []ews.Folder) map[string]ews.Folder {
	out := make(map[string]ews.Folder, len(folders))
	for _, folder := range folders {
		if id := strings.TrimSpace(folder.ID); id != "" {
			out[id] = folder
		}
	}
	return out
}

func exchangeEWSFolderLabels(parentFolderID string, folders map[string]ews.Folder) []string {
	folderID := strings.TrimSpace(parentFolderID)
	if folderID == "" || len(folders) == 0 {
		return nil
	}
	folder, ok := folders[folderID]
	if !ok {
		return nil
	}
	display := exchangeEWSMessageFolderName(folder.Name)
	if display == "" {
		return nil
	}
	labels := []string{display}
	if exchangeEWSInboxFolderName(folder.Name) && !containsFold(strings.Join(labels, "\n"), "INBOX") {
		labels = append(labels, "INBOX")
	}
	return labels
}

func exchangeEWSInboxFolderName(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "inbox", "posteingang":
		return true
	default:
		return false
	}
}

func exchangeEWSMessageFolderName(name string) string {
	clean := strings.TrimSpace(name)
	if clean == "" {
		return ""
	}
	if strings.EqualFold(clean, "Archive") {
		return "Archive"
	}
	if display := exchangeEWSDisplayFolderName(clean); display != "" {
		return display
	}
	return clean
}

func decodeExchangeEWSMessage(message ews.Message, folders map[string]ews.Folder, format string) providerdata.EmailMessage {
	recipients := make([]string, 0, len(message.To)+len(message.Cc))
	for _, group := range [][]ews.Mailbox{message.To, message.Cc} {
		for _, mb := range group {
			formatted := strings.TrimSpace(mb.Name)
			if strings.TrimSpace(mb.Email) != "" {
				if formatted != "" {
					formatted += " <" + strings.TrimSpace(mb.Email) + ">"
				} else {
					formatted = strings.TrimSpace(mb.Email)
				}
			}
			if formatted != "" {
				recipients = append(recipients, formatted)
			}
		}
	}
	attachments := make([]providerdata.Attachment, 0, len(message.Attachments))
	for _, attachment := range message.Attachments {
		attachments = append(attachments, providerdata.Attachment{ID: attachment.ID, Filename: attachment.Name, MimeType: attachment.ContentType, Size: attachment.Size, IsInline: attachment.IsInline})
	}
	sender := strings.TrimSpace(message.From.Name)
	if strings.TrimSpace(message.From.Email) != "" {
		if sender != "" {
			sender += " <" + strings.TrimSpace(message.From.Email) + ">"
		} else {
			sender = strings.TrimSpace(message.From.Email)
		}
	}
	body := strings.TrimSpace(message.Body)
	var bodyPtr *string
	if body != "" && !strings.EqualFold(strings.TrimSpace(format), "metadata") {
		bodyPtr = &body
	}
	return providerdata.EmailMessage{ID: strings.TrimSpace(message.ID), ThreadID: strings.TrimSpace(message.ConversationID), InternetMessageID: strings.TrimSpace(message.InternetMessageID), Subject: strings.TrimSpace(message.Subject), Sender: sender, Recipients: recipients, Date: message.ReceivedAt, Snippet: snippetFromBody(message.Body), Labels: exchangeEWSFolderLabels(message.ParentFolderID, folders), IsRead: message.IsRead, IsFlagged: strings.EqualFold(strings.TrimSpace(message.FlagStatus), "Flagged"), BodyText: bodyPtr, Attachments: attachments}
}

func snippetFromBody(body string) string {
	clean := strings.Join(strings.Fields(strings.TrimSpace(body)), " ")
	if len(clean) <= 280 {
		return clean
	}
	return clean[:280]
}

func (p *ExchangeEWSMailProvider) ExportRawMessage(ctx context.Context, messageID string) ([]byte, error) {
	return p.client.GetMessageMIME(ctx, messageID)
}

func (p *ExchangeEWSMailProvider) ImportRawMessage(ctx context.Context, mimeContent []byte, folder string) (string, error) {
	folderRef, err := p.resolveFolderRef(ctx, folder)
	if err != nil {
		return "", err
	}
	msg, err := p.client.CreateMessageInFolder(ctx, folderRef, mimeContent, true)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(msg.ID), nil
}

func MarshalExchangeEWSConfig(cfg ExchangeEWSConfig) (map[string]any, error) {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	delete(out, "Password")
	delete(out, "password")
	return out, nil
}

var (
	_                      EmailProvider       = (*ExchangeMailProvider)(nil)
	_                      MessagePageProvider = (*ExchangeMailProvider)(nil)
	exchangeHTMLTagPattern                     = regexp.MustCompile(`<[^>]+>`)
)

type ExchangeMailProvider struct{ client *ExchangeClient }

func NewExchangeMailProvider(cfg ExchangeConfig, opts ...ExchangeOption) (*ExchangeMailProvider, error) {
	client, err := NewExchangeClient(cfg, opts...)
	if err != nil {
		return nil, err
	}
	return &ExchangeMailProvider{client: client}, nil
}
