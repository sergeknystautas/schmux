package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/log"
	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/difftool"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/telemetry"
	"github.com/sergeknystautas/schmux/internal/workspace/ensure"
)

const (
	// workspaceNumberFormat is the format string for workspace numbering (e.g., "001", "002").
	// Supports up to 999 workspaces per repository.
	workspaceNumberFormat = "%03d"
)

var ErrWorkspaceLocked = errors.New("workspace is locked")

// Manager manages workspace directories.
type Manager struct {
	config                 *config.Config
	state                  state.StateStore
	logger                 *log.Logger
	ensurer                *ensure.Ensurer
	workspaceConfigs       map[string]*contracts.RepoConfig // workspace ID -> workspace config
	workspaceConfigsMu     sync.RWMutex
	configStates           map[string]configState // workspace path -> last known config file state
	configStatesMu         sync.RWMutex
	gitWatcher             *GitWatcher
	repoLocks              map[string]*sync.Mutex
	repoLocksMu            sync.Mutex
	randSuffix             func(length int) string
	defaultBranchCache     map[string]string // repoURL -> defaultBranch or "unknown"
	defaultBranchCacheMu   sync.RWMutex
	defaultBranchRefreshAt map[string]time.Time // repoURL -> last symbolic-ref refresh time
	lockedWorkspaces       map[string]bool
	lockedWorkspacesMu     sync.RWMutex
	workspaceGates         map[string]*sync.RWMutex // per-workspace gate: coordinates git status vs sync operations
	workspaceGatesMu       sync.Mutex
	onLockChangeFn         func(workspaceID string, locked bool)        // optional, called when lock state changes
	compoundReconcile      func(workspaceID string)                     // reconcile overlay before dispose
	syncProgressFn         func(workspaceID string, current, total int) // optional, called during LinearSyncFromDefault
	telemetry              telemetry.Telemetry                          // optional, for usage tracking
	ioTelemetry            *IOWorkspaceTelemetry                        // optional, for git command I/O telemetry
	ensuredQueryRepos      map[string]bool                              // repoURL -> true once origin query repo is validated
	ensuredQueryReposMu    sync.RWMutex
}

// New creates a new workspace manager.
func New(cfg *config.Config, st state.StateStore, statePath string, logger *log.Logger) *Manager {
	m := &Manager{
		config:                 cfg,
		state:                  st,
		logger:                 logger,
		ensurer:                ensure.New(st),
		workspaceConfigs:       make(map[string]*contracts.RepoConfig), // cache for .schmux/config.json per workspace
		configStates:           make(map[string]configState),           // track config file mtime to detect changes
		repoLocks:              make(map[string]*sync.Mutex),
		lockedWorkspaces:       make(map[string]bool),
		workspaceGates:         make(map[string]*sync.RWMutex),
		ensuredQueryRepos:      make(map[string]bool),
		defaultBranchRefreshAt: make(map[string]time.Time),
		randSuffix:             defaultRandSuffix,
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

// SetHooksDir sets the centralized hooks directory on the ensurer.
func (m *Manager) SetHooksDir(dir string) {
	m.ensurer.SetHooksDir(dir)
}

// LockWorkspace attempts to lock a workspace for a sync operation.
// Returns true if the lock was acquired, false if already locked by another sync.
// Blocks until any in-flight UpdateGitStatus on this workspace completes.
func (m *Manager) LockWorkspace(workspaceID string) bool {
	// Fail-fast if already locked by another sync operation
	m.lockedWorkspacesMu.RLock()
	if m.lockedWorkspaces[workspaceID] {
		m.lockedWorkspacesMu.RUnlock()
		return false
	}
	m.lockedWorkspacesMu.RUnlock()

	// Wait for any in-flight git status to finish
	gate := m.getWorkspaceGate(workspaceID)
	gate.Lock()
	defer gate.Unlock()

	// Re-check: another sync may have locked while we waited on the gate
	m.lockedWorkspacesMu.Lock()
	if m.lockedWorkspaces[workspaceID] {
		m.lockedWorkspacesMu.Unlock()
		return false
	}
	m.lockedWorkspaces[workspaceID] = true
	m.lockedWorkspacesMu.Unlock()

	if m.onLockChangeFn != nil {
		m.onLockChangeFn(workspaceID, true)
	}
	return true
}

// getWorkspaceGate returns the per-workspace RWMutex, creating it if needed.
func (m *Manager) getWorkspaceGate(workspaceID string) *sync.RWMutex {
	m.workspaceGatesMu.Lock()
	defer m.workspaceGatesMu.Unlock()
	gate, ok := m.workspaceGates[workspaceID]
	if !ok {
		gate = &sync.RWMutex{}
		m.workspaceGates[workspaceID] = gate
	}
	return gate
}

// UnlockWorkspace clears the lock on a workspace.
func (m *Manager) UnlockWorkspace(workspaceID string) {
	m.lockedWorkspacesMu.Lock()
	delete(m.lockedWorkspaces, workspaceID)
	m.lockedWorkspacesMu.Unlock()
	if m.onLockChangeFn != nil {
		m.onLockChangeFn(workspaceID, false)
	}
}

// IsWorkspaceLocked returns true if a sync operation is running on the workspace.
func (m *Manager) IsWorkspaceLocked(workspaceID string) bool {
	m.lockedWorkspacesMu.RLock()
	defer m.lockedWorkspacesMu.RUnlock()
	return m.lockedWorkspaces[workspaceID]
}

// SetOnLockChangeFn sets a callback invoked when workspace lock state changes.
func (m *Manager) SetOnLockChangeFn(fn func(workspaceID string, locked bool)) {
	m.onLockChangeFn = fn
}

// SetSyncProgressFn sets a callback invoked after each commit rebase in LinearSyncFromDefault.
func (m *Manager) SetSyncProgressFn(fn func(workspaceID string, current, total int)) {
	m.syncProgressFn = fn
}

// SetCompoundReconcile sets the callback for reconciling overlay files before workspace disposal.
func (m *Manager) SetCompoundReconcile(fn func(workspaceID string)) {
	m.compoundReconcile = fn
}

// SetTelemetry sets the telemetry client for usage tracking.
func (m *Manager) SetTelemetry(t telemetry.Telemetry) {
	m.telemetry = t
}

// SetIOWorkspaceTelemetry sets the I/O telemetry collector for git command instrumentation.
func (m *Manager) SetIOWorkspaceTelemetry(tel *IOWorkspaceTelemetry) {
	m.ioTelemetry = tel
}

// IOWorkspaceTelemetrySnapshot returns a point-in-time snapshot of git command telemetry.
// If reset is true, all data is cleared after taking the snapshot.
func (m *Manager) IOWorkspaceTelemetrySnapshot(reset bool) IOWorkspaceTelemetrySnapshot {
	return m.ioTelemetry.Snapshot(reset)
}

// trackWorkspaceCreated sends a telemetry event for workspace creation.
// Safe to call even if telemetry is nil.
func (m *Manager) trackWorkspaceCreated(workspaceID, repoURL, branch string) {
	if m.telemetry == nil {
		return
	}
	m.telemetry.Track("workspace_created", map[string]any{
		"workspace_id": workspaceID,
		"repo_host":    extractRepoHost(repoURL),
		"branch":       branch,
	})
}

// extractRepoHost extracts the host from a repo URL.
// Examples:
//   - "https://github.com/user/repo.git" -> "github.com"
//   - "git@github.com:user/repo.git" -> "github.com"
//   - "local:name" -> "local"
func extractRepoHost(repoURL string) string {
	if strings.HasPrefix(repoURL, "local:") {
		return "local"
	}

	// Handle git@host:path format
	if strings.HasPrefix(repoURL, "git@") {
		parts := strings.SplitN(repoURL[4:], ":", 2)
		if len(parts) > 0 {
			return parts[0]
		}
	}

	// Handle https://host/path format
	if strings.HasPrefix(repoURL, "https://") || strings.HasPrefix(repoURL, "http://") {
		url := repoURL
		if strings.HasPrefix(url, "http://") {
			url = url[7:]
		} else {
			url = url[8:]
		}
		// Find the first /
		if idx := strings.Index(url, "/"); idx > 0 {
			return url[:idx]
		}
		return url
	}

	// Handle ssh://host/path format
	if strings.HasPrefix(repoURL, "ssh://") {
		url := repoURL[6:]
		if idx := strings.Index(url, "/"); idx > 0 {
			return url[:idx]
		}
		return url
	}

	return "unknown"
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
	if m.defaultBranchRefreshAt == nil {
		m.defaultBranchRefreshAt = make(map[string]time.Time)
	}
	m.defaultBranchRefreshAt[repoURL] = time.Now()
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
			m.logger.Warn("directory missing, skipping", "id", w.ID, "path", w.Path)
			continue
		}
		if w.Repo == repoURL && w.Branch == branch {
			// Check if workspace has active sessions
			if !m.hasActiveSessions(w.ID) {
				m.logger.Info("reusing existing", "id", w.ID, "path", w.Path, "branch", branch)
				// Prepare the workspace (fetch/pull/clean)
				if err := m.prepare(ctx, w.ID, branch); err != nil {
					return nil, fmt.Errorf("failed to prepare workspace: %w", err)
				}
				// Re-copy overlay files (git clean deletes untracked overlay files)
				if repoConfig, found := m.findRepoByURL(repoURL); found {
					if manifest, err := m.copyOverlayFiles(ctx, repoConfig.Name, w.Path); err != nil {
						m.logger.Warn("failed to re-copy overlay files", "err", err)
					} else if manifest != nil {
						m.state.UpdateOverlayManifest(w.ID, manifest)
					}
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
					m.logger.Warn("directory missing, skipping", "id", w.ID, "path", w.Path)
					continue
				}
				// Only reuse if the workspace's branch hasn't diverged from the default branch.
				// If it has diverged, reusing would pollute the new branch with commits from the old one.
				if !m.isUpToDateWithDefault(ctx, w.Path, repoURL) {
					m.logger.Info("branch has diverged from default, skipping reuse", "branch", w.Branch, "id", w.ID)
					continue
				}
				m.logger.Info("reusing for different branch", "id", w.ID, "old", w.Branch, "new", branch)
				// Prepare the workspace (fetch/pull/clean) BEFORE updating state
				if err := m.prepare(ctx, w.ID, branch); err != nil {
					return nil, fmt.Errorf("failed to prepare workspace: %w", err)
				}
				// Re-copy overlay files (git clean deletes untracked overlay files)
				if repoConfig, found := m.findRepoByURL(repoURL); found {
					if manifest, err := m.copyOverlayFiles(ctx, repoConfig.Name, w.Path); err != nil {
						m.logger.Warn("failed to re-copy overlay files", "err", err)
					} else if manifest != nil {
						m.state.UpdateOverlayManifest(w.ID, manifest)
					}
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
	m.logger.Info("created", "id", w.ID, "path", w.Path, "branch", w.Branch, "repo", repoURL)

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
		m.logger.Warn("fetch failed before worktree add", "err", fetchErr)
	}

	// Fast-forward local default branch to match origin after fetch
	m.updateLocalDefaultBranch(ctx, "", RefreshTriggerExplicit, worktreeBasePath, repoURL, nil)

	createdUniqueBranch := false
	if m.config.UseWorktrees() {
		uniqueBranch, wasCreated, err := m.ensureUniqueBranch(ctx, worktreeBasePath, branch)
		if err != nil {
			return nil, fmt.Errorf("failed to pick unique branch: %w", err)
		}
		if uniqueBranch != branch {
			m.logger.Info("using unique branch", "requested", branch, "actual", uniqueBranch)
		}
		branch = uniqueBranch
		createdUniqueBranch = wasCreated
	}

	// Clean up worktree if creation fails
	cleanupNeeded := true
	defer func() {
		if cleanupNeeded {
			m.logger.Warn("cleaning up failed", "path", workspacePath)
			// Try worktree remove first, fall back to rm -rf
			if err := m.removeWorktree(ctx, worktreeBasePath, workspacePath); err != nil {
				os.RemoveAll(workspacePath)
			}
			if createdUniqueBranch {
				if err := m.deleteBranch(ctx, worktreeBasePath, branch); err != nil {
					m.logger.Warn("failed to delete branch", "branch", branch, "err", err)
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
		m.logger.Info("source_code_manager=git, using full clone")
		if err := m.cloneRepo(ctx, repoURL, workspacePath); err != nil {
			return nil, fmt.Errorf("failed to clone repo: %w", err)
		}
	}

	// Copy overlay files if they exist
	manifest, err := m.copyOverlayFiles(ctx, repoConfig.Name, workspacePath)
	if err != nil {
		m.logger.Warn("failed to copy overlay files", "err", err)
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

	// Track workspace creation
	m.trackWorkspaceCreated(w.ID, repoURL, branch)

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
			m.logger.Error("cleaning up failed local repo", "path", workspacePath)
			if err := os.RemoveAll(workspacePath); err != nil {
				m.logger.Error("failed to cleanup local repo", "path", workspacePath, "err", err)
			}
		}
	}()

	// Create the directory and initialize a local git repository
	if err := m.initLocalRepo(ctx, workspacePath, branch); err != nil {
		return nil, fmt.Errorf("failed to initialize local repo: %w", err)
	}

	m.logger.Info("created local repo", "id", workspaceID, "path", workspacePath, "branch", branch)

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

	// Track workspace creation
	m.trackWorkspaceCreated(w.ID, repoURL, branch)

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

	m.logger.Info("preparing", "id", workspaceID, "branch", branch)

	hasOrigin := m.gitHasOriginRemote(ctx, w.Path)
	if hasOrigin {
		// Fetch latest
		if err := m.gitFetch(ctx, w.Path); err != nil {
			return fmt.Errorf("git fetch failed: %w", err)
		}
	} else {
		m.logger.Debug("no origin remote, skipping fetch")
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
		m.logger.Debug("no origin remote ref, skipping pull", "branch", branch)
	}

	m.logger.Info("prepared", "id", workspaceID, "branch", branch)
	return nil
}

// Cleanup cleans up a workspace by resetting git state.
func (m *Manager) Cleanup(ctx context.Context, workspaceID string) error {
	w, found := m.state.GetWorkspace(workspaceID)
	if !found {
		return fmt.Errorf("workspace not found: %s", workspaceID)
	}

	m.logger.Info("cleaning up", "id", workspaceID, "path", w.Path)

	// Reset all changes
	if err := m.gitCheckoutDot(ctx, w.Path); err != nil {
		return fmt.Errorf("git checkout -- . failed: %w", err)
	}

	// Clean untracked files
	if err := m.gitClean(ctx, w.Path); err != nil {
		return fmt.Errorf("git clean failed: %w", err)
	}

	m.logger.Info("cleaned", "id", workspaceID)
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
//
// Public callers are treated as explicit refreshes. Internal poller/watcher paths
// should call updateGitStatusWithTrigger so telemetry attribution remains accurate.
func (m *Manager) UpdateGitStatus(ctx context.Context, workspaceID string) (*state.Workspace, error) {
	return m.updateGitStatusWithTrigger(ctx, workspaceID, RefreshTriggerExplicit)
}

// updateGitStatusWithTrigger refreshes git status for a single workspace with
// explicit telemetry attribution of the triggering source.
func (m *Manager) updateGitStatusWithTrigger(ctx context.Context, workspaceID string, trigger RefreshTrigger) (*state.Workspace, error) {
	return m.updateGitStatusWithTriggerAndRound(ctx, workspaceID, trigger, nil)
}

func (m *Manager) updateGitStatusWithTriggerAndRound(ctx context.Context, workspaceID string, trigger RefreshTrigger, round *pollRound) (*state.Workspace, error) {
	// Bail out early if context is already cancelled (e.g. during shutdown)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	w, found := m.state.GetWorkspace(workspaceID)
	if !found {
		return nil, fmt.Errorf("workspace not found: %s", workspaceID)
	}

	// Skip git operations for remote workspaces
	if w.RemoteHostID != "" {
		return &w, nil
	}

	if m.IsWorkspaceLocked(workspaceID) {
		return nil, ErrWorkspaceLocked
	}

	// Hold the gate's RLock so LockWorkspace waits for us to finish
	gate := m.getWorkspaceGate(workspaceID)
	gate.RLock()
	defer gate.RUnlock()

	// Re-check: a sync may have locked while we waited for the gate
	if m.IsWorkspaceLocked(workspaceID) {
		return nil, ErrWorkspaceLocked
	}

	// Refresh workspace config (respects lock, safe during sync)
	m.RefreshWorkspaceConfig(w)

	// Calculate git status (safe to run even with active sessions)
	dirty, ahead, behind, linesAdded, linesRemoved, filesChanged, commitsSynced, remoteBranchExists, localUnique, remoteUnique, currentBranch := m.gitStatusWithRound(ctx, workspaceID, trigger, w.Path, w.Repo, round)

	// Use branch from gitStatus; fall back to existing state if empty/detached
	actualBranch := currentBranch
	if actualBranch == "" || actualBranch == "HEAD" {
		actualBranch = w.Branch
	}

	// Detect orphaned default branch (origin/default has no common ancestor with HEAD).
	// Skip when ahead=0 and behind=0: HEAD is at the same point as origin/default,
	// so they trivially share ancestry.
	orphaned := false
	if ahead != 0 || behind != 0 {
		if defaultBranch, dbErr := m.GetDefaultBranch(ctx, w.Repo); dbErr == nil {
			defaultRef := "origin/" + defaultBranch
			if !m.hasCommonAncestorInstrumented(ctx, workspaceID, trigger, w.Path, defaultRef) {
				orphaned = true
			}
		}
	}

	// Update workspace in memory
	w.GitDirty = dirty
	w.GitAhead = ahead
	w.GitBehind = behind
	w.GitLinesAdded = linesAdded
	w.GitLinesRemoved = linesRemoved
	w.GitFilesChanged = filesChanged
	w.CommitsSyncedWithRemote = commitsSynced
	w.GitDefaultBranchOrphaned = orphaned
	w.Branch = actualBranch
	w.RemoteBranchExists = remoteBranchExists
	w.LocalUniqueCommits = localUnique
	w.RemoteUniqueCommits = remoteUnique

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
	round := newPollRound()

	// Collect local workspaces to process
	var localWorkspaces []state.Workspace
	for _, w := range workspaces {
		if w.RemoteHostID != "" {
			continue
		}
		localWorkspaces = append(localWorkspaces, w)
	}

	// Process workspaces in parallel. The gitFetchPollRound handles fetch
	// deduplication for worktrees sharing the same bare clone, and
	// state.UpdateWorkspace is mutex-protected.
	var wg sync.WaitGroup
	for _, w := range localWorkspaces {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		go func(w state.Workspace) {
			defer wg.Done()
			if _, err := m.updateGitStatusWithTriggerAndRound(ctx, w.ID, RefreshTriggerPoller, round); err != nil {
				if errors.Is(err, ErrWorkspaceLocked) {
					return
				}
				if ctx.Err() != nil {
					return
				}
				m.logger.Warn("failed to update git status", "id", w.ID, "err", err)
			}
		}(w)
	}
	wg.Wait()
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

// EnsureAll ensures all local workspaces have the necessary schmux configuration.
// Called on daemon startup to cover workspaces from previous runs.
// Individual workspace failures are logged as warnings and do not stop the sweep.
func (m *Manager) EnsureAll() {
	for _, w := range m.state.GetWorkspaces() {
		if w.RemoteHostID != "" {
			continue
		}
		if err := m.ensurer.ForWorkspace(w.ID); err != nil {
			m.logger.Warn("failed to ensure workspace", "id", w.ID, "err", err)
		}
	}
}

// Dispose deletes a workspace by removing its directory and removing it from state.
func (m *Manager) Dispose(workspaceID string) error {
	w, found := m.state.GetWorkspace(workspaceID)
	if !found {
		return fmt.Errorf("workspace not found: %s", workspaceID)
	}

	m.logger.Info("disposing", "id", workspaceID, "path", w.Path)

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
		m.logger.Info("disposed (remote)", "id", workspaceID)
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
		m.logger.Info("directory already deleted", "path", w.Path)
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
				m.logger.Warn("could not find worktree base, falling back to rm", "err", worktreeBaseErr)
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
			m.logger.Warn("failed to prune worktrees", "err", err)
		}
	}

	// Delete local branch if it wasn't pushed to remote
	if worktreeBaseErr == nil && w.Branch != "" {
		m.cleanupLocalBranch(ctx, worktreeBasePath, w)
	}

	// Remove from state
	if err := m.state.RemoveWorkspace(workspaceID); err != nil {
		return fmt.Errorf("failed to remove workspace from state: %w", err)
	}
	if err := m.state.Save(); err != nil {
		return fmt.Errorf("failed to save state: %w", err)
	}

	if err := difftool.CleanupWorkspaceTempDirs(workspaceID); err != nil {
		m.logger.Warn("failed to cleanup diff temp dirs", "id", workspaceID, "err", err)
	}

	m.logger.Info("disposed", "id", workspaceID)
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

	m.logger.Info("creating from workspace", "source", sourceWorkspaceID, "branch", currentBranch, "new_branch", newBranch)

	// 4. Find repo config by URL
	repoConfig, found := m.findRepoByURL(source.Repo)
	if !found {
		return nil, fmt.Errorf("repo URL not found in config: %s", source.Repo)
	}

	// 5. Find the next available workspace number
	workspaces := m.getWorkspacesForRepo(source.Repo)
	nextNum := findNextWorkspaceNumber(workspaces)

	// 6. Create workspace ID
	workspaceID := fmt.Sprintf("%s-"+workspaceNumberFormat, repoConfig.Name, nextNum)

	// 7. Create full path
	workspacePath := filepath.Join(m.config.GetWorkspacePath(), workspaceID)

	// 8. Ensure base repo exists (creates bare clone if needed)
	worktreeBasePath, err := m.ensureWorktreeBase(ctx, source.Repo)
	if err != nil {
		return nil, fmt.Errorf("failed to ensure worktree base: %w", err)
	}

	// 9. Fetch latest before creating worktree
	if fetchErr := m.gitFetch(ctx, worktreeBasePath); fetchErr != nil {
		m.logger.Warn("fetch failed before worktree add", "err", fetchErr)
	}

	// Fast-forward local default branch to match origin after fetch
	m.updateLocalDefaultBranch(ctx, "", RefreshTriggerExplicit, worktreeBasePath, source.Repo, nil)

	// 10. Check if branch already exists
	if m.localBranchExists(ctx, worktreeBasePath, newBranch) {
		// Use unique branch with suffix
		uniqueBranch, wasCreated, err := m.ensureUniqueBranch(ctx, worktreeBasePath, newBranch)
		if err != nil {
			return nil, fmt.Errorf("failed to pick unique branch: %w", err)
		}
		if uniqueBranch != newBranch {
			m.logger.Info("using unique branch", "requested", newBranch, "actual", uniqueBranch)
		}
		newBranch = uniqueBranch
		_ = wasCreated
	}

	// 11. Create branch from origin/<source-branch>
	sourceRef := "origin/" + currentBranch
	if err := m.createBranchFromRef(ctx, worktreeBasePath, newBranch, sourceRef); err != nil {
		return nil, fmt.Errorf("failed to create branch from %s: %w", sourceRef, err)
	}

	// 12. Clean up worktree if creation fails
	cleanupNeeded := true
	defer func() {
		if cleanupNeeded {
			m.logger.Warn("cleaning up failed", "path", workspacePath)
			// Try worktree remove first, fall back to rm -rf
			if err := m.removeWorktree(ctx, worktreeBasePath, workspacePath); err != nil {
				os.RemoveAll(workspacePath)
			}
			// Delete the branch we created
			if err := m.deleteBranch(ctx, worktreeBasePath, newBranch); err != nil {
				m.logger.Warn("failed to delete branch", "branch", newBranch, "err", err)
			}
		}
	}()

	// 13. Add worktree for the new branch
	if m.config.UseWorktrees() {
		if err := m.addWorktreeForBranch(ctx, worktreeBasePath, workspacePath, newBranch); err != nil {
			return nil, fmt.Errorf("failed to add worktree: %w", err)
		}
	} else {
		// Using full clones - clone and checkout branch
		m.logger.Info("source_code_manager=git, using full clone")
		if err := m.cloneRepo(ctx, source.Repo, workspacePath); err != nil {
			return nil, fmt.Errorf("failed to clone repo: %w", err)
		}
		// Checkout the new branch
		if _, err := m.runGit(ctx, "", RefreshTriggerExplicit, workspacePath, "checkout", newBranch); err != nil {
			return nil, fmt.Errorf("git checkout failed: %w", err)
		}
	}

	// 14. Copy overlay files if they exist
	manifest, err := m.copyOverlayFiles(ctx, repoConfig.Name, workspacePath)
	if err != nil {
		m.logger.Warn("failed to copy overlay files", "err", err)
	}

	// 15. Create workspace state
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

	// 16. State is persisted, workspace is valid
	cleanupNeeded = false

	// 17. Add filesystem watches for git metadata
	if m.gitWatcher != nil && w.RemoteHostID == "" {
		m.gitWatcher.AddWorkspace(w.ID, w.Path)
	}

	m.logger.Info("created from workspace", "id", w.ID, "path", w.Path, "branch", newBranch, "source", sourceWorkspaceID)

	// Track workspace creation
	m.trackWorkspaceCreated(w.ID, source.Repo, newBranch)

	return &w, nil
}

// getWorkspaceHEAD returns the current commit hash for a workspace.
func (m *Manager) getWorkspaceHEAD(ctx context.Context, dir string) (string, error) {
	output, err := m.runGit(ctx, "", RefreshTriggerExplicit, dir, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD failed: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// addWorktreeForBranch adds a worktree for an existing branch.
func (m *Manager) addWorktreeForBranch(ctx context.Context, worktreeBasePath, workspacePath, branch string) error {
	m.logger.Debug("adding worktree for branch", "base", worktreeBasePath, "path", workspacePath, "branch", branch)

	if _, err := m.runGit(ctx, "", RefreshTriggerExplicit, worktreeBasePath, "worktree", "add", workspacePath, branch); err != nil {
		return fmt.Errorf("git worktree add failed: %w", err)
	}

	m.logger.Info("worktree added", "path", workspacePath)
	return nil
}
