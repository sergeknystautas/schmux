package state

import (
	"time"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
)

// SessionStore defines session-related state operations.
type SessionStore interface {
	GetSessions() []Session
	GetSession(id string) (Session, bool)
	AddSession(sess Session) error
	UpdateSession(sess Session) error
	UpdateSessionFunc(id string, fn func(sess *Session)) bool
	RemoveSession(id string) error
	UpdateSessionLastOutput(sessionID string, t time.Time)
	UpdateSessionLastSignal(sessionID string, t time.Time)
	UpdateSessionXtermTitle(sessionID, title string) bool
	IncrementNudgeSeq(sessionID string) uint64
	GetNudgeSeq(sessionID string) uint64
	UpdateSessionNudge(sessionID, nudge string) error
	ClearSessionNudge(sessionID string) bool
}

// WorkspaceStore defines workspace-related state operations.
type WorkspaceStore interface {
	GetWorkspaces() []Workspace
	GetWorkspace(id string) (Workspace, bool)
	FindWorkspaceByRepoBranch(repo, branch string) (Workspace, bool)
	AddWorkspace(ws Workspace) error
	UpdateWorkspace(ws Workspace) error
	RemoveWorkspace(id string) error
	UpdateOverlayManifest(workspaceID string, manifest map[string]string)
	UpdateOverlayManifestEntry(workspaceID, relPath, hash string)
	GetWorkspaceTabs(workspaceID string) []Tab
	AddTab(workspaceID string, tab Tab) error
	RemoveTab(workspaceID, tabID string) error
	GetWorkspaceResolveConflicts(workspaceID string) []ResolveConflict
	GetResolveConflict(workspaceID, hash string) (ResolveConflict, bool)
	UpsertResolveConflict(workspaceID string, conflict ResolveConflict) error
	RemoveResolveConflict(workspaceID, hash string) error
}

// RemoteHostStore defines remote host state operations.
type RemoteHostStore interface {
	GetRemoteHosts() []RemoteHost
	GetRemoteHost(id string) (RemoteHost, bool)
	GetRemoteHostByFlavorID(flavorID string) (RemoteHost, bool)
	GetRemoteHostsByFlavorID(flavorID string) []RemoteHost
	GetRemoteHostByProfileAndFlavor(profileID, flavor string) (RemoteHost, bool)
	GetRemoteHostsByProfileAndFlavor(profileID, flavor string) []RemoteHost
	GetRemoteHostsByProfileID(profileID string) []RemoteHost
	GetRemoteHostByHostname(hostname string) (RemoteHost, bool)
	AddRemoteHost(rh RemoteHost) error
	UpdateRemoteHost(rh RemoteHost) error
	UpdateRemoteHostStatus(id, status string) error
	UpdateRemoteHostProvisioned(id string, provisioned bool) error
	RemoveRemoteHost(id string) error
	GetSessionsByRemoteHostID(hostID string) []Session
	GetWorkspacesByRemoteHostID(hostID string) []Workspace
}

// PersistenceStore defines state persistence operations.
type PersistenceStore interface {
	Save() error
	SaveBatched()
	FlushPending()
	GetNeedsRestart() bool
	SetNeedsRestart(needsRestart bool) error
}

// StateStore defines the full interface for state persistence.
// It composes all domain-specific sub-interfaces.
type StateStore interface {
	SessionStore
	WorkspaceStore
	RemoteHostStore
	PersistenceStore

	// Preview operations
	GetPreviews() []WorkspacePreview
	GetWorkspacePreviews(workspaceID string) []WorkspacePreview
	GetPreview(id string) (WorkspacePreview, bool)
	FindPreview(workspaceID, targetHost string, targetPort int) (WorkspacePreview, bool)
	UpsertPreview(preview WorkspacePreview) error
	RemovePreview(id string) error
	RemoveWorkspacePreviews(workspaceID string) int

	// Repo base operations
	GetRepoBases() []RepoBase
	GetRepoBaseByURL(repoURL string) (RepoBase, bool)
	AddRepoBase(wb RepoBase) error

	// PR discovery state
	GetPullRequests() []contracts.PullRequest
	SetPullRequests(prs []contracts.PullRequest)
	GetPublicRepos() []string
	SetPublicRepos(repos []string)

	// DashboardSX status
	GetDashboardSXStatus() *DashboardSXStatus
	SetDashboardSXStatus(status *DashboardSXStatus)
}

// Compile-time interface checks.
var _ StateStore = (*State)(nil)
var _ SessionStore = (*State)(nil)
var _ WorkspaceStore = (*State)(nil)
var _ RemoteHostStore = (*State)(nil)
var _ PersistenceStore = (*State)(nil)
