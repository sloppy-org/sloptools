package providerkind

import "strings"

const (
	Gmail          = "gmail"
	IMAP           = "imap"
	GoogleCalendar = "google_calendar"
	ICS            = "ics"
	Todoist        = "todoist"
	Evernote       = "evernote"
	Bear           = "bear"
	Zotero         = "zotero"
	Exchange       = "exchange"
	ExchangeEWS    = "exchange_ews"
)

func IsEmail(provider string) bool {
	provider = strings.ToLower(strings.TrimSpace(provider))
	switch provider {
	case Gmail, IMAP, Exchange, ExchangeEWS:
		return true
	default:
		return false
	}
}

func IsManagedEmail(provider string) bool {
	provider = strings.ToLower(strings.TrimSpace(provider))
	switch provider {
	case Gmail, Exchange, ExchangeEWS:
		return true
	default:
		return false
	}
}
