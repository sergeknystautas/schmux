package workspace

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	gh "github.com/sergeknystautas/schmux/internal/github"
	"github.com/sergeknystautas/schmux/internal/state"
)

// CheckoutPR creates a workspace from a GitHub pull request ref.
// It fetches the PR ref into the bare clone, then creates/reuses a workspace
// on the PR branch.
func (m *Manager) CheckoutPR(ctx context.Context, pr contracts.PullRequest) (*state.Workspace, error) {
	branchName := gh.PRBranchName(pr)

	if err := ValidateBranchName(branchName); err != nil {
		return nil, fmt.Errorf("invalid PR branch name %q: %w", branchName, err)
	}

	// Fetch the PR ref into the bare clone (under repo lock)
	if err := m.fetchPRRef(ctx, pr.RepoURL, pr.Number, branchName); err != nil {
		return nil, err
	}

	// Now use the standard GetOrCreate flow — the branch exists in the bare clone.
	// GetOrCreate acquires its own repo lock.
	w, err := m.GetOrCreate(ctx, pr.RepoURL, branchName)
	if err != nil {
		return nil, fmt.Errorf("failed to create workspace for PR #%d: %w", pr.Number, err)
	}

	return w, nil
}

// fetchPRRef fetches a GitHub PR ref into the bare clone.
func (m *Manager) fetchPRRef(ctx context.Context, repoURL string, prNumber int, branchName string) error {
	lock := m.repoLock(repoURL)
	lock.Lock()
	defer lock.Unlock()

	worktreeBasePath, err := m.ensureWorktreeBase(ctx, repoURL)
	if err != nil {
		return fmt.Errorf("failed to ensure worktree base: %w", err)
	}

	refSpec := fmt.Sprintf("refs/pull/%d/head:refs/heads/%s", prNumber, branchName)
	fmt.Printf("[workspace] fetching PR ref: %s\n", refSpec)
	fetchCmd := exec.CommandContext(ctx, "git", "fetch", "-f", "origin", refSpec)
	fetchCmd.Dir = worktreeBasePath
	if output, err := fetchCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to fetch PR ref: %s: %w", string(output), err)
	}

	return nil
}
