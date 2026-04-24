package providerdata

import "time"

type EmailMessage struct {
	ID                string
	ThreadID          string
	InternetMessageID string
	Subject           string
	Sender            string
	Recipients        []string
	Date              time.Time
	Snippet           string
	Labels            []string
	IsRead            bool
	IsFlagged         bool
	BodyText          *string
	BodyHTML          *string
	Attachments       []Attachment
}

type Attachment struct {
	ID       string
	Filename string
	MimeType string
	Size     int64
	IsInline bool
}

type AttachmentData struct {
	ID       string
	Filename string
	MimeType string
	Size     int64
	IsInline bool
	Content  []byte
}

type Label struct {
	ID             string
	Name           string
	Type           string
	MessagesTotal  int
	MessagesUnread int
}

type Calendar struct {
	ID          string
	Name        string
	Description string
	TimeZone    string
	Primary     bool
}

// Event is the canonical calendar event shared by Google Calendar and Exchange EWS backends.
type Event struct {
	ID          string
	CalendarID  string
	Summary     string
	Description string
	Location    string
	Start       time.Time
	End         time.Time
	AllDay      bool
	Status      string
	Organizer   string
	// Attendees carries structured invitee data; backends must populate Response when available.
	Attendees []Attendee
	Recurring bool
	// ReminderMinutes is the lead-time for a single reminder; nil means provider default or no reminder.
	ReminderMinutes *int
	// ICSUID is the stable RFC5545 UID used for cross-provider event identity.
	ICSUID string
}

// Attendee captures a single invitee and their current response on an Event.
// Response values follow Google Calendar semantics: needsAction, accepted, declined, tentative.
type Attendee struct {
	Email    string
	Name     string
	Response string
}

// InviteResponse is the payload a user sends when replying to a meeting invite.
// Status values: accepted, declined, tentative.
type InviteResponse struct {
	Status  string
	Comment string
}

// FreeBusySlot reports a single busy-time window for a calendar owner.
// Status values: free, busy, tentative, oof, workingElsewhere.
type FreeBusySlot struct {
	Start  time.Time
	End    time.Time
	Status string
}

// OOFSettings is the out-of-office auto-reply configuration for a mailbox.
// Scope values: all, contacts, external.
type OOFSettings struct {
	Enabled       bool
	Scope         string
	InternalReply string
	ExternalReply string
	StartAt       *time.Time
	EndAt         *time.Time
}

// Delegate is one entry returned by the mailbox delegation list.
// Permissions are backend-specific tokens (Gmail verification status,
// EWS per-folder permission levels such as calendar:Editor) and callers
// should treat them as opaque strings for display.
type Delegate struct {
	Email       string
	Name        string
	Permissions []string
}

// SharedMailbox is one mailbox the account has structured access to beyond
// its own. Gmail models forwarding addresses here; EWS reports empty until a
// discovery path lands.
type SharedMailbox struct {
	Email       string
	Name        string
	AccessLevel string
}

// TaskList is a single task container (Google Tasks list or Exchange tasks folder).
type TaskList struct {
	ID      string
	Name    string
	Primary bool
}

// TaskItem is a single task within a TaskList.
// Priority values follow the source backend; callers should treat them as opaque strings.
type TaskItem struct {
	ID          string
	ListID      string
	Title       string
	Notes       string
	Due         *time.Time
	CompletedAt *time.Time
	Completed   bool
	Priority    string
	ProviderRef string
}

// Contact is the canonical address-book entry shared by Google People and Exchange contacts.
type Contact struct {
	ProviderRef  string
	Name         string
	Email        string
	Organization string
	Phones       []string
	Addresses    []PostalAddress
	// Birthday carries only the date component; time of day is unused.
	Birthday *time.Time
	Notes    string
	Photos   []PhotoRef
}

// PostalAddress is a single mailing address on a Contact.
// Type values follow the source backend (for example home, work, other).
type PostalAddress struct {
	Type    string
	Street  string
	City    string
	Region  string
	Postal  string
	Country string
}

// PhotoRef references a contact photo either by URL or with an inline byte payload.
// Bytes is optional and only populated when the caller fetched the payload inline.
type PhotoRef struct {
	URL         string
	ContentType string
	Bytes       []byte
}
