package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RelocateBareRepo moves a bare git repository from oldPath to newPath and
// fixes up all worktree .git file references so they point to the new location.
//
// It does NOT touch config or state — the caller handles that.
//
// Steps:
//  1. Verify the target path does not already exist
//  2. Create parent directories for the target
//  3. Rename the bare repo directory
//  4. Fix worktree .git files to reference the new path
func RelocateBareRepo(oldPath, newPath string) error {
	// 1. Check target doesn't already exist
	if _, err := os.Stat(newPath); err == nil {
		return fmt.Errorf("target path already exists: %s", newPath)
	}

	// Resolve symlinks on the old path while it still exists, so that the
	// string replacement matches what git wrote into worktree .git files.
	// Git resolves symlinks when writing absolute paths, so we must do the same.
	resolvedOld, err := filepath.EvalSymlinks(oldPath)
	if err != nil {
		return fmt.Errorf("failed to resolve old path %s: %w", oldPath, err)
	}

	// 2. Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(newPath), 0755); err != nil {
		return fmt.Errorf("failed to create parent directory for %s: %w", newPath, err)
	}

	// Resolve symlinks on the new path's parent (the new path itself doesn't exist yet).
	resolvedNewParent, err := filepath.EvalSymlinks(filepath.Dir(newPath))
	if err != nil {
		return fmt.Errorf("failed to resolve new path parent %s: %w", filepath.Dir(newPath), err)
	}
	resolvedNew := filepath.Join(resolvedNewParent, filepath.Base(newPath))

	// 3. Rename (use original paths — the OS handles symlinks transparently)
	if err := os.Rename(oldPath, newPath); err != nil {
		return fmt.Errorf("failed to rename %s to %s: %w", oldPath, newPath, err)
	}

	// 4. Fix worktree .git files
	// The bare repo's worktrees/ directory contains one subdirectory per worktree.
	// Each subdirectory has a "gitdir" file containing the absolute path to the
	// worktree's .git file.
	if err := fixupWorktreeGitFiles(newPath, resolvedOld, resolvedNew); err != nil {
		// Attempt to roll back the rename so the caller finds the repo at oldPath.
		_ = os.Rename(newPath, oldPath)
		return err
	}

	return nil
}

// fixupWorktreeGitFiles updates worktree .git file references from resolvedOld
// to resolvedNew inside the bare repo at repoPath.
func fixupWorktreeGitFiles(repoPath, resolvedOld, resolvedNew string) error {
	worktreesDir := filepath.Join(repoPath, "worktrees")
	entries, err := os.ReadDir(worktreesDir)
	if err != nil {
		if os.IsNotExist(err) {
			// No worktrees — nothing to fix
			return nil
		}
		return fmt.Errorf("failed to read worktrees directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Read the gitdir file to find the worktree's .git file path
		gitdirFile := filepath.Join(worktreesDir, entry.Name(), "gitdir")
		gitdirContent, err := os.ReadFile(gitdirFile)
		if err != nil {
			return fmt.Errorf("failed to read gitdir file %s: %w", gitdirFile, err)
		}

		worktreeGitFile := strings.TrimSpace(string(gitdirContent))

		// Read the worktree's .git file
		dotGitContent, err := os.ReadFile(worktreeGitFile)
		if err != nil {
			return fmt.Errorf("failed to read worktree .git file %s: %w", worktreeGitFile, err)
		}

		// Replace old path with new path in the gitdir: line.
		// Use resolved paths since git writes symlink-resolved absolute paths.
		updated := strings.ReplaceAll(string(dotGitContent), resolvedOld, resolvedNew)
		if updated == string(dotGitContent) {
			return fmt.Errorf("worktree .git file %s does not reference old path %s: unexpected worktree state", worktreeGitFile, resolvedOld)
		}

		if err := os.WriteFile(worktreeGitFile, []byte(updated), 0644); err != nil {
			return fmt.Errorf("failed to write updated .git file %s: %w", worktreeGitFile, err)
		}
	}

	return nil
}
