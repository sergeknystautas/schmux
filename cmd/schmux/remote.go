package main

import (
	"fmt"

	"github.com/sergeknystautas/schmux/pkg/cli"
)

// RemoteCommand implements the remote command.
type RemoteCommand struct {
	client *cli.Client
}

// NewRemoteCommand creates a new remote command.
func NewRemoteCommand(client *cli.Client) *RemoteCommand {
	return &RemoteCommand{client: client}
}

// Run executes the remote command.
func (cmd *RemoteCommand) Run(args []string) error {
	if len(args) < 1 {
		fmt.Println("Usage: schmux remote <on|off|status>")
		return nil
	}

	switch args[0] {
	case "on":
		if err := cmd.client.RemoteAccessOn(); err != nil {
			return fmt.Errorf("failed to start remote access: %w", err)
		}
		fmt.Println("Remote access starting... URL will be shown on dashboard and sent via notification")

	case "off":
		if err := cmd.client.RemoteAccessOff(); err != nil {
			return fmt.Errorf("failed to stop remote access: %w", err)
		}
		fmt.Println("Remote access stopped")

	case "status":
		status, err := cmd.client.RemoteAccessStatus()
		if err != nil {
			return fmt.Errorf("failed to get status: %w", err)
		}
		switch status.State {
		case "off":
			fmt.Println("Remote access: off")
		case "starting":
			fmt.Println("Remote access: starting...")
		case "connected":
			fmt.Printf("Remote access: connected\n")
			fmt.Printf("URL: %s\n", status.URL)
		case "error":
			fmt.Printf("Remote access: error\n")
			if status.Error != "" {
				fmt.Printf("Error: %s\n", status.Error)
			}
		}

	default:
		return fmt.Errorf("unknown subcommand: %s (use on, off, or status)", args[0])
	}

	return nil
}
