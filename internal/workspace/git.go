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
	// Resolve to worktree base if this is a worktree
	fetchDir := dir
	if isWorktree(dir) {
		if worktreeBase, err := resolveWorktreeBaseFromWorktree(dir); err == nil {
			fetchDir = worktreeBase
		}
	}

	args := []string{"fetch"}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = fetchDir

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git fetch failed: %w: %s", err, string(output))
	}

	return nil
}

// gitCheckoutBranch runs git checkout -B, optionally resetting to origin/<branch>.
func (m *Manager) gitCheckoutBranch(ctx context.Context, dir, branch string, remoteBranchExists bool) error {
	args := []string{"checkout", "-B", branch}
	if remoteBranchExists {
		args = append(args, "origin/"+branch)
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git checkout failed: %w: %s", err, string(output))
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
		fmt.Printf("[workspace] cannot determine default branch, skipping reuse: %v\n", err)
		return false
	}

	cmd := exec.CommandContext(ctx, "git", "merge-base", "--is-ancestor", "HEAD", "origin/"+defaultBranch)
	cmd.Dir = dir
	return cmd.Run() == nil
}

// gitPullRebase runs git pull --rebase origin <branch>.
// For cloned repos with an origin remote, this avoids relying on potentially incorrect
// upstream config. For local repos without origin, skips the pull.
func (m *Manager) gitPullRebase(ctx context.Context, dir, branch string) error {
	// Check if origin remote exists
	remoteCmd := exec.CommandContext(ctx, "git", "remote", "get-url", "origin")
	remoteCmd.Dir = dir
	if _, err := remoteCmd.CombinedOutput(); err != nil {
		// No origin remote - local-only repo, nothing to pull
		fmt.Printf("[workspace] no origin remote, skipping pull\n")
		return nil
	}

	// Explicitly pull from origin/<branch> to avoid broken upstream config
	args := []string{"pull", "--rebase", "origin", branch}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git pull failed: %w: %s", err, string(output))
	}

	return nil
}

// gitHasOriginRemote checks if the repo has an origin remote configured.
func (m *Manager) gitHasOriginRemote(ctx context.Context, dir string) bool {
	remoteCmd := exec.CommandContext(ctx, "git", "remote", "get-url", "origin")
	remoteCmd.Dir = dir
	return remoteCmd.Run() == nil
}

// gitRemoteBranchExists checks for refs/remotes/origin/<branch>.
func (m *Manager) gitRemoteBranchExists(ctx context.Context, dir, branch string) (bool, error) {
	ref := "refs/remotes/origin/" + branch
	cmd := exec.CommandContext(ctx, "git", "show-ref", "--verify", "--quiet", ref)
	cmd.Dir = dir

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, fmt.Errorf("git show-ref failed: %w", err)
	}

	return true, nil
}

// gitCheckoutDot runs git checkout -- .
func (m *Manager) gitCheckoutDot(ctx context.Context, dir string) error {
	args := []string{"checkout", "--", "."}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git checkout -- . failed: %w: %s", err, string(output))
	}

	return nil
}

// gitCurrentBranch returns the current branch name for a directory.
func (m *Manager) gitCurrentBranch(ctx context.Context, dir string) (string, error) {
	args := []string{"rev-parse", "--abbrev-ref", "HEAD"}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git rev-parse failed: %w: %s", err, string(output))
	}

	return strings.TrimSpace(string(output)), nil
}

// gitClean runs git clean -fd.
func (m *Manager) gitClean(ctx context.Context, dir string) error {
	args := []string{"clean", "-fd"}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git clean failed: %w: %s", err, string(output))
	}

	return nil
}

// GetWorkspaceGitFiles returns the list of changed files for a workspace.
// Implements the WorkspaceManager interface.
func (m *Manager) GetWorkspaceGitFiles(ctx context.Context, workspaceID string) ([]GitChangedFile, error) {
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
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "diff", "HEAD", "--numstat", "--find-renames", "--diff-filter=ADM")
	output, err := cmd.Output()
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
			checkCmd := exec.CommandContext(ctx, "git", "-C", dir, "cat-file", "-e", "HEAD:"+filePath)
			if err := checkCmd.Run(); err != nil {
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
	untrackedCmd := exec.CommandContext(ctx, "git", "-C", dir, "ls-files", "--others", "--exclude-standard")
	untrackedOutput, err := untrackedCmd.Output()
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

// gitStatus calculates the git status for a workspace directory.
// Returns: (dirty bool, ahead int, behind int, linesAdded int, linesRemoved int, filesChanged int, commitsSyncedWithRemote bool)
func (m *Manager) gitStatus(ctx context.Context, dir, repoURL string) (dirty bool, ahead int, behind int, linesAdded int, linesRemoved int, filesChanged int, commitsSyncedWithRemote bool) {
	// Fetch to get latest remote state for accurate ahead/behind counts
	_ = m.gitFetch(ctx, dir)

	// Check for dirty state (any changes: modified, added, removed, or untracked)
	statusCmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	statusCmd.Dir = dir
	output, err := statusCmd.CombinedOutput()
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
		revListCmd := exec.CommandContext(ctx, "git", "rev-list", "--left-right", "--count", "HEAD...origin/"+defaultBranch)
		revListCmd.Dir = dir
		output, err = revListCmd.CombinedOutput()
		if err != nil {
			// No upstream or other error - log but continue to calculate line changes
			fmt.Printf("[workspace] git rev-list HEAD...origin/%s failed for %s: %s\n", defaultBranch, dir, strings.TrimSpace(string(output)))
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
	currentBranch, _ := m.gitCurrentBranch(ctx, dir)
	if currentBranch != "" && currentBranch != "HEAD" {
		// Check if origin/{branch} exists and compare with HEAD
		remoteRef := "origin/" + currentBranch
		mergeBaseCmd := exec.CommandContext(ctx, "git", "merge-base", "--is-ancestor", "HEAD", remoteRef)
		mergeBaseCmd.Dir = dir
		isAncestor := mergeBaseCmd.Run() == nil

		reverseCmd := exec.CommandContext(ctx, "git", "merge-base", "--is-ancestor", remoteRef, "HEAD")
		reverseCmd.Dir = dir
		remoteIsAncestor := reverseCmd.Run() == nil

		// Commits are synced if HEAD is an ancestor of remote AND remote is an ancestor of HEAD
		// (meaning they point to the same commit)
		commitsSyncedWithRemote = isAncestor && remoteIsAncestor
	}

	// Get line additions/deletions from uncommitted changes using diff --numstat HEAD
	// Using HEAD includes both staged and unstaged changes
	// Output format per line: "additions\tdeletions\tfilename"
	diffCmd := exec.CommandContext(ctx, "git", "diff", "--numstat", "HEAD")
	diffCmd.Dir = dir
	output, err = diffCmd.CombinedOutput()
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
	untrackedCmd := exec.CommandContext(ctx, "git", "ls-files", "--others", "--exclude-standard")
	untrackedCmd.Dir = dir
	untrackedOutput, err := untrackedCmd.Output()
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

	return dirty, ahead, behind, linesAdded, linesRemoved, filesChanged, commitsSyncedWithRemote
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
func (m *Manager) checkGitSafety(ctx context.Context, workspaceID string) (*GitSafetyStatus, error) {
	w, found := m.state.GetWorkspace(workspaceID)
	if !found {
		return nil, fmt.Errorf("workspace not found: %s", workspaceID)
	}

	status := &GitSafetyStatus{Safe: true}

	// Check for dirty state (any changes: modified, added, removed, or untracked)
	statusCmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	statusCmd.Dir = w.Path
	output, err := statusCmd.CombinedOutput()
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
	ahead := w.GitAhead

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
