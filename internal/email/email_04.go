package email

import (
	"context"
	"fmt"
	"github.com/sloppy-org/sloptools/internal/ews"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
)

func (p *ExchangeMailProvider) CreateReplyDraft(ctx context.Context, messageID string, input DraftInput) (Draft, error) {
	if p == nil || p.client == nil {
		return Draft{}, fmt.Errorf("exchange provider is not configured")
	}
	var message Message
	if err := p.client.doJSON(ctx, "POST", "/v1.0/me/messages/"+url.PathEscape(strings.TrimSpace(messageID))+"/createReply", nil, nil, &message); err != nil {
		return Draft{}, fmt.Errorf("exchange create reply draft: %w", err)
	}
	reply := input
	if len(reply.To) == 0 && message.From != nil {
		reply.To = []string{strings.TrimSpace(message.From.EmailAddress.Address)}
	}
	if strings.TrimSpace(reply.Subject) == "" {
		reply.Subject = ensureReplySubject(message.Subject)
	} else {
		reply.Subject = ensureReplySubject(reply.Subject)
	}
	if strings.TrimSpace(reply.ThreadID) == "" {
		reply.ThreadID = strings.TrimSpace(message.ConversationID)
	}
	updated, err := p.UpdateDraft(ctx, strings.TrimSpace(message.ID), reply)
	if err != nil {
		return Draft{}, err
	}
	if updated.ThreadID == "" {
		updated.ThreadID = strings.TrimSpace(message.ConversationID)
	}
	return updated, nil
}

func (p *ExchangeMailProvider) UpdateDraft(ctx context.Context, draftID string, input DraftInput) (Draft, error) {
	if p == nil || p.client == nil {
		return Draft{}, fmt.Errorf("exchange provider is not configured")
	}
	normalized, err := NormalizeDraftInput(input)
	if err != nil {
		return Draft{}, err
	}
	if err := p.client.doJSON(ctx, "PATCH", "/v1.0/me/messages/"+url.PathEscape(strings.TrimSpace(draftID)), nil, exchangeDraftRequest(normalized), nil); err != nil {
		return Draft{}, fmt.Errorf("exchange update draft: %w", err)
	}
	message, err := p.client.GetMessage(ctx, strings.TrimSpace(draftID))
	if err != nil {
		return Draft{}, fmt.Errorf("exchange get updated draft: %w", err)
	}
	return Draft{ID: strings.TrimSpace(message.ID), ThreadID: strings.TrimSpace(message.ConversationID)}, nil
}

func (p *ExchangeMailProvider) SendDraft(ctx context.Context, draftID string, _ DraftInput) error {
	if p == nil || p.client == nil {
		return fmt.Errorf("exchange provider is not configured")
	}
	if err := p.client.doJSON(ctx, "POST", "/v1.0/me/messages/"+url.PathEscape(strings.TrimSpace(draftID))+"/send", nil, nil, nil); err != nil {
		return fmt.Errorf("exchange send draft: %w", err)
	}
	return nil
}

func exchangeDraftRequest(input DraftInput) map[string]any {
	req := map[string]any{"subject": strings.TrimSpace(input.Subject), "body": map[string]string{"contentType": "text", "content": strings.TrimSpace(input.Body)}, "toRecipients": exchangeRecipients(input.To), "ccRecipients": exchangeRecipients(input.Cc), "bccRecipients": exchangeRecipients(input.Bcc)}
	if strings.TrimSpace(input.From) != "" {
		req["from"] = exchangeRecipient(strings.TrimSpace(input.From))
	}
	return req
}

func exchangeRecipients(values []string) []map[string]any {
	out := make([]map[string]any, 0, len(values))
	for _, value := range values {
		recipient := exchangeRecipient(value)
		if len(recipient) > 0 {
			out = append(out, recipient)
		}
	}
	return out
}

func exchangeRecipient(value string) map[string]any {
	clean := strings.TrimSpace(value)
	if clean == "" {
		return nil
	}
	if parsed, err := mail.ParseAddress(clean); err == nil {
		clean = strings.TrimSpace(parsed.Address)
	}
	if clean == "" {
		return nil
	}
	return map[string]any{"emailAddress": map[string]string{"address": clean}}
}

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

var _ ExistingDraftSender = (*ExchangeEWSMailProvider)(nil)

var _ MessagePageProvider = (*ExchangeEWSMailProvider)(nil)

var _ NamedFolderProvider = (*ExchangeEWSMailProvider)(nil)

var _ ServerFilterProvider = (*ExchangeEWSMailProvider)(nil)

var _ FolderIncrementalSyncProvider = (*ExchangeEWSMailProvider)(nil)

var _ RawMessageProvider = (*ExchangeEWSMailProvider)(nil)

var _ FlagMutator = (*ExchangeEWSMailProvider)(nil)

var _ CategoryMutator = (*ExchangeEWSMailProvider)(nil)

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
	client, err := ews.NewClient(ews.Config{Endpoint: cfg.Endpoint, Username: cfg.Username, Password: cfg.Password, ServerVersion: cfg.ServerVersion, BatchSize: cfg.BatchSize, InsecureTLS: cfg.InsecureTLS})
	if err != nil {
		return nil, err
	}
	return NewExchangeEWSMailProviderFromClient(cfg, client), nil
}

func NewExchangeEWSMailProviderFromClient(cfg ExchangeEWSConfig, client *ews.Client) *ExchangeEWSMailProvider {
	if strings.TrimSpace(cfg.ArchiveFolder) == "" {
		cfg.ArchiveFolder = "Archive"
	}
	return &ExchangeEWSMailProvider{client: client, cfg: cfg}
}

func (p *ExchangeEWSMailProvider) Client() *ews.Client {
	if p == nil {
		return nil
	}
	return p.client
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
		out = append(out, providerdata.Label{ID: folder.ID, Name: name, Type: "exchange_ews", MessagesTotal: folder.TotalCount, MessagesUnread: folder.UnreadCount})
	}
	return out, nil
}

func (p *ExchangeEWSMailProvider) ListMessages(ctx context.Context, opts SearchOptions) ([]string, error) {
	if p == nil || p.client == nil {
		return nil, fmt.Errorf("exchange ews provider is not configured")
	}
	maxResults := int(opts.MaxResults)
	if maxResults <= 0 {
		maxResults = 100
	}
	// Fast path: when any text-shaped field is set (text/subject/to/from/
	// participants), use the EWS AQS QueryString. The Outlook search index
	// answers in milliseconds against the whole mailbox, so we can scope to
	// MsgFolderRoot Deep and skip the per-folder GetMessages scan that
	// otherwise downloaded every message body to filter client-side.
	if aqs := exchangeEWSBuildAQS(opts); aqs != "" {
		folders, err := p.searchFolders(ctx, opts)
		if err != nil {
			return nil, err
		}
		found := make(map[string]struct{}, maxResults)
		needsClientPostFilter := opts.IsRead != nil || opts.IsFlagged != nil || opts.HasAttachment != nil || !opts.After.IsZero() || !opts.Before.IsZero() || !opts.Since.IsZero() || !opts.Until.IsZero()
		for _, folder := range folders {
			if len(found) >= maxResults {
				break
			}
			offset := 0
			for len(found) < maxResults {
				page, err := p.client.FindMessagesByQuery(ctx, folder, offset, maxResults, aqs, false)
				if err != nil {
					return nil, err
				}
				if len(page.ItemIDs) == 0 {
					break
				}
				if needsClientPostFilter {
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
		}
		return sortedMessageIDs(found), nil
	}
	candidates, err := p.searchFolders(ctx, opts)
	if err != nil {
		return nil, err
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
		offset := 0
		const scanLimit = 200
		for len(found) < maxResults {
			page, err := p.client.FindMessages(ctx, folder, offset, scanLimit)
			if err != nil {
				return nil, err
			}
			if len(page.ItemIDs) == 0 {
				break
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
			if page.IncludesLastPage {
				break
			}
			nextOffset := page.NextOffset
			if nextOffset <= offset {
				nextOffset = offset + len(page.ItemIDs)
			}
			offset = nextOffset
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
	out := MessagePage{IDs: make([]string, 0, len(page.ItemIDs))}
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
	return FolderIncrementalSyncResult{Cursor: strings.TrimSpace(result.SyncState), IDs: append([]string(nil), result.ItemIDs...), DeletedIDs: append([]string(nil), result.DeletedItemIDs...), More: !result.IncludesLastItem}, nil
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
	return &providerdata.AttachmentData{ID: attachment.ID, Filename: attachment.Name, MimeType: attachment.ContentType, Size: attachment.Size, IsInline: attachment.IsInline, Content: append([]byte(nil), attachment.Content...)}, nil
}
