package tmux

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// CreateSession creates a new tmux session with the given name, directory, and command.
func CreateSession(name, dir, command string) error {
	// tmux new-session -d -s <name> -c <dir> <command>
	args := []string{
		"new-session",
		"-d",       // detached
		"-s", name, // session name
		"-c", dir, // working directory
		command, // command to run
	}

	cmd := exec.Command("tmux", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create tmux session: %w: %s", err, string(output))
	}

	return nil
}

// SessionExists checks if a tmux session with the given name exists.
func SessionExists(name string) bool {
	// tmux has-session -t <name>
	args := []string{"has-session", "-t", name}

	cmd := exec.Command("tmux", args...)
	err := cmd.Run()
	return err == nil
}

// GetPanePID returns the PID of the first process in the tmux session's pane.
func GetPanePID(name string) (int, error) {
	// tmux display-message -p -t <name> "#{pane_pid}"
	args := []string{
		"display-message",
		"-p",       // output to stdout
		"-t", name, // target session
		"#{pane_pid}",
	}

	cmd := exec.Command("tmux", args...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return 0, fmt.Errorf("failed to get pane PID: %w", err)
	}

	pidStr := strings.TrimSpace(stdout.String())
	var pid int
	if _, err := fmt.Sscanf(pidStr, "%d", &pid); err != nil {
		return 0, fmt.Errorf("failed to parse PID: %w", err)
	}

	return pid, nil
}

// CaptureOutput captures the current output of a tmux session, including full scrollback history.
func CaptureOutput(name string) (string, error) {
	// tmux capture-pane -p -S - -t <name>
	// -S - captures from the start of the scrollback buffer
	args := []string{
		"capture-pane",
		"-p",      // output to stdout
		"-S", "-", // start from beginning of scrollback
		"-t", name, // target session/pane
	}

	cmd := exec.Command("tmux", args...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to capture tmux output: %w", err)
	}

	return stdout.String(), nil
}

// KillSession kills a tmux session.
func KillSession(name string) error {
	// tmux kill-session -t <name>
	args := []string{"kill-session", "-t", name}

	cmd := exec.Command("tmux", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to kill tmux session: %w: %s", err, string(output))
	}

	return nil
}

// ListSessions returns a list of all tmux session names.
func ListSessions() ([]string, error) {
	// tmux list-sessions -F "#{session_name}"
	args := []string{"list-sessions", "-F", "#{session_name}"}

	cmd := exec.Command("tmux", args...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to list tmux sessions: %w", err)
	}

	output := strings.TrimSpace(stdout.String())
	if output == "" {
		return []string{}, nil
	}

	sessions := strings.Split(output, "\n")
	return sessions, nil
}

// SendKeys sends keys to a tmux session (useful for interactive commands).
func SendKeys(name, keys string) error {
	// tmux send-keys -t <name> <keys>
	args := []string{"send-keys", "-t", name, keys}

	cmd := exec.Command("tmux", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to send keys to tmux session: %w", err)
	}

	return nil
}

// GetAttachCommand returns the command to attach to a tmux session.
func GetAttachCommand(name string) string {
	return fmt.Sprintf("tmux attach -t %s", name)
}
