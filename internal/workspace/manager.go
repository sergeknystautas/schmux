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

	"github.com/sergek/schmux/internal/config"
	"github.com/sergek/schmux/internal/state"
)

// Manager manages workspace directories.
type Manager struct {
	config    *config.Config
	state     state.StateStore
	statePath string
	logger    *log.Logger
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
			config:    cfg,
			state:     st,
			statePath: statePath,
			logger:    log.New(os.Stderr, "[workspace] ", log.LstdFlags),
		}
	}

	return &Manager{
		config:    cfg,
		state:     st,
		statePath: statePath,
		logger:    log.New(logFile, "[workspace] ", log.LstdFlags),
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

// GetOrCreate finds an existing workspace for the repoURL/branch or creates a new one.
// Returns a workspace ready for use (fetch/pull/clean already done).
func (m *Manager) GetOrCreate(ctx context.Context, repoURL, branch string) (*state.Workspace, error) {
	// Try to find an existing workspace with matching repoURL and branch
	for _, w := range m.state.GetWorkspaces() {
		// Check if workspace directory still exists
		if _, err := os.Stat(w.Path); os.IsNotExist(err) {
			m.logger.Printf("workspace directory missing, skipping: id=%s path=%s", w.ID, w.Path)
			continue
		}
		if w.Repo == repoURL && w.Branch == branch {
			// Check if workspace has active sessions
			hasActiveSessions := false
			for _, s := range m.state.GetSessions() {
				if s.WorkspaceID == w.ID {
					hasActiveSessions = true
					break
				}
			}
			if !hasActiveSessions {
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
			hasActiveSessions := false
			for _, s := range m.state.GetSessions() {
				if s.WorkspaceID == w.ID {
					hasActiveSessions = true
					break
				}
			}
			if !hasActiveSessions {
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
	workspaceID := fmt.Sprintf("%s-%03d", repoConfig.Name, nextNum)

	// Create full path
	workspacePath := filepath.Join(m.config.GetWorkspacePath(), workspaceID)

	// Clone the repository
	if err := m.cloneRepo(ctx, repoURL, workspacePath); err != nil {
		return nil, fmt.Errorf("failed to clone repo: %w", err)
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

	return &w, nil
}

// prepare prepares a workspace for use (git checkout, pull, clean).
func (m *Manager) prepare(ctx context.Context, workspaceID, branch string) error {
	w, found := m.state.GetWorkspace(workspaceID)
	if !found {
		return fmt.Errorf("workspace not found: %s", workspaceID)
	}

	// Check if workspace has active sessions
	for _, s := range m.state.GetSessions() {
		if s.WorkspaceID == workspaceID {
			return fmt.Errorf("workspace has active sessions: %s", workspaceID)
		}
	}

	m.logger.Printf("preparing workspace: id=%s branch=%s", workspaceID, branch)

	// Fetch latest
	if err := m.gitFetch(ctx, w.Path); err != nil {
		return fmt.Errorf("git fetch failed: %w", err)
	}

	// Checkout branch
	if err := m.gitCheckout(ctx, w.Path, branch); err != nil {
		return fmt.Errorf("git checkout failed: %w", err)
	}

	// Discard any local changes (must happen before pull)
	if err := m.gitCheckoutDot(ctx, w.Path); err != nil {
		return fmt.Errorf("git checkout -- . failed: %w", err)
	}

	// Clean untracked files and directories (must happen before pull)
	if err := m.gitClean(ctx, w.Path); err != nil {
		return fmt.Errorf("git clean failed: %w", err)
	}

	// Pull with rebase (working dir is now clean)
	if err := m.gitPullRebase(ctx, w.Path); err != nil {
		return fmt.Errorf("git pull --rebase failed (conflicts?): %w", err)
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

// gitCheckout runs git checkout, falling back to creating a new branch if needed.
func (m *Manager) gitCheckout(ctx context.Context, dir, branch string) error {
	// Try regular checkout first (handles existing local/remote branches)
	args := []string{"checkout", branch}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir

	if _, err := cmd.CombinedOutput(); err != nil {
		// If checkout failed, try creating a new branch
		args = []string{"checkout", "-b", branch}
		cmd = exec.CommandContext(ctx, "git", args...)
		cmd.Dir = dir

		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git checkout failed: %w: %s", err, string(output))
		}
	}

	return nil
}

// gitPullRebase runs git pull --rebase.
func (m *Manager) gitPullRebase(ctx context.Context, dir string) error {
	args := []string{"pull", "--rebase"}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir

	if output, err := cmd.CombinedOutput(); err != nil {
		// If pull fails due to no tracking info, that's ok for new local branches
		if strings.Contains(string(output), "no tracking information") {
			m.logger.Printf("no tracking information for branch, skipping pull")
			return nil
		}
		return fmt.Errorf("git pull failed: %w: %s", err, string(output))
	}

	return nil
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

	// Update workspace in memory
	w.GitDirty = dirty
	w.GitAhead = ahead
	w.GitBehind = behind

	// Update the workspace in state (this updates the in-memory copy)
	if err := m.state.UpdateWorkspace(w); err != nil {
		return nil, fmt.Errorf("failed to update workspace in state: %w", err)
	}

	return &w, nil
}

// gitStatus calculates the git status for a workspace directory.
// Returns: (dirty bool, ahead int, behind int)
func (m *Manager) gitStatus(ctx context.Context, dir string) (dirty bool, ahead int, behind int) {
	// Check for dirty state (any changes: modified, added, removed, or untracked)
	statusCmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	statusCmd.Dir = dir
	output, err := statusCmd.CombinedOutput()
	if err == nil && len(strings.TrimSpace(string(output))) > 0 {
		dirty = true
	}

	// Check ahead/behind counts using rev-list
	// @{u} is the shortcut for the upstream branch
	revListCmd := exec.CommandContext(ctx, "git", "rev-list", "--left-right", "--count", "HEAD...@{u}")
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
		fmt.Sscanf(parts[0], "%d", &ahead)
		fmt.Sscanf(parts[1], "%d", &behind)
	}

	return dirty, ahead, behind
}

// UpdateAllGitStatus refreshes git status for all workspaces.
// This is called periodically by the background goroutine.
func (m *Manager) UpdateAllGitStatus(ctx context.Context) {
	workspaces := m.state.GetWorkspaces()
	for _, w := range workspaces {
		if _, err := m.UpdateGitStatus(ctx, w.ID); err != nil {
			m.logger.Printf("failed to update git status for workspace %s: %v", w.ID, err)
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

	m.logger.Printf("disposing workspace: id=%s path=%s", workspaceID, w.Path)

	// Check if workspace has active sessions
	for _, s := range m.state.GetSessions() {
		if s.WorkspaceID == workspaceID {
			return fmt.Errorf("workspace has active sessions: %s", workspaceID)
		}
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

	m.logger.Printf("workspace disposed: id=%s", workspaceID)
	return nil
}
