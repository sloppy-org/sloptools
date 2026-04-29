package sourceitems

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/providerdata"
)

const (
	GitHubProviderName = "github"
	GitLabProviderName = "gitlab"
)

type Provider interface {
	ProviderName() string
	List(ctx context.Context) ([]providerdata.SourceItem, error)
	Close(ctx context.Context, item providerdata.SourceItem, comment string) error
	Comment(ctx context.Context, item providerdata.SourceItem, body string) error
}

type commandRunner func(ctx context.Context, name string, args ...string) *exec.Cmd

func defaultCommandRunner(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}

func loadRemote(projectDir string) (string, error) {
	out, err := exec.Command("git", "-C", projectDir, "remote", "get-url", "origin").CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return "", fmt.Errorf("git remote get-url origin: %s", msg)
		}
		return "", fmt.Errorf("git remote get-url origin: %w", err)
	}
	remote := strings.TrimSpace(string(out))
	if remote == "" {
		return "", errors.New("git remote origin is empty")
	}
	return remote, nil
}

func containerFromGitRemote(remote string) string {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return ""
	}
	if strings.Contains(remote, "://") {
		parsed, err := url.Parse(remote)
		if err == nil {
			host := strings.TrimSpace(parsed.Host)
			path := strings.Trim(strings.TrimPrefix(parsed.Path, "/"), "/")
			path = strings.TrimSuffix(path, ".git")
			if host != "" && path != "" {
				if strings.EqualFold(host, "github.com") || strings.EqualFold(host, "gitlab.com") {
					return path
				}
				return host + "/" + path
			}
		}
	}
	if strings.HasPrefix(remote, "git@") && strings.Contains(remote, ":") {
		parts := strings.SplitN(strings.TrimPrefix(remote, "git@"), ":", 2)
		if len(parts) == 2 {
			host := strings.TrimSuffix(parts[0], ".git")
			path := strings.TrimSuffix(parts[1], ".git")
			if strings.EqualFold(host, "github.com") {
				return path
			}
			if host != "" && path != "" {
				return host + "/" + path
			}
		}
	}
	return strings.TrimSuffix(remote, ".git")
}

func githubRepoArg(remote string) string {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return ""
	}
	if strings.Contains(remote, "://") {
		parsed, err := url.Parse(remote)
		if err == nil {
			host := strings.TrimSpace(parsed.Host)
			path := strings.Trim(strings.TrimPrefix(parsed.Path, "/"), "/")
			path = strings.TrimSuffix(path, ".git")
			if strings.EqualFold(host, "github.com") && path != "" {
				return path
			}
			if strings.Count(path, "/") >= 1 {
				return path
			}
		}
	}
	if strings.HasPrefix(remote, "git@") && strings.Contains(remote, ":") {
		after := strings.SplitN(strings.TrimPrefix(remote, "git@"), ":", 2)[1]
		after = strings.TrimSuffix(after, ".git")
		return after
	}
	trimmed := strings.TrimSuffix(remote, ".git")
	if strings.Count(trimmed, "/") >= 2 && !strings.Contains(trimmed, "://") {
		return trimmed
	}
	return strings.TrimPrefix(strings.TrimPrefix(trimmed, "https://github.com/"), "http://github.com/")
}

func gitlabRepoArg(remote string) string {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return ""
	}
	if strings.Contains(remote, "://") {
		parsed, err := url.Parse(remote)
		if err == nil {
			path := strings.Trim(strings.TrimPrefix(parsed.Path, "/"), "/")
			path = strings.TrimSuffix(path, ".git")
			if path != "" {
				return path
			}
		}
	}
	if strings.HasPrefix(remote, "git@") && strings.Contains(remote, ":") {
		after := strings.SplitN(strings.TrimPrefix(remote, "git@"), ":", 2)[1]
		return strings.TrimSuffix(after, ".git")
	}
	return strings.TrimSuffix(remote, ".git")
}

func normalizeList(items []providerdata.SourceItem) []providerdata.SourceItem {
	seen := make(map[string]struct{}, len(items))
	out := make([]providerdata.SourceItem, 0, len(items))
	for _, item := range items {
		key := strings.ToLower(strings.TrimSpace(item.SourceRef))
		if key == "" {
			key = strings.ToLower(strings.TrimSpace(item.Provider)) + ":" + strings.ToLower(strings.TrimSpace(item.Container)) + ":" + strings.TrimSpace(item.Kind) + ":" + strconv.FormatInt(item.Number, 10)
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt != nil && out[j].UpdatedAt != nil {
			if !out[i].UpdatedAt.Equal(*out[j].UpdatedAt) {
				return out[i].UpdatedAt.After(*out[j].UpdatedAt)
			}
		} else if out[i].UpdatedAt != nil {
			return true
		} else if out[j].UpdatedAt != nil {
			return false
		}
		if out[i].Container != out[j].Container {
			return out[i].Container < out[j].Container
		}
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Number < out[j].Number
	})
	return out
}

func sourceRef(provider, kind, container string, number int64) string {
	if strings.TrimSpace(provider) == "" || strings.TrimSpace(container) == "" || number <= 0 {
		return ""
	}
	sep := "#"
	if strings.EqualFold(provider, GitLabProviderName) && strings.EqualFold(kind, "merge_request") {
		sep = "!"
	}
	return strings.TrimSpace(provider) + ":" + strings.TrimSpace(container) + sep + strconv.FormatInt(number, 10)
}

func sourceItemMap(item providerdata.SourceItem) map[string]any {
	payload := map[string]any{
		"provider":      item.Provider,
		"kind":          item.Kind,
		"container":     item.Container,
		"number":        item.Number,
		"title":         item.Title,
		"url":           item.URL,
		"state":         item.State,
		"source_ref":    item.SourceRef,
		"review_status": item.ReviewStatus,
	}
	if len(item.Labels) > 0 {
		payload["labels"] = append([]string(nil), item.Labels...)
	}
	if len(item.Assignees) > 0 {
		payload["assignees"] = append([]string(nil), item.Assignees...)
	}
	if item.Author != "" {
		payload["author"] = item.Author
	}
	if len(item.Reviewers) > 0 {
		payload["reviewers"] = append([]string(nil), item.Reviewers...)
	}
	if item.UpdatedAt != nil {
		payload["updated_at"] = item.UpdatedAt.UTC().Format(time.RFC3339)
	}
	if item.ClosedAt != nil {
		payload["closed_at"] = item.ClosedAt.UTC().Format(time.RFC3339)
	}
	return payload
}

func runJSONList(ctx context.Context, runner commandRunner, name string, args ...string) ([]map[string]any, error) {
	cmd := runner(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return nil, fmt.Errorf("%s %s: %s", name, strings.Join(args, " "), msg)
		}
		return nil, fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	var payload []map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		return nil, fmt.Errorf("%s %s: decode json: %w", name, strings.Join(args, " "), err)
	}
	return payload, nil
}

func stringListValue(raw any) []string {
	switch v := raw.(type) {
	case nil:
		return nil
	case []string:
		return compactStrings(v)
	case []any:
		out := make([]string, 0, len(v))
		for _, elem := range v {
			if s := stringFromAny(elem); s != "" {
				out = append(out, s)
			}
		}
		return compactStrings(out)
	default:
		if s := stringFromAny(v); s != "" {
			return []string{s}
		}
		return nil
	}
}

func stringFromAny(raw any) string {
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v)
	case map[string]any:
		for _, key := range []string{"login", "name", "username", "title", "value"} {
			if s := strings.TrimSpace(fmt.Sprint(v[key])); s != "" && s != "<nil>" {
				return s
			}
		}
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	}
	s := strings.TrimSpace(fmt.Sprint(raw))
	if s == "<nil>" || s == "" {
		return ""
	}
	return s
}

func timeValue(raw any) *time.Time {
	s := strings.TrimSpace(stringFromAny(raw))
	if s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil
	}
	return &t
}

func numberValue(raw any) int64 {
	switch v := raw.(type) {
	case int:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	case json.Number:
		n, _ := v.Int64()
		return n
	case string:
		n, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		return n
	default:
		return 0
	}
}

func authorValue(raw any) string {
	switch v := raw.(type) {
	case map[string]any:
		for _, key := range []string{"login", "name", "username"} {
			if s := stringFromAny(v[key]); s != "" {
				return s
			}
		}
	default:
		return stringFromAny(v)
	}
	return ""
}

func compactStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		clean := strings.TrimSpace(value)
		if clean == "" {
			continue
		}
		key := strings.ToLower(clean)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, clean)
	}
	return out
}

func itemFromMap(provider, kind, container string, raw map[string]any) providerdata.SourceItem {
	number := numberValue(raw["number"])
	if number == 0 {
		number = numberValue(raw["iid"])
	}
	item := providerdata.SourceItem{
		Provider:  provider,
		Kind:      kind,
		Container: container,
		Number:    number,
		Title:     stringFromAny(raw["title"]),
		URL:       firstString(raw, "url", "html_url", "web_url"),
		State:     strings.ToLower(strings.TrimSpace(firstString(raw, "state", "status"))),
		Labels:    labelsFromAny(raw["labels"]),
		Assignees: participantNames(raw["assignees"]),
		Author:    authorValue(raw["author"]),
		UpdatedAt: timeValue(firstAny(raw, "updatedAt", "updated_at")),
		ClosedAt:  timeValue(firstAny(raw, "closedAt", "closed_at")),
	}
	if item.URL == "" {
		item.URL = firstString(raw, "web_url", "url")
	}
	item.ReviewStatus = strings.ToLower(strings.TrimSpace(firstString(raw, "reviewDecision", "review_decision", "merge_status", "detailed_merge_status")))
	if kind == "pull_request" || kind == "merge_request" {
		reviewers := participantNames(firstAnySlice(raw, "reviewRequests", "review_requests", "reviewers"))
		if len(reviewers) > 0 {
			item.Reviewers = reviewers
			if item.ReviewStatus == "" {
				item.ReviewStatus = "review_requested"
			}
		}
		if item.ReviewStatus == "" && boolValue(firstAny(raw, "isDraft", "draft", "work_in_progress")) {
			item.ReviewStatus = "draft"
		}
	}
	item.SourceRef = sourceRef(provider, kind, container, number)
	return item
}

func firstAny(raw map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := raw[key]; ok {
			return value
		}
	}
	return nil
}

func firstAnySlice(raw map[string]any, keys ...string) []any {
	for _, key := range keys {
		if value, ok := raw[key]; ok {
			switch v := value.(type) {
			case []any:
				return v
			}
		}
	}
	return nil
}

func firstString(raw map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := raw[key]; ok {
			if s := stringFromAny(value); s != "" {
				return s
			}
		}
	}
	return ""
}

func labelsFromAny(raw any) []string {
	switch v := raw.(type) {
	case []string:
		return compactStrings(v)
	case []any:
		out := make([]string, 0, len(v))
		for _, elem := range v {
			switch e := elem.(type) {
			case map[string]any:
				out = append(out, firstString(e, "name", "title", "label"))
			default:
				out = append(out, stringFromAny(elem))
			}
		}
		return compactStrings(out)
	default:
		if s := stringFromAny(raw); s != "" {
			return []string{s}
		}
		return nil
	}
}

func participantNames(raw any) []string {
	switch v := raw.(type) {
	case nil:
		return nil
	case []string:
		return compactStrings(v)
	case []any:
		out := make([]string, 0, len(v))
		for _, elem := range v {
			switch e := elem.(type) {
			case map[string]any:
				out = append(out, authorValue(e))
			default:
				out = append(out, stringFromAny(elem))
			}
		}
		return compactStrings(out)
	default:
		if s := stringFromAny(raw); s != "" {
			return []string{s}
		}
		return nil
	}
}

func boolValue(raw any) bool {
	switch v := raw.(type) {
	case bool:
		return v
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "1", "yes":
			return true
		}
	}
	return false
}
