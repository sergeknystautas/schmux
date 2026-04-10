package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/sergeknystautas/schmux/internal/difftool"
	"github.com/sergeknystautas/schmux/internal/state"
)

type GitBackend struct {
	manager *Manager
}

func NewGitBackend(m *Manager) *GitBackend {
	return &GitBackend{manager: m}
}

var _ VCSBackend = (*GitBackend)(nil)

func (g *GitBackend) cloneBareRepo(ctx context.Context, url, path string) error {
	g.manager.logger.Info("cloning bare repository", "url", url, "path", path)

	if _, err := g.manager.runGit(ctx, "", RefreshTriggerExplicit, "", "clone", "--bare", url, path); err != nil {
		return fmt.Errorf("git clone --bare failed: %w", err)
	}

	if _, err := g.manager.runGit(ctx, "", RefreshTriggerExplicit, path, "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*"); err != nil {
		return fmt.Errorf("git config fetch refspec failed: %w", err)
	}

	g.manager.logger.Info("bare repository cloned", "path", path)
	return nil
}

func (g *GitBackend) EnsureRepoBase(ctx context.Context, repoIdentifier, basePath string) (string, error) {
	if wb, found := g.manager.state.GetRepoBaseByURL(repoIdentifier); found {
		if _, err := os.Stat(wb.Path); err == nil {
			g.manager.logger.Debug("using existing worktree base", "url", repoIdentifier, "path", wb.Path)
			return wb.Path, nil
		}
		g.manager.logger.Warn("worktree base missing on disk, will recreate", "url", repoIdentifier)
	}

	repo, found := g.manager.config.FindRepoByURL(repoIdentifier)
	if !found {
		return "", fmt.Errorf("repo not found in config: %s", repoIdentifier)
	}
	if repo.BarePath == "" {
		return "", fmt.Errorf("repo %s has no bare_path set", repo.Name)
	}

	worktreeBasePath := filepath.Join(g.manager.config.GetWorktreeBasePath(), repo.BarePath)

	if err := os.MkdirAll(filepath.Dir(worktreeBasePath), 0755); err != nil {
		return "", fmt.Errorf("failed to create repo directory: %w", err)
	}

	if err := g.cloneBareRepo(ctx, repoIdentifier, worktreeBasePath); err != nil {
		if _, statErr := os.Stat(worktreeBasePath); statErr == nil {
			g.manager.logger.Info("worktree base created by concurrent request, using existing", "path", worktreeBasePath)
		} else {
			return "", err
		}
	}

	if err := g.manager.state.AddRepoBase(state.RepoBase{RepoURL: repoIdentifier, Path: worktreeBasePath}); err != nil {
		return "", fmt.Errorf("failed to add worktree base to state: %w", err)
	}
	if err := g.manager.state.Save(); err != nil {
		return "", fmt.Errorf("failed to save state: %w", err)
	}

	return worktreeBasePath, nil
}

func (g *GitBackend) CreateWorkspace(ctx context.Context, repoBasePath, branch, destPath string) error {
	g.manager.logger.Info("adding worktree", "base", repoBasePath, "path", destPath, "branch", branch)

	localBranchExists := g.manager.runGitErr(ctx, "", RefreshTriggerExplicit, repoBasePath, "show-ref", "--verify", "--quiet", "refs/heads/"+branch) == nil

	remoteBranch := "origin/" + branch
	remoteBranchExists := g.manager.runGitErr(ctx, "", RefreshTriggerExplicit, repoBasePath, "show-ref", "--verify", "--quiet", "refs/remotes/"+remoteBranch) == nil

	if localBranchExists && remoteBranchExists && !g.manager.isBranchInWorktree(ctx, repoBasePath, branch) {
		localRef := "refs/heads/" + branch
		remoteRef := "refs/remotes/origin/" + branch
		if g.manager.runGitErr(ctx, "", RefreshTriggerExplicit, repoBasePath, "merge-base", "--is-ancestor", localRef, remoteRef) != nil {
			g.manager.logger.Info("local branch diverged from origin, resetting", "branch", branch)
			if _, err := g.manager.runGit(ctx, "", RefreshTriggerExplicit, repoBasePath, "update-ref", localRef, remoteRef); err != nil {
				return fmt.Errorf("failed to reset diverged local %s: %w", branch, err)
			}
		}
	}

	var args []string
	if localBranchExists {
		args = []string{"worktree", "add", destPath, branch}
	} else if remoteBranchExists {
		args = []string{"worktree", "add", "--track", "-b", branch, destPath, remoteBranch}
	} else {
		defaultBranch := g.manager.getDefaultBranch(ctx, repoBasePath)
		if defaultBranch == "" {
			return fmt.Errorf("failed to detect default branch from bare clone")
		}
		args = []string{"worktree", "add", "-b", branch, destPath, "origin/" + defaultBranch}
	}

	if _, err := g.manager.runGit(ctx, "", RefreshTriggerExplicit, repoBasePath, args...); err != nil {
		return fmt.Errorf("git worktree add failed: %w", err)
	}

	g.manager.logger.Info("worktree added", "path", destPath)
	return nil
}

func (g *GitBackend) RemoveWorkspace(ctx context.Context, workspacePath string) error {
	g.manager.logger.Info("removing worktree", "path", workspacePath)

	// Resolve the worktree base BEFORE deleting the directory, because
	// resolution reads the .git file inside the worktree.
	worktreeBase, resolveErr := resolveWorktreeBaseFromWorktree(workspacePath)

	// Use os.RemoveAll instead of `git worktree remove --force`.
	// git worktree remove deletes files one-by-one and can exceed context
	// deadlines on large repos, leaving a half-deleted directory that
	// blocks all subsequent dispose attempts.
	if err := os.RemoveAll(workspacePath); err != nil {
		return fmt.Errorf("failed to remove workspace directory: %w", err)
	}

	// Clean up git's worktree bookkeeping (.git/worktrees/<name>).
	// Non-fatal: the directory is already gone, prune is just metadata.
	if resolveErr == nil {
		if _, err := g.manager.runGit(ctx, "", RefreshTriggerExplicit, worktreeBase, "worktree", "prune"); err != nil {
			g.manager.logger.Warn("worktree prune failed (non-fatal)", "path", workspacePath, "err", err)
		}
	} else {
		g.manager.logger.Warn("could not resolve worktree base for prune", "path", workspacePath, "err", resolveErr)
	}

	g.manager.logger.Info("worktree removed", "path", workspacePath)
	return nil
}

func (g *GitBackend) PruneStale(ctx context.Context, repoBasePath string) error {
	g.manager.logger.Debug("pruning stale worktrees", "base", repoBasePath)

	if _, err := g.manager.runGit(ctx, "", RefreshTriggerExplicit, repoBasePath, "worktree", "prune"); err != nil {
		return fmt.Errorf("git worktree prune failed: %w", err)
	}

	g.manager.logger.Debug("worktrees pruned", "base", repoBasePath)
	return nil
}

func (g *GitBackend) IsBranchInUse(ctx context.Context, repoBasePath, branch string) (bool, error) {
	return g.manager.isBranchInWorktree(ctx, repoBasePath, branch), nil
}

func (g *GitBackend) ensureUniqueBranch(ctx context.Context, worktreeBasePath, branch string) (string, bool, error) {
	if !g.manager.isBranchInWorktree(ctx, worktreeBasePath, branch) {
		return branch, false, nil
	}

	sourceRef, err := g.manager.branchSourceRef(ctx, worktreeBasePath, branch)
	if err != nil {
		return "", false, err
	}

	for i := 0; i < 10; i++ {
		suffix := g.manager.randSuffix(3)
		candidate := fmt.Sprintf("%s-%s", branch, suffix)
		if g.manager.isBranchInWorktree(ctx, worktreeBasePath, candidate) {
			continue
		}
		if g.manager.localBranchExists(ctx, worktreeBasePath, candidate) {
			continue
		}
		if err := g.manager.createBranchFromRef(ctx, worktreeBasePath, candidate, sourceRef); err != nil {
			return "", false, err
		}
		return candidate, true, nil
	}

	return "", false, fmt.Errorf("could not find a unique branch name for %s", branch)
}

func (g *GitBackend) GetStatus(ctx context.Context, workspacePath string) (VCSStatus, error) {
	var status VCSStatus

	output, err := g.manager.runGit(ctx, "", RefreshTriggerExplicit, workspacePath, "status", "--porcelain")
	trimmedOutput := strings.TrimSpace(string(output))
	status.Dirty = err == nil && len(trimmedOutput) > 0
	if err == nil && trimmedOutput != "" {
		status.FilesChanged = len(strings.Split(trimmedOutput, "\n"))
	}

	status.CurrentBranch, _ = g.manager.gitCurrentBranch(ctx, workspacePath)

	defaultBranch := g.detectDefaultBranch(ctx, workspacePath)
	if defaultBranch != "" {
		output, err = g.manager.runGit(ctx, "", RefreshTriggerExplicit, workspacePath, "rev-list", "--left-right", "--count", "HEAD...origin/"+defaultBranch)
		if err == nil {
			parts := strings.Split(strings.TrimSpace(string(output)), "\t")
			if len(parts) == 2 {
				status.AheadOfDefault, _ = strconv.Atoi(parts[0])
				status.BehindDefault, _ = strconv.Atoi(parts[1])
			}
		}

		status.DefaultBranchOrphaned = !g.manager.hasCommonAncestor(ctx, workspacePath, "origin/"+defaultBranch)
	}

	if status.CurrentBranch != "" && status.CurrentBranch != "HEAD" {
		remoteBranchExists, _ := g.manager.gitRemoteBranchExists(ctx, workspacePath, status.CurrentBranch)
		status.RemoteBranchExists = remoteBranchExists
		if remoteBranchExists {
			remoteRef := "origin/" + status.CurrentBranch
			revOutput, revErr := g.manager.runGit(ctx, "", RefreshTriggerExplicit, workspacePath, "rev-list", "--left-right", "--count", "HEAD..."+remoteRef)
			if revErr == nil {
				parts := strings.Split(strings.TrimSpace(string(revOutput)), "\t")
				if len(parts) == 2 {
					status.LocalUniqueCommits, _ = strconv.Atoi(parts[0])
					status.RemoteUniqueCommits, _ = strconv.Atoi(parts[1])
				}
			}
			status.SyncedWithRemote = (status.LocalUniqueCommits == 0 && status.RemoteUniqueCommits == 0)
		}
	}

	output, err = g.manager.runGit(ctx, "", RefreshTriggerExplicit, workspacePath, "diff", "--numstat", "HEAD")
	if err == nil {
		trimmed := strings.TrimSpace(string(output))
		if trimmed != "" {
			lines := strings.Split(trimmed, "\n")
			for _, line := range lines {
				parts := strings.Split(line, "\t")
				if len(parts) >= 3 {
					if a, aErr := strconv.Atoi(parts[0]); aErr == nil {
						status.LinesAdded += a
					}
					if r, rErr := strconv.Atoi(parts[1]); rErr == nil && parts[1] != "-" {
						status.LinesRemoved += r
					}
				}
			}
		}
	}

	untrackedOutput, err := g.manager.runGit(ctx, "", RefreshTriggerExplicit, workspacePath, "ls-files", "--others", "--exclude-standard")
	if err == nil {
		untrackedLines := strings.Split(string(untrackedOutput), "\n")
		for _, filePath := range untrackedLines {
			if filePath == "" {
				continue
			}
			fullPath := filepath.Join(workspacePath, filePath)
			var lineCount int
			if difftool.IsBinaryFile(ctx, workspacePath, filePath) {
				lineCount = 0
			} else {
				lc, lcErr := countLinesCapped(fullPath, 1024*1024)
				if lcErr != nil {
					lineCount = 0
				} else {
					lineCount = lc
				}
			}
			status.LinesAdded += lineCount
		}
	}

	return status, nil
}

func (g *GitBackend) detectDefaultBranch(ctx context.Context, workspacePath string) string {
	dir := workspacePath
	if isWorktree(dir) {
		if bareRepoPath, err := resolveWorktreeBaseFromWorktree(dir); err == nil {
			dir = bareRepoPath
		}
	}
	return g.manager.getDefaultBranch(ctx, dir)
}

func (g *GitBackend) GetChangedFiles(ctx context.Context, workspacePath string) ([]VCSChangedFile, error) {
	gitFiles, err := g.manager.GetDirtyFiles(ctx, workspacePath)
	if err != nil {
		return nil, err
	}
	result := make([]VCSChangedFile, len(gitFiles))
	for i, gf := range gitFiles {
		result[i] = VCSChangedFile(gf)
	}
	return result, nil
}

func (g *GitBackend) GetDefaultBranch(ctx context.Context, repoBasePath string) (string, error) {
	branch := g.manager.getDefaultBranch(ctx, repoBasePath)
	if branch == "" {
		return "", fmt.Errorf("failed to detect default branch")
	}
	return branch, nil
}

func (g *GitBackend) GetCurrentBranch(ctx context.Context, workspacePath string) (string, error) {
	return g.manager.gitCurrentBranch(ctx, workspacePath)
}

func (g *GitBackend) Fetch(ctx context.Context, path string) error {
	return g.manager.gitFetch(ctx, path)
}

func (g *GitBackend) EnsureQueryRepo(ctx context.Context, repoIdentifier, path string) error {
	_, err := g.manager.ensureOriginQueryRepo(ctx, repoIdentifier)
	return err
}

func (g *GitBackend) FetchQueryRepo(ctx context.Context, path string) error {
	return g.manager.gitFetch(ctx, path)
}

func (g *GitBackend) ListRecentBranches(ctx context.Context, path string, limit int) ([]RecentBranch, error) {
	return nil, nil
}

func (g *GitBackend) GetBranchLog(ctx context.Context, path, branch string, limit int) ([]string, error) {
	return nil, nil
}
