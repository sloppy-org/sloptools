package email

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	defaultExchangeGraphBaseURL = "https://graph.microsoft.com"
	defaultExchangeAuthBaseURL  = "https://login.microsoftonline.com"
)

var errExchangeSecretMissing = errors.New("exchange client secret is not configured")

type ExchangeConfig struct {
	Label       string
	ClientID    string
	TenantID    string
	Scopes      []string
	BaseURL     string
	AuthBaseURL string
	ConfigDir   string
	TokenPath   string
}

type ExchangeOption func(*ExchangeClient)

type DeviceCodePrompt func(DeviceCodeInfo) error

type DeviceCodeInfo struct {
	UserCode        string
	VerificationURI string
	VerificationURL string
	Message         string
	ExpiresIn       time.Duration
	Interval        time.Duration
}

type ExchangeClient struct {
	httpClient *http.Client
	cfg        ExchangeConfig
	lookupEnv  func(string) (string, bool)
	now        func() time.Time
	sleep      func(context.Context, time.Duration) error
	prompt     DeviceCodePrompt

	mu          sync.Mutex
	token       exchangeToken
	tokenLoaded bool
}

type exchangeToken struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type exchangeTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
}

type exchangeDeviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	VerificationURL string `json:"verification_url"`
	Message         string `json:"message"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

type exchangeGraphError struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func WithExchangeHTTPClient(client *http.Client) ExchangeOption {
	return func(c *ExchangeClient) {
		if client != nil {
			c.httpClient = client
		}
	}
}

func WithExchangeEnvLookup(lookup func(string) (string, bool)) ExchangeOption {
	return func(c *ExchangeClient) {
		if lookup != nil {
			c.lookupEnv = lookup
		}
	}
}

func WithExchangeClock(now func() time.Time) ExchangeOption {
	return func(c *ExchangeClient) {
		if now != nil {
			c.now = now
		}
	}
}

func WithExchangeSleep(sleep func(context.Context, time.Duration) error) ExchangeOption {
	return func(c *ExchangeClient) {
		if sleep != nil {
			c.sleep = sleep
		}
	}
}

func WithExchangeDeviceCodePrompt(prompt DeviceCodePrompt) ExchangeOption {
	return func(c *ExchangeClient) {
		c.prompt = prompt
	}
}

func ExchangeConfigFromMap(label string, config map[string]any) (ExchangeConfig, error) {
	cfg := ExchangeConfig{Label: strings.TrimSpace(label)}
	if raw, ok := config["client_id"].(string); ok {
		cfg.ClientID = strings.TrimSpace(raw)
	}
	if raw, ok := config["tenant_id"].(string); ok {
		cfg.TenantID = strings.TrimSpace(raw)
	}
	if raw, ok := config["base_url"].(string); ok {
		cfg.BaseURL = strings.TrimSpace(raw)
	}
	if raw, ok := config["auth_base_url"].(string); ok {
		cfg.AuthBaseURL = strings.TrimSpace(raw)
	}
	if raw, ok := config["config_dir"].(string); ok {
		cfg.ConfigDir = strings.TrimSpace(raw)
	}
	if raw, ok := config["token_path"].(string); ok {
		cfg.TokenPath = strings.TrimSpace(raw)
	}
	switch raw := config["scopes"].(type) {
	case []string:
		cfg.Scopes = append(cfg.Scopes, raw...)
	case []any:
		for _, value := range raw {
			text, ok := value.(string)
			if ok && strings.TrimSpace(text) != "" {
				cfg.Scopes = append(cfg.Scopes, text)
			}
		}
	}
	return cfg, nil
}

func NewExchangeClient(cfg ExchangeConfig, opts ...ExchangeOption) (*ExchangeClient, error) {
	cfg = normalizeExchangeConfig(cfg)
	if err := validateExchangeConfig(cfg); err != nil {
		return nil, err
	}
	client := &ExchangeClient{
		httpClient: http.DefaultClient,
		cfg:        cfg,
		lookupEnv:  os.LookupEnv,
		now:        time.Now,
		sleep:      sleepContext,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(client)
		}
	}
	return client, nil
}

func normalizeExchangeConfig(cfg ExchangeConfig) ExchangeConfig {
	cfg.Label = strings.TrimSpace(cfg.Label)
	cfg.ClientID = strings.TrimSpace(cfg.ClientID)
	cfg.TenantID = strings.TrimSpace(cfg.TenantID)
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	cfg.AuthBaseURL = strings.TrimRight(strings.TrimSpace(cfg.AuthBaseURL), "/")
	cfg.ConfigDir = strings.TrimSpace(cfg.ConfigDir)
	cfg.TokenPath = strings.TrimSpace(cfg.TokenPath)
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultExchangeGraphBaseURL
	}
	if cfg.AuthBaseURL == "" {
		cfg.AuthBaseURL = defaultExchangeAuthBaseURL
	}
	if cfg.ConfigDir == "" {
		cfg.ConfigDir = defaultEmailConfigDir()
	}
	if cfg.TokenPath == "" {
		cfg.TokenPath = ExchangeTokenPath(cfg.ConfigDir, cfg.Label)
	}
	if len(cfg.Scopes) == 0 {
		cfg.Scopes = []string{"offline_access", "Mail.ReadWrite", "Contacts.Read"}
	}
	cleanScopes := make([]string, 0, len(cfg.Scopes))
	for _, scope := range cfg.Scopes {
		scope = strings.TrimSpace(scope)
		if scope != "" {
			cleanScopes = append(cleanScopes, scope)
		}
	}
	cfg.Scopes = cleanScopes
	return cfg
}

func validateExchangeConfig(cfg ExchangeConfig) error {
	if cfg.Label == "" {
		return errors.New("exchange label is required")
	}
	if cfg.ClientID == "" {
		return errors.New("exchange client_id is required")
	}
	if cfg.TenantID == "" {
		return errors.New("exchange tenant_id is required")
	}
	if len(cfg.Scopes) == 0 {
		return errors.New("exchange scopes are required")
	}
	return nil
}

func ExchangeSecretEnvVar(label string) string {
	return "SLOPPY_EXCHANGE_SECRET_" + sanitizeExchangeEnvSegment(label)
}

func ExchangeTokenPath(configDir, label string) string {
	name := "exchange_" + strings.ToLower(sanitizeExchangeEnvSegment(label)) + ".json"
	return filepath.Join(strings.TrimSpace(configDir), "tokens", name)
}

func (c *ExchangeClient) ListFolders(ctx context.Context) ([]Folder, error) {
	var resp struct {
		Value []Folder `json:"value"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/v1.0/me/mailFolders", nil, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Value, nil
}

func (c *ExchangeClient) ListMessages(ctx context.Context, opts ListMessageOptions) ([]Message, error) {
	path := "/v1.0/me/messages"
	if folderID := strings.TrimSpace(opts.FolderID); folderID != "" {
		path = "/v1.0/me/mailFolders/" + url.PathEscape(folderID) + "/messages"
	}
	query := url.Values{}
	if opts.Top > 0 {
		query.Set("$top", strconv.Itoa(opts.Top))
	}
	if opts.Skip > 0 {
		query.Set("$skip", strconv.Itoa(opts.Skip))
	}
	if filter := strings.TrimSpace(opts.Filter); filter != "" {
		query.Set("$filter", filter)
	}
	if len(opts.Select) > 0 {
		fields := make([]string, 0, len(opts.Select))
		for _, field := range opts.Select {
			field = strings.TrimSpace(field)
			if field != "" {
				fields = append(fields, field)
			}
		}
		if len(fields) > 0 {
			query.Set("$select", strings.Join(fields, ","))
		}
	}
	var resp struct {
		Value []Message `json:"value"`
	}
	if err := c.doJSON(ctx, http.MethodGet, path, query, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Value, nil
}

func (c *ExchangeClient) GetMessage(ctx context.Context, messageID string) (Message, error) {
	var message Message
	if err := c.doJSON(ctx, http.MethodGet, "/v1.0/me/messages/"+url.PathEscape(strings.TrimSpace(messageID)), nil, nil, &message); err != nil {
		return Message{}, err
	}
	return message, nil
}

func (c *ExchangeClient) MoveMessage(ctx context.Context, messageID, destinationID string) error {
	body := map[string]string{"destinationId": strings.TrimSpace(destinationID)}
	return c.doJSON(ctx, http.MethodPost, "/v1.0/me/messages/"+url.PathEscape(strings.TrimSpace(messageID))+"/move", nil, body, nil)
}

func (c *ExchangeClient) ArchiveMessage(ctx context.Context, messageID string) error {
	return c.MoveMessage(ctx, messageID, "archive")
}

func (c *ExchangeClient) MoveMessageToInbox(ctx context.Context, messageID string) error {
	return c.MoveMessage(ctx, messageID, "inbox")
}

func (c *ExchangeClient) DeleteMessage(ctx context.Context, messageID string) error {
	return c.doJSON(ctx, http.MethodDelete, "/v1.0/me/messages/"+url.PathEscape(strings.TrimSpace(messageID)), nil, nil, nil)
}

func (c *ExchangeClient) MarkRead(ctx context.Context, messageID string) error {
	return c.setReadState(ctx, messageID, true)
}

func (c *ExchangeClient) MarkUnread(ctx context.Context, messageID string) error {
	return c.setReadState(ctx, messageID, false)
}

func (c *ExchangeClient) setReadState(ctx context.Context, messageID string, isRead bool) error {
	return c.doJSON(ctx, http.MethodPatch, "/v1.0/me/messages/"+url.PathEscape(strings.TrimSpace(messageID)), nil, map[string]bool{"isRead": isRead}, nil)
}

func (c *ExchangeClient) doJSON(ctx context.Context, method, path string, query url.Values, reqBody any, respBody any) error {
	token, err := c.ensureAccessToken(ctx)
	if err != nil {
		return err
	}
	fullURL := c.cfg.BaseURL + path
	if len(query) > 0 {
		fullURL += "?" + query.Encode()
	}
	var bodyReader *bytes.Reader
	if reqBody != nil {
		raw, err := json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("marshal exchange request: %w", err)
		}
		bodyReader = bytes.NewReader(raw)
	} else {
		bodyReader = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var graphErr exchangeGraphError
		if err := json.NewDecoder(resp.Body).Decode(&graphErr); err == nil && graphErr.Error.Message != "" {
			return fmt.Errorf("exchange graph %s: %s", graphErr.Error.Code, graphErr.Error.Message)
		}
		return fmt.Errorf("exchange graph http %d", resp.StatusCode)
	}
	if respBody == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(respBody); err != nil {
		return fmt.Errorf("decode exchange response: %w", err)
	}
	return nil
}

func (c *ExchangeClient) ensureAccessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.tokenLoaded {
		token, err := loadExchangeTokenFile(c.cfg.TokenPath)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		if err == nil {
			c.token = token
		}
		c.tokenLoaded = true
	}
	now := c.now()
	if c.token.AccessToken != "" && c.token.ExpiresAt.After(now.Add(30*time.Second)) {
		return c.token.AccessToken, nil
	}
	switch {
	case c.token.RefreshToken != "":
		token, err := c.refreshToken(ctx, c.token.RefreshToken)
		if err != nil {
			return "", err
		}
		c.token = token
	case c.prompt != nil:
		token, err := c.deviceCodeToken(ctx)
		if err != nil {
			return "", err
		}
		c.token = token
	default:
		return "", errors.New("exchange token cache is empty; device code prompt is not configured")
	}
	if err := saveExchangeTokenFile(c.cfg.TokenPath, c.token); err != nil {
		return "", err
	}
	return c.token.AccessToken, nil
}

func (c *ExchangeClient) refreshToken(ctx context.Context, refreshToken string) (exchangeToken, error) {
	secret, err := c.exchangeSecret()
	if err != nil {
		return exchangeToken{}, err
	}
	form := url.Values{
		"client_id":     {c.cfg.ClientID},
		"client_secret": {secret},
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"scope":         {strings.Join(c.cfg.Scopes, " ")},
	}
	return c.exchangeTokenRequest(ctx, form)
}

func (c *ExchangeClient) deviceCodeToken(ctx context.Context) (exchangeToken, error) {
	form := url.Values{
		"client_id": {c.cfg.ClientID},
		"scope":     {strings.Join(c.cfg.Scopes, " ")},
	}
	resp, err := c.postForm(ctx, c.deviceCodeURL(), form)
	if err != nil {
		return exchangeToken{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return exchangeToken{}, decodeExchangeHTTPError(resp)
	}
	var device exchangeDeviceCodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&device); err != nil {
		return exchangeToken{}, fmt.Errorf("decode exchange device code response: %w", err)
	}
	if err := c.prompt(DeviceCodeInfo{
		UserCode:        device.UserCode,
		VerificationURI: device.VerificationURI,
		VerificationURL: device.VerificationURL,
		Message:         device.Message,
		ExpiresIn:       time.Duration(device.ExpiresIn) * time.Second,
		Interval:        time.Duration(maxInt(device.Interval, 1)) * time.Second,
	}); err != nil {
		return exchangeToken{}, err
	}
	deadline := c.now().Add(time.Duration(device.ExpiresIn) * time.Second)
	wait := time.Duration(maxInt(device.Interval, 1)) * time.Second
	for c.now().Before(deadline) {
		if err := c.sleep(ctx, wait); err != nil {
			return exchangeToken{}, err
		}
		tokenForm := url.Values{
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
			"client_id":   {c.cfg.ClientID},
			"device_code": {device.DeviceCode},
		}
		if secret, err := c.exchangeSecret(); err == nil && secret != "" {
			tokenForm.Set("client_secret", secret)
		}
		token, err := c.exchangeTokenRequest(ctx, tokenForm)
		if err == nil {
			return token, nil
		}
		if shouldContinueExchangeDeviceCode(err) {
			continue
		}
		return exchangeToken{}, err
	}
	return exchangeToken{}, errors.New("exchange device code authorization expired")
}

func (c *ExchangeClient) exchangeTokenRequest(ctx context.Context, form url.Values) (exchangeToken, error) {
	resp, err := c.postForm(ctx, c.tokenURL(), form)
	if err != nil {
		return exchangeToken{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return exchangeToken{}, decodeExchangeHTTPError(resp)
	}
	var tokenResp exchangeTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return exchangeToken{}, fmt.Errorf("decode exchange token response: %w", err)
	}
	token := exchangeToken{
		AccessToken:  strings.TrimSpace(tokenResp.AccessToken),
		RefreshToken: strings.TrimSpace(tokenResp.RefreshToken),
		TokenType:    strings.TrimSpace(tokenResp.TokenType),
		ExpiresAt:    c.now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
	}
	if token.AccessToken == "" {
		return exchangeToken{}, errors.New("exchange token response missing access_token")
	}
	if token.RefreshToken == "" {
		token.RefreshToken = form.Get("refresh_token")
	}
	if token.TokenType == "" {
		token.TokenType = "Bearer"
	}
	return token, nil
}

func (c *ExchangeClient) postForm(ctx context.Context, fullURL string, form url.Values) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	return c.httpClient.Do(req)
}

func (c *ExchangeClient) deviceCodeURL() string {
	return c.cfg.AuthBaseURL + "/" + url.PathEscape(c.cfg.TenantID) + "/oauth2/v2.0/devicecode"
}

func (c *ExchangeClient) tokenURL() string {
	return c.cfg.AuthBaseURL + "/" + url.PathEscape(c.cfg.TenantID) + "/oauth2/v2.0/token"
}

func (c *ExchangeClient) exchangeSecret() (string, error) {
	value, ok := c.lookupEnv(ExchangeSecretEnvVar(c.cfg.Label))
	if !ok || strings.TrimSpace(value) == "" {
		return "", errExchangeSecretMissing
	}
	return strings.TrimSpace(value), nil
}

func loadExchangeTokenFile(path string) (exchangeToken, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return exchangeToken{}, err
	}
	var token exchangeToken
	if err := json.Unmarshal(raw, &token); err != nil {
		return exchangeToken{}, fmt.Errorf("decode exchange token file: %w", err)
	}
	return token, nil
}

func saveExchangeTokenFile(path string, token exchangeToken) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("exchange token path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal exchange token file: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "exchange-token-*.json")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func decodeExchangeHTTPError(resp *http.Response) error {
	var graphErr exchangeGraphError
	if err := json.NewDecoder(resp.Body).Decode(&graphErr); err == nil && graphErr.Error.Code != "" {
		return fmt.Errorf("exchange auth %s: %s", graphErr.Error.Code, graphErr.Error.Message)
	}
	return fmt.Errorf("exchange auth http %d", resp.StatusCode)
}

func shouldContinueExchangeDeviceCode(err error) bool {
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "authorization_pending") || strings.Contains(text, "slow_down")
}

func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func defaultEmailConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ".sloppy"
	}
	return filepath.Join(home, ".config", "sloppy")
}

func sanitizeExchangeEnvSegment(raw string) string {
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

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (c *ExchangeClient) Close() error {
	return nil
}
