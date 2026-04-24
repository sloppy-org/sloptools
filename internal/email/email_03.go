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
	"strings"
	"time"
	"unicode"
)

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
	form := url.Values{"client_id": {c.cfg.ClientID}, "client_secret": {secret}, "grant_type": {"refresh_token"}, "refresh_token": {refreshToken}, "scope": {strings.Join(c.cfg.Scopes, " ")}}
	return c.exchangeTokenRequest(ctx, form)
}

func (c *ExchangeClient) deviceCodeToken(ctx context.Context) (exchangeToken, error) {
	form := url.Values{"client_id": {c.cfg.ClientID}, "scope": {strings.Join(c.cfg.Scopes, " ")}}
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
	if err := c.prompt(DeviceCodeInfo{UserCode: device.UserCode, VerificationURI: device.VerificationURI, VerificationURL: device.VerificationURL, Message: device.Message, ExpiresIn: time.Duration(device.ExpiresIn) * time.Second, Interval: time.Duration(maxInt(device.Interval, 1)) * time.Second}); err != nil {
		return exchangeToken{}, err
	}
	deadline := c.now().Add(time.Duration(device.ExpiresIn) * time.Second)
	wait := time.Duration(maxInt(device.Interval, 1)) * time.Second
	for c.now().Before(deadline) {
		if err := c.sleep(ctx, wait); err != nil {
			return exchangeToken{}, err
		}
		tokenForm := url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}, "client_id": {c.cfg.ClientID}, "device_code": {device.DeviceCode}}
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
	token := exchangeToken{AccessToken: strings.TrimSpace(tokenResp.AccessToken), RefreshToken: strings.TrimSpace(tokenResp.RefreshToken), TokenType: strings.TrimSpace(tokenResp.TokenType), ExpiresAt: c.now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)}
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

func (c *ExchangeClient) doAbsoluteJSON(ctx context.Context, method, fullURL string, reqBody any, respBody any) error {
	token, err := c.ensureAccessToken(ctx)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, fullURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
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

var _ DraftProvider = (*ExchangeMailProvider)(nil)

func (p *ExchangeMailProvider) CreateDraft(ctx context.Context, input DraftInput) (Draft, error) {
	if p == nil || p.client == nil {
		return Draft{}, fmt.Errorf("exchange provider is not configured")
	}
	normalized, err := NormalizeDraftInput(input)
	if err != nil {
		return Draft{}, err
	}
	req := exchangeDraftRequest(normalized)
	var message Message
	if err := p.client.doJSON(ctx, "POST", "/v1.0/me/messages", nil, req, &message); err != nil {
		return Draft{}, fmt.Errorf("exchange create draft: %w", err)
	}
	return Draft{ID: strings.TrimSpace(message.ID), ThreadID: strings.TrimSpace(message.ConversationID)}, nil
}
