// Package zulip is a small read-only client for the Zulip REST API used
// by the meeting kickoff helper. It only covers the surface needed to
// fetch messages from a stream + topic in a bounded time window; deeper
// integration (sending, subscribing, mentions API) belongs in a future
// package or wrapper.
package zulip

import (
	"context"
	"errors"
	"strings"
	"time"
)

// Message is a single Zulip message returned by the messages endpoint,
// trimmed to the fields the kickoff helper consumes.
type Message struct {
	ID          int64     `json:"id"`
	SenderName  string    `json:"sender_full_name"`
	SenderEmail string    `json:"sender_email"`
	Stream      string    `json:"display_recipient"`
	Topic       string    `json:"subject"`
	Timestamp   time.Time `json:"timestamp"`
	Content     string    `json:"content"`
}

// MessagesParams selects messages by stream + topic over a time window.
// After is the inclusive lower bound (e.g. cutoff - 24h) and Before is
// the exclusive upper bound (e.g. the meeting start). Limit caps the
// number of messages returned; a non-positive value falls back to a
// safe default.
type MessagesParams struct {
	Stream string
	Topic  string
	After  time.Time
	Before time.Time
	Limit  int
}

// MessagesProvider is the read-side interface the kickoff helper depends
// on. It is satisfied by the REST client in this package and by test
// fakes that pre-load fixed messages.
type MessagesProvider interface {
	Messages(ctx context.Context, params MessagesParams) ([]Message, error)
}

// Errors returned by the client. Callers should compare with errors.Is.
var (
	ErrCredentialsMissing = errors.New("zulip credentials are not configured")
	ErrStreamRequired     = errors.New("zulip messages require a stream name")
	ErrTopicRequired      = errors.New("zulip messages require a topic name")
)

func cleanString(value string) string {
	return strings.TrimSpace(value)
}

const defaultMessageLimit = 100
