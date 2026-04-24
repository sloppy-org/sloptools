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

func IsCalendar(provider string) bool {
	provider = strings.ToLower(strings.TrimSpace(provider))
	switch provider {
	case GoogleCalendar, ICS, ExchangeEWS:
		return true
	default:
		return false
	}
}

func IsTask(provider string) bool {
	provider = strings.ToLower(strings.TrimSpace(provider))
	switch provider {
	case Todoist, ExchangeEWS:
		return true
	default:
		return false
	}
}

func DisplayName(provider string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	switch provider {
	case Gmail:
		return "Gmail"
	case IMAP:
		return "IMAP"
	case Exchange:
		return "Exchange"
	case ExchangeEWS:
		return "Exchange EWS"
	case GoogleCalendar:
		return "Google Calendar"
	case ICS:
		return "ICS"
	case Todoist:
		return "Todoist"
	case Evernote:
		return "Evernote"
	case Bear:
		return "Bear"
	case Zotero:
		return "Zotero"
	default:
		return strings.TrimSpace(provider)
	}
}
