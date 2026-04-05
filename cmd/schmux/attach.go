package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/sergeknystautas/schmux/pkg/cli"
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

	// Find the session
	var found *cli.Session
	for _, ws := range sessions {
		for i := range ws.Sessions {
			if ws.Sessions[i].ID == sessionID {
				found = &ws.Sessions[i]
				break
			}
		}
		if found != nil {
			break
		}
	}

	if found == nil {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	// Use structured fields for safe exec.Command construction (no shell injection).
	tmuxSocket := found.TmuxSocket
	if tmuxSocket == "" {
		tmuxSocket = "schmux" // default for sessions that predate socket configurability
	}
	tmuxSession := found.TmuxSession
	if tmuxSession == "" {
		// Fallback: parse from attach command for backward compat with old daemons
		tmuxSession = parseTmuxSession(found.AttachCmd)
		if tmuxSession == "" {
			tmuxSession = sessionID
		}
	}

	tmuxCmd := exec.Command("tmux", "-L", tmuxSocket, "attach", "-t", "="+tmuxSession)
	tmuxCmd.Stdin = os.Stdin
	tmuxCmd.Stdout = os.Stdout
	tmuxCmd.Stderr = os.Stderr

	return tmuxCmd.Run()
}

// parseTmuxSession extracts the tmux session name from an attach command.
// Handles both quoted and unquoted session names, stripping the "=" exact-match prefix.
// Examples:
//
//	tmux -L schmux attach -t "=my session" -> my session
//	tmux -L schmux attach -t "=my-session" -> my-session
//	tmux attach -t my-session -> my-session (legacy)
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

	var name string

	// If it starts with a quote, extract the quoted content
	if rest[0] == '"' || rest[0] == '\'' {
		quote := rune(rest[0])
		rest = rest[1:]
		endQuote := strings.IndexRune(rest, quote)
		if endQuote == -1 {
			// Unclosed quote, return rest
			name = rest
		} else {
			name = rest[:endQuote]
		}
	} else {
		// Otherwise, take the first word (up to space or end)
		if idx := strings.IndexAny(rest, " \t\n"); idx != -1 {
			name = rest[:idx]
		} else {
			name = rest
		}
	}

	// Strip the "=" exact-match prefix if present
	name = strings.TrimPrefix(name, "=")

	return name
}
