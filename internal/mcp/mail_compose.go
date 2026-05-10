package mcp

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/sloppy-org/sloptools/internal/email"
	"github.com/sloppy-org/sloptools/internal/groupware"
	"github.com/sloppy-org/sloptools/internal/mailboxsettings"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/store"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"time"
)

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
	return email.DraftAttachment{Filename: filename, ContentType: contentType, Content: content}, nil
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

func (s *Server) mailFlagSet(args map[string]interface{}) (map[string]interface{}, error) {
	account, provider, err := s.mailProviderForTool(args)
	if err != nil {
		return nil, err
	}
	defer provider.Close()
	mutator, ok := groupware.Supports[email.FlagMutator](provider)
	if !ok {
		return nil, fmt.Errorf("flag mutation is not supported for provider %s", account.Provider)
	}
	ids := mailMessageIDsArg(args)
	if len(ids) == 0 {
		return nil, fmt.Errorf("message_ids is required")
	}
	status, err := normalizeFlagStatus(strArg(args, "status"))
	if err != nil {
		return nil, err
	}
	if status == email.FlagStatusNotFlagged {
		return nil, fmt.Errorf("use mail_flag_clear to clear a flag; status %q is not valid for mail_flag_set", status)
	}
	flag := email.Flag{Status: status}
	if raw, ok := optionalStringArg(args, "due_at"); ok && raw != nil && strings.TrimSpace(*raw) != "" {
		due, err := parseFlagDueAt(*raw)
		if err != nil {
			return nil, err
		}
		flag.DueAt = &due
	}
	count, err := mutator.SetFlag(context.Background(), ids, flag)
	if err != nil {
		if errors.Is(err, email.ErrCapabilityUnsupported) {
			return map[string]interface{}{"account": account, "succeeded": 0, "message_ids": ids, "status": status, "error_code": "capability_unsupported", "error_detail": err.Error()}, nil
		}
		return nil, err
	}
	bindingRefs := s.mailBindingAffectedRefs(context.Background(), account, provider, ids)
	return withAffected(
		map[string]interface{}{"account": account, "succeeded": count, "message_ids": ids, "status": status},
		append(mailMessageAffectedRefs(account, ids, nil), bindingRefs...)...,
	), nil
}

func (s *Server) mailFlagClear(args map[string]interface{}) (map[string]interface{}, error) {
	account, provider, err := s.mailProviderForTool(args)
	if err != nil {
		return nil, err
	}
	defer provider.Close()
	mutator, ok := groupware.Supports[email.FlagMutator](provider)
	if !ok {
		return nil, fmt.Errorf("flag mutation is not supported for provider %s", account.Provider)
	}
	ids := mailMessageIDsArg(args)
	if len(ids) == 0 {
		return nil, fmt.Errorf("message_ids is required")
	}
	count, err := mutator.ClearFlag(context.Background(), ids)
	if err != nil {
		return nil, err
	}
	bindingRefs := s.mailBindingAffectedRefs(context.Background(), account, provider, ids)
	return withAffected(
		map[string]interface{}{"account": account, "succeeded": count, "message_ids": ids},
		append(mailMessageAffectedRefs(account, ids, nil), bindingRefs...)...,
	), nil
}

func (s *Server) mailCategoriesSet(args map[string]interface{}) (map[string]interface{}, error) {
	account, provider, err := s.mailProviderForTool(args)
	if err != nil {
		return nil, err
	}
	defer provider.Close()
	mutator, ok := groupware.Supports[email.CategoryMutator](provider)
	if !ok {
		return nil, fmt.Errorf("category mutation is not supported for provider %s", account.Provider)
	}
	ids := mailMessageIDsArg(args)
	if len(ids) == 0 {
		return nil, fmt.Errorf("message_ids is required")
	}
	categories, err := mailCategoriesArg(args)
	if err != nil {
		return nil, err
	}
	count, err := mutator.SetCategories(context.Background(), ids, categories)
	if err != nil {
		return nil, err
	}
	bindingRefs := s.mailBindingAffectedRefs(context.Background(), account, provider, ids)
	return withAffected(
		map[string]interface{}{"account": account, "succeeded": count, "message_ids": ids, "categories": categories},
		append(mailMessageAffectedRefs(account, ids, nil), bindingRefs...)...,
	), nil
}

func normalizeFlagStatus(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", strings.ToLower(email.FlagStatusFlagged):
		return email.FlagStatusFlagged, nil
	case strings.ToLower(email.FlagStatusNotFlagged):
		return email.FlagStatusNotFlagged, nil
	case strings.ToLower(email.FlagStatusComplete):
		return email.FlagStatusComplete, nil
	}
	return "", fmt.Errorf("status %q must be one of flagged, complete, notFlagged", raw)
}

func parseFlagDueAt(raw string) (time.Time, error) {
	clean := strings.TrimSpace(raw)
	layouts := []string{ // parseFlagDueAt accepts RFC3339 as well as the shorter date-only and
		// minute-precision forms used by the rest of the mail tool surface.
		time.RFC3339, "2006-01-02T15:04", "2006-01-02 15:04", "2006-01-02"}
	for _, layout := range layouts {
		if value, err := time.Parse(layout, clean); err == nil {
			return value.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("due_at %q must be RFC3339, YYYY-MM-DDTHH:MM, YYYY-MM-DD HH:MM, or YYYY-MM-DD", raw)
}

func mailCategoriesArg(args map[string]interface{}) ([]string, error) {
	values := make([]string, 0)
	if raw, ok := args["categories"].([]interface{}); ok {
		for _, item := range raw {
			text, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("categories must be a list of strings")
			}
			values = append(values, text)
		}
	}
	if raw, ok := args["categories"].([]string); ok {
		values = append(values, raw...)
	}
	cleaned := make([]string, 0, len(values))
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
		cleaned = append(cleaned, clean)
	}
	return cleaned, nil
}

func (s *Server) callMailboxSettingsTool(name string, args map[string]interface{}) (map[string]interface{}, error) {
	switch name {
	case "mail_oof_get":
		return s.mailOOFGet(args)
	case "mail_oof_set":
		return s.mailOOFSet(args)
	case "mail_delegate_list":
		return s.mailDelegateList(args)
	}
	return nil, fmt.Errorf("unknown mailbox-settings tool: %s", name)
}

func (s *Server) mailboxSettingsProviderForTool(args map[string]interface{}) (store.ExternalAccount, mailboxsettings.OOFProvider, error) {
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
	if !account.Enabled {
		return store.ExternalAccount{}, nil, fmt.Errorf("account %d is disabled", accountID)
	}
	if s.newMailboxSettingsProvider != nil {
		provider, err := s.newMailboxSettingsProvider(context.Background(), account)
		if err != nil {
			return store.ExternalAccount{}, nil, err
		}
		return account, provider, nil
	}
	if s.groupware == nil {
		return store.ExternalAccount{}, nil, fmt.Errorf("groupware registry is not configured")
	}
	provider, err := s.groupware.MailboxSettingsFor(context.Background(), accountID)
	if err != nil {
		return store.ExternalAccount{}, nil, err
	}
	return account, provider, nil
}

func (s *Server) mailOOFGet(args map[string]interface{}) (map[string]interface{}, error) {
	account, provider, err := s.mailboxSettingsProviderForTool(args)
	if err != nil {
		return nil, err
	}
	defer provider.Close()
	settings, err := provider.GetOOF(context.Background())
	if err != nil {
		if errors.Is(err, mailboxsettings.ErrUnsupported) {
			return map[string]interface{}{"account_id": account.ID, "provider": provider.ProviderName(), "error_code": "capability_unsupported", "error_detail": err.Error()}, nil
		}
		return nil, err
	}
	return map[string]interface{}{"account_id": account.ID, "provider": provider.ProviderName(), "settings": oofSettingsToMap(settings)}, nil
}

func (s *Server) mailOOFSet(args map[string]interface{}) (map[string]interface{}, error) {
	account, provider, err := s.mailboxSettingsProviderForTool(args)
	if err != nil {
		return nil, err
	}
	defer provider.Close()
	settings, err := oofSettingsFromArgs(args)
	if err != nil {
		return nil, err
	}
	if err := provider.SetOOF(context.Background(), settings); err != nil {
		if errors.Is(err, mailboxsettings.ErrUnsupported) {
			return map[string]interface{}{"account_id": account.ID, "provider": provider.ProviderName(), "error_code": "capability_unsupported", "error_detail": err.Error()}, nil
		}
		return nil, err
	}
	return map[string]interface{}{"account_id": account.ID, "provider": provider.ProviderName(), "set": true, "settings": oofSettingsToMap(settings)}, nil
}

func (s *Server) mailDelegateList(args map[string]interface{}) (map[string]interface{}, error) {
	account, provider, err := s.mailboxSettingsProviderForTool(args)
	if err != nil {
		return nil, err
	}
	defer provider.Close()
	delegator, ok := groupware.Supports[mailboxsettings.DelegationProvider](provider)
	if !ok {
		return map[string]interface{}{"account_id": account.ID, "provider": provider.ProviderName(), "error_code": "capability_unsupported", "capability": "mailboxsettings.DelegationProvider", "error_detail": fmt.Sprintf("provider %s does not expose mailbox delegation", provider.ProviderName())}, nil
	}
	ctx := context.Background()
	delegates, err := delegator.ListDelegates(ctx)
	if err != nil {
		if errors.Is(err, mailboxsettings.ErrUnsupported) {
			return map[string]interface{}{"account_id": account.ID, "provider": provider.ProviderName(), "error_code": "capability_unsupported", "capability": "mailboxsettings.DelegationProvider", "error_detail": err.Error()}, nil
		}
		return nil, err
	}
	shared, err := delegator.ListSharedMailboxes(ctx)
	if err != nil {
		if errors.Is(err, mailboxsettings.ErrUnsupported) {
			return map[string]interface{}{"account_id": account.ID, "provider": provider.ProviderName(), "error_code": "capability_unsupported", "capability": "mailboxsettings.DelegationProvider", "error_detail": err.Error()}, nil
		}
		return nil, err
	}
	delegatePayload := make([]map[string]interface{}, 0, len(delegates))
	for _, d := range delegates {
		delegatePayload = append(delegatePayload, map[string]interface{}{"email": d.Email, "name": d.Name, "permissions": d.Permissions})
	}
	sharedPayload := make([]map[string]interface{}, 0, len(shared))
	for _, m := range shared {
		sharedPayload = append(sharedPayload, map[string]interface{}{"email": m.Email, "name": m.Name, "access_level": m.AccessLevel})
	}
	return map[string]interface{}{"account_id": account.ID, "provider": provider.ProviderName(), "delegates": delegatePayload, "shared_mailboxes": sharedPayload}, nil
}

func oofSettingsToMap(settings providerdata.OOFSettings) map[string]interface{} {
	out := map[string]interface{}{"enabled": settings.Enabled, "scope": settings.Scope, "internal_reply": settings.InternalReply, "external_reply": settings.ExternalReply}
	if settings.StartAt != nil {
		out["start_at"] = settings.StartAt.UTC().Format(time.RFC3339)
	}
	if settings.EndAt != nil {
		out["end_at"] = settings.EndAt.UTC().Format(time.RFC3339)
	}
	return out
}

func oofSettingsFromArgs(args map[string]interface{}) (providerdata.OOFSettings, error) {
	raw, ok := args["settings"].(map[string]interface{})
	if !ok {
		return providerdata.OOFSettings{}, fmt.Errorf("settings is required")
	}
	out := providerdata.OOFSettings{Enabled: boolArgOf(raw, "enabled"), Scope: strings.TrimSpace(strArgOf(raw, "scope")), InternalReply: strArgOf(raw, "internal_reply"), ExternalReply: strArgOf(raw, "external_reply")}
	if start, err := optionalTime(raw, "start_at"); err != nil {
		return providerdata.OOFSettings{}, err
	} else if start != nil {
		out.StartAt = start
	}
	if end, err := optionalTime(raw, "end_at"); err != nil {
		return providerdata.OOFSettings{}, err
	} else if end != nil {
		out.EndAt = end
	}
	return out, nil
}

func strArgOf(m map[string]interface{}, key string) string {
	s, _ := m[key].(string)
	return s
}

func boolArgOf(m map[string]interface{}, key string) bool {
	b, _ := m[key].(bool)
	return b
}

func optionalTime(m map[string]interface{}, key string) (*time.Time, error) {
	raw, ok := m[key].(string)
	if !ok || strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04", "2006-01-02 15:04", "2006-01-02"} {
		if t, err := time.Parse(layout, raw); err == nil {
			return &t, nil
		}
	}
	return nil, fmt.Errorf("%s must be RFC3339, YYYY-MM-DDTHH:MM, YYYY-MM-DD HH:MM, or YYYY-MM-DD", key)
}
