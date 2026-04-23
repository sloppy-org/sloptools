package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/email"
	"github.com/sloppy-org/sloptools/internal/groupware"
)

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
			return map[string]interface{}{
				"account":      account,
				"succeeded":    0,
				"message_ids":  ids,
				"status":       status,
				"error_code":   "capability_unsupported",
				"error_detail": err.Error(),
			}, nil
		}
		return nil, err
	}
	return map[string]interface{}{
		"account":     account,
		"succeeded":   count,
		"message_ids": ids,
		"status":      status,
	}, nil
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
	return map[string]interface{}{
		"account":     account,
		"succeeded":   count,
		"message_ids": ids,
	}, nil
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
	return map[string]interface{}{
		"account":     account,
		"succeeded":   count,
		"message_ids": ids,
		"categories":  categories,
	}, nil
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

// parseFlagDueAt accepts RFC3339 as well as the shorter date-only and
// minute-precision forms used by the rest of the mail tool surface.
func parseFlagDueAt(raw string) (time.Time, error) {
	clean := strings.TrimSpace(raw)
	layouts := []string{time.RFC3339, "2006-01-02T15:04", "2006-01-02 15:04", "2006-01-02"}
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
