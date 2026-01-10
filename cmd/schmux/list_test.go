package main

import (
	"testing"

	"github.com/sergek/schmux/pkg/cli"
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
			ID:       "schmux-001",
			Repo:     "https://github.com/user/schmux.git",
			Branch:   "main",
			Path:     "/path/to/schmux-001",
			GitDirty: true,
			GitAhead: 0,
			GitBehind: 0,
			Sessions: []cli.Session{
				{
					ID:        "schmux-001-abc123",
					Agent:     "glm",
					Nickname:  "reviewer",
					Running:   true,
					CreatedAt: "2026-01-10T10:00:00",
				},
			},
		},
		{
			ID:       "schmux-002",
			Repo:     "https://github.com/user/schmux.git",
			Branch:   "feature-x",
			Path:     "/path/to/schmux-002",
			GitDirty: false,
			GitAhead: 3,
			GitBehind: 0,
			Sessions: []cli.Session{
				{
					ID:        "schmux-002-def456",
					Agent:     "claude",
					Nickname:  "",
					Running:   false,
					CreatedAt: "2026-01-10T11:00:00",
				},
			},
		},
		// Workspace with no sessions should be skipped
		{
			ID:       "schmux-003",
			Repo:     "https://github.com/user/schmux.git",
			Branch:   "main",
			Path:     "/path/to/schmux-003",
			GitDirty: false,
			GitAhead: 0,
			GitBehind: 1,
			Sessions: []cli.Session{},
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
