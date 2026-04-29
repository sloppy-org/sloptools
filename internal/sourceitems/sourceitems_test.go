package sourceitems

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/sloppy-org/sloptools/internal/providerdata"
)

func initGitRepo(t *testing.T, remote string) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, strings.TrimSpace(string(out)))
		}
	}
	run("init")
	run("remote", "add", "origin", remote)
	return dir
}

type scriptedResponse struct {
	prefix string
	output string
}

func scriptedRunner(responses []scriptedResponse, calls *[]string) commandRunner {
	return func(ctx context.Context, name string, args ...string) *exec.Cmd {
		call := strings.TrimSpace(strings.Join(append([]string{name}, args...), " "))
		*calls = append(*calls, call)
		for _, response := range responses {
			if strings.HasPrefix(call, response.prefix) {
				if response.output == "" {
					return exec.CommandContext(ctx, "sh", "-c", ":")
				}
				return exec.CommandContext(ctx, "sh", "-c", "printf %s \"$1\"", "sh", response.output)
			}
		}
		return exec.CommandContext(ctx, "sh", "-c", "echo unexpected >&2; exit 1")
	}
}

func marshalJSON(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(data)
}

func TestGitHubProviderListsAndMutatesAssignedItems(t *testing.T) {
	projectDir := initGitRepo(t, "https://github.com/sloppy-org/slopshell.git")
	updated := time.Date(2026, time.April, 29, 12, 0, 0, 0, time.UTC)
	calls := []string{}
	provider, err := NewGitHubProviderWithRunner(projectDir, scriptedRunner([]scriptedResponse{
		{prefix: "gh issue list", output: marshalJSON(t, []map[string]any{{
			"number": 12, "title": "Fix bug", "url": "https://github.com/sloppy-org/slopshell/issues/12", "state": "OPEN",
			"labels": []map[string]any{{"name": "gtd"}}, "assignees": []map[string]any{{"login": "ada"}}, "author": map[string]any{"login": "ada"},
			"updatedAt": updated.Format(time.RFC3339),
		}})},
		{prefix: "gh pr list", output: marshalJSON(t, []map[string]any{{
			"number": 51, "title": "Add source adapters", "url": "https://github.com/sloppy-org/slopshell/pull/51", "state": "OPEN",
			"labels": []map[string]any{{"name": "review"}}, "assignees": []map[string]any{{"login": "ada"}}, "author": map[string]any{"login": "ada"},
			"updatedAt": updated.Add(time.Minute).Format(time.RFC3339), "reviewDecision": "REVIEW_REQUIRED", "reviewRequests": []map[string]any{{"login": "octocat"}},
		}})},
		{prefix: "gh issue close", output: ""},
		{prefix: "gh pr close", output: ""},
		{prefix: "gh issue comment", output: ""},
		{prefix: "gh pr comment", output: ""},
	}, &calls))
	if err != nil {
		t.Fatalf("NewGitHubProviderWithRunner(): %v", err)
	}

	items, err := provider.List(context.Background())
	if err != nil {
		t.Fatalf("List(): %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2: %#v", len(items), items)
	}
	if items[0].SourceRef != "github:sloppy-org/slopshell#51" {
		t.Fatalf("items[0].SourceRef = %q, want github:sloppy-org/slopshell#51", items[0].SourceRef)
	}
	if items[0].ReviewStatus != "review_required" || len(items[0].Reviewers) != 1 || items[0].Reviewers[0] != "octocat" {
		t.Fatalf("items[0] review fields = %#v", items[0])
	}
	if items[1].SourceRef != "github:sloppy-org/slopshell#12" {
		t.Fatalf("items[1].SourceRef = %q, want github:sloppy-org/slopshell#12", items[1].SourceRef)
	}
	if items[1].Kind != "issue" || len(items[1].Assignees) != 1 || items[1].Assignees[0] != "ada" {
		t.Fatalf("items[1] = %#v", items[1])
	}

	if err := provider.Comment(context.Background(), items[0], "Please review the diff"); err != nil {
		t.Fatalf("Comment(): %v", err)
	}
	if err := provider.Close(context.Background(), items[1], "Done upstream"); err != nil {
		t.Fatalf("Close(): %v", err)
	}
	if got := strings.Join(calls, "\n"); !strings.Contains(got, "gh pr comment 51") || !strings.Contains(got, "gh issue close 12") {
		t.Fatalf("calls = %q", got)
	}
}

func TestGitLabProviderListsAndMutatesAssignedItems(t *testing.T) {
	projectDir := initGitRepo(t, "https://gitlab.com/sloppy-org/slopshell.git")
	updated := time.Date(2026, time.April, 29, 12, 0, 0, 0, time.UTC)
	calls := []string{}
	provider, err := NewGitLabProviderWithRunner(projectDir, scriptedRunner([]scriptedResponse{
		{prefix: "glab issue list", output: marshalJSON(t, []map[string]any{{
			"iid": 12, "title": "Fix bug", "web_url": "https://gitlab.com/sloppy-org/slopshell/-/issues/12", "state": "opened",
			"labels": []any{"gtd"}, "assignees": []map[string]any{{"username": "ada"}}, "author": map[string]any{"username": "ada"},
			"updated_at": updated.Format(time.RFC3339),
		}})},
		{prefix: "glab mr list", output: marshalJSON(t, []map[string]any{{
			"iid": 51, "title": "Add source adapters", "web_url": "https://gitlab.com/sloppy-org/slopshell/-/merge_requests/51", "state": "opened",
			"labels": []any{"review"}, "assignees": []map[string]any{{"username": "ada"}}, "author": map[string]any{"username": "ada"},
			"updated_at": updated.Add(time.Minute).Format(time.RFC3339), "reviewers": []map[string]any{{"username": "octocat"}},
		}})},
		{prefix: "glab issue note", output: ""},
		{prefix: "glab issue close", output: ""},
		{prefix: "glab mr note create", output: ""},
		{prefix: "glab mr close", output: ""},
	}, &calls))
	if err != nil {
		t.Fatalf("NewGitLabProviderWithRunner(): %v", err)
	}

	items, err := provider.List(context.Background())
	if err != nil {
		t.Fatalf("List(): %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2: %#v", len(items), items)
	}
	if items[0].SourceRef != "gitlab:sloppy-org/slopshell!51" {
		t.Fatalf("items[0].SourceRef = %q, want gitlab:sloppy-org/slopshell!51", items[0].SourceRef)
	}
	if items[0].ReviewStatus != "review_requested" || len(items[0].Reviewers) != 1 || items[0].Reviewers[0] != "octocat" {
		t.Fatalf("items[0] review fields = %#v", items[0])
	}
	if items[1].SourceRef != "gitlab:sloppy-org/slopshell#12" {
		t.Fatalf("items[1].SourceRef = %q, want gitlab:sloppy-org/slopshell#12", items[1].SourceRef)
	}
	if items[1].Kind != "issue" || len(items[1].Assignees) != 1 || items[1].Assignees[0] != "ada" {
		t.Fatalf("items[1] = %#v", items[1])
	}

	if err := provider.Comment(context.Background(), items[0], "Please review the diff"); err != nil {
		t.Fatalf("Comment(): %v", err)
	}
	if err := provider.Close(context.Background(), items[1], "Done upstream"); err != nil {
		t.Fatalf("Close(): %v", err)
	}
	if got := strings.Join(calls, "\n"); !strings.Contains(got, "glab mr note create 51") || !strings.Contains(got, "glab issue close 12") {
		t.Fatalf("calls = %q", got)
	}
}

func TestSourceItemMapIncludesCompactFields(t *testing.T) {
	updated := time.Date(2026, time.April, 29, 12, 0, 0, 0, time.UTC)
	item := providerdata.SourceItem{
		Provider:     GitHubProviderName,
		Kind:         "pull_request",
		Container:    "sloppy-org/slopshell",
		Number:       51,
		Title:        "Add source adapters",
		URL:          "https://github.com/sloppy-org/slopshell/pull/51",
		State:        "open",
		Labels:       []string{"gtd", "review"},
		Assignees:    []string{"ada"},
		Author:       "octocat",
		ReviewStatus: "review_requested",
		Reviewers:    []string{"grace"},
		SourceRef:    "github:sloppy-org/slopshell#51",
		UpdatedAt:    &updated,
	}
	payload := sourceItemMap(item)
	if payload["source_ref"] != "github:sloppy-org/slopshell#51" {
		t.Fatalf("payload source_ref = %#v", payload["source_ref"])
	}
	if got, ok := payload["labels"].([]string); !ok || len(got) != 2 {
		t.Fatalf("payload labels = %#v", payload["labels"])
	}
	if payload["updated_at"] != updated.UTC().Format(time.RFC3339) {
		t.Fatalf("payload updated_at = %#v", payload["updated_at"])
	}
}
