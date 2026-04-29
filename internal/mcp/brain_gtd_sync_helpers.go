package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	braingtd "github.com/sloppy-org/sloptools/internal/brain/gtd"
	"github.com/sloppy-org/sloptools/internal/store"
)

var runGTDSyncCommandOutput = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return nil, fmt.Errorf("%s %s: %s", name, strings.Join(args, " "), msg)
		}
		return nil, fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return out, nil
}

func closeGitHubBinding(binding braingtd.SourceBinding) error {
	ref, err := parseIssueBinding(binding, "github")
	if err != nil {
		return err
	}
	cmd := "issue"
	if ref.kind == "pull_request" {
		cmd = "pr"
	}
	return runGTDSyncCommand(context.Background(), "gh", cmd, "close", strconv.FormatInt(ref.number, 10), "-R", ref.container)
}

func closeGitLabBinding(binding braingtd.SourceBinding) error {
	ref, err := parseIssueBinding(binding, "gitlab")
	if err != nil {
		return err
	}
	cmd := "issue"
	if ref.kind == "merge_request" {
		cmd = "mr"
	}
	return runGTDSyncCommand(context.Background(), "glab", cmd, "close", strconv.FormatInt(ref.number, 10), "-R", ref.container)
}

func runGTDSyncCommand(ctx context.Context, name string, args ...string) error {
	_, err := runGTDSyncCommandOutput(ctx, name, args...)
	return err
}

func readGitHubBindingState(binding braingtd.SourceBinding) (gtdSyncState, error) {
	ref, err := parseIssueBinding(binding, "github")
	if err != nil {
		return gtdSyncState{}, err
	}
	cmd := "issue"
	if ref.kind == "pull_request" {
		cmd = "pr"
	}
	out, err := runGTDSyncCommandOutput(context.Background(), "gh", cmd, "view", strconv.FormatInt(ref.number, 10), "-R", ref.container, "--json", "state,closedAt")
	if err != nil {
		return gtdSyncState{}, err
	}
	return parseIssueState(out, "state", "closedAt")
}

func readGitLabBindingState(binding braingtd.SourceBinding) (gtdSyncState, error) {
	ref, err := parseIssueBinding(binding, "gitlab")
	if err != nil {
		return gtdSyncState{}, err
	}
	cmd := "issue"
	if ref.kind == "merge_request" {
		cmd = "mr"
	}
	out, err := runGTDSyncCommandOutput(context.Background(), "glab", cmd, "view", strconv.FormatInt(ref.number, 10), "-R", ref.container, "--output", "json")
	if err != nil {
		return gtdSyncState{}, err
	}
	return parseIssueState(out, "state", "closed_at")
}

func parseIssueState(out []byte, stateKey, closedAtKey string) (gtdSyncState, error) {
	var payload map[string]interface{}
	if err := json.Unmarshal(out, &payload); err != nil {
		return gtdSyncState{}, err
	}
	state := strings.ToLower(issueString(payload[stateKey]))
	switch state {
	case "closed", "merged":
		return gtdSyncState{Status: "closed", ClosedAt: issueString(payload[closedAtKey])}, nil
	case "open", "opened":
		return gtdSyncState{Status: "open"}, nil
	default:
		return gtdSyncState{}, fmt.Errorf("unsupported upstream state %q", state)
	}
}

func issueString(value interface{}) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func gtdSyncProvider(provider string) string {
	clean := strings.ToLower(strings.TrimSpace(provider))
	if clean == "mail" || emailCapableProvider(clean) {
		return "mail"
	}
	return clean
}

type issueBindingRef struct {
	container string
	kind      string
	number    int64
}

func parseIssueBinding(binding braingtd.SourceBinding, provider string) (issueBindingRef, error) {
	ref := strings.TrimSpace(binding.Ref)
	sep := "#"
	kind := "issue"
	if provider == "gitlab" && strings.Contains(ref, "!") {
		sep = "!"
		kind = "merge_request"
	}
	if provider == "github" && strings.Contains(binding.URL, "/pull/") {
		kind = "pull_request"
	}
	parts := strings.Split(ref, sep)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" {
		return issueBindingRef{}, fmt.Errorf("invalid %s binding ref %q", provider, binding.Ref)
	}
	number, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
	if err != nil || number <= 0 {
		return issueBindingRef{}, fmt.Errorf("invalid %s binding number %q", provider, binding.Ref)
	}
	return issueBindingRef{container: strings.TrimSpace(parts[0]), kind: kind, number: number}, nil
}

func firstProviderAccount(st *store.Store, sphere, provider string, capable func(string) bool) (store.ExternalAccount, error) {
	accounts, err := st.ListExternalAccounts(strings.TrimSpace(sphere))
	if err != nil {
		return store.ExternalAccount{}, err
	}
	for _, account := range accounts {
		if !account.Enabled || !capable(account.Provider) {
			continue
		}
		if provider != "" && !strings.EqualFold(account.Provider, provider) {
			continue
		}
		return account, nil
	}
	if provider != "" {
		return store.ExternalAccount{}, fmt.Errorf("no enabled %s account for sphere %q", provider, sphere)
	}
	return store.ExternalAccount{}, fmt.Errorf("no enabled account for sphere %q", sphere)
}

func splitTaskBindingRef(ref string) (string, string) {
	clean := strings.TrimSpace(ref)
	for _, sep := range []string{"/", ":"} {
		if i := strings.Index(clean, sep); i > 0 {
			return strings.TrimSpace(clean[:i]), strings.TrimSpace(clean[i+1:])
		}
	}
	return "", clean
}

func syncAction(note dedupNote, binding braingtd.SourceBinding, action string, dryRun bool) gtdSyncAction {
	return gtdSyncAction{Path: note.Entry.Path, Binding: binding.StableID(), Provider: binding.Provider, Action: action, DryRun: dryRun}
}

func syncError(note dedupNote, binding braingtd.SourceBinding, err error) gtdSyncError {
	return gtdSyncError{Path: note.Entry.Path, Binding: binding.StableID(), Error: err.Error()}
}

func commitmentClosed(commitment braingtd.Commitment) bool {
	status := strings.ToLower(strings.TrimSpace(commitment.LocalOverlay.Status))
	if status == "" {
		status = strings.ToLower(strings.TrimSpace(commitment.Status))
	}
	return closedStatus(status)
}

func closedStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "closed", "done", "dropped":
		return true
	default:
		return false
	}
}

func syncClosedAt(state gtdSyncState) string {
	if strings.TrimSpace(state.ClosedAt) != "" {
		return state.ClosedAt
	}
	return time.Now().UTC().Format(time.RFC3339)
}

func mailLabelsContain(labels []string, want string) bool {
	for _, label := range labels {
		if strings.EqualFold(strings.TrimSpace(label), want) {
			return true
		}
	}
	return false
}

func isPathWithin(root, path string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	return err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))))
}

func compactSyncStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if clean := strings.TrimSpace(value); clean != "" {
			out = append(out, clean)
		}
	}
	return out
}

type gtdSourcesFile struct {
	Sources []gtdSourceRule `toml:"source"`
}

type gtdSourceRule struct {
	Sphere    string `toml:"sphere"`
	Provider  string `toml:"provider"`
	Ref       string `toml:"ref"`
	Writeable bool   `toml:"writeable"`
}

type gtdSources struct {
	rules []gtdSourceRule
}

func loadGTDSources(path string) (gtdSources, error) {
	resolved, explicit, err := sloptoolsConfigPath(path, "sources.toml")
	if err != nil {
		return gtdSources{}, err
	}
	var file gtdSourcesFile
	if _, err := toml.DecodeFile(resolved, &file); err != nil {
		if !explicit && os.IsNotExist(err) {
			return gtdSources{}, nil
		}
		return gtdSources{}, fmt.Errorf("load GTD sources: %w", err)
	}
	rules := make([]gtdSourceRule, 0, len(file.Sources))
	for _, rule := range file.Sources {
		rule.Sphere = strings.ToLower(strings.TrimSpace(rule.Sphere))
		rule.Provider = strings.ToLower(strings.TrimSpace(rule.Provider))
		rule.Ref = strings.TrimSpace(rule.Ref)
		if rule.Provider == "" || rule.Ref == "" {
			continue
		}
		rules = append(rules, rule)
	}
	return gtdSources{rules: rules}, nil
}

func (s gtdSources) writeable(note dedupNote, binding braingtd.SourceBinding) bool {
	provider := strings.ToLower(strings.TrimSpace(binding.Provider))
	ref := strings.TrimSpace(binding.Ref)
	sphere := strings.ToLower(strings.TrimSpace(string(note.Resolved.Sphere)))
	for _, rule := range s.rules {
		if !rule.Writeable || !sourceProviderMatches(rule.Provider, provider) || rule.Ref != ref {
			continue
		}
		if rule.Sphere == "" || rule.Sphere == sphere {
			return true
		}
	}
	return false
}

func sourceProviderMatches(ruleProvider, bindingProvider string) bool {
	rule := strings.ToLower(strings.TrimSpace(ruleProvider))
	binding := strings.ToLower(strings.TrimSpace(bindingProvider))
	return rule == binding || (rule == "mail" && gtdSyncProvider(binding) == "mail")
}

func copyArgs(args map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(args)+1)
	for key, value := range args {
		out[key] = value
	}
	return out
}
