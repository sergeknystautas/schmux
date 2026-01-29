package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

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

// configState tracks the last known state of a workspace's config file
type configState struct {
	mtime   time.Time
	existed bool
}

// Manager manages workspace directories.
type Manager struct {
	config             *config.Config
	state              state.StateStore
	workspaceConfigs   map[string]*contracts.RepoConfig // workspace ID -> workspace config
	workspaceConfigsMu sync.RWMutex
	configStates       map[string]configState // workspace path -> last known config file state
	configStatesMu     sync.RWMutex
}

// New creates a new workspace manager.
func New(cfg *config.Config, st state.StateStore, statePath string) *Manager {
	m := &Manager{
		config:           cfg,
		state:            st,
		workspaceConfigs: make(map[string]*contracts.RepoConfig), // cache for .schmux/config.json per workspace
		configStates:     make(map[string]configState),           // track config file mtime to detect changes
	}
	// Pre-load workspace configs so they're available on first API call
	// (before the first poll cycle runs)
	for _, w := range st.GetWorkspaces() {
		m.RefreshWorkspaceConfig(w)
	}
	return m
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

	// Check source code management setting
	if m.config.UseWorktrees() {
		// Using worktrees - no fallback, branch conflicts should be caught by UI
		if err := m.addWorktree(ctx, baseRepoPath, workspacePath, branch); err != nil {
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
	dirty, ahead, behind, linesAdded, linesRemoved, filesChanged := m.gitStatus(ctx, w.Path)

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
	w.Branch = actualBranch

	// Update the workspace in state (this updates the in-memory copy)
	if err := m.state.UpdateWorkspace(w); err != nil {
		return nil, fmt.Errorf("failed to update workspace in state: %w", err)
	}

	return &w, nil
}

// UpdateAllGitStatus refreshes git status for all workspaces.
// This is called periodically by the background goroutine.
// Skips workspaces that have active sessions (recent terminal output),
// unless forceAll is true.
func (m *Manager) UpdateAllGitStatus(ctx context.Context, forceAll bool) {
	workspaces := m.state.GetWorkspaces()

	var cutoff time.Time
	if !forceAll {
		// Calculate activity threshold - only update workspaces that have been
		// quiet (no session output) within the last poll interval
		pollIntervalMs := m.config.GetGitStatusPollIntervalMs()
		cutoff = time.Now().Add(-time.Duration(pollIntervalMs) * time.Millisecond)
	}

	for _, w := range workspaces {
		// Refresh workspace config for this workspace
		m.RefreshWorkspaceConfig(w)

		// Skip if workspace has recent activity (not quiet), unless forcing all
		if !forceAll && !m.isQuietSince(w.ID, cutoff) {
			continue
		}

		if _, err := m.UpdateGitStatus(ctx, w.ID); err != nil {
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

// LoadRepoConfig reads the .schmux/config.json file from a workspace directory.
// Returns the config and any error (but returns nil config for missing files, only errors on parse failure).
func LoadRepoConfig(workspacePath string) (*contracts.RepoConfig, error) {
	configPath := filepath.Join(workspacePath, ".schmux", "config.json")

	data, err := os.ReadFile(configPath)
	if err != nil {
		// File doesn't exist or can't be read - not an error, just no config
		return nil, nil
	}

	var repoConfig contracts.RepoConfig
	if err := json.Unmarshal(data, &repoConfig); err != nil {
		// Invalid JSON - return error so caller can log it
		return nil, fmt.Errorf("failed to parse %s: %w", configPath, err)
	}

	return &repoConfig, nil
}

// RefreshWorkspaceConfig refreshes the cached workspace config for a single workspace.
// Only logs when the config file changes (by mtime).
func (m *Manager) RefreshWorkspaceConfig(w state.Workspace) {
	configPath := filepath.Join(w.Path, ".schmux", "config.json")

	// Check if file has changed since last read
	var currentMtime time.Time
	var fileExists bool
	if info, err := os.Stat(configPath); err == nil {
		currentMtime = info.ModTime()
		fileExists = true
	}

	m.configStatesMu.Lock()
	lastState, hasLastState := m.configStates[w.Path]
	fileChanged := !hasLastState || lastState.mtime != currentMtime || lastState.existed != fileExists
	if fileChanged {
		m.configStates[w.Path] = configState{mtime: currentMtime, existed: fileExists}
	}
	m.configStatesMu.Unlock()

	// If file hasn't changed, skip processing entirely
	if !fileChanged {
		return
	}

	repoCfg, err := LoadRepoConfig(w.Path)

	// Log on change: error or success
	if err != nil {
		fmt.Printf("[workspace] warning: %v\n", err)
		return
	}
	if repoCfg != nil {
		fmt.Printf("[workspace] loaded config from %s\n", configPath)
	}

	validQuickLaunch := validateWorkspaceQuickLaunch(configPath, repoCfg, m.config)
	if repoCfg == nil || len(validQuickLaunch) == 0 {
		m.workspaceConfigsMu.Lock()
		delete(m.workspaceConfigs, w.ID)
		m.workspaceConfigsMu.Unlock()
		return
	}

	m.workspaceConfigsMu.Lock()
	m.workspaceConfigs[w.ID] = &contracts.RepoConfig{QuickLaunch: validQuickLaunch}
	m.workspaceConfigsMu.Unlock()
}

// GetWorkspaceConfig returns the cached workspace config for the given workspace ID.
func (m *Manager) GetWorkspaceConfig(workspaceID string) *contracts.RepoConfig {
	m.workspaceConfigsMu.RLock()
	cfg := m.workspaceConfigs[workspaceID]
	m.workspaceConfigsMu.RUnlock()
	if cfg == nil {
		return nil
	}
	copyCfg := &contracts.RepoConfig{QuickLaunch: make([]contracts.QuickLaunch, len(cfg.QuickLaunch))}
	copy(copyCfg.QuickLaunch, cfg.QuickLaunch)
	return copyCfg
}

func validateWorkspaceQuickLaunch(configPath string, repoCfg *contracts.RepoConfig, cfg *config.Config) []contracts.QuickLaunch {
	if repoCfg == nil {
		return nil
	}
	presets := repoCfg.QuickLaunch
	if len(presets) == 0 {
		return nil
	}
	valid := make([]contracts.QuickLaunch, 0, len(presets))
	seen := make(map[string]bool)
	detected := cfg.GetDetectedRunTargets()

	for _, preset := range presets {
		name := strings.TrimSpace(preset.Name)
		if name == "" {
			fmt.Printf("[workspace] parse error: %s: quick_launch entry missing name\n", configPath)
			continue
		}
		if seen[name] {
			fmt.Printf("[workspace] parse error: %s: quick_launch %q is duplicated\n", configPath, name)
			continue
		}
		command := strings.TrimSpace(preset.Command)
		target := strings.TrimSpace(preset.Target)
		hasCommand := command != ""
		hasTarget := target != ""
		if hasCommand == hasTarget {
			fmt.Printf("[workspace] parse error: %s: quick_launch %q must set either command or target\n", configPath, name)
			continue
		}
		if hasCommand {
			if preset.Prompt != nil && strings.TrimSpace(*preset.Prompt) != "" {
				fmt.Printf("[workspace] parse error: %s: quick_launch %q cannot include prompt for command\n", configPath, name)
				continue
			}
			preset.Name = name
			preset.Command = command
			preset.Target = ""
			preset.Prompt = nil
			valid = append(valid, preset)
			seen[name] = true
			continue
		}

		promptable, found := config.IsTargetPromptable(cfg, detected, target)
		if !found {
			fmt.Printf("[workspace] parse error: %s: quick_launch %q target not found: %s\n", configPath, name, target)
			continue
		}
		prompt := ""
		if preset.Prompt != nil {
			prompt = strings.TrimSpace(*preset.Prompt)
		}
		if promptable && prompt == "" {
			fmt.Printf("[workspace] parse error: %s: quick_launch %q requires prompt\n", configPath, name)
			continue
		}
		if !promptable && prompt != "" {
			fmt.Printf("[workspace] parse error: %s: quick_launch %q cannot include prompt for command target\n", configPath, name)
			continue
		}
		preset.Name = name
		preset.Command = ""
		preset.Target = target
		if preset.Prompt != nil && prompt == "" {
			preset.Prompt = nil
		}
		valid = append(valid, preset)
		seen[name] = true
	}
	return valid
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

	// Find base repo for worktree cleanup (works even if directory is gone)
	baseRepoPath, baseRepoErr := m.findBaseRepoForWorkspace(w)

	// Delete workspace directory (worktree or legacy full clone)
	if dirExists {
		if isWorktree(w.Path) {
			// Use git worktree remove for worktrees
			if baseRepoErr != nil {
				fmt.Printf("[workspace] warning: could not find base repo, falling back to rm: %v\n", baseRepoErr)
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
	}

	// Prune stale worktree references (handles case where directory was already deleted)
	if baseRepoErr == nil {
		if err := m.pruneWorktrees(ctx, baseRepoPath); err != nil {
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
