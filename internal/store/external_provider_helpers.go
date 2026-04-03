package store

import "strings"

func IsEmailProvider(provider string) bool {
	switch normalizeExternalAccountProvider(provider) {
	case ExternalProviderGmail, ExternalProviderIMAP, ExternalProviderExchange, ExternalProviderExchangeEWS:
		return true
	default:
		return false
	}
}

func IsManagedEmailProvider(provider string) bool {
	switch normalizeExternalAccountProvider(provider) {
	case ExternalProviderGmail, ExternalProviderExchange, ExternalProviderExchangeEWS:
		return true
	default:
		return false
	}
}

func IsCalendarProvider(provider string) bool {
	switch normalizeExternalAccountProvider(provider) {
	case ExternalProviderGoogleCalendar, ExternalProviderICS, ExternalProviderExchangeEWS:
		return true
	default:
		return false
	}
}

func IsTaskProvider(provider string) bool {
	switch normalizeExternalAccountProvider(provider) {
	case ExternalProviderTodoist, ExternalProviderExchangeEWS:
		return true
	default:
		return false
	}
}

func ExternalProviderDisplayName(provider string) string {
	switch normalizeExternalAccountProvider(provider) {
	case ExternalProviderGmail:
		return "Gmail"
	case ExternalProviderIMAP:
		return "IMAP"
	case ExternalProviderExchange:
		return "Exchange"
	case ExternalProviderExchangeEWS:
		return "Exchange EWS"
	case ExternalProviderGoogleCalendar:
		return "Google Calendar"
	case ExternalProviderICS:
		return "ICS"
	case ExternalProviderTodoist:
		return "Todoist"
	case ExternalProviderEvernote:
		return "Evernote"
	case ExternalProviderBear:
		return "Bear"
	case ExternalProviderZotero:
		return "Zotero"
	default:
		return strings.TrimSpace(provider)
	}
}
