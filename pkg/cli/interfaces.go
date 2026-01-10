package cli

import (
	"context"
)

// DaemonClient is the interface for communicating with the schmux daemon.
type DaemonClient interface {
	// IsRunning checks if the daemon is running.
	IsRunning() bool

	// GetConfig fetches the daemon configuration.
	GetConfig() (*Config, error)

	// GetWorkspaces fetches all workspaces.
	GetWorkspaces() ([]Workspace, error)

	// GetSessions fetches all sessions grouped by workspace.
	GetSessions() ([]WorkspaceWithSessions, error)

	// Spawn spawns a new session.
	Spawn(ctx context.Context, req SpawnRequest) ([]SpawnResult, error)

	// DisposeSession disposes a session.
	DisposeSession(ctx context.Context, sessionID string) error

	// ScanWorkspaces triggers a workspace scan.
	ScanWorkspaces(ctx context.Context) (*ScanResult, error)
}
