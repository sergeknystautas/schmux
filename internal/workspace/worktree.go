package workspace

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"

	"github.com/sergeknystautas/schmux/internal/state"
)

// cloneBareRepo clones a repository as a bare clone and configures it for fetching.
// Note: git clone --bare doesn't set up fetch refspecs by default (it's designed for
// servers). We add the refspec so that 'git fetch' creates remote tracking branches.
func (m *Manager) cloneBareRepo(ctx context.Context, url, path string) error {
	m.logger.Info("cloning bare repository", "url", url, "path", path)

	if _, err := m.runGit(ctx, "", RefreshTriggerExplicit, "", "clone", "--bare", url, path); err != nil {
		return fmt.Errorf("git clone --bare failed: %w", err)
	}

	// Configure fetch refspec so 'git fetch' creates remote tracking branches
	// Without this, origin/main won't exist after fetch
	if _, err := m.runGit(ctx, "", RefreshTriggerExplicit, path, "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*"); err != nil {
		return fmt.Errorf("git config fetch refspec failed: %w", err)
	}

	m.logger.Info("bare repository cloned", "path", path)
	return nil
}

// ensureWorktreeBase creates or returns an existing bare clone for a repo URL.
//
// Race condition handling: If two requests try to create the same worktree base
// concurrently, git clone --bare will fail for the second request because the
// directory already exists. This is acceptable because:
//  1. The first clone will succeed and create the repo
//  2. The second clone fails with "already exists" error
//  3. The caller (create()) will fail, but a retry will find the existing repo
//
// In practice, this race is rare since workspace creation is typically sequential
// through the API. The state.AddWorktreeBase() call is also idempotent (updates if exists).
//
// Path determination: Uses repo.BarePath from config (set during migration or repo creation).
func (m *Manager) ensureWorktreeBase(ctx context.Context, repoURL string) (string, error) {
	// Check if worktree base already exists in state
	if wb, found := m.state.GetWorktreeBaseByURL(repoURL); found {
		// Verify it still exists on disk (handles external deletion)
		if _, err := os.Stat(wb.Path); err == nil {
			m.logger.Debug("using existing worktree base", "url", repoURL, "path", wb.Path)
			return wb.Path, nil
		}
		m.logger.Warn("worktree base missing on disk, will recreate", "url", repoURL)
	}

	// Get BarePath from config
	repo, found := m.config.FindRepoByURL(repoURL)
	if !found {
		return "", fmt.Errorf("repo not found in config: %s", repoURL)
	}
	if repo.BarePath == "" {
		return "", fmt.Errorf("repo %s has no bare_path set", repo.Name)
	}

	worktreeBasePath := filepath.Join(m.config.GetWorktreeBasePath(), repo.BarePath)

	// Create parent directory (e.g., ~/.schmux/repos/facebook/)
	if err := os.MkdirAll(filepath.Dir(worktreeBasePath), 0755); err != nil {
		return "", fmt.Errorf("failed to create repo directory: %w", err)
	}

	// Clone as bare repo (may fail if concurrent request already created it)
	if err := m.cloneBareRepo(ctx, repoURL, worktreeBasePath); err != nil {
		// Check if it failed because directory already exists (race condition)
		if _, statErr := os.Stat(worktreeBasePath); statErr == nil {
			m.logger.Info("worktree base created by concurrent request, using existing", "path", worktreeBasePath)
			// Fall through to add to state (idempotent)
		} else {
			return "", err
		}
	}

	// Track in state
	if err := m.state.AddWorktreeBase(state.WorktreeBase{RepoURL: repoURL, Path: worktreeBasePath}); err != nil {
		return "", fmt.Errorf("failed to add worktree base to state: %w", err)
	}
	if err := m.state.Save(); err != nil {
		return "", fmt.Errorf("failed to save state: %w", err)
	}

	return worktreeBasePath, nil
}

// addWorktree adds a worktree from a worktree base.
func (m *Manager) addWorktree(ctx context.Context, worktreeBasePath, workspacePath, branch, repoURL string) error {
	m.logger.Info("adding worktree", "base", worktreeBasePath, "path", workspacePath, "branch", branch)

	// Check if local branch exists
	localBranchExists := m.runGitErr(ctx, "", RefreshTriggerExplicit, worktreeBasePath, "show-ref", "--verify", "--quiet", "refs/heads/"+branch) == nil

	// Check if remote branch exists
	remoteBranch := "origin/" + branch
	remoteBranchExists := m.runGitErr(ctx, "", RefreshTriggerExplicit, worktreeBasePath, "show-ref", "--verify", "--quiet", "refs/remotes/"+remoteBranch) == nil

	// When both local and remote branches exist, check if local has diverged
	// from remote (e.g., after a force-push of the default branch). If diverged,
	// reset the local ref to match remote so the worktree gets the current history.
	// Only safe when the branch is NOT checked out in another worktree.
	if localBranchExists && remoteBranchExists && !m.isBranchInWorktree(ctx, worktreeBasePath, branch) {
		localRef := "refs/heads/" + branch
		remoteRef := "refs/remotes/origin/" + branch
		if m.runGitErr(ctx, "", RefreshTriggerExplicit, worktreeBasePath, "merge-base", "--is-ancestor", localRef, remoteRef) != nil {
			// Local branch has diverged from remote — reset to match remote
			m.logger.Info("local branch diverged from origin, resetting", "branch", branch)
			if _, err := m.runGit(ctx, "", RefreshTriggerExplicit, worktreeBasePath, "update-ref", localRef, remoteRef); err != nil {
				return fmt.Errorf("failed to reset diverged local %s: %w", branch, err)
			}
		}
	}

	var args []string
	if localBranchExists {
		// Branch exists locally - check it out directly (no -b)
		args = []string{"worktree", "add", workspacePath, branch}
	} else if remoteBranchExists {
		// Track existing remote branch (create local branch)
		args = []string{"worktree", "add", "--track", "-b", branch, workspacePath, remoteBranch}
	} else {
		// Create new local branch from default branch (ensures we start from latest)
		// Default branch is required to create a new branch from origin/<default>
		defaultBranch, err := m.GetDefaultBranch(ctx, repoURL)
		if err != nil {
			return fmt.Errorf("failed to get default branch: %w", err)
		}
		args = []string{"worktree", "add", "-b", branch, workspacePath, "origin/" + defaultBranch}
	}

	if _, err := m.runGit(ctx, "", RefreshTriggerExplicit, worktreeBasePath, args...); err != nil {
		return fmt.Errorf("git worktree add failed: %w", err)
	}

	m.logger.Info("worktree added", "path", workspacePath)
	return nil
}

// ensureUniqueBranch returns a unique branch name if the requested one is already
// checked out by another worktree. The new branch is created from the requested
// branch's tip (origin/<branch> preferred, else local).
func (m *Manager) ensureUniqueBranch(ctx context.Context, worktreeBasePath, branch string) (string, bool, error) {
	if !m.isBranchInWorktree(ctx, worktreeBasePath, branch) {
		return branch, false, nil
	}

	sourceRef, err := m.branchSourceRef(ctx, worktreeBasePath, branch)
	if err != nil {
		return "", false, err
	}

	for i := 0; i < 10; i++ {
		suffix := m.randSuffix(3)
		candidate := fmt.Sprintf("%s-%s", branch, suffix)
		if m.isBranchInWorktree(ctx, worktreeBasePath, candidate) {
			continue
		}
		if m.localBranchExists(ctx, worktreeBasePath, candidate) {
			continue
		}
		if err := m.createBranchFromRef(ctx, worktreeBasePath, candidate, sourceRef); err != nil {
			return "", false, err
		}
		return candidate, true, nil
	}

	return "", false, fmt.Errorf("could not find a unique branch name for %s", branch)
}

func (m *Manager) branchSourceRef(ctx context.Context, worktreeBasePath, branch string) (string, error) {
	remoteRef := "refs/remotes/origin/" + branch
	if m.runGitErr(ctx, "", RefreshTriggerExplicit, worktreeBasePath, "show-ref", "--verify", "--quiet", remoteRef) == nil {
		return "origin/" + branch, nil
	}

	localRef := "refs/heads/" + branch
	if m.runGitErr(ctx, "", RefreshTriggerExplicit, worktreeBasePath, "show-ref", "--verify", "--quiet", localRef) == nil {
		return branch, nil
	}

	return "", fmt.Errorf("branch %s not found in worktree base", branch)
}

func (m *Manager) localBranchExists(ctx context.Context, worktreeBasePath, branch string) bool {
	localRef := "refs/heads/" + branch
	return m.runGitErr(ctx, "", RefreshTriggerExplicit, worktreeBasePath, "show-ref", "--verify", "--quiet", localRef) == nil
}

func (m *Manager) createBranchFromRef(ctx context.Context, worktreeBasePath, branch, sourceRef string) error {
	if _, err := m.runGit(ctx, "", RefreshTriggerExplicit, worktreeBasePath, "branch", branch, sourceRef); err != nil {
		return fmt.Errorf("git branch %s %s failed: %w", branch, sourceRef, err)
	}
	return nil
}

func (m *Manager) deleteBranch(ctx context.Context, worktreeBasePath, branch string) error {
	if _, err := m.runGit(ctx, "", RefreshTriggerExplicit, worktreeBasePath, "branch", "-D", branch); err != nil {
		return fmt.Errorf("git branch -D %s failed: %w", branch, err)
	}
	return nil
}

func defaultRandSuffix(length int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

// isBranchInWorktree checks if a branch is already checked out in any worktree.
// Uses `git worktree list --porcelain` for stable, machine-readable output.
func (m *Manager) isBranchInWorktree(ctx context.Context, worktreeBasePath, branch string) bool {
	return m.isBranchInWorktreeWithCache(ctx, worktreeBasePath, branch, nil)
}

// isBranchInWorktreeWithCache is like isBranchInWorktree but uses a per-round
// cache to avoid redundant `git worktree list` calls when multiple workspaces
// share the same bare clone within a single poll sweep.
func (m *Manager) isBranchInWorktreeWithCache(ctx context.Context, worktreeBasePath, branch string, cache *worktreeListCache) bool {
	var output []byte
	var err error

	if cache != nil {
		key := filepath.Clean(worktreeBasePath)
		output, err = cache.Get(ctx, key, func() ([]byte, error) {
			return m.runGit(ctx, "", RefreshTriggerExplicit, worktreeBasePath, "worktree", "list", "--porcelain")
		})
	} else {
		output, err = m.runGit(ctx, "", RefreshTriggerExplicit, worktreeBasePath, "worktree", "list", "--porcelain")
	}

	if err != nil {
		return false // If we can't check, assume not in use
	}

	// Porcelain format outputs "branch refs/heads/<name>" for each worktree
	// Detached HEAD worktrees have "detached" instead of "branch ..."
	searchStr := "branch refs/heads/" + branch
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if line == searchStr {
			return true
		}
	}
	return false
}

// removeWorktree removes a worktree.
func (m *Manager) removeWorktree(ctx context.Context, worktreeBasePath, workspacePath string) error {
	m.logger.Info("removing worktree", "base", worktreeBasePath, "path", workspacePath)

	if _, err := m.runGit(ctx, "", RefreshTriggerExplicit, worktreeBasePath, "worktree", "remove", "--force", workspacePath); err != nil {
		return fmt.Errorf("git worktree remove failed: %w", err)
	}

	m.logger.Info("worktree removed", "path", workspacePath)
	return nil
}

// pruneWorktrees runs git worktree prune to clean up stale worktree references.
// This removes worktree metadata for worktrees whose directories no longer exist.
func (m *Manager) pruneWorktrees(ctx context.Context, worktreeBasePath string) error {
	m.logger.Debug("pruning stale worktrees", "base", worktreeBasePath)

	if _, err := m.runGit(ctx, "", RefreshTriggerExplicit, worktreeBasePath, "worktree", "prune"); err != nil {
		return fmt.Errorf("git worktree prune failed: %w", err)
	}

	m.logger.Debug("worktrees pruned", "base", worktreeBasePath)
	return nil
}

// initLocalRepo initializes a new local git repository at the given path.
// It creates the directory, runs git init, creates the initial branch, and makes an empty commit.
func (m *Manager) initLocalRepo(ctx context.Context, path, branch string) error {
	m.logger.Info("initializing local repository", "path", path, "branch", branch)

	// Create the directory
	if err := os.MkdirAll(path, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Run git init
	if _, err := m.runGit(ctx, "", RefreshTriggerExplicit, path, "init"); err != nil {
		return fmt.Errorf("git init failed: %w", err)
	}

	// Configure user for initial commit (required for git commit)
	if _, err := m.runGit(ctx, "", RefreshTriggerExplicit, path, "config", "user.email", "schmux@localhost"); err != nil {
		return fmt.Errorf("git config user.email failed: %w", err)
	}

	if _, err := m.runGit(ctx, "", RefreshTriggerExplicit, path, "config", "user.name", "schmux"); err != nil {
		return fmt.Errorf("git config user.name failed: %w", err)
	}

	// Create and checkout the branch
	if _, err := m.runGit(ctx, "", RefreshTriggerExplicit, path, "checkout", "-b", branch); err != nil {
		return fmt.Errorf("git checkout -b %s failed: %w", branch, err)
	}

	// Create an empty commit for a valid git state
	if _, err := m.runGit(ctx, "", RefreshTriggerExplicit, path, "commit", "--allow-empty", "-m", "Initial commit"); err != nil {
		return fmt.Errorf("git commit failed: %w", err)
	}

	m.logger.Info("local repository initialized", "path", path)
	return nil
}

// cloneRepo clones a repository to the given path.
// Deprecated: Use ensureWorktreeBase + addWorktree for new workspaces.
func (m *Manager) cloneRepo(ctx context.Context, url, path string) error {
	m.logger.Info("cloning repository", "url", url, "path", path)

	if _, err := m.runGit(ctx, "", RefreshTriggerExplicit, "", "clone", url, path); err != nil {
		return fmt.Errorf("git clone failed: %w", err)
	}

	m.logger.Info("repository cloned", "path", path)
	return nil
}

// cleanupLocalBranch deletes a workspace's local branch from the bare clone
// if the branch was never pushed to the remote. This is best-effort; errors
// are logged but not fatal.
func (m *Manager) cleanupLocalBranch(ctx context.Context, worktreeBasePath string, w state.Workspace) {
	// Skip default branch
	defaultBranch, err := m.GetDefaultBranch(ctx, w.Repo)
	if err == nil && w.Branch == defaultBranch {
		return
	}

	// Skip if branch is checked out by another worktree
	if m.isBranchInWorktree(ctx, worktreeBasePath, w.Branch) {
		return
	}

	// Check if branch exists on remote
	remoteBranchExists, err := m.gitRemoteBranchExists(ctx, worktreeBasePath, w.Branch)
	if err != nil {
		m.logger.Warn("could not check remote branch", "err", err)
		return
	}
	if remoteBranchExists {
		return // branch was pushed, keep it
	}

	// Delete local branch
	if err := m.deleteBranch(ctx, worktreeBasePath, w.Branch); err != nil {
		m.logger.Warn("failed to delete local branch", "branch", w.Branch, "err", err)
	} else {
		m.logger.Info("deleted local branch", "branch", w.Branch)
	}
}

// findWorktreeBaseForWorkspace finds the worktree base path for a workspace.
// First tries to read the .git file (if directory exists), then falls back
// to looking up the worktree base by URL in state (works even if directory is gone).
func (m *Manager) findWorktreeBaseForWorkspace(w state.Workspace) (string, error) {
	// Try to resolve from .git file (works for worktrees when directory exists)
	if isWorktree(w.Path) {
		if worktreeBase, err := resolveWorktreeBaseFromWorktree(w.Path); err == nil {
			return worktreeBase, nil
		}
	}

	// Fall back to looking up worktree base by URL in state
	// This works even when the workspace directory has been deleted
	if wb, found := m.state.GetWorktreeBaseByURL(w.Repo); found {
		return wb.Path, nil
	}

	return "", fmt.Errorf("could not find worktree base for workspace: %s", w.ID)
}
