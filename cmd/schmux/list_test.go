package main

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/pkg/cli"
)

func TestListParseJsonFlag(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantErr   bool
		wantJSON  bool
		isRunning bool
		sessions  []cli.WorkspaceWithSessions
	}{
		{
			name:      "no args outputs human format",
			args:      []string{},
			isRunning: true,
			sessions:  []cli.WorkspaceWithSessions{},
		},
		{
			name:      "json flag produces JSON output",
			args:      []string{"--json"},
			isRunning: true,
			wantJSON:  true,
			sessions: []cli.WorkspaceWithSessions{
				{ID: "ws-001", Branch: "main", Sessions: []cli.Session{{ID: "ws-001-abc", Target: "claude"}}},
			},
		},
		{
			name:      "short json flag produces JSON output",
			args:      []string{"-json"},
			isRunning: true,
			wantJSON:  true,
			sessions: []cli.WorkspaceWithSessions{
				{ID: "ws-001", Branch: "main", Sessions: []cli.Session{{ID: "ws-001-abc", Target: "claude"}}},
			},
		},
		{
			name:      "json flag after target still produces JSON",
			args:      []string{"sessions", "--json"},
			isRunning: true,
			wantJSON:  true,
			sessions: []cli.WorkspaceWithSessions{
				{ID: "ws-001", Branch: "main", Sessions: []cli.Session{{ID: "ws-001-abc", Target: "claude"}}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &MockDaemonClient{
				isRunning: tt.isRunning,
				sessions:  tt.sessions,
			}

			cmd := NewListCommand(mock)

			// Capture stdout
			oldStdout := os.Stdout
			r, w, _ := os.Pipe()
			os.Stdout = w

			err := cmd.Run(tt.args)

			w.Close()
			out, _ := io.ReadAll(r)
			os.Stdout = oldStdout

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			output := string(out)
			if tt.wantJSON {
				// JSON output should be valid JSON
				var parsed interface{}
				if jsonErr := json.Unmarshal([]byte(output), &parsed); jsonErr != nil {
					t.Errorf("expected valid JSON output, got parse error: %v\noutput: %s", jsonErr, output)
				}
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

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := cmd.outputHuman(sessions)

	w.Close()
	out, _ := io.ReadAll(r)
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("outputHuman() error = %v", err)
	}

	output := string(out)

	// Verify workspace headers appear
	if !strings.Contains(output, "schmux-001") {
		t.Error("output should contain workspace ID schmux-001")
	}
	if !strings.Contains(output, "main") {
		t.Error("output should contain branch name")
	}
	if !strings.Contains(output, "dirty") {
		t.Error("output should show dirty status for schmux-001")
	}
	if !strings.Contains(output, "ahead 3") {
		t.Error("output should show ahead count for schmux-002")
	}

	// Verify session rows with nickname and status
	if !strings.Contains(output, "reviewer") {
		t.Error("output should contain session nickname 'reviewer'")
	}
	if !strings.Contains(output, "running") {
		t.Error("output should contain running status")
	}
	if !strings.Contains(output, "stopped") {
		t.Error("output should contain stopped status")
	}

	// Workspace with no sessions should not appear
	if strings.Contains(output, "schmux-003") {
		t.Error("output should not contain workspace with no sessions")
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
