package workspace

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	gh "github.com/sergeknystautas/schmux/internal/github"
	"github.com/sergeknystautas/schmux/internal/state"
)

// CheckoutPR creates a workspace from a GitHub pull request.
//
// When the PR's head branch lives on origin (the common same-repo case), the
// workspace is created directly on that branch so it gets a normal
// remote-tracking setup — which is what CI status, PR chips, and upstream
// drift detection all key off. Fetching refs/pull/N/head into a synthetic
// "pr/N" branch instead would strand the workspace with no remote counterpart.
//
// Fork PRs have no origin branch, so they still go through the PR ref.
func (m *Manager) CheckoutPR(ctx context.Context, pr contracts.PullRequest) (*state.Workspace, error) {
	if repo, found := m.findRepoByURL(pr.RepoURL); found && !IsGitVCS(repo.VCS) {
		return nil, fmt.Errorf("PR checkout is not supported for %s repos", repo.VCS)
	}

	branchName, err := m.prBranchToCheckout(ctx, pr)
	if err != nil {
		return nil, err
	}

	// GetOrCreate acquires its own repo lock.
	w, err := m.GetOrCreate(ctx, pr.RepoURL, branchName)
	if err != nil {
		return nil, fmt.Errorf("failed to create workspace for PR #%d: %w", pr.Number, err)
	}

	return w, nil
}

// prBranchToCheckout resolves which branch the PR workspace should sit on,
// fetching whatever ref that requires. It prefers the PR's real head branch on
// origin and falls back to a synthetic branch built from refs/pull/N/head.
func (m *Manager) prBranchToCheckout(ctx context.Context, pr contracts.PullRequest) (string, error) {
	if pr.SourceBranch != "" && ValidateBranchName(pr.SourceBranch) == nil {
		onOrigin, err := m.originHasBranch(ctx, pr.RepoURL, pr.SourceBranch)
		if err != nil {
			return "", err
		}
		if onOrigin {
			m.logger.Info("PR checkout using origin branch",
				"pr", pr.Number, "branch", pr.SourceBranch)
			return pr.SourceBranch, nil
		}
	}

	// Fork PR, or a head branch that is not on origin: fall back to the PR ref.
	branchName := gh.PRBranchName(pr)
	if err := ValidateBranchName(branchName); err != nil {
		return "", fmt.Errorf("invalid PR branch name %q: %w", branchName, err)
	}
	m.logger.Info("PR checkout using PR ref; head branch not on origin",
		"pr", pr.Number, "branch", branchName)
	if err := m.fetchPRRef(ctx, pr.RepoURL, pr.Number, branchName); err != nil {
		return "", err
	}
	return branchName, nil
}

// originHasBranch fetches origin and reports whether the branch exists there.
func (m *Manager) originHasBranch(ctx context.Context, repoURL, branch string) (bool, error) {
	lock := m.repoLock(repoURL)
	lock.Lock()
	defer lock.Unlock()

	worktreeBasePath, err := m.backendFor(repoURL).EnsureRepoBase(ctx, repoURL, "")
	if err != nil {
		return false, fmt.Errorf("failed to ensure worktree base: %w", err)
	}

	// Refresh remote refs so a branch pushed after the last fetch is visible.
	if _, err := m.runGit(ctx, "", RefreshTriggerExplicit, worktreeBasePath, "fetch", "--prune", "origin"); err != nil {
		// Non-fatal: fall back to whatever refs we already have.
		m.logger.Warn("PR checkout: origin fetch failed", "repo", repoURL, "err", err)
	}

	exists, err := m.gitRemoteBranchExists(ctx, worktreeBasePath, branch)
	if err != nil {
		return false, fmt.Errorf("checking origin for branch %q: %w", branch, err)
	}
	return exists, nil
}

// fetchPRRef fetches a GitHub PR ref into the bare clone.
func (m *Manager) fetchPRRef(ctx context.Context, repoURL string, prNumber int, branchName string) error {
	lock := m.repoLock(repoURL)
	lock.Lock()
	defer lock.Unlock()

	worktreeBasePath, err := m.backendFor(repoURL).EnsureRepoBase(ctx, repoURL, "")
	if err != nil {
		return fmt.Errorf("failed to ensure worktree base: %w", err)
	}

	refSpec := fmt.Sprintf("refs/pull/%d/head:refs/heads/%s", prNumber, branchName)
	m.logger.Info("fetching PR ref", "refSpec", refSpec)
	fetchCmd := exec.CommandContext(ctx, "git", "fetch", "-f", "origin", refSpec)
	fetchCmd.Dir = worktreeBasePath
	if output, err := fetchCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to fetch PR ref: %s: %w", string(output), err)
	}

	return nil
}
