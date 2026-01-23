package workspace

import (
	"context"
	"fmt"
	"net/url"
	"os/exec"
	"strings"
)

// BuildGitBranchURL constructs a web URL for viewing a git branch.
// Returns nil if the repo URL or branch is empty, or if the URL cannot be parsed.
// Supports GitHub, GitLab, Bitbucket, and a generic fallback pattern.
func BuildGitBranchURL(repoURL, branch string) string {
	if repoURL == "" || branch == "" {
		return ""
	}

	// Remove .git suffix if present
	cleanRepoURL := strings.TrimSuffix(repoURL, ".git")

	// Parse URL to extract host, owner, and repo name
	var u *url.URL
	var err error

	// Handle SSH URLs (git@github.com:user/repo.git)
	if strings.HasPrefix(cleanRepoURL, "git@") {
		parts := strings.TrimPrefix(cleanRepoURL, "git@")
		colonIdx := strings.Index(parts, ":")
		if colonIdx == -1 {
			return ""
		}
		host := parts[:colonIdx]
		pathParts := strings.Split(parts[colonIdx+1:], "/")
		if len(pathParts) < 2 {
			return ""
		}
		owner := pathParts[0]
		repo := pathParts[1]
		// Build URL object for consistent handling
		u = &url.URL{
			Scheme: "https",
			Host:   host,
			Path:   fmt.Sprintf("%s/%s", owner, repo),
		}
	} else {
		u, err = url.Parse(cleanRepoURL)
		if err != nil {
			return ""
		}
	}

	hostname := u.Hostname()
	pathParts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(pathParts) < 2 {
		return ""
	}
	owner := pathParts[0]
	repo := pathParts[1]

	// Build branch URL based on git host
	switch hostname {
	case "github.com":
		return fmt.Sprintf("https://github.com/%s/%s/tree/%s", owner, repo, encodeBranch(branch))
	case "gitlab.com":
		return fmt.Sprintf("https://gitlab.com/%s/%s/-/tree/%s", owner, repo, encodeBranch(branch))
	case "bitbucket.org":
		return fmt.Sprintf("https://bitbucket.org/%s/%s/src/%s", owner, repo, encodeBranch(branch))
	default:
		// Generic pattern for other git hosts (assumes GitHub-like structure)
		scheme := "https"
		if u.Scheme != "" {
			scheme = u.Scheme
		}
		return fmt.Sprintf("%s://%s/%s/%s/tree/%s", scheme, hostname, owner, repo, encodeBranch(branch))
	}
}

// encodeBranch encodes a branch name for use in URLs.
// Preserves forward slashes (used in hierarchical branch names) but encodes other special characters.
func encodeBranch(branch string) string {
	parts := strings.Split(branch, "/")
	for i, part := range parts {
		// PathEscape encodes / as %2F, so convert it back to preserve slash separators
		parts[i] = strings.ReplaceAll(url.PathEscape(part), "%2F", "/")
	}
	return strings.Join(parts, "/")
}

// BranchHasUpstream checks if the current branch in a directory has a remote tracking branch (upstream).
// Returns true if @{u} (upstream) is configured for the current branch.
func BranchHasUpstream(ctx context.Context, dir string) bool {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	cmd.Dir = dir
	err := cmd.Run()
	return err == nil
}
