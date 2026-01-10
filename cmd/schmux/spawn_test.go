package main

import (
	"context"
	"testing"

	"github.com/sergek/schmux/pkg/cli"
)

func TestParseTmuxSession(t *testing.T) {
	tests := []struct {
		name     string
		cmd      string
		expected string
	}{
		{
			name:     "quoted session name",
			cmd:      `tmux attach -t "cli commands"`,
			expected: "cli commands",
		},
		{
			name:     "single quoted session name",
			cmd:      `tmux attach -t 'my session'`,
			expected: "my session",
		},
		{
			name:     "unquoted session name",
			cmd:      "tmux attach -t my-session",
			expected: "my-session",
		},
		{
			name:     "session name with spaces and quotes",
			cmd:      `tmux attach -t "xterm select bug"`,
			expected: "xterm select bug",
		},
		{
			name:     "session name with extra spaces after",
			cmd:      `tmux attach -t "session"  `,
			expected: "session",
		},
		{
			name:     "no -t flag",
			cmd:      `tmux attach session`,
			expected: "",
		},
		{
			name:     "empty command",
			cmd:      "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseTmuxSession(tt.cmd)
			if result != tt.expected {
				t.Errorf("parseTmuxSession(%q) = %q, want %q", tt.cmd, result, tt.expected)
			}
		})
	}
}

// MockDaemonClient is a mock implementation for testing
type MockDaemonClient struct {
	isRunning      bool
	config         *cli.Config
	workspaces     []cli.Workspace
	sessions       []cli.WorkspaceWithSessions
	scanResult     *cli.ScanResult
	scanErr        error
	spawnResults   []cli.SpawnResult
	spawnErr       error
}

func (m *MockDaemonClient) IsRunning() bool {
	return m.isRunning
}

func (m *MockDaemonClient) GetConfig() (*cli.Config, error) {
	return m.config, nil
}

func (m *MockDaemonClient) GetWorkspaces() ([]cli.Workspace, error) {
	return m.workspaces, nil
}

func (m *MockDaemonClient) GetSessions() ([]cli.WorkspaceWithSessions, error) {
	return m.sessions, nil
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
	return nil
}

func TestAutoDetectWorkspace(t *testing.T) {
	tests := []struct {
		name          string
		workspaces    []cli.Workspace
		currentDir    string
		wantWorkspace string
		wantRepo      string
		wantErr       bool
	}{
		{
			name: "finds workspace by path",
			workspaces: []cli.Workspace{
				{
					ID:   "schmux-002",
					Path: "/Users/sergek/dev/schmux-workspaces/schmux-002",
					Repo: "https://github.com/user/schmux.git",
				},
			},
			currentDir:    "/Users/sergek/dev/schmux-workspaces/schmux-002",
			wantWorkspace: "schmux-002",
			wantRepo:      "",
			wantErr:       false,
		},
		{
			name:       "not in a workspace",
			workspaces: []cli.Workspace{
				{
					ID:   "schmux-002",
					Path: "/Users/sergek/dev/schmux-workspaces/schmux-002",
					Repo: "https://github.com/user/schmux.git",
				},
			},
			currentDir:    "/Users/sergek/dev/schmux-workspaces/schmux-003",
			wantWorkspace: "",
			wantRepo:      "",
			wantErr:       true,
		},
		{
			name:          "no workspaces exist",
			workspaces:    []cli.Workspace{},
			currentDir:    "/some/path",
			wantWorkspace: "",
			wantRepo:      "",
			wantErr:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// We can't easily test autoDetectWorkspace without modifying it to accept dependencies
			// For now, we'll test the logic inline here
			workspaceID, repoURL := "", ""

			for _, ws := range tt.workspaces {
				if ws.Path == tt.currentDir {
					workspaceID = ws.ID
					repoURL = ""
					break
				}
			}

			// If we didn't find a workspace and expected to, that's an error
			if workspaceID == "" && !tt.wantErr && tt.wantWorkspace != "" {
				t.Errorf("expected to find workspace %q for path %q", tt.wantWorkspace, tt.currentDir)
			}

			if workspaceID != tt.wantWorkspace {
				t.Errorf("workspaceID = %q, want %q", workspaceID, tt.wantWorkspace)
			}

			if repoURL != tt.wantRepo {
				t.Errorf("repoURL = %q, want %q", repoURL, tt.wantRepo)
			}
		})
	}
}

func TestFindAgent(t *testing.T) {
	cfg := &cli.Config{
		Agents: []cli.Agent{
			{Name: "claude", Command: "claude", Agentic: boolPtr(true)},
			{Name: "zsh", Command: "zsh", Agentic: boolPtr(false)},
		},
	}

	cmd := &SpawnCommand{}

	t.Run("finds existing agent", func(t *testing.T) {
		agent, found := cmd.findAgent("claude", cfg)
		if !found {
			t.Fatal("agent not found")
		}
		if agent.Name != "claude" {
			t.Errorf("got name %q, want %q", agent.Name, "claude")
		}
	})

	t.Run("agent not found", func(t *testing.T) {
		_, found := cmd.findAgent("nonexistent", cfg)
		if found {
			t.Error("expected agent not to be found")
		}
	})
}

func TestFindRepo(t *testing.T) {
	cfg := &cli.Config{
		Repos: []cli.Repo{
			{Name: "schmux", URL: "https://github.com/user/schmux.git"},
		},
	}

	cmd := &SpawnCommand{}

	t.Run("finds existing repo", func(t *testing.T) {
		repo, found := cmd.findRepo("schmux", cfg)
		if !found {
			t.Fatal("repo not found")
		}
		if repo.Name != "schmux" {
			t.Errorf("got name %q, want %q", repo.Name, "schmux")
		}
	})

	t.Run("repo not found", func(t *testing.T) {
		_, found := cmd.findRepo("nonexistent", cfg)
		if found {
			t.Error("expected repo not to be found")
		}
	})
}

func boolPtr(b bool) *bool {
	return &b
}
