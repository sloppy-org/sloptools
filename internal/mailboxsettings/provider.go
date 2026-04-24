// Package mailboxsettings exposes capability interfaces for mailbox-level
// settings that sit alongside email/calendar/contacts but do not belong to
// any of them. Today: out-of-office (OOF / vacation responder).
package mailboxsettings

import (
	"context"
	"errors"

	"github.com/sloppy-org/sloptools/internal/providerdata"
)

// ErrUnsupported signals that a Provider does not implement a requested
// capability. Callers use errors.Is to detect it so wrapped errors still match.
var ErrUnsupported = errors.New("mailboxsettings: provider does not support this capability")

// OOFProvider is the contract for reading and writing the mailbox's
// out-of-office / vacation-responder settings. Backends expose this as their
// principal capability; future capabilities (delegation, forwarding) plug in
// as peer interfaces on the same provider.
type OOFProvider interface {
	GetOOF(ctx context.Context) (providerdata.OOFSettings, error)
	SetOOF(ctx context.Context, settings providerdata.OOFSettings) error
	ProviderName() string
	Close() error
}

// DelegationProvider reads the set of mailbox delegates and shared mailboxes
// accessible to the account. Both lists are read-only for now; mutating
// delegate assignments is out of scope for the first cut.
type DelegationProvider interface {
	ListDelegates(ctx context.Context) ([]providerdata.Delegate, error)
	ListSharedMailboxes(ctx context.Context) ([]providerdata.SharedMailbox, error)
}
