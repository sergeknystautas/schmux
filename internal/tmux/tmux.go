package tmux

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// Checker verifies tmux is available and functional.
type Checker interface {
	Check() error
}

// TmuxChecker is the package-level checker instance.
// NOTE: This global is a known anti-pattern. Tests override it for mocking.
// TODO: Refactor to use dependency injection once daemon structure allows it.
var TmuxChecker Checker = &defaultChecker{}

// NewDefaultChecker returns a new default checker instance.
// Use this in production code that can accept a Checker via dependency injection.
func NewDefaultChecker() Checker {
	return &defaultChecker{}
}

// defaultChecker implements Checker by running tmux -V.
type defaultChecker struct{}

func (c *defaultChecker) Check() error {
	cmd := exec.Command("tmux", "-V")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux is not installed or not accessible.\n-> %w", err)
	}
	// tmux -V outputs version info like "tmux 3.3a", this confirms it's working
	if len(output) == 0 {
		return fmt.Errorf("tmux command produced no output")
	}
	return nil
}

// ANSI escape sequence regex for stripping terminal codes.
// Compiled once at package initialization for efficiency.
var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\][^\x07\x1b]*\x07|\x1b\][^\x07\x1b]*\x1b\\`)

// CreateSession creates a new tmux session with the given name, directory, and command.
func CreateSession(ctx context.Context, name, dir, command string) error {
	// tmux new-session -d -s <name> -c <dir> <command>
	args := []string{
		"new-session",
		"-d",       // detached
		"-s", name, // session name
		"-c", dir, // working directory
		command, // command to run
	}

	cmd := exec.CommandContext(ctx, "tmux", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create tmux session: %w: %s", err, string(output))
	}

	return nil
}

// SessionExists checks if a tmux session with the given name exists.
func SessionExists(ctx context.Context, name string) bool {
	// tmux has-session -t <name> (= prefix for exact match)
	args := []string{"has-session", "-t", "=" + name}

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
	// -S - captures from the start of the scrollback buffer (capture-pane does not support = prefix)
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

// CaptureLastLines captures the last N lines of the pane.
func CaptureLastLines(ctx context.Context, name string, lines int, includeEscapes bool) (string, error) {
	if lines <= 0 {
		return "", fmt.Errorf("invalid line count: %d", lines)
	}
	args := []string{"capture-pane"}
	if includeEscapes {
		args = append(args, "-e") // include escape sequences
	}
	args = append(args,
		"-p", // output to stdout
		"-S", fmt.Sprintf("-%d", lines),
		"-t", name, // target session/pane
	)

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
	// tmux kill-session -t <name> (= prefix for exact match)
	args := []string{"kill-session", "-t", "=" + name}

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
	// tmux send-keys -t <name> <keys> (send-keys does not support = prefix)
	args := []string{"send-keys", "-t", name, keys}

	cmd := exec.CommandContext(ctx, "tmux", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to send keys to tmux session: %w: %s", err, strings.TrimSpace(string(output)))
	}

	return nil
}

// SendLiteral sends literal text to a tmux session (spaces/newlines are treated as text).
func SendLiteral(ctx context.Context, name, text string) error {
	// tmux send-keys -l -t <name> <text> (send-keys does not support = prefix)
	args := []string{"send-keys", "-l", "-t", name, text}

	cmd := exec.CommandContext(ctx, "tmux", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to send literal text to tmux session: %w: %s", err, strings.TrimSpace(string(output)))
	}

	return nil
}

// GetAttachCommand returns the command to attach to a tmux session.
func GetAttachCommand(name string) string {
	return fmt.Sprintf("tmux attach -t \"=%s\"", name)
}

// StripAnsi removes ANSI escape sequences from text.
func StripAnsi(text string) string {
	return ansiRegex.ReplaceAllString(text, "")
}

// SetWindowSizeManual forces tmux to ignore client resize requests.
func SetWindowSizeManual(ctx context.Context, sessionName string) error {
	// set-option does not support = prefix for session target
	args := []string{"set-option", "-t", sessionName, "window-size", "manual"}
	cmd := exec.CommandContext(ctx, "tmux", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to set window-size manual: %w: %s", err, string(output))
	}
	return nil
}

// SetOption sets a tmux option on a session.
func SetOption(ctx context.Context, sessionName, option, value string) error {
	args := []string{"set-option", "-t", sessionName, option, value}
	cmd := exec.CommandContext(ctx, "tmux", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to set %s: %w: %s", option, err, string(output))
	}
	return nil
}

// ConfigureStatusBar sets the standard schmux status bar on a tmux session:
// process name on left, empty center, empty right.
func ConfigureStatusBar(ctx context.Context, sessionName string) {
	_ = SetOption(ctx, sessionName, "status-left", "#{pane_current_command} ")
	_ = SetOption(ctx, sessionName, "window-status-format", "")
	_ = SetOption(ctx, sessionName, "window-status-current-format", "")
	_ = SetOption(ctx, sessionName, "status-right", "")
}

// GetWindowSize returns the current tmux window size for the session.
func GetWindowSize(ctx context.Context, sessionName string) (width, height int, err error) {
	args := []string{
		"display-message",
		"-p",
		"-t", fmt.Sprintf("=%s:0.0", sessionName),
		"#{window_width} #{window_height}",
	}
	cmd := exec.CommandContext(ctx, "tmux", args...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if runErr := cmd.Run(); runErr != nil {
		return 0, 0, fmt.Errorf("failed to get window size: %w", runErr)
	}

	fields := strings.Fields(strings.TrimSpace(stdout.String()))
	if len(fields) < 2 {
		return 0, 0, fmt.Errorf("failed to parse window size output: %q", stdout.String())
	}
	w, convErr := strconv.Atoi(fields[0])
	if convErr != nil {
		return 0, 0, fmt.Errorf("failed to parse window width %q: %w", fields[0], convErr)
	}
	h, convErr := strconv.Atoi(fields[1])
	if convErr != nil {
		return 0, 0, fmt.Errorf("failed to parse window height %q: %w", fields[1], convErr)
	}
	return w, h, nil
}

// ResizeWindow resizes the window to fixed dimensions (80x24 for deterministic TUI).
func ResizeWindow(ctx context.Context, sessionName string, width, height int) error {
	args := []string{
		"resize-window",
		"-t", fmt.Sprintf("=%s:0.0", sessionName),
		"-x", strconv.Itoa(width),
		"-y", strconv.Itoa(height),
	}
	cmd := exec.CommandContext(ctx, "tmux", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to resize window: %w: %s", err, string(output))
	}
	return nil
}

// RenameSession renames an existing tmux session.
// This is used when updating session nicknames.
func RenameSession(ctx context.Context, oldName, newName string) error {
	args := []string{"rename-session", "-t", "=" + oldName, newName}
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

const (
	// MaxExtractedLines is the maximum number of lines to extract from terminal output.
	MaxExtractedLines = 40
)

// ExtractLatestResponse extracts the latest meaningful response from captured tmux lines.
func ExtractLatestResponse(lines []string) string {
	promptIdx := len(lines)
	for i := len(lines) - 1; i >= 0; i-- {
		if IsPromptLine(lines[i]) {
			promptIdx = i
			break
		}
	}

	var response []string
	contentCount := 0
	for i := promptIdx - 1; i >= 0; i-- {
		text := strings.TrimSpace(lines[i])
		if text == "" {
			continue
		}
		if IsPromptLine(text) {
			continue
		}
		if IsSeparatorLine(text) {
			continue
		}
		if IsAgentStatusLine(text) {
			continue
		}

		response = append([]string{text}, response...)
		contentCount++
		if contentCount >= MaxExtractedLines {
			break
		}
	}

	choices := extractChoiceLines(lines, promptIdx)
	if len(choices) > 0 {
		response = append(response, choices...)
	}

	return strings.Join(response, "\n")
}

func extractChoiceLines(lines []string, promptIdx int) []string {
	if promptIdx < 0 || promptIdx >= len(lines) {
		return nil
	}

	// First, find the index of the first choice line
	firstChoiceIdx := -1
	var contextLines []string
	for i := promptIdx; i < len(lines); i++ {
		text := strings.TrimSpace(lines[i])
		if text == "" {
			continue
		}

		// If this is a choice line, we found our start
		if IsChoiceLine(text) {
			firstChoiceIdx = i
			break
		}

		// Collect short non-separator context lines before choices
		if len(text) < 100 && !IsSeparatorLine(text) {
			contextLines = append(contextLines, text)
		} else {
			// Reset if we hit a long line or separator
			contextLines = nil
		}
	}

	if firstChoiceIdx == -1 {
		return nil
	}

	// Now collect all consecutive choice lines
	var choices []string
	choices = append(choices, contextLines...)
	for i := firstChoiceIdx; i < len(lines); i++ {
		text := strings.TrimSpace(lines[i])
		if text == "" {
			break
		}
		if !IsChoiceLine(text) {
			break
		}
		choices = append(choices, text)
	}

	if len(choices) < 2 {
		return nil
	}

	return choices
}

// IsSeparatorLine returns true if the line is mostly repeated separator characters.
func IsSeparatorLine(text string) bool {
	if len(text) < 10 {
		return false
	}
	runes := []rune(text)
	// Check if 80%+ of the line is the same character (dashes, equals, etc.)
	firstChar := runes[0]
	count := 0
	for _, c := range runes {
		if c == firstChar {
			count++
		}
	}
	return float64(count)/float64(len(runes)) > 0.8
}

// IsPromptLine returns true if the line looks like a shell prompt.
func IsPromptLine(text string) bool {
	trimmed := strings.TrimSpace(text)
	return strings.HasPrefix(trimmed, "❯") || strings.HasPrefix(trimmed, "›")
}

// IsChoiceLine returns true if the line looks like a prompt choice entry.
func IsChoiceLine(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}

	if strings.HasPrefix(trimmed, "❯") || strings.HasPrefix(trimmed, "›") {
		trimmed = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(trimmed, "❯"), "›"))
	}

	if trimmed == "" {
		return false
	}

	dot := strings.IndexAny(trimmed, ".)")
	if dot <= 0 {
		return false
	}

	for _, r := range trimmed[:dot] {
		if r < '0' || r > '9' {
			return false
		}
	}

	return true
}

// IsAgentStatusLine returns true if the line looks like agent UI noise.
func IsAgentStatusLine(text string) bool {
	// Filter out Claude Code's vertical bar status lines (⎿)
	trimmed := strings.TrimSpace(text)
	return strings.HasPrefix(trimmed, "⎿")
}
