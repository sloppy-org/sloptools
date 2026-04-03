package email

import (
	"context"
	"encoding/json"
	"fmt"
	"net/mail"
	"sort"
	"strconv"
	"strings"

	"github.com/krystophny/sloppy/internal/ews"
	"github.com/krystophny/sloppy/internal/providerdata"
)

type ExchangeEWSConfig struct {
	Label         string
	Endpoint      string
	Username      string
	Password      string
	ServerVersion string
	BatchSize     int
	InsecureTLS   bool
	ArchiveFolder string
}

type ExchangeEWSMailProvider struct {
	client *ews.Client
	cfg    ExchangeEWSConfig
}

var _ EmailProvider = (*ExchangeEWSMailProvider)(nil)
var _ AttachmentProvider = (*ExchangeEWSMailProvider)(nil)
var _ DraftProvider = (*ExchangeEWSMailProvider)(nil)
var _ MessagePageProvider = (*ExchangeEWSMailProvider)(nil)
var _ NamedFolderProvider = (*ExchangeEWSMailProvider)(nil)
var _ ServerFilterProvider = (*ExchangeEWSMailProvider)(nil)
var _ FolderIncrementalSyncProvider = (*ExchangeEWSMailProvider)(nil)
var _ RawMessageProvider = (*ExchangeEWSMailProvider)(nil)

func ExchangeEWSConfigFromMap(label string, config map[string]any) (ExchangeEWSConfig, error) {
	cfg := ExchangeEWSConfig{Label: strings.TrimSpace(label)}
	if raw, ok := config["endpoint"].(string); ok {
		cfg.Endpoint = strings.TrimSpace(raw)
	}
	if raw, ok := config["base_url"].(string); ok && cfg.Endpoint == "" {
		cfg.Endpoint = strings.TrimSpace(raw)
	}
	if raw, ok := config["username"].(string); ok {
		cfg.Username = strings.TrimSpace(raw)
	}
	if raw, ok := config["server_version"].(string); ok {
		cfg.ServerVersion = strings.TrimSpace(raw)
	}
	if raw, ok := config["archive_folder"].(string); ok {
		cfg.ArchiveFolder = strings.TrimSpace(raw)
	}
	if raw, ok := config["batch_size"].(float64); ok {
		cfg.BatchSize = int(raw)
	}
	if raw, ok := config["batch_size"].(int); ok {
		cfg.BatchSize = raw
	}
	if raw, ok := config["insecure_tls"].(bool); ok {
		cfg.InsecureTLS = raw
	}
	return cfg, nil
}

func NewExchangeEWSMailProvider(cfg ExchangeEWSConfig) (*ExchangeEWSMailProvider, error) {
	client, err := ews.NewClient(ews.Config{
		Endpoint:      cfg.Endpoint,
		Username:      cfg.Username,
		Password:      cfg.Password,
		ServerVersion: cfg.ServerVersion,
		BatchSize:     cfg.BatchSize,
		InsecureTLS:   cfg.InsecureTLS,
	})
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.ArchiveFolder) == "" {
		cfg.ArchiveFolder = "Archive"
	}
	return &ExchangeEWSMailProvider{client: client, cfg: cfg}, nil
}

func (p *ExchangeEWSMailProvider) ListLabels(ctx context.Context) ([]providerdata.Label, error) {
	folders, err := p.client.ListFolders(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]providerdata.Label, 0, len(folders))
	for _, folder := range folders {
		if folder.Kind == ews.FolderKindCalendar || folder.Kind == ews.FolderKindContacts || folder.Kind == ews.FolderKindTasks {
			continue
		}
		name := exchangeEWSDisplayFolderName(folder.Name)
		if name == "" {
			continue
		}
		out = append(out, providerdata.Label{
			ID:             folder.ID,
			Name:           name,
			Type:           "exchange_ews",
			MessagesTotal:  folder.TotalCount,
			MessagesUnread: folder.UnreadCount,
		})
	}
	return out, nil
}

func (p *ExchangeEWSMailProvider) ListMessages(ctx context.Context, opts SearchOptions) ([]string, error) {
	if p == nil || p.client == nil {
		return nil, fmt.Errorf("exchange ews provider is not configured")
	}
	candidates, err := p.searchFolders(ctx, opts)
	if err != nil {
		return nil, err
	}
	maxResults := int(opts.MaxResults)
	if maxResults <= 0 {
		maxResults = 100
	}
	needsFilter := exchangeEWSNeedsMessageFilter(opts)
	restriction := exchangeEWSBuildRestriction(opts)
	useServerFilter := restriction != nil
	clientFilter := exchangeEWSNeedsClientFilter(opts)
	found := make(map[string]struct{}, maxResults)
	for _, folder := range candidates {
		if useServerFilter {
			offset := 0
			for len(found) < maxResults {
				page, err := p.client.FindMessagesRestricted(ctx, folder, offset, maxResults, *restriction)
				if err != nil {
					return nil, err
				}
				if len(page.ItemIDs) == 0 {
					break
				}
				if clientFilter {
					messages, err := p.client.GetMessages(ctx, page.ItemIDs)
					if err != nil {
						return nil, err
					}
					for _, message := range messages {
						if matchExchangeEWSMessage(message, opts) {
							found[message.ID] = struct{}{}
							if len(found) >= maxResults {
								return sortedMessageIDs(found), nil
							}
						}
					}
				} else {
					for _, itemID := range page.ItemIDs {
						if clean := strings.TrimSpace(itemID); clean != "" {
							found[clean] = struct{}{}
						}
						if len(found) >= maxResults {
							return sortedMessageIDs(found), nil
						}
					}
				}
				if page.IncludesLastPage {
					break
				}
				nextOffset := page.NextOffset
				if nextOffset <= offset {
					nextOffset = offset + len(page.ItemIDs)
				}
				offset = nextOffset
			}
			continue
		}
		if !needsFilter {
			page, err := p.client.FindMessages(ctx, folder, 0, maxResults)
			if err != nil {
				return nil, err
			}
			for _, itemID := range page.ItemIDs {
				if clean := strings.TrimSpace(itemID); clean != "" {
					found[clean] = struct{}{}
				}
				if len(found) >= maxResults {
					return sortedMessageIDs(found), nil
				}
			}
			continue
		}
		// Scan first page of each folder breadth-first to avoid timeout
		// on large mailboxes with many folders.
		page, err := p.client.FindMessages(ctx, folder, 0, maxResults)
		if err != nil {
			return nil, err
		}
		if len(page.ItemIDs) == 0 {
			continue
		}
		messages, err := p.client.GetMessages(ctx, page.ItemIDs)
		if err != nil {
			return nil, err
		}
		for _, message := range messages {
			if matchExchangeEWSMessage(message, opts) {
				found[message.ID] = struct{}{}
				if len(found) >= maxResults {
					return sortedMessageIDs(found), nil
				}
			}
		}
	}
	return sortedMessageIDs(found), nil
}

func (p *ExchangeEWSMailProvider) ListMessagesPage(ctx context.Context, opts SearchOptions, pageToken string) (MessagePage, error) {
	if p == nil || p.client == nil {
		return MessagePage{}, fmt.Errorf("exchange ews provider is not configured")
	}
	candidates, err := p.searchFolders(ctx, opts)
	if err != nil {
		return MessagePage{}, err
	}
	if len(candidates) == 0 {
		return MessagePage{}, nil
	}
	maxResults := int(opts.MaxResults)
	if maxResults <= 0 {
		maxResults = 100
	}
	needsFilter := exchangeEWSNeedsMessageFilter(opts)
	singleFolder := len(candidates) == 1
	// When filtering across multiple folders, use ListMessages which
	// iterates all folders internally, then return results as a single page.
	if needsFilter && !singleFolder {
		ids, err := p.ListMessages(ctx, opts)
		if err != nil {
			return MessagePage{}, err
		}
		return MessagePage{IDs: ids}, nil
	}
	offset := 0
	if strings.TrimSpace(pageToken) != "" {
		value, err := strconv.Atoi(strings.TrimSpace(pageToken))
		if err != nil || value < 0 {
			return MessagePage{}, fmt.Errorf("exchange ews invalid page token %q", pageToken)
		}
		offset = value
	}
	page, err := p.client.FindMessages(ctx, candidates[0], offset, maxResults)
	if err != nil {
		return MessagePage{}, err
	}
	out := MessagePage{
		IDs: make([]string, 0, len(page.ItemIDs)),
	}
	if len(page.ItemIDs) == 0 {
		return out, nil
	}
	if !needsFilter {
		for _, itemID := range page.ItemIDs {
			if clean := strings.TrimSpace(itemID); clean != "" {
				out.IDs = append(out.IDs, clean)
			}
		}
		if !page.IncludesLastPage && len(page.ItemIDs) > 0 {
			nextOffset := page.NextOffset
			if nextOffset <= offset {
				nextOffset = offset + len(page.ItemIDs)
			}
			out.NextPageToken = strconv.Itoa(nextOffset)
		}
		return out, nil
	}
	messages, err := p.client.GetMessages(ctx, page.ItemIDs)
	if err != nil {
		return MessagePage{}, err
	}
	for _, message := range messages {
		if !matchExchangeEWSMessage(message, opts) {
			continue
		}
		out.IDs = append(out.IDs, strings.TrimSpace(message.ID))
	}
	if !page.IncludesLastPage && len(page.ItemIDs) > 0 {
		nextOffset := page.NextOffset
		if nextOffset <= offset {
			nextOffset = offset + len(page.ItemIDs)
		}
		out.NextPageToken = strconv.Itoa(nextOffset)
	}
	return out, nil
}

func (p *ExchangeEWSMailProvider) SyncFolderChanges(ctx context.Context, folder, cursor string, maxChanges int) (FolderIncrementalSyncResult, error) {
	if p == nil || p.client == nil {
		return FolderIncrementalSyncResult{}, fmt.Errorf("exchange ews provider is not configured")
	}
	folderID, err := p.resolveFolderRef(ctx, folder)
	if err != nil {
		return FolderIncrementalSyncResult{}, err
	}
	if maxChanges <= 0 {
		maxChanges = 200
	}
	result, err := p.client.SyncFolderItems(ctx, folderID, cursor, maxChanges)
	if err != nil {
		return FolderIncrementalSyncResult{}, err
	}
	return FolderIncrementalSyncResult{
		Cursor:     strings.TrimSpace(result.SyncState),
		IDs:        append([]string(nil), result.ItemIDs...),
		DeletedIDs: append([]string(nil), result.DeletedItemIDs...),
		More:       !result.IncludesLastItem,
	}, nil
}

func (p *ExchangeEWSMailProvider) GetMessage(ctx context.Context, messageID, format string) (*providerdata.EmailMessage, error) {
	messages, err := p.getMessagesWithFormat(ctx, []string{messageID}, format)
	if err != nil {
		return nil, err
	}
	if len(messages) == 0 {
		return nil, fmt.Errorf("exchange ews message %q not found", strings.TrimSpace(messageID))
	}
	folders, err := p.client.ListFolders(ctx)
	if err != nil {
		return nil, err
	}
	decoded := decodeExchangeEWSMessage(messages[0], exchangeEWSFolderIndex(folders), format)
	return &decoded, nil
}

func (p *ExchangeEWSMailProvider) GetAttachment(ctx context.Context, messageID, attachmentID string) (*providerdata.AttachmentData, error) {
	if p == nil || p.client == nil {
		return nil, fmt.Errorf("exchange ews provider is not configured")
	}
	if strings.TrimSpace(messageID) == "" {
		return nil, fmt.Errorf("message_id is required")
	}
	cleanAttachmentID := strings.TrimSpace(attachmentID)
	if cleanAttachmentID == "" {
		return nil, fmt.Errorf("attachment_id is required")
	}
	attachment, err := p.client.GetAttachment(ctx, cleanAttachmentID)
	if err != nil {
		return nil, err
	}
	return &providerdata.AttachmentData{
		ID:       attachment.ID,
		Filename: attachment.Name,
		MimeType: attachment.ContentType,
		Size:     attachment.Size,
		IsInline: attachment.IsInline,
		Content:  append([]byte(nil), attachment.Content...),
	}, nil
}

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
	return ServerFilterCapabilities{
		Provider:         p.ProviderName(),
		SupportsList:     true,
		SupportsUpsert:   true,
		SupportsDelete:   true,
		SupportsArchive:  true,
		SupportsTrash:    true,
		SupportsMoveTo:   true,
		SupportsMarkRead: true,
		SupportsForward:  true,
	}
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
	// EWS create does not return the created rule id; reload rules and find by name.
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
	return p.client.UpdateInboxRules(ctx, []ews.RuleOperation{{
		Kind: ews.RuleOperationDelete,
		Rule: ews.Rule{ID: strings.TrimSpace(id)},
	}})
}

func (p *ExchangeEWSMailProvider) Delete(ctx context.Context, messageIDs []string) (int, error) {
	ids := compactMessageIDs(messageIDs)
	if err := p.client.DeleteItems(ctx, ids, true); err != nil {
		return 0, err
	}
	return len(ids), nil
}

func (p *ExchangeEWSMailProvider) ProviderName() string { return "exchange_ews" }

func (p *ExchangeEWSMailProvider) CreateDraft(ctx context.Context, input DraftInput) (Draft, error) {
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
	created, err := p.client.CreateDraft(ctx, ews.DraftMessage{
		Subject:    normalized.Subject,
		MIME:       raw,
		ThreadID:   normalized.ThreadID,
		InReplyTo:  normalized.InReplyTo,
		References: normalized.References,
	})
	if err != nil {
		return Draft{}, fmt.Errorf("exchange ews create draft: %w", err)
	}
	return Draft{ID: strings.TrimSpace(created.ID), ThreadID: strings.TrimSpace(created.ConversationID)}, nil
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
	updated, err := p.client.UpdateDraft(ctx, strings.TrimSpace(draftID), ews.DraftMessage{
		Subject:    normalized.Subject,
		MIME:       raw,
		ThreadID:   normalized.ThreadID,
		InReplyTo:  normalized.InReplyTo,
		References: normalized.References,
	})
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
	out := ServerFilter{
		ID:      strings.TrimSpace(rule.ID),
		Name:    strings.TrimSpace(rule.Name),
		Enabled: rule.Enabled,
		Criteria: ServerFilterCriteria{
			Subject: strings.Join(rule.Conditions.ContainsSubjectStrings, " "),
		},
		Action: ServerFilterAction{
			Trash:     rule.Actions.Delete,
			MarkRead:  rule.Actions.MarkAsRead,
			ForwardTo: mailboxesToStrings(rule.Actions.RedirectToRecipients),
			MoveTo:    moveTarget,
		},
	}
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
	rule := ews.Rule{
		ID:       strings.TrimSpace(filter.ID),
		Name:     strings.TrimSpace(filter.Name),
		Enabled:  filter.Enabled,
		Priority: 1,
		Conditions: ews.RuleConditions{
			ContainsSubjectStrings: compactMessageIDs([]string{filter.Criteria.Subject}),
			ContainsSenderStrings:  compactMessageIDs([]string{filter.Criteria.From}),
		},
		Actions: ews.RuleActions{
			Delete:               filter.Action.Trash,
			MarkAsRead:           filter.Action.MarkRead,
			RedirectToRecipients: stringsToMailboxes(filter.Action.ForwardTo),
		},
	}
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
		out = append(out, ActionResolution{
			OriginalMessageID: original,
			ResolvedMessageID: resolved,
		})
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
	haystack := strings.ToLower(strings.Join([]string{
		message.Subject,
		message.Body,
		message.From.Name,
		message.From.Email,
		message.DisplayTo,
		message.DisplayCc,
	}, "\n"))
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
	return opts.IsRead != nil ||
		opts.HasAttachment != nil ||
		opts.IsFlagged != nil ||
		opts.Text != "" ||
		opts.Subject != "" ||
		opts.From != "" ||
		opts.To != "" ||
		!opts.After.IsZero() ||
		!opts.Before.IsZero() ||
		!opts.Since.IsZero() ||
		!opts.Until.IsZero()
}

func exchangeEWSBuildRestriction(opts SearchOptions) *ews.FindRestriction {
	if !exchangeEWSNeedsMessageFilter(opts) {
		return nil
	}
	r := &ews.FindRestriction{
		From:          strings.TrimSpace(opts.From),
		HasAttachment: opts.HasAttachment,
	}
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
	return opts.IsRead != nil ||
		opts.IsFlagged != nil ||
		opts.Text != "" ||
		opts.Subject != "" ||
		opts.To != ""
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
		attachments = append(attachments, providerdata.Attachment{
			ID:       attachment.ID,
			Filename: attachment.Name,
			MimeType: attachment.ContentType,
			Size:     attachment.Size,
			IsInline: attachment.IsInline,
		})
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
	return providerdata.EmailMessage{
		ID:                strings.TrimSpace(message.ID),
		ThreadID:          strings.TrimSpace(message.ConversationID),
		InternetMessageID: strings.TrimSpace(message.InternetMessageID),
		Subject:           strings.TrimSpace(message.Subject),
		Sender:            sender,
		Recipients:        recipients,
		Date:              message.ReceivedAt,
		Snippet:           snippetFromBody(message.Body),
		Labels:            exchangeEWSFolderLabels(message.ParentFolderID, folders),
		IsRead:            message.IsRead,
		IsFlagged:         strings.EqualFold(strings.TrimSpace(message.FlagStatus), "Flagged"),
		BodyText:          bodyPtr,
		Attachments:       attachments,
	}
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
