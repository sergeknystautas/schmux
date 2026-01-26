package workspace

import (
	"context"

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

	// LinearSyncFromMain performs an iterative rebase from origin/main into the current branch.
	LinearSyncFromMain(ctx context.Context, workspaceID string) (*LinearSyncResult, error)

	// LinearSyncToMain performs a fast-forward push to origin/main.
	LinearSyncToMain(ctx context.Context, workspaceID string) (*LinearSyncResult, error)
}

// Ensure *Manager implements WorkspaceManager at compile time.
var _ WorkspaceManager = (*Manager)(nil)
