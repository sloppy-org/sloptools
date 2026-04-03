package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/email"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/store"
)

const mcpEmailBindingObjectType = "email"

type mailSyncAccountConfig struct {
	Host            string `json:"host"`
	Port            int    `json:"port"`
	Username        string `json:"username"`
	TLS             bool   `json:"tls"`
	StartTLS        bool   `json:"starttls"`
	FromAddress     string `json:"from_address"`
	TokenPath       string `json:"token_path"`
	TokenFile       string `json:"token_file"`
	CredentialsPath string `json:"credentials_path"`
	CredentialsFile string `json:"credentials_file"`
}

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
	return map[string]interface{}{
		"accounts": out,
		"count":    len(out),
	}, nil
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
	return map[string]interface{}{
		"account": account,
		"labels":  labels,
		"count":   len(labels),
	}, nil
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
	if len(ids) <= 10 {
		format = "full"
	}
	messages, err := provider.GetMessages(context.Background(), ids, format)
	if err != nil {
		return nil, err
	}
	sort.Slice(messages, func(i, j int) bool {
		return messages[i].Date.After(messages[j].Date)
	})
	return map[string]interface{}{
		"account":         account,
		"messages":        messages,
		"count":           len(messages),
		"page_token":      pageToken,
		"next_page_token": nextPageToken,
	}, nil
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
	return map[string]interface{}{
		"account": account,
		"message": message,
	}, nil
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
	return map[string]interface{}{
		"account": account,
		"attachment": map[string]interface{}{
			"id":             strings.TrimSpace(attachment.ID),
			"filename":       strings.TrimSpace(attachment.Filename),
			"mime_type":      strings.TrimSpace(attachment.MimeType),
			"size":           attachment.Size,
			"is_inline":      attachment.IsInline,
			"content_base64": base64.StdEncoding.EncodeToString(attachment.Content),
		},
	}, nil
}

func (s *Server) mailAction(args map[string]interface{}) (map[string]interface{}, error) {
	st, err := s.requireStore()
	if err != nil {
		return nil, err
	}
	account, provider, err := s.mailProviderForTool(args)
	if err != nil {
		return nil, err
	}
	defer provider.Close()
	action := strings.TrimSpace(strings.ToLower(strArg(args, "action")))
	if action == "" {
		return nil, fmt.Errorf("action is required")
	}
	untilAt, untilRaw, err := parseMailActionUntil(args, action)
	if err != nil {
		return nil, err
	}
	query := strings.TrimSpace(strArg(args, "query"))
	messageIDs, err := resolveMailActionMessageIDs(context.Background(), provider, args)
	if err != nil {
		return nil, err
	}
	folder := strings.TrimSpace(strArg(args, "folder"))
	label := strings.TrimSpace(strArg(args, "label"))
	var archive *bool
	if value, ok := args["archive"].(bool); ok {
		archive = &value
	}
	byID := mailMessagesByID(context.Background(), provider, messageIDs)
	targetFolder := mcpMailActionTargetFolder(account, action, folder, label)
	requestPayload := mailActionRequestPayload(args, action, messageIDs, folder, label, query, archive, untilRaw)
	if len(messageIDs) == 0 {
		return mailActionResult(account, action, nil, 0, untilAt), nil
	}
	logs := make([]store.MailActionLog, 0, len(messageIDs))
	for _, messageID := range messageIDs {
		message := byID[messageID]
		logEntry, err := st.CreateMailActionLog(store.MailActionLogInput{
			AccountID:  account.ID,
			Provider:   account.Provider,
			MessageID:  messageID,
			Action:     action,
			FolderFrom: mcpMailActionMessageFolder(message),
			FolderTo:   targetFolder,
			Subject:    mcpMailActionMessageSubject(message),
			Sender:     mcpMailActionMessageSender(message),
			Request:    requestPayload,
			Status:     store.MailActionLogPending,
		})
		if err != nil {
			return nil, err
		}
		logs = append(logs, logEntry)
	}
	applied, err := applyMailActionGeneric(context.Background(), account, provider, action, messageIDs, folder, label, archive, untilAt)
	if err != nil {
		for _, logEntry := range logs {
			_ = st.UpdateMailActionLogResult(logEntry.ID, store.MailActionLogFailed, "", err.Error())
		}
		return nil, err
	}
	if err := applyMailActionResolutionsStore(st, account, action, targetFolder, applied.Resolutions); err != nil {
		for _, logEntry := range logs {
			_ = st.UpdateMailActionLogResult(logEntry.ID, store.MailActionLogReconcileFailed, "", err.Error())
		}
		return nil, err
	}
	resolvedByMessageID := make(map[string]string, len(applied.Resolutions))
	for _, resolution := range applied.Resolutions {
		resolvedByMessageID[strings.TrimSpace(resolution.OriginalMessageID)] = strings.TrimSpace(resolution.ResolvedMessageID)
	}
	for _, logEntry := range logs {
		_ = st.UpdateMailActionLogResult(logEntry.ID, store.MailActionLogApplied, resolvedByMessageID[strings.TrimSpace(logEntry.MessageID)], "")
	}
	return mailActionResult(account, action, messageIDs, applied.Count, untilAt), nil
}

func resolveMailActionMessageIDs(ctx context.Context, provider email.EmailProvider, args map[string]interface{}) ([]string, error) {
	messageIDs := mailMessageIDsArg(args)
	if len(messageIDs) > 0 {
		return messageIDs, nil
	}
	query := strings.TrimSpace(strArg(args, "query"))
	if query == "" {
		return nil, fmt.Errorf("message_ids or query are required")
	}
	searchArgs := make(map[string]interface{}, len(args)+1)
	for key, value := range args {
		searchArgs[key] = value
	}
	if strings.TrimSpace(strArg(searchArgs, "text")) == "" {
		searchArgs["text"] = query
	}
	opts, _, err := mailSearchOptionsFromArgs(searchArgs)
	if err != nil {
		return nil, err
	}
	ids, _, err := listMailMessageIDs(ctx, provider, opts, "")
	if err != nil {
		return nil, err
	}
	return compactStringList(ids), nil
}

func mailMessagesByID(ctx context.Context, provider email.EmailProvider, messageIDs []string) map[string]*providerdata.EmailMessage {
	resolvedMessages, _ := provider.GetMessages(ctx, messageIDs, "full")
	byID := map[string]*providerdata.EmailMessage{}
	for _, message := range resolvedMessages {
		if message == nil {
			continue
		}
		if id := strings.TrimSpace(message.ID); id != "" {
			byID[id] = message
		}
	}
	return byID
}

func mailActionRequestPayload(args map[string]interface{}, action string, messageIDs []string, folder, label, query string, archive *bool, untilRaw string) map[string]any {
	requestPayload := map[string]any{
		"action":      action,
		"message_ids": append([]string(nil), messageIDs...),
		"folder":      folder,
		"label":       label,
	}
	if query != "" {
		requestPayload["query"] = query
		if limit, ok := args["limit"]; ok {
			requestPayload["limit"] = limit
		}
	}
	if archive != nil {
		requestPayload["archive"] = *archive
	}
	if untilRaw != "" {
		requestPayload["until"] = untilRaw
	}
	return requestPayload
}

func mailActionResult(account store.ExternalAccount, action string, messageIDs []string, succeeded int, untilAt time.Time) map[string]interface{} {
	result := map[string]interface{}{
		"account":     account,
		"action":      action,
		"message_ids": append([]string(nil), messageIDs...),
		"succeeded":   succeeded,
	}
	if action == "defer" && !untilAt.IsZero() {
		result["until"] = untilAt.UTC().Format(time.RFC3339)
	}
	return result
}

func (s *Server) mailMessageCopy(args map[string]interface{}) (map[string]interface{}, error) {
	st, err := s.requireStore()
	if err != nil {
		return nil, err
	}
	sourceAccountID, err := int64Arg(args, "source_account_id")
	if err != nil {
		return nil, err
	}
	targetAccountID, err := int64Arg(args, "target_account_id")
	if err != nil {
		return nil, err
	}
	targetFolder := strings.TrimSpace(strArg(args, "target_folder"))
	if targetFolder == "" {
		return nil, fmt.Errorf("target_folder is required")
	}
	messageIDs := mailMessageIDsArg(args)
	if len(messageIDs) == 0 {
		return nil, fmt.Errorf("message_id or message_ids is required")
	}
	sourceAccount, err := st.GetExternalAccount(sourceAccountID)
	if err != nil {
		return nil, fmt.Errorf("source account: %w", err)
	}
	targetAccount, err := st.GetExternalAccount(targetAccountID)
	if err != nil {
		return nil, fmt.Errorf("target account: %w", err)
	}
	sourceProvider, err := s.emailProviderForAccount(context.Background(), sourceAccount)
	if err != nil {
		return nil, fmt.Errorf("source provider: %w", err)
	}
	defer sourceProvider.Close()
	targetProvider, err := s.emailProviderForAccount(context.Background(), targetAccount)
	if err != nil {
		return nil, fmt.Errorf("target provider: %w", err)
	}
	defer targetProvider.Close()
	sourceRaw, ok := sourceProvider.(email.RawMessageProvider)
	if !ok {
		return nil, fmt.Errorf("source account %q does not support raw message export", sourceAccount.AccountName)
	}
	targetRaw, ok := targetProvider.(email.RawMessageProvider)
	if !ok {
		return nil, fmt.Errorf("target account %q does not support raw message import", targetAccount.AccountName)
	}
	return copyRawMessages(context.Background(), sourceRaw, targetRaw, sourceAccount, targetAccount, messageIDs, targetFolder)
}

func copyRawMessages(ctx context.Context, source, target email.RawMessageProvider, sourceAccount, targetAccount store.ExternalAccount, messageIDs []string, targetFolder string) (map[string]interface{}, error) {
	newIDs := make([]string, 0, len(messageIDs))
	for _, messageID := range messageIDs {
		mime, err := source.ExportRawMessage(ctx, messageID)
		if err != nil {
			return nil, fmt.Errorf("export message %s: %w", messageID, err)
		}
		newID, err := target.ImportRawMessage(ctx, mime, targetFolder)
		if err != nil {
			return nil, fmt.Errorf("import message %s: %w", messageID, err)
		}
		newIDs = append(newIDs, newID)
	}
	return map[string]interface{}{
		"source_account": sourceAccount,
		"target_account": targetAccount,
		"target_folder":  targetFolder,
		"copied":         len(newIDs),
		"new_message_ids": newIDs,
	}, nil
}

func (s *Server) mailServerFilterList(args map[string]interface{}) (map[string]interface{}, error) {
	account, provider, err := s.mailProviderForTool(args)
	if err != nil {
		return nil, err
	}
	defer provider.Close()
	filterProvider, ok := provider.(email.ServerFilterProvider)
	if !ok {
		return nil, fmt.Errorf("server filters are not supported for this account")
	}
	filters, err := filterProvider.ListServerFilters(context.Background())
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"account":      account,
		"capabilities": filterProvider.ServerFilterCapabilities(),
		"filters":      filters,
		"count":        len(filters),
	}, nil
}

func (s *Server) mailServerFilterUpsert(args map[string]interface{}) (map[string]interface{}, error) {
	account, provider, err := s.mailProviderForTool(args)
	if err != nil {
		return nil, err
	}
	defer provider.Close()
	filterProvider, ok := provider.(email.ServerFilterProvider)
	if !ok {
		return nil, fmt.Errorf("server filters are not supported for this account")
	}
	raw, ok := args["filter"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("filter is required")
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var filter email.ServerFilter
	if err := json.Unmarshal(data, &filter); err != nil {
		return nil, fmt.Errorf("invalid filter: %w", err)
	}
	if overrideID := strings.TrimSpace(strArg(args, "filter_id")); overrideID != "" {
		filter.ID = overrideID
	}
	if strings.TrimSpace(filter.Name) == "" {
		return nil, fmt.Errorf("filter name is required")
	}
	saved, err := filterProvider.UpsertServerFilter(context.Background(), filter)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"account": account,
		"filter":  saved,
	}, nil
}

func (s *Server) mailServerFilterDelete(args map[string]interface{}) (map[string]interface{}, error) {
	account, provider, err := s.mailProviderForTool(args)
	if err != nil {
		return nil, err
	}
	defer provider.Close()
	filterProvider, ok := provider.(email.ServerFilterProvider)
	if !ok {
		return nil, fmt.Errorf("server filters are not supported for this account")
	}
	filterID := strings.TrimSpace(strArg(args, "filter_id"))
	if filterID == "" {
		return nil, fmt.Errorf("filter_id is required")
	}
	if err := filterProvider.DeleteServerFilter(context.Background(), filterID); err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"account":   account,
		"filter_id": filterID,
		"deleted":   true,
	}, nil
}

func (s *Server) mailProviderForTool(args map[string]interface{}) (store.ExternalAccount, email.EmailProvider, error) {
	st, err := s.requireStore()
	if err != nil {
		return store.ExternalAccount{}, nil, err
	}
	accountID, err := int64Arg(args, "account_id")
	if err != nil {
		return store.ExternalAccount{}, nil, err
	}
	account, err := st.GetExternalAccount(accountID)
	if err != nil {
		return store.ExternalAccount{}, nil, err
	}
	provider, err := s.emailProviderForAccount(context.Background(), account)
	if err != nil {
		return store.ExternalAccount{}, nil, err
	}
	return account, provider, nil
}

func (s *Server) emailProviderForAccount(ctx context.Context, account store.ExternalAccount) (email.EmailProvider, error) {
	if s.newEmailProvider != nil {
		return s.newEmailProvider(ctx, account)
	}
	cfg, err := decodeMailSyncAccountConfig(account)
	if err != nil {
		return nil, err
	}
	switch account.Provider {
	case store.ExternalProviderGmail:
		return email.NewGmailWithFiles(gmailCredentialsPathForAccount(cfg), gmailTokenPathForAccount(account, cfg))
	case store.ExternalProviderIMAP:
		if cfg.Host == "" {
			return nil, fmt.Errorf("imap host is required")
		}
		if cfg.Username == "" {
			return nil, fmt.Errorf("imap username is required")
		}
		password, _, err := s.store.ResolveExternalAccountPasswordForAccount(ctx, account)
		if err != nil {
			return nil, err
		}
		useTLS := cfg.TLS || cfg.Port == 993
		return email.NewIMAPClient(account.AccountName, cfg.Host, cfg.Port, cfg.Username, password, useTLS, cfg.StartTLS), nil
	case store.ExternalProviderExchange:
		exchangeCfg, err := decodeExchangeAccountConfig(account)
		if err != nil {
			return nil, err
		}
		return email.NewExchangeMailProvider(exchangeCfg)
	case store.ExternalProviderExchangeEWS:
		ewsCfg, err := decodeExchangeEWSAccountConfig(account)
		if err != nil {
			return nil, err
		}
		password, _, err := s.store.ResolveExternalAccountPasswordForAccount(ctx, account)
		if err != nil {
			return nil, err
		}
		ewsCfg.Password = password
		return email.NewExchangeEWSMailProvider(ewsCfg)
	default:
		return nil, fmt.Errorf("email provider %s is not supported", account.Provider)
	}
}

func decodeMailSyncAccountConfig(account store.ExternalAccount) (mailSyncAccountConfig, error) {
	var cfg mailSyncAccountConfig
	raw := strings.TrimSpace(account.ConfigJSON)
	if raw == "" || raw == "{}" {
		return cfg, nil
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return mailSyncAccountConfig{}, fmt.Errorf("decode %s mail config: %w", account.Provider, err)
	}
	cfg.Host = strings.TrimSpace(cfg.Host)
	cfg.Username = strings.TrimSpace(cfg.Username)
	cfg.FromAddress = strings.TrimSpace(cfg.FromAddress)
	cfg.TokenPath = strings.TrimSpace(cfg.TokenPath)
	cfg.TokenFile = strings.TrimSpace(cfg.TokenFile)
	cfg.CredentialsPath = strings.TrimSpace(cfg.CredentialsPath)
	cfg.CredentialsFile = strings.TrimSpace(cfg.CredentialsFile)
	return cfg, nil
}

func decodeExchangeAccountConfig(account store.ExternalAccount) (email.ExchangeConfig, error) {
	config := map[string]interface{}{}
	raw := strings.TrimSpace(account.ConfigJSON)
	if raw != "" && raw != "{}" {
		if err := json.Unmarshal([]byte(raw), &config); err != nil {
			return email.ExchangeConfig{}, fmt.Errorf("decode exchange account config: %w", err)
		}
	}
	return email.ExchangeConfigFromMap(account.AccountName, config)
}

func decodeExchangeEWSAccountConfig(account store.ExternalAccount) (email.ExchangeEWSConfig, error) {
	config := map[string]interface{}{}
	raw := strings.TrimSpace(account.ConfigJSON)
	if raw != "" && raw != "{}" {
		if err := json.Unmarshal([]byte(raw), &config); err != nil {
			return email.ExchangeEWSConfig{}, fmt.Errorf("decode exchange ews account config: %w", err)
		}
	}
	return email.ExchangeEWSConfigFromMap(account.AccountName, config)
}

func mailSyncConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ".sloptools"
	}
	return filepath.Join(home, ".config", "sloptools")
}

func emailConfigPath(configDir, explicitPath, fileName string) string {
	switch {
	case strings.TrimSpace(explicitPath) != "":
		clean := filepath.Clean(strings.TrimSpace(explicitPath))
		if filepath.IsAbs(clean) {
			return clean
		}
		return filepath.Join(configDir, clean)
	case strings.TrimSpace(fileName) != "":
		return filepath.Join(configDir, strings.TrimSpace(fileName))
	default:
		return ""
	}
}

func gmailTokenPathForAccount(account store.ExternalAccount, cfg mailSyncAccountConfig) string {
	configDir := mailSyncConfigDir()
	if path := emailConfigPath(configDir, cfg.TokenPath, ""); path != "" {
		return path
	}
	if strings.TrimSpace(cfg.TokenFile) != "" {
		return filepath.Join(configDir, "tokens", strings.TrimSpace(cfg.TokenFile))
	}
	return store.ExternalAccountTokenPath(configDir, account.Provider, account.AccountName)
}

func gmailCredentialsPathForAccount(cfg mailSyncAccountConfig) string {
	configDir := mailSyncConfigDir()
	if path := emailConfigPath(configDir, cfg.CredentialsPath, cfg.CredentialsFile); path != "" {
		return path
	}
	return filepath.Join(configDir, "gmail_credentials.json")
}

func mailSearchOptionsFromArgs(args map[string]interface{}) (email.SearchOptions, string, error) {
	opts := email.DefaultSearchOptions()
	opts.Folder = strings.TrimSpace(strArg(args, "folder"))
	opts.Text = strings.TrimSpace(strArg(args, "text"))
	opts.Subject = strings.TrimSpace(strArg(args, "subject"))
	opts.From = strings.TrimSpace(strArg(args, "from"))
	opts.To = strings.TrimSpace(strArg(args, "to"))
	if raw, ok := optionalStringArg(args, "limit"); ok && raw != nil {
		value, err := strconv.Atoi(*raw)
		if err != nil || value <= 0 {
			return email.SearchOptions{}, "", fmt.Errorf("limit must be a positive integer")
		}
		opts.MaxResults = int64(value)
	}
	switch raw := args["limit"].(type) {
	case float64:
		if raw > 0 {
			opts.MaxResults = int64(raw)
		}
	case int:
		if raw > 0 {
			opts.MaxResults = int64(raw)
		}
	case int64:
		if raw > 0 {
			opts.MaxResults = raw
		}
	}
	if raw, ok := args["days"].(float64); ok && raw > 0 {
		opts = opts.WithLastDays(int(raw))
	}
	if raw, ok := optionalStringArg(args, "after"); ok && raw != nil && *raw != "" {
		value, err := time.Parse(time.RFC3339, *raw)
		if err != nil {
			return email.SearchOptions{}, "", fmt.Errorf("after must be RFC3339")
		}
		opts.After = value
	}
	if raw, ok := optionalStringArg(args, "before"); ok && raw != nil && *raw != "" {
		value, err := time.Parse(time.RFC3339, *raw)
		if err != nil {
			return email.SearchOptions{}, "", fmt.Errorf("before must be RFC3339")
		}
		opts.Before = value
	}
	if value, ok := args["include_spam_trash"].(bool); ok {
		opts.IncludeSpamTrash = value
	}
	if value, ok := args["has_attachment"].(bool); ok {
		opts.HasAttachment = &value
	}
	if value, ok := args["is_read"].(bool); ok {
		opts.IsRead = &value
	}
	if value, ok := args["is_flagged"].(bool); ok {
		opts.IsFlagged = &value
	}
	return opts, strings.TrimSpace(strArg(args, "page_token")), nil
}

func listMailMessageIDs(ctx context.Context, provider email.EmailProvider, opts email.SearchOptions, pageToken string) ([]string, string, error) {
	pager, ok := provider.(email.MessagePageProvider)
	if ok {
		page, err := pager.ListMessagesPage(ctx, opts, pageToken)
		if err != nil {
			return nil, "", err
		}
		return page.IDs, strings.TrimSpace(page.NextPageToken), nil
	}
	if pageToken != "" {
		return nil, "", fmt.Errorf("page_token is not supported for this provider")
	}
	ids, err := provider.ListMessages(ctx, opts)
	if err != nil {
		return nil, "", err
	}
	return ids, "", nil
}

func mailMessageIDsArg(args map[string]interface{}) []string {
	values := []string{}
	if raw, ok := args["message_ids"].([]interface{}); ok {
		for _, value := range raw {
			text, ok := value.(string)
			if ok {
				values = append(values, text)
			}
		}
	}
	if raw, ok := args["message_ids"].([]string); ok {
		values = append(values, raw...)
	}
	if raw := strings.TrimSpace(strArg(args, "message_id")); raw != "" {
		values = append(values, raw)
	}
	return compactStringList(values)
}

type mcpMailActionApplyResult struct {
	Count       int
	Resolutions []email.ActionResolution
}

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
		updates = append(updates, store.ExternalBindingReconcileUpdate{
			ObjectType:        mcpEmailBindingObjectType,
			OldRemoteID:       resolution.OriginalMessageID,
			NewRemoteID:       resolution.ResolvedMessageID,
			ContainerRef:      containerRef,
			FollowUpItemState: itemState,
		})
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
