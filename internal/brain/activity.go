package brain

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const ActivityReportDir = "reports/activity"

type ActivityLogOpts struct {
	Sphere    Sphere
	Date      string
	Operation string
	Tool      string
	Message   string
	Links     []string
	Now       time.Time
}

type ActivityLogResult struct {
	Sphere Sphere   `json:"sphere"`
	Date   string   `json:"date"`
	Path   string   `json:"path"`
	Entry  string   `json:"entry"`
	Links  []string `json:"links"`
}

func WriteActivityLog(cfg *Config, opts ActivityLogOpts) (*ActivityLogResult, error) {
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	date, err := activityDate(opts.Date, opts.Now)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(opts.Message) == "" {
		return nil, fmt.Errorf("activity message is required")
	}
	vault, err := cfgVault(cfg, opts.Sphere)
	if err != nil {
		return nil, err
	}
	links := normalizeActivityLinks(vault, opts.Links)
	entry := renderActivityLogEntry(opts, links)
	path := activityReportPath(vault, date)
	if err := ensureActivityReport(path, opts.Sphere, date); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if _, err := file.WriteString(entry); err != nil {
		return nil, err
	}
	return &ActivityLogResult{Sphere: opts.Sphere, Date: date, Path: path, Entry: strings.TrimSpace(entry), Links: links}, nil
}

func ReadActivitySummary(cfg *Config, sphere Sphere, date string) (string, string, error) {
	vault, err := cfgVault(cfg, sphere)
	if err != nil {
		return "", "", err
	}
	path := activityReportPath(vault, date)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", path, err
	}
	return string(data), path, nil
}

func RenderActivitySleepPacket(markdown string) string {
	lines := strings.Split(markdown, "\n")
	var b strings.Builder
	for _, line := range lines {
		if strings.HasPrefix(line, "---") ||
			strings.HasPrefix(line, "kind:") ||
			strings.HasPrefix(line, "sphere:") ||
			strings.HasPrefix(line, "date:") ||
			strings.HasPrefix(line, "source:") {
			continue
		}
		fmt.Fprintln(&b, line)
	}
	return strings.TrimSpace(b.String())
}

func activityReportPath(vault Vault, date string) string {
	return filepath.Join(vault.BrainRoot(), ActivityReportDir, date+".md")
}

func ensureActivityReport(path string, sphere Sphere, date string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body := fmt.Sprintf("---\nkind: activity_log\nsphere: %s\ndate: %s\nsource: sloptools\n---\n\n# Brain activity %s\n\n", sphere, date, date)
	return os.WriteFile(path, []byte(body), 0o644)
}

func renderActivityLogEntry(opts ActivityLogOpts, links []string) string {
	when := opts.Now.Format("15:04")
	operation := strings.TrimSpace(opts.Operation)
	if operation == "" {
		operation = "note"
	}
	tool := strings.TrimSpace(opts.Tool)
	if tool == "" {
		tool = "sloptools"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "- %s %s/%s: %s", when, tool, operation, strings.TrimSpace(opts.Message))
	if len(links) > 0 {
		fmt.Fprint(&b, " ")
		for i, link := range links {
			if i > 0 {
				fmt.Fprint(&b, ", ")
			}
			fmt.Fprintf(&b, "[[%s]]", strings.TrimSuffix(link, ".md"))
		}
	}
	fmt.Fprint(&b, "\n")
	return b.String()
}

func normalizeActivityLinks(vault Vault, raw []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, item := range raw {
		clean := normalizeActivityLink(vault, item)
		if clean == "" || seen[clean] {
			continue
		}
		seen[clean] = true
		out = append(out, clean)
	}
	sort.Strings(out)
	return out
}

func normalizeActivityLink(vault Vault, raw string) string {
	clean := strings.TrimSpace(raw)
	clean = strings.Trim(clean, "[]")
	if strings.HasPrefix(clean, "brain/") {
		clean = strings.TrimPrefix(clean, "brain/")
	}
	if filepath.IsAbs(clean) {
		rel, err := filepath.Rel(vault.BrainRoot(), clean)
		if err != nil || strings.HasPrefix(rel, "..") {
			return ""
		}
		clean = rel
	}
	clean = filepath.ToSlash(filepath.Clean(clean))
	if clean == "." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
		return ""
	}
	if !strings.HasSuffix(clean, ".md") {
		clean += ".md"
	}
	return clean
}

func activityDate(raw string, now time.Time) (string, error) {
	date := strings.TrimSpace(raw)
	if date == "" {
		return now.Format("2006-01-02"), nil
	}
	if _, err := time.ParseInLocation("2006-01-02", date, time.Local); err != nil {
		return "", fmt.Errorf("date must be YYYY-MM-DD")
	}
	return date, nil
}
