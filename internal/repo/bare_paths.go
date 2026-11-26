package repo

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sergeknystautas/schmux/internal/config"
)

// PopulateBarePaths detects and populates bare_path for repos that don't have it.
// This is a one-time migration for existing repos - new repos get bare_path set on creation.
// Detection order:
//  1. Check worktree base dir (repos/) for legacy flat path (repo.git) that matches URL
//  2. Check query dir (query/) for legacy flat path that matches URL
//  3. Check worktree base dir for namespaced path (owner/repo.git) that matches URL
//  4. Check query dir for namespaced path that matches URL
//  5. Fall back to {name}.git if nothing exists on disk
func (m *Manager) PopulateBarePaths() {
	m.mu.Lock()
	defer m.mu.Unlock()

	var changed bool
	reposPath := m.config.GetWorktreeBasePath()
	queryPath := m.config.GetQueryRepoPath()

	for _, repo := range m.state.GetRepos() {
		if repo.BarePath != "" {
			// Validate that the configured bare_path actually exists on disk.
			// If it doesn't, re-detect — the repo may have been cloned under
			// a namespaced path (e.g. "owner/repo.git") while the entry has
			// the flat name ("repo.git").
			exists := false
			for _, basePath := range []string{reposPath, queryPath} {
				fullPath := filepath.Join(basePath, repo.BarePath)
				if _, err := os.Stat(fullPath); err == nil {
					exists = true
					break
				}
			}
			if exists {
				continue // bare_path is valid
			}
			// Only correct if we find the actual repo on disk (no fallback)
			if found := findBarePathOnDisk([]string{reposPath, queryPath}, repo.URL); found != "" && found != repo.BarePath {
				fmt.Fprintf(os.Stderr, "[repo] corrected bare_path for repo %q: %s → %s\n", repo.Name, repo.BarePath, found)
				repo.BarePath = found
				changed = true
				if err := m.state.UpdateRepo(repo); err != nil {
					fmt.Fprintf(os.Stderr, "[repo] warning: could not update bare_path for repo %q: %v\n", repo.Name, err)
				}
			}
			continue
		}

		// Empty BarePath — detect with fallback for new repos
		barePath := detectExistingBarePath([]string{reposPath, queryPath}, repo.URL, repo.Name)
		if barePath != "" {
			repo.BarePath = barePath
			changed = true
			fmt.Fprintf(os.Stderr, "[repo] migrated bare_path for repo %q: %s\n", repo.Name, barePath)
			if err := m.state.UpdateRepo(repo); err != nil {
				fmt.Fprintf(os.Stderr, "[repo] warning: could not update bare_path for repo %q: %v\n", repo.Name, err)
			}
		}
	}

	if changed {
		// Best-effort save
		if err := m.state.Save(); err != nil {
			fmt.Fprintf(os.Stderr, "[repo] warning: could not save migrated bare_paths: %v\n", err)
		}
	}
}

// NormalizeBarePaths renames non-conforming bare repo directories to {name}.git
// and updates the repo entries and worktree references.
// Skips Sapling repos (different base-path semantics).
// Should be called at daemon startup after config and state are loaded.
func (m *Manager) NormalizeBarePaths() {
	m.mu.Lock()
	defer m.mu.Unlock()

	reposPath := m.config.GetWorktreeBasePath()
	queryPath := m.config.GetQueryRepoPath()
	for _, repo := range m.state.GetRepos() {
		// Skip Sapling repos
		if repo.VCS == "sapling" {
			continue
		}

		// Skip already-conforming repos
		canonical := repo.Name + ".git"
		if repo.BarePath == canonical {
			continue
		}

		// Skip repos with empty BarePath (PopulateBarePaths handles these)
		if repo.BarePath == "" {
			continue
		}

		// Try to normalize in both repos/ and query/ directories.
		// Track whether any rename succeeded — only update BarePath if so.
		renamed := false
		for _, basePath := range []string{reposPath, queryPath} {
			if basePath == "" {
				continue
			}

			oldPath := filepath.Join(basePath, repo.BarePath)
			if _, err := os.Stat(oldPath); err != nil {
				continue // Not on disk in this base path
			}

			newPath := filepath.Join(basePath, canonical)

			if _, err := os.Stat(newPath); err == nil {
				fmt.Fprintf(os.Stderr, "[repo] cannot normalize repo %q: target %s already exists — rename one of the repos with duplicate name %q\n", repo.Name, newPath, repo.Name)
				continue
			}

			if err := relocateBareRepo(oldPath, newPath); err != nil {
				fmt.Fprintf(os.Stderr, "[repo] failed to normalize repo %q from %s to %s: %v\n", repo.Name, oldPath, newPath, err)
				continue
			}

			fmt.Fprintf(os.Stderr, "[repo] normalized bare path for repo %q: %s → %s\n", repo.Name, repo.BarePath, canonical)
			renamed = true

			// Update state RepoBase if this is the repos/ directory
			if basePath == reposPath {
				if rb, found := m.state.GetRepoBaseByURL(repo.URL); found {
					rb.Path = newPath
					m.state.AddRepoBase(rb)
				}
			}
		}

		if renamed {
			repo.BarePath = canonical
			// Save state immediately after each successful normalization.
			// If the process crashes between rename and save, the next startup would
			// see a stale BarePath pointing to a directory that no longer exists.
			if err := m.state.UpdateRepo(repo); err != nil {
				fmt.Fprintf(os.Stderr, "[repo] warning: could not update bare path for repo %q: %v\n", repo.Name, err)
			}
			if err := m.state.Save(); err != nil {
				fmt.Fprintf(os.Stderr, "[repo] warning: could not save state after normalization: %v\n", err)
			}
		}
	}
}

// detectExistingBarePath detects the bare path for a repo by checking what's on disk.
// Checks multiple base paths (repos/, query/) in order.
// Returns the relative path (e.g., "schmux.git" or "owner/repo.git").
func detectExistingBarePath(basePaths []string, repoURL, repoName string) string {
	if found := findBarePathOnDisk(basePaths, repoURL); found != "" {
		return found
	}

	// Fall back to {name}.git for new repos or if nothing on disk
	if repoName != "" {
		return repoName + ".git"
	}

	// Last resort: use URL-derived name
	legacyName := extractRepoNameFromURL(repoURL)
	if legacyName != "" {
		return legacyName + ".git"
	}

	return ""
}

// findBarePathOnDisk looks for an existing bare repo on disk that matches the given URL.
// Returns the relative path if found, or empty string if nothing matches on disk.
func findBarePathOnDisk(basePaths []string, repoURL string) string {
	legacyName := extractRepoNameFromURL(repoURL)
	owner, repo := parseGitHubURL(repoURL)

	for _, basePath := range basePaths {
		// 1. Check for legacy flat path (repo.git)
		if legacyName != "" {
			legacyPath := filepath.Join(basePath, legacyName+".git")
			if bareRepoMatchesURL(legacyPath, repoURL) {
				return legacyName + ".git"
			}
		}

		// 2. Check for namespaced GitHub path (owner/repo.git)
		if owner != "" && repo != "" {
			namespacedPath := filepath.Join(basePath, owner, repo+".git")
			if bareRepoMatchesURL(namespacedPath, repoURL) {
				return filepath.Join(owner, repo+".git")
			}
		}
	}

	return ""
}

// extractRepoNameFromURL extracts the repository name from a URL.
// Handles: git@github.com:user/myrepo.git, https://github.com/user/myrepo.git, etc.
func extractRepoNameFromURL(repoURL string) string {
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

// parseGitHubURL extracts owner and repo from a GitHub URL.
// Returns empty strings if not a GitHub URL.
func parseGitHubURL(repoURL string) (owner, repo string) {
	// Handle git@github.com:owner/repo.git
	if strings.HasPrefix(repoURL, "git@github.com:") {
		path := strings.TrimPrefix(repoURL, "git@github.com:")
		path = strings.TrimSuffix(path, ".git")
		parts := strings.SplitN(path, "/", 2)
		if len(parts) == 2 {
			return parts[0], parts[1]
		}
		return "", ""
	}

	// Handle https://github.com/owner/repo.git
	if strings.HasPrefix(repoURL, "https://github.com/") {
		path := strings.TrimPrefix(repoURL, "https://github.com/")
		path = strings.TrimSuffix(path, ".git")
		parts := strings.SplitN(path, "/", 2)
		if len(parts) == 2 {
			return parts[0], parts[1]
		}
		return "", ""
	}

	return "", ""
}

// bareRepoMatchesURL checks if a bare repo at the given path has the expected origin URL.
func bareRepoMatchesURL(repoPath, expectedURL string) bool {
	return config.BareRepoOrigin(repoPath) == expectedURL
}
