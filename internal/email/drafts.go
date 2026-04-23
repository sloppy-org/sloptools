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
	"net/mail"
	"net/smtp"
	"path/filepath"
	"strings"
	"time"
)

type DraftAttachment struct {
	Filename    string
	ContentType string
	Content     []byte
}

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
	Attachments []DraftAttachment
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

// ExistingDraftSender sends a draft that already lives in the mailbox as-is,
// without rewriting its content. Providers that cannot send by id alone (for
// example IMAP+SMTP, where the SMTP envelope must be rebuilt from the stored
// draft) do not implement this interface.
type ExistingDraftSender interface {
	SendExistingDraft(ctx context.Context, draftID string) error
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
	attachments, err := normalizeDraftAttachments(input.Attachments)
	if err != nil {
		return DraftInput{}, err
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
		Attachments: attachments,
	}, nil
}

func normalizeDraftAttachments(values []DraftAttachment) ([]DraftAttachment, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]DraftAttachment, 0, len(values))
	for i, raw := range values {
		filename := strings.TrimSpace(raw.Filename)
		if filename == "" {
			return nil, fmt.Errorf("attachment %d: filename is required", i)
		}
		filename = filepath.Base(filename)
		if len(raw.Content) == 0 {
			return nil, fmt.Errorf("attachment %q: content is empty", filename)
		}
		ct := strings.TrimSpace(raw.ContentType)
		if ct == "" {
			ct = "application/octet-stream"
		}
		out = append(out, DraftAttachment{
			Filename:    filename,
			ContentType: ct,
			Content:     append([]byte(nil), raw.Content...),
		})
	}
	return out, nil
}

func NormalizeDraftInput(input DraftInput) (DraftInput, error) {
	return normalizeDraftInput(input, false)
}

func ExportRFC822ForTest(input DraftInput) ([]byte, error) {
	return buildRFC822Message(input)
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
	return EnsureReplySubject(subject)
}

func EnsureReplySubject(subject string) string {
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
