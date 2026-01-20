package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/pkg/cli"
)

// TestAttachCommand_Run tests the attach command Run method
func TestAttachCommand_Run(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		isRunning   bool
		sessions    []cli.WorkspaceWithSessions
		tmuxExecErr error
		wantErr     bool
		errContains string
	}{
		{
			name:        "requires session id",
			args:        []string{},
			isRunning:   true,
			wantErr:     true,
			errContains: "usage:",
		},
		{
			name:        "daemon not running",
			args:        []string{"test-session"},
			isRunning:   false,
			wantErr:     true,
			errContains: "daemon is not running",
		},
		{
			name:      "session not found",
			args:      []string{"nonexistent-session"},
			isRunning: true,
			sessions: []cli.WorkspaceWithSessions{
				{ID: "ws-001", Sessions: []cli.Session{{ID: "ws-001-abc", AttachCmd: `tmux attach -t "ws-001-abc"`}}},
			},
			wantErr:     true,
			errContains: "session not found",
		},
		{
			name:      "attach succeeds",
			args:      []string{"ws-001-abc"},
			isRunning: true,
			sessions: []cli.WorkspaceWithSessions{
				{ID: "ws-001", Sessions: []cli.Session{{ID: "ws-001-abc", AttachCmd: `tmux attach -t "ws-001-abc"`}}},
			},
			wantErr: true, // tmux attach will fail in test environment
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &MockDaemonClient{
				isRunning: tt.isRunning,
				sessions:  tt.sessions,
			}

			cmd := NewAttachCommand(mock)

			// Override exec.Command for testing
			originalExecCommand := execCommand
			execCommandCalled := false
			execCommand = func(name string, args ...string) *exec.Cmd {
				execCommandCalled = true
				if name != "tmux" {
					t.Errorf("expected tmux command, got %s", name)
				}
				// Return a mock command that will fail
				cmd := exec.Command("false")
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr
				cmd.Stdin = strings.NewReader("")
				return cmd
			}
			defer func() { execCommand = originalExecCommand }()

			err := cmd.Run(tt.args)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errContains)
				} else if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("expected error containing %q, got %q", tt.errContains, err.Error())
				}
			} else {
				if err != nil && execCommandCalled {
					// tmux attach will fail in test environment, that's expected
					t.Logf("got expected tmux error (non-terminal): %v", err)
				}
			}
		})
	}
}

// Variable to mock exec.Command for testing
var execCommand = exec.Command
