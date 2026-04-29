package sourceitems

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/sloppy-org/sloptools/internal/providerdata"
)

type GitHubProvider struct {
	repoArg    string
	container  string
	projectDir string
	runner     commandRunner
}

func NewGitHubProvider(projectDir string) (*GitHubProvider, error) {
	return NewGitHubProviderWithRunner(projectDir, defaultCommandRunner)
}

func NewGitHubProviderWithRunner(projectDir string, runner commandRunner) (*GitHubProvider, error) {
	remote, err := loadRemote(projectDir)
	if err != nil {
		return nil, err
	}
	repoArg := githubRepoArg(remote)
	if strings.TrimSpace(repoArg) == "" {
		return nil, fmt.Errorf("unable to resolve GitHub repo from %q", remote)
	}
	return &GitHubProvider{
		repoArg:    repoArg,
		container:  containerFromGitRemote(remote),
		projectDir: projectDir,
		runner:     runner,
	}, nil
}

func (p *GitHubProvider) ProviderName() string { return GitHubProviderName }

func (p *GitHubProvider) List(ctx context.Context) ([]providerdata.SourceItem, error) {
	var items []providerdata.SourceItem
	for _, filter := range [][]string{{"--assignee", "@me"}, {"--author", "@me"}} {
		issues, err := p.listIssues(ctx, filter...)
		if err != nil {
			return nil, err
		}
		items = append(items, issues...)
	}
	for _, filter := range [][]string{{"--assignee", "@me"}, {"--author", "@me"}, {"--search", "review-requested:@me"}} {
		prs, err := p.listPullRequests(ctx, filter...)
		if err != nil {
			return nil, err
		}
		items = append(items, prs...)
	}
	return normalizeList(items), nil
}

func (p *GitHubProvider) Close(ctx context.Context, item providerdata.SourceItem, comment string) error {
	if item.Number <= 0 {
		return errors.New("source number is required")
	}
	args := []string{"-R", p.repoArg}
	if strings.TrimSpace(comment) != "" {
		args = append(args, "--comment", comment)
	}
	switch strings.ToLower(strings.TrimSpace(item.Kind)) {
	case "pull_request":
		args = append([]string{"pr", "close", strconv.FormatInt(item.Number, 10)}, args...)
	case "issue":
		args = append([]string{"issue", "close", strconv.FormatInt(item.Number, 10)}, args...)
	default:
		return fmt.Errorf("unsupported GitHub item kind %q", item.Kind)
	}
	return runCommand(ctx, p.runner, "gh", args...)
}

func (p *GitHubProvider) Comment(ctx context.Context, item providerdata.SourceItem, body string) error {
	if strings.TrimSpace(body) == "" {
		return errors.New("comment body is required")
	}
	if item.Number <= 0 {
		return errors.New("source number is required")
	}
	args := []string{"-R", p.repoArg, "--body", body}
	switch strings.ToLower(strings.TrimSpace(item.Kind)) {
	case "pull_request":
		args = append([]string{"pr", "comment", strconv.FormatInt(item.Number, 10)}, args...)
	case "issue":
		args = append([]string{"issue", "comment", strconv.FormatInt(item.Number, 10)}, args...)
	default:
		return fmt.Errorf("unsupported GitHub item kind %q", item.Kind)
	}
	return runCommand(ctx, p.runner, "gh", args...)
}

func (p *GitHubProvider) listIssues(ctx context.Context, filter ...string) ([]providerdata.SourceItem, error) {
	args := []string{"issue", "list", "-R", p.repoArg, "--state", "all", "--limit", "100", "--json", "number,title,url,state,labels,assignees,author,updatedAt,closedAt"}
	args = append(args, filter...)
	rows, err := runJSONList(ctx, p.runner, "gh", args...)
	if err != nil {
		return nil, err
	}
	out := make([]providerdata.SourceItem, 0, len(rows))
	for _, row := range rows {
		out = append(out, p.mapItem("issue", row))
	}
	return out, nil
}

func (p *GitHubProvider) listPullRequests(ctx context.Context, filter ...string) ([]providerdata.SourceItem, error) {
	args := []string{"pr", "list", "-R", p.repoArg, "--state", "all", "--limit", "100", "--json", "number,title,url,state,labels,assignees,author,updatedAt,closedAt,reviewDecision,reviewRequests,isDraft"}
	args = append(args, filter...)
	rows, err := runJSONList(ctx, p.runner, "gh", args...)
	if err != nil {
		return nil, err
	}
	out := make([]providerdata.SourceItem, 0, len(rows))
	for _, row := range rows {
		out = append(out, p.mapItem("pull_request", row))
	}
	return out, nil
}

func (p *GitHubProvider) mapItem(kind string, row map[string]any) providerdata.SourceItem {
	item := itemFromMap(GitHubProviderName, kind, p.container, row)
	if item.ReviewStatus == "" && kind == "pull_request" {
		item.ReviewStatus = strings.ToLower(strings.TrimSpace(firstString(row, "reviewDecision")))
	}
	if item.ReviewStatus == "" && kind == "pull_request" && boolValue(firstAny(row, "isDraft")) {
		item.ReviewStatus = "draft"
	}
	return item
}

func runCommand(ctx context.Context, runner commandRunner, name string, args ...string) error {
	cmd := runner(ctx, name, args...)
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
