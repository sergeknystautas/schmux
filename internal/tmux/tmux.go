package tmux

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// ANSI escape sequence regex for stripping terminal codes.
// Compiled once at package initialization for efficiency.
var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\][^\x07\x1b]*\x07|\x1b\][^\x07\x1b]*\x1b\\`)

// CreateSession creates a new tmux session with the given name, directory, and command.
func CreateSession(ctx context.Context, name, dir, command string) error {
	return CreateSessionWithEnv(ctx, name, dir, command, nil)
}

// CreateSessionWithEnv creates a new tmux session with environment variables.
// The env slice should be in the format ["KEY=value", ...].
// If env is nil or empty, behaves like CreateSession.
func CreateSessionWithEnv(ctx context.Context, name, dir, command string, env []string) error {
	// tmux new-session -d -s <name> -c <dir> <command>
	args := []string{
		"new-session",
		"-d",       // detached
		"-s", name, // session name
		"-c", dir,  // working directory
		command,    // command to run
	}

	cmd := exec.CommandContext(ctx, "tmux", args...)
	if env != nil {
		cmd.Env = append(os.Environ(), env...)
	}
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create tmux session: %w: %s", err, string(output))
	}

	return nil
}

// SessionExists checks if a tmux session with the given name exists.
func SessionExists(ctx context.Context, name string) bool {
	// tmux has-session -t <name>
	args := []string{"has-session", "-t", name}

	cmd := exec.CommandContext(ctx, "tmux", args...)
	err := cmd.Run()
	return err == nil
}

// GetPanePID returns the PID of the first process in the tmux session's pane.
func GetPanePID(ctx context.Context, name string) (int, error) {
	// tmux display-message -p -t <name> "#{pane_pid}"
	args := []string{
		"display-message",
		"-p",       // output to stdout
		"-t", name, // target session
		"#{pane_pid}",
	}

	cmd := exec.CommandContext(ctx, "tmux", args...)
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
func CaptureOutput(ctx context.Context, name string) (string, error) {
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

	cmd := exec.CommandContext(ctx, "tmux", args...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to capture tmux output: %w", err)
	}

	return stdout.String(), nil
}

// CaptureLastLines captures the last N lines of the pane, including escape sequences.
func CaptureLastLines(ctx context.Context, name string, lines int) (string, error) {
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

	cmd := exec.CommandContext(ctx, "tmux", args...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to capture tmux output: %w", err)
	}

	return stdout.String(), nil
}

// KillSession kills a tmux session.
func KillSession(ctx context.Context, name string) error {
	// tmux kill-session -t <name>
	args := []string{"kill-session", "-t", name}

	cmd := exec.CommandContext(ctx, "tmux", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to kill tmux session: %w: %s", err, string(output))
	}

	return nil
}

// ListSessions returns a list of all tmux session names.
func ListSessions(ctx context.Context) ([]string, error) {
	// tmux list-sessions -F "#{session_name}"
	args := []string{"list-sessions", "-F", "#{session_name}"}

	cmd := exec.CommandContext(ctx, "tmux", args...)
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
func SendKeys(ctx context.Context, name, keys string) error {
	// tmux send-keys -t <name> <keys>
	args := []string{"send-keys", "-t", name, keys}

	cmd := exec.CommandContext(ctx, "tmux", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to send keys to tmux session: %w: %s", err, strings.TrimSpace(string(output)))
	}

	return nil
}

// SendLiteral sends literal text to a tmux session (spaces/newlines are treated as text).
func SendLiteral(ctx context.Context, name, text string) error {
	// tmux send-keys -l -t <name> <text>
	args := []string{"send-keys", "-l", "-t", name, text}

	cmd := exec.CommandContext(ctx, "tmux", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to send literal text to tmux session: %w: %s", err, strings.TrimSpace(string(output)))
	}

	return nil
}

// GetAttachCommand returns the command to attach to a tmux session.
func GetAttachCommand(name string) string {
	return fmt.Sprintf("tmux attach -t \"%s\"", name)
}

// StripAnsi removes ANSI escape sequences from text.
func StripAnsi(text string) string {
	return ansiRegex.ReplaceAllString(text, "")
}

// SetWindowSizeManual forces tmux to ignore client resize requests.
func SetWindowSizeManual(ctx context.Context, sessionName string) error {
	args := []string{"set-option", "-t", sessionName, "window-size", "manual"}
	cmd := exec.CommandContext(ctx, "tmux", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to set window-size manual: %w: %s", err, string(output))
	}
	return nil
}

// ResizeWindow resizes the window to fixed dimensions (80x24 for deterministic TUI).
func ResizeWindow(ctx context.Context, sessionName string, width, height int) error {
	args := []string{
		"resize-window",
		"-t", fmt.Sprintf("%s:0.0", sessionName),
		"-x", strconv.Itoa(width),
		"-y", strconv.Itoa(height),
	}
	cmd := exec.CommandContext(ctx, "tmux", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to resize window: %w: %s", err, string(output))
	}
	return nil
}

// StartPipePane begins streaming pane output to a log file.
func StartPipePane(ctx context.Context, sessionName, logPath string) error {
	// Escape single quotes in logPath for shell safety: replace ' with '"'"'
	escapedPath := strings.ReplaceAll(logPath, "'", "'\"'\"'")
	args := []string{
		"pipe-pane",
		"-o", // only output, not input
		"-t", fmt.Sprintf("%s:0.0", sessionName),
		fmt.Sprintf("cat >> '%s'", escapedPath),
	}
	cmd := exec.CommandContext(ctx, "tmux", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to start pipe-pane: %w: %s", err, string(output))
	}
	return nil
}

// StopPipePane stops streaming pane output.
func StopPipePane(ctx context.Context, sessionName string) error {
	args := []string{"pipe-pane", "-t", fmt.Sprintf("%s:0.0", sessionName), ""}
	cmd := exec.CommandContext(ctx, "tmux", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to stop pipe-pane: %w: %s", err, string(output))
	}
	return nil
}

// IsPipePaneActive checks if pipe-pane is running for a session.
func IsPipePaneActive(ctx context.Context, sessionName string) bool {
	args := []string{
		"display-message", "-p", "-t",
		fmt.Sprintf("%s:0.0", sessionName),
		"#{pane_pipe}",
	}
	cmd := exec.CommandContext(ctx, "tmux", args...)
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
func RenameSession(ctx context.Context, oldName, newName string) error {
	args := []string{"rename-session", "-t", oldName, newName}
	cmd := exec.CommandContext(ctx, "tmux", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to rename tmux session: %w: %s", err, string(output))
	}
	return nil
}

// GetCursorPosition returns the cursor position (x, y) for a session.
// Coordinates are 0-indexed.
func GetCursorPosition(ctx context.Context, sessionName string) (x, y int, err error) {
	args := []string{
		"display-message", "-p", "-t", sessionName,
		"#{cursor_x}", "#{cursor_y}",
	}
	cmd := exec.CommandContext(ctx, "tmux", args...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return 0, 0, fmt.Errorf("failed to get cursor position: %w", err)
	}

	// Parse output: "x y" on two lines
	parts := strings.Split(strings.TrimSpace(stdout.String()), " ")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("unexpected cursor position format: %q", stdout.String())
	}

	_, err = fmt.Sscanf(parts[0], "%d", &x)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to parse cursor_x: %w", err)
	}
	_, err = fmt.Sscanf(parts[1], "%d", &y)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to parse cursor_y: %w", err)
	}

	return x, y, nil
}
