package mailboxsettings

import (
	"context"
	"fmt"
	"strings"

	"github.com/sloppy-org/sloptools/internal/ews"
	"github.com/sloppy-org/sloptools/internal/providerdata"
)

const ewsProviderName = "exchange_ews_mailbox_settings"

// EWSProvider implements OOFProvider against the EWS `GetUserOofSettings` and
// `SetUserOofSettings` SOAP operations. The underlying ews.Client is shared
// across mail/calendar/contacts/tasks providers via the groupware registry.
type EWSProvider struct {
	client  *ews.Client
	mailbox string
}

var _ OOFProvider = (*EWSProvider)(nil)

// NewEWSProvider wraps a cached EWS client for the given mailbox SMTP address;
// the address is sent in the SOAP `Mailbox/Address` element of the OOF
// operations.
func NewEWSProvider(client *ews.Client, mailbox string) *EWSProvider {
	return &EWSProvider{client: client, mailbox: strings.TrimSpace(mailbox)}
}

// Client exposes the cached ews.Client so callers can verify sharing across
// feature providers.
func (p *EWSProvider) Client() *ews.Client { return p.client }

// ProviderName identifies the backend in logs and MCP payloads.
func (p *EWSProvider) ProviderName() string { return ewsProviderName }

// Close is a no-op; the registry owns the EWS client.
func (p *EWSProvider) Close() error { return nil }

// GetOOF reads the EWS out-of-office configuration and converts it to the
// canonical providerdata shape. Scheduled state populates StartAt/EndAt;
// always-on Enabled state leaves them nil.
func (p *EWSProvider) GetOOF(ctx context.Context) (providerdata.OOFSettings, error) {
	if p == nil || p.client == nil {
		return providerdata.OOFSettings{}, fmt.Errorf("ews OOF GetOOF: client is not configured")
	}
	if p.mailbox == "" {
		return providerdata.OOFSettings{}, fmt.Errorf("ews OOF GetOOF: mailbox address is required")
	}
	raw, err := p.client.GetUserOofSettings(ctx, p.mailbox)
	if err != nil {
		return providerdata.OOFSettings{}, fmt.Errorf("ews OOF GetOOF: %w", err)
	}
	out := providerdata.OOFSettings{
		Enabled:       raw.State == ews.OofStateEnabled || raw.State == ews.OofStateScheduled,
		Scope:         scopeFromAudience(raw.ExternalAudience),
		InternalReply: raw.InternalReply,
		ExternalReply: raw.ExternalReply,
	}
	if raw.State == ews.OofStateScheduled {
		if !raw.Start.IsZero() {
			start := raw.Start
			out.StartAt = &start
		}
		if !raw.End.IsZero() {
			end := raw.End
			out.EndAt = &end
		}
	}
	return out, nil
}

// SetOOF writes the EWS out-of-office configuration. When Enabled is false the
// responder is disabled; when StartAt/EndAt are both set the request becomes a
// Scheduled responder; otherwise it is always-on Enabled.
func (p *EWSProvider) SetOOF(ctx context.Context, settings providerdata.OOFSettings) error {
	if p == nil || p.client == nil {
		return fmt.Errorf("ews OOF SetOOF: client is not configured")
	}
	if p.mailbox == "" {
		return fmt.Errorf("ews OOF SetOOF: mailbox address is required")
	}
	state := ews.OofStateDisabled
	if settings.Enabled {
		state = ews.OofStateEnabled
		if settings.StartAt != nil && settings.EndAt != nil {
			state = ews.OofStateScheduled
		}
	}
	payload := ews.OofSettings{
		State:            state,
		ExternalAudience: audienceFromScope(settings.Scope),
		InternalReply:    settings.InternalReply,
		ExternalReply:    settings.ExternalReply,
	}
	if settings.StartAt != nil {
		payload.Start = settings.StartAt.UTC()
	}
	if settings.EndAt != nil {
		payload.End = settings.EndAt.UTC()
	}
	if err := p.client.SetUserOofSettings(ctx, p.mailbox, payload); err != nil {
		return fmt.Errorf("ews OOF SetOOF: %w", err)
	}
	return nil
}

func scopeFromAudience(audience ews.OofExternalAudience) string {
	switch audience {
	case ews.OofAudienceAll:
		return "all"
	case ews.OofAudienceKnown:
		return "contacts"
	default:
		return "internal"
	}
}

func audienceFromScope(scope string) ews.OofExternalAudience {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "all", "external", "":
		return ews.OofAudienceAll
	case "contacts", "known":
		return ews.OofAudienceKnown
	case "internal", "none":
		return ews.OofAudienceNone
	default:
		return ews.OofAudienceAll
	}
}
