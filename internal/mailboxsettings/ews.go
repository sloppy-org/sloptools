package mailboxsettings

import (
	"context"
	"fmt"

	"github.com/sloppy-org/sloptools/internal/ews"
	"github.com/sloppy-org/sloptools/internal/providerdata"
)

const ewsProviderName = "exchange_ews_mailbox_settings"

// EWSProvider is the EWS-backed OOF implementation. The EWS OOF SOAP
// operations (GetUserOofSettings / SetUserOofSettings) need their own
// request bodies on top of the existing ews.Client transport; the first
// wire-up here returns ErrUnsupported so callers probing the capability get
// a clean machine-readable refusal. A follow-up issue tracks adding the full
// SOAP wrappers and flipping these methods to real implementations.
type EWSProvider struct {
	client  *ews.Client
	mailbox string
}

var _ OOFProvider = (*EWSProvider)(nil)

// NewEWSProvider wraps a cached EWS client for the given mailbox address
// (used in the SOAP Mailbox/Address header of the OOF operations).
func NewEWSProvider(client *ews.Client, mailbox string) *EWSProvider {
	return &EWSProvider{client: client, mailbox: mailbox}
}

// Client exposes the cached ews.Client so callers can verify sharing across
// feature providers.
func (p *EWSProvider) Client() *ews.Client { return p.client }

// ProviderName identifies the backend in logs and MCP payloads.
func (p *EWSProvider) ProviderName() string { return ewsProviderName }

// Close is a no-op; the registry owns the EWS client.
func (p *EWSProvider) Close() error { return nil }

// GetOOF reads the out-of-office settings from the EWS mailbox. Not yet
// wired through; returns ErrUnsupported until the SOAP wrappers land.
func (p *EWSProvider) GetOOF(ctx context.Context) (providerdata.OOFSettings, error) {
	return providerdata.OOFSettings{}, fmt.Errorf("ews OOF GetOOF: %w", ErrUnsupported)
}

// SetOOF writes the out-of-office settings. Not yet wired through.
func (p *EWSProvider) SetOOF(ctx context.Context, settings providerdata.OOFSettings) error {
	return fmt.Errorf("ews OOF SetOOF: %w", ErrUnsupported)
}
