package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
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
	case cleanKey == "legacy_helpy_env_var":
		return fmt.Errorf("external account config cannot store %s", fullKey)
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

func scanExternalAccount(row interface{ Scan(dest ...any) error }) (ExternalAccount, error) {
	var out ExternalAccount
	var enabled int
	if err := row.Scan(&out.ID, &out.Sphere, &out.Provider, &out.AccountName, &out.ConfigJSON, &enabled, &out.CreatedAt, &out.UpdatedAt); err != nil {
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
	rows, err := s.db.Query(`SELECT id, `+scopedContextSelect("context_external_accounts", "account_id", "external_accounts.id")+` AS sphere, provider, label AS account_name, config_json, enabled, created_at, updated_at
		 FROM external_accounts
		 WHERE provider = ?
		 ORDER BY lower(account_name), id`, cleanProvider)
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
	row := s.db.QueryRow(`SELECT id, `+scopedContextSelect("context_external_accounts", "account_id", "external_accounts.id")+` AS sphere, provider, label AS account_name, config_json, enabled, created_at, updated_at
		 FROM external_accounts
		 WHERE id = ?`, id)
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
	res, err := s.db.Exec(`INSERT INTO external_accounts (provider, label, config_json, enabled)
		 VALUES (?, ?, ?, 1)`, cleanProvider, cleanAccountName, configJSON)
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
	res, err := s.db.Exec(`UPDATE external_accounts
		 SET provider = ?, label = ?, config_json = ?, enabled = ?, updated_at = datetime('now')
		 WHERE id = ?`, provider, accountName, configJSON, boolToInt(enabled), id)
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
	return fmt.Sprintf("SLOPPY_%s_PASSWORD_%s", sanitizeExternalAccountEnvSegment(provider), sanitizeExternalAccountEnvSegment(accountName))
}

func ExternalAccountTokenPath(configDir, provider, accountName string) string {
	base := strings.TrimSpace(configDir)
	fileName := strings.ToLower(sanitizeExternalAccountEnvSegment(provider)+"_"+sanitizeExternalAccountEnvSegment(accountName)) + ".json"
	return filepath.Join(base, "tokens", fileName)
}

func normalizeExternalBindingObjectType(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func normalizeExternalBindingRemoteID(raw string) string {
	return strings.TrimSpace(raw)
}

func scanExternalBinding(row interface{ Scan(dest ...any) error }) (ExternalBinding, error) {
	var (
		out                           ExternalBinding
		itemID, artifactID            sql.NullInt64
		containerRef, remoteUpdatedAt sql.NullString
	)
	if err := row.Scan(&out.ID, &out.AccountID, &out.Provider, &out.ObjectType, &out.RemoteID, &itemID, &artifactID, &containerRef, &remoteUpdatedAt, &out.LastSyncedAt); err != nil {
		return ExternalBinding{}, err
	}
	out.Provider = normalizeExternalAccountProvider(out.Provider)
	out.ObjectType = normalizeExternalBindingObjectType(out.ObjectType)
	out.RemoteID = normalizeExternalBindingRemoteID(out.RemoteID)
	out.ItemID = nullInt64Pointer(itemID)
	out.ArtifactID = nullInt64Pointer(artifactID)
	out.ContainerRef = nullStringPointer(containerRef)
	out.RemoteUpdatedAt = nullStringPointer(remoteUpdatedAt)
	out.LastSyncedAt = strings.TrimSpace(out.LastSyncedAt)
	return out, nil
}

func (s *Store) validateExternalBindingAccount(accountID int64, provider string) (ExternalAccount, error) {
	if accountID <= 0 {
		return ExternalAccount{}, errors.New("external binding account_id is required")
	}
	cleanProvider := normalizeExternalAccountProvider(provider)
	if cleanProvider == "" {
		return ExternalAccount{}, errors.New("external binding provider is required")
	}
	account, err := s.GetExternalAccount(accountID)
	if err != nil {
		return ExternalAccount{}, err
	}
	if account.Provider != cleanProvider {
		return ExternalAccount{}, errors.New("external binding provider must match account provider")
	}
	return account, nil
}

func (s *Store) UpsertExternalBinding(binding ExternalBinding) (ExternalBinding, error) {
	account, err := s.validateExternalBindingAccount(binding.AccountID, binding.Provider)
	if err != nil {
		return ExternalBinding{}, err
	}
	objectType := normalizeExternalBindingObjectType(binding.ObjectType)
	if objectType == "" {
		return ExternalBinding{}, errors.New("external binding object_type is required")
	}
	remoteID := normalizeExternalBindingRemoteID(binding.RemoteID)
	if remoteID == "" {
		return ExternalBinding{}, errors.New("external binding remote_id is required")
	}
	remoteUpdatedAt, err := normalizeOptionalRFC3339String(binding.RemoteUpdatedAt)
	if err != nil {
		return ExternalBinding{}, errors.New("external binding remote_updated_at must be a valid RFC3339 timestamp")
	}
	lastSyncedAt := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := s.db.Exec(`INSERT INTO external_bindings (
			account_id, provider, object_type, remote_id, item_id, artifact_id, container_ref, remote_updated_at, last_synced_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(account_id, provider, object_type, remote_id) DO UPDATE SET
			item_id = excluded.item_id,
			artifact_id = excluded.artifact_id,
			container_ref = excluded.container_ref,
			remote_updated_at = excluded.remote_updated_at,
			last_synced_at = excluded.last_synced_at`, account.ID, account.Provider, objectType, remoteID, nullablePositiveID(valueOrZero(binding.ItemID)), nullablePositiveID(valueOrZero(binding.ArtifactID)), normalizeOptionalString(binding.ContainerRef), remoteUpdatedAt, lastSyncedAt); err != nil {
		return ExternalBinding{}, err
	}
	return s.GetBindingByRemote(account.ID, account.Provider, objectType, remoteID)
}

func (s *Store) GetBindingByRemote(accountID int64, provider, objectType, remoteID string) (ExternalBinding, error) {
	if _, err := s.validateExternalBindingAccount(accountID, provider); err != nil {
		return ExternalBinding{}, err
	}
	cleanObjectType := normalizeExternalBindingObjectType(objectType)
	if cleanObjectType == "" {
		return ExternalBinding{}, errors.New("external binding object_type is required")
	}
	cleanRemoteID := normalizeExternalBindingRemoteID(remoteID)
	if cleanRemoteID == "" {
		return ExternalBinding{}, errors.New("external binding remote_id is required")
	}
	return scanExternalBinding(s.db.QueryRow(`SELECT id, account_id, provider, object_type, remote_id, item_id, artifact_id, container_ref, remote_updated_at, last_synced_at
		 FROM external_bindings
		 WHERE account_id = ? AND provider = ? AND object_type = ? AND remote_id = ?`, accountID, normalizeExternalAccountProvider(provider), cleanObjectType, cleanRemoteID))
}

func (s *Store) GetBindingsByItem(itemID int64) ([]ExternalBinding, error) {
	rows, err := s.db.Query(`SELECT id, account_id, provider, object_type, remote_id, item_id, artifact_id, container_ref, remote_updated_at, last_synced_at
		 FROM external_bindings
		 WHERE item_id = ?
		 ORDER BY lower(provider), lower(object_type), remote_id, id`, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanExternalBindingRows(rows)
}

func (s *Store) GetBindingsByArtifact(artifactID int64) ([]ExternalBinding, error) {
	rows, err := s.db.Query(`SELECT id, account_id, provider, object_type, remote_id, item_id, artifact_id, container_ref, remote_updated_at, last_synced_at
		 FROM external_bindings
		 WHERE artifact_id = ?
		 ORDER BY lower(provider), lower(object_type), remote_id, id`, artifactID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanExternalBindingRows(rows)
}

func (s *Store) ListBindingsByAccount(accountID int64, provider, objectType string) ([]ExternalBinding, error) {
	if _, err := s.validateExternalBindingAccount(accountID, provider); err != nil {
		return nil, err
	}
	cleanObjectType := normalizeExternalBindingObjectType(objectType)
	if cleanObjectType == "" {
		return nil, errors.New("external binding object_type is required")
	}
	rows, err := s.db.Query(`SELECT id, account_id, provider, object_type, remote_id, item_id, artifact_id, container_ref, remote_updated_at, last_synced_at
		 FROM external_bindings
		 WHERE account_id = ? AND provider = ? AND object_type = ?
		 ORDER BY remote_id, id`, accountID, normalizeExternalAccountProvider(provider), cleanObjectType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanExternalBindingRows(rows)
}

func (s *Store) LatestBindingRemoteUpdatedAt(accountID int64, provider, objectType string) (*string, error) {
	if _, err := s.validateExternalBindingAccount(accountID, provider); err != nil {
		return nil, err
	}
	cleanObjectType := normalizeExternalBindingObjectType(objectType)
	if cleanObjectType == "" {
		return nil, errors.New("external binding object_type is required")
	}
	var value sql.NullString
	err := s.db.QueryRow(`SELECT remote_updated_at
		 FROM external_bindings
		 WHERE account_id = ? AND provider = ? AND object_type = ? AND remote_updated_at IS NOT NULL
		 ORDER BY datetime(remote_updated_at) DESC, id DESC
		 LIMIT 1`, accountID, normalizeExternalAccountProvider(provider), cleanObjectType).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !value.Valid {
		return nil, nil
	}
	clean := strings.TrimSpace(value.String)
	if clean == "" {
		return nil, nil
	}
	return &clean, nil
}
