package main

import (
	"context"

	"github.com/sergek/schmux/pkg/cli"
)

// MockDaemonClient is a mock implementation of DaemonClient for testing.
type MockDaemonClient struct {
	isRunning      bool
	config         *cli.Config
	workspaces     []cli.Workspace
	sessions       []cli.WorkspaceWithSessions
	scanResult     *cli.ScanResult
	scanErr        error
	spawnResults   []cli.SpawnResult
	spawnErr       error
	disposeErr     error
	getConfigErr   error
	getSessionsErr error
}

func (m *MockDaemonClient) IsRunning() bool {
	return m.isRunning
}

func (m *MockDaemonClient) GetConfig() (*cli.Config, error) {
	return m.config, m.getConfigErr
}

func (m *MockDaemonClient) GetWorkspaces() ([]cli.Workspace, error) {
	return m.workspaces, nil
}

func (m *MockDaemonClient) GetSessions() ([]cli.WorkspaceWithSessions, error) {
	return m.sessions, m.getSessionsErr
}

func (m *MockDaemonClient) ScanWorkspaces(ctx context.Context) (*cli.ScanResult, error) {
	return m.scanResult, m.scanErr
}

func (m *MockDaemonClient) Spawn(ctx context.Context, req cli.SpawnRequest) ([]cli.SpawnResult, error) {
	if m.spawnErr != nil {
		return nil, m.spawnErr
	}
	if m.spawnResults != nil {
		return m.spawnResults, nil
	}
	return []cli.SpawnResult{
		{
			SessionID:   "test-session-123",
			WorkspaceID: "test-workspace-001",
			Agent:       "test-agent",
		},
	}, nil
}

func (m *MockDaemonClient) DisposeSession(ctx context.Context, sessionID string) error {
	return m.disposeErr
}
