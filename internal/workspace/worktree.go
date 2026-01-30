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

// ensureBaseRepo creates or returns an existing bare clone for a repo URL.
//
// Race condition handling: If two requests try to create the same base repo
// concurrently, git clone --bare will fail for the second request because the
// directory already exists. This is acceptable because:
//  1. The first clone will succeed and create the repo
//  2. The second clone fails with "already exists" error
//  3. The caller (create()) will fail, but a retry will find the existing repo
//
// In practice, this race is rare since workspace creation is typically sequential
// through the API. The state.AddBaseRepo() call is also idempotent (updates if exists).
func (m *Manager) ensureBaseRepo(ctx context.Context, repoURL string) (string, error) {
	// Check if base repo already exists in state
	if br, found := m.state.GetBaseRepoByURL(repoURL); found {
		// Verify it still exists on disk (handles external deletion)
		if _, err := os.Stat(br.Path); err == nil {
			fmt.Printf("[workspace] using existing base repo: url=%s path=%s\n", repoURL, br.Path)
			return br.Path, nil
		}
		fmt.Printf("[workspace] base repo missing on disk, will recreate: url=%s\n", repoURL)
	}

	// Derive base repo path from repo name
	repoName := extractRepoName(repoURL)
	baseRepoPath := filepath.Join(m.config.GetBaseReposPath(), repoName+".git")

	// Ensure base repos directory exists
	if err := os.MkdirAll(m.config.GetBaseReposPath(), 0755); err != nil {
		return "", fmt.Errorf("failed to create base repos directory: %w", err)
	}

	// Clone as bare repo (may fail if concurrent request already created it)
	if err := m.cloneBareRepo(ctx, repoURL, baseRepoPath); err != nil {
		// Check if it failed because directory already exists (race condition)
		if _, statErr := os.Stat(baseRepoPath); statErr == nil {
			fmt.Printf("[workspace] base repo created by concurrent request, using existing: %s\n", baseRepoPath)
			// Fall through to add to state (idempotent)
		} else {
			return "", err
		}
	}

	// Track in state
	if err := m.state.AddBaseRepo(state.BaseRepo{RepoURL: repoURL, Path: baseRepoPath}); err != nil {
		return "", fmt.Errorf("failed to add base repo to state: %w", err)
	}
	if err := m.state.Save(); err != nil {
		return "", fmt.Errorf("failed to save state: %w", err)
	}

	return baseRepoPath, nil
}

// addWorktree adds a worktree from a base repo.
func (m *Manager) addWorktree(ctx context.Context, baseRepoPath, workspacePath, branch string) error {
	fmt.Printf("[workspace] adding worktree: base=%s path=%s branch=%s\n", baseRepoPath, workspacePath, branch)

	// Check if local branch exists
	localBranchCmd := exec.CommandContext(ctx, "git", "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	localBranchCmd.Dir = baseRepoPath
	localBranchExists := localBranchCmd.Run() == nil

	// Check if remote branch exists
	remoteBranch := "origin/" + branch
	remoteBranchCmd := exec.CommandContext(ctx, "git", "show-ref", "--verify", "--quiet", "refs/remotes/"+remoteBranch)
	remoteBranchCmd.Dir = baseRepoPath
	remoteBranchExists := remoteBranchCmd.Run() == nil

	var args []string
	if localBranchExists {
		// Branch exists locally - check it out directly (no -b)
		args = []string{"worktree", "add", workspacePath, branch}
	} else if remoteBranchExists {
		// Track existing remote branch (create local branch)
		args = []string{"worktree", "add", "--track", "-b", branch, workspacePath, remoteBranch}
	} else {
		// Create new local branch from origin/main (ensures we start from latest)
		args = []string{"worktree", "add", "-b", branch, workspacePath, "origin/main"}
	}

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = baseRepoPath

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git worktree add failed: %w: %s", err, string(output))
	}

	fmt.Printf("[workspace] worktree added: path=%s\n", workspacePath)
	return nil
}

// ensureUniqueBranch returns a unique branch name if the requested one is already
// checked out by another worktree. The new branch is created from the requested
// branch's tip (origin/<branch> preferred, else local).
func (m *Manager) ensureUniqueBranch(ctx context.Context, baseRepoPath, branch string) (string, bool, error) {
	if !m.isBranchInWorktree(ctx, baseRepoPath, branch) {
		return branch, false, nil
	}

	sourceRef, err := m.branchSourceRef(ctx, baseRepoPath, branch)
	if err != nil {
		return "", false, err
	}

	for i := 0; i < 10; i++ {
		suffix := m.randSuffix(3)
		candidate := fmt.Sprintf("%s-%s", branch, suffix)
		if m.isBranchInWorktree(ctx, baseRepoPath, candidate) {
			continue
		}
		if m.localBranchExists(ctx, baseRepoPath, candidate) {
			continue
		}
		if err := m.createBranchFromRef(ctx, baseRepoPath, candidate, sourceRef); err != nil {
			return "", false, err
		}
		return candidate, true, nil
	}

	return "", false, fmt.Errorf("could not find a unique branch name for %s", branch)
}

func (m *Manager) branchSourceRef(ctx context.Context, baseRepoPath, branch string) (string, error) {
	remoteRef := "refs/remotes/origin/" + branch
	remoteCmd := exec.CommandContext(ctx, "git", "show-ref", "--verify", "--quiet", remoteRef)
	remoteCmd.Dir = baseRepoPath
	if remoteCmd.Run() == nil {
		return "origin/" + branch, nil
	}

	localRef := "refs/heads/" + branch
	localCmd := exec.CommandContext(ctx, "git", "show-ref", "--verify", "--quiet", localRef)
	localCmd.Dir = baseRepoPath
	if localCmd.Run() == nil {
		return branch, nil
	}

	return "", fmt.Errorf("branch %s not found in base repo", branch)
}

func (m *Manager) localBranchExists(ctx context.Context, baseRepoPath, branch string) bool {
	localRef := "refs/heads/" + branch
	localCmd := exec.CommandContext(ctx, "git", "show-ref", "--verify", "--quiet", localRef)
	localCmd.Dir = baseRepoPath
	return localCmd.Run() == nil
}

func (m *Manager) createBranchFromRef(ctx context.Context, baseRepoPath, branch, sourceRef string) error {
	cmd := exec.CommandContext(ctx, "git", "branch", branch, sourceRef)
	cmd.Dir = baseRepoPath
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git branch %s %s failed: %w: %s", branch, sourceRef, err, string(output))
	}
	return nil
}

func (m *Manager) deleteBranch(ctx context.Context, baseRepoPath, branch string) error {
	cmd := exec.CommandContext(ctx, "git", "branch", "-D", branch)
	cmd.Dir = baseRepoPath
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
func (m *Manager) isBranchInWorktree(ctx context.Context, baseRepoPath, branch string) bool {
	cmd := exec.CommandContext(ctx, "git", "worktree", "list", "--porcelain")
	cmd.Dir = baseRepoPath

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
func (m *Manager) removeWorktree(ctx context.Context, baseRepoPath, workspacePath string) error {
	fmt.Printf("[workspace] removing worktree: base=%s path=%s\n", baseRepoPath, workspacePath)

	args := []string{"worktree", "remove", "--force", workspacePath}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = baseRepoPath

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git worktree remove failed: %w: %s", err, string(output))
	}

	fmt.Printf("[workspace] worktree removed: path=%s\n", workspacePath)
	return nil
}

// pruneWorktrees runs git worktree prune to clean up stale worktree references.
// This removes worktree metadata for worktrees whose directories no longer exist.
func (m *Manager) pruneWorktrees(ctx context.Context, baseRepoPath string) error {
	fmt.Printf("[workspace] pruning stale worktrees: base=%s\n", baseRepoPath)

	args := []string{"worktree", "prune"}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = baseRepoPath

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git worktree prune failed: %w: %s", err, string(output))
	}

	fmt.Printf("[workspace] worktrees pruned: base=%s\n", baseRepoPath)
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
// Deprecated: Use ensureBaseRepo + addWorktree for new workspaces.
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

// findBaseRepoForWorkspace finds the base repo path for a workspace.
// First tries to read the .git file (if directory exists), then falls back
// to looking up the base repo by URL in state (works even if directory is gone).
func (m *Manager) findBaseRepoForWorkspace(w state.Workspace) (string, error) {
	// Try to resolve from .git file (works for worktrees when directory exists)
	if isWorktree(w.Path) {
		if baseRepo, err := resolveBaseRepoFromWorktree(w.Path); err == nil {
			return baseRepo, nil
		}
	}

	// Fall back to looking up base repo by URL in state
	// This works even when the workspace directory has been deleted
	if br, found := m.state.GetBaseRepoByURL(w.Repo); found {
		return br.Path, nil
	}

	return "", fmt.Errorf("could not find base repo for workspace: %s", w.ID)
}
