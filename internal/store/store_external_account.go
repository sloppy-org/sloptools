package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"unicode"
)

func normalizeExternalAccountSphere(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case SphereWork:
		return SphereWork
	case SpherePrivate:
		return SpherePrivate
	default:
		return ""
	}
}

func normalizeExternalAccountProvider(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case ExternalProviderGmail:
		return ExternalProviderGmail
	case ExternalProviderIMAP:
		return ExternalProviderIMAP
	case ExternalProviderGoogleCalendar:
		return ExternalProviderGoogleCalendar
	case ExternalProviderICS:
		return ExternalProviderICS
	case ExternalProviderTodoist:
		return ExternalProviderTodoist
	case ExternalProviderEvernote:
		return ExternalProviderEvernote
	case ExternalProviderBear:
		return ExternalProviderBear
	case ExternalProviderZotero:
		return ExternalProviderZotero
	case ExternalProviderExchange:
		return ExternalProviderExchange
	case ExternalProviderExchangeEWS:
		return ExternalProviderExchangeEWS
	default:
		return ""
	}
}

func normalizeExternalAccountName(raw string) string {
	return strings.TrimSpace(raw)
}

func normalizeExternalAccountConfig(config map[string]any) (string, error) {
	if config == nil {
		config = map[string]any{}
	}
	if err := validateExternalAccountConfigValue("", config); err != nil {
		return "", err
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("marshal external account config: %w", err)
	}
	return string(raw), nil
}

func validateExternalAccountConfigValue(path string, value any) error {
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			cleanKey := strings.TrimSpace(key)
			if cleanKey == "" {
				return errors.New("external account config keys must be non-empty")
			}
			if err := validateExternalAccountConfigKey(path, cleanKey); err != nil {
				return err
			}
			nextPath := cleanKey
			if path != "" {
				nextPath = path + "." + cleanKey
			}
			if err := validateExternalAccountConfigValue(nextPath, nested); err != nil {
				return err
			}
		}
	case []any:
		for i := range typed {
			if err := validateExternalAccountConfigValue(path, typed[i]); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateExternalAccountConfigKey(path, key string) error {
	cleanKey := strings.ToLower(strings.TrimSpace(key))
	fullKey := strings.TrimSpace(key)
	if path != "" {
		fullKey = path + "." + strings.TrimSpace(key)
	}
	switch {
	case strings.Contains(cleanKey, "password"):
		return fmt.Errorf("external account config cannot store %s", fullKey)
	case strings.Contains(cleanKey, "secret"):
		return fmt.Errorf("external account config cannot store %s", fullKey)
	case strings.Contains(cleanKey, "token") && !strings.Contains(cleanKey, "file") && !strings.Contains(cleanKey, "path"):
		return fmt.Errorf("external account config cannot store %s", fullKey)
	default:
		return nil
	}
}

func scanExternalAccount(
	row interface {
		Scan(dest ...any) error
	},
) (ExternalAccount, error) {
	var out ExternalAccount
	var enabled int
	if err := row.Scan(
		&out.ID,
		&out.Sphere,
		&out.Provider,
		&out.AccountName,
		&out.ConfigJSON,
		&enabled,
		&out.CreatedAt,
		&out.UpdatedAt,
	); err != nil {
		return ExternalAccount{}, err
	}
	out.Sphere = normalizeExternalAccountSphere(out.Sphere)
	out.Provider = normalizeExternalAccountProvider(out.Provider)
	out.AccountName = normalizeExternalAccountName(out.AccountName)
	out.Label = out.AccountName
	out.ConfigJSON = strings.TrimSpace(out.ConfigJSON)
	if out.ConfigJSON == "" {
		out.ConfigJSON = "{}"
	}
	out.Enabled = enabled != 0
	return out, nil
}

func (s *Store) ListExternalAccounts(sphere string) ([]ExternalAccount, error) {
	cleanSphere := strings.TrimSpace(sphere)
	query := `SELECT id, ` + scopedContextSelect("context_external_accounts", "account_id", "external_accounts.id") + ` AS sphere, provider, label AS account_name, config_json, enabled, created_at, updated_at
	 FROM external_accounts`
	args := []any{}
	if cleanSphere != "" {
		cleanSphere = normalizeExternalAccountSphere(cleanSphere)
		if cleanSphere == "" {
			return nil, errors.New("external account sphere is required")
		}
		query += ` WHERE ` + scopedContextFilter("context_external_accounts", "account_id", "external_accounts.id")
		args = append(args, cleanSphere)
	}
	query += ` ORDER BY lower(account_name), id`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ExternalAccount
	for rows.Next() {
		account, err := scanExternalAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, account)
	}
	return out, rows.Err()
}

func (s *Store) ListExternalAccountsByProvider(provider string) ([]ExternalAccount, error) {
	cleanProvider := normalizeExternalAccountProvider(provider)
	if cleanProvider == "" {
		return nil, errors.New("external account provider is required")
	}
	rows, err := s.db.Query(
		`SELECT id, `+scopedContextSelect("context_external_accounts", "account_id", "external_accounts.id")+` AS sphere, provider, label AS account_name, config_json, enabled, created_at, updated_at
		 FROM external_accounts
		 WHERE provider = ?
		 ORDER BY lower(account_name), id`,
		cleanProvider,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ExternalAccount
	for rows.Next() {
		account, err := scanExternalAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, account)
	}
	return out, rows.Err()
}

func (s *Store) GetExternalAccount(id int64) (ExternalAccount, error) {
	row := s.db.QueryRow(
		`SELECT id, `+scopedContextSelect("context_external_accounts", "account_id", "external_accounts.id")+` AS sphere, provider, label AS account_name, config_json, enabled, created_at, updated_at
		 FROM external_accounts
		 WHERE id = ?`,
		id,
	)
	return scanExternalAccount(row)
}

func (s *Store) CreateExternalAccount(sphere, provider, accountName string, config map[string]any) (ExternalAccount, error) {
	cleanSphere := normalizeExternalAccountSphere(sphere)
	if cleanSphere == "" {
		return ExternalAccount{}, errors.New("external account sphere is required")
	}
	cleanProvider := normalizeExternalAccountProvider(provider)
	if cleanProvider == "" {
		return ExternalAccount{}, errors.New("external account provider is required")
	}
	cleanAccountName := normalizeExternalAccountName(accountName)
	if cleanAccountName == "" {
		return ExternalAccount{}, errors.New("external account name is required")
	}
	configJSON, err := normalizeExternalAccountConfig(config)
	if err != nil {
		return ExternalAccount{}, err
	}
	res, err := s.db.Exec(
		`INSERT INTO external_accounts (provider, label, config_json, enabled)
		 VALUES (?, ?, ?, 1)`,
		cleanProvider,
		cleanAccountName,
		configJSON,
	)
	if err != nil {
		return ExternalAccount{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return ExternalAccount{}, err
	}
	if err := s.syncScopedContextLink("context_external_accounts", "account_id", id, cleanSphere); err != nil {
		return ExternalAccount{}, err
	}
	return s.GetExternalAccount(id)
}

func (s *Store) UpdateExternalAccount(id int64, update ExternalAccountUpdate) error {
	current, err := s.GetExternalAccount(id)
	if err != nil {
		return err
	}

	sphere := current.Sphere
	if update.Sphere != nil {
		sphere = normalizeExternalAccountSphere(*update.Sphere)
		if sphere == "" {
			return errors.New("external account sphere is required")
		}
	}

	provider := current.Provider
	if update.Provider != nil {
		provider = normalizeExternalAccountProvider(*update.Provider)
		if provider == "" {
			return errors.New("external account provider is required")
		}
	}

	accountName := current.AccountName
	if update.AccountName != nil {
		accountName = normalizeExternalAccountName(*update.AccountName)
		if accountName == "" {
			return errors.New("external account name is required")
		}
	}

	configJSON := current.ConfigJSON
	if update.Config != nil {
		configJSON, err = normalizeExternalAccountConfig(update.Config)
		if err != nil {
			return err
		}
	}

	enabled := current.Enabled
	if update.Enabled != nil {
		enabled = *update.Enabled
	}

	res, err := s.db.Exec(
		`UPDATE external_accounts
		 SET provider = ?, label = ?, config_json = ?, enabled = ?, updated_at = datetime('now')
		 WHERE id = ?`,
		provider,
		accountName,
		configJSON,
		boolToInt(enabled),
		id,
	)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return s.syncScopedContextLink("context_external_accounts", "account_id", id, sphere)
}

func (s *Store) DeleteExternalAccount(id int64) error {
	res, err := s.db.Exec(`DELETE FROM external_accounts WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func sanitizeExternalAccountEnvSegment(raw string) string {
	var b strings.Builder
	lastUnderscore := true
	for _, r := range strings.ToUpper(strings.TrimSpace(raw)) {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			b.WriteRune(r)
			lastUnderscore = false
		case !lastUnderscore:
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	clean := strings.Trim(b.String(), "_")
	if clean == "" {
		return "ACCOUNT"
	}
	return clean
}

func ExternalAccountPasswordEnvVar(provider, accountName string) string {
	return fmt.Sprintf(
		"SLOPSHELL_%s_PASSWORD_%s",
		sanitizeExternalAccountEnvSegment(provider),
		sanitizeExternalAccountEnvSegment(accountName),
	)
}

func ExternalAccountTokenPath(configDir, provider, accountName string) string {
	base := strings.TrimSpace(configDir)
	fileName := strings.ToLower(
		sanitizeExternalAccountEnvSegment(provider)+"_"+sanitizeExternalAccountEnvSegment(accountName),
	) + ".json"
	return filepath.Join(base, "tokens", fileName)
}
