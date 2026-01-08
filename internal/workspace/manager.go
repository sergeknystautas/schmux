package workspace

import (
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
	state     *state.State
	statePath string
	logger    *log.Logger
}

// New creates a new workspace manager.
func New(cfg *config.Config, st *state.State, statePath string) *Manager {
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
func (m *Manager) GetOrCreate(repoURL, branch string) (*state.Workspace, error) {
	// Try to find an existing workspace with matching repoURL and branch
	for _, w := range m.state.Workspaces {
		// Check if workspace directory still exists
		if _, err := os.Stat(w.Path); os.IsNotExist(err) {
			m.logger.Printf("workspace directory missing, skipping: id=%s path=%s", w.ID, w.Path)
			continue
		}
		if w.Repo == repoURL && w.Branch == branch {
			// Check if workspace has active sessions
			hasActiveSessions := false
			for _, s := range m.state.Sessions {
				if s.WorkspaceID == w.ID {
					hasActiveSessions = true
					break
				}
			}
			if !hasActiveSessions {
				m.logger.Printf("reusing existing workspace: id=%s path=%s branch=%s", w.ID, w.Path, branch)
				// Prepare the workspace (fetch/pull/clean)
				if err := m.prepare(w.ID, branch); err != nil {
					return nil, fmt.Errorf("failed to prepare workspace: %w", err)
				}
				return &w, nil
			}
		}
	}

	// Try to find any unused workspace for this repo (different branch OK)
	for _, w := range m.state.Workspaces {
		if w.Repo == repoURL {
			// Check if workspace has active sessions
			hasActiveSessions := false
			for _, s := range m.state.Sessions {
				if s.WorkspaceID == w.ID {
					hasActiveSessions = true
					break
				}
			}
			if !hasActiveSessions {
				m.logger.Printf("reusing workspace for different branch: id=%s old_branch=%s new_branch=%s", w.ID, w.Branch, branch)
				// Prepare the workspace (fetch/pull/clean) BEFORE updating state
				if err := m.prepare(w.ID, branch); err != nil {
					return nil, fmt.Errorf("failed to prepare workspace: %w", err)
				}
				// Update branch in state only after successful prepare
				w.Branch = branch
				m.state.UpdateWorkspace(w)
				return &w, nil
			}
		}
	}

	// Create a new workspace
	w, err := m.create(repoURL, branch)
	if err != nil {
		return nil, err
	}
	m.logger.Printf("created new workspace: id=%s path=%s branch=%s repo=%s", w.ID, w.Path, branch, repoURL)

	// Prepare the workspace
	if err := m.prepare(w.ID, branch); err != nil {
		return nil, fmt.Errorf("failed to prepare workspace: %w", err)
	}

	return w, nil
}

// create creates a new workspace directory for the given repoURL.
func (m *Manager) create(repoURL, branch string) (*state.Workspace, error) {
	// Find repo config by URL
	repoConfig, found := m.findRepoByURL(repoURL)
	if !found {
		return nil, fmt.Errorf("repo URL not found in config: %s", repoURL)
	}

	// Find the next available workspace number
	workspaces := m.getWorkspacesForRepo(repoURL)
	nextNum := len(workspaces) + 1

	// Check for gaps in numbering
	for _, w := range workspaces {
		num, err := extractWorkspaceNumber(w.ID)
		if err != nil {
			continue
		}
		if num >= nextNum {
			nextNum = num + 1
		}
	}

	// Create workspace ID
	workspaceID := fmt.Sprintf("%s-%03d", repoConfig.Name, nextNum)

	// Create full path
	workspacePath := filepath.Join(m.config.GetWorkspacePath(), workspaceID)

	// Clone the repository
	if err := m.cloneRepo(repoURL, workspacePath); err != nil {
		return nil, fmt.Errorf("failed to clone repo: %w", err)
	}

	// Create workspace state with branch
	w := state.Workspace{
		ID:     workspaceID,
		Repo:   repoURL,
		Branch: branch,
		Path:   workspacePath,
	}

	m.state.AddWorkspace(w)
	if err := state.Save(m.state, m.statePath); err != nil {
		return nil, fmt.Errorf("failed to save state: %w", err)
	}

	return &w, nil
}

// prepare prepares a workspace for use (git checkout, pull, clean).
func (m *Manager) prepare(workspaceID, branch string) error {
	w, found := m.state.GetWorkspace(workspaceID)
	if !found {
		return fmt.Errorf("workspace not found: %s", workspaceID)
	}

	// Check if workspace has active sessions
	for _, s := range m.state.Sessions {
		if s.WorkspaceID == workspaceID {
			return fmt.Errorf("workspace has active sessions: %s", workspaceID)
		}
	}

	m.logger.Printf("preparing workspace: id=%s branch=%s", workspaceID, branch)

	// Fetch latest
	if err := m.gitFetch(w.Path); err != nil {
		return fmt.Errorf("git fetch failed: %w", err)
	}

	// Checkout branch
	if err := m.gitCheckout(w.Path, branch); err != nil {
		return fmt.Errorf("git checkout failed: %w", err)
	}

	// Discard any local changes (must happen before pull)
	if err := m.gitCheckoutDot(w.Path); err != nil {
		return fmt.Errorf("git checkout -- . failed: %w", err)
	}

	// Clean untracked files and directories (must happen before pull)
	if err := m.gitClean(w.Path); err != nil {
		return fmt.Errorf("git clean failed: %w", err)
	}

	// Pull with rebase (working dir is now clean)
	if err := m.gitPullRebase(w.Path); err != nil {
		return fmt.Errorf("git pull --rebase failed (conflicts?): %w", err)
	}

	m.logger.Printf("workspace prepared: id=%s branch=%s", workspaceID, branch)
	return nil
}

// Cleanup cleans up a workspace by resetting git state.
func (m *Manager) Cleanup(workspaceID string) error {
	w, found := m.state.GetWorkspace(workspaceID)
	if !found {
		return fmt.Errorf("workspace not found: %s", workspaceID)
	}

	m.logger.Printf("cleaning up workspace: id=%s path=%s", workspaceID, w.Path)

	// Reset all changes
	if err := m.gitCheckoutDot(w.Path); err != nil {
		return fmt.Errorf("git checkout -- . failed: %w", err)
	}

	// Clean untracked files
	if err := m.gitClean(w.Path); err != nil {
		return fmt.Errorf("git clean failed: %w", err)
	}

	m.logger.Printf("workspace cleaned: id=%s", workspaceID)
	return nil
}

// getWorkspacesForRepo returns all workspaces for a given repoURL.
func (m *Manager) getWorkspacesForRepo(repoURL string) []state.Workspace {
	var result []state.Workspace
	for _, w := range m.state.Workspaces {
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
func (m *Manager) cloneRepo(url, path string) error {
	m.logger.Printf("cloning repository: url=%s path=%s", url, path)
	args := []string{"clone", url, path}
	cmd := exec.Command("git", args...)

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git clone failed: %w: %s", err, string(output))
	}

	m.logger.Printf("repository cloned: path=%s", path)
	return nil
}

// gitFetch runs git fetch.
func (m *Manager) gitFetch(dir string) error {
	args := []string{"fetch"}
	cmd := exec.Command("git", args...)
	cmd.Dir = dir

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git fetch failed: %w: %s", err, string(output))
	}

	return nil
}

// gitCheckout runs git checkout, falling back to creating a new branch if needed.
func (m *Manager) gitCheckout(dir, branch string) error {
	// Try regular checkout first (handles existing local/remote branches)
	args := []string{"checkout", branch}
	cmd := exec.Command("git", args...)
	cmd.Dir = dir

	if _, err := cmd.CombinedOutput(); err != nil {
		// If checkout failed, try creating a new branch
		args = []string{"checkout", "-b", branch}
		cmd = exec.Command("git", args...)
		cmd.Dir = dir

		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git checkout failed: %w: %s", err, string(output))
		}
	}

	return nil
}

// gitPullRebase runs git pull --rebase.
func (m *Manager) gitPullRebase(dir string) error {
	args := []string{"pull", "--rebase"}
	cmd := exec.Command("git", args...)
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
func (m *Manager) gitCheckoutDot(dir string) error {
	args := []string{"checkout", "--", "."}
	cmd := exec.Command("git", args...)
	cmd.Dir = dir

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git checkout -- . failed: %w: %s", err, string(output))
	}

	return nil
}

// gitClean runs git clean -fd.
func (m *Manager) gitClean(dir string) error {
	args := []string{"clean", "-fd"}
	cmd := exec.Command("git", args...)
	cmd.Dir = dir

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git clean failed: %w: %s", err, string(output))
	}

	return nil
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

// EnsureWorkspaceDir ensures the workspace base directory exists.
func (m *Manager) EnsureWorkspaceDir() error {
	path := m.config.GetWorkspacePath()
	if err := os.MkdirAll(path, 0755); err != nil {
		return fmt.Errorf("failed to create workspace directory: %w", err)
	}
	return nil
}
