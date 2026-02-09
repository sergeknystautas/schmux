package workspace

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sergeknystautas/schmux/internal/state"
)

// cloneBareRepo clones a repository as a bare clone and configures it for fetching.
// Note: git clone --bare doesn't set up fetch refspecs by default (it's designed for
// servers). We add the refspec so that 'git fetch' creates remote tracking branches.
func (m *Manager) cloneBareRepo(ctx context.Context, url, path string) error {
	fmt.Printf("[workspace] cloning bare repository: url=%s path=%s\n", url, path)
	args := []string{"clone", "--bare", url, path}
	cmd := exec.CommandContext(ctx, "git", args...)

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git clone --bare failed: %w: %s", err, string(output))
	}

	// Configure fetch refspec so 'git fetch' creates remote tracking branches
	// Without this, origin/main won't exist after fetch
	configCmd := exec.CommandContext(ctx, "git", "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
	configCmd.Dir = path
	if output, err := configCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git config fetch refspec failed: %w: %s", err, string(output))
	}

	fmt.Printf("[workspace] bare repository cloned: path=%s\n", path)
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
// Backward compatibility: State already tracks worktree bases by URL. Existing repos
// with old flat paths (repo.git) continue to work via state lookup or legacy detection.
// New repos use namespaced paths (owner/repo.git) to avoid fork collisions.
func (m *Manager) ensureWorktreeBase(ctx context.Context, repoURL string) (string, error) {
	// Check if worktree base already exists in state (handles legacy paths)
	if wb, found := m.state.GetWorktreeBaseByURL(repoURL); found {
		// Verify it still exists on disk (handles external deletion)
		if _, err := os.Stat(wb.Path); err == nil {
			fmt.Printf("[workspace] using existing worktree base: url=%s path=%s\n", repoURL, wb.Path)
			return wb.Path, nil
		}
		fmt.Printf("[workspace] worktree base missing on disk, will recreate: url=%s\n", repoURL)
	}

	// Check for legacy flat path (not in state but exists on disk)
	if legacyPath := legacyBareRepoPath(m.config.GetWorktreeBasePath(), repoURL); legacyPath != "" {
		fmt.Printf("[workspace] using legacy worktree base: url=%s path=%s\n", repoURL, legacyPath)
		// Track in state for future lookups
		if err := m.state.AddWorktreeBase(state.WorktreeBase{RepoURL: repoURL, Path: legacyPath}); err != nil {
			return "", fmt.Errorf("failed to add worktree base to state: %w", err)
		}
		if err := m.state.Save(); err != nil {
			return "", fmt.Errorf("failed to save state: %w", err)
		}
		return legacyPath, nil
	}

	// Use namespaced path for new repos (owner/repo.git for GitHub, repo.git for others)
	repoPath := extractRepoPath(repoURL)
	worktreeBasePath := filepath.Join(m.config.GetWorktreeBasePath(), repoPath+".git")

	// Create parent directory (e.g., ~/.schmux/repos/facebook/)
	if err := os.MkdirAll(filepath.Dir(worktreeBasePath), 0755); err != nil {
		return "", fmt.Errorf("failed to create repo directory: %w", err)
	}

	// Clone as bare repo (may fail if concurrent request already created it)
	if err := m.cloneBareRepo(ctx, repoURL, worktreeBasePath); err != nil {
		// Check if it failed because directory already exists (race condition)
		if _, statErr := os.Stat(worktreeBasePath); statErr == nil {
			fmt.Printf("[workspace] worktree base created by concurrent request, using existing: %s\n", worktreeBasePath)
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
	fmt.Printf("[workspace] adding worktree: base=%s path=%s branch=%s\n", worktreeBasePath, workspacePath, branch)

	// Check if local branch exists
	localBranchCmd := exec.CommandContext(ctx, "git", "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	localBranchCmd.Dir = worktreeBasePath
	localBranchExists := localBranchCmd.Run() == nil

	// Check if remote branch exists
	remoteBranch := "origin/" + branch
	remoteBranchCmd := exec.CommandContext(ctx, "git", "show-ref", "--verify", "--quiet", "refs/remotes/"+remoteBranch)
	remoteBranchCmd.Dir = worktreeBasePath
	remoteBranchExists := remoteBranchCmd.Run() == nil

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

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = worktreeBasePath

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git worktree add failed: %w: %s", err, string(output))
	}

	fmt.Printf("[workspace] worktree added: path=%s\n", workspacePath)
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
	remoteCmd := exec.CommandContext(ctx, "git", "show-ref", "--verify", "--quiet", remoteRef)
	remoteCmd.Dir = worktreeBasePath
	if remoteCmd.Run() == nil {
		return "origin/" + branch, nil
	}

	localRef := "refs/heads/" + branch
	localCmd := exec.CommandContext(ctx, "git", "show-ref", "--verify", "--quiet", localRef)
	localCmd.Dir = worktreeBasePath
	if localCmd.Run() == nil {
		return branch, nil
	}

	return "", fmt.Errorf("branch %s not found in worktree base", branch)
}

func (m *Manager) localBranchExists(ctx context.Context, worktreeBasePath, branch string) bool {
	localRef := "refs/heads/" + branch
	localCmd := exec.CommandContext(ctx, "git", "show-ref", "--verify", "--quiet", localRef)
	localCmd.Dir = worktreeBasePath
	return localCmd.Run() == nil
}

func (m *Manager) createBranchFromRef(ctx context.Context, worktreeBasePath, branch, sourceRef string) error {
	cmd := exec.CommandContext(ctx, "git", "branch", branch, sourceRef)
	cmd.Dir = worktreeBasePath
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git branch %s %s failed: %w: %s", branch, sourceRef, err, string(output))
	}
	return nil
}

func (m *Manager) deleteBranch(ctx context.Context, worktreeBasePath, branch string) error {
	cmd := exec.CommandContext(ctx, "git", "branch", "-D", branch)
	cmd.Dir = worktreeBasePath
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git branch -D %s failed: %w: %s", branch, err, string(output))
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
	cmd := exec.CommandContext(ctx, "git", "worktree", "list", "--porcelain")
	cmd.Dir = worktreeBasePath

	output, err := cmd.Output()
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
	fmt.Printf("[workspace] removing worktree: base=%s path=%s\n", worktreeBasePath, workspacePath)

	args := []string{"worktree", "remove", "--force", workspacePath}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = worktreeBasePath

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git worktree remove failed: %w: %s", err, string(output))
	}

	fmt.Printf("[workspace] worktree removed: path=%s\n", workspacePath)
	return nil
}

// pruneWorktrees runs git worktree prune to clean up stale worktree references.
// This removes worktree metadata for worktrees whose directories no longer exist.
func (m *Manager) pruneWorktrees(ctx context.Context, worktreeBasePath string) error {
	fmt.Printf("[workspace] pruning stale worktrees: base=%s\n", worktreeBasePath)

	args := []string{"worktree", "prune"}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = worktreeBasePath

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git worktree prune failed: %w: %s", err, string(output))
	}

	fmt.Printf("[workspace] worktrees pruned: base=%s\n", worktreeBasePath)
	return nil
}

// initLocalRepo initializes a new local git repository at the given path.
// It creates the directory, runs git init, creates the initial branch, and makes an empty commit.
func (m *Manager) initLocalRepo(ctx context.Context, path, branch string) error {
	fmt.Printf("[workspace] initializing local repository: path=%s branch=%s\n", path, branch)

	// Create the directory
	if err := os.MkdirAll(path, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Run git init
	initCmd := exec.CommandContext(ctx, "git", "init")
	initCmd.Dir = path
	if output, err := initCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git init failed: %w: %s", err, string(output))
	}

	// Configure user for initial commit (required for git commit)
	configUserCmd := exec.CommandContext(ctx, "git", "config", "user.email", "schmux@localhost")
	configUserCmd.Dir = path
	if output, err := configUserCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git config user.email failed: %w: %s", err, string(output))
	}

	configNameCmd := exec.CommandContext(ctx, "git", "config", "user.name", "schmux")
	configNameCmd.Dir = path
	if output, err := configNameCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git config user.name failed: %w: %s", err, string(output))
	}

	// Create and checkout the branch
	branchCmd := exec.CommandContext(ctx, "git", "checkout", "-b", branch)
	branchCmd.Dir = path
	if output, err := branchCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git checkout -b %s failed: %w: %s", branch, err, string(output))
	}

	// Create an empty commit for a valid git state
	commitCmd := exec.CommandContext(ctx, "git", "commit", "--allow-empty", "-m", "Initial commit")
	commitCmd.Dir = path
	if output, err := commitCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git commit failed: %w: %s", err, string(output))
	}

	fmt.Printf("[workspace] local repository initialized: path=%s\n", path)
	return nil
}

// cloneRepo clones a repository to the given path.
// Deprecated: Use ensureWorktreeBase + addWorktree for new workspaces.
func (m *Manager) cloneRepo(ctx context.Context, url, path string) error {
	fmt.Printf("[workspace] cloning repository: url=%s path=%s\n", url, path)
	args := []string{"clone", url, path}
	cmd := exec.CommandContext(ctx, "git", args...)

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git clone failed: %w: %s", err, string(output))
	}

	fmt.Printf("[workspace] repository cloned: path=%s\n", path)
	return nil
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
