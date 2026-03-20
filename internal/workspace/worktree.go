package workspace

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"

	"github.com/sergeknystautas/schmux/internal/state"
)

func (m *Manager) branchSourceRef(ctx context.Context, worktreeBasePath, branch string) (string, error) {
	remoteRef := "refs/remotes/origin/" + branch
	if m.runGitErr(ctx, "", RefreshTriggerExplicit, worktreeBasePath, "show-ref", "--verify", "--quiet", remoteRef) == nil {
		return "origin/" + branch, nil
	}

	localRef := "refs/heads/" + branch
	if m.runGitErr(ctx, "", RefreshTriggerExplicit, worktreeBasePath, "show-ref", "--verify", "--quiet", localRef) == nil {
		return branch, nil
	}

	return "", fmt.Errorf("branch %s not found in worktree base", branch)
}

func (m *Manager) localBranchExists(ctx context.Context, worktreeBasePath, branch string) bool {
	localRef := "refs/heads/" + branch
	return m.runGitErr(ctx, "", RefreshTriggerExplicit, worktreeBasePath, "show-ref", "--verify", "--quiet", localRef) == nil
}

func (m *Manager) createBranchFromRef(ctx context.Context, worktreeBasePath, branch, sourceRef string) error {
	if _, err := m.runGit(ctx, "", RefreshTriggerExplicit, worktreeBasePath, "branch", branch, sourceRef); err != nil {
		return fmt.Errorf("git branch %s %s failed: %w", branch, sourceRef, err)
	}
	return nil
}

func (m *Manager) deleteBranch(ctx context.Context, worktreeBasePath, branch string) error {
	if _, err := m.runGit(ctx, "", RefreshTriggerExplicit, worktreeBasePath, "branch", "-D", branch); err != nil {
		return fmt.Errorf("git branch -D %s failed: %w", branch, err)
	}
	return nil
}

func defaultRandSuffix(length int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

func (m *Manager) isBranchInWorktree(ctx context.Context, worktreeBasePath, branch string) bool {
	return m.isBranchInWorktreeWithCache(ctx, worktreeBasePath, branch, nil)
}

func (m *Manager) isBranchInWorktreeWithCache(ctx context.Context, worktreeBasePath, branch string, cache *worktreeListCache) bool {
	var output []byte
	var err error

	if cache != nil {
		key := filepath.Clean(worktreeBasePath)
		output, err = cache.Get(ctx, key, func() ([]byte, error) {
			return m.runGit(ctx, "", RefreshTriggerExplicit, worktreeBasePath, "worktree", "list", "--porcelain")
		})
	} else {
		output, err = m.runGit(ctx, "", RefreshTriggerExplicit, worktreeBasePath, "worktree", "list", "--porcelain")
	}

	if err != nil {
		return false
	}

	searchStr := "branch refs/heads/" + branch
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if line == searchStr {
			return true
		}
	}
	return false
}

func (m *Manager) initLocalRepo(ctx context.Context, path, branch string) error {
	m.logger.Info("initializing local repository", "path", path, "branch", branch)

	if err := os.MkdirAll(path, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	if _, err := m.runGit(ctx, "", RefreshTriggerExplicit, path, "init"); err != nil {
		return fmt.Errorf("git init failed: %w", err)
	}

	if _, err := m.runGit(ctx, "", RefreshTriggerExplicit, path, "config", "user.email", "schmux@localhost"); err != nil {
		return fmt.Errorf("git config user.email failed: %w", err)
	}

	if _, err := m.runGit(ctx, "", RefreshTriggerExplicit, path, "config", "user.name", "schmux"); err != nil {
		return fmt.Errorf("git config user.name failed: %w", err)
	}

	// Create and checkout the branch
	if _, err := m.runGit(ctx, "", RefreshTriggerExplicit, path, "checkout", "-b", branch); err != nil {
		return fmt.Errorf("git checkout -b %s failed: %w", branch, err)
	}

	if _, err := m.runGit(ctx, "", RefreshTriggerExplicit, path, "commit", "--allow-empty", "-m", "Initial commit"); err != nil {
		return fmt.Errorf("git commit failed: %w", err)
	}

	m.logger.Info("local repository initialized", "path", path)
	return nil
}

func (m *Manager) cloneRepo(ctx context.Context, url, path string) error {
	m.logger.Info("cloning repository", "url", url, "path", path)

	if _, err := m.runGit(ctx, "", RefreshTriggerExplicit, "", "clone", url, path); err != nil {
		return fmt.Errorf("git clone failed: %w", err)
	}

	m.logger.Info("repository cloned", "path", path)
	return nil
}

func (m *Manager) cleanupLocalBranch(ctx context.Context, worktreeBasePath string, w state.Workspace) {
	defaultBranch, err := m.GetDefaultBranch(ctx, w.Repo)
	if err == nil && w.Branch == defaultBranch {
		return
	}

	if m.isBranchInWorktree(ctx, worktreeBasePath, w.Branch) {
		return
	}

	remoteBranchExists, err := m.gitRemoteBranchExists(ctx, worktreeBasePath, w.Branch)
	if err != nil {
		m.logger.Warn("could not check remote branch", "err", err)
		return
	}
	if remoteBranchExists {
		return
	}

	if err := m.deleteBranch(ctx, worktreeBasePath, w.Branch); err != nil {
		m.logger.Warn("failed to delete local branch", "branch", w.Branch, "err", err)
	} else {
		m.logger.Info("deleted local branch", "branch", w.Branch)
	}
}

func (m *Manager) findWorktreeBaseForWorkspace(w state.Workspace) (string, error) {
	if isWorktree(w.Path) {
		if worktreeBase, err := resolveWorktreeBaseFromWorktree(w.Path); err == nil {
			return worktreeBase, nil
		}
	}

	if wb, found := m.state.GetRepoBaseByURL(w.Repo); found {
		return wb.Path, nil
	}

	return "", fmt.Errorf("could not find worktree base for workspace: %s", w.ID)
}
