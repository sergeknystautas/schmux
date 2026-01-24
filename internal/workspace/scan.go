package workspace

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sergeknystautas/schmux/internal/state"
)

// Scan scans the workspace directory and reconciles state with filesystem.
// Returns what was added, updated, and removed.
func (m *Manager) Scan() (ScanResult, error) {
	log.Printf("[workspace] scanning workspace directory: %s", m.config.GetWorkspacePath())

	result := ScanResult{
		Added:   []state.Workspace{},
		Updated: []WorkspaceChange{},
		Removed: []state.Workspace{},
	}

	workspaceBasePath := m.config.GetWorkspacePath()

	// Step 1: Scan filesystem for git repos
	type fsRepoInfo struct {
		path   string
		branch string
		repo   string
	}

	fsRepos := make(map[string]fsRepoInfo)

	entries, err := os.ReadDir(workspaceBasePath)
	if err != nil && !os.IsNotExist(err) {
		return ScanResult{}, fmt.Errorf("failed to read workspace directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		dirPath := filepath.Join(workspaceBasePath, entry.Name())
		gitDir := filepath.Join(dirPath, ".git")

		// Check if it's a git repo
		if _, err := os.Stat(gitDir); err != nil {
			continue
		}

		// Get current branch
		branch, err := m.gitGetCurrentBranch(dirPath)
		if err != nil {
			log.Printf("[workspace] failed to get branch for %s: %v", dirPath, err)
			continue
		}

		// Get remote URL
		repoURL, err := m.gitGetRemoteURL(dirPath)
		if err != nil {
			log.Printf("[workspace] failed to get remote URL for %s: %v", dirPath, err)
			continue
		}

		// Only include repos that are in our config
		if _, found := m.findRepoByURL(repoURL); !found {
			log.Printf("[workspace] skipping unconfigured repo: %s", repoURL)
			continue
		}

		fsRepos[entry.Name()] = fsRepoInfo{
			path:   dirPath,
			branch: branch,
			repo:   repoURL,
		}
	}

	// Step 2: Validate existing workspaces and check for updates
	existingWorkspaces := m.state.GetWorkspaces()
	for _, ws := range existingWorkspaces {
		// Check if workspace has active sessions - skip these
		hasActiveSessions := false
		for _, s := range m.state.GetSessions() {
			if s.WorkspaceID == ws.ID {
				hasActiveSessions = true
				break
			}
		}
		if hasActiveSessions {
			continue
		}

		// Get directory name from path
		dirName := filepath.Base(ws.Path)

		fsInfo, foundInFS := fsRepos[dirName]
		if !foundInFS {
			// Workspace no longer exists or is not a git repo - remove it
			m.state.RemoveWorkspace(ws.ID)
			result.Removed = append(result.Removed, ws)
			log.Printf("[workspace] removed workspace: id=%s (not found in filesystem)", ws.ID)
			continue
		}

		// Check if branch or repo changed
		if fsInfo.branch != ws.Branch || fsInfo.repo != ws.Repo {
			oldWS := ws // Capture old state before modifying
			ws.Branch = fsInfo.branch
			ws.Repo = fsInfo.repo
			m.state.UpdateWorkspace(ws)
			result.Updated = append(result.Updated, WorkspaceChange{
				Old: oldWS,
				New: ws,
			})
			log.Printf("[workspace] updated workspace: id=%s branch=%s repo=%s", ws.ID, ws.Branch, ws.Repo)
		}

		// Remove from fsRepos so we know it's been processed
		delete(fsRepos, dirName)
	}

	// Step 3: Add new workspaces that we found but weren't in state
	for dirName, fsInfo := range fsRepos {
		// Create a workspace ID from the directory name
		workspaceID := dirName

		// Check if this workspace ID already exists (shouldn't happen but be safe)
		if _, exists := m.state.GetWorkspace(workspaceID); exists {
			continue
		}

		newWS := state.Workspace{
			ID:     workspaceID,
			Repo:   fsInfo.repo,
			Branch: fsInfo.branch,
			Path:   fsInfo.path,
		}
		m.state.AddWorkspace(newWS)
		result.Added = append(result.Added, newWS)
		log.Printf("[workspace] added workspace: id=%s repo=%s branch=%s", newWS.ID, newWS.Repo, newWS.Branch)
	}

	// Step 4: Save state if anything changed
	if len(result.Added) > 0 || len(result.Updated) > 0 || len(result.Removed) > 0 {
		if err := m.state.Save(); err != nil {
			return ScanResult{}, fmt.Errorf("failed to save state: %w", err)
		}
	}

	log.Printf("[workspace] scan complete: added=%d updated=%d removed=%d", len(result.Added), len(result.Updated), len(result.Removed))
	return result, nil
}

// gitGetCurrentBranch returns the current branch name of a git repository.
func (m *Manager) gitGetCurrentBranch(dir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse failed: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// gitGetRemoteURL returns the origin remote URL of a git repository.
func (m *Manager) gitGetRemoteURL(dir string) (string, error) {
	cmd := exec.Command("git", "config", "--get", "remote.origin.url")
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git config remote.origin.url failed: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}
