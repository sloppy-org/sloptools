package ews

import "strings"

func canonicalDistinguishedFolderID(folderID string) (string, bool) {
	clean := strings.ToLower(strings.TrimSpace(folderID))
	switch clean {
	case "inbox", "calendar", "contacts", "tasks", "drafts", "sentitems", "deleteditems", "junkemail", "msgfolderroot", "archivemsgfolderroot", "archiveinbox", "archivedeleteditems":
		return clean, true
	case "sent", "sent items":
		return "sentitems", true
	case "trash", "deleted", "deleted items":
		return "deleteditems", true
	case "junk", "spam", "junk email", "junk-e-mail":
		return "junkemail", true
	case "draft":
		return "drafts", true
	default:
		return "", false
	}
}
