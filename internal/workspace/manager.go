package workspace

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/difftool"
	"github.com/sergeknystautas/schmux/internal/state"
)

const (
	// workspaceNumberFormat is the format string for workspace numbering (e.g., "001", "002").
	// Supports up to 999 workspaces per repository.
	workspaceNumberFormat = "%03d"
)

// Manager manages workspace directories.
type Manager struct {
	config *config.Config
	state  state.StateStore
}

// New creates a new workspace manager.
func New(cfg *config.Config, st state.StateStore, statePath string) *Manager {
	return &Manager{
		config: cfg,
		state:  st,
	}
}

// GetByID returns a workspace by its ID.
func (m *Manager) GetByID(workspaceID string) (*state.Workspace, bool) {
	w, found := m.state.GetWorkspace(workspaceID)
	if !found {
		return nil, false
	}
	return &w, true
}

// hasActiveSessions returns true if the workspace has any active sessions.
func (m *Manager) hasActiveSessions(workspaceID string) bool {
	for _, s := range m.state.GetSessions() {
		if s.WorkspaceID == workspaceID {
			return true
		}
	}
	return false
}

// isQuietSince returns true if the workspace has no sessions with activity
// after the cutoff time (i.e., it's safe to run git operations).
func (m *Manager) isQuietSince(workspaceID string, cutoff time.Time) bool {
	for _, s := range m.state.GetSessions() {
		if s.WorkspaceID == workspaceID && s.LastOutputAt.After(cutoff) {
			return false
		}
	}
	return true
}

// GetOrCreate finds an existing workspace for the repoURL/branch or creates a new one.
// Returns a workspace ready for use (fetch/pull/clean already done).
// For local repositories (URL format "local:{name}"), always creates a fresh workspace.
func (m *Manager) GetOrCreate(ctx context.Context, repoURL, branch string) (*state.Workspace, error) {
	// Handle local repositories (format: "local:{name}")
	if strings.HasPrefix(repoURL, "local:") {
		repoName := strings.TrimPrefix(repoURL, "local:")
		return m.CreateLocalRepo(ctx, repoName, branch)
	}

	// Try to find an existing workspace with matching repoURL and branch
	for _, w := range m.state.GetWorkspaces() {
		// Check if workspace directory still exists
		if _, err := os.Stat(w.Path); os.IsNotExist(err) {
			fmt.Printf("[workspace] directory missing, skipping: id=%s path=%s\n", w.ID, w.Path)
			continue
		}
		if w.Repo == repoURL && w.Branch == branch {
			// Check if workspace has active sessions
			if !m.hasActiveSessions(w.ID) {
				fmt.Printf("[workspace] reusing existing: id=%s path=%s branch=%s\n", w.ID, w.Path, branch)
				// Prepare the workspace (fetch/pull/clean)
				if err := m.prepare(ctx, w.ID, branch); err != nil {
					return nil, fmt.Errorf("failed to prepare workspace: %w", err)
				}
				return &w, nil
			}
		}
	}

	// Try to find any unused workspace for this repo (different branch OK)
	for _, w := range m.state.GetWorkspaces() {
		if w.Repo == repoURL {
			// Check if workspace has active sessions
			if !m.hasActiveSessions(w.ID) {
				fmt.Printf("[workspace] reusing for different branch: id=%s old=%s new=%s\n", w.ID, w.Branch, branch)
				// Prepare the workspace (fetch/pull/clean) BEFORE updating state
				if err := m.prepare(ctx, w.ID, branch); err != nil {
					return nil, fmt.Errorf("failed to prepare workspace: %w", err)
				}
				// Update branch in state only after successful prepare
				w.Branch = branch
				if err := m.state.UpdateWorkspace(w); err != nil {
					return nil, fmt.Errorf("failed to update workspace in state: %w", err)
				}
				return &w, nil
			}
		}
	}

	// Create a new workspace
	w, err := m.create(ctx, repoURL, branch)
	if err != nil {
		return nil, err
	}
	fmt.Printf("[workspace] created: id=%s path=%s branch=%s repo=%s\n", w.ID, w.Path, branch, repoURL)

	// Prepare the workspace
	if err := m.prepare(ctx, w.ID, branch); err != nil {
		return nil, fmt.Errorf("failed to prepare workspace: %w", err)
	}

	return w, nil
}

// create creates a new workspace directory for the given repoURL using git worktrees.
func (m *Manager) create(ctx context.Context, repoURL, branch string) (*state.Workspace, error) {
	// Find repo config by URL
	repoConfig, found := m.findRepoByURL(repoURL)
	if !found {
		return nil, fmt.Errorf("repo URL not found in config: %s", repoURL)
	}

	// Find the next available workspace number
	workspaces := m.getWorkspacesForRepo(repoURL)
	nextNum := findNextWorkspaceNumber(workspaces)

	// Create workspace ID
	workspaceID := fmt.Sprintf("%s-"+workspaceNumberFormat, repoConfig.Name, nextNum)

	// Create full path
	workspacePath := filepath.Join(m.config.GetWorkspacePath(), workspaceID)

	// Ensure base repo exists (creates bare clone if needed)
	baseRepoPath, err := m.ensureBaseRepo(ctx, repoURL)
	if err != nil {
		return nil, fmt.Errorf("failed to ensure base repo: %w", err)
	}

	// Fetch latest before creating worktree
	if fetchErr := m.gitFetch(ctx, baseRepoPath); fetchErr != nil {
		fmt.Printf("[workspace] warning: fetch failed before worktree add: %v\n", fetchErr)
	}

	// Clean up worktree if creation fails
	cleanupNeeded := true
	defer func() {
		if cleanupNeeded {
			fmt.Printf("[workspace] cleaning up failed: %s\n", workspacePath)
			// Try worktree remove first, fall back to rm -rf
			if err := m.removeWorktree(ctx, baseRepoPath, workspacePath); err != nil {
				os.RemoveAll(workspacePath)
			}
		}
	}()

	// Add worktree (fall back to full clone if branch is already checked out)
	if err := m.addWorktree(ctx, baseRepoPath, workspacePath, branch); err != nil {
		// Check if error is due to branch already being checked out
		if strings.Contains(err.Error(), "is already checked out") {
			fmt.Printf("[workspace] branch already checked out, falling back to full clone\n")
			if err := m.cloneRepo(ctx, repoURL, workspacePath); err != nil {
				return nil, fmt.Errorf("failed to clone repo (worktree fallback): %w", err)
			}
		} else {
			return nil, fmt.Errorf("failed to add worktree: %w", err)
		}
	}

	// Copy overlay files if they exist
	if err := m.copyOverlayFiles(ctx, repoConfig.Name, workspacePath); err != nil {
		fmt.Printf("[workspace] warning: failed to copy overlay files: %v\n", err)
		// Don't fail workspace creation if overlay copy fails
	}

	// Create workspace state with branch
	w := state.Workspace{
		ID:     workspaceID,
		Repo:   repoURL,
		Branch: branch,
		Path:   workspacePath,
	}

	if err := m.state.AddWorkspace(w); err != nil {
		return nil, fmt.Errorf("failed to add workspace to state: %w", err)
	}
	if err := m.state.Save(); err != nil {
		return nil, fmt.Errorf("failed to save state: %w", err)
	}

	// State is persisted, workspace is valid
	cleanupNeeded = false
	return &w, nil
}

// CreateLocalRepo creates a new workspace with a fresh local git repository.
// The repoName parameter is used to create the workspace ID (e.g., "myproject-001").
// A new git repository is initialized with the specified branch and an initial empty commit.
func (m *Manager) CreateLocalRepo(ctx context.Context, repoName, branch string) (*state.Workspace, error) {
	// Validate repo name (should be a valid directory name)
	if repoName == "" {
		return nil, fmt.Errorf("repo name is required")
	}
	// Basic sanitization - prevent directory traversal
	if strings.Contains(repoName, "..") || strings.Contains(repoName, "/") || strings.Contains(repoName, "\\") {
		return nil, fmt.Errorf("invalid repo name: %s", repoName)
	}

	// Construct the repo URL for state (local:{name})
	repoURL := fmt.Sprintf("local:%s", repoName)

	// Find the next available workspace number for this "local repo"
	workspaces := m.getWorkspacesForRepo(repoURL)
	nextNum := findNextWorkspaceNumber(workspaces)

	// Create workspace ID
	workspaceID := fmt.Sprintf("%s-"+workspaceNumberFormat, repoName, nextNum)

	// Create full path
	workspacePath := filepath.Join(m.config.GetWorkspacePath(), workspaceID)

	// Clean up directory if creation fails (registered before any directory creation)
	cleanupNeeded := true
	defer func() {
		if cleanupNeeded {
			fmt.Printf("[workspace] cleaning up failed local repo: %s\n", workspacePath)
			if err := os.RemoveAll(workspacePath); err != nil {
				fmt.Printf("[workspace] failed to cleanup local repo %s: %v\n", workspacePath, err)
			}
		}
	}()

	// Create the directory and initialize a local git repository
	if err := m.initLocalRepo(ctx, workspacePath, branch); err != nil {
		return nil, fmt.Errorf("failed to initialize local repo: %w", err)
	}

	fmt.Printf("[workspace] created local repo: id=%s path=%s branch=%s\n", workspaceID, workspacePath, branch)

	// Create workspace state
	w := state.Workspace{
		ID:     workspaceID,
		Repo:   repoURL,
		Branch: branch,
		Path:   workspacePath,
	}

	if err := m.state.AddWorkspace(w); err != nil {
		return nil, fmt.Errorf("failed to add workspace to state: %w", err)
	}
	if err := m.state.Save(); err != nil {
		return nil, fmt.Errorf("failed to save state: %w", err)
	}

	// State is persisted, workspace is valid even if config update fails
	cleanupNeeded = false

	// Add the new local repository to config so it appears in the spawn wizard dropdown
	m.config.Repos = append(m.config.Repos, config.Repo{
		Name: repoName,
		URL:  repoURL,
	})
	if err := m.config.Save(); err != nil {
		return nil, fmt.Errorf("failed to save config: %w", err)
	}

	return &w, nil
}

// prepare prepares a workspace for use (git checkout, pull, clean).
func (m *Manager) prepare(ctx context.Context, workspaceID, branch string) error {
	w, found := m.state.GetWorkspace(workspaceID)
	if !found {
		return fmt.Errorf("workspace not found: %s", workspaceID)
	}

	// Check if workspace has active sessions
	if m.hasActiveSessions(workspaceID) {
		return fmt.Errorf("workspace has active sessions: %s", workspaceID)
	}

	fmt.Printf("[workspace] preparing: id=%s branch=%s\n", workspaceID, branch)

	hasOrigin := m.gitHasOriginRemote(ctx, w.Path)
	if hasOrigin {
		// Fetch latest
		if err := m.gitFetch(ctx, w.Path); err != nil {
			return fmt.Errorf("git fetch failed: %w", err)
		}
	} else {
		fmt.Printf("[workspace] no origin remote, skipping fetch\n")
	}

	remoteBranchExists := false
	if hasOrigin {
		var err error
		remoteBranchExists, err = m.gitRemoteBranchExists(ctx, w.Path, branch)
		if err != nil {
			return fmt.Errorf("git remote branch check failed: %w", err)
		}
	}

	// Discard any local changes (must happen before pull)
	if err := m.gitCheckoutDot(ctx, w.Path); err != nil {
		return fmt.Errorf("git checkout -- . failed: %w", err)
	}

	// Clean untracked files and directories (must happen before pull)
	if err := m.gitClean(ctx, w.Path); err != nil {
		return fmt.Errorf("git clean failed: %w", err)
	}

	// Checkout/reset branch after cleaning
	if err := m.gitCheckoutBranch(ctx, w.Path, branch, remoteBranchExists); err != nil {
		return fmt.Errorf("git checkout failed: %w", err)
	}

	// Pull with rebase (working dir is now clean)
	if remoteBranchExists {
		if err := m.gitPullRebase(ctx, w.Path, branch); err != nil {
			return fmt.Errorf("git pull --rebase failed (conflicts?): %w", err)
		}
	} else {
		fmt.Printf("[workspace] no origin/%s remote ref, skipping pull\n", branch)
	}

	fmt.Printf("[workspace] prepared: id=%s branch=%s\n", workspaceID, branch)
	return nil
}

// Cleanup cleans up a workspace by resetting git state.
func (m *Manager) Cleanup(ctx context.Context, workspaceID string) error {
	w, found := m.state.GetWorkspace(workspaceID)
	if !found {
		return fmt.Errorf("workspace not found: %s", workspaceID)
	}

	fmt.Printf("[workspace] cleaning up: id=%s path=%s\n", workspaceID, w.Path)

	// Reset all changes
	if err := m.gitCheckoutDot(ctx, w.Path); err != nil {
		return fmt.Errorf("git checkout -- . failed: %w", err)
	}

	// Clean untracked files
	if err := m.gitClean(ctx, w.Path); err != nil {
		return fmt.Errorf("git clean failed: %w", err)
	}

	fmt.Printf("[workspace] cleaned: id=%s\n", workspaceID)
	return nil
}

// getWorkspacesForRepo returns all workspaces for a given repoURL.
func (m *Manager) getWorkspacesForRepo(repoURL string) []state.Workspace {
	var result []state.Workspace
	for _, w := range m.state.GetWorkspaces() {
		if w.Repo == repoURL {
			result = append(result, w)
		}
	}
	return result
}

// findRepoByURL finds a repo config by URL.
func (m *Manager) findRepoByURL(repoURL string) (config.Repo, bool) {
	for _, repo := range m.config.GetRepos() {
		if repo.URL == repoURL {
			return repo, true
		}
	}
	return config.Repo{}, false
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
		// Create new local branch (use HEAD as starting point)
		args = []string{"worktree", "add", "-b", branch, workspacePath, "HEAD"}
	}

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = baseRepoPath

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git worktree add failed: %w: %s", err, string(output))
	}

	fmt.Printf("[workspace] worktree added: path=%s\n", workspacePath)
	return nil
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

// gitFetch runs git fetch. For worktrees, fetches from the base repo.
func (m *Manager) gitFetch(ctx context.Context, dir string) error {
	// Resolve to base repo if this is a worktree
	fetchDir := dir
	if isWorktree(dir) {
		if baseRepo, err := resolveBaseRepoFromWorktree(dir); err == nil {
			fetchDir = baseRepo
		}
	}

	args := []string{"fetch"}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = fetchDir

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git fetch failed: %w: %s", err, string(output))
	}

	return nil
}

// gitCheckoutBranch runs git checkout -B, optionally resetting to origin/<branch>.
func (m *Manager) gitCheckoutBranch(ctx context.Context, dir, branch string, remoteBranchExists bool) error {
	args := []string{"checkout", "-B", branch}
	if remoteBranchExists {
		args = append(args, "origin/"+branch)
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git checkout failed: %w: %s", err, string(output))
	}

	return nil
}

// gitPullRebase runs git pull --rebase origin <branch>.
// For cloned repos with an origin remote, this avoids relying on potentially incorrect
// upstream config. For local repos without origin, skips the pull.
func (m *Manager) gitPullRebase(ctx context.Context, dir, branch string) error {
	// Check if origin remote exists
	remoteCmd := exec.CommandContext(ctx, "git", "remote", "get-url", "origin")
	remoteCmd.Dir = dir
	if _, err := remoteCmd.CombinedOutput(); err != nil {
		// No origin remote - local-only repo, nothing to pull
		fmt.Printf("[workspace] no origin remote, skipping pull\n")
		return nil
	}

	// Explicitly pull from origin/<branch> to avoid broken upstream config
	args := []string{"pull", "--rebase", "origin", branch}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git pull failed: %w: %s", err, string(output))
	}

	return nil
}

// gitHasOriginRemote checks if the repo has an origin remote configured.
func (m *Manager) gitHasOriginRemote(ctx context.Context, dir string) bool {
	remoteCmd := exec.CommandContext(ctx, "git", "remote", "get-url", "origin")
	remoteCmd.Dir = dir
	return remoteCmd.Run() == nil
}

// gitRemoteBranchExists checks for refs/remotes/origin/<branch>.
func (m *Manager) gitRemoteBranchExists(ctx context.Context, dir, branch string) (bool, error) {
	ref := "refs/remotes/origin/" + branch
	cmd := exec.CommandContext(ctx, "git", "show-ref", "--verify", "--quiet", ref)
	cmd.Dir = dir

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, fmt.Errorf("git show-ref failed: %w", err)
	}

	return true, nil
}

// gitCheckoutDot runs git checkout -- .
func (m *Manager) gitCheckoutDot(ctx context.Context, dir string) error {
	args := []string{"checkout", "--", "."}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git checkout -- . failed: %w: %s", err, string(output))
	}

	return nil
}

// gitCurrentBranch returns the current branch name for a directory.
func (m *Manager) gitCurrentBranch(ctx context.Context, dir string) (string, error) {
	args := []string{"rev-parse", "--abbrev-ref", "HEAD"}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git rev-parse failed: %w: %s", err, string(output))
	}

	return strings.TrimSpace(string(output)), nil
}

// gitClean runs git clean -fd.
func (m *Manager) gitClean(ctx context.Context, dir string) error {
	args := []string{"clean", "-fd"}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git clean failed: %w: %s", err, string(output))
	}

	return nil
}

// findNextWorkspaceNumber finds the next available workspace number, filling gaps.
// It starts from 1 and returns the first unused number.
func findNextWorkspaceNumber(workspaces []state.Workspace) int {
	// Track which numbers are used
	used := make(map[int]bool)
	for _, w := range workspaces {
		num, err := extractWorkspaceNumber(w.ID)
		if err == nil {
			used[num] = true
		}
	}

	// Find first unused number starting from 1
	nextNum := 1
	for used[nextNum] {
		nextNum++
	}
	return nextNum
}

// extractWorkspaceNumber extracts the numeric suffix from a workspace ID.
func extractWorkspaceNumber(id string) (int, error) {
	parts := strings.Split(id, "-")
	if len(parts) < 2 {
		return 0, fmt.Errorf("invalid workspace ID format: %s", id)
	}

	numStr := parts[len(parts)-1]
	return strconv.Atoi(numStr)
}

// UpdateGitStatus refreshes the git status for a single workspace.
// Returns the updated workspace or an error.
func (m *Manager) UpdateGitStatus(ctx context.Context, workspaceID string) (*state.Workspace, error) {
	w, found := m.state.GetWorkspace(workspaceID)
	if !found {
		return nil, fmt.Errorf("workspace not found: %s", workspaceID)
	}

	// Calculate git status (safe to run even with active sessions)
	dirty, ahead, behind := m.gitStatus(ctx, w.Path)

	// Detect actual current branch (may differ from state if user manually switched)
	actualBranch, err := m.gitCurrentBranch(ctx, w.Path)
	if err != nil {
		fmt.Printf("[workspace] failed to get current branch for %s: %v\n", w.ID, err)
		actualBranch = w.Branch // fallback to existing state
	}

	// Update workspace in memory
	w.GitDirty = dirty
	w.GitAhead = ahead
	w.GitBehind = behind
	w.Branch = actualBranch

	// Update the workspace in state (this updates the in-memory copy)
	if err := m.state.UpdateWorkspace(w); err != nil {
		return nil, fmt.Errorf("failed to update workspace in state: %w", err)
	}

	return &w, nil
}

// gitStatus calculates the git status for a workspace directory.
// Returns: (dirty bool, ahead int, behind int)
func (m *Manager) gitStatus(ctx context.Context, dir string) (dirty bool, ahead int, behind int) {
	// Fetch to get latest remote state for accurate ahead/behind counts
	_ = m.gitFetch(ctx, dir)

	// Check for dirty state (any changes: modified, added, removed, or untracked)
	statusCmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	statusCmd.Dir = dir
	output, err := statusCmd.CombinedOutput()
	dirty = err == nil && len(strings.TrimSpace(string(output))) > 0

	// Check ahead/behind counts using rev-list
	// Compare against origin/main to show GitHub-style status:
	// - ahead = commits in this branch not in main
	// - behind = commits in main not in this branch
	revListCmd := exec.CommandContext(ctx, "git", "rev-list", "--left-right", "--count", "HEAD...origin/main")
	revListCmd.Dir = dir
	output, err = revListCmd.CombinedOutput()
	if err != nil {
		// No upstream or other error - log and just return dirty state
		fmt.Printf("[workspace] git rev-list failed for %s: %v\n", dir, err)
		return dirty, 0, 0
	}

	// Parse output: "ahead\tbehind" (e.g., "3\t2" means 3 ahead, 2 behind)
	parts := strings.Split(strings.TrimSpace(string(output)), "\t")
	if len(parts) == 2 {
		ahead, _ = strconv.Atoi(parts[0])
		behind, _ = strconv.Atoi(parts[1])
	}

	return dirty, ahead, behind
}

// UpdateAllGitStatus refreshes git status for all workspaces.
// This is called periodically by the background goroutine.
// Skips workspaces that have active sessions (recent terminal output).
func (m *Manager) UpdateAllGitStatus(ctx context.Context) {
	workspaces := m.state.GetWorkspaces()

	// Calculate activity threshold - only update workspaces that have been
	// quiet (no session output) within the last poll interval
	pollIntervalMs := m.config.GetGitStatusPollIntervalMs()
	cutoff := time.Now().Add(-time.Duration(pollIntervalMs) * time.Millisecond)

	for _, w := range workspaces {
		// Skip if workspace has recent activity (not quiet)
		if !m.isQuietSince(w.ID, cutoff) {
			continue
		}

		if _, err := m.UpdateGitStatus(ctx, w.ID); err != nil {
			fmt.Printf("[workspace] failed to update git status for %s: %v\n", w.ID, err)
		}
	}
}

// GitSafetyStatus represents the git safety status of a workspace.
type GitSafetyStatus struct {
	Safe           bool   // true if workspace is safe to dispose
	Reason         string // explanation if not safe
	ModifiedFiles  int    // number of modified files
	UntrackedFiles int    // number of untracked files
	AheadCommits   int    // number of unpushed commits
}

// checkGitSafety checks if a workspace is safe to dispose based on git state.
// Returns detailed status about why the workspace is not safe.
func (m *Manager) checkGitSafety(ctx context.Context, workspaceID string) (*GitSafetyStatus, error) {
	w, found := m.state.GetWorkspace(workspaceID)
	if !found {
		return nil, fmt.Errorf("workspace not found: %s", workspaceID)
	}

	status := &GitSafetyStatus{Safe: true}

	// Check for dirty state (any changes: modified, added, removed, or untracked)
	statusCmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	statusCmd.Dir = w.Path
	output, err := statusCmd.CombinedOutput()
	if err != nil {
		// Git command failed - this might mean the repo is corrupt, treat as unsafe
		status.Safe = false
		status.Reason = fmt.Sprintf("git status failed: %v", err)
		return status, nil
	}

	// Parse status output to count file types
	// Format: XY filename where X is staged, Y is unstaged
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Check for untracked files (starts with ??)
		if strings.HasPrefix(line, "??") {
			status.UntrackedFiles++
			status.Safe = false
			continue
		}

		// Any other output means modified/added/deleted files
		status.ModifiedFiles++
		status.Safe = false
	}

	// Check ahead/behind counts using rev-list (only if there's an upstream)
	revListCmd := exec.CommandContext(ctx, "git", "rev-list", "--left-right", "--count", "HEAD...@{u}")
	revListCmd.Dir = w.Path
	output, err = revListCmd.CombinedOutput()
	if err != nil {
		// No upstream branch or other error - skip ahead/behind check
		// A clean working tree with no upstream is safe to dispose
		// (local-only commits are OK if there's no remote to push to)
		fmt.Printf("[workspace] no upstream branch for %s, skipping ahead/behind check\n", workspaceID)
	} else {
		// Parse output: "ahead\tbehind" (e.g., "3\t2" means 3 ahead, 2 behind)
		parts := strings.Split(strings.TrimSpace(string(output)), "\t")
		if len(parts) == 2 {
			ahead, _ := strconv.Atoi(parts[0])
			status.AheadCommits = ahead
			if ahead > 0 {
				status.Safe = false
			}
		}
	}

	// Build reason string if not safe
	if !status.Safe {
		var reasons []string
		if status.ModifiedFiles > 0 {
			reasons = append(reasons, fmt.Sprintf("%d modified file(s)", status.ModifiedFiles))
		}
		if status.UntrackedFiles > 0 {
			reasons = append(reasons, fmt.Sprintf("%d untracked file(s)", status.UntrackedFiles))
		}
		if status.AheadCommits > 0 {
			reasons = append(reasons, fmt.Sprintf("%d unpushed commit(s)", status.AheadCommits))
		}
		if status.Reason != "" {
			reasons = append(reasons, status.Reason)
		}
		status.Reason = strings.Join(reasons, "; ")
	}

	return status, nil
}

// EnsureWorkspaceDir ensures the workspace base directory exists.
func (m *Manager) EnsureWorkspaceDir() error {
	path := m.config.GetWorkspacePath()
	// Skip if workspace_path is empty (during wizard setup)
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(path, 0755); err != nil {
		return fmt.Errorf("failed to create workspace directory: %w", err)
	}
	return nil
}

// copyOverlayFiles copies overlay files from the overlay directory to the workspace.
// If the overlay directory doesn't exist, this is a no-op.
func (m *Manager) copyOverlayFiles(ctx context.Context, repoName, workspacePath string) error {
	overlayDir, err := OverlayDir(repoName)
	if err != nil {
		return fmt.Errorf("failed to get overlay directory: %w", err)
	}

	// Check if overlay directory exists
	if _, err := os.Stat(overlayDir); os.IsNotExist(err) {
		fmt.Printf("[workspace] no overlay directory for repo %s, skipping\n", repoName)
		return nil
	}

	fmt.Printf("[workspace] copying overlay files: repo=%s to=%s\n", repoName, workspacePath)
	if err := CopyOverlay(ctx, overlayDir, workspacePath); err != nil {
		return fmt.Errorf("failed to copy overlay files: %w", err)
	}

	fmt.Printf("[workspace] overlay files copied successfully\n")
	return nil
}

// RefreshOverlay reapplies overlay files to an existing workspace.
func (m *Manager) RefreshOverlay(ctx context.Context, workspaceID string) error {
	w, found := m.state.GetWorkspace(workspaceID)
	if !found {
		return fmt.Errorf("workspace not found: %s", workspaceID)
	}

	// Find repo config by URL to get repo name
	repoConfig, found := m.findRepoByURL(w.Repo)
	if !found {
		return fmt.Errorf("repo URL not found in config: %s", w.Repo)
	}

	fmt.Printf("[workspace] refreshing overlay: id=%s repo=%s\n", workspaceID, repoConfig.Name)

	if err := m.copyOverlayFiles(ctx, repoConfig.Name, w.Path); err != nil {
		return fmt.Errorf("failed to copy overlay files: %w", err)
	}

	fmt.Printf("[workspace] overlay refreshed successfully: %s\n", workspaceID)
	return nil
}

// EnsureOverlayDirs ensures overlay directories exist for all configured repos.
func (m *Manager) EnsureOverlayDirs(repos []config.Repo) error {
	for _, repo := range repos {
		if err := EnsureOverlayDir(repo.Name); err != nil {
			return fmt.Errorf("failed to ensure overlay directory for %s: %w", repo.Name, err)
		}
	}
	fmt.Printf("[workspace] ensured overlay directories for %d repos\n", len(repos))
	return nil
}

// extractRepoName extracts the repository name from various URL formats.
// Handles: git@github.com:user/myrepo.git, https://github.com/user/myrepo.git, etc.
func extractRepoName(repoURL string) string {
	// Strip .git suffix
	name := strings.TrimSuffix(repoURL, ".git")

	// Get last path component (handle both / and : separators)
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}
	if idx := strings.LastIndex(name, ":"); idx >= 0 {
		name = name[idx+1:]
	}

	return name
}

// isWorktree checks if a path is a worktree (has .git file) vs full clone (.git dir).
func isWorktree(path string) bool {
	gitPath := filepath.Join(path, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		return false
	}
	return !info.IsDir() // File = worktree, Dir = full clone
}

// resolveBaseRepoFromWorktree reads the .git file to find the base repo path.
func resolveBaseRepoFromWorktree(worktreePath string) (string, error) {
	gitFilePath := filepath.Join(worktreePath, ".git")
	content, err := os.ReadFile(gitFilePath)
	if err != nil {
		return "", fmt.Errorf("failed to read .git file: %w", err)
	}

	// Format: "gitdir: /path/to/base.git/worktrees/workspace-name"
	line := strings.TrimSpace(string(content))
	if !strings.HasPrefix(line, "gitdir: ") {
		return "", fmt.Errorf("invalid .git file format")
	}

	gitdir := strings.TrimPrefix(line, "gitdir: ")

	// Strip "/worktrees/xxx" to get base repo path
	if idx := strings.Index(gitdir, "/worktrees/"); idx >= 0 {
		return gitdir[:idx], nil
	}

	return "", fmt.Errorf("could not parse base repo from gitdir: %s", gitdir)
}

// findBaseRepoForWorkspace finds the base repo path for a workspace.
func (m *Manager) findBaseRepoForWorkspace(w state.Workspace) (string, error) {
	// Try to resolve from .git file (works for worktrees)
	if isWorktree(w.Path) {
		return resolveBaseRepoFromWorktree(w.Path)
	}

	// Not a worktree
	return "", fmt.Errorf("workspace is not a worktree: %s", w.ID)
}

// Dispose deletes a workspace by removing its directory and removing it from state.
func (m *Manager) Dispose(workspaceID string) error {
	w, found := m.state.GetWorkspace(workspaceID)
	if !found {
		return fmt.Errorf("workspace not found: %s", workspaceID)
	}

	fmt.Printf("[workspace] disposing: id=%s path=%s\n", workspaceID, w.Path)

	// Check if workspace has active sessions
	if m.hasActiveSessions(workspaceID) {
		return fmt.Errorf("workspace has active sessions: %s", workspaceID)
	}

	// Check git safety - hard block on dirty state
	ctx := context.Background()
	gitStatus, err := m.checkGitSafety(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("failed to check git status: %w", err)
	}
	if !gitStatus.Safe {
		return fmt.Errorf("workspace has unsaved changes: %s", gitStatus.Reason)
	}

	// Delete workspace directory (worktree or legacy full clone)
	if isWorktree(w.Path) {
		// Use git worktree remove for worktrees
		baseRepoPath, err := m.findBaseRepoForWorkspace(w)
		if err != nil {
			fmt.Printf("[workspace] warning: could not find base repo, falling back to rm: %v\n", err)
			if err := os.RemoveAll(w.Path); err != nil {
				return fmt.Errorf("failed to delete workspace directory: %w", err)
			}
		} else {
			if err := m.removeWorktree(ctx, baseRepoPath, w.Path); err != nil {
				return fmt.Errorf("failed to remove worktree: %w", err)
			}
		}
	} else {
		// Legacy full clone - delete directory
		if err := os.RemoveAll(w.Path); err != nil {
			return fmt.Errorf("failed to delete workspace directory: %w", err)
		}
	}

	// Remove from state
	if err := m.state.RemoveWorkspace(workspaceID); err != nil {
		return fmt.Errorf("failed to remove workspace from state: %w", err)
	}
	if err := m.state.Save(); err != nil {
		return fmt.Errorf("failed to save state: %w", err)
	}

	if err := difftool.CleanupWorkspaceTempDirs(workspaceID); err != nil {
		fmt.Printf("[workspace] failed to cleanup diff temp dirs for %s: %v\n", workspaceID, err)
	}

	fmt.Printf("[workspace] disposed: id=%s\n", workspaceID)
	return nil
}
