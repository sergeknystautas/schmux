package workspace

import (
	"context"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/state"
)

// ScanResult represents the results of a workspace scan operation.
type ScanResult struct {
	Added   []state.Workspace `json:"added"`
	Updated []WorkspaceChange `json:"updated"`
	Removed []state.Workspace `json:"removed"`
}

// WorkspaceChange represents a workspace that was updated, with old and new values.
type WorkspaceChange struct {
	Old state.Workspace `json:"old"`
	New state.Workspace `json:"new"`
}

// RecentBranch represents a branch with its recent commit information.
type RecentBranch struct {
	RepoName   string `json:"repo_name"`
	RepoURL    string `json:"repo_url"`
	Branch     string `json:"branch"`
	CommitDate string `json:"commit_date"`
	Subject    string `json:"subject"`
}

// LinearSyncResult represents the result of a linear sync operation (from or to main).
type LinearSyncResult struct {
	Success         bool   `json:"success"`
	SuccessCount    int    `json:"success_count,omitempty"`    // Number of commits successfully applied
	ConflictingHash string `json:"conflicting_hash,omitempty"` // The commit hash that caused the conflict, if any
	Branch          string `json:"branch,omitempty"`           // The branch name synced with
}

// ConflictResolution represents a single conflict that was resolved during rebase.
type ConflictResolution struct {
	LocalCommit        string   `json:"local_commit"`
	LocalCommitMessage string   `json:"local_commit_message"`
	AllResolved        bool     `json:"all_resolved"`
	Confidence         string   `json:"confidence"`
	Summary            string   `json:"summary"`
	Files              []string `json:"files"`
}

// LinearSyncResolveConflictResult contains the result of a conflict resolution rebase.
type LinearSyncResolveConflictResult struct {
	Success     bool                 `json:"success"`
	Message     string               `json:"message"`
	Hash        string               `json:"hash,omitempty"`
	Resolutions []ConflictResolution `json:"resolutions"`
}

// ResolveConflictStep represents a progress step emitted during conflict resolution.
type ResolveConflictStep struct {
	Action             string   `json:"action"`
	Status             string   `json:"status"` // "in_progress", "done", "failed"
	Message            string   `json:"message"`
	Hash               string   `json:"hash,omitempty"`
	LocalCommit        string   `json:"local_commit,omitempty"`
	LocalCommitMessage string   `json:"local_commit_message,omitempty"`
	Files              []string `json:"files,omitempty"`
	Confidence         string   `json:"confidence,omitempty"`
	Summary            string   `json:"summary,omitempty"`
	Created            *bool    `json:"created,omitempty"`
}

// ResolveConflictStepFunc is a callback invoked at each step of the conflict resolution process.
type ResolveConflictStepFunc func(step ResolveConflictStep)

// GitSafetyStatus represents the git safety status of a workspace.
type GitSafetyStatus struct {
	Safe           bool   // true if workspace is safe to dispose
	Reason         string // explanation if not safe
	ModifiedFiles  int    // number of modified files
	UntrackedFiles int    // number of untracked files
	AheadCommits   int    // number of unpushed commits
}

// WorkspaceManager defines the interface for workspace operations.
type WorkspaceManager interface {
	// GetByID returns a workspace by its ID.
	GetByID(workspaceID string) (*state.Workspace, bool)

	// GetOrCreate finds an existing workspace for the repoURL/branch or creates a new one.
	// Returns a workspace ready for use (fetch/pull/clean already done).
	GetOrCreate(ctx context.Context, repoURL, branch string) (*state.Workspace, error)

	// Cleanup cleans up a workspace by resetting git state.
	Cleanup(ctx context.Context, workspaceID string) error

	// UpdateGitStatus refreshes the git status for a single workspace.
	UpdateGitStatus(ctx context.Context, workspaceID string) (*state.Workspace, error)

	// GetWorkspaceGitFiles returns detailed file change info for a workspace.
	// This includes file paths, status, and line counts. It does NOT fetch from the remote.
	GetWorkspaceGitFiles(ctx context.Context, workspaceID string) ([]GitChangedFile, error)

	// UpdateAllGitStatus refreshes git status for all workspaces.
	UpdateAllGitStatus(ctx context.Context)

	// EnsureWorkspaceDir ensures the workspace base directory exists.
	EnsureWorkspaceDir() error

	// Dispose deletes a workspace by removing its directory and removing it from state.
	Dispose(workspaceID string) error

	// Scan scans the workspace directory and reconciles state with filesystem.
	// Returns what was added, updated, and removed.
	Scan() (ScanResult, error)

	// RefreshOverlay reapplies overlay files to an existing workspace.
	RefreshOverlay(ctx context.Context, workspaceID string) error

	// EnsureOverlayDirs ensures overlay directories exist for all configured repos.
	EnsureOverlayDirs(repos []config.Repo) error

	// GetWorkspaceConfig returns the cached workspace config for the given workspace ID.
	GetWorkspaceConfig(workspaceID string) *contracts.RepoConfig

	// CreateLocalRepo creates a new workspace with a fresh local git repository.
	CreateLocalRepo(ctx context.Context, repoName, branch string) (*state.Workspace, error)

	// CreateFromWorkspace creates a new workspace with a new branch,
	// branching from the source workspace's current branch/commit.
	CreateFromWorkspace(ctx context.Context, sourceWorkspaceID, newBranch string) (*state.Workspace, error)

	// GetDefaultBranch returns the detected default branch for a repo URL.
	GetDefaultBranch(ctx context.Context, repoURL string) (string, error)

	// LinearSyncFromDefault performs an iterative rebase from the default branch into the current branch.
	LinearSyncFromDefault(ctx context.Context, workspaceID string) (*LinearSyncResult, error)

	// LinearSyncToDefault performs a fast-forward push to the default branch.
	LinearSyncToDefault(ctx context.Context, workspaceID string) (*LinearSyncResult, error)

	// LinearSyncResolveConflict rebases exactly one commit from the default branch, handling conflicts.
	// The optional onStep callback is called at each progress step (may be nil).
	LinearSyncResolveConflict(ctx context.Context, workspaceID string, onStep ResolveConflictStepFunc) (*LinearSyncResolveConflictResult, error)

	// EnsureOriginQueries ensures origin query repos exist for all configured repos.
	EnsureOriginQueries(ctx context.Context) error

	// FetchOriginQueries fetches updates for all origin query repos.
	FetchOriginQueries(ctx context.Context)

	// GetRecentBranches returns recent branches from all bare clones, sorted by commit date.
	GetRecentBranches(ctx context.Context, limit int) ([]RecentBranch, error)

	// GetBranchCommitLog returns commit subjects for a branch relative to the default branch.
	GetBranchCommitLog(ctx context.Context, repoURL, branch string, limit int) ([]string, error)

	// CheckoutPR creates a workspace from a GitHub pull request ref.
	CheckoutPR(ctx context.Context, pr contracts.PullRequest) (*state.Workspace, error)

	// GetGitGraph returns the commit graph for a workspace showing local branch vs origin/main.
	// maxTotal: Maximum total commits to display (applied after category limits)
	// mainContext: Number of commits on main BEFORE fork point (historical context)
	GetGitGraph(ctx context.Context, workspaceID string, maxTotal int, mainContext int) (*contracts.GitGraphResponse, error)
}

// Ensure *Manager implements WorkspaceManager at compile time.
var _ WorkspaceManager = (*Manager)(nil)
