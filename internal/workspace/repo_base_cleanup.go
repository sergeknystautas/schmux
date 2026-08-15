package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CleanupUnusedRepoBases deletes local git bases that are no longer referenced
// by config or workspace state. Bases with linked worktrees are retained even
// if state is incomplete, and paths outside schmux's repo directory are never
// deleted.
func (m *Manager) CleanupUnusedRepoBases(ctx context.Context) error {
	inUse := make(map[string]bool)
	for _, repo := range m.config.GetRepos() {
		inUse[repo.URL] = true
	}
	for _, ws := range m.state.GetWorkspaces() {
		inUse[ws.Repo] = true
	}

	var cleanupErr error
	stateChanged := false
	for _, base := range m.state.GetRepoBases() {
		if inUse[base.RepoURL] {
			continue
		}

		// Sapling base paths may refer to user-managed repositories outside
		// schmux's storage. Drop the stale state entry, but never delete them.
		if base.VCS != "sapling" && base.Path != "" {
			if !pathWithinDir(m.config.GetWorktreeBasePath(), base.Path) {
				cleanupErr = errors.Join(cleanupErr, fmt.Errorf("refusing to remove repo base outside managed directory: %s", base.Path))
				continue
			}
			if _, err := os.Stat(base.Path); err == nil {
				out, err := m.runGit(ctx, "", RefreshTriggerExplicit, base.Path, "worktree", "list", "--porcelain")
				if err != nil {
					cleanupErr = errors.Join(cleanupErr, fmt.Errorf("list worktrees for %s: %w", base.Path, err))
					continue
				}
				if linkedWorktreeCount(out) > 0 {
					continue
				}
				if err := os.RemoveAll(base.Path); err != nil {
					cleanupErr = errors.Join(cleanupErr, fmt.Errorf("remove unused repo base %s: %w", base.Path, err))
					continue
				}
				m.logger.Info("removed unused repo base", "url", base.RepoURL, "path", base.Path)
			} else if !os.IsNotExist(err) {
				cleanupErr = errors.Join(cleanupErr, fmt.Errorf("stat repo base %s: %w", base.Path, err))
				continue
			}
		}

		if err := m.state.RemoveRepoBase(base.RepoURL); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("remove repo base state for %s: %w", base.RepoURL, err))
			continue
		}
		stateChanged = true
	}

	if stateChanged {
		if err := m.state.Save(); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("save repo base cleanup: %w", err))
		}
	}
	return cleanupErr
}

func pathWithinDir(dir, path string) bool {
	dirAbs, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(dirAbs, pathAbs)
	if err != nil || rel == "." {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func linkedWorktreeCount(output []byte) int {
	count := 0
	for _, line := range strings.Split(string(output), "\n") {
		if strings.HasPrefix(line, "worktree ") {
			count++
		}
	}
	// A bare repository lists itself as the first worktree entry.
	if count > 0 {
		count--
	}
	return count
}
