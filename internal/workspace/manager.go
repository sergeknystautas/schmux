package workspace

import (
	"fmt"
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
	config *config.Config
	state  *state.State
}

// New creates a new workspace manager.
func New(cfg *config.Config, st *state.State) *Manager {
	return &Manager{
		config: cfg,
		state:  st,
	}
}

// GetOrCreate finds an available workspace or creates a new one.
func (m *Manager) GetOrCreate(repo, branch string) (*state.Workspace, error) {
	// Try to find an available workspace
	if w, found := m.state.FindAvailableWorkspace(repo); found {
		return &w, nil
	}

	// Create a new workspace
	return m.create(repo)
}

// create creates a new workspace directory for the given repo.
func (m *Manager) create(repo string) (*state.Workspace, error) {
	// Find the next available workspace number
	workspaces := m.getWorkspacesForRepo(repo)
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
	workspaceID := fmt.Sprintf("%s-%03d", repo, nextNum)

	// Create full path
	workspacePath := filepath.Join(m.config.GetWorkspacePath(), workspaceID)

	// Clone the repository
	repoConfig, found := m.config.FindRepo(repo)
	if !found {
		return nil, fmt.Errorf("repo not found in config: %s", repo)
	}

	if err := m.cloneRepo(repoConfig.URL, workspacePath); err != nil {
		return nil, fmt.Errorf("failed to clone repo: %w", err)
	}

	// Create workspace state
	w := state.Workspace{
		ID:     workspaceID,
		Repo:   repo,
		Path:   workspacePath,
		InUse:  false,
		Usable: true,
	}

	m.state.AddWorkspace(w)
	if err := m.state.Save(); err != nil {
		return nil, fmt.Errorf("failed to save state: %w", err)
	}

	return &w, nil
}

// Prepare prepares a workspace for use (git checkout, pull).
func (m *Manager) Prepare(workspaceID, branch string) error {
	w, found := m.state.GetWorkspace(workspaceID)
	if !found {
		return fmt.Errorf("workspace not found: %s", workspaceID)
	}

	if !w.Usable {
		return fmt.Errorf("workspace is not usable: %s", workspaceID)
	}

	// Fetch latest
	if err := m.gitFetch(w.Path); err != nil {
		w.Usable = false
		m.state.UpdateWorkspace(w)
		m.state.Save()
		return fmt.Errorf("git fetch failed: %w", err)
	}

	// Checkout branch
	if err := m.gitCheckout(w.Path, branch); err != nil {
		w.Usable = false
		m.state.UpdateWorkspace(w)
		m.state.Save()
		return fmt.Errorf("git checkout failed: %w", err)
	}

	// Pull with rebase
	if err := m.gitPullRebase(w.Path); err != nil {
		w.Usable = false
		m.state.UpdateWorkspace(w)
		m.state.Save()
		return fmt.Errorf("git pull --rebase failed (conflicts?): %w", err)
	}

	return nil
}

// Cleanup cleans up a workspace by resetting git state.
func (m *Manager) Cleanup(workspaceID string) error {
	w, found := m.state.GetWorkspace(workspaceID)
	if !found {
		return fmt.Errorf("workspace not found: %s", workspaceID)
	}

	// Reset all changes
	if err := m.gitCheckoutDot(w.Path); err != nil {
		return fmt.Errorf("git checkout -- . failed: %w", err)
	}

	// Clean untracked files
	if err := m.gitClean(w.Path); err != nil {
		return fmt.Errorf("git clean failed: %w", err)
	}

	return nil
}

// MarkInUse marks a workspace as in use.
func (m *Manager) MarkInUse(workspaceID, sessionID string) error {
	w, found := m.state.GetWorkspace(workspaceID)
	if !found {
		return fmt.Errorf("workspace not found: %s", workspaceID)
	}

	w.InUse = true
	w.SessionID = sessionID
	m.state.UpdateWorkspace(w)

	return m.state.Save()
}

// Release releases a workspace from use.
func (m *Manager) Release(workspaceID string) error {
	w, found := m.state.GetWorkspace(workspaceID)
	if !found {
		return fmt.Errorf("workspace not found: %s", workspaceID)
	}

	w.InUse = false
	w.SessionID = ""
	m.state.UpdateWorkspace(w)

	return m.state.Save()
}

// getWorkspacesForRepo returns all workspaces for a given repo.
func (m *Manager) getWorkspacesForRepo(repo string) []state.Workspace {
	var result []state.Workspace
	for _, w := range m.state.Workspaces {
		if w.Repo == repo {
			result = append(result, w)
		}
	}
	return result
}

// cloneRepo clones a repository to the given path.
func (m *Manager) cloneRepo(url, path string) error {
	args := []string{"clone", url, path}
	cmd := exec.Command("git", args...)

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git clone failed: %w: %s", err, string(output))
	}

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

// gitCheckout runs git checkout.
func (m *Manager) gitCheckout(dir, branch string) error {
	args := []string{"checkout", branch}
	cmd := exec.Command("git", args...)
	cmd.Dir = dir

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git checkout failed: %w: %s", err, string(output))
	}

	return nil
}

// gitPullRebase runs git pull --rebase.
func (m *Manager) gitPullRebase(dir string) error {
	args := []string{"pull", "--rebase"}
	cmd := exec.Command("git", args...)
	cmd.Dir = dir

	if output, err := cmd.CombinedOutput(); err != nil {
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
