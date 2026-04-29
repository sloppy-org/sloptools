package sourceitems

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/sloppy-org/sloptools/internal/providerdata"
)

type GitLabProvider struct {
	repoArg    string
	container  string
	projectDir string
	runner     commandRunner
}

func NewGitLabProvider(projectDir string) (*GitLabProvider, error) {
	return NewGitLabProviderWithRunner(projectDir, defaultCommandRunner)
}

func NewGitLabProviderWithRunner(projectDir string, runner commandRunner) (*GitLabProvider, error) {
	remote, err := loadRemote(projectDir)
	if err != nil {
		return nil, err
	}
	repoArg := gitlabRepoArg(remote)
	if strings.TrimSpace(repoArg) == "" {
		return nil, fmt.Errorf("unable to resolve GitLab repo from %q", remote)
	}
	return &GitLabProvider{
		repoArg:    repoArg,
		container:  containerFromGitRemote(remote),
		projectDir: projectDir,
		runner:     runner,
	}, nil
}

func (p *GitLabProvider) ProviderName() string { return GitLabProviderName }

func (p *GitLabProvider) List(ctx context.Context) ([]providerdata.SourceItem, error) {
	var items []providerdata.SourceItem
	for _, filter := range [][]string{{"--assignee=@me"}, {"--author=@me"}} {
		issues, err := p.listIssues(ctx, filter...)
		if err != nil {
			return nil, err
		}
		items = append(items, issues...)
	}
	for _, filter := range [][]string{{"--assignee=@me"}, {"--author=@me"}, {"--reviewer=@me"}} {
		mrs, err := p.listMergeRequests(ctx, filter...)
		if err != nil {
			return nil, err
		}
		items = append(items, mrs...)
	}
	return normalizeList(items), nil
}

func (p *GitLabProvider) Close(ctx context.Context, item providerdata.SourceItem, comment string) error {
	if item.Number <= 0 {
		return errors.New("source number is required")
	}
	if strings.TrimSpace(comment) != "" {
		if err := p.Comment(ctx, item, comment); err != nil {
			return err
		}
	}
	switch strings.ToLower(strings.TrimSpace(item.Kind)) {
	case "merge_request":
		return runCommand(ctx, p.runner, "glab", "mr", "close", strconv.FormatInt(item.Number, 10), "-R", p.repoArg)
	case "issue":
		return runCommand(ctx, p.runner, "glab", "issue", "close", strconv.FormatInt(item.Number, 10), "-R", p.repoArg)
	default:
		return fmt.Errorf("unsupported GitLab item kind %q", item.Kind)
	}
}

func (p *GitLabProvider) Comment(ctx context.Context, item providerdata.SourceItem, body string) error {
	if strings.TrimSpace(body) == "" {
		return errors.New("comment body is required")
	}
	if item.Number <= 0 {
		return errors.New("source number is required")
	}
	switch strings.ToLower(strings.TrimSpace(item.Kind)) {
	case "merge_request":
		return runCommand(ctx, p.runner, "glab", "mr", "note", "create", strconv.FormatInt(item.Number, 10), "-R", p.repoArg, "-m", body)
	case "issue":
		return runCommand(ctx, p.runner, "glab", "issue", "note", strconv.FormatInt(item.Number, 10), "-R", p.repoArg, "-m", body)
	default:
		return fmt.Errorf("unsupported GitLab item kind %q", item.Kind)
	}
}

func (p *GitLabProvider) listIssues(ctx context.Context, filter ...string) ([]providerdata.SourceItem, error) {
	args := []string{"issue", "list", "-R", p.repoArg, "--all", "--output", "json"}
	args = append(args, filter...)
	rows, err := runJSONList(ctx, p.runner, "glab", args...)
	if err != nil {
		return nil, err
	}
	out := make([]providerdata.SourceItem, 0, len(rows))
	for _, row := range rows {
		out = append(out, p.mapItem("issue", row))
	}
	return out, nil
}

func (p *GitLabProvider) listMergeRequests(ctx context.Context, filter ...string) ([]providerdata.SourceItem, error) {
	args := []string{"mr", "list", "-R", p.repoArg, "--all", "--output", "json"}
	args = append(args, filter...)
	rows, err := runJSONList(ctx, p.runner, "glab", args...)
	if err != nil {
		return nil, err
	}
	out := make([]providerdata.SourceItem, 0, len(rows))
	for _, row := range rows {
		out = append(out, p.mapItem("merge_request", row))
	}
	return out, nil
}

func (p *GitLabProvider) mapItem(kind string, row map[string]any) providerdata.SourceItem {
	item := itemFromMap(GitLabProviderName, kind, p.container, row)
	if item.Number == 0 {
		item.Number = numberValue(firstAny(row, "iid", "number", "id"))
		item.SourceRef = sourceRef(GitLabProviderName, kind, p.container, item.Number)
	}
	if item.ReviewStatus == "" && kind == "merge_request" {
		if len(item.Reviewers) > 0 {
			item.ReviewStatus = "review_requested"
		} else if boolValue(firstAny(row, "draft", "work_in_progress")) {
			item.ReviewStatus = "draft"
		}
	}
	return item
}
