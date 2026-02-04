package github

import (
	"fmt"
	"regexp"
	"strings"
)

// RepoInfo holds parsed GitHub owner/repo from a URL.
type RepoInfo struct {
	Owner string
	Repo  string
}

// APIPath returns the GitHub API path segment "owner/repo".
func (r RepoInfo) APIPath() string {
	return r.Owner + "/" + r.Repo
}

var (
	// git@github.com:owner/repo.git or git@github.com:owner/repo
	sshPattern = regexp.MustCompile(`^git@github\.com:([^/]+)/([^/]+?)(?:\.git)?$`)
	// https://github.com/owner/repo.git or https://github.com/owner/repo
	httpsPattern = regexp.MustCompile(`^https://github\.com/([^/]+)/([^/]+?)(?:\.git)?$`)
)

// ParseRepoURL extracts the GitHub owner and repo from a git URL.
// Returns an error if the URL is not a recognized GitHub URL.
func ParseRepoURL(url string) (RepoInfo, error) {
	if m := sshPattern.FindStringSubmatch(url); m != nil {
		return RepoInfo{Owner: m[1], Repo: m[2]}, nil
	}
	if m := httpsPattern.FindStringSubmatch(url); m != nil {
		return RepoInfo{Owner: m[1], Repo: m[2]}, nil
	}
	return RepoInfo{}, fmt.Errorf("not a GitHub URL: %s", url)
}

// IsGitHubURL returns true if the URL points to a GitHub repository.
func IsGitHubURL(url string) bool {
	return strings.Contains(url, "github.com")
}
