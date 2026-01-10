package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/sergek/schmux/pkg/cli"
)

// ListCommand implements the list command.
type ListCommand struct {
	client cli.DaemonClient
}

// NewListCommand creates a new list command.
func NewListCommand(client cli.DaemonClient) *ListCommand {
	return &ListCommand{client: client}
}

// Run executes the list command.
func (cmd *ListCommand) Run(args []string) error {
	var (
		jsonOutput bool
	)

	// Manually parse -json/--json flag (can appear anywhere)
	for _, arg := range args {
		if arg == "-json" || arg == "--json" {
			jsonOutput = true
		}
	}

	// Check if daemon is running
	if !cmd.client.IsRunning() {
		return fmt.Errorf("daemon is not running. Start it with: schmux start")
	}

	// Get sessions (grouped by workspace)
	sessions, err := cmd.client.GetSessions()
	if err != nil {
		return fmt.Errorf("failed to get sessions: %w", err)
	}

	if jsonOutput {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(sessions)
	}

	return cmd.outputHuman(sessions)
}

// outputHuman outputs sessions in human-readable format.
func (cmd *ListCommand) outputHuman(sessions []cli.WorkspaceWithSessions) error {
	if len(sessions) == 0 {
		fmt.Println("No sessions found.")
		return nil
	}

	fmt.Println("Sessions:")
	fmt.Println()

	for _, ws := range sessions {
		if len(ws.Sessions) == 0 {
			continue
		}

		// Workspace header with git status
		gitStatus := ""
		if ws.GitDirty {
			gitStatus = " [dirty]"
		}
		if ws.GitAhead > 0 || ws.GitBehind > 0 {
			if ws.GitAhead > 0 {
				gitStatus += fmt.Sprintf(" [ahead %d]", ws.GitAhead)
			}
			if ws.GitBehind > 0 {
				gitStatus += fmt.Sprintf(" [behind %d]", ws.GitBehind)
			}
		}
		fmt.Printf("%s (%s)%s\n", ws.ID, ws.Branch, gitStatus)

		// Sessions
		for _, sess := range ws.Sessions {
			status := "stopped"
			if sess.Running {
				status = "running"
			}
			name := sess.Agent
			if sess.Nickname != "" {
				name = sess.Nickname
			}
			fmt.Printf("  [%s] %s - %s\n", sess.ID, name, status)
		}
		fmt.Println()
	}

	return nil
}
