package workspace

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/charmbracelet/log"
	"github.com/sergeknystautas/schmux/internal/compound"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
)

// OverlayDir returns the overlay directory path for a given repo name.
// Returns <schmuxdir>/overlays/<repoName>/.
func OverlayDir(repoName string) (string, error) {
	return filepath.Join(schmuxdir.Get(), "overlays", repoName), nil
}

// EnsureOverlayDir ensures the overlay directory exists for a given repo name.
// Creates the directory if it doesn't exist.
func EnsureOverlayDir(repoName string) error {
	overlayDir, err := OverlayDir(repoName)
	if err != nil {
		return err
	}

	// Check if directory already exists
	if _, err := os.Stat(overlayDir); err == nil {
		return nil // Already exists
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to check overlay directory: %w", err)
	}

	// Create the directory
	if err := os.MkdirAll(overlayDir, 0755); err != nil {
		return fmt.Errorf("failed to create overlay directory: %w", err)
	}

	return nil
}

// CopyOverlay copies overlay files from srcDir (overlay) to destDir (workspace).
// Only copies files that are covered by .gitignore in the destination workspace.
// Preserves directory structure, file permissions, and symlinks.
// Returns a manifest mapping relative paths to SHA-256 hashes of copied files.
func CopyOverlay(ctx context.Context, srcDir, destDir string, logger *log.Logger) (map[string]string, error) {
	manifest := make(map[string]string)

	// Walk the overlay directory
	err := filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Get the relative path from overlay root
		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			return fmt.Errorf("failed to get relative path: %w", err)
		}

		// Skip the overlay root directory itself
		if relPath == "." {
			return nil
		}

		// Destination path in workspace
		destPath := filepath.Join(destDir, relPath)

		if d.IsDir() {
			// Create directory in workspace
			if err := os.MkdirAll(destPath, 0755); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", destPath, err)
			}
			return nil
		}

		// For files, check if covered by .gitignore
		ignored, err := isIgnoredByGit(ctx, destDir, relPath)
		if err != nil {
			logger.Warn("failed to check gitignore for overlay file", "path", relPath, "err", err)
			// Skip files if we can't verify gitignore coverage
			return nil
		}
		if !ignored {
			logger.Warn("skipping overlay file (not in .gitignore)", "path", relPath)
			return nil
		}

		// Check if this is a symlink (must use d.Type(), not info.Mode(),
		// because DirEntry.Info() follows symlinks and loses the symlink flag)
		if d.Type()&os.ModeSymlink != 0 {
			// Copy symlink as-is
			target, err := os.Readlink(path)
			if err != nil {
				return fmt.Errorf("failed to read symlink %s: %w", path, err)
			}
			if err := os.Symlink(target, destPath); err != nil {
				return fmt.Errorf("failed to create symlink %s: %w", destPath, err)
			}
			logger.Debug("copied overlay symlink", "path", relPath, "target", target)
			return nil
		}

		// Get file info for permissions
		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("failed to get file info for %s: %w", path, err)
		}

		// Copy regular file
		if err := copyFile(path, destPath, info.Mode()); err != nil {
			return fmt.Errorf("failed to copy %s to %s: %w", path, destPath, err)
		}

		// Compute SHA-256 hash of the copied file
		hash, err := compound.FileHash(destPath)
		if err != nil {
			return fmt.Errorf("failed to hash %s: %w", destPath, err)
		}
		manifest[relPath] = hash

		logger.Debug("copied overlay file", "path", relPath)

		return nil
	})

	if err != nil {
		return nil, err
	}
	return manifest, nil
}

// copyFile copies a single file from src to dst with the given mode.
// Uses io.Copy for efficient copying of large files.
func copyFile(src, dst string, mode fs.FileMode) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}

	_, copyErr := io.Copy(dstFile, srcFile)
	closeErr := dstFile.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

// isIgnoredByGit checks if a file path is covered by .gitignore in the given directory.
// Uses `git check-ignore -q <path>` which returns exit code 0 if ignored, 1 if not.
// For non-git directories (e.g., sapling workspaces), returns false (not ignored).
func isIgnoredByGit(ctx context.Context, dir, filePath string) (bool, error) {
	dotGit := filepath.Join(dir, ".git")
	if _, err := os.Stat(dotGit); err != nil {
		return false, nil
	}

	cmd := exec.CommandContext(ctx, "git", "check-ignore", "-q", filePath)
	cmd.Dir = dir

	// Run the command
	err := cmd.Run()

	// git check-ignore returns:
	// - exit code 0 if file IS ignored
	// - exit code 1 if file is NOT ignored
	// - other errors for actual failures
	if err == nil {
		return true, nil // File is ignored
	}

	// Check if this is the expected "not ignored" exit code
	if exitError, ok := err.(*exec.ExitError); ok && exitError.ExitCode() == 1 {
		return false, nil // File is not ignored
	}

	// Some other error occurred
	return false, fmt.Errorf("git check-ignore failed: %w", err)
}

// copyOverlayFiles copies overlay files from the overlay directory to the workspace.
// If the overlay directory doesn't exist, this is a no-op.
// Returns a manifest mapping relative paths to SHA-256 hashes of copied files.
func (m *Manager) copyOverlayFiles(ctx context.Context, repoName, workspacePath string) (map[string]string, error) {
	overlayDir, err := OverlayDir(repoName)
	if err != nil {
		return nil, fmt.Errorf("failed to get overlay directory: %w", err)
	}

	// Check if overlay directory exists
	if _, err := os.Stat(overlayDir); os.IsNotExist(err) {
		m.logger.Debug("no overlay directory for repo, skipping", "repo", repoName)
		return nil, nil
	}

	m.logger.Info("copying overlay files", "repo", repoName, "to", workspacePath)
	manifest, err := CopyOverlay(ctx, overlayDir, workspacePath, m.logger)
	if err != nil {
		return nil, fmt.Errorf("failed to copy overlay files: %w", err)
	}

	m.logger.Info("overlay files copied successfully")
	return manifest, nil
}

// RefreshOverlay reapplies overlay files to an existing workspace
// and ensures schmux-managed configuration is up to date.
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

	m.logger.Info("refreshing overlay", "id", workspaceID, "repo", repoConfig.Name)

	manifest, err := m.copyOverlayFiles(ctx, repoConfig.Name, w.Path)
	if err != nil {
		return fmt.Errorf("failed to copy overlay files: %w", err)
	}

	if manifest != nil {
		m.state.UpdateOverlayManifest(workspaceID, manifest)
	}

	// Ensure schmux-managed configuration (hooks, scripts, git exclude, etc.)
	if err := m.ensurer.ForWorkspace(workspaceID); err != nil {
		m.logger.Warn("failed to ensure workspace config", "err", err)
	}

	m.logger.Info("overlay refreshed successfully", "id", workspaceID)
	return nil
}

// cleanStaleOverlayFiles removes overlay files from a workspace that existed in the
// previous lifecycle but are absent from the fresh overlay manifest. During workspace
// recycling, git clean -fd does not remove gitignored files, so overlay files from
// the previous lifecycle can persist on disk. This function removes those stale files
// to prevent them from influencing a new agent or being propagated through the compound
// system via declared paths.
func cleanStaleOverlayFiles(oldManifest, freshManifest map[string]string, workspacePath string, declaredPaths []string, logger *log.Logger) {
	// Collect all paths that might have stale files: old manifest entries
	// plus declared paths (config-driven, may have been created by the previous agent).
	candidates := make(map[string]bool)
	for relPath := range oldManifest {
		candidates[relPath] = true
	}
	for _, relPath := range declaredPaths {
		candidates[relPath] = true
	}

	for relPath := range candidates {
		if _, current := freshManifest[relPath]; current {
			continue // File was freshly copied from overlay dir
		}
		absPath := filepath.Join(workspacePath, relPath)
		if err := os.Remove(absPath); err != nil {
			if !os.IsNotExist(err) {
				logger.Warn("failed to remove stale overlay file", "path", relPath, "err", err)
			}
			continue
		}
		logger.Info("removed stale overlay file", "path", relPath)
	}
}

// EnsureOverlayDirs ensures overlay directories exist for all configured repos.
func (m *Manager) EnsureOverlayDirs(repos []config.Repo) error {
	for _, repo := range repos {
		if err := EnsureOverlayDir(repo.Name); err != nil {
			return fmt.Errorf("failed to ensure overlay directory for %s: %w", repo.Name, err)
		}
	}
	m.logger.Info("ensured overlay directories", "count", len(repos))
	return nil
}

// ListOverlayFiles returns a list of files in the overlay directory for a repo.
// Returns relative paths from the overlay root.
func ListOverlayFiles(repoName string) ([]string, error) {
	overlayDir, err := OverlayDir(repoName)
	if err != nil {
		return nil, err
	}

	var files []string
	err = filepath.WalkDir(overlayDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Get the relative path from overlay root
		relPath, err := filepath.Rel(overlayDir, path)
		if err != nil {
			return err
		}

		// Skip the overlay root directory itself
		if relPath == "." {
			return nil
		}

		// Only add files (not directories)
		if !d.IsDir() {
			files = append(files, relPath)
		}

		return nil
	})

	if err != nil {
		// If overlay directory doesn't exist, return empty list (not an error)
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}

	return files, nil
}
