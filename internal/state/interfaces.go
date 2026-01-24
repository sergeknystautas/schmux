package state

import "time"

// StateStore defines the interface for state persistence.
type StateStore interface {
	// Session operations
	GetSessions() []Session
	GetSession(id string) (Session, bool)
	AddSession(sess Session) error
	UpdateSession(sess Session) error
	RemoveSession(id string) error
	UpdateSessionLastOutput(sessionID string, t time.Time)

	// Workspace operations
	GetWorkspaces() []Workspace
	GetWorkspace(id string) (Workspace, bool)
	AddWorkspace(ws Workspace) error
	UpdateWorkspace(ws Workspace) error
	RemoveWorkspace(id string) error

	// Base repo operations (for git worktrees)
	GetBaseRepos() []BaseRepo
	GetBaseRepoByURL(repoURL string) (BaseRepo, bool)
	AddBaseRepo(br BaseRepo) error

	// Daemon state
	GetNeedsRestart() bool
	SetNeedsRestart(needsRestart bool) error

	// Persistence
	Save() error
}

// Ensure State implements StateStore at compile time.
var _ StateStore = (*State)(nil)
