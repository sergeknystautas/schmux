// Package vcs provides a command builder abstraction for version control systems.
// It generates shell command strings for VCS operations, allowing the same logic
// to work across git and sapling by swapping the command builder implementation.
package vcs

// CommandBuilder generates shell command strings for VCS operations.
// Each method returns a complete command string ready to be executed in a shell.
type CommandBuilder interface {
	// DiffNumstat returns the command for numstat diff against HEAD.
	DiffNumstat() string
	// ShowFile returns the command to show a file at a given revision.
	ShowFile(path, revision string) string
	// FileContent returns the command to read a file from the working directory.
	FileContent(path string) string
	// UntrackedFiles returns the command to list untracked files.
	UntrackedFiles() string
	// Log returns the command for commit log output in a parseable format.
	// Format: hash|short_hash|message|author|timestamp|parents
	Log(refs []string, maxCount int) string
	// LogRange returns the command for log between forkPoint and refs.
	LogRange(refs []string, forkPoint string) string
	// ResolveRef returns the command to resolve a ref to a hash.
	ResolveRef(ref string) string
	// MergeBase returns the command to find the merge base between two refs.
	MergeBase(ref1, ref2 string) string
	// DefaultBranchRef returns the upstream branch ref (e.g., "origin/main").
	DefaultBranchRef(branch string) string
	// DetectDefaultBranch returns the command to detect the default branch name.
	// The output should be just the branch name (e.g., "main").
	DetectDefaultBranch() string
	// RevListCount returns the command to count commits in a range (e.g., "HEAD..origin/main").
	RevListCount(rangeSpec string) string
	// CurrentBranch returns a command to get the current branch name.
	CurrentBranch() string
	// StatusPorcelain returns a command to get working tree status in machine-readable format.
	StatusPorcelain() string
	// RemoteBranchExists returns a command to check if a branch exists on the remote.
	// The command should exit 0 / produce output if the branch exists.
	RemoteBranchExists(branch string) string

	// --- Working-copy mutation commands ---

	// AddFiles returns the command to stage the specified files.
	AddFiles(files []string) string
	// CommitAmendNoEdit returns the command to amend the last commit without editing its message.
	CommitAmendNoEdit() string
	// DiscardFile returns the command to discard changes to a tracked file, restoring it from HEAD.
	DiscardFile(file string) string
	// DiscardAllTracked returns the command to discard all tracked file changes.
	DiscardAllTracked() string
	// CleanUntrackedFile returns the command to remove a single untracked file.
	CleanUntrackedFile(file string) string
	// CleanAllUntracked returns the command to remove all untracked files and directories.
	CleanAllUntracked() string
	// UnstageNewFile returns the command to unstage a file that was newly added (not in HEAD).
	UnstageNewFile(file string) string
	// Uncommit returns the command to undo the last commit, keeping changes as unstaged.
	Uncommit() string
	// CheckIgnore returns the command to check if a file matches VCS ignore patterns.
	// Exit code 0 means ignored, non-zero means not ignored.
	CheckIgnore(file string) string
	// DiffUnified returns the command for a unified diff against HEAD.
	DiffUnified() string
}
