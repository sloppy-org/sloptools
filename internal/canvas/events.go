package canvas

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

type EventKind string

const (
	EventText  EventKind = "text_artifact"
	EventImage EventKind = "image_artifact"
	EventPDF   EventKind = "pdf_artifact"
	EventClear EventKind = "clear_canvas"
)

type Event struct {
	EventID string                 `json:"event_id"`
	TS      string                 `json:"ts"`
	Kind    EventKind              `json:"kind"`
	Title   string                 `json:"title,omitempty"`
	Text    string                 `json:"text,omitempty"`
	Path    string                 `json:"path,omitempty"`
	Page    int                    `json:"page,omitempty"`
	Reason  string                 `json:"reason,omitempty"`
	Meta    map[string]interface{} `json:"meta,omitempty"`
}

func NewEvent(kind EventKind) Event {
	return Event{
		EventID: newID(),
		TS:      time.Now().UTC().Format(time.RFC3339Nano),
		Kind:    kind,
	}
}

func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return time.Now().UTC().Format("20060102150405.000000000")
	}
	return hex.EncodeToString(b)
}
