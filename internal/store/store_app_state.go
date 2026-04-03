package store

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/krystophny/sloppy/internal/modelprofile"
)

func (s *Store) SetAppState(key, value string) error {
	cleanKey := strings.TrimSpace(key)
	if cleanKey == "" {
		return errors.New("app state key is required")
	}
	_, err := s.db.Exec(
		`INSERT INTO app_state (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		cleanKey,
		strings.TrimSpace(value),
	)
	return err
}

func (s *Store) AppState(key string) (string, error) {
	cleanKey := strings.TrimSpace(key)
	if cleanKey == "" {
		return "", errors.New("app state key is required")
	}
	var value string
	if err := s.db.QueryRow(`SELECT value FROM app_state WHERE key = ?`, cleanKey).Scan(&value); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(value), nil
}

func normalizeWorkspaceChatModel(raw string) string {
	alias := modelprofile.ResolveAlias(raw, modelprofile.AliasLocal)
	if alias == "" {
		return modelprofile.AliasLocal
	}
	return alias
}

func normalizeWorkspaceChatModelReasoningEffort(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", modelprofile.ReasoningNone:
		return modelprofile.ReasoningNone
	case modelprofile.ReasoningLow:
		return modelprofile.ReasoningLow
	case modelprofile.ReasoningMedium:
		return modelprofile.ReasoningMedium
	case modelprofile.ReasoningHigh:
		return modelprofile.ReasoningHigh
	case modelprofile.ReasoningExtraHigh, "extra_high":
		return modelprofile.ReasoningExtraHigh
	default:
		return ""
	}
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
