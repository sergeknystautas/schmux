package workspace

import "context"

type VCSBackend interface {
	EnsureRepoBase(ctx context.Context, repoIdentifier, basePath string) (string, error)
	CreateWorkspace(ctx context.Context, repoBasePath, branch, destPath string) error
	RemoveWorkspace(ctx context.Context, workspacePath string) error
	PruneStale(ctx context.Context, repoBasePath string) error
	Fetch(ctx context.Context, path string) error
	IsBranchInUse(ctx context.Context, repoBasePath, branch string) (bool, error)
	GetStatus(ctx context.Context, workspacePath string) (VCSStatus, error)
	GetChangedFiles(ctx context.Context, workspacePath string) ([]VCSChangedFile, error)
	GetDefaultBranch(ctx context.Context, repoBasePath string) (string, error)
	GetCurrentBranch(ctx context.Context, workspacePath string) (string, error)
	EnsureQueryRepo(ctx context.Context, repoIdentifier, path string) error
	FetchQueryRepo(ctx context.Context, path string) error
	ListRecentBranches(ctx context.Context, path string, limit int) ([]RecentBranch, error)
	GetBranchLog(ctx context.Context, path, branch string, limit int) ([]string, error)
}

type VCSStatus struct {
	Dirty                 bool
	CurrentBranch         string
	AheadOfDefault        int
	BehindDefault         int
	LinesAdded            int
	LinesRemoved          int
	FilesChanged          int
	SyncedWithRemote      bool
	RemoteBranchExists    bool
	LocalUniqueCommits    int
	RemoteUniqueCommits   int
	DefaultBranchOrphaned bool
}

type VCSChangedFile struct {
	Path         string
	Status       string
	LinesAdded   int
	LinesRemoved int
}

// IsGitVCS returns true if the VCS type string represents a git-based VCS.
// Empty string defaults to git for backward compatibility.
func IsGitVCS(vcs string) bool {
	switch vcs {
	case "", "git", "git-worktree", "git-clone":
		return true
	default:
		return false
	}
}
