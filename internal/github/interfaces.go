//go:build !nogithub

package github

import (
	"time"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
)

// DiscoveryProvider defines the interface for PR discovery and lifecycle management.
// Consumers depend on this abstraction instead of the concrete Discovery type.
type DiscoveryProvider interface {
	GetPRs() ([]contracts.PullRequest, *time.Time, string)
	Refresh(repos []config.Repo) ([]contracts.PullRequest, *int, error)
	GetPublicRepos() []string
	FindPR(repoURL string, prNumber int) (contracts.PullRequest, bool)
	Seed(prs []contracts.PullRequest, publicRepos []string)
	SetTarget(target string, getRepos func() []config.Repo)
	Stop()
}

// Compile-time interface satisfaction check.
var _ DiscoveryProvider = (*Discovery)(nil)
