package workspace

import (
	"context"
	"fmt"
	"log"
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
	logger *log.Logger
}

// New creates a new workspace manager.
func New(cfg *config.Config, st state.StateStore, statePath string) *Manager {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = ""
	}
	logPath := filepath.Join(homeDir, ".schmux", "daemon.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		// Fall back to stderr if log file can't be opened
		return &Manager{
			config: cfg,
			state:  st,
			logger: log.New(os.Stderr, "[workspace] ", log.LstdFlags),
		}
	}

	return &Manager{
		config: cfg,
		state:  st,
		logger: log.New(logFile, "[workspace] ", log.LstdFlags),
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
			m.logger.Printf("workspace directory missing, skipping: id=%s path=%s", w.ID, w.Path)
			continue
		}
		if w.Repo == repoURL && w.Branch == branch {
			// Check if workspace has active sessions
			if !m.hasActiveSessions(w.ID) {
				m.logger.Printf("reusing existing workspace: id=%s path=%s branch=%s", w.ID, w.Path, branch)
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
				m.logger.Printf("reusing workspace for different branch: id=%s old_branch=%s new_branch=%s", w.ID, w.Branch, branch)
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
	m.logger.Printf("created new workspace: id=%s path=%s branch=%s repo=%s", w.ID, w.Path, branch, repoURL)

	// Prepare the workspace
	if err := m.prepare(ctx, w.ID, branch); err != nil {
		return nil, fmt.Errorf("failed to prepare workspace: %w", err)
	}

	return w, nil
}

// create creates a new workspace directory for the given repoURL.
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

	// Clean up directory if creation fails (registered before any directory creation)
	cleanupNeeded := true
	defer func() {
		if cleanupNeeded {
			m.logger.Printf("cleaning up failed workspace directory: %s", workspacePath)
			if err := os.RemoveAll(workspacePath); err != nil {
				m.logger.Printf("failed to cleanup workspace directory %s: %v", workspacePath, err)
			}
		}
	}()

	// Clone the repository
	if err := m.cloneRepo(ctx, repoURL, workspacePath); err != nil {
		return nil, fmt.Errorf("failed to clone repo: %w", err)
	}

	// Copy overlay files if they exist
	if err := m.copyOverlayFiles(ctx, repoConfig.Name, workspacePath); err != nil {
		m.logger.Printf("warning: failed to copy overlay files: %v", err)
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
			m.logger.Printf("cleaning up failed local repository directory: %s", workspacePath)
			if err := os.RemoveAll(workspacePath); err != nil {
				m.logger.Printf("failed to cleanup local repository directory %s: %v", workspacePath, err)
			}
		}
	}()

	// Create the directory and initialize a local git repository
	if err := m.initLocalRepo(ctx, workspacePath, branch); err != nil {
		return nil, fmt.Errorf("failed to initialize local repo: %w", err)
	}

	m.logger.Printf("created new local repository: id=%s path=%s branch=%s", workspaceID, workspacePath, branch)

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

	m.logger.Printf("preparing workspace: id=%s branch=%s", workspaceID, branch)

	hasOrigin := m.gitHasOriginRemote(ctx, w.Path)
	if hasOrigin {
		// Fetch latest
		if err := m.gitFetch(ctx, w.Path); err != nil {
			return fmt.Errorf("git fetch failed: %w", err)
		}
	} else {
		m.logger.Printf("no origin remote, skipping fetch")
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
		m.logger.Printf("no origin/%s remote ref, skipping pull", branch)
	}

	m.logger.Printf("workspace prepared: id=%s branch=%s", workspaceID, branch)
	return nil
}

// Cleanup cleans up a workspace by resetting git state.
func (m *Manager) Cleanup(ctx context.Context, workspaceID string) error {
	w, found := m.state.GetWorkspace(workspaceID)
	if !found {
		return fmt.Errorf("workspace not found: %s", workspaceID)
	}

	m.logger.Printf("cleaning up workspace: id=%s path=%s", workspaceID, w.Path)

	// Reset all changes
	if err := m.gitCheckoutDot(ctx, w.Path); err != nil {
		return fmt.Errorf("git checkout -- . failed: %w", err)
	}

	// Clean untracked files
	if err := m.gitClean(ctx, w.Path); err != nil {
		return fmt.Errorf("git clean failed: %w", err)
	}

	m.logger.Printf("workspace cleaned: id=%s", workspaceID)
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
func (m *Manager) cloneRepo(ctx context.Context, url, path string) error {
	m.logger.Printf("cloning repository: url=%s path=%s", url, path)
	args := []string{"clone", url, path}
	cmd := exec.CommandContext(ctx, "git", args...)

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git clone failed: %w: %s", err, string(output))
	}

	m.logger.Printf("repository cloned: path=%s", path)
	return nil
}

// initLocalRepo initializes a new local git repository at the given path.
// It creates the directory, runs git init, creates the initial branch, and makes an empty commit.
func (m *Manager) initLocalRepo(ctx context.Context, path, branch string) error {
	m.logger.Printf("initializing local repository: path=%s branch=%s", path, branch)

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

	m.logger.Printf("local repository initialized: path=%s", path)
	return nil
}

// gitFetch runs git fetch.
func (m *Manager) gitFetch(ctx context.Context, dir string) error {
	args := []string{"fetch"}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir

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
		m.logger.Printf("no origin remote, skipping pull")
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
		m.logger.Printf("failed to get current branch for %s: %v", w.ID, err)
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
		m.logger.Printf("git rev-list failed for %s: %v", dir, err)
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
			m.logger.Printf("failed to update git status for workspace %s: %v", w.ID, err)
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
		m.logger.Printf("no upstream branch for %s, skipping ahead/behind check", workspaceID)
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
		m.logger.Printf("no overlay directory for repo %s, skipping", repoName)
		return nil
	}

	m.logger.Printf("copying overlay files for repo %s to %s", repoName, workspacePath)
	if err := CopyOverlay(ctx, m.logger, overlayDir, workspacePath); err != nil {
		return fmt.Errorf("failed to copy overlay files: %w", err)
	}

	m.logger.Printf("overlay files copied successfully")
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

	m.logger.Printf("refreshing overlay for workspace: id=%s repo=%s", workspaceID, repoConfig.Name)

	if err := m.copyOverlayFiles(ctx, repoConfig.Name, w.Path); err != nil {
		return fmt.Errorf("failed to copy overlay files: %w", err)
	}

	m.logger.Printf("overlay refreshed successfully for workspace: %s", workspaceID)
	return nil
}

// EnsureOverlayDirs ensures overlay directories exist for all configured repos.
func (m *Manager) EnsureOverlayDirs(repos []config.Repo) error {
	for _, repo := range repos {
		if err := EnsureOverlayDir(repo.Name); err != nil {
			return fmt.Errorf("failed to ensure overlay directory for %s: %w", repo.Name, err)
		}
	}
	m.logger.Printf("ensured overlay directories for %d repos", len(repos))
	return nil
}

// Dispose deletes a workspace by removing its directory and removing it from state.
func (m *Manager) Dispose(workspaceID string) error {
	w, found := m.state.GetWorkspace(workspaceID)
	if !found {
		return fmt.Errorf("workspace not found: %s", workspaceID)
	}

	m.logger.Printf("disposing workspace: id=%s path=%s", workspaceID, w.Path)

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

	// Delete workspace directory
	if err := os.RemoveAll(w.Path); err != nil {
		return fmt.Errorf("failed to delete workspace directory: %w", err)
	}

	// Remove from state
	if err := m.state.RemoveWorkspace(workspaceID); err != nil {
		return fmt.Errorf("failed to remove workspace from state: %w", err)
	}
	if err := m.state.Save(); err != nil {
		return fmt.Errorf("failed to save state: %w", err)
	}

	if err := difftool.CleanupWorkspaceTempDirs(workspaceID); err != nil {
		m.logger.Printf("failed to cleanup diff temp dirs for workspace %s: %v", workspaceID, err)
	}

	m.logger.Printf("workspace disposed: id=%s", workspaceID)
	return nil
}
