package sourceitems

import (
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"strings"
)

func DetectProvider(projectDir string) (string, error) {
	remote, err := loadRemote(projectDir)
	if err != nil {
		return "", err
	}
	host := remoteHost(remote)
	switch {
	case strings.EqualFold(host, "github.com"):
		return GitHubProviderName, nil
	case strings.EqualFold(host, "gitlab.com"), strings.Contains(strings.ToLower(host), "gitlab"):
		return GitLabProviderName, nil
	default:
		return "", fmt.Errorf("unsupported git remote host %q", host)
	}
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

func remoteHost(remote string) string {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return ""
	}
	if strings.Contains(remote, "://") {
		parsed, err := url.Parse(remote)
		if err == nil {
			return strings.TrimSpace(parsed.Host)
		}
	}
	if strings.HasPrefix(remote, "git@") && strings.Contains(remote, ":") {
		parts := strings.SplitN(strings.TrimPrefix(remote, "git@"), ":", 2)
		return strings.TrimSpace(parts[0])
	}
	return ""
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
			path := remotePath(parsed)
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
			path := remotePath(parsed)
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
		return strings.TrimSuffix(after, ".git")
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
			path := remotePath(parsed)
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

func remotePath(parsed *url.URL) string {
	path := strings.Trim(strings.TrimPrefix(parsed.Path, "/"), "/")
	return strings.TrimSuffix(path, ".git")
}
