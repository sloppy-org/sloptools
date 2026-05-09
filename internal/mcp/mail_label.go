package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/BurntSushi/toml"
	"github.com/sloppy-org/sloptools/internal/brain"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/store"
)

// mailFolderClassification is the derived label/project shape from a mail
// folder path per the D5 GTD/mail convention. Project, when set, takes
// precedence over the leaf folder segment in Labels.
type mailFolderClassification struct {
	Project string
	Labels  []string
}

const defaultMailWaitingFolder = "Waiting"

// mailFolderToLabel converts a mail folder path (e.g. "INBOX/Teaching/WSD")
// into a project link plus track/<seg> labels per D5. INBOX is treated as the
// root and contributes no labels. If the leaf slug or the full slug path
// resolves to a project note in the matching sphere's brain vault, it
// becomes Project; the leaf segment is then dropped from Labels.
func mailFolderToLabel(folder string, sphere string, brainCfg *brain.Config) mailFolderClassification {
	segments := mailFolderSegments(folder)
	if len(segments) == 0 {
		return mailFolderClassification{}
	}
	classification := mailFolderClassification{Labels: make([]string, 0, len(segments))}
	for _, segment := range segments {
		classification.Labels = append(classification.Labels, "track/"+segment)
	}
	leafIdx := len(segments) - 1
	candidates := []string{segments[leafIdx]}
	if leafIdx > 0 {
		candidates = append(candidates, strings.Join(segments, "-"))
	}
	for _, candidate := range candidates {
		name, ok := brainProjectNoteName(brainCfg, sphere, candidate)
		if !ok {
			continue
		}
		classification.Project = "[[projects/" + name + "]]"
		classification.Labels = append(classification.Labels[:leafIdx], classification.Labels[leafIdx+1:]...)
		break
	}
	return classification
}

// mailFolderSegments slugifies the parts of a folder path. INBOX (root) is
// dropped; deeper segments are slugified individually.
func mailFolderSegments(folder string) []string {
	clean := strings.TrimSpace(folder)
	if clean == "" {
		return nil
	}
	parts := strings.Split(strings.ReplaceAll(clean, "\\", "/"), "/")
	out := make([]string, 0, len(parts))
	for i, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		if i == 0 && strings.EqualFold(trimmed, "inbox") {
			continue
		}
		slug := slugify(trimmed)
		if slug == "" || slug == "item" {
			continue
		}
		out = append(out, slug)
	}
	return out
}

// brainProjectNoteName returns the canonical Name from
// brain/projects/<slug>.md if it exists for the given sphere. It tolerates
// case differences in the filename so existing projects with mixed-case
// slugs (e.g. "RT-08.md") still resolve.
func brainProjectNoteName(brainCfg *brain.Config, sphere, slug string) (string, bool) {
	if brainCfg == nil {
		return "", false
	}
	clean := strings.TrimSpace(slug)
	if clean == "" {
		return "", false
	}
	vault, ok := brainCfg.Vault(brain.Sphere(strings.ToLower(strings.TrimSpace(sphere))))
	if !ok {
		return "", false
	}
	dir := filepath.Join(vault.BrainRoot(), "projects")
	entries, err := os.ReadDir(dir)
	if err != nil {
		exact := filepath.Join(dir, clean+".md")
		if info, statErr := os.Stat(exact); statErr == nil && !info.IsDir() {
			return clean, true
		}
		return "", false
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		base := strings.TrimSuffix(entry.Name(), ".md")
		if strings.EqualFold(base, clean) {
			return base, true
		}
	}
	return "", false
}

// mailDerivedStatus is the GTD status derived from a mail message per D5.
// FollowUp is only set when Status is "deferred".
type mailDerivedStatus struct {
	Status   string
	FollowUp string
}

// mailMessageToGTDStatus maps a mail message to its GTD status per the D5
// convention. waitingFolder lets accounts override the default "Waiting"
// folder name; matching is case-insensitive on the leaf or any path
// segment.
func mailMessageToGTDStatus(message *providerdata.EmailMessage, waitingFolder string) mailDerivedStatus {
	if message == nil {
		return mailDerivedStatus{}
	}
	folder := mailMessageFolder(message)
	if mailFolderMatchesWaiting(folder, waitingFolder) {
		return mailDerivedStatus{Status: "waiting"}
	}
	if !mailFolderInsideInbox(folder) {
		return mailDerivedStatus{Status: "closed"}
	}
	if !message.IsRead {
		return mailDerivedStatus{Status: "inbox"}
	}
	if message.IsFlagged && message.FollowUpAt != nil {
		due := message.FollowUpAt.UTC()
		if due.After(time.Now().UTC()) {
			return mailDerivedStatus{Status: "deferred", FollowUp: due.Format("2006-01-02")}
		}
	}
	return mailDerivedStatus{Status: "next"}
}

func mailMessageFolder(message *providerdata.EmailMessage) string {
	if message == nil {
		return ""
	}
	if folder := strings.TrimSpace(message.Folder); folder != "" {
		return folder
	}
	for _, label := range message.Labels {
		clean := strings.TrimSpace(label)
		lower := strings.ToLower(clean)
		switch lower {
		case "", "inbox", "posteingang", "sent", "starred", "unread", "spam", "trash", "draft":
			continue
		}
		return clean
	}
	for _, label := range message.Labels {
		lower := strings.ToLower(strings.TrimSpace(label))
		if lower == "inbox" || lower == "posteingang" {
			return "INBOX"
		}
	}
	return ""
}

func mailFolderInsideInbox(folder string) bool {
	clean := strings.TrimSpace(folder)
	if clean == "" {
		return false
	}
	parts := strings.Split(strings.ReplaceAll(clean, "\\", "/"), "/")
	first := strings.ToLower(strings.TrimSpace(parts[0]))
	return first == "inbox" || first == "posteingang"
}

func mailFolderMatchesWaiting(folder, configured string) bool {
	clean := strings.TrimSpace(folder)
	if clean == "" {
		return false
	}
	target := strings.TrimSpace(configured)
	if target == "" {
		target = defaultMailWaitingFolder
	}
	parts := strings.Split(strings.ReplaceAll(clean, "\\", "/"), "/")
	for _, part := range parts {
		if strings.EqualFold(strings.TrimSpace(part), target) {
			return true
		}
	}
	return false
}

// mailAccountWaitingFolder reads the waiting_folder override from the
// account's ConfigJSON, defaulting to "Waiting".
func mailAccountWaitingFolder(account store.ExternalAccount) string {
	raw := strings.TrimSpace(account.ConfigJSON)
	if raw == "" || raw == "{}" {
		return defaultMailWaitingFolder
	}
	var cfg struct {
		WaitingFolder string `json:"waiting_folder"`
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return defaultMailWaitingFolder
	}
	if folder := strings.TrimSpace(cfg.WaitingFolder); folder != "" {
		return folder
	}
	return defaultMailWaitingFolder
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

func resolveAttachmentDir(arg string) (string, error) {
	dest := strings.TrimSpace(arg)
	if dest == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home dir: %w", err)
		}
		dest = filepath.Join(home, "Downloads", "sloppy-attachments")
	}
	if strings.HasPrefix(dest, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home dir: %w", err)
		}
		dest = filepath.Join(home, strings.TrimPrefix(dest, "~/"))
	}
	absDir, err := filepath.Abs(dest)
	if err != nil {
		return "", fmt.Errorf("resolve dest_dir: %w", err)
	}
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		return "", fmt.Errorf("create dest_dir: %w", err)
	}
	return absDir, nil
}

func writeAttachmentFile(destDir, account, messageID string, a *providerdata.AttachmentData) (string, error) {
	filename := sanitizeFilenameComponent(strings.TrimSpace(a.Filename))
	if filename == "" {
		filename = sanitizeFilenameComponent(strings.TrimSpace(a.ID))
	}
	if filename == "" {
		filename = "attachment"
	}
	prefix := sanitizeFilenameComponent(strings.TrimSpace(account))
	if prefix == "" {
		prefix = "unknown-account"
	}
	msgTag := sanitizeFilenameComponent(strings.TrimSpace(messageID))
	if len(msgTag) > 16 {
		msgTag = msgTag[:16]
	}
	if msgTag != "" {
		prefix = prefix + "_" + msgTag
	}
	base := prefix + "_" + filename
	absPath := filepath.Join(destDir, base)
	absPath, err := writeNoClobber(absPath, a.Content)
	if err != nil {
		return "", fmt.Errorf("write attachment: %w", err)
	}
	return absPath, nil
}

func writeNoClobber(path string, data []byte) (string, error) {
	candidate := path
	for i := 0; i < 1000; i++ {
		f, err := os.OpenFile(candidate, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			defer f.Close()
			if _, werr := f.Write(data); werr != nil {
				return "", werr
			}
			return candidate, nil
		}
		if !os.IsExist(err) {
			return "", err
		}
		ext := filepath.Ext(path)
		stem := strings.TrimSuffix(path, ext)
		candidate = fmt.Sprintf("%s-%d%s", stem, i+1, ext)
	}
	return "", fmt.Errorf("too many filename collisions for %s", path)
}

func sanitizeFilenameComponent(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			b.WriteRune(r)
		case r == '.' || r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	cleaned := strings.Trim(b.String(), ".")
	if len(cleaned) > 120 {
		cleaned = cleaned[:120]
	}
	return cleaned
}
