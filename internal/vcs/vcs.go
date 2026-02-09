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
}
