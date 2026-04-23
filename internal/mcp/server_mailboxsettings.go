package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/mailboxsettings"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/store"
)

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
			return map[string]interface{}{
				"account_id":   account.ID,
				"provider":     provider.ProviderName(),
				"error_code":   "capability_unsupported",
				"error_detail": err.Error(),
			}, nil
		}
		return nil, err
	}
	return map[string]interface{}{
		"account_id": account.ID,
		"provider":   provider.ProviderName(),
		"settings":   oofSettingsToMap(settings),
	}, nil
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
			return map[string]interface{}{
				"account_id":   account.ID,
				"provider":     provider.ProviderName(),
				"error_code":   "capability_unsupported",
				"error_detail": err.Error(),
			}, nil
		}
		return nil, err
	}
	return map[string]interface{}{
		"account_id": account.ID,
		"provider":   provider.ProviderName(),
		"set":        true,
		"settings":   oofSettingsToMap(settings),
	}, nil
}

func oofSettingsToMap(settings providerdata.OOFSettings) map[string]interface{} {
	out := map[string]interface{}{
		"enabled":        settings.Enabled,
		"scope":          settings.Scope,
		"internal_reply": settings.InternalReply,
		"external_reply": settings.ExternalReply,
	}
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
	out := providerdata.OOFSettings{
		Enabled:       boolArgOf(raw, "enabled"),
		Scope:         strings.TrimSpace(strArgOf(raw, "scope")),
		InternalReply: strArgOf(raw, "internal_reply"),
		ExternalReply: strArgOf(raw, "external_reply"),
	}
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
