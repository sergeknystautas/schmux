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
	Success         bool     `json:"success"`
	SuccessCount    int      `json:"success_count,omitempty"`    // Number of commits successfully applied
	ConflictingHash string   `json:"conflicting_hash,omitempty"` // The commit hash that caused the conflict, if any
	Branch          string   `json:"branch,omitempty"`           // The branch name synced with
	Message         string   `json:"message,omitempty"`          // Human-readable message (e.g., error context)
	NeedsConfirm    bool     `json:"needs_confirm,omitempty"`    // True if push needs confirmation before proceeding
	DivergedCommits []string `json:"diverged_commits,omitempty"` // Commits on origin that would be overwritten
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
	Action             string              `json:"action"`
	Status             string              `json:"status"` // "in_progress", "done", "failed"
	Message            []string            `json:"message"`
	Hash               string              `json:"hash,omitempty"`
	HashMessage        string              `json:"hash_message,omitempty"`
	LocalCommit        string              `json:"local_commit,omitempty"`
	LocalCommitMessage string              `json:"local_commit_message,omitempty"`
	Files              []string            `json:"files,omitempty"`
	ConflictDiffs      map[string][]string `json:"conflict_diffs,omitempty"` // file path -> conflict marker hunks
	Confidence         string              `json:"confidence,omitempty"`
	Summary            string              `json:"summary,omitempty"`
	TmuxSession        string              `json:"tmux_session,omitempty"` // tmux session name for live terminal streaming
	Created            *bool               `json:"created,omitempty"`
}

// ResolveConflictStepFunc is a callback invoked at each step of the conflict resolution process.
type ResolveConflictStepFunc func(step ResolveConflictStep)

type VCSSafetyStatus struct {
	Safe           bool   // true if workspace is safe to dispose
	Reason         string // explanation if not safe
	ModifiedFiles  int    // number of modified files
	UntrackedFiles int    // number of untracked files
	AheadCommits   int    // number of unpushed commits
}

// WorkspaceCRUD defines workspace lifecycle operations.
type WorkspaceCRUD interface {
	GetByID(workspaceID string) (*state.Workspace, bool)
	GetOrCreate(ctx context.Context, repoURL, branch string) (*state.Workspace, error)
	Dispose(ctx context.Context, workspaceID string) error
	DisposeForce(ctx context.Context, workspaceID string) error
	Purge(ctx context.Context, workspaceID string) error
	PurgeAll(ctx context.Context, repoURL string) (int, error)
	Cleanup(ctx context.Context, workspaceID string) error
	CreateLocalRepo(ctx context.Context, repoName, branch string) (*state.Workspace, error)
	CreateFromWorkspace(ctx context.Context, sourceWorkspaceID, newBranch string) (*state.Workspace, error)
	GetWorkspaceConfig(workspaceID string) *contracts.RepoConfig
	IsWorkspaceLocked(workspaceID string) bool
	MarkWorkspaceDisposing(workspaceID string) (previousStatus string, err error)
	RevertWorkspaceStatus(workspaceID, previousStatus string)
	Scan() (ScanResult, error)

	// Tab lifecycle
	OpenCommitTab(wsID, hash string) (*state.Tab, error)
	OpenMarkdownTab(wsID, filepath string) (*state.Tab, error)
	OpenPreviewTab(wsID, previewID string, port int) (*state.Tab, error)
	OpenResolveConflictTab(wsID, hash string) (*state.Tab, error)
	CloseTab(wsID, tabID string) error
	RegisterTabCloseHook(kind string, hook TabCloseHook)
	AddWorkspaceWithTabs(ws state.Workspace) error
}

// WorkspaceVCS defines version control operations on workspaces.
type WorkspaceVCS interface {
	UpdateVCSStatus(ctx context.Context, workspaceID string) (*state.Workspace, error)
	UpdateAllVCSStatus(ctx context.Context)
	GetWorkspaceChangedFiles(ctx context.Context, workspaceID string) ([]GitChangedFile, error)
	GetDefaultBranch(ctx context.Context, repoURL string) (string, error)
	GetGitGraph(ctx context.Context, workspaceID string, maxTotal int, mainContext int) (*contracts.CommitGraphResponse, error)
	GetCommitDetail(ctx context.Context, workspaceID, commitHash string) (*contracts.CommitDetailResponse, error)
	GetRecentBranches(ctx context.Context, limit int) ([]RecentBranch, error)
	GetBranchCommitLog(ctx context.Context, repoURL, branch string, limit int) ([]string, error)
	LinearSyncFromDefault(ctx context.Context, workspaceID string) (*LinearSyncResult, error)
	LinearSyncToDefault(ctx context.Context, workspaceID string) (*LinearSyncResult, error)
	LinearSyncResolveConflict(ctx context.Context, workspaceID string, onStep ResolveConflictStepFunc) (*LinearSyncResolveConflictResult, error)
	PushToBranch(ctx context.Context, workspaceID string, confirm bool) (*LinearSyncResult, error)
	CheckoutPR(ctx context.Context, pr contracts.PullRequest) (*state.Workspace, error)
}

// WorkspaceInfra defines infrastructure and overlay operations.
type WorkspaceInfra interface {
	EnsureWorkspaceDir() error
	EnsureOriginQueries(ctx context.Context) error
	FetchOriginQueries(ctx context.Context)
	RefreshOverlay(ctx context.Context, workspaceID string) error
	EnsureOverlayDirs(repos []config.Repo) error
}

// WorkspaceManager defines the full interface for workspace operations.
// It composes all domain-specific sub-interfaces.
type WorkspaceManager interface {
	WorkspaceCRUD
	WorkspaceVCS
	WorkspaceInfra
}

// Compile-time interface checks.
var _ WorkspaceManager = (*Manager)(nil)
var _ WorkspaceCRUD = (*Manager)(nil)
var _ WorkspaceVCS = (*Manager)(nil)
var _ WorkspaceInfra = (*Manager)(nil)
