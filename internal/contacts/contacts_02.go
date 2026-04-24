package contacts

import (
	"context"
	"errors"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	people "google.golang.org/api/people/v1"
	"strings"
)

func firstGooglePhotoURL(values []*people.Photo) string {
	for _, value := range values {
		if value == nil {
			continue
		}
		if url := strings.TrimSpace(value.Url); url != "" {
			return url
		}
	}
	return ""
}

var ErrUnsupported = errors.New("contacts: provider does not support this capability") // ErrUnsupported signals that a Provider does not implement a requested
// capability. Callers should use errors.Is to detect it so that wrapped
// errors from downstream layers still match.

type Provider interface {
	ListContacts(ctx context.Context) ([]providerdata. // Provider is the minimum contract every contact backend must satisfy:
		// enumerate the authenticated user's contacts, fetch one by id, identify
		// itself, and release long-lived resources.
		Contact, error)
	GetContact(ctx context.Context, id string) (providerdata.Contact, error)
	ProviderName() string
	Close() error
}

type Searcher interface {
	SearchContacts(ctx context.Context, query string) ([]providerdata. // Searcher exposes backend-native free-text search over the contact store.
		// Backends that only offer client-side filtering omit this capability so
		// callers can fall back to ListContacts + local filtering.
		Contact, error)
}

type Mutator interface {
	CreateContact(ctx context.Context, c providerdata.Contact) (providerdata.Contact, error)
	UpdateContact(ctx context.Context, c providerdata.Contact) (providerdata.Contact, error)
	DeleteContact(ctx context.Context, id string) error
} // Mutator covers CRUD on individual contacts.

type Group struct {
	ID          string
	Name        string
	MemberCount int
} // Group represents a named collection of contacts (Google contact groups,
// EWS distribution lists / categories).

type Grouper interface {
	ListContactGroups(ctx context.Context) ([]Group, // Grouper lets callers read and reshape group membership without forcing every
		// backend to model groups the same way internally.
		error)
	AddToGroup(ctx context.Context, groupID string, contactIDs []string) error
	RemoveFromGroup(ctx context.Context, groupID string, contactIDs []string) error
}

type PhotoFetcher interface {
	GetContactPhoto(ctx context.Context, id string) (data []byte, // PhotoFetcher returns the binary photo content plus its MIME type.
		// Backends without a photo pipeline return ErrUnsupported.
		mime string, err error)
}
