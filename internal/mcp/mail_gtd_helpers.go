package mcp

import (
	"encoding/json"
	"fmt"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/store"
)

func cloneMailMessage(message *providerdata.EmailMessage) *providerdata.EmailMessage {
	if message == nil {
		return nil
	}
	clone := *message
	if len(message.Recipients) > 0 {
		clone.Recipients = append([]string(nil), message.Recipients...)
	}
	if len(message.Labels) > 0 {
		clone.Labels = append([]string(nil), message.Labels...)
	}
	if len(message.Attachments) > 0 {
		clone.Attachments = append([]providerdata.Attachment(nil), message.Attachments...)
	}
	return &clone
}

func mailCommitmentText(message *providerdata.EmailMessage) string {
	if message == nil {
		return ""
	}
	body := preferPlainBody(message)
	if len(body) > 6000 {
		body = body[:6000]
	}
	parts := []string{strings.TrimSpace(message.Subject), strings.TrimSpace(message.Snippet), strings.TrimSpace(body)}
	return strings.Join(parts, " ")
}

func mailLooksSent(message *providerdata.EmailMessage, selfAddresses []string) bool {
	if message == nil {
		return false
	}
	for _, label := range message.Labels {
		clean := strings.ToLower(strings.TrimSpace(label))
		if clean == "" {
			continue
		}
		if strings.Contains(clean, "sent") {
			return true
		}
	}
	if mailAddressMatchesAny(message.Sender, selfAddresses) {
		return true
	}
	return false
}

func mailSenderLooksHuman(raw string) bool {
	sender := strings.TrimSpace(raw)
	if sender == "" {
		return false
	}
	if mailMachinePattern.MatchString(sender) {
		return false
	}
	return true
}

func mailPeerForMessage(message *providerdata.EmailMessage, selfAddresses []string, sent bool) string {
	if message == nil {
		return ""
	}
	if sent {
		for _, recipient := range message.Recipients {
			if !mailAddressMatchesAny(recipient, selfAddresses) {
				if label := mailPersonLabel(recipient); label != "" {
					return label
				}
			}
		}
		if len(message.Recipients) > 0 {
			return mailPersonLabel(message.Recipients[0])
		}
		return mailPersonLabel(message.Sender)
	}
	return mailPersonLabel(message.Sender)
}

func mailDeadlineFromText(text string, _ time.Time) (string, bool) {
	match := mailDeadlinePattern.FindStringSubmatch(text)
	if len(match) < 2 {
		return "", false
	}
	parsed, err := mailParseLooseTime(match[1])
	if err != nil {
		return "", false
	}
	return parsed.UTC().Format("2006-01-02"), true
}

func mailParseLooseTime(raw string) (time.Time, error) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return time.Time{}, fmt.Errorf("empty time")
	}
	layouts := []string{time.RFC3339, "2006-01-02T15:04", "2006-01-02 15:04", "2006-01-02"}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, clean); err == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported time %q", raw)
}

func mailCommitmentTitle(status, peer, subject string) string {
	cleanSubject := strings.TrimSpace(subject)
	if cleanSubject == "" {
		if status == "waiting" {
			return "Waiting for " + peer
		}
		return "Reply to " + peer
	}
	return cleanSubject
}

func mailCommitmentLabels(message *providerdata.EmailMessage) []string {
	if message == nil || len(message.Labels) == 0 {
		return nil
	}
	return append([]string(nil), message.Labels...)
}

func mailCommitmentPeople(status, peer string) []string {
	if strings.TrimSpace(peer) == "" {
		return nil
	}
	if status == "waiting" {
		return []string{peer}
	}
	return []string{peer}
}

func mailContext(message *providerdata.EmailMessage) string {
	if message == nil {
		return "mail"
	}
	for _, label := range message.Labels {
		clean := strings.TrimSpace(label)
		if clean == "" {
			continue
		}
		lower := strings.ToLower(clean)
		if lower == "inbox" || lower == "sent" || strings.Contains(lower, "sent") {
			return "mail/" + lower
		}
	}
	return "mail"
}

func mailProjectForMessage(message *providerdata.EmailMessage) string {
	if message == nil {
		return ""
	}
	for _, label := range message.Labels {
		clean := strings.TrimSpace(label)
		if clean == "" {
			continue
		}
		lower := strings.ToLower(clean)
		switch lower {
		case "inbox", "sent", "starred", "unread", "spam", "trash", "draft":
			continue
		default:
			return clean
		}
	}
	return ""
}

func mailAccountAddresses(account store.ExternalAccount) []string {
	candidates := []string{account.AccountName}
	var cfg struct {
		Username    string `json:"username"`
		FromAddress string `json:"from_address"`
		Email       string `json:"email"`
	}
	if raw := strings.TrimSpace(account.ConfigJSON); raw != "" && raw != "{}" {
		_ = json.Unmarshal([]byte(raw), &cfg)
	}
	candidates = append(candidates, cfg.Username, cfg.FromAddress, cfg.Email)
	return compactStringList(candidates)
}

func mailAddressMatchesAny(raw string, addresses []string) bool {
	if len(addresses) == 0 {
		return false
	}
	cleanRaw := strings.TrimSpace(raw)
	if cleanRaw == "" {
		return false
	}
	parsed, err := mail.ParseAddress(cleanRaw)
	if err == nil {
		cleanRaw = strings.ToLower(strings.TrimSpace(parsed.Address))
	} else {
		cleanRaw = strings.ToLower(cleanRaw)
	}
	for _, candidate := range addresses {
		cleanCandidate := strings.TrimSpace(candidate)
		if cleanCandidate == "" {
			continue
		}
		parsedCandidate, err := mail.ParseAddress(cleanCandidate)
		if err == nil {
			if strings.EqualFold(strings.TrimSpace(parsedCandidate.Address), cleanRaw) {
				return true
			}
			continue
		}
		if strings.EqualFold(cleanCandidate, cleanRaw) {
			return true
		}
	}
	return false
}

func mailPersonLabel(raw string) string {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return ""
	}
	if parsed, err := mail.ParseAddress(clean); err == nil {
		if name := strings.TrimSpace(parsed.Name); name != "" {
			return name
		}
		if addr := strings.TrimSpace(parsed.Address); addr != "" {
			return addr
		}
	}
	return clean
}

func mailPersonEmail(raw string) string {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return ""
	}
	if parsed, err := mail.ParseAddress(clean); err == nil {
		if addr := strings.TrimSpace(parsed.Address); addr != "" {
			return addr
		}
	}
	return clean
}

func evidenceTimestamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func mailArtifactMetaJSON(account store.ExternalAccount, message *providerdata.EmailMessage, sourceURL string, bodyFetched bool) string {
	meta := map[string]any{
		"provider":     account.Provider,
		"account_id":   account.ID,
		"message_id":   strings.TrimSpace(message.ID),
		"thread_id":    strings.TrimSpace(message.ThreadID),
		"subject":      strings.TrimSpace(message.Subject),
		"sender":       strings.TrimSpace(message.Sender),
		"recipients":   append([]string(nil), message.Recipients...),
		"labels":       append([]string(nil), message.Labels...),
		"source_url":   sourceURL,
		"body_fetched": bodyFetched,
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func mailSourceURL(account store.ExternalAccount, messageID string) string {
	return (&url.URL{
		Scheme: "sloptools",
		Host:   "mail",
		Path:   fmt.Sprintf("/%s/%d/%s", url.PathEscape(strings.TrimSpace(account.Provider)), account.ID, url.PathEscape(strings.TrimSpace(messageID))),
	}).String()
}

func stringPtr(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	v := value
	return &v
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
