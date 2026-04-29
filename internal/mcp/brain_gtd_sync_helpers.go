package mcp

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	braingtd "github.com/sloppy-org/sloptools/internal/brain/gtd"
	"github.com/sloppy-org/sloptools/internal/store"
)

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
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return fmt.Errorf("%s %s: %s", name, strings.Join(args, " "), msg)
		}
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
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
	return status == "closed" || status == "done" || status == "dropped"
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
