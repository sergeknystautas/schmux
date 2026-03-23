package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/sergeknystautas/schmux/internal/difftool"
)

// runGitErr is a convenience wrapper around runGit that discards stdout.
// Used for commands where only the exit code matters (e.g., show-ref --verify --quiet).
func (m *Manager) runGitErr(ctx context.Context, workspaceID string, trigger RefreshTrigger, dir string, args ...string) error {
	_, err := m.runGit(ctx, workspaceID, trigger, dir, args...)
	return err
}

var branchNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_]+(?:[._/-][a-zA-Z0-9_]+)*$`)

// ErrInvalidBranchName is returned when a branch name fails validation.
var ErrInvalidBranchName = errors.New("invalid branch name")

// GitChangedFile represents a changed file with its status and line changes.
type GitChangedFile struct {
	Path         string
	Status       string // "added", "modified", "deleted", "untracked"
	LinesAdded   int
	LinesRemoved int
}

// ValidateBranchName checks whether a branch name is acceptable for use.
// Returns nil if valid, or an error describing the problem.
func ValidateBranchName(branch string) error {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return fmt.Errorf("%w: branch name cannot be empty", ErrInvalidBranchName)
	}
	if !branchNamePattern.MatchString(branch) {
		return fmt.Errorf("%w: %q does not match required format (alphanumeric, underscores, hyphens, forward slashes, or periods)", ErrInvalidBranchName, branch)
	}
	// Check for consecutive separators (-, ., /, _)
	for i := 0; i < len(branch)-1; i++ {
		if branch[i] == branch[i+1] && (branch[i] == '-' || branch[i] == '.' || branch[i] == '/' || branch[i] == '_') {
			return fmt.Errorf("%w: %q has consecutive characters", ErrInvalidBranchName, branch)
		}
	}
	return nil
}

// isWorktree checks if a path is a worktree (has .git file) vs full clone (.git dir).
func isWorktree(path string) bool {
	gitPath := filepath.Join(path, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		return false
	}
	return !info.IsDir() // File = worktree, Dir = full clone
}

// resolveWorktreeBaseFromWorktree reads the .git file to find the worktree base path.
func resolveWorktreeBaseFromWorktree(worktreePath string) (string, error) {
	gitFilePath := filepath.Join(worktreePath, ".git")
	content, err := os.ReadFile(gitFilePath)
	if err != nil {
		return "", fmt.Errorf("failed to read .git file: %w", err)
	}

	// Format: "gitdir: /path/to/base.git/worktrees/workspace-name"
	line := strings.TrimSpace(string(content))
	if !strings.HasPrefix(line, "gitdir: ") {
		return "", fmt.Errorf("invalid .git file format")
	}

	gitdir := strings.TrimPrefix(line, "gitdir: ")

	// Strip "/worktrees/xxx" to get worktree base path
	if idx := strings.Index(gitdir, "/worktrees/"); idx >= 0 {
		return gitdir[:idx], nil
	}

	return "", fmt.Errorf("could not parse worktree base from gitdir: %s", gitdir)
}

// gitFetch runs git fetch. For worktrees, fetches from the worktree base.
func (m *Manager) gitFetch(ctx context.Context, dir string) error {
	return m.gitFetchInstrumented(ctx, "", RefreshTriggerExplicit, dir)
}

// gitFetchInstrumented runs git fetch with telemetry recording.
func (m *Manager) gitFetchInstrumented(ctx context.Context, workspaceID string, trigger RefreshTrigger, dir string) error {
	return m.gitFetchInstrumentedWithRound(ctx, workspaceID, trigger, dir, nil)
}

// gitFetchInstrumentedWithRound runs git fetch with telemetry recording and optional
// per-poll-round deduplication keyed by the effective fetch dir.
func (m *Manager) gitFetchInstrumentedWithRound(ctx context.Context, workspaceID string, trigger RefreshTrigger, dir string, round *gitFetchPollRound) error {
	fetchDir := resolveGitFetchDir(dir)
	fetchKey := ""
	if fetchDir != "" {
		fetchKey = filepath.Clean(fetchDir)
	}

	runFetch := func(fetchCtx context.Context) error {
		if _, err := m.runGit(fetchCtx, workspaceID, trigger, fetchDir, "fetch"); err != nil {
			return fmt.Errorf("git fetch failed: %w", err)
		}
		return nil
	}

	if round != nil {
		return round.Do(ctx, fetchKey, runFetch)
	}

	return runFetch(ctx)
}

func resolveGitFetchDir(dir string) string {
	if dir == "" {
		return dir
	}
	// Resolve to worktree base if this is a worktree
	fetchDir := dir
	if isWorktree(dir) {
		if worktreeBase, err := resolveWorktreeBaseFromWorktree(dir); err == nil {
			fetchDir = worktreeBase
		}
	}

	return fetchDir
}

// updateLocalDefaultBranch fast-forwards the local default branch in a bare clone
// to match origin/<default>. This keeps refs/heads/main in sync with origin/main
// so that new worktrees created from the local branch get the latest commits.
// Only updates when:
//   - The branch is not checked out in any worktree (safe to update ref)
//   - The update is a fast-forward (origin is ahead of local, not diverged)
func (m *Manager) updateLocalDefaultBranch(ctx context.Context, workspaceID string, trigger RefreshTrigger, bareRepoPath, repoURL string, wtCache *worktreeListCache) {
	defaultBranch, err := m.GetDefaultBranch(ctx, repoURL)
	if err != nil || defaultBranch == "" {
		return
	}

	localRef := "refs/heads/" + defaultBranch
	remoteRef := "refs/remotes/origin/" + defaultBranch

	// Resolve both refs in a single git command. If either doesn't exist,
	// rev-parse fails and we bail out (nothing to update).
	output, err := m.runGit(ctx, workspaceID, trigger, bareRepoPath, "rev-parse", localRef, remoteRef)
	if err != nil {
		return // one or both refs don't exist
	}
	shas := strings.SplitN(strings.TrimSpace(string(output)), "\n", 2)
	if len(shas) == 2 && shas[0] == shas[1] {
		return // already up to date — nothing to do
	}

	// Refs differ — check if branch is checked out before updating
	if m.isBranchInWorktreeWithCache(ctx, bareRepoPath, defaultBranch, wtCache) {
		return
	}

	// Verify this would be a fast-forward: local must be an ancestor of origin
	if m.runGitErr(ctx, workspaceID, trigger, bareRepoPath, "merge-base", "--is-ancestor", localRef, remoteRef) != nil {
		return // not a fast-forward (diverged or local is ahead)
	}

	// Fast-forward the local ref to match origin
	if _, err := m.runGit(ctx, workspaceID, trigger, bareRepoPath, "update-ref", localRef, remoteRef); err != nil {
		m.logger.Warn("failed to fast-forward local branch", "branch", defaultBranch, "err", err)
	}
}

// gitCheckoutBranch runs git checkout -B, optionally resetting to origin/<branch>.
func (m *Manager) gitCheckoutBranch(ctx context.Context, dir, branch string, remoteBranchExists bool) error {
	args := []string{"checkout", "-B", branch}
	if remoteBranchExists {
		args = append(args, "origin/"+branch)
	}

	if _, err := m.runGit(ctx, "", RefreshTriggerExplicit, dir, args...); err != nil {
		return fmt.Errorf("git checkout failed: %w", err)
	}

	return nil
}

// isUpToDateWithDefault checks whether HEAD in the given directory is an ancestor
// of (or equal to) origin/<defaultBranch>. Returns true when the workspace has no
// commits that have diverged from the default branch, meaning it's safe to reuse
// for a different branch without polluting commit history.
func (m *Manager) isUpToDateWithDefault(ctx context.Context, dir, repoURL string) bool {
	defaultBranch, err := m.GetDefaultBranch(ctx, repoURL)
	if err != nil {
		m.logger.Debug("cannot determine default branch, skipping reuse", "err", err)
		return false
	}

	return m.runGitErr(ctx, "", RefreshTriggerExplicit, dir, "merge-base", "--is-ancestor", "HEAD", "origin/"+defaultBranch) == nil
}

// gitPullRebase runs git pull --rebase origin <branch>.
// For cloned repos with an origin remote, this avoids relying on potentially incorrect
// upstream config. For local repos without origin, skips the pull.
func (m *Manager) gitPullRebase(ctx context.Context, dir, branch string) error {
	// Check if origin remote exists
	if _, err := m.runGit(ctx, "", RefreshTriggerExplicit, dir, "remote", "get-url", "origin"); err != nil {
		// No origin remote - local-only repo, nothing to pull
		m.logger.Debug("no origin remote, skipping pull")
		return nil
	}

	// Explicitly pull from origin/<branch> to avoid broken upstream config
	if _, err := m.runGit(ctx, "", RefreshTriggerExplicit, dir, "pull", "--rebase", "origin", branch); err != nil {
		return fmt.Errorf("git pull failed: %w", err)
	}

	return nil
}

// gitHasOriginRemote checks if the repo has an origin remote configured.
func (m *Manager) gitHasOriginRemote(ctx context.Context, dir string) bool {
	return m.runGitErr(ctx, "", RefreshTriggerExplicit, dir, "remote", "get-url", "origin") == nil
}

// gitRemoteBranchExists checks for refs/remotes/origin/<branch>.
func (m *Manager) gitRemoteBranchExists(ctx context.Context, dir, branch string) (bool, error) {
	return m.gitRemoteBranchExistsInstrumented(ctx, "", RefreshTriggerExplicit, dir, branch)
}

// gitRemoteBranchExistsInstrumented checks for refs/remotes/origin/<branch> with
// telemetry attribution for the caller's trigger/workspace.
func (m *Manager) gitRemoteBranchExistsInstrumented(ctx context.Context, workspaceID string, trigger RefreshTrigger, dir, branch string) (bool, error) {
	ref := "refs/remotes/origin/" + branch

	if err := m.runGitErr(ctx, workspaceID, trigger, dir, "show-ref", "--verify", "--quiet", ref); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, fmt.Errorf("git show-ref failed: %w", err)
	}

	return true, nil
}

// gitCheckoutDot runs git checkout -- .
func (m *Manager) gitCheckoutDot(ctx context.Context, dir string) error {
	if _, err := m.runGit(ctx, "", RefreshTriggerExplicit, dir, "checkout", "--", "."); err != nil {
		return fmt.Errorf("git checkout -- . failed: %w", err)
	}

	return nil
}

// gitCurrentBranch returns the current branch name for a directory.
func (m *Manager) gitCurrentBranch(ctx context.Context, dir string) (string, error) {
	return m.gitCurrentBranchInstrumented(ctx, "", RefreshTriggerExplicit, dir)
}

// gitCurrentBranchInstrumented returns the current branch name with telemetry recording.
func (m *Manager) gitCurrentBranchInstrumented(ctx context.Context, workspaceID string, trigger RefreshTrigger, dir string) (string, error) {
	output, err := m.runGit(ctx, workspaceID, trigger, dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", fmt.Errorf("git rev-parse failed: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}

// gitClean runs git clean -fd.
func (m *Manager) gitClean(ctx context.Context, dir string) error {
	if _, err := m.runGit(ctx, "", RefreshTriggerExplicit, dir, "clean", "-fd"); err != nil {
		return fmt.Errorf("git clean failed: %w", err)
	}

	return nil
}

// GetWorkspaceGitFiles returns the list of changed files for a workspace.
// Implements the WorkspaceManager interface.
func (m *Manager) GetWorkspaceChangedFiles(ctx context.Context, workspaceID string) ([]GitChangedFile, error) {
	ws, found := m.state.GetWorkspace(workspaceID)
	if !found {
		return nil, fmt.Errorf("workspace not found: %s", workspaceID)
	}
	return m.GetDirtyFiles(ctx, ws.Path)
}

// GetDirtyFiles returns the list of changed files in a workspace directory.
// Uses the same logic as the diff endpoint for consistency.
func (m *Manager) GetDirtyFiles(ctx context.Context, dir string) ([]GitChangedFile, error) {
	files := []GitChangedFile{}

	// Get changed files using git diff HEAD --numstat (same as diff endpoint)
	// --numstat shows: added/deleted lines filename
	// HEAD compares against last commit (includes both staged and unstaged)
	// --find-renames finds renames
	output, err := m.runGit(ctx, "", RefreshTriggerExplicit, dir, "diff", "HEAD", "--numstat", "--find-renames", "--diff-filter=ADM")
	if err != nil {
		// No changes is not an error
		output = []byte{}
	}

	// Parse numstat output
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 3 {
			continue
		}

		addedStr := parts[0]
		filePath := parts[2]

		// Determine status
		status := "modified"
		if addedStr == "-" {
			// File was deleted
			status = "deleted"
		} else {
			// Check if file exists in HEAD to determine if it's added or modified
			if m.runGitErr(ctx, "", RefreshTriggerExplicit, dir, "cat-file", "-e", "HEAD:"+filePath) != nil {
				// File doesn't exist in HEAD, so it's new
				status = "added"
			}
		}

		files = append(files, GitChangedFile{
			Path:   filePath,
			Status: status,
		})
	}

	// Get untracked files (same as diff endpoint)
	// ls-files --others --exclude-standard lists untracked files (respecting .gitignore)
	untrackedOutput, err := m.runGit(ctx, "", RefreshTriggerExplicit, dir, "ls-files", "--others", "--exclude-standard")
	if err == nil {
		untrackedLines := strings.Split(string(untrackedOutput), "\n")
		for _, filePath := range untrackedLines {
			if filePath == "" {
				continue
			}
			files = append(files, GitChangedFile{
				Path:   filePath,
				Status: "untracked",
			})
		}
	}

	return files, nil
}

// hasCommonAncestor checks whether HEAD and the given ref share any common ancestor.
// Returns true if `git merge-base HEAD <ref>` succeeds (i.e., the histories are related).
// Returns false if there is no common ancestor (e.g., orphaned/force-pushed branch).
func (m *Manager) hasCommonAncestor(ctx context.Context, dir, ref string) bool {
	return m.hasCommonAncestorInstrumented(ctx, "", RefreshTriggerExplicit, dir, ref)
}

func (m *Manager) hasCommonAncestorInstrumented(ctx context.Context, workspaceID string, trigger RefreshTrigger, dir, ref string) bool {
	return m.runGitErr(ctx, workspaceID, trigger, dir, "merge-base", "HEAD", ref) == nil
}

// gitStatus calculates the git status for a workspace directory.
// Returns: (dirty bool, ahead int, behind int, linesAdded int, linesRemoved int, filesChanged int, commitsSyncedWithRemote bool, remoteBranchExists bool, localUnique int, remoteUnique int, currentBranch string)
func (m *Manager) gitStatus(ctx context.Context, workspaceID string, trigger RefreshTrigger, dir, repoURL string) (dirty bool, ahead int, behind int, linesAdded int, linesRemoved int, filesChanged int, commitsSyncedWithRemote bool, remoteBranchExists bool, localUnique int, remoteUnique int, currentBranch string) {
	return m.gitStatusWithRound(ctx, workspaceID, trigger, dir, repoURL, nil)
}

func (m *Manager) gitStatusWithRound(ctx context.Context, workspaceID string, trigger RefreshTrigger, dir, repoURL string, round *pollRound) (dirty bool, ahead int, behind int, linesAdded int, linesRemoved int, filesChanged int, commitsSyncedWithRemote bool, remoteBranchExists bool, localUnique int, remoteUnique int, currentBranch string) {
	// Extract sub-caches from the poll round (nil-safe)
	var fetchRound *gitFetchPollRound
	var wtCache *worktreeListCache
	if round != nil {
		fetchRound = round.fetch
		wtCache = round.worktree
	}

	// Skip fetch for watcher-triggered refreshes — the watcher exists for fast
	// local feedback (dirty files, branch switches). The 10-second poller already
	// handles remote state, so running a network fetch (~700ms) on every file
	// save is wasteful.
	if trigger != RefreshTriggerWatcher {
		_ = m.gitFetchInstrumentedWithRound(ctx, workspaceID, trigger, dir, fetchRound)
	}

	// Fast-forward local default branch in bare clone to match origin
	if isWorktree(dir) {
		if bareRepoPath, err := resolveWorktreeBaseFromWorktree(dir); err == nil {
			m.updateLocalDefaultBranch(ctx, workspaceID, trigger, bareRepoPath, repoURL, wtCache)
		}
	}

	// Check for dirty state (any changes: modified, added, removed, or untracked)
	output, err := m.runGit(ctx, workspaceID, trigger, dir, "status", "--porcelain")
	trimmedOutput := strings.TrimSpace(string(output))
	dirty = err == nil && len(trimmedOutput) > 0

	// Count files changed from porcelain output
	if err == nil && trimmedOutput != "" {
		filesChanged = len(strings.Split(trimmedOutput, "\n"))
	}

	// Check ahead/behind counts using rev-list
	// Compare against the detected default branch to show GitHub-style status:
	// - ahead = commits in this branch not in default branch
	// - behind = commits in default branch not in this branch
	defaultBranch, err := m.GetDefaultBranch(ctx, repoURL)
	if err == nil {
		output, err = m.runGit(ctx, workspaceID, trigger, dir, "rev-list", "--left-right", "--count", "HEAD...origin/"+defaultBranch)
		if err != nil {
			// No upstream or other error - log but continue to calculate line changes
			m.logger.Debug("git rev-list failed", "ref", "origin/"+defaultBranch, "dir", dir)
		} else {
			// Parse output: "ahead\tbehind" (e.g., "3\t2" means 3 ahead, 2 behind)
			parts := strings.Split(strings.TrimSpace(string(output)), "\t")
			if len(parts) == 2 {
				ahead, _ = strconv.Atoi(parts[0])
				behind, _ = strconv.Atoi(parts[1])
			}
		}
	}

	// Check if local HEAD matches origin/{branch} (indicates commits are synced to remote branch)
	// Get current branch name first
	currentBranch, _ = m.gitCurrentBranchInstrumented(ctx, workspaceID, trigger, dir)
	if currentBranch != "" && currentBranch != "HEAD" {
		// Check if origin/{branch} exists
		remoteBranchExists, _ = m.gitRemoteBranchExistsInstrumented(ctx, workspaceID, trigger, dir, currentBranch)
		if remoteBranchExists {
			remoteRef := "origin/" + currentBranch

			// Calculate unique commits using rev-list --left-right --count.
			// This also tells us whether commits are synced: if both counts are 0,
			// HEAD and origin/<branch> point to the same commit.
			revOutput, revErr := m.runGit(ctx, workspaceID, trigger, dir, "rev-list", "--left-right", "--count", "HEAD..."+remoteRef)
			if revErr == nil {
				parts := strings.Split(strings.TrimSpace(string(revOutput)), "\t")
				if len(parts) == 2 {
					localUnique, _ = strconv.Atoi(parts[0])  // commits local has (left)
					remoteUnique, _ = strconv.Atoi(parts[1]) // commits remote has (right)
				}
			}
			commitsSyncedWithRemote = (localUnique == 0 && remoteUnique == 0)
		}
	}

	// Get line additions/deletions from uncommitted changes using diff --numstat HEAD
	// Using HEAD includes both staged and unstaged changes
	// Output format per line: "additions\tdeletions\tfilename"
	output, err = m.runGit(ctx, workspaceID, trigger, dir, "diff", "--numstat", "HEAD")
	if err == nil {
		trimmed := strings.TrimSpace(string(output))
		if trimmed != "" {
			lines := strings.Split(trimmed, "\n")
			for _, line := range lines {
				parts := strings.Split(line, "\t")
				if len(parts) >= 3 {
					if a, err := strconv.Atoi(parts[0]); err == nil {
						linesAdded += a
					}
					if r, err := strconv.Atoi(parts[1]); err == nil && parts[1] != "-" {
						linesRemoved += r
					}
				}
			}
		}
	}

	// Get untracked files and count their lines as additions
	// ls-files --others --exclude-standard lists untracked files (respecting .gitignore)
	untrackedOutput, err := m.runGit(ctx, workspaceID, trigger, dir, "ls-files", "--others", "--exclude-standard")
	if err == nil {
		untrackedLines := strings.Split(string(untrackedOutput), "\n")
		for _, filePath := range untrackedLines {
			if filePath == "" {
				continue
			}
			// Check if file is binary using git's detection (with fast heuristic fallback)
			fullPath := filepath.Join(dir, filePath)
			var lineCount int
			if difftool.IsBinaryFile(ctx, dir, filePath) {
				lineCount = 0
			} else {
				// Count lines with a size cap to avoid loading large files
				lc, err := countLinesCapped(fullPath, 1024*1024) // 1MB cap
				if err != nil {
					lineCount = 0 // Skip files we can't read
				} else {
					lineCount = lc
				}
			}
			linesAdded += lineCount
		}
	}

	return dirty, ahead, behind, linesAdded, linesRemoved, filesChanged, commitsSyncedWithRemote, remoteBranchExists, localUnique, remoteUnique, currentBranch
}

// countLinesCapped counts newlines in a file up to maxBytes.
// If the file exceeds maxBytes, it only counts lines in the first maxBytes.
// This prevents loading multi-gigabyte files into memory just to count lines.
func countLinesCapped(path string, maxBytes int) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	buf := make([]byte, 8192)
	lineCount := 0
	bytesRead := 0
	prevCharWasNewline := false

	for bytesRead < maxBytes {
		toRead := len(buf)
		if bytesRead+toRead > maxBytes {
			toRead = maxBytes - bytesRead
		}
		n, err := f.Read(buf[:toRead])
		if n > 0 {
			bytesRead += n
			for i := 0; i < n; i++ {
				if buf[i] == '\n' {
					lineCount++
				}
				prevCharWasNewline = buf[i] == '\n'
			}
		}
		if err != nil {
			break
		}
	}

	// If we didn't end with a newline and read at least one byte, count the last line
	if bytesRead > 0 && !prevCharWasNewline {
		lineCount++
	}

	return lineCount, nil
}

// checkGitSafety checks if a workspace is safe to dispose based on git state.
// Returns detailed status about why the workspace is not safe.
func (m *Manager) checkGitSafety(ctx context.Context, workspaceID string) (*VCSSafetyStatus, error) {
	w, found := m.state.GetWorkspace(workspaceID)
	if !found {
		return nil, fmt.Errorf("workspace not found: %s", workspaceID)
	}

	status := &VCSSafetyStatus{Safe: true}

	if w.VCS == "sapling" {
		output, err := m.runCmd(ctx, "sl", workspaceID, RefreshTriggerExplicit, w.Path, "status")
		if err != nil {
			status.Safe = false
			status.Reason = fmt.Sprintf("sl status failed: %v", err)
			return status, nil
		}
		trimmed := strings.TrimSpace(string(output))
		if trimmed != "" {
			for _, line := range strings.Split(trimmed, "\n") {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				if strings.HasPrefix(line, "?") {
					status.UntrackedFiles++
				} else {
					status.ModifiedFiles++
				}
				status.Safe = false
			}
			if !status.Safe {
				var reasons []string
				if status.ModifiedFiles > 0 {
					reasons = append(reasons, fmt.Sprintf("%d modified file(s)", status.ModifiedFiles))
				}
				if status.UntrackedFiles > 0 {
					reasons = append(reasons, fmt.Sprintf("%d untracked file(s)", status.UntrackedFiles))
				}
				status.Reason = strings.Join(reasons, ", ")
			}
		}
		return status, nil
	}

	output, err := m.runGit(ctx, workspaceID, RefreshTriggerExplicit, w.Path, "status", "--porcelain")
	if err != nil {
		// Git command failed - this might mean the repo is corrupt, treat as unsafe
		status.Safe = false
		status.Reason = fmt.Sprintf("git status failed: %v", err)
		return status, nil
	}

	// Parse status output to count file types
	// Format: XY filename where X is staged, Y is unstaged
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Check for untracked files (starts with ??)
		if strings.HasPrefix(line, "??") {
			status.UntrackedFiles++
			status.Safe = false
			continue
		}

		// Any other output means modified/added/deleted files
		status.ModifiedFiles++
		status.Safe = false
	}

	// Use pre-computed values from gitStatus() for ahead/behind counts.
	// NOTE: These values may be stale (updated on the git status poll interval).
	// This is intentional — running a full gitStatus() here would be expensive
	// and this function is called from the dispose flow where a slight delay
	// in reflecting push state is acceptable. The dirty-file check above is
	// always fresh since it runs git status --porcelain directly.
	commitsSyncedWithRemote := w.CommitsSyncedWithRemote
	ahead := w.Ahead

	status.AheadCommits = ahead
	// Only unsafe if ahead AND not synced with remote branch
	if ahead > 0 && !commitsSyncedWithRemote {
		status.Safe = false
	}

	// Build reason string if not safe
	if !status.Safe {
		var reasons []string
		if status.ModifiedFiles > 0 {
			reasons = append(reasons, fmt.Sprintf("%d modified file(s)", status.ModifiedFiles))
		}
		if status.UntrackedFiles > 0 {
			reasons = append(reasons, fmt.Sprintf("%d untracked file(s)", status.UntrackedFiles))
		}
		if status.AheadCommits > 0 {
			reasons = append(reasons, fmt.Sprintf("%d unpushed commit(s)", status.AheadCommits))
		}
		if status.Reason != "" {
			reasons = append(reasons, status.Reason)
		}
		status.Reason = strings.Join(reasons, "; ")
	}

	return status, nil
}

// CheckWorkspaceClean returns whether a workspace directory has no unstaged changes,
// no staged changes, and no commits ahead of origin's default branch.
// Returns (true, "") if clean, or (false, reason) if not.
func CheckWorkspaceClean(dir string) (bool, string) {
	// Check for dirty working tree (unstaged + staged)
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		return false, fmt.Sprintf("git status failed: %v", err)
	}
	if len(strings.TrimSpace(string(output))) > 0 {
		return false, "workspace has uncommitted changes"
	}

	// Find the default branch
	defaultBranch := detectDefaultBranch(dir)

	// Check commits ahead of origin/default
	cmd = exec.Command("git", "rev-list", "--count", fmt.Sprintf("origin/%s..HEAD", defaultBranch))
	cmd.Dir = dir
	output, err = cmd.Output()
	if err != nil {
		return false, fmt.Sprintf("rev-list failed: %v", err)
	}
	ahead := strings.TrimSpace(string(output))
	if ahead != "0" {
		return false, fmt.Sprintf("workspace has %s commits ahead of origin/%s", ahead, defaultBranch)
	}

	return true, ""
}

// detectDefaultBranch returns the default branch name for a repo directory.
// Tries git symbolic-ref, falls back to "main".
func detectDefaultBranch(dir string) string {
	cmd := exec.Command("git", "symbolic-ref", "refs/remotes/origin/HEAD")
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		return "main"
	}
	ref := strings.TrimSpace(string(output))
	// refs/remotes/origin/main -> main
	return strings.TrimPrefix(ref, "refs/remotes/origin/")
}
