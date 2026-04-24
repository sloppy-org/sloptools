package email

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

func buildRFC822Message(input DraftInput) ([]byte, error) {
	normalized, err := NormalizeDraftInput(input)
	if err != nil {
		return nil, err
	}
	var msg bytes.Buffer
	if normalized.From != "" {
		from := normalized.From
		if name := strings.TrimSpace(input.From); name != "" && !strings.EqualFold(name, normalized.From) {
			from = (&mail.Address{Name: name, Address: normalized.From}).String()
		}
		fmt.Fprintf(&msg, "From: %s\r\n", from)
	}
	if len(normalized.To) > 0 {
		fmt.Fprintf(&msg, "To: %s\r\n", formatDraftHeaderAddresses(normalized.To))
	}
	if len(normalized.Cc) > 0 {
		fmt.Fprintf(&msg, "Cc: %s\r\n", formatDraftHeaderAddresses(normalized.Cc))
	}
	if normalized.Subject != "" {
		fmt.Fprintf(&msg, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", normalized.Subject))
	}
	fmt.Fprintf(&msg, "Date: %s\r\n", time.Now().UTC().Format(time.RFC1123Z))
	fmt.Fprintf(&msg, "MIME-Version: 1.0\r\n")
	if normalized.InReplyTo != "" {
		fmt.Fprintf(&msg, "In-Reply-To: %s\r\n", normalized.InReplyTo)
	}
	if len(normalized.References) > 0 {
		fmt.Fprintf(&msg, "References: %s\r\n", strings.Join(normalized.References, " "))
	}
	body := withTrailingNewline(normalized.Body)
	if len(normalized.Attachments) == 0 {
		fmt.Fprintf(&msg, "Content-Type: text/plain; charset=UTF-8\r\n")
		fmt.Fprintf(&msg, "Content-Transfer-Encoding: 8bit\r\n")
		msg.WriteString("\r\n")
		msg.WriteString(strings.ReplaceAll(body, "\n", "\r\n"))
		return msg.Bytes(), nil
	}
	boundary, err := newMIMEBoundary()
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(&msg, "Content-Type: multipart/mixed; boundary=%q\r\n", boundary)
	msg.WriteString("\r\n")
	msg.WriteString("This is a multi-part message in MIME format.\r\n")
	fmt.Fprintf(&msg, "--%s\r\n", boundary)
	msg.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	msg.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(strings.ReplaceAll(body, "\n", "\r\n"))
	for _, att := range normalized.Attachments {
		fmt.Fprintf(&msg, "--%s\r\n", boundary)
		fmt.Fprintf(&msg, "Content-Type: %s\r\n", att.ContentType)
		fmt.Fprintf(&msg, "Content-Disposition: attachment; filename=%s\r\n", mime.BEncoding.Encode("utf-8", att.Filename))
		msg.WriteString("Content-Transfer-Encoding: base64\r\n")
		msg.WriteString("\r\n")
		msg.WriteString(wrapBase64(att.Content, 76))
	}
	fmt.Fprintf(&msg, "--%s--\r\n", boundary)
	return msg.Bytes(), nil
}

func withTrailingNewline(body string) string {
	if strings.HasSuffix(body, "\n") {
		return body
	}
	return body + "\n"
}

func newMIMEBoundary() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return "sloptools-" + hex.EncodeToString(buf[:]), nil
}

func wrapBase64(data []byte, width int) string {
	if width <= 0 {
		width = 76
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	var b strings.Builder
	b.Grow(len(encoded) + len(encoded)/width*2 + 2)
	for i := 0; i < len(encoded); i += width {
		end := i + width
		if end > len(encoded) {
			end = len(encoded)
		}
		b.WriteString(encoded[i:end])
		b.WriteString("\r\n")
	}
	return b.String()
}

func encodeGmailRawMessage(input DraftInput) (string, error) {
	raw, err := buildRFC822Message(input)
	if err != nil {
		return "", err
	}
	return base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(raw), nil
}

func defaultSMTPSender(ctx context.Context, cfg SMTPConfig, from string, recipients []string, msg []byte) error {
	host := strings.TrimSpace(cfg.Host)
	if host == "" {
		return errors.New("smtp host is required")
	}
	port := cfg.Port
	if port == 0 {
		if cfg.TLS {
			port = 465
		} else {
			port = 587
		}
	}
	addr := fmt.Sprintf("%s:%d", host, port)
	username := strings.TrimSpace(cfg.Username)
	password := cfg.Password
	if cfg.TLS {
		dialer := &net.Dialer{}
		conn, err := dialer.DialContext(ctx, "tcp", addr)
		if err != nil {
			return err
		}
		defer conn.Close()
		tlsConn := tls.Client(conn, &tls.Config{ServerName: host})
		if err := tlsConn.Handshake(); err != nil {
			return err
		}
		client, err := smtp.NewClient(tlsConn, host)
		if err != nil {
			return err
		}
		defer client.Close()
		if username != "" && password != "" {
			if ok, _ := client.Extension("AUTH"); ok {
				if err := client.Auth(smtp.PlainAuth("", username, password, host)); err != nil {
					return err
				}
			}
		}
		return smtpSendWithClient(client, from, recipients, msg)
	}
	client, err := smtp.Dial(addr)
	if err != nil {
		return err
	}
	defer client.Close()
	if ok, _ := client.Extension("STARTTLS"); cfg.StartTLS && ok {
		if err := client.StartTLS(&tls.Config{ServerName: host}); err != nil {
			return err
		}
	}
	if username != "" && password != "" {
		if ok, _ := client.Extension("AUTH"); ok {
			if err := client.Auth(smtp.PlainAuth("", username, password, host)); err != nil {
				return err
			}
		}
	}
	return smtpSendWithClient(client, from, recipients, msg)
}

func smtpSendWithClient(client *smtp.Client, from string, recipients []string, msg []byte) error {
	if err := client.Mail(from); err != nil {
		return err
	}
	for _, recipient := range recipients {
		if err := client.Rcpt(recipient); err != nil {
			return err
		}
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := writer.Write(msg); err != nil {
		_ = writer.Close()
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return client.Quit()
}

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
	httpClient  *http.Client
	cfg         ExchangeConfig
	lookupEnv   func(string) (string, bool)
	now         func() time.Time
	sleep       func(context.Context, time.Duration) error
	prompt      DeviceCodePrompt
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
	client := &ExchangeClient{httpClient: http.DefaultClient, cfg: cfg, lookupEnv: os.LookupEnv, now: time.Now, sleep: sleepContext}
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
