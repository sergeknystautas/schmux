package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/sergeknystautas/schmux/pkg/cli"
)

// EscalateCommand implements the escalate command.
type EscalateCommand struct {
	client *cli.Client
}

// NewEscalateCommand creates a new escalate command.
func NewEscalateCommand(client *cli.Client) *EscalateCommand {
	return &EscalateCommand{client: client}
}

// Run executes the escalate command.
func (cmd *EscalateCommand) Run(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: schmux escalate <message>")
	}

	message := strings.Join(args, " ")

	if !cmd.client.IsRunning() {
		return fmt.Errorf("daemon is not running. Start it with: schmux start")
	}

	if err := cmd.client.Escalate(context.Background(), message); err != nil {
		return fmt.Errorf("failed to escalate: %w", err)
	}

	fmt.Println("Escalation sent.")
	return nil
}
