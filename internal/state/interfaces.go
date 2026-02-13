package state

import (
	"time"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
)

// StateStore defines the interface for state persistence.
type StateStore interface {
	// Session operations
	GetSessions() []Session
	GetSession(id string) (Session, bool)
	AddSession(sess Session) error
	UpdateSession(sess Session) error
	RemoveSession(id string) error
	UpdateSessionLastOutput(sessionID string, t time.Time)
	UpdateSessionLastSignal(sessionID string, t time.Time)
	IncrementNudgeSeq(sessionID string) uint64
	GetNudgeSeq(sessionID string) uint64
	UpdateSessionNudge(sessionID, nudge string) error
	ClearSessionNudge(sessionID string) bool

	// Workspace operations
	GetWorkspaces() []Workspace
	GetWorkspace(id string) (Workspace, bool)
	AddWorkspace(ws Workspace) error
	UpdateWorkspace(ws Workspace) error
	RemoveWorkspace(id string) error

	// Preview operations
	GetPreviews() []WorkspacePreview
	GetWorkspacePreviews(workspaceID string) []WorkspacePreview
	GetPreview(id string) (WorkspacePreview, bool)
	FindPreview(workspaceID, targetHost string, targetPort int) (WorkspacePreview, bool)
	UpsertPreview(preview WorkspacePreview) error
	RemovePreview(id string) error
	RemoveWorkspacePreviews(workspaceID string) int

	// Worktree base operations (for git worktrees)
	GetWorktreeBases() []WorktreeBase
	GetWorktreeBaseByURL(repoURL string) (WorktreeBase, bool)
	AddWorktreeBase(wb WorktreeBase) error

	// Remote host operations
	GetRemoteHosts() []RemoteHost
	GetRemoteHost(id string) (RemoteHost, bool)
	GetRemoteHostByFlavorID(flavorID string) (RemoteHost, bool)
	GetRemoteHostByHostname(hostname string) (RemoteHost, bool)
	AddRemoteHost(rh RemoteHost) error
	UpdateRemoteHost(rh RemoteHost) error
	UpdateRemoteHostStatus(id, status string) error
	RemoveRemoteHost(id string) error
	GetSessionsByRemoteHostID(hostID string) []Session
	GetWorkspacesByRemoteHostID(hostID string) []Workspace

	// PR discovery state
	GetPullRequests() []contracts.PullRequest
	SetPullRequests(prs []contracts.PullRequest)
	GetPublicRepos() []string
	SetPublicRepos(repos []string)

	// Daemon state
	GetNeedsRestart() bool
	SetNeedsRestart(needsRestart bool) error

	// Persistence
	Save() error
}

// Ensure State implements StateStore at compile time.
var _ StateStore = (*State)(nil)
