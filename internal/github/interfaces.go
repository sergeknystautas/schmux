//go:build !nogithub

package github

import (
	"time"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/state"
)

// DiscoveryProvider defines the interface for PR discovery and lifecycle management.
// Consumers depend on this abstraction instead of the concrete Discovery type.
type DiscoveryProvider interface {
	GetPRs() ([]contracts.PullRequest, *time.Time, string)
	Refresh(repos []state.Repo, cfg *config.Config) ([]contracts.PullRequest, *int, error)
	FindPR(repoURL string, prNumber int) (contracts.PullRequest, bool)
	Seed(prs []contracts.PullRequest)
	SetTarget(target string, getRepos func() []state.Repo, cfg *config.Config)
	Stop()
}

// Compile-time interface satisfaction check.
var _ DiscoveryProvider = (*Discovery)(nil)
