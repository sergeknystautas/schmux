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
	// Check for --clear flag
	isClear := false
	var messageArgs []string
	for _, arg := range args {
		if arg == "--clear" {
			isClear = true
		} else {
			messageArgs = append(messageArgs, arg)
		}
	}

	if isClear && len(messageArgs) > 0 {
		return fmt.Errorf("cannot use --clear with a message")
	}

	if !isClear && len(messageArgs) < 1 {
		return fmt.Errorf("usage: schmux escalate <message>\n       schmux escalate --clear")
	}

	if !cmd.client.IsRunning() {
		return fmt.Errorf("daemon is not running. Start it with: schmux start")
	}

	if isClear {
		if err := cmd.client.ClearEscalation(context.Background()); err != nil {
			return fmt.Errorf("failed to clear escalation: %w", err)
		}
		fmt.Println("Escalation cleared.")
		return nil
	}

	message := strings.Join(messageArgs, " ")


	if err := cmd.client.Escalate(context.Background(), message); err != nil {
		return fmt.Errorf("failed to escalate: %w", err)
	}

	fmt.Println("Escalation sent.")
	return nil
}
