// Package zulip is a small read-only client for the Zulip REST API.
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

// SearchParams selects recent messages with optional Zulip narrow terms.
// Query maps to Zulip's full-text search operator.
type SearchParams struct {
	Query  string
	Stream string
	Topic  string
	After  time.Time
	Before time.Time
	Limit  int
}

// Stream is one subscribed Zulip stream visible to the configured bot.
type Stream struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
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
