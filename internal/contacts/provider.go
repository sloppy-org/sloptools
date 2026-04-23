// Package contacts defines the core contact-management contract and the
// capability interfaces that specific backends (Google People, Exchange EWS)
// may implement on top of it. The split mirrors internal/email/provider.go:
// the core Provider covers read and identity, capabilities layer on search,
// mutation, group membership, and photo fetching.
package contacts

import (
	"context"
	"errors"

	"github.com/sloppy-org/sloptools/internal/providerdata"
)

// ErrUnsupported signals that a Provider does not implement a requested
// capability. Callers should use errors.Is to detect it so that wrapped
// errors from downstream layers still match.
var ErrUnsupported = errors.New("contacts: provider does not support this capability")

// Provider is the minimum contract every contact backend must satisfy:
// enumerate the authenticated user's contacts, fetch one by id, identify
// itself, and release long-lived resources.
type Provider interface {
	ListContacts(ctx context.Context) ([]providerdata.Contact, error)
	GetContact(ctx context.Context, id string) (providerdata.Contact, error)
	ProviderName() string
	Close() error
}

// Searcher exposes backend-native free-text search over the contact store.
// Backends that only offer client-side filtering omit this capability so
// callers can fall back to ListContacts + local filtering.
type Searcher interface {
	SearchContacts(ctx context.Context, query string) ([]providerdata.Contact, error)
}

// Mutator covers CRUD on individual contacts.
type Mutator interface {
	CreateContact(ctx context.Context, c providerdata.Contact) (providerdata.Contact, error)
	UpdateContact(ctx context.Context, c providerdata.Contact) (providerdata.Contact, error)
	DeleteContact(ctx context.Context, id string) error
}

// Group represents a named collection of contacts (Google contact groups,
// EWS distribution lists / categories).
type Group struct {
	ID          string
	Name        string
	MemberCount int
}

// Grouper lets callers read and reshape group membership without forcing every
// backend to model groups the same way internally.
type Grouper interface {
	ListContactGroups(ctx context.Context) ([]Group, error)
	AddToGroup(ctx context.Context, groupID string, contactIDs []string) error
	RemoveFromGroup(ctx context.Context, groupID string, contactIDs []string) error
}

// PhotoFetcher returns the binary photo content plus its MIME type.
// Backends without a photo pipeline return ErrUnsupported.
type PhotoFetcher interface {
	GetContactPhoto(ctx context.Context, id string) (data []byte, mime string, err error)
}
