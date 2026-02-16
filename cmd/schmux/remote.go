package main

import (
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/sergeknystautas/schmux/pkg/cli"
	"golang.org/x/term"
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
		fmt.Println("Usage: schmux remote <on|off|status|set-pin>")
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

	case "set-pin":
		return cmd.runSetPin()

	default:
		return fmt.Errorf("unknown subcommand: %s (use on, off, status, or set-pin)", args[0])
	}

	return nil
}

func (cmd *RemoteCommand) runSetPin() error {
	if !term.IsTerminal(int(syscall.Stdin)) {
		return fmt.Errorf("set-pin requires an interactive terminal")
	}

	fmt.Print("Enter PIN: ")
	pin1, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	if err != nil {
		return fmt.Errorf("failed to read PIN: %w", err)
	}

	pin := strings.TrimSpace(string(pin1))
	if len(pin) < 6 {
		return fmt.Errorf("PIN must be at least 6 characters")
	}

	fmt.Print("Confirm PIN: ")
	pin2, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	if err != nil {
		return fmt.Errorf("failed to read PIN confirmation: %w", err)
	}

	if pin != strings.TrimSpace(string(pin2)) {
		return fmt.Errorf("PINs do not match")
	}

	if err := cmd.client.RemoteAccessSetPin(pin); err != nil {
		return fmt.Errorf("failed to set PIN: %w", err)
	}

	fmt.Fprintln(os.Stderr, "Remote access PIN set successfully")
	return nil
}
