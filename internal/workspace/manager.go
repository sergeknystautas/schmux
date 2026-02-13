package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/difftool"
	"github.com/sergeknystautas/schmux/internal/state"
)

const (
	// workspaceNumberFormat is the format string for workspace numbering (e.g., "001", "002").
	// Supports up to 999 workspaces per repository.
	workspaceNumberFormat = "%03d"
)

var ErrWorkspaceLocked = errors.New("workspace is locked")

// Manager manages workspace directories.
type Manager struct {
	config               *config.Config
	state                state.StateStore
	workspaceConfigs     map[string]*contracts.RepoConfig // workspace ID -> workspace config
	workspaceConfigsMu   sync.RWMutex
	configStates         map[string]configState // workspace path -> last known config file state
	configStatesMu       sync.RWMutex
	gitWatcher           *GitWatcher
	repoLocks            map[string]*sync.Mutex
	repoLocksMu          sync.Mutex
	randSuffix           func(length int) string
	defaultBranchCache   map[string]string // repoURL -> defaultBranch or "unknown"
	defaultBranchCacheMu sync.RWMutex
	workspaceLockedFn    func(workspaceID string) bool
	compoundReconcile    func(workspaceID string) // reconcile overlay before dispose
}

// New creates a new workspace manager.
func New(cfg *config.Config, st state.StateStore, statePath string) *Manager {
	m := &Manager{
		config:           cfg,
		state:            st,
		workspaceConfigs: make(map[string]*contracts.RepoConfig), // cache for .schmux/config.json per workspace
		configStates:     make(map[string]configState),           // track config file mtime to detect changes
		repoLocks:        make(map[string]*sync.Mutex),
		randSuffix:       defaultRandSuffix,
	}
	// Pre-load workspace configs so they're available on first API call
	// (before the first poll cycle runs)
	for _, w := range st.GetWorkspaces() {
		m.RefreshWorkspaceConfig(w)
	}
	return m
}

// SetGitWatcher sets the git watcher for the manager.
func (m *Manager) SetGitWatcher(gw *GitWatcher) {
	m.gitWatcher = gw
}

// SetWorkspaceLockedFn sets a predicate to skip workspace updates when locked.
func (m *Manager) SetWorkspaceLockedFn(fn func(workspaceID string) bool) {
	m.workspaceLockedFn = fn
}

// SetCompoundReconcile sets the callback for reconciling overlay files before workspace disposal.
func (m *Manager) SetCompoundReconcile(fn func(workspaceID string)) {
	m.compoundReconcile = fn
}

func (m *Manager) repoLock(repoURL string) *sync.Mutex {
	m.repoLocksMu.Lock()
	defer m.repoLocksMu.Unlock()
	lock, ok := m.repoLocks[repoURL]
	if !ok {
		lock = &sync.Mutex{}
		m.repoLocks[repoURL] = lock
	}
	return lock
}

// GetDefaultBranch returns the cached default branch for a repo URL.
// Returns an error if the default branch cannot be determined.
// Uses negative caching ("unknown") to avoid repeated failed git commands.
func (m *Manager) GetDefaultBranch(ctx context.Context, repoURL string) (string, error) {
	// Check in-memory cache first
	m.defaultBranchCacheMu.RLock()
	if branch, ok := m.defaultBranchCache[repoURL]; ok {
		m.defaultBranchCacheMu.RUnlock()
		if branch == "unknown" {
			// Previously failed to detect - don't keep retrying
			return "", fmt.Errorf("default branch unknown for %s", repoURL)
		}
		return branch, nil
	}
	m.defaultBranchCacheMu.RUnlock()

	// Detect from origin query repo (preferred - created on daemon startup)
	queryRepoPath, err := m.ensureOriginQueryRepo(ctx, repoURL)
	if err != nil {
		m.setDefaultBranch(repoURL, "unknown")
		return "", err
	}

	branch := m.getDefaultBranch(ctx, queryRepoPath)
	if branch != "" {
		// Cache the result
		m.setDefaultBranch(repoURL, branch)
		return branch, nil
	}

	// Detection failed - cache as "unknown"
	m.setDefaultBranch(repoURL, "unknown")
	return "", fmt.Errorf("failed to detect default branch for %s", repoURL)
}

// setDefaultBranch caches the default branch in memory.
func (m *Manager) setDefaultBranch(repoURL, branch string) {
	m.defaultBranchCacheMu.Lock()
	defer m.defaultBranchCacheMu.Unlock()
	if m.defaultBranchCache == nil {
		m.defaultBranchCache = make(map[string]string)
	}
	m.defaultBranchCache[repoURL] = branch
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

// GetOrCreate finds an existing workspace for the repoURL/branch or creates a new one.
// Returns a workspace ready for use (fetch/pull/clean already done).
// For local repositories (URL format "local:{name}"), always creates a fresh workspace.
func (m *Manager) GetOrCreate(ctx context.Context, repoURL, branch string) (*state.Workspace, error) {
	if err := ValidateBranchName(branch); err != nil {
		return nil, fmt.Errorf("failed to get workspace: %w", err)
	}

	// Handle local repositories (format: "local:{name}")
	if strings.HasPrefix(repoURL, "local:") {
		repoName := strings.TrimPrefix(repoURL, "local:")
		return m.CreateLocalRepo(ctx, repoName, branch)
	}

	lock := m.repoLock(repoURL)
	lock.Lock()
	defer lock.Unlock()

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
				// Check if workspace directory still exists
				if _, err := os.Stat(w.Path); os.IsNotExist(err) {
					fmt.Printf("[workspace] directory missing, skipping: id=%s path=%s\n", w.ID, w.Path)
					continue
				}
				// Only reuse if the workspace's branch hasn't diverged from the default branch.
				// If it has diverged, reusing would pollute the new branch with commits from the old one.
				if !m.isUpToDateWithDefault(ctx, w.Path, repoURL) {
					fmt.Printf("[workspace] branch %s has diverged from default, skipping reuse: id=%s\n", w.Branch, w.ID)
					continue
				}
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
	fmt.Printf("[workspace] created: id=%s path=%s branch=%s repo=%s\n", w.ID, w.Path, w.Branch, repoURL)

	// Prepare the workspace
	if err := m.prepare(ctx, w.ID, w.Branch); err != nil {
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
	worktreeBasePath, err := m.ensureWorktreeBase(ctx, repoURL)
	if err != nil {
		return nil, fmt.Errorf("failed to ensure worktree base: %w", err)
	}

	// Fetch latest before creating worktree
	if fetchErr := m.gitFetch(ctx, worktreeBasePath); fetchErr != nil {
		fmt.Printf("[workspace] warning: fetch failed before worktree add: %v\n", fetchErr)
	}

	createdUniqueBranch := false
	if m.config.UseWorktrees() {
		uniqueBranch, wasCreated, err := m.ensureUniqueBranch(ctx, worktreeBasePath, branch)
		if err != nil {
			return nil, fmt.Errorf("failed to pick unique branch: %w", err)
		}
		if uniqueBranch != branch {
			fmt.Printf("[workspace] using unique branch: requested=%s actual=%s\n", branch, uniqueBranch)
		}
		branch = uniqueBranch
		createdUniqueBranch = wasCreated
	}

	// Clean up worktree if creation fails
	cleanupNeeded := true
	defer func() {
		if cleanupNeeded {
			fmt.Printf("[workspace] cleaning up failed: %s\n", workspacePath)
			// Try worktree remove first, fall back to rm -rf
			if err := m.removeWorktree(ctx, worktreeBasePath, workspacePath); err != nil {
				os.RemoveAll(workspacePath)
			}
			if createdUniqueBranch {
				if err := m.deleteBranch(ctx, worktreeBasePath, branch); err != nil {
					fmt.Printf("[workspace] warning: failed to delete branch %s: %v\n", branch, err)
				}
			}
		}
	}()

	// Check source code management setting
	if m.config.UseWorktrees() {
		// Using worktrees - no fallback, branch conflicts are auto-resolved with suffixes
		if err := m.addWorktree(ctx, worktreeBasePath, workspacePath, branch, repoURL); err != nil {
			return nil, fmt.Errorf("failed to add worktree: %w", err)
		}
	} else {
		// Using full clones
		fmt.Printf("[workspace] source_code_manager=git, using full clone\n")
		if err := m.cloneRepo(ctx, repoURL, workspacePath); err != nil {
			return nil, fmt.Errorf("failed to clone repo: %w", err)
		}
	}

	// Copy overlay files if they exist
	manifest, err := m.copyOverlayFiles(ctx, repoConfig.Name, workspacePath)
	if err != nil {
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

	// Store overlay manifest after workspace is persisted
	if manifest != nil {
		m.state.UpdateOverlayManifest(w.ID, manifest)
	}

	// State is persisted, workspace is valid
	cleanupNeeded = false

	// Add filesystem watches for git metadata (skip remote workspaces)
	if m.gitWatcher != nil && w.RemoteHostID == "" {
		m.gitWatcher.AddWorkspace(w.ID, w.Path)
	}

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
		Name:     repoName,
		URL:      repoURL,
		BarePath: repoName + ".git",
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

	// Skip git operations for remote workspaces
	if w.RemoteHostID != "" {
		return &w, nil
	}

	if m.workspaceLockedFn != nil && m.workspaceLockedFn(workspaceID) {
		return nil, ErrWorkspaceLocked
	}

	// Calculate git status (safe to run even with active sessions)
	dirty, ahead, behind, linesAdded, linesRemoved, filesChanged, commitsSynced := m.gitStatus(ctx, w.Path, w.Repo)

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
	w.GitLinesAdded = linesAdded
	w.GitLinesRemoved = linesRemoved
	w.GitFilesChanged = filesChanged
	w.CommitsSyncedWithRemote = commitsSynced
	w.Branch = actualBranch

	// Update the workspace in state (this updates the in-memory copy)
	if err := m.state.UpdateWorkspace(w); err != nil {
		return nil, fmt.Errorf("failed to update workspace in state: %w", err)
	}

	return &w, nil
}

// UpdateAllGitStatus refreshes git status for all workspaces.
// This is called periodically by the background goroutine.
func (m *Manager) UpdateAllGitStatus(ctx context.Context) {
	workspaces := m.state.GetWorkspaces()

	for _, w := range workspaces {
		// Skip remote workspaces - they don't have local git repos
		if w.RemoteHostID != "" {
			continue
		}

		// Refresh workspace config for this workspace
		m.RefreshWorkspaceConfig(w)

		if _, err := m.UpdateGitStatus(ctx, w.ID); err != nil {
			if errors.Is(err, ErrWorkspaceLocked) {
				continue
			}
			fmt.Printf("[workspace] failed to update git status for %s: %v\n", w.ID, err)
		}
	}
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

// Dispose deletes a workspace by removing its directory and removing it from state.
func (m *Manager) Dispose(workspaceID string) error {
	w, found := m.state.GetWorkspace(workspaceID)
	if !found {
		return fmt.Errorf("workspace not found: %s", workspaceID)
	}

	fmt.Printf("[workspace] disposing: id=%s path=%s\n", workspaceID, w.Path)

	// Remote workspaces don't have local directories - just clean up state
	if w.RemoteHostID != "" {
		// Remove any remaining sessions for this workspace
		for _, s := range m.state.GetSessions() {
			if s.WorkspaceID == workspaceID {
				m.state.RemoveSession(s.ID)
			}
		}
		if err := m.state.RemoveWorkspace(workspaceID); err != nil {
			return fmt.Errorf("failed to remove workspace from state: %w", err)
		}
		if err := m.state.Save(); err != nil {
			return fmt.Errorf("failed to save state: %w", err)
		}
		fmt.Printf("[workspace] disposed (remote): id=%s\n", workspaceID)
		return nil
	}

	// Check if workspace has active sessions
	if m.hasActiveSessions(workspaceID) {
		return fmt.Errorf("workspace has active sessions: %s", workspaceID)
	}

	ctx := context.Background()

	// Check if workspace directory exists
	dirExists := true
	if _, err := os.Stat(w.Path); os.IsNotExist(err) {
		dirExists = false
		fmt.Printf("[workspace] directory already deleted: %s\n", w.Path)
	}

	// Check git safety - only if directory exists
	if dirExists {
		gitStatus, err := m.checkGitSafety(ctx, workspaceID)
		if err != nil {
			return fmt.Errorf("failed to check git status: %w", err)
		}
		if !gitStatus.Safe {
			return fmt.Errorf("workspace has unsaved changes: %s", gitStatus.Reason)
		}
	}

	// Reconcile overlay files before disposal
	if m.compoundReconcile != nil {
		m.compoundReconcile(workspaceID)
	}

	// Remove filesystem watches before directory removal
	if m.gitWatcher != nil {
		m.gitWatcher.RemoveWorkspace(workspaceID)
	}

	// Find base repo for worktree cleanup (works even if directory is gone)
	worktreeBasePath, worktreeBaseErr := m.findWorktreeBaseForWorkspace(w)

	// Delete workspace directory (worktree or legacy full clone)
	if dirExists {
		if isWorktree(w.Path) {
			// Use git worktree remove for worktrees
			if worktreeBaseErr != nil {
				fmt.Printf("[workspace] warning: could not find worktree base, falling back to rm: %v\n", worktreeBaseErr)
				if err := os.RemoveAll(w.Path); err != nil {
					return fmt.Errorf("failed to delete workspace directory: %w", err)
				}
			} else {
				if err := m.removeWorktree(ctx, worktreeBasePath, w.Path); err != nil {
					return fmt.Errorf("failed to remove worktree: %w", err)
				}
			}
		} else {
			// Legacy full clone - delete directory
			if err := os.RemoveAll(w.Path); err != nil {
				return fmt.Errorf("failed to delete workspace directory: %w", err)
			}
		}
	}

	// Prune stale worktree references (handles case where directory was already deleted)
	if worktreeBaseErr == nil {
		if err := m.pruneWorktrees(ctx, worktreeBasePath); err != nil {
			fmt.Printf("[workspace] warning: failed to prune worktrees: %v\n", err)
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

// CreateFromWorkspace creates a new workspace with a new branch,
// branching from the source workspace's branch on origin.
func (m *Manager) CreateFromWorkspace(ctx context.Context, sourceWorkspaceID, newBranch string) (*state.Workspace, error) {
	// 1. Get source workspace
	source, found := m.state.GetWorkspace(sourceWorkspaceID)
	if !found {
		return nil, fmt.Errorf("source workspace not found: %s", sourceWorkspaceID)
	}

	// 2. Validate branch name
	if err := ValidateBranchName(newBranch); err != nil {
		return nil, fmt.Errorf("invalid branch name: %w", err)
	}

	// 3. Get source workspace's current branch
	currentBranch, err := m.gitCurrentBranch(ctx, source.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to get current branch: %w", err)
	}
	if currentBranch == "HEAD" {
		return nil, fmt.Errorf("source workspace is on detached HEAD - please checkout a branch first")
	}

	fmt.Printf("[workspace] creating from workspace: source=%s branch=%s new_branch=%s\n",
		sourceWorkspaceID, currentBranch, newBranch)

	// 5. Find repo config by URL
	repoConfig, found := m.findRepoByURL(source.Repo)
	if !found {
		return nil, fmt.Errorf("repo URL not found in config: %s", source.Repo)
	}

	// 6. Find the next available workspace number
	workspaces := m.getWorkspacesForRepo(source.Repo)
	nextNum := findNextWorkspaceNumber(workspaces)

	// 7. Create workspace ID
	workspaceID := fmt.Sprintf("%s-"+workspaceNumberFormat, repoConfig.Name, nextNum)

	// 8. Create full path
	workspacePath := filepath.Join(m.config.GetWorkspacePath(), workspaceID)

	// 9. Ensure base repo exists (creates bare clone if needed)
	worktreeBasePath, err := m.ensureWorktreeBase(ctx, source.Repo)
	if err != nil {
		return nil, fmt.Errorf("failed to ensure worktree base: %w", err)
	}

	// 10. Fetch latest before creating worktree
	if fetchErr := m.gitFetch(ctx, worktreeBasePath); fetchErr != nil {
		fmt.Printf("[workspace] warning: fetch failed before worktree add: %v\n", fetchErr)
	}

	// 11. Check if branch already exists
	if m.localBranchExists(ctx, worktreeBasePath, newBranch) {
		// Use unique branch with suffix
		uniqueBranch, wasCreated, err := m.ensureUniqueBranch(ctx, worktreeBasePath, newBranch)
		if err != nil {
			return nil, fmt.Errorf("failed to pick unique branch: %w", err)
		}
		if uniqueBranch != newBranch {
			fmt.Printf("[workspace] using unique branch: requested=%s actual=%s\n", newBranch, uniqueBranch)
		}
		newBranch = uniqueBranch
		_ = wasCreated
	}

	// 12. Create branch from origin/<source-branch>
	sourceRef := "origin/" + currentBranch
	if err := m.createBranchFromRef(ctx, worktreeBasePath, newBranch, sourceRef); err != nil {
		return nil, fmt.Errorf("failed to create branch from %s: %w", sourceRef, err)
	}

	// 13. Clean up worktree if creation fails
	cleanupNeeded := true
	defer func() {
		if cleanupNeeded {
			fmt.Printf("[workspace] cleaning up failed: %s\n", workspacePath)
			// Try worktree remove first, fall back to rm -rf
			if err := m.removeWorktree(ctx, worktreeBasePath, workspacePath); err != nil {
				os.RemoveAll(workspacePath)
			}
			// Delete the branch we created
			if err := m.deleteBranch(ctx, worktreeBasePath, newBranch); err != nil {
				fmt.Printf("[workspace] warning: failed to delete branch %s: %v\n", newBranch, err)
			}
		}
	}()

	// 14. Add worktree for the new branch
	if m.config.UseWorktrees() {
		if err := m.addWorktreeForBranch(ctx, worktreeBasePath, workspacePath, newBranch); err != nil {
			return nil, fmt.Errorf("failed to add worktree: %w", err)
		}
	} else {
		// Using full clones - clone and checkout branch
		fmt.Printf("[workspace] source_code_manager=git, using full clone\n")
		if err := m.cloneRepo(ctx, source.Repo, workspacePath); err != nil {
			return nil, fmt.Errorf("failed to clone repo: %w", err)
		}
		// Checkout the new branch
		checkoutCmd := exec.CommandContext(ctx, "git", "checkout", newBranch)
		checkoutCmd.Dir = workspacePath
		if output, err := checkoutCmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("git checkout failed: %w: %s", err, string(output))
		}
	}

	// 15. Copy overlay files if they exist
	manifest, err := m.copyOverlayFiles(ctx, repoConfig.Name, workspacePath)
	if err != nil {
		fmt.Printf("[workspace] warning: failed to copy overlay files: %v\n", err)
	}

	// 16. Create workspace state
	w := state.Workspace{
		ID:     workspaceID,
		Repo:   source.Repo,
		Branch: newBranch,
		Path:   workspacePath,
	}

	if err := m.state.AddWorkspace(w); err != nil {
		return nil, fmt.Errorf("failed to add workspace to state: %w", err)
	}
	if err := m.state.Save(); err != nil {
		return nil, fmt.Errorf("failed to save state: %w", err)
	}

	// Store overlay manifest if files were copied
	if manifest != nil {
		m.state.UpdateOverlayManifest(w.ID, manifest)
	}

	// 17. State is persisted, workspace is valid
	cleanupNeeded = false

	// 18. Add filesystem watches for git metadata
	if m.gitWatcher != nil && w.RemoteHostID == "" {
		m.gitWatcher.AddWorkspace(w.ID, w.Path)
	}

	fmt.Printf("[workspace] created from workspace: id=%s path=%s branch=%s source=%s\n",
		w.ID, w.Path, newBranch, sourceWorkspaceID)

	return &w, nil
}

// getWorkspaceHEAD returns the current commit hash for a workspace.
func (m *Manager) getWorkspaceHEAD(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD failed: %w: %s", err, string(output))
	}
	return strings.TrimSpace(string(output)), nil
}

// addWorktreeForBranch adds a worktree for an existing branch.
func (m *Manager) addWorktreeForBranch(ctx context.Context, worktreeBasePath, workspacePath, branch string) error {
	fmt.Printf("[workspace] adding worktree for branch: base=%s path=%s branch=%s\n",
		worktreeBasePath, workspacePath, branch)

	args := []string{"worktree", "add", workspacePath, branch}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = worktreeBasePath

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git worktree add failed: %w: %s", err, string(output))
	}

	fmt.Printf("[workspace] worktree added: path=%s\n", workspacePath)
	return nil
}
