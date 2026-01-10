package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/sergek/schmux/pkg/cli"
)

// AttachCommand implements the attach command.
type AttachCommand struct {
	client cli.DaemonClient
}

// NewAttachCommand creates a new attach command.
func NewAttachCommand(client cli.DaemonClient) *AttachCommand {
	return &AttachCommand{client: client}
}

// Run executes the attach command.
func (cmd *AttachCommand) Run(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: schmux attach <session-id>")
	}

	sessionID := args[0]

	// Check if daemon is running
	if !cmd.client.IsRunning() {
		return fmt.Errorf("daemon is not running. Start it with: schmux start")
	}

	// Get sessions to find the tmux session name
	sessions, err := cmd.client.GetSessions()
	if err != nil {
		return fmt.Errorf("failed to get sessions: %w", err)
	}

	// Find the session and get its tmux session name
	var tmuxSession string
	for _, ws := range sessions {
		for _, sess := range ws.Sessions {
			if sess.ID == sessionID {
				// Parse attach command to get tmux session name
				// Attach command is: tmux attach -t "<session-name>" or tmux attach -t <session-name>
				tmuxSession = parseTmuxSession(sess.AttachCmd)
				if tmuxSession == "" {
					// Fallback: couldn't parse, try session ID
					tmuxSession = sessionID
				}
				goto found
			}
		}
	}

found:
	if tmuxSession == "" {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	// Execute tmux attach
	tmuxCmd := exec.Command("tmux", "attach", "-t", tmuxSession)
	tmuxCmd.Stdin = os.Stdin
	tmuxCmd.Stdout = os.Stdout
	tmuxCmd.Stderr = os.Stderr

	return tmuxCmd.Run()
}

// parseTmuxSession extracts the tmux session name from an attach command.
// Handles both quoted and unquoted session names.
// Examples:
//
//	tmux attach -t "my session" -> my session
//	tmux attach -t my-session -> my-session
func parseTmuxSession(cmd string) string {
	// Find the "-t" flag
	idx := strings.Index(cmd, "-t")
	if idx == -1 {
		return ""
	}

	// Get everything after "-t"
	rest := strings.TrimSpace(cmd[idx+2:])
	if rest == "" {
		return ""
	}

	// If it starts with a quote, extract the quoted content
	if rest[0] == '"' || rest[0] == '\'' {
		quote := rune(rest[0])
		rest = rest[1:]
		endQuote := strings.IndexRune(rest, quote)
		if endQuote == -1 {
			// Unclosed quote, return rest
			return rest
		}
		return rest[:endQuote]
	}

	// Otherwise, take the first word (up to space or end)
	if idx := strings.IndexAny(rest, " \t\n"); idx != -1 {
		return rest[:idx]
	}

	return rest
}
