//go:build nogithub

package github

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/log"
	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
)

// DiscoveryProvider defines the interface for PR discovery and lifecycle management.
type DiscoveryProvider interface {
	GetPRs() ([]contracts.PullRequest, *time.Time, string)
	Refresh(repos []config.Repo) ([]contracts.PullRequest, *int, error)
	GetPublicRepos() []string
	FindPR(repoURL string, prNumber int) (contracts.PullRequest, bool)
	Seed(prs []contracts.PullRequest, publicRepos []string)
	SetTarget(target string, getRepos func() []config.Repo)
	Stop()
}

// Discovery is a no-op stub when the GitHub module is excluded.
type Discovery struct{}

// Compile-time interface satisfaction check.
var _ DiscoveryProvider = (*Discovery)(nil)

// NewDiscovery returns a disabled discovery instance.
func NewDiscovery(_ *log.Logger) *Discovery {
	return &Discovery{}
}

func (d *Discovery) GetPRs() ([]contracts.PullRequest, *time.Time, string) {
	return nil, nil, ""
}

func (d *Discovery) Refresh(_ []config.Repo) ([]contracts.PullRequest, *int, error) {
	return nil, nil, fmt.Errorf("GitHub integration is not available in this build")
}

func (d *Discovery) GetPublicRepos() []string { return nil }

func (d *Discovery) FindPR(_ string, _ int) (contracts.PullRequest, bool) {
	return contracts.PullRequest{}, false
}

func (d *Discovery) Seed(_ []contracts.PullRequest, _ []string) {}

func (d *Discovery) SetTarget(_ string, _ func() []config.Repo) {}

func (d *Discovery) Stop() {}

// CheckAuth returns an empty status when the GitHub module is excluded.
func CheckAuth(_ context.Context) contracts.GitHubStatus {
	return contracts.GitHubStatus{}
}

// BuildReviewPrompt returns an empty string when the GitHub module is excluded.
func BuildReviewPrompt(_ contracts.PullRequest, _, _ string) string {
	return ""
}

// PRBranchName returns an empty string when the GitHub module is excluded.
func PRBranchName(_ contracts.PullRequest) string {
	return ""
}

// RepoInfo holds parsed GitHub owner/repo from a URL.
type RepoInfo struct {
	Owner string
	Repo  string
}

// APIPath returns the GitHub API path segment "owner/repo".
func (r RepoInfo) APIPath() string {
	return r.Owner + "/" + r.Repo
}

// ParseRepoURL returns an error when the GitHub module is excluded.
func ParseRepoURL(_ string) (RepoInfo, error) {
	return RepoInfo{}, fmt.Errorf("GitHub integration is not available in this build")
}

// IsGitHubURL returns false when the GitHub module is excluded.
func IsGitHubURL(_ string) bool {
	return false
}

// RateLimitError is returned when the GitHub API rate limit is exceeded.
type RateLimitError struct {
	RetryAfterSec int
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("GitHub API rate limit exceeded, retry after %d seconds", e.RetryAfterSec)
}

// CheckVisibility returns false when the GitHub module is excluded.
func CheckVisibility(_ RepoInfo) (bool, error) {
	return false, fmt.Errorf("GitHub integration is not available in this build")
}

// FetchOpenPRs returns an error when the GitHub module is excluded.
func FetchOpenPRs(_ RepoInfo, _, _ string) ([]contracts.PullRequest, error) {
	return nil, fmt.Errorf("GitHub integration is not available in this build")
}

// IsAvailable reports whether the GitHub module is included in this build.
func IsAvailable() bool { return false }
