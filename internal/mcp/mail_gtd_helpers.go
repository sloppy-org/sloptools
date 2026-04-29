package mcp

import (
	"encoding/json"
	"fmt"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/sloppy-org/sloptools/internal/brain"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/store"
)

type mailProjectRule struct {
	Name     string   `toml:"name"`
	Keywords []string `toml:"keywords"`
	People   []string `toml:"people"`
}

type mailProjectConfig struct {
	Projects []mailProjectRule `toml:"project"`
}

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

func mailProjectForMessage(message *providerdata.EmailMessage, candidate mailCommitmentCandidate, rules []mailProjectRule) string {
	if message == nil {
		return ""
	}
	for _, rule := range rules {
		if rule.matches(message, candidate) {
			return "[[projects/" + rule.Name + "]]"
		}
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

func (r mailProjectRule) matches(message *providerdata.EmailMessage, candidate mailCommitmentCandidate) bool {
	name := strings.TrimSpace(r.Name)
	if name == "" || message == nil {
		return false
	}
	peer := strings.ToLower(strings.TrimSpace(candidate.peer))
	sender := strings.ToLower(mailPersonLabel(message.Sender))
	recipients := strings.ToLower(strings.Join(message.Recipients, " "))
	for _, person := range r.People {
		clean := strings.ToLower(strings.TrimSpace(person))
		if clean != "" && (strings.Contains(peer, clean) || strings.Contains(sender, clean) || strings.Contains(recipients, clean)) {
			return true
		}
	}
	text := strings.ToLower(strings.Join([]string{message.Subject, message.Snippet, strings.Join(message.Labels, " "), mailCommitmentText(message)}, " "))
	for _, keyword := range r.Keywords {
		clean := strings.ToLower(strings.TrimSpace(keyword))
		if clean != "" && strings.Contains(text, clean) {
			return true
		}
	}
	return false
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

func loadMailProjectRules(path string) ([]mailProjectRule, error) {
	resolved, explicit, err := sloptoolsConfigPath(path, "projects.toml")
	if err != nil {
		return nil, err
	}
	var cfg mailProjectConfig
	if _, err := toml.DecodeFile(resolved, &cfg); err != nil {
		if !explicit && os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("load mail project rules: %w", err)
	}
	out := make([]mailProjectRule, 0, len(cfg.Projects))
	for _, rule := range cfg.Projects {
		rule.Name = strings.TrimSpace(rule.Name)
		if rule.Name == "" {
			continue
		}
		rule.Name = strings.Trim(rule.Name, "/")
		rule.Keywords = compactStringList(rule.Keywords)
		rule.People = compactStringList(rule.People)
		out = append(out, rule)
	}
	return out, nil
}

func loadMailBrainConfig(path string) (*brain.Config, error) {
	if strings.TrimSpace(path) == "" {
		cfg, err := brain.LoadConfig("")
		if err != nil {
			return nil, nil
		}
		return cfg, nil
	}
	cfg, err := brain.LoadConfig(path)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

func mailPersonNoteDiagnostic(cfg *brain.Config, sphere, person string) string {
	target, ok := mailPersonNoteTarget(person)
	if !ok || cfg == nil {
		return ""
	}
	vault, ok := cfg.Vault(brain.Sphere(sphere))
	if !ok {
		return ""
	}
	path := filepath.Join(vault.BrainRoot(), "people", target+".md")
	if _, err := os.Stat(path); err == nil {
		return ""
	}
	return "needs_person_note: " + target
}

func mailPersonNoteTarget(person string) (string, bool) {
	clean := strings.TrimSpace(person)
	if clean == "" {
		return "", false
	}
	if strings.Contains(clean, "@") && !strings.Contains(clean, " ") {
		return "", false
	}
	clean = strings.Trim(clean, "/")
	clean = strings.ReplaceAll(clean, string(filepath.Separator), " ")
	clean = strings.Join(strings.Fields(clean), " ")
	return clean, clean != ""
}

func sloptoolsConfigPath(path, name string) (string, bool, error) {
	clean := strings.TrimSpace(path)
	if clean != "" {
		if strings.HasPrefix(clean, "~/") {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", true, err
			}
			clean = filepath.Join(home, strings.TrimPrefix(clean, "~/"))
		}
		return filepath.Clean(clean), true, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false, err
	}
	return filepath.Join(home, ".config", "sloptools", name), false, nil
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
