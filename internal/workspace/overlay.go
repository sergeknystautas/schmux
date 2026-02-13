package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/sergeknystautas/schmux/internal/config"
)

// fileHash computes the SHA-256 hex digest of a file.
func fileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// OverlayDir returns the overlay directory path for a given repo name.
// Returns ~/.schmux/overlays/<repoName>/.
func OverlayDir(repoName string) (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(homeDir, ".schmux", "overlays", repoName), nil
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
func CopyOverlay(ctx context.Context, srcDir, destDir string) (map[string]string, error) {
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
			fmt.Printf("[workspace] WARNING: failed to check gitignore for %s: %v\n", relPath, err)
			// Skip files if we can't verify gitignore coverage
			return nil
		}
		if !ignored {
			fmt.Printf("[workspace] WARNING: skipping overlay file (not in .gitignore): %s\n", relPath)
			return nil
		}

		// Get file info to check permissions and mode
		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("failed to get file info for %s: %w", path, err)
		}

		// Check if this is a symlink
		if info.Mode()&os.ModeSymlink != 0 {
			// Copy symlink as-is
			target, err := os.Readlink(path)
			if err != nil {
				return fmt.Errorf("failed to read symlink %s: %w", path, err)
			}
			if err := os.Symlink(target, destPath); err != nil {
				return fmt.Errorf("failed to create symlink %s: %w", destPath, err)
			}
			fmt.Printf("[workspace] copied overlay symlink: %s -> %s\n", relPath, target)
			return nil
		}

		// Copy regular file
		if err := copyFile(path, destPath, info.Mode()); err != nil {
			return fmt.Errorf("failed to copy %s to %s: %w", path, destPath, err)
		}

		// Compute SHA-256 hash of the copied file
		hash, err := fileHash(destPath)
		if err != nil {
			return fmt.Errorf("failed to hash %s: %w", destPath, err)
		}
		manifest[relPath] = hash

		fmt.Printf("[workspace] copied overlay file: %s\n", relPath)

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
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

// isIgnoredByGit checks if a file path is covered by .gitignore in the given directory.
// Uses `git check-ignore -q <path>` which returns exit code 0 if ignored, 1 if not.
func isIgnoredByGit(ctx context.Context, dir, filePath string) (bool, error) {
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
		fmt.Printf("[workspace] no overlay directory for repo %s, skipping\n", repoName)
		return nil, nil
	}

	fmt.Printf("[workspace] copying overlay files: repo=%s to=%s\n", repoName, workspacePath)
	manifest, err := CopyOverlay(ctx, overlayDir, workspacePath)
	if err != nil {
		return nil, fmt.Errorf("failed to copy overlay files: %w", err)
	}

	fmt.Printf("[workspace] overlay files copied successfully\n")
	return manifest, nil
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

	manifest, err := m.copyOverlayFiles(ctx, repoConfig.Name, w.Path)
	if err != nil {
		return fmt.Errorf("failed to copy overlay files: %w", err)
	}

	if manifest != nil {
		m.state.UpdateOverlayManifest(workspaceID, manifest)
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
