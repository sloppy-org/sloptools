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
	Attendees   []string
	Recurring   bool
}

type Contact struct {
	ProviderRef  string
	Name         string
	Email        string
	Organization string
	Phones       []string
}
