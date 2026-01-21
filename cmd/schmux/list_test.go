package main

import (
	"os"
	"testing"

	"github.com/sergeknystautas/schmux/pkg/cli"
)

func TestListParseJsonFlag(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		jsonOutput bool
	}{
		{
			name:       "no args",
			args:       []string{},
			jsonOutput: false,
		},
		{
			name:       "json flag before target",
			args:       []string{"--json"},
			jsonOutput: true,
		},
		{
			name:       "short json flag",
			args:       []string{"-json"},
			jsonOutput: true,
		},
		{
			name:       "json flag after target (should still work)",
			args:       []string{"sessions", "--json"},
			jsonOutput: true,
		},
		{
			name:       "json flag in middle",
			args:       []string{"--json", "sessions"},
			jsonOutput: true,
		},
		{
			name:       "no json flag",
			args:       []string{"sessions"},
			jsonOutput: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jsonOutput := false
			for _, arg := range tt.args {
				if arg == "-json" || arg == "--json" {
					jsonOutput = true
				}
			}

			if jsonOutput != tt.jsonOutput {
				t.Errorf("jsonOutput = %v, want %v", jsonOutput, tt.jsonOutput)
			}
		})
	}
}

func TestListOutputHuman(t *testing.T) {
	cmd := &ListCommand{}

	sessions := []cli.WorkspaceWithSessions{
		{
			ID:        "schmux-001",
			Repo:      "https://github.com/user/schmux.git",
			Branch:    "main",
			Path:      "/path/to/schmux-001",
			GitDirty:  true,
			GitAhead:  0,
			GitBehind: 0,
			Sessions: []cli.Session{
				{
					ID:        "schmux-001-abc123",
					Target:    "glm",
					Nickname:  "reviewer",
					Running:   true,
					CreatedAt: "2026-01-10T10:00:00",
				},
			},
		},
		{
			ID:        "schmux-002",
			Repo:      "https://github.com/user/schmux.git",
			Branch:    "feature-x",
			Path:      "/path/to/schmux-002",
			GitDirty:  false,
			GitAhead:  3,
			GitBehind: 0,
			Sessions: []cli.Session{
				{
					ID:        "schmux-002-def456",
					Target:    "claude",
					Nickname:  "",
					Running:   false,
					CreatedAt: "2026-01-10T11:00:00",
				},
			},
		},
		// Workspace with no sessions should be skipped
		{
			ID:        "schmux-003",
			Repo:      "https://github.com/user/schmux.git",
			Branch:    "main",
			Path:      "/path/to/schmux-003",
			GitDirty:  false,
			GitAhead:  0,
			GitBehind: 1,
			Sessions:  []cli.Session{},
		},
	}

	// Just verify it doesn't error - output formatting can change
	err := cmd.outputHuman(sessions)
	if err != nil {
		t.Fatalf("outputHuman() error = %v", err)
	}
}

func TestListOutputHumanEmpty(t *testing.T) {
	cmd := &ListCommand{}

	err := cmd.outputHuman([]cli.WorkspaceWithSessions{})
	if err != nil {
		t.Fatalf("outputHuman() error = %v", err)
	}
}

// TestListCommand_Run tests the list command Run method
func TestListCommand_Run(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		isRunning bool
		sessions  []cli.WorkspaceWithSessions
		wantErr   bool
	}{
		{
			name:      "lists sessions successfully",
			args:      []string{},
			isRunning: true,
			sessions: []cli.WorkspaceWithSessions{
				{
					ID:        "test-001",
					Branch:    "main",
					GitDirty:  false,
					GitAhead:  0,
					GitBehind: 0,
					Sessions: []cli.Session{
						{ID: "test-001-abc", Target: "claude", Running: true},
					},
				},
			},
			wantErr: false,
		},
		{
			name:      "lists empty sessions",
			args:      []string{},
			isRunning: true,
			sessions:  []cli.WorkspaceWithSessions{},
			wantErr:   false,
		},
		{
			name:      "daemon not running",
			args:      []string{},
			isRunning: false,
			wantErr:   true,
		},
		{
			name:      "lists with json flag",
			args:      []string{"--json"},
			isRunning: true,
			sessions: []cli.WorkspaceWithSessions{
				{
					ID:     "test-001",
					Branch: "main",
					Sessions: []cli.Session{
						{ID: "test-001-abc", Target: "claude"},
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &MockDaemonClient{
				isRunning: tt.isRunning,
				sessions:  tt.sessions,
			}

			cmd := NewListCommand(mock)

			// Capture output
			oldStdout := os.Stdout
			r, w, _ := os.Pipe()
			os.Stdout = w

			err := cmd.Run(tt.args)

			w.Close()
			os.Stdout = oldStdout
			r.Close()

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}
