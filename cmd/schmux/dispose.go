package main

import (
	"context"
	"fmt"

	"github.com/sergeknystautas/schmux/pkg/cli"
)

// DisposeCommand implements the dispose command.
type DisposeCommand struct {
	client cli.DaemonClient
}

// NewDisposeCommand creates a new dispose command.
func NewDisposeCommand(client cli.DaemonClient) *DisposeCommand {
	return &DisposeCommand{client: client}
}

// Run executes the dispose command.
func (cmd *DisposeCommand) Run(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: schmux dispose <session-id>")
	}

	sessionID := args[0]

	// Check if daemon is running
	if !cmd.client.IsRunning() {
		return fmt.Errorf("daemon is not running. Start it with: schmux start")
	}

	// Verify session exists
	sessions, err := cmd.client.GetSessions()
	if err != nil {
		return fmt.Errorf("failed to get sessions: %w", err)
	}

	var found bool
	for _, ws := range sessions {
		for _, sess := range ws.Sessions {
			if sess.ID == sessionID {
				found = true
				break
			}
		}
	}

	if !found {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	// Confirm disposal
	fmt.Printf("Dispose session %s? [y/N] ", sessionID)
	var response string
	fmt.Scanln(&response)
	if response != "y" && response != "Y" {
		fmt.Println("Cancelled.")
		return nil
	}

	// Dispose the session
	if err := cmd.client.DisposeSession(context.Background(), sessionID); err != nil {
		return fmt.Errorf("failed to dispose session: %w", err)
	}

	fmt.Printf("Session %s disposed.\n", sessionID)
	return nil
}
