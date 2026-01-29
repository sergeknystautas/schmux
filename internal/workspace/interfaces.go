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

	// UpdateAllGitStatus refreshes git status for all workspaces.
	// If forceAll is true, updates all workspaces regardless of session activity.
	UpdateAllGitStatus(ctx context.Context, forceAll bool)

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

	// LinearSyncFromMain performs an iterative rebase from origin/main into the current branch.
	LinearSyncFromMain(ctx context.Context, workspaceID string) (*LinearSyncResult, error)

	// LinearSyncToMain performs a fast-forward push to origin/main.
	LinearSyncToMain(ctx context.Context, workspaceID string) (*LinearSyncResult, error)

	// LinearSyncResolveConflict rebases exactly one commit from main, handling conflicts.
	LinearSyncResolveConflict(ctx context.Context, workspaceID string) (*LinearSyncResolveConflictResult, error)

	// EnsureBareClones ensures bare clones exist for all configured repos (for branch querying).
	EnsureBareClones(ctx context.Context) error

	// FetchBareClones fetches updates for all bare clones.
	FetchBareClones(ctx context.Context)

	// GetRecentBranches returns recent branches from all bare clones, sorted by commit date.
	GetRecentBranches(ctx context.Context, limit int) ([]RecentBranch, error)

	// GetBranchCommitLog returns commit subjects for a branch relative to the default branch.
	GetBranchCommitLog(ctx context.Context, repoURL, branch string, limit int) ([]string, error)
}

// Ensure *Manager implements WorkspaceManager at compile time.
var _ WorkspaceManager = (*Manager)(nil)
