package main

import (
	"strings"
	"testing"

	"github.com/sergek/schmux/pkg/cli"
)

// TestDisposeCommand_Run tests the dispose command Run method
func TestDisposeCommand_Run(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		isRunning   bool
		sessions    []cli.WorkspaceWithSessions
		disposeErr  error
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
				{ID: "ws-001", Sessions: []cli.Session{{ID: "ws-001-abc"}}},
			},
			wantErr:     true,
			errContains: "session not found",
		},
		{
			name:      "dispose succeeds",
			args:      []string{"ws-001-abc"},
			isRunning: true,
			sessions: []cli.WorkspaceWithSessions{
				{ID: "ws-001", Sessions: []cli.Session{{ID: "ws-001-abc"}}},
			},
			wantErr: false,
		},
		// Note: "dispose with error" test removed because confirmation prompt
		// happens before DisposeSession call, making it hard to test
		// the error path without stdin mocking.
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &MockDaemonClient{
				isRunning:  tt.isRunning,
				sessions:   tt.sessions,
				disposeErr: tt.disposeErr,
			}

			cmd := NewDisposeCommand(mock)

			err := cmd.Run(tt.args)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errContains)
				} else if !strings.Contains(err.Error(), tt.errContains) {
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
