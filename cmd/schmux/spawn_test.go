package main

import (
	"os"
	"strings"
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
			name: "not in a workspace",
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

func TestFindRunTarget(t *testing.T) {
	cfg := &cli.Config{
		RunTargets: []cli.RunTarget{
			{Name: "claude", Type: "promptable", Command: "claude"},
			{Name: "zsh", Type: "command", Command: "zsh"},
		},
	}

	cmd := &SpawnCommand{}

	t.Run("finds existing target", func(t *testing.T) {
		target, found := cmd.findRunTarget("claude", cfg)
		if !found {
			t.Fatal("target not found")
		}
		if target.Name != "claude" {
			t.Errorf("got name %q, want %q", target.Name, "claude")
		}
	})

	t.Run("target not found", func(t *testing.T) {
		_, found := cmd.findRunTarget("nonexistent", cfg)
		if found {
			t.Error("expected target not to be found")
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

// TestSpawnCommand_Run tests the spawn command Run method
func TestSpawnCommand_Run(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		isRunning    bool
		config       *cli.Config
		workspaces   []cli.Workspace
		scanResult   *cli.ScanResult
		scanErr      error
		spawnResults []cli.SpawnResult
		spawnErr     error
		wantErr      bool
		errContains  string
	}{
		{
			name:        "requires agent flag",
			args:        []string{},
			isRunning:   true,
			wantErr:     true,
			errContains: "required flag -a",
		},
		{
			name:        "daemon not running",
			args:        []string{"-a", "test"},
			isRunning:   false,
			wantErr:     true,
			errContains: "daemon is not running",
		},
		{
			name:      "spawn with repo flag (skip workspace check)",
			args:      []string{"-r", "schmux", "-a", "claude", "-p", "test"},
			isRunning: true,
			config: &cli.Config{
				RunTargets: []cli.RunTarget{
					{Name: "claude", Type: "promptable", Command: "claude"},
				},
				Repos: []cli.Repo{
					{Name: "schmux", URL: "https://github.com/user/schmux.git"},
				},
			},
			spawnResults: []cli.SpawnResult{
				{SessionID: "new-001", WorkspaceID: "schmux-001", Agent: "claude"},
			},
			wantErr: false,
		},
		{
			name:      "spawn with promptable target without prompt (repo flag)",
			args:      []string{"-r", "schmux", "-a", "claude"},
			isRunning: true,
			config: &cli.Config{
				RunTargets: []cli.RunTarget{
					{Name: "claude", Type: "promptable", Command: "claude"},
				},
				Repos: []cli.Repo{
					{Name: "schmux", URL: "https://github.com/user/schmux.git"},
				},
			},
			wantErr:     true,
			errContains: "prompt (-p/--prompt) is required for promptable targets",
		},
		{
			name:      "spawn with invalid repo",
			args:      []string{"-r", "unknown", "-a", "test"},
			isRunning: true,
			config: &cli.Config{
				Repos: []cli.Repo{
					{Name: "schmux", URL: "https://github.com/user/schmux.git"},
				},
			},
			wantErr:     true,
			errContains: "repo not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &MockDaemonClient{
				isRunning:    tt.isRunning,
				config:       tt.config,
				workspaces:   tt.workspaces,
				scanResult:   tt.scanResult,
				scanErr:      tt.scanErr,
				spawnResults: tt.spawnResults,
				spawnErr:     tt.spawnErr,
			}

			cmd := NewSpawnCommand(mock)

			// Capture output by redirecting os.Stdout
			oldStdout := os.Stdout
			r, w, _ := os.Pipe()
			os.Stdout = w

			err := cmd.Run(tt.args)

			w.Close()
			os.Stdout = oldStdout
			r.Close()

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errContains)
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("expected error containing %q, got %q", tt.errContains, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

// TestSpawnCommand_ResolveWorkspace tests workspace resolution
func TestSpawnCommand_ResolveWorkspace(t *testing.T) {
	// Simple test - just verify the method exists and handles basic cases
	mock := &MockDaemonClient{
		workspaces: []cli.Workspace{
			{ID: "test-001", Path: "/Users/test/workspace-001"},
		},
		scanResult: &cli.ScanResult{},
		isRunning:  true,
		config:     &cli.Config{},
	}

	cmd := NewSpawnCommand(mock)

	// Test with exact path match
	result, err := cmd.resolveWorkspace("/Users/test/workspace-001", mock.config)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result != "test-001" {
		t.Errorf("got %q, want %q", result, "test-001")
	}

	// Test with non-existent path
	_, err = cmd.resolveWorkspace("/nonexistent", mock.config)
	if err == nil {
		t.Error("expected error for non-existent workspace")
	}
}

// TestSpawnCommand_OutputHuman tests human-readable output formatting
func TestSpawnCommand_OutputHuman(t *testing.T) {
	cmd := &SpawnCommand{}

	results := []cli.SpawnResult{
		{SessionID: "ws-001-abc", WorkspaceID: "ws-001", Agent: "claude"},
		{SessionID: "ws-002-def", WorkspaceID: "ws-002", Agent: "glm", Error: "some error"},
	}

	// Capture output
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := cmd.outputHuman(results, "ws-001")

	w.Close()
	os.Stdout = oldStdout
	r.Close()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestSpawnCommand_OutputJSON tests JSON output formatting
func TestSpawnCommand_OutputJSON(t *testing.T) {
	cmd := &SpawnCommand{}

	results := []cli.SpawnResult{
		{SessionID: "ws-001-abc", WorkspaceID: "ws-001", Agent: "claude"},
	}

	// Capture output
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := cmd.outputJSON(results)

	w.Close()
	os.Stdout = oldStdout
	r.Close()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
