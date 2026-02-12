package main

import (
	"context"
	"fmt"

	"github.com/sergeknystautas/schmux/pkg/cli"
)

// MockDaemonClient is a mock implementation of DaemonClient for testing.
type MockDaemonClient struct {
	isRunning         bool
	config            *cli.Config
	workspaces        []cli.Workspace
	sessions          []cli.WorkspaceWithSessions
	scanResult        *cli.ScanResult
	scanErr           error
	spawnResults      []cli.SpawnResult
	spawnErr          error
	disposeErr        error
	getConfigErr      error
	getSessionsErr    error
	refreshOverlayErr error
	analyzeRepoResp   *cli.AnalyzeRepoResponse
	analyzeRepoErr    error
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
			Target:      "test-agent",
		},
	}, nil
}

func (m *MockDaemonClient) DisposeSession(ctx context.Context, sessionID string) error {
	return m.disposeErr
}

func (m *MockDaemonClient) RefreshOverlay(ctx context.Context, workspaceID string) error {
	// Validate workspace ID is not empty
	if workspaceID == "" {
		return fmt.Errorf("workspace ID cannot be empty")
	}

	// Return configured error if set
	if m.refreshOverlayErr != nil {
		return m.refreshOverlayErr
	}

	// Check if workspace exists in our mock data
	for _, ws := range m.workspaces {
		if ws.ID == workspaceID {
			return nil // Success - workspace found
		}
	}

	// Workspace not found
	return fmt.Errorf("workspace not found: %s", workspaceID)
}

func (m *MockDaemonClient) AnalyzeRepo(ctx context.Context, repoName string, depth int, output string) (*cli.AnalyzeRepoResponse, error) {
	if m.analyzeRepoErr != nil {
		return nil, m.analyzeRepoErr
	}
	if m.analyzeRepoResp != nil {
		return m.analyzeRepoResp, nil
	}
	return &cli.AnalyzeRepoResponse{
		RepoName: repoName,
		RepoPath: "/tmp/workspace",
		Output:   "/tmp/workspace/repo-index.json",
	}, nil
}
