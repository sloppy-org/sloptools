package mailboxsettings

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/googleauth"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	gmail "google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

const gmailProviderName = "gmail_mailbox_settings"

// GmailProvider maps Gmail's `users.settings.vacationResponder` shape onto
// the canonical `providerdata.OOFSettings` type. Uses a shared
// `googleauth.Session` so mail/calendar/contacts/tasks/mailbox settings
// all route through one OAuth pipeline per account.
type GmailProvider struct {
	session *googleauth.Session
	svcFn   func(ctx context.Context) (*gmail.Service, error)
}

var _ OOFProvider = (*GmailProvider)(nil)

// NewGmailProvider wraps an existing session. The default scope set
// (googleauth.DefaultScopes) already grants Gmail modify access.
func NewGmailProvider(session *googleauth.Session) *GmailProvider {
	return &GmailProvider{session: session}
}

// Session exposes the cached OAuth session for sharing verification.
func (p *GmailProvider) Session() *googleauth.Session { return p.session }

// ProviderName identifies the backend in logs and MCP payloads.
func (p *GmailProvider) ProviderName() string { return gmailProviderName }

// Close is a no-op; the registry owns the session.
func (p *GmailProvider) Close() error { return nil }

func (p *GmailProvider) service(ctx context.Context) (*gmail.Service, error) {
	if p == nil {
		return nil, fmt.Errorf("mailboxsettings: gmail provider is nil")
	}
	if p.svcFn != nil {
		return p.svcFn(ctx)
	}
	if p.session == nil {
		return nil, fmt.Errorf("mailboxsettings: gmail session is not configured")
	}
	tokenSource, err := p.session.TokenSource(ctx)
	if err != nil {
		return nil, err
	}
	return gmail.NewService(ctx, option.WithTokenSource(tokenSource))
}

// GetOOF reads the vacation responder. Gmail exposes no scope field, so Scope
// always comes back as "all" when the responder is enabled.
func (p *GmailProvider) GetOOF(ctx context.Context) (providerdata.OOFSettings, error) {
	svc, err := p.service(ctx)
	if err != nil {
		return providerdata.OOFSettings{}, err
	}
	settings, err := svc.Users.Settings.GetVacation("me").Context(ctx).Do()
	if err != nil {
		return providerdata.OOFSettings{}, fmt.Errorf("get gmail vacation responder: %w", err)
	}
	out := providerdata.OOFSettings{
		Enabled:       settings.EnableAutoReply,
		InternalReply: pickReplyBody(settings.ResponseBodyPlainText, settings.ResponseBodyHtml),
		ExternalReply: pickReplyBody(settings.ResponseBodyPlainText, settings.ResponseBodyHtml),
	}
	if out.Enabled {
		out.Scope = gmailVacationScope(settings)
	}
	if settings.StartTime > 0 {
		t := time.UnixMilli(settings.StartTime).UTC()
		out.StartAt = &t
	}
	if settings.EndTime > 0 {
		t := time.UnixMilli(settings.EndTime).UTC()
		out.EndAt = &t
	}
	return out, nil
}

// SetOOF writes the vacation responder.
func (p *GmailProvider) SetOOF(ctx context.Context, settings providerdata.OOFSettings) error {
	svc, err := p.service(ctx)
	if err != nil {
		return err
	}
	req := &gmail.VacationSettings{
		EnableAutoReply:       settings.Enabled,
		ResponseBodyPlainText: strings.TrimSpace(settings.InternalReply),
	}
	if req.ResponseBodyPlainText == "" {
		req.ResponseBodyPlainText = strings.TrimSpace(settings.ExternalReply)
	}
	if settings.StartAt != nil {
		req.StartTime = settings.StartAt.UnixMilli()
	}
	if settings.EndAt != nil {
		req.EndTime = settings.EndAt.UnixMilli()
	}
	req.RestrictToContacts = false
	req.RestrictToDomain = false
	switch strings.ToLower(strings.TrimSpace(settings.Scope)) {
	case "contacts":
		req.RestrictToContacts = true
	case "internal", "external":
		req.RestrictToDomain = true
	}
	if _, err := svc.Users.Settings.UpdateVacation("me", req).Context(ctx).Do(); err != nil {
		return fmt.Errorf("update gmail vacation responder: %w", err)
	}
	return nil
}

func pickReplyBody(plain, html string) string {
	if trimmed := strings.TrimSpace(plain); trimmed != "" {
		return trimmed
	}
	return strings.TrimSpace(html)
}

func gmailVacationScope(s *gmail.VacationSettings) string {
	switch {
	case s.RestrictToContacts && !s.RestrictToDomain:
		return "contacts"
	case s.RestrictToDomain && !s.RestrictToContacts:
		return "internal"
	case s.RestrictToDomain && s.RestrictToContacts:
		return "internal"
	default:
		return "all"
	}
}
