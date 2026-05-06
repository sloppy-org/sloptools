package inbox

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/brain"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/store"
)

type TaskSourceParts struct {
	AccountID int64
	ListID    string
}

type FileSource struct {
	Sphere string
	Root   string
	Inbox  string
	Count  int
}

type FileItem struct {
	ID      string
	Path    string
	Size    int64
	ModTime time.Time
}

func TaskSourceID(account store.ExternalAccount, list providerdata.TaskList) string {
	return fmt.Sprintf("google_tasks:%s:%d:%s", account.Sphere, account.ID, list.ID)
}

func ParseTaskSourceID(sourceID string) TaskSourceParts {
	parts := strings.Split(strings.TrimSpace(sourceID), ":")
	if len(parts) != 4 || parts[0] != "google_tasks" {
		return TaskSourceParts{}
	}
	var accountID int64
	if _, err := fmt.Sscan(parts[2], &accountID); err != nil {
		return TaskSourceParts{}
	}
	return TaskSourceParts{AccountID: accountID, ListID: parts[3]}
}

func FileSourceID(sphere string) string {
	return fmt.Sprintf("file:%s:INBOX", sphere)
}

func IsFileSourceID(sourceID string) bool {
	return parseFileSourceID(sourceID) != ""
}

func FileSources(cfg *brain.Config, sphere string) ([]FileSource, error) {
	out := make([]FileSource, 0, len(cfg.Vaults))
	for _, vault := range cfg.Vaults {
		current := string(vault.Sphere)
		if sphere != "" && current != sphere {
			continue
		}
		inbox := filepath.Join(vault.Root, "INBOX")
		count, err := CountBareFiles(inbox)
		if err != nil {
			return nil, err
		}
		out = append(out, FileSource{Sphere: current, Root: vault.Root, Inbox: inbox, Count: count})
	}
	return out, nil
}

func FileSourceForID(cfg *brain.Config, sourceID string) (FileSource, error) {
	sphere := parseFileSourceID(sourceID)
	if sphere == "" {
		return FileSource{}, errors.New("file source_id is required")
	}
	vault, ok := cfg.Vault(brain.Sphere(sphere))
	if !ok {
		return FileSource{}, fmt.Errorf("no vault configured for sphere %q", sphere)
	}
	inbox := filepath.Join(vault.Root, "INBOX")
	count, err := CountBareFiles(inbox)
	if err != nil {
		return FileSource{}, err
	}
	return FileSource{Sphere: sphere, Root: vault.Root, Inbox: inbox, Count: count}, nil
}

func FileItemForID(source FileSource, id string) (FileItem, error) {
	if id == "" {
		return FileItem{}, errors.New("id is required")
	}
	if id != filepath.Base(id) || strings.Contains(id, string(filepath.Separator)) {
		return FileItem{}, fmt.Errorf("invalid file inbox id %q", id)
	}
	path := filepath.Join(source.Inbox, id)
	info, err := os.Lstat(path)
	if err != nil {
		return FileItem{}, err
	}
	if !info.Mode().IsRegular() {
		return FileItem{}, fmt.Errorf("inbox item %q is not a regular file", id)
	}
	return FileItem{ID: id, Path: filepath.ToSlash(filepath.Join("INBOX", id)), Size: info.Size(), ModTime: info.ModTime()}, nil
}

func ListBareFiles(source FileSource) ([]FileItem, error) {
	entries, err := os.ReadDir(source.Inbox)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	items := make([]FileItem, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		name := entry.Name()
		items = append(items, FileItem{ID: name, Path: filepath.ToSlash(filepath.Join("INBOX", name)), Size: info.Size(), ModTime: info.ModTime()})
	}
	sort.Slice(items, func(i, j int) bool {
		return strings.ToLower(items[i].ID) < strings.ToLower(items[j].ID)
	})
	return items, nil
}

func CountBareFiles(inbox string) (int, error) {
	items, err := ListBareFiles(FileSource{Inbox: inbox})
	if err != nil {
		return 0, err
	}
	return len(items), nil
}

func MoveFile(source FileSource, item FileItem, targetPath string) (string, error) {
	dest, err := resolveTarget(source, targetPath)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(filepath.Join(source.Inbox, item.ID), dest); err != nil {
		return "", err
	}
	rel, _ := filepath.Rel(source.Root, dest)
	return filepath.ToSlash(rel), nil
}

func ChooseTaskInboxList(lists []providerdata.TaskList, listID string) (providerdata.TaskList, bool) {
	if listID != "" {
		for _, list := range lists {
			if list.ID == listID {
				return list, true
			}
		}
		return providerdata.TaskList{}, false
	}
	for _, list := range lists {
		if strings.EqualFold(strings.TrimSpace(list.Name), "INBOX") {
			return list, true
		}
	}
	for _, list := range lists {
		if list.Primary || list.IsInboxProject {
			return list, true
		}
	}
	if len(lists) == 1 {
		return lists[0], true
	}
	return providerdata.TaskList{}, false
}

func IncompleteTasks(items []providerdata.TaskItem) []providerdata.TaskItem {
	out := make([]providerdata.TaskItem, 0, len(items))
	for _, item := range items {
		if !item.Completed {
			out = append(out, item)
		}
	}
	return out
}

func SortTasks(items []providerdata.TaskItem) {
	sort.Slice(items, func(i, j int) bool {
		return strings.ToLower(items[i].Title) < strings.ToLower(items[j].Title)
	})
}

func ClassifyTask(sphere string, item providerdata.TaskItem, contextText string) map[string]interface{} {
	text := strings.TrimSpace(item.Title + " " + item.Notes + " " + item.Description + " " + contextText)
	if sphere == "" || LooksLikeShopping(text) {
		sphere = store.SpherePrivate
	}
	kind, targetKind, target, language := "commitment", "brain_gtd", "brain/commitments", ""
	if LooksLikeShopping(text) {
		kind, targetKind, target, language = "shopping", "shopping_list", "private Einkaufsliste / shoppy", "de"
	}
	return map[string]interface{}{"sphere": sphere, "kind": kind, "target_kind": targetKind, "target": target, "language": language, "ack_action": "inbox.item_ack", "ack_after": "canonical target written and validated", "source_binding": fmt.Sprintf("tasks:%s:%s:%s", sphere, item.ListID, item.ID), "requires_review": false}
}

func ClassifyFile(source FileSource, item FileItem, contextText string) map[string]interface{} {
	kind := "file"
	switch strings.ToLower(filepath.Ext(item.ID)) {
	case ".jpg", ".jpeg", ".png", ".heic", ".webp":
		kind = "photo"
	case ".pdf":
		kind = "scan_or_document"
	case ".md", ".txt":
		kind = "note_or_text"
	}
	return map[string]interface{}{"sphere": source.Sphere, "kind": kind, "target_kind": "source_folder", "target": "natural vault folder outside INBOX", "context": strings.TrimSpace(contextText), "ack_action": "inbox.item_ack", "ack_after": "file moved or canonical target written and validated", "source_binding": fmt.Sprintf("file:%s:%s", source.Sphere, item.Path), "requires_review": false}
}

func LooksLikeShopping(text string) bool {
	normalized := strings.ToLower(strings.TrimSpace(text))
	if normalized == "" {
		return false
	}
	terms := []string{"kaufen", "einkaufen", "besorgen", "dm", "billa", "spar", "alfies", "milch", "brot", "kaese", "käse", "wurst", "nudeln", "pasta", "reis", "tomaten", "gurke", "avocado", "edamame", "lachs", "fischstaebchen", "fischstäbchen", "leberkaese", "leberkäse", "eier", "ei", "muesli", "müsli", "tee", "kaffee", "matcha", "bier", "hummus", "topfen", "brokkoli", "spinat", "pueree", "püree", "obst", "banane", "apfel"}
	for _, term := range terms {
		if strings.Contains(normalized, term) {
			return true
		}
	}
	return len(strings.Fields(normalized)) <= 2 && !strings.ContainsAny(normalized, ".:;?!")
}

func parseFileSourceID(sourceID string) string {
	parts := strings.Split(strings.TrimSpace(sourceID), ":")
	if len(parts) != 3 || parts[0] != "file" || parts[2] != "INBOX" {
		return ""
	}
	if parts[1] != store.SpherePrivate && parts[1] != store.SphereWork {
		return ""
	}
	return parts[1]
}

func resolveTarget(source FileSource, targetPath string) (string, error) {
	if filepath.IsAbs(targetPath) {
		return "", errors.New("target_path must be vault-relative")
	}
	clean := filepath.Clean(filepath.FromSlash(targetPath))
	if clean == "." || clean == "" {
		return "", errors.New("target_path is required")
	}
	if clean == "INBOX" || strings.HasPrefix(clean, "INBOX"+string(filepath.Separator)) {
		return "", errors.New("target_path must be outside INBOX")
	}
	dest := filepath.Join(source.Root, clean)
	rel, err := filepath.Rel(source.Root, dest)
	if err != nil || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return "", errors.New("target_path escapes vault root")
	}
	if _, err := os.Lstat(dest); err == nil {
		return "", fmt.Errorf("target_path already exists: %s", filepath.ToSlash(clean))
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return dest, nil
}
