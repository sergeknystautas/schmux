package tmux

import (
	"bytes"
	"fmt"
	"os/exec"
	"strconv"
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
	// tmux capture-pane -e -p -S - -t <name>
	// -e includes escape sequences for colors/attributes
	// -p outputs to stdout
	// -S - captures from the start of the scrollback buffer
	args := []string{
		"capture-pane",
		"-e",      // include escape sequences
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

// CaptureLastLines captures the last N lines of the pane, including escape sequences.
func CaptureLastLines(name string, lines int) (string, error) {
	if lines <= 0 {
		return "", fmt.Errorf("invalid line count: %d", lines)
	}
	args := []string{
		"capture-pane",
		"-e", // include escape sequences
		"-p", // output to stdout
		"-S", fmt.Sprintf("-%d", lines),
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
	return fmt.Sprintf("tmux attach -t \"%s\"", name)
}

// SetWindowSizeManual forces tmux to ignore client resize requests.
func SetWindowSizeManual(sessionName string) error {
	args := []string{"set-option", "-t", sessionName, "window-size", "manual"}
	cmd := exec.Command("tmux", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to set window-size manual: %w: %s", err, string(output))
	}
	return nil
}

// ResizeWindow resizes the window to fixed dimensions (80x24 for deterministic TUI).
func ResizeWindow(sessionName string, width, height int) error {
	args := []string{
		"resize-window",
		"-t", fmt.Sprintf("%s:0.0", sessionName),
		"-x", strconv.Itoa(width),
		"-y", strconv.Itoa(height),
	}
	cmd := exec.Command("tmux", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to resize window: %w: %s", err, string(output))
	}
	return nil
}

// StartPipePane begins streaming pane output to a log file.
func StartPipePane(sessionName, logPath string) error {
	args := []string{
		"pipe-pane",
		"-o", // only output, not input
		"-t", fmt.Sprintf("%s:0.0", sessionName),
		fmt.Sprintf("cat >> '%s'", logPath),
	}
	cmd := exec.Command("tmux", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to start pipe-pane: %w: %s", err, string(output))
	}
	return nil
}

// StopPipePane stops streaming pane output.
func StopPipePane(sessionName string) error {
	args := []string{"pipe-pane", "-t", fmt.Sprintf("%s:0.0", sessionName), ""}
	cmd := exec.Command("tmux", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to stop pipe-pane: %w: %s", err, string(output))
	}
	return nil
}

// IsPipePaneActive checks if pipe-pane is running for a session.
func IsPipePaneActive(sessionName string) bool {
	args := []string{
		"display-message", "-p", "-t",
		fmt.Sprintf("%s:0.0", sessionName),
		"#{pane_pipe}",
	}
	cmd := exec.Command("tmux", args...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return false
	}
	output := strings.TrimSpace(stdout.String())
	return output != "" && output != "0"
}

// RenameSession renames an existing tmux session.
// This is used when updating session nicknames.
func RenameSession(oldName, newName string) error {
	args := []string{"rename-session", "-t", oldName, newName}
	cmd := exec.Command("tmux", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to rename tmux session: %w: %s", err, string(output))
	}
	return nil
}
