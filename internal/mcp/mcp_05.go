package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/sloppy-org/sloptools/internal/email"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/store"
	"strconv"
	"strings"
	"time"
)

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
		logEntry, err := st.CreateMailActionLog(store.MailActionLogInput{AccountID: account.ID, Provider: account.Provider, MessageID: messageID, Action: action, FolderFrom: mcpMailActionMessageFolder(message), FolderTo: targetFolder, Subject: mcpMailActionMessageSubject(message), Sender: mcpMailActionMessageSender(message), Request: requestPayload, Status: store.MailActionLogPending})
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
	bindingRefs := s.mailBindingAffectedRefs(context.Background(), account, provider, mailWalkPostMutationIDs(messageIDs, applied.Resolutions))
	return withAffected(
		mailActionResult(account, action, messageIDs, applied.Count, untilAt),
		append(mailMessageAffectedRefs(account, messageIDs, applied.Resolutions), bindingRefs...)...,
	), nil
}

// mailWalkPostMutationIDs returns the message IDs that survived a mail
// mutation (resolved IDs win over originals). Empty when every message was
// removed (e.g. delete) so the walker still runs against the originals.
func mailWalkPostMutationIDs(originals []string, resolutions []email.ActionResolution) []string {
	resolved := map[string]string{}
	for _, resolution := range resolutions {
		original := strings.TrimSpace(resolution.OriginalMessageID)
		new := strings.TrimSpace(resolution.ResolvedMessageID)
		if original == "" {
			continue
		}
		resolved[original] = new
	}
	out := make([]string, 0, len(originals))
	for _, id := range originals {
		clean := strings.TrimSpace(id)
		if clean == "" {
			continue
		}
		if mapped, ok := resolved[clean]; ok && mapped != "" {
			out = append(out, mapped)
			continue
		}
		out = append(out, clean)
	}
	return out
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
	requestPayload := map[string]any{"action": action, "message_ids": append([]string(nil), messageIDs...), "folder": folder, "label": label}
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
	result := map[string]interface{}{"account": account, "action": action, "message_ids": append([]string(nil), messageIDs...), "succeeded": succeeded}
	if action == "defer" && !untilAt.IsZero() {
		result["until"] = untilAt.UTC().Format(time.RFC3339)
	}
	return result
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func stringListArg(args map[string]interface{}, key string) []string {
	value, ok := args[key]
	if !ok {
		return nil
	}
	switch typed := value.(type) {
	case []string:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if clean := strings.TrimSpace(item); clean != "" {
				out = append(out, clean)
			}
		}
		return out
	case []interface{}:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if clean := strings.TrimSpace(fmt.Sprint(item)); clean != "" && clean != "<nil>" {
				out = append(out, clean)
			}
		}
		return out
	case string:
		parts := strings.Split(typed, ",")
		out := make([]string, 0, len(parts))
		for _, part := range parts {
			if clean := strings.TrimSpace(part); clean != "" {
				out = append(out, clean)
			}
		}
		return out
	default:
		return nil
	}
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
	return map[string]interface{}{"source_account": sourceAccount, "target_account": targetAccount, "target_folder": targetFolder, "copied": len(newIDs), "new_message_ids": newIDs}, nil
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
	return map[string]interface{}{"account": account, "capabilities": filterProvider.ServerFilterCapabilities(), "filters": filters, "count": len(filters)}, nil
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
	return map[string]interface{}{"account": account, "filter": saved}, nil
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
	return map[string]interface{}{"account": account, "filter_id": filterID, "deleted": true}, nil
}

func (s *Server) mailProviderForTool(args map[string]interface{}) (store.ExternalAccount, email.EmailProvider, error) {
	st, err := s.requireStore()
	if err != nil {
		return store.ExternalAccount{}, nil, err
	}
	account, err := accountForTool(st, args, "email", emailCapableProvider)
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
	return s.groupware.MailFor(ctx, account.ID)
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
