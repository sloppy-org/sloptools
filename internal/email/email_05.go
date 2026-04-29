package email

import (
	"context"
	"fmt"
	"github.com/sloppy-org/sloptools/internal/ews"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"net/mail"
	"strings"
	"time"
)

func (p *ExchangeEWSMailProvider) GetMessages(ctx context.Context, messageIDs []string, format string) ([]*providerdata.EmailMessage, error) {
	requested := compactMessageIDs(messageIDs)
	messages, err := p.getMessagesWithFormat(ctx, messageIDs, format)
	if err != nil {
		if !exchangeEWSMissingItemError(err) {
			return nil, err
		}
		messages = make([]ews.Message, 0, len(requested))
		for _, messageID := range requested {
			single, singleErr := p.getMessagesWithFormat(ctx, []string{messageID}, format)
			if singleErr != nil {
				if exchangeEWSMissingItemError(singleErr) {
					continue
				}
				return nil, singleErr
			}
			messages = append(messages, single...)
		}
	}
	folders, err := p.client.ListFolders(ctx)
	if err != nil {
		return nil, err
	}
	folderIndex := exchangeEWSFolderIndex(folders)
	decodedByID := make(map[string]*providerdata.EmailMessage, len(messages))
	for _, message := range messages {
		decoded := decodeExchangeEWSMessage(message, folderIndex, format)
		id := strings.TrimSpace(decoded.ID)
		if id == "" {
			continue
		}
		decodedCopy := decoded
		decodedByID[id] = &decodedCopy
	}
	out := make([]*providerdata.EmailMessage, 0, len(requested))
	for _, messageID := range requested {
		if decoded, ok := decodedByID[strings.TrimSpace(messageID)]; ok {
			out = append(out, decoded)
		}
	}
	return out, nil
}

func (p *ExchangeEWSMailProvider) getMessagesWithFormat(ctx context.Context, messageIDs []string, format string) ([]ews.Message, error) {
	if strings.EqualFold(strings.TrimSpace(format), "metadata") {
		return p.client.GetMessageSummaries(ctx, messageIDs)
	}
	return p.client.GetMessages(ctx, messageIDs)
}

func exchangeEWSMissingItemError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "erroritemnotfound")
}

func (p *ExchangeEWSMailProvider) MarkRead(ctx context.Context, messageIDs []string) (int, error) {
	ids := compactMessageIDs(messageIDs)
	if err := p.client.SetReadState(ctx, ids, true); err != nil {
		return 0, err
	}
	return len(ids), nil
}

func (p *ExchangeEWSMailProvider) MarkUnread(ctx context.Context, messageIDs []string) (int, error) {
	ids := compactMessageIDs(messageIDs)
	if err := p.client.SetReadState(ctx, ids, false); err != nil {
		return 0, err
	}
	return len(ids), nil
}

func (p *ExchangeEWSMailProvider) Archive(ctx context.Context, messageIDs []string) (int, error) {
	resolutions, err := p.ArchiveResolved(ctx, messageIDs)
	if err != nil {
		return 0, err
	}
	return len(resolutions), nil
}

func (p *ExchangeEWSMailProvider) ArchiveResolved(ctx context.Context, messageIDs []string) ([]ActionResolution, error) {
	ids := compactMessageIDs(messageIDs)
	folderID, err := p.resolveArchiveFolderID(ctx)
	if err != nil {
		return nil, err
	}
	if folderID == "" {
		return nil, fmt.Errorf("exchange ews archive folder is not configured")
	}
	resolved, err := p.client.MoveItems(ctx, ids, folderID)
	if err != nil {
		return nil, err
	}
	return actionResolutions(ids, resolved), nil
}

func (p *ExchangeEWSMailProvider) MoveToInbox(ctx context.Context, messageIDs []string) (int, error) {
	resolutions, err := p.MoveToInboxResolved(ctx, messageIDs)
	if err != nil {
		return 0, err
	}
	return len(resolutions), nil
}

func (p *ExchangeEWSMailProvider) MoveToInboxResolved(ctx context.Context, messageIDs []string) ([]ActionResolution, error) {
	ids := compactMessageIDs(messageIDs)
	resolved, err := p.client.MoveItems(ctx, ids, "inbox")
	if err != nil {
		return nil, err
	}
	return actionResolutions(ids, resolved), nil
}

func (p *ExchangeEWSMailProvider) Trash(ctx context.Context, messageIDs []string) (int, error) {
	resolutions, err := p.TrashResolved(ctx, messageIDs)
	if err != nil {
		return 0, err
	}
	return len(resolutions), nil
}

func (p *ExchangeEWSMailProvider) TrashResolved(ctx context.Context, messageIDs []string) ([]ActionResolution, error) {
	ids := compactMessageIDs(messageIDs)
	resolved, err := p.client.MoveItems(ctx, ids, "deleteditems")
	if err != nil {
		return nil, err
	}
	return actionResolutions(ids, resolved), nil
}

func (p *ExchangeEWSMailProvider) MoveToFolder(ctx context.Context, messageIDs []string, folder string) (int, error) {
	resolutions, err := p.MoveToFolderResolved(ctx, messageIDs, folder)
	if err != nil {
		return 0, err
	}
	return len(resolutions), nil
}

func (p *ExchangeEWSMailProvider) MoveToFolderResolved(ctx context.Context, messageIDs []string, folder string) ([]ActionResolution, error) {
	ids := compactMessageIDs(messageIDs)
	if len(ids) == 0 {
		return nil, nil
	}
	folderID, err := p.resolveFolderRef(ctx, folder)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(folderID) == "" {
		return nil, fmt.Errorf("exchange ews folder %q not found", strings.TrimSpace(folder))
	}
	resolved, err := p.client.MoveItems(ctx, ids, folderID)
	if err != nil {
		return nil, err
	}
	return actionResolutions(ids, resolved), nil
}

func (p *ExchangeEWSMailProvider) ServerFilterCapabilities() ServerFilterCapabilities {
	return ServerFilterCapabilities{Provider: p.ProviderName(), SupportsList: true, SupportsUpsert: true, SupportsDelete: true, SupportsArchive: true, SupportsTrash: true, SupportsMoveTo: true, SupportsMarkRead: true, SupportsForward: true}
}

func (p *ExchangeEWSMailProvider) ListServerFilters(ctx context.Context) ([]ServerFilter, error) {
	rules, err := p.client.GetInboxRules(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ServerFilter, 0, len(rules))
	for _, rule := range rules {
		out = append(out, p.ruleToServerFilter(ctx, rule))
	}
	return out, nil
}

func (p *ExchangeEWSMailProvider) UpsertServerFilter(ctx context.Context, filter ServerFilter) (ServerFilter, error) {
	op := ews.RuleOperationCreate
	if strings.TrimSpace(filter.ID) != "" {
		op = ews.RuleOperationSet
	}
	rule, err := p.serverFilterToRule(ctx, filter)
	if err != nil {
		return ServerFilter{}, err
	}
	if err := p.client.UpdateInboxRules(ctx, []ews.RuleOperation{{Kind: op, Rule: rule}}); err != nil {
		return ServerFilter{}, err
	}
	if op == ews.RuleOperationSet {
		return p.ruleToServerFilter(ctx, rule), nil
	}
	rules, err := p.client.GetInboxRules(ctx)
	if err != nil {
		return ServerFilter{}, err
	}
	for _, existing := range rules {
		if strings.EqualFold(strings.TrimSpace(existing.Name), strings.TrimSpace(filter.Name)) {
			return p.ruleToServerFilter(ctx, existing), nil
		}
	}
	return p.ruleToServerFilter(ctx, rule), nil
}

func (p *ExchangeEWSMailProvider) DeleteServerFilter(ctx context.Context, id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("exchange ews rule id is required")
	}
	return p.client.UpdateInboxRules(ctx, []ews.RuleOperation{{Kind: ews.RuleOperationDelete, Rule: ews.Rule{ID: strings.TrimSpace(id)}}})
}

func (p *ExchangeEWSMailProvider) Delete(ctx context.Context, messageIDs []string) (int, error) {
	ids := compactMessageIDs(messageIDs)
	if err := p.client.DeleteItems(ctx, ids, true); err != nil {
		return 0, err
	}
	return len(ids), nil
}

func (p *ExchangeEWSMailProvider) ProviderName() string {
	return "exchange_ews"
}

func (p *ExchangeEWSMailProvider) CreateDraft(ctx context.Context, input DraftInput) (Draft, error) {
	if p == nil || p.client == nil {
		return Draft{}, fmt.Errorf("exchange ews provider is not configured")
	}
	normalized, err := p.normalizeDraftInput(input, false)
	if err != nil {
		return Draft{}, err
	}
	attachments := normalized.Attachments
	bodyOnly := normalized
	bodyOnly.Attachments = nil
	raw, err := buildRFC822Message(bodyOnly)
	if err != nil {
		return Draft{}, err
	}
	created, err := p.client.CreateDraft(ctx, ews.DraftMessage{Subject: normalized.Subject, MIME: raw, ThreadID: normalized.ThreadID, InReplyTo: normalized.InReplyTo, References: normalized.References})
	if err != nil {
		return Draft{}, fmt.Errorf("exchange ews create draft: %w", err)
	}
	draftID := strings.TrimSpace(created.ID)
	changeKey := strings.TrimSpace(created.ChangeKey)
	for _, att := range attachments {
		updatedKey, err := p.client.CreateAttachment(ctx, draftID, changeKey, ews.AttachmentFile{Name: att.Filename, ContentType: att.ContentType, Content: att.Content})
		if err != nil {
			return Draft{}, fmt.Errorf("exchange ews add attachment %q: %w", att.Filename, err)
		}
		if updatedKey != "" {
			changeKey = updatedKey
		}
	}
	return Draft{ID: draftID, ThreadID: strings.TrimSpace(created.ConversationID)}, nil
}

func (p *ExchangeEWSMailProvider) CreateReplyDraft(ctx context.Context, messageID string, input DraftInput) (Draft, error) {
	if p == nil || p.client == nil {
		return Draft{}, fmt.Errorf("exchange ews provider is not configured")
	}
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return Draft{}, fmt.Errorf("exchange ews message id is required")
	}
	message, err := p.client.GetMessages(ctx, []string{messageID})
	if err != nil {
		return Draft{}, fmt.Errorf("exchange ews get reply message: %w", err)
	}
	if len(message) == 0 {
		return Draft{}, fmt.Errorf("exchange ews message %q not found", messageID)
	}
	seed := input
	remote := message[0]
	if len(seed.To) == 0 {
		addr := strings.TrimSpace(remote.From.Email)
		if addr != "" {
			if strings.TrimSpace(remote.From.Name) != "" {
				addr = (&mail.Address{Name: strings.TrimSpace(remote.From.Name), Address: addr}).String()
			}
			seed.To = []string{addr}
		}
	}
	if strings.TrimSpace(seed.Subject) == "" {
		seed.Subject = ensureReplySubject(remote.Subject)
	} else {
		seed.Subject = ensureReplySubject(seed.Subject)
	}
	if strings.TrimSpace(seed.ThreadID) == "" {
		seed.ThreadID = strings.TrimSpace(remote.ConversationID)
	}
	if strings.TrimSpace(seed.InReplyTo) == "" {
		seed.InReplyTo = strings.TrimSpace(remote.InternetMessageID)
	}
	if len(seed.References) == 0 && strings.TrimSpace(remote.InternetMessageID) != "" {
		seed.References = []string{strings.TrimSpace(remote.InternetMessageID)}
	}
	return p.CreateDraft(ctx, seed)
}

func (p *ExchangeEWSMailProvider) UpdateDraft(ctx context.Context, draftID string, input DraftInput) (Draft, error) {
	if p == nil || p.client == nil {
		return Draft{}, fmt.Errorf("exchange ews provider is not configured")
	}
	normalized, err := p.normalizeDraftInput(input, false)
	if err != nil {
		return Draft{}, err
	}
	raw, err := buildRFC822Message(normalized)
	if err != nil {
		return Draft{}, err
	}
	updated, err := p.client.UpdateDraft(ctx, strings.TrimSpace(draftID), ews.DraftMessage{Subject: normalized.Subject, MIME: raw, ThreadID: normalized.ThreadID, InReplyTo: normalized.InReplyTo, References: normalized.References})
	if err != nil {
		return Draft{}, fmt.Errorf("exchange ews update draft: %w", err)
	}
	return Draft{ID: strings.TrimSpace(updated.ID), ThreadID: strings.TrimSpace(updated.ConversationID)}, nil
}

func (p *ExchangeEWSMailProvider) SendDraft(ctx context.Context, draftID string, input DraftInput) error {
	if p == nil || p.client == nil {
		return fmt.Errorf("exchange ews provider is not configured")
	}
	normalized, err := NormalizeDraftSendInput(input)
	if err != nil {
		return err
	}
	if strings.TrimSpace(draftID) == "" {
		created, err := p.CreateDraft(ctx, normalized)
		if err != nil {
			return err
		}
		draftID = created.ID
	}
	if _, err := p.UpdateDraft(ctx, strings.TrimSpace(draftID), normalized); err != nil {
		return err
	}
	if err := p.client.SendDraft(ctx, strings.TrimSpace(draftID)); err != nil {
		return fmt.Errorf("exchange ews send draft: %w", err)
	}
	return nil
}

func (p *ExchangeEWSMailProvider) SendExistingDraft(ctx context.Context, draftID string) error {
	if p == nil || p.client == nil {
		return fmt.Errorf("exchange ews provider is not configured")
	}
	draftID = strings.TrimSpace(draftID)
	if draftID == "" {
		return fmt.Errorf("draft_id is required")
	}
	if err := p.client.SendDraft(ctx, draftID); err != nil {
		return fmt.Errorf("exchange ews send draft: %w", err)
	}
	return nil
}

func (p *ExchangeEWSMailProvider) SetFlag(ctx context.Context, messageIDs []string, flag Flag) (int, error) {
	if p == nil || p.client == nil {
		return 0, fmt.Errorf("exchange ews provider is not configured")
	}
	status, err := exchangeEWSFlagStatus(flag.Status)
	if err != nil {
		return 0, err
	}
	ids := compactMessageIDs(messageIDs)
	dueAt := time.Time{}
	if flag.DueAt != nil {
		dueAt = *flag.DueAt
	}
	if err := p.client.SetFlag(ctx, ids, status, dueAt); err != nil {
		return 0, err
	}
	return len(ids), nil
}

func (p *ExchangeEWSMailProvider) ClearFlag(ctx context.Context, messageIDs []string) (int, error) {
	if p == nil || p.client == nil {
		return 0, fmt.Errorf("exchange ews provider is not configured")
	}
	ids := compactMessageIDs(messageIDs)
	if err := p.client.SetFlag(ctx, ids, ews.FlagStatusNotFlagged, time.Time{}); err != nil {
		return 0, err
	}
	return len(ids), nil
}

func (p *ExchangeEWSMailProvider) SetCategories(ctx context.Context, messageIDs []string, categories []string) (int, error) {
	if p == nil || p.client == nil {
		return 0, fmt.Errorf("exchange ews provider is not configured")
	}
	ids := compactMessageIDs(messageIDs)
	if err := p.client.SetCategories(ctx, ids, categories); err != nil {
		return 0, err
	}
	return len(ids), nil
}

func exchangeEWSFlagStatus(status string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case strings.ToLower(FlagStatusNotFlagged):
		return ews.FlagStatusNotFlagged, nil
	case strings.ToLower(FlagStatusFlagged):
		return ews.FlagStatusFlagged, nil
	case strings.ToLower(FlagStatusComplete):
		return ews.FlagStatusComplete, nil
	}
	return "", fmt.Errorf("exchange ews flag status %q is not recognised", status)
}

func (p *ExchangeEWSMailProvider) Close() error {
	if p == nil || p.client == nil {
		return nil
	}
	return p.client.Close()
}

func (p *ExchangeEWSMailProvider) searchFolders(ctx context.Context, opts SearchOptions) ([]string, error) {
	if strings.TrimSpace(opts.Folder) != "" {
		ref, err := p.resolveFolderRef(ctx, strings.TrimSpace(opts.Folder))
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(ref) == "" {
			return nil, fmt.Errorf("exchange ews folder %q not found", strings.TrimSpace(opts.Folder))
		}
		return []string{ref}, nil
	}
	folders, err := p.client.ListFolders(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(folders))
	for _, folder := range folders {
		if folder.Kind == ews.FolderKindCalendar || folder.Kind == ews.FolderKindContacts || folder.Kind == ews.FolderKindTasks {
			continue
		}
		name := strings.TrimSpace(folder.Name)
		if name == "" {
			continue
		}
		if !opts.IncludeSpamTrash && exchangeEWSSpamOrTrash(name) {
			continue
		}
		if strings.TrimSpace(folder.ID) != "" {
			out = append(out, strings.TrimSpace(folder.ID))
		}
	}
	if len(out) == 0 {
		out = append(out, "inbox")
	}
	return out, nil
}
