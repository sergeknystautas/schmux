package main

import (
	"context"
	"fmt"

	"github.com/sergeknystautas/schmux/pkg/cli"
)

// RefreshOverlayCommand implements the refresh-overlay command.
type RefreshOverlayCommand struct {
	client cli.DaemonClient
}

// NewRefreshOverlayCommand creates a new refresh-overlay command.
func NewRefreshOverlayCommand(client cli.DaemonClient) *RefreshOverlayCommand {
	return &RefreshOverlayCommand{client: client}
}

// Run executes the refresh-overlay command.
func (cmd *RefreshOverlayCommand) Run(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: schmux refresh-overlay <workspace-id>")
	}

	workspaceID := args[0]

	// Check if daemon is running
	if !cmd.client.IsRunning() {
		return fmt.Errorf("daemon is not running. Start it with: schmux start")
	}

	// Refresh the overlay (server will validate workspace exists)
	fmt.Printf("Refreshing overlay for workspace %s...\n", workspaceID)
	if err := cmd.client.RefreshOverlay(context.Background(), workspaceID); err != nil {
		return fmt.Errorf("failed to refresh overlay: %w", err)
	}

	fmt.Printf("Overlay refreshed successfully for workspace %s.\n", workspaceID)
	return nil
}
