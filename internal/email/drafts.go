package email

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"
)

type DraftInput struct {
	From        string
	To          []string
	Cc          []string
	Bcc         []string
	Subject     string
	Body        string
	ThreadID    string
	InReplyTo   string
	References  []string
	ReplyToID   string
	ReplyToAddr string
}

type Draft struct {
	ID       string
	ThreadID string
}

type DraftProvider interface {
	CreateDraft(context.Context, DraftInput) (Draft, error)
	CreateReplyDraft(context.Context, string, DraftInput) (Draft, error)
	UpdateDraft(context.Context, string, DraftInput) (Draft, error)
	SendDraft(context.Context, string, DraftInput) error
}

type SMTPConfig struct {
	Host      string
	Port      int
	Username  string
	Password  string
	TLS       bool
	StartTLS  bool
	From      string
	FromName  string
	DraftsBox string
}

type SMTPSender func(context.Context, SMTPConfig, string, []string, []byte) error

func normalizeDraftAddress(raw string) (string, error) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return "", nil
	}
	addr, err := mail.ParseAddress(clean)
	if err != nil {
		return "", fmt.Errorf("invalid address %q", clean)
	}
	return strings.ToLower(strings.TrimSpace(addr.Address)), nil
}

func normalizeDraftAddresses(values []string) ([]string, error) {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, raw := range values {
		clean := strings.TrimSpace(raw)
		if clean == "" {
			continue
		}
		parsed, err := mail.ParseAddressList(clean)
		if err != nil {
			single, singleErr := normalizeDraftAddress(clean)
			if singleErr != nil {
				return nil, fmt.Errorf("invalid address %q", clean)
			}
			if single != "" {
				if _, ok := seen[single]; !ok {
					seen[single] = struct{}{}
					out = append(out, single)
				}
			}
			continue
		}
		for _, addr := range parsed {
			lower := strings.ToLower(strings.TrimSpace(addr.Address))
			if lower == "" {
				continue
			}
			if _, ok := seen[lower]; ok {
				continue
			}
			seen[lower] = struct{}{}
			out = append(out, lower)
		}
	}
	return out, nil
}

func normalizeDraftInput(input DraftInput, requireRecipients bool) (DraftInput, error) {
	to, err := normalizeDraftAddresses(input.To)
	if err != nil {
		return DraftInput{}, err
	}
	cc, err := normalizeDraftAddresses(input.Cc)
	if err != nil {
		return DraftInput{}, err
	}
	bcc, err := normalizeDraftAddresses(input.Bcc)
	if err != nil {
		return DraftInput{}, err
	}
	from, err := normalizeDraftAddress(input.From)
	if err != nil {
		return DraftInput{}, err
	}
	replyToAddr, err := normalizeDraftAddress(input.ReplyToAddr)
	if err != nil {
		return DraftInput{}, err
	}
	subject := strings.TrimSpace(input.Subject)
	body := strings.ReplaceAll(strings.TrimSpace(input.Body), "\r\n", "\n")
	if requireRecipients && len(to) == 0 && len(cc) == 0 && len(bcc) == 0 {
		return DraftInput{}, errors.New("at least one recipient is required")
	}
	return DraftInput{
		From:        from,
		To:          to,
		Cc:          cc,
		Bcc:         bcc,
		Subject:     subject,
		Body:        body,
		ThreadID:    strings.TrimSpace(input.ThreadID),
		InReplyTo:   strings.TrimSpace(input.InReplyTo),
		References:  trimStringList(input.References),
		ReplyToID:   strings.TrimSpace(input.ReplyToID),
		ReplyToAddr: replyToAddr,
	}, nil
}

func NormalizeDraftInput(input DraftInput) (DraftInput, error) {
	return normalizeDraftInput(input, false)
}

func NormalizeDraftSendInput(input DraftInput) (DraftInput, error) {
	return normalizeDraftInput(input, true)
}

func trimStringList(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		clean := strings.TrimSpace(value)
		if clean != "" {
			out = append(out, clean)
		}
	}
	return out
}

func ensureReplySubject(subject string) string {
	clean := strings.TrimSpace(subject)
	if clean == "" {
		return "Re:"
	}
	if strings.HasPrefix(strings.ToLower(clean), "re:") {
		return clean
	}
	return "Re: " + clean
}

func formatDraftHeaderAddresses(values []string) string {
	return strings.Join(trimStringList(values), ", ")
}

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
	fmt.Fprintf(&msg, "Content-Type: text/plain; charset=UTF-8\r\n")
	fmt.Fprintf(&msg, "Content-Transfer-Encoding: 8bit\r\n")
	if normalized.InReplyTo != "" {
		fmt.Fprintf(&msg, "In-Reply-To: %s\r\n", normalized.InReplyTo)
	}
	if len(normalized.References) > 0 {
		fmt.Fprintf(&msg, "References: %s\r\n", strings.Join(normalized.References, " "))
	}
	msg.WriteString("\r\n")
	msg.WriteString(strings.ReplaceAll(normalized.Body, "\n", "\r\n"))
	if !strings.HasSuffix(normalized.Body, "\n") {
		msg.WriteString("\r\n")
	}
	return msg.Bytes(), nil
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
